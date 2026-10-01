package service

import (
	"crypto/sha256"
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
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
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
}

type FinanceUsageSnapshot struct {
	Quantities map[string]*string  `json:"quantities"`
	Source     string              `json:"source"`
	Estimate   FinanceCostEstimate `json:"estimate"`
	CostCNY    *string             `json:"estimated_cost_cny"`
	Missing    []string            `json:"missing"`
}

type FinanceBillingSnapshot struct {
	Scope            string `json:"scope"`
	ChargedQuota     *int   `json:"charged_quota"`
	FundingSource    string `json:"funding_source"`
	Status           string `json:"status"`
	AllocationStatus string `json:"allocation_status"`
}

func FinanceCaptureHealth() gin.H {
	return gin.H{"schema_ready": model.FinanceRequestsReady(model.DB) == nil, "capture_failures_since_process_start": financeCaptureFailures.Load(), "status": "partial", "warnings": []string{"best_effort_capture_not_atomic_with_wallet", "failure_counter_resets_on_restart", "transport_internal_retries_not_observed", "non_shared_transports_not_observed", "violation_fees_and_non_text_settlements_not_observed", "cash_receipts_and_source_allocation_not_connected"}}
}

func recordFinanceObservation(info *relaycommon.RelayInfo, attemptID, phase string, payload any) bool {
	defer recoverFinanceCapture()
	if !common.FinanceEnabled() {
		return false
	}
	if info == nil || info.RequestId == "" || len(info.RequestId) > 64 || len(attemptID) > 64 || info.UserId <= 0 || info.ChannelMeta == nil || info.ChannelId <= 0 || len(info.UpstreamModelName) > 200 || model.FinanceRequestsReady(model.DB) != nil {
		financeCaptureFailures.Add(1)
		return false
	}
	encoded, err := common.Marshal(payload)
	if err != nil || len(encoded) > 16384 {
		financeCaptureFailures.Add(1)
		return false
	}
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(info.RequestId+":"+attemptID+":"+phase)))
	entry := model.FinanceRequestEvent{EventKey: key, RequestID: info.RequestId, AttemptID: attemptID, Phase: phase, UserID: info.UserId, ChannelID: info.ChannelId, UpstreamModel: info.UpstreamModelName, At: time.Now().Unix(), Payload: string(encoded)}
	var previous model.FinanceRequestEvent
	if err := model.DB.Where("event_key = ?", key).Find(&previous).Error; err != nil {
		financeCaptureFailures.Add(1)
		return false
	}
	if previous.ID != 0 {
		if previous.Payload != entry.Payload || previous.UserID != entry.UserID || previous.ChannelID != entry.ChannelID || previous.UpstreamModel != entry.UpstreamModel {
			financeCaptureFailures.Add(1)
			return false
		}
		return true
	}
	if err := model.DB.Create(&entry).Error; err != nil {
		financeCaptureFailures.Add(1)
		return false
	}
	return true
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
	c.Set(financeAttemptContextKey, "")
	if model.FinanceRequestsReady(model.DB) != nil || info == nil || info.ChannelMeta == nil {
		financeCaptureFailures.Add(1)
		return ""
	}
	snapshot := FinanceAttemptSnapshot{Missing: []string{}}
	snapshot.RetryIndex = info.RetryIndex
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
	id := common.GetUUID()
	if !recordFinanceObservation(info, id, "attempt_start", snapshot) {
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
	recordFinanceObservation(info, id, "attempt_finish", struct {
		Failed bool `json:"failed"`
	}{failed})
	c.Set(financeAttemptContextKey, "")
}

// dto.Usage's scalar fields cannot distinguish omitted zero from reported
// zero. Only positive observed counters become known here. Absence stays nil.
func CaptureFinanceTextUsage(c *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage) {
	defer recoverFinanceCapture()
	if !common.FinanceEnabled() || c.GetString(financeAttemptContextKey) == "" {
		return
	}
	quantities := map[string]*string{}
	source := "unavailable"
	if usage != nil && !common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens) {
		source = "upstream_usage_scalar_presence_limited"
		input := int64(usage.PromptTokens)
		read := int64(usage.PromptTokensDetails.CachedTokens)
		write := int64(max(usage.PromptTokensDetails.CachedCreationTokens, usage.PromptTokensDetails.CacheWriteTokens))
		for _, quantity := range []int64{input, read, write, int64(usage.CompletionTokens), int64(usage.ClaudeCacheCreation5mTokens), int64(usage.ClaudeCacheCreation1hTokens)} {
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
		for meter, quantity := range map[string]int64{"input": input, "output": int64(usage.CompletionTokens), "cache_read": read, "cache_write": write, "cache_write_5m": int64(usage.ClaudeCacheCreation5mTokens), "cache_write_1h": int64(usage.ClaudeCacheCreation1hTokens)} {
			if quantity > 0 && quantity <= common.MaxWalletQuota {
				value := strconv.FormatInt(quantity, 10)
				quantities[meter] = &value
			}
		}
		if input < 0 || usage.PromptTokensDetails.AudioTokens != 0 || usage.PromptTokensDetails.ImageTokens != 0 || usage.CompletionTokenDetails.AudioTokens != 0 {
			quantities = map[string]*string{}
			source = "unsupported_or_inconsistent_usage"
		}
	}
	CaptureFinanceUsage(c, info, quantities, source)
}

func CaptureFinanceUsage(c *gin.Context, info *relaycommon.RelayInfo, quantities map[string]*string, source string) {
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
	var snapshot FinanceAttemptSnapshot
	if common.UnmarshalJsonStr(start.Payload, &snapshot) != nil {
		financeCaptureFailures.Add(1)
		return
	}
	result := FinanceUsageSnapshot{Quantities: quantities, Source: source, Missing: append([]string{}, snapshot.Missing...), Estimate: FinanceCostEstimate{Status: "unknown", Missing: []string{"cost_rule"}}}
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
	recordFinanceObservation(info, "", phase, FinanceBillingSnapshot{Scope: "billing_session_primary_funding", ChargedQuota: charged, FundingSource: funding, Status: status, AllocationStatus: "not_allocated_to_sources"})
}
