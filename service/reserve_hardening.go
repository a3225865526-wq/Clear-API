package service

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/shopspring/decimal"
)

// EstimatorVersion identifies the reservation hardening estimator release
// stored inside every authorization snapshot.
const EstimatorVersion = "h2-reserve-v1"

// Sentinel dispatch failures. They must surface as billing errors before any
// upstream request is sent; they never clamp the reservation and retry.
var (
	ErrReserveQuarantine   = errors.New("no approved reserve profile for this channel/model")
	ErrReserveCapExceeded  = errors.New("required reservation exceeds the authorization cap")
	ErrReserveDomainBroken = errors.New("final request exceeds the approved usage domain")
)

// ReserveEstimate is the frozen dispatch-time authorization decision. Every
// field is a billing scalar or version identifier, safe to persist for audit.
type ReserveEstimate struct {
	PolicyVersion      int
	Mode               string
	EstimatorVersion   string
	ProfileSource      string
	ProfileVersion     int
	ProfileApproved    bool
	CacheMode          string
	PricingMode        string
	PricingFingerprint string
	GroupRatio         float64
	QuotaPerUnit       float64
	Margin             float64
	FloorQuota         int64

	VisibleInputMax       int64
	HiddenInputMax        int64
	HiddenCacheReadMax    int64
	HiddenCacheWriteMax   int64
	HiddenCacheWrite1hMax int64
	OutputMax             int64
	ContextLenMax         int64

	DomainQuota   float64 // engine upper bound over the usage domain, before margin/floor
	ToolQuota     float64
	RequiredQuota int // margin/floor applied, tools added, capped by authorization cap
	CapQuota      int
	ReservedQuota int // final reservation the dispatcher will hold (max with the visible estimate)
}

// EstimateDispatchReservation resolves the approved hidden usage envelope for
// the dispatch candidate (channel + final upstream model) and prices its
// usage-domain upper bound with the real pricing engine. It returns a typed
// error instead of an unbounded estimate when the candidate is quarantined,
// the domain is exceeded, or the policy itself is invalid.
func EstimateDispatchReservation(info *relaycommon.RelayInfo, promptTokens int, output float64, imageCount int, toolQuota float64) (*ReserveEstimate, error) {
	if info == nil || info.ChannelMeta == nil {
		return nil, fmt.Errorf("reserve hardening requires a dispatch candidate")
	}
	policy := operation_setting.GetBillingReservePolicy()
	margin, floor, err := operation_setting.EffectiveMarginFloor(policy)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrReserveDomainBroken, err)
	}
	channelID := info.ChannelMeta.ChannelId
	finalModel := info.GetUpstreamModelName()
	profile, ok := operation_setting.ResolveReserveProfile(policy, channelID, finalModel)
	if !ok {
		return nil, fmt.Errorf("%w: channel %d model %s", ErrReserveQuarantine, channelID, finalModel)
	}

	estimate := &ReserveEstimate{
		PolicyVersion:         policy.Version,
		Mode:                  policy.Mode,
		EstimatorVersion:      EstimatorVersion,
		ProfileSource:         profile.Source,
		ProfileVersion:        profile.Version,
		ProfileApproved:       true,
		CacheMode:             profile.CacheMode,
		GroupRatio:            info.PriceData.GroupRatioInfo.GroupRatio,
		QuotaPerUnit:          common.QuotaPerUnit,
		Margin:                margin,
		FloorQuota:            floor,
		VisibleInputMax:       int64(promptTokens),
		HiddenInputMax:        profile.HiddenInputMax,
		HiddenCacheReadMax:    profile.HiddenCacheReadMax,
		HiddenCacheWriteMax:   profile.HiddenCacheWriteMax,
		HiddenCacheWrite1hMax: profile.HiddenCacheWrite1hMax,
		OutputMax:             int64(math.Ceil(output)),
		ContextLenMax:         profile.HiddenInputMax + profile.HiddenCacheReadMax + profile.HiddenCacheWriteMax + profile.HiddenCacheWrite1hMax + int64(promptTokens),
		CapQuota:              int(profile.MaxRequestQuota),
	}

	hiddenTotal := profile.HiddenInputMax + profile.HiddenCacheReadMax + profile.HiddenCacheWriteMax + profile.HiddenCacheWrite1hMax
	if profile.ModelContextMax > 0 && estimate.ContextLenMax > profile.ModelContextMax {
		return nil, fmt.Errorf("%w: visible+hidden context %d exceeds approved model_context_max %d", ErrReserveDomainBroken, estimate.ContextLenMax, profile.ModelContextMax)
	}
	if profile.ModelOutputMax > 0 && estimate.OutputMax > profile.ModelOutputMax {
		return nil, fmt.Errorf("%w: output budget %d exceeds approved model_output_max %d", ErrReserveDomainBroken, estimate.OutputMax, profile.ModelOutputMax)
	}

	if snap := info.TieredBillingSnapshot; snap != nil {
		quota, fingerprint, tieredErr := estimateTieredDomainQuota(info, snap, promptTokens, output, imageCount, profile, hiddenTotal)
		if tieredErr != nil {
			return nil, tieredErr
		}
		estimate.PricingMode = billing_setting_mode_tiered
		estimate.PricingFingerprint = fingerprint
		estimate.DomainQuota = quota
	} else if !info.PriceData.UsePrice {
		quota, fingerprint, ratioErr := estimateRatioDomainQuota(info, promptTokens, output, profile)
		if ratioErr != nil {
			return nil, ratioErr
		}
		estimate.PricingMode = billing_setting_mode_ratio
		estimate.PricingFingerprint = fingerprint
		estimate.DomainQuota = quota
	} else {
		// A per-request price cannot under-charge hidden token usage.
		estimate.PricingMode = billing_setting_mode_request
		estimate.PricingFingerprint = fmt.Sprintf("price:%s:%d", info.GetBillingModelName(), policy.Version)
		estimate.DomainQuota = 0
	}

	estimate.ToolQuota = toolQuota
	if estimate.PricingMode != billing_setting_mode_request {
		domainDecimal := decimal.NewFromFloat(estimate.DomainQuota)
		marginDecimal := decimal.NewFromFloat(estimate.Margin)
		floorDecimal := decimal.NewFromInt(estimate.FloorQuota)
		required := decimal.Max(domainDecimal.Mul(marginDecimal), domainDecimal.Add(floorDecimal)).Ceil()
		if estimate.ToolQuota > 0 {
			required = required.Add(decimal.NewFromFloat(estimate.ToolQuota * estimate.GroupRatio))
		}
		requiredQuota, clamp := common.QuotaFromDecimalChecked(required)
		if clamp != nil {
			return nil, fmt.Errorf("%w: reservation estimate overflows the quota range", ErrReserveDomainBroken)
		}
		estimate.RequiredQuota = requiredQuota
		// The authorization cap is a rejection gate, never a clamp: a request
		// whose required reservation exceeds it must not be dispatched.
		if estimate.RequiredQuota > estimate.CapQuota {
			return nil, fmt.Errorf("%w: required %d exceeds cap %d (profile %s)", ErrReserveCapExceeded, estimate.RequiredQuota, estimate.CapQuota, estimate.ProfileSource)
		}
	}
	return estimate, nil
}

const (
	billing_setting_mode_tiered  = "tiered_expr"
	billing_setting_mode_ratio   = "ratio"
	billing_setting_mode_request = "per_request"
)

func estimateTieredDomainQuota(info *relaycommon.RelayInfo, snap *billingexpr.BillingSnapshot, promptTokens int, output float64, imageCount int, profile *operation_setting.ResolvedReserveProfile, hiddenTotal int64) (float64, string, error) {
	usedVars := billingexpr.UsedVarsByHash(snap.ExprString, snap.ExprHash)
	// Hidden image/audio usage has no profile domain; an expression that prices
	// those variables cannot be bounded safely and must fail before dispatch.
	for _, variable := range []string{"img", "img_cr", "ai", "img_o", "ao"} {
		if usedVars[variable] {
			return 0, "", fmt.Errorf("%w: expression prices %s but the reserve profile has no such hidden domain", ErrReserveDomainBroken, variable)
		}
	}
	domain := billingexpr.UsageDomain{
		P:          float64(promptTokens) + float64(profile.HiddenInputMax),
		CR:         float64(profile.HiddenCacheReadMax),
		CC:         float64(profile.HiddenCacheWriteMax),
		CC1h:       float64(profile.HiddenCacheWrite1hMax),
		C:          output,
		Len:        float64(promptTokens) + float64(hiddenTotal),
		ImageCount: imageCount,
	}
	if BillingProtectionEnabled() {
		// The visible prompt can be read from or written to cache too. Do not
		// assume that absent cache-write usage means free cache writes. Bound
		// every input category by the whole approved context envelope; the
		// interval evaluator deliberately over-reserves mutually exclusive
		// splits, without changing the actual settlement tariff.
		domain.P, domain.CR, domain.CC, domain.CC1h = domain.Len, domain.Len, domain.Len, domain.Len
	}
	upper, err := billingexpr.ReservationDomainUpperBound(snap.ExprString, domain)
	if err != nil {
		return 0, "", fmt.Errorf("%w: %w", ErrReserveDomainBroken, err)
	}
	quotaPerUnit := snap.QuotaPerUnit
	if quotaPerUnit <= 0 {
		quotaPerUnit = common.QuotaPerUnit
	}
	groupRatio := snap.GroupRatio
	if groupRatio == 0 {
		groupRatio = info.PriceData.GroupRatioInfo.GroupRatio
	}
	quota := upper / 1_000_000 * quotaPerUnit * groupRatio
	return quota, "expr:" + snap.ExprHash, nil
}

func estimateRatioDomainQuota(info *relaycommon.RelayInfo, promptTokens int, output float64, profile *operation_setting.ResolvedReserveProfile) (float64, string, error) {
	price := info.PriceData
	groupRatio := price.GroupRatioInfo.GroupRatio
	// Every hidden token is covered: each category carries the maximum of its
	// own price ratio and the ordinary input ratio 1.0, so any actual split
	// inside the profile costs no more than this bound under either the OpenAI
	// (prompt includes cache) or Anthropic (prompt excludes cache) semantics.
	weightedInput := float64(int64(promptTokens)+profile.HiddenInputMax) +
		float64(profile.HiddenCacheReadMax)*math.Max(1, price.CacheRatio) +
		float64(profile.HiddenCacheWriteMax)*math.Max(1, price.CacheCreation5mRatio) +
		float64(profile.HiddenCacheWrite1hMax)*math.Max(1, price.CacheCreation1hRatio)
	outputRatio := math.Max(price.CompletionRatio, price.AudioRatio*price.AudioCompletionRatio)
	if math.IsNaN(weightedInput) || math.IsInf(weightedInput, 0) {
		return 0, "", fmt.Errorf("%w: invalid ratio budget", ErrReserveDomainBroken)
	}
	quota := (weightedInput + output*outputRatio) * price.ModelRatio * groupRatio
	if math.IsNaN(quota) || math.IsInf(quota, 0) {
		return 0, "", fmt.Errorf("%w: invalid ratio budget", ErrReserveDomainBroken)
	}
	fingerprint := ratioFingerprint(info.GetBillingModelName(), price.ModelRatio, price.CompletionRatio, price.CacheRatio, price.CacheCreation5mRatio, price.CacheCreation1hRatio, groupRatio)
	return quota, fingerprint, nil
}

// ratioFingerprint freezes the exact ratio set used for the bound so a later
// restart can detect that settlement would no longer price identically.
func ratioFingerprint(modelName string, ratios ...float64) string {
	canonical := modelName
	for _, ratio := range ratios {
		canonical += fmt.Sprintf("|%.9f", ratio)
	}
	sum := sha256.Sum256([]byte(canonical))
	return "ratio:" + fmt.Sprintf("%x", sum[:16])
}

// UsageAnomaly is the settlement-side detection result for actual usage that
// escaped the approved hidden usage domain.
type UsageAnomaly struct {
	Anomaly         bool   `json:"anomaly"`
	Reason          string `json:"reason"`
	ProfileSource   string `json:"profile_source,omitempty"`
	PolicyVersion   int    `json:"policy_version,omitempty"`
	VisibleEstimate int    `json:"visible_estimate"`
	ActualLen       int    `json:"actual_len"`
	HiddenDelta     int    `json:"hidden_delta"`
	AllowedHidden   int    `json:"allowed_hidden"`
}

// EvaluateTextUsageAnomaly compares the settled usage length against the
// approved hidden envelope. It never re-prices or re-authorizes anything: the
// caller only uses it to retain the reservation for review instead of
// treating an out-of-domain usage as ordinary income.
func EvaluateTextUsageAnomaly(info *relaycommon.RelayInfo, usage *dto.Usage, isClaudeUsageSemantic bool, usedVars map[string]bool) UsageAnomaly {
	anomaly := UsageAnomaly{}
	if usage == nil {
		return anomaly
	}
	mode := operation_setting.ReservationHardeningMode()
	if mode == operation_setting.ReserveModeOff {
		return anomaly
	}
	// Malformed provider claims are anomalies regardless of any profile.
	negative := usage.PromptTokens < 0 || usage.CompletionTokens < 0 ||
		usage.PromptTokensDetails.CachedTokens < 0 ||
		usage.PromptTokensDetails.CacheCreationTokensTotal() < 0
	oversized := usage.PromptTokens > math.MaxInt32/2 || usage.CompletionTokens > math.MaxInt32/2
	if negative || oversized {
		anomaly.Anomaly = true
		if negative {
			anomaly.Reason = "negative_usage_field"
		} else {
			anomaly.Reason = "usage_out_of_representable_range"
		}
		return anomaly
	}

	policy := operation_setting.GetBillingReservePolicy()
	if info == nil || info.ChannelMeta == nil {
		return anomaly
	}
	profile, ok := operation_setting.ResolveReserveProfile(policy, info.ChannelMeta.ChannelId, info.GetUpstreamModelName())
	if !ok {
		return anomaly
	}
	anomaly.ProfileSource = profile.Source
	anomaly.PolicyVersion = policy.Version

	tokens := BuildTieredTokenParams(usage, isClaudeUsageSemantic, usedVars)
	visible := info.GetEstimatePromptTokens()
	actualLen := int(tokens.Len)
	delta := actualLen - visible
	if delta < 0 {
		delta = 0
	}
	anomaly.VisibleEstimate = visible
	anomaly.ActualLen = actualLen
	anomaly.HiddenDelta = delta

	allowed := profile.HiddenTotalMax
	if relative := int64(math.Ceil(float64(visible) * profile.HiddenRelativeLimit)); relative > allowed {
		allowed = relative
	}
	if profile.ModelContextMax > 0 && allowed > profile.ModelContextMax {
		allowed = profile.ModelContextMax
	}
	anomaly.AllowedHidden = int(allowed)
	if int64(delta) > allowed {
		anomaly.Anomaly = true
		anomaly.Reason = "hidden_usage_exceeds_domain"
	}
	return anomaly
}

// ToAuthSnapshot converts the frozen estimate into its durable form.
func (e *ReserveEstimate) ToAuthSnapshot(requestID string) *model.BillingAuthSnapshot {
	if e == nil {
		return nil
	}
	fingerprint := e.PricingFingerprint
	if len(fingerprint) > 80 {
		fingerprint = fingerprint[:80]
	}
	return &model.BillingAuthSnapshot{
		RequestID:             requestID,
		PolicyVersion:         e.PolicyVersion,
		Mode:                  e.Mode,
		EstimatorVersion:      e.EstimatorVersion,
		ProfileSource:         e.ProfileSource,
		ProfileVersion:        e.ProfileVersion,
		ProfileApproved:       e.ProfileApproved,
		CacheMode:             e.CacheMode,
		PricingMode:           e.PricingMode,
		PricingFingerprint:    fingerprint,
		GroupRatio:            e.GroupRatio,
		QuotaPerUnit:          e.QuotaPerUnit,
		Margin:                e.Margin,
		FloorQuota:            e.FloorQuota,
		VisibleInputMax:       e.VisibleInputMax,
		HiddenInputMax:        e.HiddenInputMax,
		HiddenCacheReadMax:    e.HiddenCacheReadMax,
		HiddenCacheWriteMax:   e.HiddenCacheWriteMax,
		HiddenCacheWrite1hMax: e.HiddenCacheWrite1hMax,
		OutputMax:             e.OutputMax,
		ContextLenMax:         e.ContextLenMax,
		RequiredQuota:         e.RequiredQuota,
		CapQuota:              e.CapQuota,
		ReservedQuota:         e.ReservedQuota,
	}
}

// ReserveEstimateFingerprint is a compact identity for logging without any
// request content.
func ReserveEstimateFingerprint(estimate *ReserveEstimate) string {
	if estimate == nil {
		return ""
	}
	encoded, err := json.Marshal(map[string]any{
		"policy": estimate.PolicyVersion, "profile": estimate.ProfileSource,
		"required": estimate.RequiredQuota, "cap": estimate.CapQuota,
	})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", sum[:8])
}
