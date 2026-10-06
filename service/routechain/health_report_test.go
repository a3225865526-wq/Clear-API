package routechain

import (
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestParseRetryAfterSeconds(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		max  int
		want int
	}{
		{"empty falls back", "", 300, 5},
		{"delta seconds", "42", 300, 42},
		{"delta capped", "999999999", 300, 300},
		{"negative falls back", "-5", 300, 5},
		{"garbage falls back", "soon", 300, 5},
		{"overflow falls back", "99999999999999999999999", 300, 5},
		{"http date future", "Sun, 06 Nov 2044 08:49:37 GMT", 300, 300},
		{"http date past falls back", "Sun, 06 Nov 2000 08:49:37 GMT", 300, 5},
		{"max clamped to one", "5000", 0, 1},
		{"zero seconds falls back", "0", 300, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, ParseRetryAfterSeconds(tc.raw, tc.max))
		})
	}
}

// A probe held for candidate A is only visible to A's report; the report
// releases it exactly once.
func TestProbeHeldCorrelation(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.False(t, probeHeldFor(c, 1))
	markProbeHeld(c, 1)
	require.True(t, probeHeldFor(c, 1))
	require.False(t, probeHeldFor(c, 2), "another candidate's report must not release A's probe")
	releaseProbeHeld(c)
	require.False(t, probeHeldFor(c, 1))
}

// ReportAttemptHealthFailure with the breaker gate off is a no-op returning
// an empty effect (C29: routing/health behavior identical to Stage 4).
func TestReportHealthInertWhenGateOff(t *testing.T) {
	common.SetRouteCircuitBreakerEnabledForTest(false)
	t.Cleanup(func() { common.SetRouteCircuitBreakerEnabledForTest(false) })
	state := stateFixture(1)
	health := ComputeAttemptHealth(stateTestContext(), state, HealthSignals{Outcome: OutcomeRequestWrittenOutcomeUncertain})
	require.Equal(t, AttemptHealth{}, health)
}

// The full failure-path orchestration: verdict + breaker update + 429
// cooldown from the captured header (C19).
func TestReportAttemptHealthFailureOrchestration(t *testing.T) {
	common.SetRouteCircuitBreakerEnabledForTest(true)
	t.Cleanup(func() { common.SetRouteCircuitBreakerEnabledForTest(false) })
	withBreakerDefaults(t, 3, 60, 1, 300)
	var now atomic.Int64
	now.Store(1000)
	registry := newTestRegistry(&now)
	oldBreaker := Breaker
	Breaker = registry
	t.Cleanup(func() { Breaker = oldBreaker })

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "req-health-1")

	state := stateFixture(1)
	state.MarkAttempt(c, 0) // candidate id 1 is now the failing candidate

	health := ComputeAttemptHealth(c, state, HealthSignals{
		Outcome:  OutcomeRateLimitRejectionNoWork,
		Evidence: Evidence{RequestWritten: true, HTTPStatus: 429, UpstreamErrorCode: "rate_limit_exceeded"},
	})
	require.Equal(t, BreakerEffectCooldown, health.Effect)
	require.Equal(t, HealthNeutral, health.Verdict)
	snap := registry.CandidateSnapshot(1, 1, nil)
	require.Equal(t, CircuitStateOpen, snap.State)
	require.Equal(t, 0, snap.ConsecutiveFailures, "429 must not count failures")
	require.Equal(t, int64(1005), snap.OpenUntil, "no captured header → bounded default cooldown (5s)")
	require.Equal(t, int64(0), snap.Counters["circuit_open_total"])
	require.Equal(t, int64(1), snap.Counters["retry_after_cooldown_total"])
	require.Equal(t, string(OutcomeRateLimitRejectionNoWork), snap.LastFailureClass, "class recorded even on cooldown")
	require.Equal(t, "rate_limit_exceeded", snap.LastFailureCode)
}

// The success-path orchestration with an abnormally cut SSE stream: the
// handler returned nil but the provider verdict is failure (C17).
func TestReportAttemptHealthSuccessAbnormalStream(t *testing.T) {
	common.SetRouteCircuitBreakerEnabledForTest(true)
	t.Cleanup(func() { common.SetRouteCircuitBreakerEnabledForTest(false) })
	withBreakerDefaults(t, 1, 60, 1, 300)
	var now atomic.Int64
	now.Store(1000)
	registry := newTestRegistry(&now)
	oldBreaker := Breaker
	Breaker = registry
	t.Cleanup(func() { Breaker = oldBreaker })

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	state := stateFixture(1)
	state.MarkAttempt(c, 0)

	health := ComputeRequestSuccessHealth(c, state, nil)
	require.Equal(t, BreakerEffectSuccess, health.Effect, "no stream snapshot (non-stream) → plain success")
	require.Equal(t, HealthSuccess, health.Verdict)

	// Now with a stream status cut mid-stream.
	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	c2.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	status := relaycommon.NewStreamStatus()
	status.SetEndReason(relaycommon.StreamEndReasonScannerErr, nil)
	info := &relaycommon.RelayInfo{StreamStatus: status}
	health = ComputeRequestSuccessHealth(c2, state, info)
	require.Equal(t, BreakerEffectFailure, health.Effect)
	snap := registry.CandidateSnapshot(1, 1, nil)
	require.Equal(t, CircuitStateOpen, snap.State, "single abnormal stream end trips a threshold-1 circuit")
	require.Equal(t, string(HealthFailure), snap.LastHealthOutcome)
}
