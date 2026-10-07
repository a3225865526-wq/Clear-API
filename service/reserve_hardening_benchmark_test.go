package service_test

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

// BenchmarkReserveHardeningFullChain measures the enforced hardening path end
// to end: visible estimate, dispatch estimate, durable reserve+raise, and the
// durable settlement of a hidden-usage request on an isolated SQLite DB.
func BenchmarkReserveHardeningFullChain(b *testing.B) {
	newHardeningDB(b)
	configureTieredModel(b, h2ModelA, fixtureAExpr)
	profileA := coldProfile(4480)
	obj := config.GlobalConfig.Get("billing_reserve_policy")
	require.NoError(b, config.UpdateConfigFromMap(obj, map[string]string{
		"mode": "enforce", "margin": "1.10", "floor_quota": "0", "max_request_quota": "5000",
		"version": "1", "unknown_action": "quarantine",
		"profiles": profilesJSON(b, map[string]map[string]any{"channel::1::model::gpt-6-luna": profileA}),
	}))
	u := createHardeningUser(b, model.DB, "bench", 1000000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		flow := startHardeningFlow(b, u, h2ChannelA, h2ModelA, h2OutboundBodyA)
		if err := flow.dispatch(b); err != nil {
			b.Fatal(err)
		}
		flow.settle(b, hiddenUsageFixtureA())
	}
	b.StopTimer()
}

// BenchmarkDispatchEstimate isolates the estimator hot path (policy resolve +
// domain bound) without any database work.
func BenchmarkDispatchEstimate(b *testing.B) {
	setPolicy(b, operation_setting.ReserveModeEnforce, profilesJSON(b, map[string]map[string]any{
		"channel::1::model::gpt-6-luna": coldProfile(4480),
	}))
	info := estimateRelayInfo(1, "gpt-6-luna", fixtureAExpr, 1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := service.EstimateDispatchReservation(info, 16, 30, 1, 0)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkLegacyOutboundBudget measures the frozen outbound budget path for
// comparison (visible-only upper bound on the same expression).
func BenchmarkLegacyOutboundBudget(b *testing.B) {
	info := estimateRelayInfo(1, "gpt-6-luna", fixtureAExpr, 1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := billingexpr.ReservationUpperBound(info.TieredBillingSnapshot.ExprString, 16, 30, 1)
		if err != nil {
			b.Fatal(err)
		}
	}
}
