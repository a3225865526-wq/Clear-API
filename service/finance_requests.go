package service

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service/routechain"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const financeAttemptContextKey = "clearapi_private_finance_attempt"

var financeCaptureFailures atomic.Int64

func recoverFinanceCapture() {
	if recover() != nil {
		financeCaptureFailures.Add(1)
	}
}

type FinanceSellingSnapshot struct {
	Group           string  `json:"group"`
	BillingModel    string  `json:"billing_model"`
	ModelPrice      float64 `json:"model_price"`
	ModelRatio      float64 `json:"model_ratio"`
	OutputRatio     float64 `json:"output_ratio"`
	CacheReadRatio  float64 `json:"cache_read_ratio"`
	CacheWriteRatio float64 `json:"cache_write_ratio"`
	GroupRatio      float64 `json:"group_ratio"`
	UsePrice        bool    `json:"use_price"`
	QuotaPerUnit    float64 `json:"quota_per_unit"`
	ExpressionHash  string  `json:"expression_hash,omitempty"`
}

type FinanceAttemptSnapshot struct {
	RetryIndex    int                    `json:"retry_index"`
	PricingAt     int64                  `json:"pricing_at"`
	CostVersionID int64                  `json:"cost_version_id"`
	FXVersionID   int64                  `json:"fx_version_id"`
	PoolKey       string                 `json:"pool_key"`
	Missing       []string               `json:"missing"`
	Selling       FinanceSellingSnapshot `json:"selling"`
	// ChannelAccountIndex is the multi-key slot index inside the channel, a
	// reliable internal account reference. It is never a credential or key
	// material; without it account-level attribution stays at pool level.
	ChannelAccountIndex int `json:"channel_account_index,omitempty"`
}

type FinanceUsageSnapshot struct {
	Metering   TokenObservation    `json:"metering"`
	Quantities map[string]*string  `json:"quantities"`
	Source     string              `json:"source"`
	Estimate   FinanceCostEstimate `json:"estimate"`
	CostCNY    *string             `json:"estimated_cost_cny"`
	Missing    []string            `json:"missing"`
	// Observed carries upstream-reported quantities that are kept for
	// procurement review but deliberately excluded from quantities above,
	// so they never silently change the cost estimate. Sub-category token
	// counts (image/audio) are already included in the upstream input/output
	// semantics, and audio/video duration meters must come from supplier
	// billing facts, not from token counts.
	Observed map[string]*string `json:"observed,omitempty"`
}

type FinanceBillingSnapshot struct {
	Scope            string `json:"scope"`
	ChargedQuota     *int   `json:"charged_quota"`
	FundingSource    string `json:"funding_source"`
	Status           string `json:"status"`
	AllocationStatus string `json:"allocation_status"`
}

func FinanceCaptureHealth() gin.H {
	return gin.H{"schema_ready": model.FinanceRequestsReady(model.DB) == nil, "capture_failures_since_process_start": financeCaptureFailures.Load(), "status": "partial", "warnings": []string{"best_effort_capture_not_atomic_with_wallet", "failure_counter_resets_on_restart", "transport_internal_retries_not_observed", "violation_fees_not_observed", "midjourney_path_not_observed", "image_count_procurement_meter_not_observed", "task_facts_not_procurement_normalized", "reused_ws_responses_collapse_outbox_keys", "cash_receipts_and_source_allocation_not_connected"}}
}

func recordFinanceObservation(info *relaycommon.RelayInfo, attemptID, phase string, payload any) (int64, bool) {
	return recordFinanceObservationAt(info, attemptID, phase, payload, time.Now().Unix())
}

// at is the business fact time (send time, task finish time). It never
// rewrites an existing row; identical replays keep the original timestamp.
func recordFinanceObservationAt(info *relaycommon.RelayInfo, attemptID, phase string, payload any, at int64) (int64, bool) {
	defer recoverFinanceCapture()
	if !common.FinanceEnabled() {
		return 0, false
	}
	if info == nil || info.RequestId == "" || len(info.RequestId) > 64 || len(attemptID) > 64 || info.UserId <= 0 || info.ChannelMeta == nil || info.ChannelId <= 0 || len(info.UpstreamModelName) > 200 || model.FinanceRequestsReady(model.DB) != nil {
		financeCaptureFailures.Add(1)
		return 0, false
	}
	encoded, err := common.Marshal(payload)
	if err != nil || len(encoded) > 16384 {
		financeCaptureFailures.Add(1)
		return 0, false
	}
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(info.RequestId+":"+attemptID+":"+phase)))
	entry := model.FinanceRequestEvent{EventKey: key, RequestID: info.RequestId, AttemptID: attemptID, Phase: phase, UserID: info.UserId, ChannelID: info.ChannelId, UpstreamModel: info.UpstreamModelName, At: at, Payload: string(encoded)}
	var previous model.FinanceRequestEvent
	if err := model.DB.Where("event_key = ?", key).Find(&previous).Error; err != nil {
		financeCaptureFailures.Add(1)
		return 0, false
	}
	if previous.ID != 0 {
		if previous.Payload != entry.Payload || previous.UserID != entry.UserID || previous.ChannelID != entry.ChannelID || previous.UpstreamModel != entry.UpstreamModel {
			financeCaptureFailures.Add(1)
			return 0, false
		}
		return previous.ID, true
	}
	if err := model.DB.Create(&entry).Error; err != nil {
		financeCaptureFailures.Add(1)
		return 0, false
	}
	return entry.ID, true
}

// Look up the latest effective revision per key before checking Active/To.
// Deactivation/expiry masks an older revision. Multiple matching keys are
// ambiguous rather than selecting an arbitrary procurement price.
func financeEffectiveVersions(kind string, at int64) ([]model.FinanceVersion, error) {
	var rows []model.FinanceVersion
	if err := model.DB.Where("kind = ? AND effective_from <= ?", kind, at).Order("revision desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	result := []model.FinanceVersion{}
	for _, row := range rows {
		if seen[row.RecordKey] {
			continue
		}
		seen[row.RecordKey] = true
		if row.Active && at < row.EffectiveTo {
			result = append(result, row)
		}
	}
	return result, nil
}

// Called immediately before the shared HTTP transport sends, after mapping.
// No request/response bodies, URLs, headers or credential fields are read.
func BeginFinanceAttempt(c *gin.Context, info *relaycommon.RelayInfo) string {
	defer recoverFinanceCapture()
	if !common.FinanceEnabled() {
		return ""
	}
	// A still-open attempt means the previous gateway attempt failed and the
	// caller is retrying: close it as failed before its identity is replaced.
	if previous := c.GetString(financeAttemptContextKey); previous != "" {
		finishFinanceAttemptByID(c, info, previous, true)
	}
	c.Set(financeAttemptContextKey, "")
	if model.FinanceRequestsReady(model.DB) != nil || info == nil || info.ChannelMeta == nil {
		financeCaptureFailures.Add(1)
		return ""
	}
	snapshot := FinanceAttemptSnapshot{Missing: []string{}}
	snapshot.RetryIndex = info.RetryIndex
	snapshot.ChannelAccountIndex = info.ChannelMeta.ChannelMultiKeyIndex
	price := info.PriceData
	snapshot.Selling = FinanceSellingSnapshot{Group: info.UsingGroup, BillingModel: info.GetBillingModelName(), ModelPrice: price.ModelPrice, ModelRatio: price.ModelRatio, OutputRatio: price.CompletionRatio, CacheReadRatio: price.CacheRatio, CacheWriteRatio: price.CacheCreationRatio, GroupRatio: price.GroupRatioInfo.GroupRatio, UsePrice: price.UsePrice, QuotaPerUnit: common.QuotaPerUnit}
	if info.TieredBillingSnapshot != nil {
		snapshot.Selling.ExpressionHash = info.TieredBillingSnapshot.ExprHash
	}
	at := time.Now().Unix()
	snapshot.PricingAt = at
	versions, err := financeEffectiveVersions("cost", at)
	if err != nil {
		financeCaptureFailures.Add(1)
		return ""
	}
	var cost FinanceCostRule
	var matches int
	for _, version := range versions {
		var rule FinanceCostRule
		if common.UnmarshalJsonStr(version.Payload, &rule) != nil {
			financeCaptureFailures.Add(1)
			return ""
		}
		if rule.ChannelID == info.ChannelId && rule.UpstreamModel == info.UpstreamModelName {
			matches++
			cost, snapshot.CostVersionID, snapshot.PoolKey = rule, version.ID, rule.PoolKey
		}
	}
	if matches != 1 {
		snapshot.CostVersionID, snapshot.PoolKey = 0, ""
		snapshot.Missing = append(snapshot.Missing, "missing_or_ambiguous_cost_rule")
	}
	if snapshot.CostVersionID != 0 && cost.Currency != "CNY" {
		fxVersions, err := financeEffectiveVersions("fx", at)
		if err != nil {
			financeCaptureFailures.Add(1)
			return ""
		}
		var fxMatches int
		for _, version := range fxVersions {
			var fx FinanceFXRate
			if common.UnmarshalJsonStr(version.Payload, &fx) != nil {
				financeCaptureFailures.Add(1)
				return ""
			}
			if fx.Currency == cost.Currency {
				fxMatches++
				snapshot.FXVersionID = version.ID
			}
		}
		if fxMatches != 1 {
			snapshot.FXVersionID = 0
			snapshot.Missing = append(snapshot.Missing, "missing_or_ambiguous_fx")
		}
	}
	// Stage 4: a chain-routed request already carries the canonical attempt
	// id (route ledger); the finance attempt adopts it so finance events
	// join the ledger by attempt_id. Legacy requests keep the fresh UUID.
	id := common.GetUUID()
	if chainAttemptID := routechain.ActiveAttemptID(c); chainAttemptID != "" {
		id = chainAttemptID
	}
	if _, ok := recordFinanceObservation(info, id, "attempt_start", snapshot); !ok {
		return ""
	}
	c.Set(financeAttemptContextKey, id)
	return id
}

func CaptureFinanceTransport(info *relaycommon.RelayInfo, attemptID string, status int, transportFailed bool) {
	if attemptID == "" {
		return
	}
	recordFinanceObservation(info, attemptID, "transport_result", struct {
		Status     int    `json:"http_status"`
		Failed     bool   `json:"transport_failed"`
		CostStatus string `json:"cost_status"`
	}{status, transportFailed, "unknown_until_usage_or_bill"})
}

func FinishFinanceAttempt(c *gin.Context, info *relaycommon.RelayInfo, failed bool) {
	id := c.GetString(financeAttemptContextKey)
	if id == "" {
		return
	}
	c.Set(financeAttemptContextKey, "")
	finishFinanceAttemptByID(c, info, id, failed)
}

// finishFinanceAttemptByID closes an attempt. A failed attempt without any
// usage observation gets an explicit unknown-usage fact, so a failed request
// is never silently treated as free procurement; a successful attempt without
// usage (e.g. an async submission) gets the same explicit marker.
func finishFinanceAttemptByID(c *gin.Context, info *relaycommon.RelayInfo, id string, failed bool) {
	defer recoverFinanceCapture()
	if id == "" {
		return
	}
	var start model.FinanceRequestEvent
	if err := model.DB.Where("attempt_id = ? AND phase = ?", id, "attempt_start").First(&start).Error; err == nil {
		var count int64
		if err := model.DB.Model(&model.FinanceRequestEvent{}).Where("attempt_id = ? AND phase = ?", id, "usage").Count(&count).Error; err == nil && count == 0 {
			source := "no_usage_failed_attempt"
			if !failed {
				source = "no_usage_without_settlement"
			}
			CaptureFinanceUsageWithIdentity(c, info, id, map[string]*string{}, source, start)
		}
	}
	identity := financeAttemptIdentity(start)
	recordFinanceObservation(identity, id, "attempt_finish", struct {
		Failed bool `json:"failed"`
	}{failed})
}

// financeAttemptIdentity attributes follow-up facts to the immutable send
// facts recorded at attempt start rather than metadata that may have changed
// during retries.
func financeAttemptIdentity(start model.FinanceRequestEvent) *relaycommon.RelayInfo {
	if start.ID == 0 {
		return nil
	}
	return &relaycommon.RelayInfo{RequestId: start.RequestID, UserId: start.UserID, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: start.ChannelID, UpstreamModelName: start.UpstreamModel}}
}

// dto.Usage's scalar fields cannot distinguish omitted zero from reported
// zero. Only positive observed counters become known here. Absence stays nil.
// Sub-category image/audio token counts stay procurement-observable facts:
// they are already inside the upstream input/output semantics, so they are
// recorded under Observed and never silently reprice the estimate.
func CaptureFinanceTextUsage(c *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage) {
	defer recoverFinanceCapture()
	if !common.FinanceEnabled() || c.GetString(financeAttemptContextKey) == "" {
		return
	}
	if usage != nil && usage.CodexMetering != nil {
		m := usage.CodexMetering
		if m.Status != "exact" {
			CaptureFinanceUsage(c, info, map[string]*string{}, "codex_authoritative_usage_unavailable")
			return
		}
		input, output, cached, reasoning, total := strconv.FormatInt(*m.Input-*m.Cached, 10), strconv.FormatInt(*m.Output, 10), strconv.FormatInt(*m.Cached, 10), strconv.FormatInt(*m.Reasoning, 10), strconv.FormatInt(*m.Total, 10)
		zero := "0"
		CaptureFinanceUsage(c, info, map[string]*string{"input": &input, "output": &output, "cache_read": &cached, "cache_write": &zero}, "codex_authoritative_exact", map[string]*string{"reasoning_tokens": &reasoning, "total_tokens": &total})
		return
	}
	quantities := map[string]*string{}
	observed := map[string]*string{}
	source := "unavailable"
	if usage != nil && !common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens) {
		source = "upstream_usage_scalar_presence_limited"
		input := int64(usage.PromptTokens)
		read := int64(usage.PromptTokensDetails.CachedTokens)
		write := int64(max(usage.PromptTokensDetails.CachedCreationTokens, usage.PromptTokensDetails.CacheWriteTokens))
		for _, quantity := range []int64{input, read, write, int64(usage.CompletionTokens), int64(usage.ClaudeCacheCreation5mTokens), int64(usage.ClaudeCacheCreation1hTokens), int64(usage.PromptTokensDetails.ImageTokens), int64(usage.PromptTokensDetails.AudioTokens), int64(usage.CompletionTokenDetails.AudioTokens)} {
			if quantity < 0 || quantity > common.MaxWalletQuota {
				CaptureFinanceUsage(c, info, quantities, "out_of_range_usage")
				return
			}
		}
		splitWrite := int64(usage.ClaudeCacheCreation5mTokens) + int64(usage.ClaudeCacheCreation1hTokens)
		if write == 0 {
			write = splitWrite
		}
		if splitWrite > 0 && write != splitWrite {
			CaptureFinanceUsage(c, info, quantities, "inconsistent_cache_write_usage")
			return
		}
		if usageSemanticFromUsage(info, usage) != "anthropic" {
			input -= read + write
		}
		if input < 0 {
			CaptureFinanceUsage(c, info, quantities, "inconsistent_usage_semantics")
			return
		}
		for meter, quantity := range map[string]int64{"input": input, "output": int64(usage.CompletionTokens), "cache_read": read, "cache_write": write, "cache_write_5m": int64(usage.ClaudeCacheCreation5mTokens), "cache_write_1h": int64(usage.ClaudeCacheCreation1hTokens)} {
			if quantity > 0 && quantity <= common.MaxWalletQuota {
				value := strconv.FormatInt(quantity, 10)
				quantities[meter] = &value
			}
		}
		for meter, quantity := range map[string]int64{"image_input_tokens": int64(usage.PromptTokensDetails.ImageTokens), "audio_input_tokens": int64(usage.PromptTokensDetails.AudioTokens), "audio_output_tokens": int64(usage.CompletionTokenDetails.AudioTokens)} {
			if quantity > 0 && quantity <= common.MaxWalletQuota {
				value := strconv.FormatInt(quantity, 10)
				observed[meter] = &value
			}
		}
	}
	CaptureFinanceUsage(c, info, quantities, source, observed)
}

func CaptureFinanceUsage(c *gin.Context, info *relaycommon.RelayInfo, quantities map[string]*string, source string, observed ...map[string]*string) {
	defer recoverFinanceCapture()
	id := c.GetString(financeAttemptContextKey)
	if !common.FinanceEnabled() || id == "" {
		return
	}
	var start model.FinanceRequestEvent
	if err := model.DB.Where("attempt_id = ? AND phase = ?", id, "attempt_start").First(&start).Error; err != nil {
		financeCaptureFailures.Add(1)
		return
	}
	CaptureFinanceUsageWithIdentity(c, info, id, quantities, source, start, observed...)
}

// CaptureFinanceUsageWithIdentity records a usage fact for an already resolved
// attempt start row, so attempt close can attach the explicit no-usage marker.
func CaptureFinanceUsageWithIdentity(c *gin.Context, info *relaycommon.RelayInfo, id string, quantities map[string]*string, source string, start model.FinanceRequestEvent, observed ...map[string]*string) {
	defer recoverFinanceCapture()
	if !common.FinanceEnabled() || id == "" {
		return
	}
	var snapshot FinanceAttemptSnapshot
	if common.UnmarshalJsonStr(start.Payload, &snapshot) != nil {
		financeCaptureFailures.Add(1)
		return
	}
	result := FinanceUsageSnapshot{Quantities: quantities, Source: source, Missing: append([]string{}, snapshot.Missing...), Estimate: FinanceCostEstimate{Status: "unknown", Missing: []string{"cost_rule"}}}
	result.Metering = TokenObservationForLog(c, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
	if info != nil {
		result.Metering.InternalModel, result.Metering.ModelAlias = info.UpstreamModelName, info.OriginModelName
	}
	if len(observed) > 0 {
		merged := map[string]*string{}
		for _, entry := range observed {
			for meter, value := range entry {
				merged[meter] = value
			}
		}
		result.Observed = merged
	}
	if snapshot.CostVersionID != 0 {
		var version model.FinanceVersion
		var rule FinanceCostRule
		if model.DB.First(&version, snapshot.CostVersionID).Error != nil || common.UnmarshalJsonStr(version.Payload, &rule) != nil {
			financeCaptureFailures.Add(1)
			return
		}
		estimate, err := EstimateFinanceCost(rule, quantities)
		if err != nil {
			result.Missing = append(result.Missing, "invalid_usage")
			estimate.Amount, estimate.Status = nil, "unknown"
		}
		priced := map[string]bool{}
		for _, price := range rule.Prices {
			priced[price.Meter] = true
		}
		for meter, value := range quantities {
			if value != nil && !priced[meter] && !(meter == "cache_write" && (priced["cache_write_5m"] || priced["cache_write_1h"])) && !((meter == "cache_write_5m" || meter == "cache_write_1h") && priced["cache_write"]) {
				estimate.Amount, estimate.Status = nil, "unknown"
				estimate.Missing = append(estimate.Missing, "unpriced_"+meter)
			}
		}
		result.Estimate = estimate
		slices.Sort(result.Estimate.Missing)
		if estimate.Amount != nil {
			amount, err := decimal.NewFromString(*estimate.Amount)
			if err != nil {
				financeCaptureFailures.Add(1)
				return
			}
			if rule.Currency == "CNY" {
				value := amount.String()
				result.CostCNY = &value
			} else if snapshot.FXVersionID != 0 {
				rate, err := FinanceFrozenRate(model.DB, snapshot.FXVersionID, rule.Currency, snapshot.PricingAt)
				if err != nil {
					result.Missing = append(result.Missing, "invalid_frozen_fx")
				} else {
					value := amount.Mul(rate).String()
					result.CostCNY = &value
				}
			}
		}
	}
	// Attribute the usage to the immutable send facts rather than any metadata
	// the provider's response handling may subsequently change.
	identity := &relaycommon.RelayInfo{RequestId: start.RequestID, UserId: start.UserID, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: start.ChannelID, UpstreamModelName: start.UpstreamModel}}
	recordFinanceObservation(identity, id, "usage", result)
}

func CaptureFinanceBilling(info *relaycommon.RelayInfo, phase string, charged *int, funding, status string) {
	if !common.FinanceEnabled() {
		return
	}
	observationID, ok := recordFinanceObservation(info, "", phase, FinanceBillingSnapshot{Scope: "billing_session_primary_funding", ChargedQuota: charged, FundingSource: funding, Status: status, AllocationStatus: "not_allocated_to_sources"})
	if !ok || phase != "text_settlement" {
		return
	}
	emitFinanceSettlementEvent(info, observationID, charged, funding, status)
}

// Emission happens only after the settlement is durably landed: with batch
// updates enabled the quota change still sits in the in-memory queue, so no
// reliable fact is claimed and the gap stays visible in the health sample.
func emitFinanceSettlementEvent(info *relaycommon.RelayInfo, observationID int64, charged *int, funding, status string) {
	defer recoverFinanceCapture()
	if observationID <= 0 || charged == nil || *charged <= 0 || int64(*charged) > common.MaxWalletQuota || funding != BillingSourceWallet || status != "settled" || common.BatchUpdateEnabled {
		return
	}
	if info == nil || info.RequestId == "" || info.UserId <= 0 || model.FinanceSyncReady(model.DB) != nil {
		financeCaptureFailures.Add(1)
		return
	}
	now := time.Now().Unix()
	entry := model.FinanceSyncEvent{EventKey: "auto-text-settlement-" + info.RequestId, Topic: "consumption", UserID: info.UserId, RefID: observationID, At: now, Payload: "{}", Status: model.FinanceSyncPending, NextAttemptAt: now, CreatedAt: now}
	var duplicate model.FinanceSyncEvent
	if err := model.DB.Where("event_key = ?", entry.EventKey).First(&duplicate).Error; err == nil {
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		financeCaptureFailures.Add(1)
		return
	}
	if err := model.DB.Create(&entry).Error; err != nil {
		financeCaptureFailures.Add(1)
	}
}

// CaptureFinanceRealtimeUsage records the Realtime session usage from the
// upstream summary. OpenAI realtime semantics apply: input_tokens already
// contains the audio and cached subsets, so cache reads are subtracted from
// input for procurement quantities, and audio details stay in Observed.
func CaptureFinanceRealtimeUsage(c *gin.Context, info *relaycommon.RelayInfo, usage *dto.RealtimeUsage) {
	defer recoverFinanceCapture()
	if !common.FinanceEnabled() || c.GetString(financeAttemptContextKey) == "" {
		return
	}
	quantities := map[string]*string{}
	observed := map[string]*string{}
	source := "unavailable"
	if usage != nil {
		source = "upstream_usage_scalar_presence_limited"
		input := int64(usage.InputTokens)
		read := int64(usage.InputTokenDetails.CachedTokens)
		for _, quantity := range []int64{input, read, int64(usage.OutputTokens), int64(usage.InputTokenDetails.AudioTokens), int64(usage.OutputTokenDetails.AudioTokens)} {
			if quantity < 0 || quantity > common.MaxWalletQuota {
				CaptureFinanceUsage(c, info, quantities, "out_of_range_usage")
				return
			}
		}
		input -= read
		for meter, quantity := range map[string]int64{"input": input, "output": int64(usage.OutputTokens), "cache_read": read} {
			if quantity > 0 && quantity <= common.MaxWalletQuota {
				value := strconv.FormatInt(quantity, 10)
				quantities[meter] = &value
			}
		}
		for meter, quantity := range map[string]int64{"audio_input_tokens": int64(usage.InputTokenDetails.AudioTokens), "audio_output_tokens": int64(usage.OutputTokenDetails.AudioTokens)} {
			if quantity > 0 && quantity <= common.MaxWalletQuota {
				value := strconv.FormatInt(quantity, 10)
				observed[meter] = &value
			}
		}
	}
	CaptureFinanceUsage(c, info, quantities, source, observed)
}

// FinanceAttemptIDFromContext exposes the open attempt reference so a durable
// asynchronous task can link its later settlement facts back to the frozen
// procurement snapshot of the submitting request. It is an internal
// observation reference, never a credential.
func FinanceAttemptIDFromContext(c *gin.Context) string {
	return c.GetString(financeAttemptContextKey)
}

const financeTaskRequestPrefix = "task:"

// FinanceBillingSnapshot gains an optional attempt link for task settlements;
// text settlements keep it empty and unchanged.
type FinanceTaskBillingSnapshot struct {
	FinanceBillingSnapshot
	FinanceAttemptID string `json:"finance_attempt_id,omitempty"`
	TaskID           string `json:"task_id,omitempty"`
	Platform         string `json:"platform,omitempty"`
	Action           string `json:"action,omitempty"`
}

type FinanceTaskAdjustmentSnapshot struct {
	Delta            int            `json:"delta"`
	ActualQuota      int            `json:"actual_quota"`
	PreConsumedQuota int            `json:"pre_consumed_quota"`
	Reason           string         `json:"reason"`
	UsageFacts       map[string]any `json:"usage_facts,omitempty"`
	FinanceAttemptID string         `json:"finance_attempt_id,omitempty"`
}

type FinanceTaskRefundSnapshot struct {
	Quota            int    `json:"quota"`
	Reason           string `json:"reason"`
	Refunded         bool   `json:"refunded"`
	FinanceAttemptID string `json:"finance_attempt_id,omitempty"`
}

func financeTaskIdentity(task *model.Task) *relaycommon.RelayInfo {
	if task == nil || task.TaskID == "" || len(financeTaskRequestPrefix+task.TaskID) > 64 || task.UserId <= 0 || task.ChannelId <= 0 {
		return nil
	}
	return &relaycommon.RelayInfo{RequestId: financeTaskRequestPrefix + task.TaskID, UserId: task.UserId, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: task.ChannelId, UpstreamModelName: task.Properties.UpstreamModelName}}
}

func recordFinanceTaskObservation(task *model.Task, attemptID, phase string, payload any, at int64) (int64, bool) {
	identity := financeTaskIdentity(task)
	if identity == nil {
		financeCaptureFailures.Add(1)
		return 0, false
	}
	return recordFinanceObservationAt(identity, attemptID, phase, payload, at)
}

// CaptureFinanceTaskSubmission records the submit-time settlement of an
// asynchronous task. It deliberately does not emit a sync outbox event: the
// submit settlement is an estimate that polling later adjusts, so importing it
// as a consumption fact would double-count the task.
func CaptureFinanceTaskSubmission(task *model.Task, charged *int, funding, status, attemptID string) {
	if !common.FinanceEnabled() {
		return
	}
	at := time.Now().Unix()
	if task != nil && task.SubmitTime > 0 {
		at = task.SubmitTime
	}
	recordFinanceTaskObservation(task, "submit", "task_submission", FinanceTaskBillingSnapshot{
		FinanceBillingSnapshot: FinanceBillingSnapshot{Scope: "billing_session_primary_funding", ChargedQuota: charged, FundingSource: funding, Status: status, AllocationStatus: "not_allocated_to_sources"},
		FinanceAttemptID:       attemptID,
		TaskID:                 task.TaskID,
		Platform:               string(task.Platform),
		Action:                 task.Action,
	}, at)
}

// CaptureFinanceTaskAdjustment records one polling-time delta settlement. The
// attempt key derives from the delta outcome, so replaying the same settlement
// is idempotent while a genuinely different adjustment stays a new fact.
func CaptureFinanceTaskAdjustment(task *model.Task, actualQuota, preConsumedQuota int, reason string, usageFacts map[string]any) {
	if !common.FinanceEnabled() {
		return
	}
	delta := actualQuota - preConsumedQuota
	at := time.Now().Unix()
	if task != nil && task.FinishTime > 0 {
		at = task.FinishTime
	}
	snapshot := FinanceTaskAdjustmentSnapshot{Delta: delta, ActualQuota: actualQuota, PreConsumedQuota: preConsumedQuota, Reason: reason, UsageFacts: usageFacts}
	if task != nil {
		snapshot.FinanceAttemptID = task.PrivateData.FinanceAttemptID
	}
	attemptID := fmt.Sprintf("adjust-delta%d-actual%d", delta, actualQuota)
	recordFinanceTaskObservation(task, attemptID, "task_adjustment", snapshot, at)
}

// CaptureFinanceTaskRefund records a polling-time task failure refund. A
// failed refund stays a marker only; it never claims money moved.
func CaptureFinanceTaskRefund(task *model.Task, quota int, reason string, refunded bool) {
	if !common.FinanceEnabled() {
		return
	}
	at := time.Now().Unix()
	snapshot := FinanceTaskRefundSnapshot{Quota: quota, Reason: reason, Refunded: refunded}
	if task != nil {
		if task.FinishTime > 0 {
			at = task.FinishTime
		}
		snapshot.FinanceAttemptID = task.PrivateData.FinanceAttemptID
	}
	recordFinanceTaskObservation(task, "refund", "task_refund", snapshot, at)
}
