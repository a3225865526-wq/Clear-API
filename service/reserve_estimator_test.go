package service_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fixtureAExpr = `len <= 272000 ? tier("upstream_1", p*0.02 + cr*0.002 + cc*0.025 + cc1h*0.025 + c*0.10) : tier("upstream_2", p*0.04 + cr*0.04 + cc*0.05 + cc1h*0.05 + c*0.15)`
const fixtureBExpr = `len <= 272000 ? tier("upstream_1", p*0.24 + cr*0.012 + cc*0.30 + cc1h*0.30 + c*1.20) : tier("upstream_2", p*0.48 + cr*0.024 + cc*0.60 + cc1h*0.60 + c*1.80)`

// setPolicy replaces the billing_reserve_policy module fields. Scalar option
// values are stored raw (no JSON quoting) by the config registry; the
// profiles map is the one JSON-encoded field.

func mustReservePolicyObj(t testing.TB) any {
	t.Helper()
	return config.GlobalConfig.Get("billing_reserve_policy")
}

func setPolicy(t testing.TB, mode string, profilesJSON string) {
	t.Helper()
	require.NoError(t, config.UpdateConfigFromMap(mustReservePolicyObj(t), map[string]string{
		"mode":              mode,
		"unknown_action":    operation_setting.ReserveUnknownQuarantine,
		"margin":            "1.10",
		"floor_quota":       "0",
		"max_request_quota": "5000",
		"version":           "1",
	}))
	if profilesJSON == "" {
		profilesJSON = "{}"
	}
	require.NoError(t, config.UpdateConfigFromMap(mustReservePolicyObj(t), map[string]string{
		"profiles": profilesJSON,
	}))
	t.Cleanup(func() {
		_ = config.UpdateConfigFromMap(mustReservePolicyObj(t), map[string]string{
			"mode":     "off",
			"profiles": "{}",
		})
	})
}

func coldProfile(hiddenTotal int64) map[string]any {
	return map[string]any{
		"approved":          true,
		"version":           1,
		"cache_mode":        "cold",
		"hidden_total_max":  hiddenTotal,
		"model_context_max": 400000,
		"model_output_max":  30000,
	}
}

func profilesJSON(t testing.TB, profiles map[string]map[string]any) string {
	t.Helper()
	encoded, err := json.Marshal(profiles)
	require.NoError(t, err)
	return string(encoded)
}

func estimateRelayInfo(channelID int, finalModel, expr string, groupRatio float64) *relaycommon.RelayInfo {
	snap := &billingexpr.BillingSnapshot{
		BillingMode:           "tiered_expr",
		ModelName:             finalModel,
		ExprString:            expr,
		ExprHash:              billingexpr.ExprHashString(expr),
		GroupRatio:            groupRatio,
		EstimatedPromptTokens: 16,
		QuotaPerUnit:          500000,
		ExprVersion:           billingexpr.ExprVersion(expr),
	}
	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: snap,
	}
	info.OriginModelName = finalModel
	info.ChannelMeta = &relaycommon.ChannelMeta{ChannelId: channelID, UpstreamModelName: finalModel}
	info.PriceData.GroupRatioInfo.GroupRatio = groupRatio
	return info
}

func TestEstimateDispatchReservationFixtureCold(t *testing.T) {
	setPolicy(t, operation_setting.ReserveModeEnforce, profilesJSON(t, map[string]map[string]any{
		"channel::1::model::gpt-6-luna":  coldProfile(4480),
		"channel::2::model::gpt-6.1-sol": coldProfile(4480),
	}))

	estimate, err := service.EstimateDispatchReservation(estimateRelayInfo(1, "gpt-6-luna", fixtureAExpr, 1), 16, 30, 1, 0)
	require.NoError(t, err)
	assert.Equal(t, 52, estimate.RequiredQuota, "fixture A cold reserve must reproduce the H1 verified 52")
	assert.Equal(t, 5000, estimate.CapQuota)
	assert.Equal(t, "channel::1::model::gpt-6-luna", estimate.ProfileSource)
	assert.Equal(t, "cold", estimate.CacheMode)
	assert.True(t, strings.HasPrefix(estimate.PricingFingerprint, "expr:"))
	assert.Equal(t, int64(4496), estimate.ContextLenMax, "cold: visible + full hidden total")

	estimateB, err := service.EstimateDispatchReservation(estimateRelayInfo(2, "gpt-6.1-sol", fixtureBExpr, 1), 16, 30, 1, 0)
	require.NoError(t, err)
	assert.Equal(t, 614, estimateB.RequiredQuota, "fixture B cold reserve must reproduce the H1 verified 614")
}

func TestEstimateDispatchReservationFixtureWarm(t *testing.T) {
	warm := func(hiddenTotal int64) map[string]any {
		profile := coldProfile(hiddenTotal)
		profile["cache_mode"] = "warm"
		profile["hidden_input_max"] = 640
		profile["hidden_cache_read_max"] = 3840
		profile["hidden_cache_write_max"] = 0
		profile["hidden_cache_write_1h_max"] = 0
		return profile
	}
	setPolicy(t, operation_setting.ReserveModeEnforce, profilesJSON(t, map[string]map[string]any{
		"channel::1::model::gpt-6-luna":  warm(4480),
		"channel::2::model::gpt-6.1-sol": warm(4480),
	}))

	estimate, err := service.EstimateDispatchReservation(estimateRelayInfo(1, "gpt-6-luna", fixtureAExpr, 1), 16, 30, 1, 0)
	require.NoError(t, err)
	assert.Equal(t, 14, estimate.RequiredQuota, "fixture A warm reserve must reproduce the H1 verified 14")
	assert.Equal(t, int64(4496), estimate.ContextLenMax)

	estimateB, err := service.EstimateDispatchReservation(estimateRelayInfo(2, "gpt-6.1-sol", fixtureBExpr, 1), 16, 30, 1, 0)
	require.NoError(t, err)
	assert.Equal(t, 132, estimateB.RequiredQuota, "fixture B warm reserve must reproduce the H1 verified 132")
}

func TestEstimateDispatchReservationQuarantineAndDomain(t *testing.T) {
	setPolicy(t, operation_setting.ReserveModeEnforce, profilesJSON(t, map[string]map[string]any{
		"model::gpt-6-luna": coldProfile(4480),
	}))

	// Unknown channel/model without an approved profile quarantines.
	_, err := service.EstimateDispatchReservation(estimateRelayInfo(9, "unknown-model", fixtureAExpr, 1), 16, 30, 1, 0)
	require.ErrorIs(t, err, service.ErrReserveQuarantine)

	// A model profile still applies for a known model on an unknown channel.
	estimate, err := service.EstimateDispatchReservation(estimateRelayInfo(7, "gpt-6-luna", fixtureAExpr, 1), 16, 30, 1, 0)
	require.NoError(t, err)
	assert.Equal(t, 52, estimate.RequiredQuota)
	assert.Equal(t, "model::gpt-6-luna", estimate.ProfileSource)

	// Output beyond the approved profile domain fails before dispatch.
	_, err = service.EstimateDispatchReservation(estimateRelayInfo(7, "gpt-6-luna", fixtureAExpr, 1), 16, 40000, 1, 0)
	require.ErrorIs(t, err, service.ErrReserveDomainBroken)

	// Context beyond the approved profile domain fails before dispatch.
	_, err = service.EstimateDispatchReservation(estimateRelayInfo(7, "gpt-6-luna", fixtureAExpr, 1), 396000, 30, 1, 0)
	require.ErrorIs(t, err, service.ErrReserveDomainBroken)
}

func TestEstimateDispatchReservationCapExceeded(t *testing.T) {
	setPolicy(t, operation_setting.ReserveModeEnforce, profilesJSON(t, map[string]map[string]any{
		"channel::1::model::gpt-6-luna": coldProfile(4480),
	}))

	// Tier crossing: 270000 visible + 4000 hidden must reserve at the
	// expensive tier and exceed the initial 5000 quota cap.
	_, err := service.EstimateDispatchReservation(estimateRelayInfo(1, "gpt-6-luna", fixtureAExpr, 1), 270000, 30, 1, 0)
	require.ErrorIs(t, err, service.ErrReserveCapExceeded)

	// With the cap raised the same domain reserves 6042 at tier2 pricing:
	// P=274480 crosses the 272000 boundary so the expensive branch dominates.
	require.NoError(t, config.UpdateConfigFromMap(mustReservePolicyObj(t), map[string]string{"max_request_quota": "80000"}))
	estimate, err := service.EstimateDispatchReservation(estimateRelayInfo(1, "gpt-6-luna", fixtureAExpr, 1), 270000, 30, 1, 0)
	require.NoError(t, err)
	assert.Equal(t, 6042, estimate.RequiredQuota)
	tier1Only, err := service.EstimateDispatchReservation(estimateRelayInfo(1, "gpt-6-luna", `tier("upstream_1", p*0.02 + cr*0.002 + cc*0.025 + cc1h*0.025 + c*0.10)`, 1), 270000, 30, 1, 0)
	require.NoError(t, err)
	assert.Less(t, tier1Only.RequiredQuota, estimate.RequiredQuota, "crossing the tier boundary must reserve at the expensive tier")
}

func TestEstimateDispatchReservationRatioPath(t *testing.T) {
	info := estimateRelayInfo(1, "ratio-model", fixtureAExpr, 1)
	info.TieredBillingSnapshot = nil
	info.PriceData.ModelRatio = 0.04
	info.PriceData.CompletionRatio = 3
	info.PriceData.CacheRatio = 0.5
	info.PriceData.GroupRatioInfo.GroupRatio = 1
	setPolicy(t, operation_setting.ReserveModeEnforce, profilesJSON(t, map[string]map[string]any{
		"channel::1::model::ratio-model": coldProfile(4480),
	}))

	// Cold: every hidden token priced at input ratio 1.0.
	// (16+4480 + 30*3) * 0.04 = 183.44 quota -> margin 1.10 -> 201.784 -> ceil 202
	estimate, err := service.EstimateDispatchReservation(info, 16, 30, 1, 0)
	require.NoError(t, err)
	assert.Equal(t, 202, estimate.RequiredQuota)
	assert.True(t, strings.HasPrefix(estimate.PricingFingerprint, "ratio:"))
	assert.Equal(t, "ratio", estimate.PricingMode)
}

func TestEstimateDispatchReservationMonitorModeDoesNotEnforce(t *testing.T) {
	setPolicy(t, operation_setting.ReserveModeMonitor, "{}")
	// Monitor mode resolves and estimates without an approved profile
	// returning a quarantine error; the dispatcher decides what to log.
	_, err := service.EstimateDispatchReservation(estimateRelayInfo(1, "gpt-6-luna", fixtureAExpr, 1), 16, 30, 1, 0)
	require.ErrorIs(t, err, service.ErrReserveQuarantine)
}

func TestEstimateDispatchReservationInvalidPolicyFailsClosed(t *testing.T) {
	setPolicy(t, operation_setting.ReserveModeEnforce, profilesJSON(t, map[string]map[string]any{
		"channel::1::model::gpt-6-luna": coldProfile(4480),
	}))
	require.NoError(t, config.UpdateConfigFromMap(mustReservePolicyObj(t), map[string]string{"margin": "3.0"}))
	t.Cleanup(func() { _ = config.UpdateConfigFromMap(mustReservePolicyObj(t), map[string]string{"margin": "1.10"}) })

	_, err := service.EstimateDispatchReservation(estimateRelayInfo(1, "gpt-6-luna", fixtureAExpr, 1), 16, 30, 1, 0)
	require.ErrorIs(t, err, service.ErrReserveDomainBroken)
}

func TestReserveEstimateSnapshotConversion(t *testing.T) {
	setPolicy(t, operation_setting.ReserveModeEnforce, profilesJSON(t, map[string]map[string]any{
		"channel::1::model::gpt-6-luna": coldProfile(4480),
	}))
	estimate, err := service.EstimateDispatchReservation(estimateRelayInfo(1, "gpt-6-luna", fixtureAExpr, 1), 16, 30, 1, 0)
	require.NoError(t, err)
	estimate.ReservedQuota = 52
	snapshot := estimate.ToAuthSnapshot("req-1")
	require.NotNil(t, snapshot)
	assert.Equal(t, "req-1", snapshot.RequestID)
	assert.Equal(t, "h2-reserve-v1", snapshot.EstimatorVersion)
	assert.Equal(t, "channel::1::model::gpt-6-luna", snapshot.ProfileSource)
	assert.Equal(t, int64(4480), snapshot.HiddenInputMax)
	assert.Equal(t, int64(0), snapshot.HiddenCacheReadMax, "cold profile collapses every hidden category to ordinary input")
	assert.Equal(t, 52, snapshot.RequiredQuota)
	assert.Equal(t, 52, snapshot.ReservedQuota)
	assert.NotContains(t, snapshot.ProfileSource, "key", "snapshot must not carry credentials")
}

func TestValidateBillingReserveOptionRejectsUnsafeValues(t *testing.T) {
	require.Error(t, operation_setting.ValidateBillingReserveOption("billing_reserve_policy.mode", `"always"`))
	require.Error(t, operation_setting.ValidateBillingReserveOption("billing_reserve_policy.margin", `0.5`))
	require.Error(t, operation_setting.ValidateBillingReserveOption("billing_reserve_policy.margin", `5`))
	require.Error(t, operation_setting.ValidateBillingReserveOption("billing_reserve_policy.max_request_quota", `-1`))
	require.Error(t, operation_setting.ValidateBillingReserveOption("billing_reserve_policy.profiles", `{"x":1}`))
	require.NoError(t, operation_setting.ValidateBillingReserveOption("billing_reserve_policy.mode", "enforce"))
	require.NoError(t, operation_setting.ValidateBillingReserveOption("billing_reserve_policy.margin", "1.1"))
	require.NoError(t, operation_setting.ValidateBillingReserveOption("billing_reserve_policy.profiles", `{"channel::1::model::m":{"approved":true,"cache_mode":"cold","hidden_total_max":4480}}`))
	require.Error(t, operation_setting.ValidateBillingReserveOption("billing_reserve_policy.profiles", `{"weird-key":{"approved":true,"cache_mode":"cold"}}`))
}
