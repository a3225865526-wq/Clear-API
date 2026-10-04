package accountpool

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const quotaTTL = 5 * time.Minute

// QuotaMetric deliberately keeps units and scope separate. Shared pools are
// represented once, never added to each model's quota.
type QuotaMetric struct {
	Kind          string  `json:"kind"`
	Value         float64 `json:"value"`
	Unit          string  `json:"unit"`
	Scope         string  `json:"scope"`
	Pool          string  `json:"pool"`
	Model         string  `json:"model,omitempty"`
	WindowSeconds *int64  `json:"window_seconds,omitempty"`
	ResetAt       *int64  `json:"reset_at,omitempty"`
	Source        string  `json:"source"`
	UpdatedAt     int64   `json:"updated_at"`
	Stale         bool    `json:"stale"`
	QueryResult   string  `json:"query_result"`
}

type QuotaView struct {
	QueryCapability string        `json:"query_capability"`
	Result          string        `json:"result"`
	Metrics         []QuotaMetric `json:"metrics"`
	CheckedAt       int64         `json:"checked_at"`
	NextRefreshAt   int64         `json:"next_refresh_at"`
	Stale           bool          `json:"stale"`
	Reason          string        `json:"reason,omitempty"`
}

type QuotaExecutor interface {
	QueryQuota(context.Context, string, string) (QuotaView, error)
}

// QueryQuota only reads the pinned executor's passive observation endpoint.
// No generation request, arbitrary plugin probe or credential download exists.
func (c *CliproxyClient) QueryQuota(ctx context.Context, ref, platform string) (QuotaView, error) {
	if platform != "codex" {
		return QuotaView{}, ErrPoolUnsupported
	}
	status, payload, err := c.do(ctx, http.MethodGet, "/v0/management/auth-files", "")
	if err != nil {
		return QuotaView{}, err
	}
	if status != http.StatusOK {
		return QuotaView{}, ErrPoolExecutorFailed
	}
	files, ok := mapField[[]any](payload, "files")
	if !ok {
		return QuotaView{}, ErrPoolExecutorFailed
	}
	for _, file := range files {
		entry, ok := file.(map[string]any)
		if !ok || entry["name"] != ref {
			continue
		}
		if provider, _ := entry["provider"].(string); provider != "codex" {
			return QuotaView{}, ErrPoolExecutorFailed
		}
		quota, _ := entry["quota"].(map[string]any)
		return ParseCodexQuotaObservation(quota), nil
	}
	return QuotaView{}, ErrPoolNotFound
}

// ParseCodexQuotaObservation uses the window semantics of the pinned
// helps/codex_quota.go: a complete window needs explicit used%, duration and
// recovery information. No percentage becomes a token or request count.
func ParseCodexQuotaObservation(quota map[string]any) QuotaView {
	view := QuotaView{QueryCapability: "observations_only", Result: "unknown", Metrics: []QuotaMetric{}}
	observedText, _ := quota["observed_at"].(string)
	observed, err := time.Parse(time.RFC3339, observedText)
	if err != nil {
		return view
	}
	raw, _ := quota["signals"].(map[string]any)
	signals := make(map[string]string)
	for k, v := range raw {
		if s, ok := v.(string); ok {
			signals[strings.ToLower(k)] = s
		}
	}
	for _, window := range []string{"primary", "secondary"} {
		prefix := "x-codex-" + window + "-"
		used, usedErr := strconv.ParseFloat(signals[prefix+"used-percent"], 64)
		minutes, windowErr := strconv.ParseInt(signals[prefix+"window-minutes"], 10, 64)
		reset, resetErr := strconv.ParseInt(signals[prefix+"reset-at"], 10, 64)
		if resetErr != nil || reset <= 0 {
			after, afterErr := strconv.ParseInt(signals[prefix+"reset-after-seconds"], 10, 64)
			if afterErr != nil || after < 0 || after > 366*86400 {
				continue
			}
			reset = observed.Unix() + after
		}
		if usedErr != nil || math.IsNaN(used) || math.IsInf(used, 0) || used < 0 || used > 100 || windowErr != nil || minutes <= 0 || minutes > 366*1440 {
			continue
		}
		seconds := minutes * 60
		metric := QuotaMetric{Kind: "used_percent", Value: used, Unit: "%", Scope: "shared_pool", Pool: window,
			WindowSeconds: &seconds, ResetAt: &reset, Source: "upstream", UpdatedAt: observed.Unix()}
		view.Metrics = append(view.Metrics, metric)
		metric.Kind, metric.Value, metric.Source = "remaining_percent", 100-used, "upstream_derived"
		view.Metrics = append(view.Metrics, metric)
	}
	if len(view.Metrics) > 0 {
		view.Result = "success"
	}
	return view
}

type quotaCacheEntry struct {
	view     QuotaView
	failures int
}
type QuotaCache struct {
	mu      sync.Mutex
	entries map[string]quotaCacheEntry
	flights singleflight.Group
	Now     func() time.Time
}

func NewQuotaCache() *QuotaCache {
	return &QuotaCache{entries: make(map[string]quotaCacheEntry), Now: time.Now}
}

var poolQuotaCache = NewQuotaCache()

func markQuotaStale(view QuotaView, now int64) QuotaView {
	view.Metrics = append([]QuotaMetric{}, view.Metrics...)
	view.Stale = view.Result == "failed"
	for i := range view.Metrics {
		m := &view.Metrics[i]
		m.QueryResult = view.Result
		m.Stale = view.Result == "failed" || m.UpdatedAt <= now-int64(quotaTTL.Seconds()) || m.UpdatedAt > now || (m.ResetAt != nil && *m.ResetAt <= now)
		view.Stale = view.Stale || m.Stale
	}
	return view
}

// Read deduplicates concurrent refreshes, throttles both success and failures,
// and preserves the last snapshot on error. force refresh intentionally does
// not bypass the server throttle.
func (cache *QuotaCache) Read(key string, load func() (QuotaView, error), fetch func() (QuotaView, error), save func(QuotaView) error) (QuotaView, error) {
	result, err, _ := cache.flights.Do(key, func() (any, error) {
		now := cache.Now().Unix()
		cache.mu.Lock()
		entry, exists := cache.entries[key]
		cache.mu.Unlock()
		if !exists {
			var err error
			entry.view, err = load()
			if err != nil {
				return QuotaView{}, err
			}
		}
		if entry.view.NextRefreshAt > now {
			return markQuotaStale(entry.view, now), nil
		}
		fresh, fetchErr := fetch()
		if fetchErr != nil {
			entry.failures++
			entry.view.Result, entry.view.Reason = "failed", executorErrorText(fetchErr)
			entry.view.NextRefreshAt = now + int64(time.Minute.Seconds())*int64(1<<min(entry.failures, 4))
		} else {
			entry.failures = 0
			fresh.CheckedAt, fresh.NextRefreshAt = now, now+int64(quotaTTL.Seconds())
			entry.view = fresh
		}
		entry.view.CheckedAt = now
		if err := save(entry.view); err != nil {
			return QuotaView{}, err
		}
		cache.mu.Lock()
		if _, present := cache.entries[key]; !present && len(cache.entries) >= 1024 {
			for existing, saved := range cache.entries {
				if saved.view.NextRefreshAt <= now {
					delete(cache.entries, existing)
				}
			}
			if len(cache.entries) >= 1024 {
				cache.mu.Unlock()
				return QuotaView{}, ErrPoolConflict
			}
		}
		cache.entries[key] = entry
		cache.mu.Unlock()
		return markQuotaStale(entry.view, now), nil
	})
	if err != nil {
		return QuotaView{}, err
	}
	return result.(QuotaView), nil
}

func QuotaForAccount(account *model.UpstreamPoolAccount, refresh bool) (QuotaView, error) {
	unknown := QuotaView{QueryCapability: "unsupported", Result: "unsupported", Metrics: []QuotaMetric{}, Reason: "no_verified_quota_adapter"}
	if account.Platform != "codex" {
		return unknown, nil
	}
	unknown.QueryCapability, unknown.Result, unknown.Reason = "observations_only", "unknown", "no_upstream_observation"
	if account.CredentialRef == "" {
		return unknown, nil
	}
	load := func() (QuotaView, error) {
		var saved model.UpstreamPoolQuotaSnapshot
		err := db().First(&saved, account.Id).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return QuotaView{}, err
		}
		if err == nil && saved.CredentialRef == account.CredentialRef {
			var view QuotaView
			if common.UnmarshalJsonStr(saved.Payload, &view) == nil {
				return view, nil
			}
			return QuotaView{}, ErrPoolExecutorFailed
		}
		return unknown, nil
	}
	if !refresh {
		view, err := load()
		return markQuotaStale(view, time.Now().Unix()), err
	}
	key := strconv.Itoa(account.Id) + ":" + account.ExecutorId + ":" + account.CredentialRef
	return poolQuotaCache.Read(key, load, func() (QuotaView, error) {
		executor, err := executorFor(account.ExecutorId)
		if err != nil {
			return QuotaView{}, err
		}
		query, ok := executor.(QuotaExecutor)
		if !ok {
			return QuotaView{QueryCapability: "unsupported", Result: "unsupported", Metrics: []QuotaMetric{}, Reason: "no_verified_quota_adapter"}, nil
		}
		return query.QueryQuota(context.Background(), account.CredentialRef, account.Platform)
	}, func(view QuotaView) error {
		payload, err := common.Marshal(view)
		if err != nil {
			return err
		}
		saved := model.UpstreamPoolQuotaSnapshot{AccountId: account.Id, CredentialRef: account.CredentialRef, Payload: string(payload)}
		return db().Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "account_id"}}, DoUpdates: clause.AssignmentColumns([]string{"credential_ref", "payload"})}).Create(&saved).Error
	})
}
