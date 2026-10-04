package accountpool

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCliproxyQuotaReadsOnlyPassiveObservation(t *testing.T) {
	calls := 0
	client, _ := newTestCliproxyClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/v0/management/auth-files" || r.URL.RawQuery != "" {
			t.Error("unexpected probe", r.Method, r.URL)
		}
		_, _ = w.Write([]byte(`{"files":[{"name":"synthetic.json","provider":"codex","quota":{"observed_at":"2026-10-04T00:00:00Z","signals":{"x-codex-primary-used-percent":"25","x-codex-primary-window-minutes":"300","x-codex-primary-reset-after-seconds":"60"}}}]}`))
	})
	got, err := client.QueryQuota(context.Background(), "synthetic.json", "codex")
	if err != nil || len(got.Metrics) != 2 || got.Metrics[1].Value != 75 {
		t.Fatal(got, err)
	}
	if _, err = client.QueryQuota(context.Background(), "synthetic.json", "grok"); err != ErrPoolUnsupported || calls != 1 {
		t.Fatal("unsupported queried network", calls, err)
	}
}

func TestQuotaCodexWindowsAndMissingFields(t *testing.T) {
	signals := map[string]any{"x-codex-primary-used-percent": "0", "x-codex-primary-window-minutes": "300", "x-codex-primary-reset-after-seconds": "60"}
	quota := map[string]any{"observed_at": "2026-10-04T00:00:00Z", "signals": signals}
	got := ParseCodexQuotaObservation(quota)
	if len(got.Metrics) != 2 || got.Metrics[1].Value != 100 || got.Metrics[1].Source != "upstream_derived" || got.Metrics[0].Scope != "shared_pool" || got.Metrics[0].Model != "" {
		t.Fatalf("explicit zero or shared scope lost: %+v", got)
	}
	for _, invalid := range []string{"", "NaN", "101", "-1"} {
		signals["x-codex-primary-used-percent"] = invalid
		got = ParseCodexQuotaObservation(quota)
		if got.Result != "unknown" || len(got.Metrics) != 0 {
			t.Fatal(invalid, got)
		}
	}
	signals["x-codex-primary-used-percent"] = "25"
	delete(quota, "observed_at")
	if got = ParseCodexQuotaObservation(quota); len(got.Metrics) != 0 {
		t.Fatal("unanchored data accepted")
	}
}

func TestQuotaConcurrentCacheFailureBackoffAndPersistence(t *testing.T) {
	cache := NewQuotaCache()
	now := time.Unix(1000, 0)
	cache.Now = func() time.Time { return now }
	var fetches atomic.Int32
	load := func() (QuotaView, error) { return QuotaView{}, nil }
	save := func(QuotaView) error { return nil }
	fetch := func() (QuotaView, error) {
		fetches.Add(1)
		return QuotaView{Result: "success", Metrics: []QuotaMetric{{Value: 30, Kind: "remaining_percent", UpdatedAt: 1000}}}, nil
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			view, err := cache.Read("same", load, fetch, save)
			if err != nil || view.Metrics[0].Value != 30 {
				t.Errorf("%+v %v", view, err)
			}
		}()
	}
	wg.Wait()
	if fetches.Load() != 1 {
		t.Fatal("query storm", fetches.Load())
	}
	now = now.Add(6 * time.Minute)
	fail := func() (QuotaView, error) { fetches.Add(1); return QuotaView{}, errors.New("fixed failure") }
	got, err := cache.Read("same", load, fail, save)
	if err != nil || got.Result != "failed" || !got.Stale || !got.Metrics[0].Stale || got.Metrics[0].Value != 30 || got.Metrics[0].QueryResult != "failed" {
		t.Fatal(got, err)
	}
	if _, err = cache.Read("same", load, fail, save); err != nil || fetches.Load() != 2 {
		t.Fatal("backoff bypass", err, fetches.Load())
	}
	restart := NewQuotaCache()
	restart.Now = cache.Now
	if _, err = restart.Read("same", func() (QuotaView, error) { return got, nil }, fail, save); err != nil || fetches.Load() != 2 {
		t.Fatal("persisted backoff bypass")
	}
	if _, err = cache.Read("db-error", func() (QuotaView, error) { return QuotaView{}, errors.New("load failed") }, fetch, save); err == nil {
		t.Fatal("database failure hidden")
	}
	if _, err = cache.Read("save-error", load, fetch, func(QuotaView) error { return errors.New("save failed") }); err == nil {
		t.Fatal("persistence failure hidden")
	}
}
