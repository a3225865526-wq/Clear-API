package routechain

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// newTestRegistry returns an isolated registry driven by a caller-controlled
// clock (no sleeps, no global state pollution).
func newTestRegistry(now *atomic.Int64) *BreakerRegistry {
	return &BreakerRegistry{
		circuits: map[string]*circuit{},
		now:      func() int64 { return now.Load() },
	}
}

func withBreakerDefaults(t *testing.T, threshold, openSeconds, maxProbes, retryAfterMax int) {
	t.Helper()
	oldThreshold, oldOpen, oldProbes, oldRetry := common.RouteChainCircuitFailureThreshold,
		common.RouteChainCircuitOpenSeconds, common.RouteChainCircuitHalfOpenMaxProbes,
		common.RouteChainRetryAfterMaxSeconds
	common.RouteChainCircuitFailureThreshold = threshold
	common.RouteChainCircuitOpenSeconds = openSeconds
	common.RouteChainCircuitHalfOpenMaxProbes = maxProbes
	common.RouteChainRetryAfterMaxSeconds = retryAfterMax
	t.Cleanup(func() {
		common.RouteChainCircuitFailureThreshold = oldThreshold
		common.RouteChainCircuitOpenSeconds = oldOpen
		common.RouteChainCircuitHalfOpenMaxProbes = oldProbes
		common.RouteChainRetryAfterMaxSeconds = oldRetry
	})
}

// C1: threshold consecutive provider failures open the circuit.
func TestBreakerThresholdOpensCircuit(t *testing.T) {
	withBreakerDefaults(t, 3, 60, 1, 300)
	var now atomic.Int64
	now.Store(1000)
	r := newTestRegistry(&now)
	for i := 0; i < 2; i++ {
		effect := r.ReportAttemptHealth(nil, 1, 11, nil, HealthFailure, "post_dispatch_uncertain", "", 0, false)
		require.Equal(t, BreakerEffectFailure, effect)
	}
	snap := r.CandidateSnapshot(1, 11, nil)
	require.Equal(t, CircuitStateClosed, snap.State)
	require.Equal(t, 2, snap.ConsecutiveFailures)
	require.Equal(t, "DEGRADED", snap.HealthSummary())
	r.ReportAttemptHealth(nil, 1, 11, nil, HealthFailure, "post_dispatch_uncertain", "", 0, false)
	snap = r.CandidateSnapshot(1, 11, nil)
	require.Equal(t, CircuitStateOpen, snap.State)
	require.Equal(t, int64(1060), snap.OpenUntil)
	require.Equal(t, "CIRCUIT_OPEN", snap.HealthSummary())
	require.Equal(t, int64(1), snap.Counters["circuit_open_total"])
	// OPEN blocks dispatch as a selection-level skip.
	allowed, reason, probe := r.AllowDispatch(nil, 1, 11, nil)
	require.False(t, allowed)
	require.Equal(t, SkipReasonCircuitOpen, reason)
	require.False(t, probe)
}

// A health success in CLOSED resets the consecutive counter (§三十八);
// neutral outcomes neither increment nor reset it.
func TestBreakerSuccessResetsNeutralDoesNot(t *testing.T) {
	withBreakerDefaults(t, 3, 60, 1, 300)
	var now atomic.Int64
	now.Store(1000)
	r := newTestRegistry(&now)
	r.ReportAttemptHealth(nil, 1, 12, nil, HealthFailure, "transport_failure", "", 0, false)
	r.ReportAttemptHealth(nil, 1, 12, nil, HealthFailure, "transport_failure", "", 0, false)
	r.ReportAttemptHealth(nil, 1, 12, nil, HealthNeutral, "", "", 0, false)
	require.Equal(t, 2, r.CandidateSnapshot(1, 12, nil).ConsecutiveFailures)
	r.ReportAttemptHealth(nil, 1, 12, nil, HealthSuccess, "", "", 0, false)
	require.Equal(t, 0, r.CandidateSnapshot(1, 12, nil).ConsecutiveFailures)
	require.Equal(t, "HEALTHY", r.CandidateSnapshot(1, 12, nil).HealthSummary())
}

// C3/C4/C5: cooldown expiry lazily half-opens; the probe verdict resolves it.
func TestBreakerHalfOpenLifecycle(t *testing.T) {
	withBreakerDefaults(t, 2, 60, 1, 300)
	var now atomic.Int64
	now.Store(1000)
	r := newTestRegistry(&now)
	r.ReportAttemptHealth(nil, 1, 13, nil, HealthFailure, "post_dispatch_uncertain", "", 0, false)
	r.ReportAttemptHealth(nil, 1, 13, nil, HealthFailure, "post_dispatch_uncertain", "", 0, false)
	require.Equal(t, CircuitStateOpen, r.CandidateSnapshot(1, 13, nil).State)

	now.Store(1060) // cooldown elapsed
	allowed, reason, probeHeld := r.AllowDispatch(nil, 1, 13, nil)
	require.True(t, allowed)
	require.Equal(t, "", reason)
	require.True(t, probeHeld)
	snap := r.CandidateSnapshot(1, 13, nil)
	require.Equal(t, CircuitStateHalfOpen, snap.State)
	require.Equal(t, 1, snap.ActiveProbes)
	require.Equal(t, "HALF_OPEN", snap.HealthSummary())

	// Probe fails provider-attributably → back OPEN with a fresh cooldown.
	now.Store(1100)
	r.ReportAttemptHealth(nil, 1, 13, nil, HealthFailure, "post_dispatch_uncertain", "", 0, true)
	snap = r.CandidateSnapshot(1, 13, nil)
	require.Equal(t, CircuitStateOpen, snap.State)
	require.Equal(t, 0, snap.ActiveProbes)
	require.Equal(t, int64(1160), snap.OpenUntil)
	require.Equal(t, int64(2), snap.Counters["circuit_open_total"])

	// Second cooldown → HALF_OPEN → probe succeeds → CLOSED, counters cleared.
	now.Store(1200)
	allowed, _, probeHeld = r.AllowDispatch(nil, 1, 13, nil)
	require.True(t, allowed)
	require.True(t, probeHeld)
	r.ReportAttemptHealth(nil, 1, 13, nil, HealthSuccess, "", "", 0, true)
	snap = r.CandidateSnapshot(1, 13, nil)
	require.Equal(t, CircuitStateClosed, snap.State)
	require.Equal(t, 0, snap.ConsecutiveFailures)
	require.Equal(t, int64(1), snap.Counters["circuit_close_total"])
	require.Equal(t, "HEALTHY", snap.HealthSummary())
}

// C6/C33: after cooldown expires, only max-probes dispatches pass; everyone
// else is rejected as a selection-level skip.
func TestBreakerHalfOpenProbeLimit(t *testing.T) {
	withBreakerDefaults(t, 1, 60, 1, 300)
	var now atomic.Int64
	now.Store(1000)
	r := newTestRegistry(&now)
	r.ReportAttemptHealth(nil, 1, 14, nil, HealthFailure, "post_dispatch_uncertain", "", 0, false)
	now.Store(1060)

	allowed1, _, probe1 := r.AllowDispatch(nil, 1, 14, nil)
	require.True(t, allowed1)
	require.True(t, probe1)
	for i := 0; i < 49; i++ {
		allowed, reason, probe := r.AllowDispatch(nil, 1, 14, nil)
		require.False(t, allowed, "request %d must not dispatch", i)
		require.Equal(t, SkipReasonHalfOpenProbes, reason)
		require.False(t, probe)
	}
	snap := r.CandidateSnapshot(1, 14, nil)
	require.Equal(t, 1, snap.ActiveProbes)
	require.Equal(t, int64(49), snap.Counters["half_open_probe_rejected_total"])
	_ = r.ReportAttemptHealth(nil, 1, 14, nil, HealthSuccess, "", "", 0, probe1)
	require.Equal(t, CircuitStateClosed, r.CandidateSnapshot(1, 14, nil).State)
}

// A leaked probe (panic path — no report ever arrives) self-heals only after
// the full lease window, keeping the stampede bound intact meanwhile.
func TestBreakerProbeLeaseSelfHeal(t *testing.T) {
	withBreakerDefaults(t, 1, 60, 1, 300)
	var now atomic.Int64
	now.Store(1000)
	r := newTestRegistry(&now)
	r.ReportAttemptHealth(nil, 1, 15, nil, HealthFailure, "post_dispatch_uncertain", "", 0, false)
	now.Store(1060)
	allowed, _, probe := r.AllowDispatch(nil, 1, 15, nil)
	require.True(t, allowed)
	require.True(t, probe)
	// Before the lease expires the slot stays claimed.
	allowedNow, reason, _ := r.AllowDispatch(nil, 1, 15, nil)
	require.False(t, allowedNow)
	require.Equal(t, SkipReasonHalfOpenProbes, reason)
	// After the lease (max(2*60,60)s) the slot is reclaimed.
	now.Store(1060 + 120)
	allowedLater, _, probeLater := r.AllowDispatch(nil, 1, 15, nil)
	require.True(t, allowedLater)
	require.True(t, probeLater)
}

// C32: 100 concurrent failure reports produce exactly one OPEN transition
// with a consistent counter state — no torn reads, no double transition.
func TestBreakerConcurrentFailureRace(t *testing.T) {
	withBreakerDefaults(t, 3, 60, 1, 300)
	var now atomic.Int64
	now.Store(1000)
	r := newTestRegistry(&now)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.ReportAttemptHealth(nil, 1, 16, nil, HealthFailure, "post_dispatch_uncertain", "", 0, false)
		}()
	}
	wg.Wait()
	snap := r.CandidateSnapshot(1, 16, nil)
	require.Equal(t, CircuitStateOpen, snap.State)
	require.Equal(t, int64(1), snap.Counters["circuit_open_total"])
	require.Equal(t, int64(100), snap.Counters["health_failure_total"])
	// The counter freezes at the threshold once OPEN: further failures are
	// recorded as evidence, not as additional consecutive transitions.
	require.Equal(t, 3, snap.ConsecutiveFailures)
}

// Concurrent HALF_OPEN probe acquisition: with maxProbes=1 exactly one of the
// racing dispatches wins the slot.
func TestBreakerHalfOpenProbeRace(t *testing.T) {
	withBreakerDefaults(t, 1, 60, 1, 300)
	var now atomic.Int64
	now.Store(1000)
	r := newTestRegistry(&now)
	r.ReportAttemptHealth(nil, 1, 17, nil, HealthFailure, "post_dispatch_uncertain", "", 0, false)
	now.Store(1060)
	var allowedCount, probeCount int64
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			allowed, _, probe := r.AllowDispatch(nil, 1, 17, nil)
			if allowed {
				atomic.AddInt64(&allowedCount, 1)
			}
			if probe {
				atomic.AddInt64(&probeCount, 1)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int64(1), allowedCount)
	require.Equal(t, int64(1), probeCount)
}

// §十七: a 429-derived cooldown opens the window without any failure count.
func TestBreakerRetryAfterCooldownDoesNotCountFailures(t *testing.T) {
	withBreakerDefaults(t, 3, 60, 1, 300)
	var now atomic.Int64
	now.Store(1000)
	r := newTestRegistry(&now)
	effect := r.ReportAttemptHealth(nil, 1, 18, nil, HealthNeutral, "", "", 120, false)
	require.Equal(t, BreakerEffectCooldown, effect)
	snap := r.CandidateSnapshot(1, 18, nil)
	require.Equal(t, CircuitStateOpen, snap.State)
	require.Equal(t, 0, snap.ConsecutiveFailures)
	require.Equal(t, int64(0), snap.Counters["circuit_open_total"])
	require.Equal(t, int64(1), snap.Counters["retry_after_cooldown_total"])
	require.Equal(t, int64(1120), snap.OpenUntil)
}

// §三十一/§三十二: reset returns the probeable closed state; force-open is a
// transient override that expires.
func TestBreakerResetAndForceOpen(t *testing.T) {
	withBreakerDefaults(t, 1, 60, 1, 300)
	var now atomic.Int64
	now.Store(1000)
	r := newTestRegistry(&now)
	r.ReportAttemptHealth(nil, 1, 19, nil, HealthFailure, "post_dispatch_uncertain", "", 0, false)
	require.Equal(t, CircuitStateOpen, r.CandidateSnapshot(1, 19, nil).State)
	r.ResetCandidate(nil, 1, 19)
	snap := r.CandidateSnapshot(1, 19, nil)
	require.Equal(t, CircuitStateClosed, snap.State)
	require.Equal(t, 0, snap.ConsecutiveFailures)
	require.Equal(t, "UNKNOWN", snap.HealthSummary())
	require.Equal(t, int64(1), snap.Counters["circuit_open_total"], "audit counters survive reset")

	r.ForceOpen(nil, 1, 19, 30)
	snap = r.CandidateSnapshot(1, 19, nil)
	require.Equal(t, CircuitStateOpen, snap.State)
	require.True(t, snap.Forced)
	allowed, _, _ := r.AllowDispatch(nil, 1, 19, nil)
	require.False(t, allowed)
	now.Store(1030)
	allowed, _, probe := r.AllowDispatch(nil, 1, 19, nil)
	require.True(t, allowed)
	require.True(t, probe, "expired force-open lazily half-opens")
}

// §二十: candidate policy overrides the global defaults; global clamps hold.
func TestBreakerPolicyOverrideAndClamps(t *testing.T) {
	withBreakerDefaults(t, 3, 60, 1, 300)
	var now atomic.Int64
	now.Store(1000)
	r := newTestRegistry(&now)
	policy := &model.RouteCandidatePolicy{Health: &model.RouteHealthPolicy{FailureThreshold: 2, CooldownSeconds: 30}}
	r.ReportAttemptHealth(nil, 1, 20, policy, HealthFailure, "post_dispatch_uncertain", "", 0, false)
	r.ReportAttemptHealth(nil, 1, 20, policy, HealthFailure, "post_dispatch_uncertain", "", 0, false)
	snap := r.CandidateSnapshot(1, 20, policy)
	require.Equal(t, CircuitStateOpen, snap.State)
	require.Equal(t, int64(1030), snap.OpenUntil)
	require.Equal(t, 2, snap.FailureThreshold)
	require.Equal(t, 30, snap.CooldownSeconds)

	// An out-of-range policy value is clamped, never trusted (parse-time
	// validation already rejects it; the clamp is the second fence).
	extreme := &model.RouteCandidatePolicy{Health: &model.RouteHealthPolicy{FailureThreshold: 999, CooldownSeconds: 99999}}
	cfg := ResolveCircuitConfig(extreme)
	require.Equal(t, 20, cfg.FailureThreshold)
	require.Equal(t, 3600, cfg.CooldownSeconds)
}

// OPEN + a real health success closes the circuit (positive proof of health).
func TestBreakerSuccessDuringOpenCloses(t *testing.T) {
	withBreakerDefaults(t, 1, 60, 1, 300)
	var now atomic.Int64
	now.Store(1000)
	r := newTestRegistry(&now)
	r.ReportAttemptHealth(nil, 1, 21, nil, HealthFailure, "post_dispatch_uncertain", "", 0, false)
	effect := r.ReportAttemptHealth(nil, 1, 21, nil, HealthSuccess, "", "", 0, false)
	require.Equal(t, BreakerEffectSuccess, effect)
	snap := r.CandidateSnapshot(1, 21, nil)
	require.Equal(t, CircuitStateClosed, snap.State)
	require.Equal(t, int64(1), snap.Counters["circuit_close_total"])
}

func TestBreakerGlobalCounters(t *testing.T) {
	withBreakerDefaults(t, 1, 60, 1, 300)
	var now atomic.Int64
	now.Store(1000)
	r := newTestRegistry(&now)
	r.ReportAttemptHealth(nil, 1, 22, nil, HealthSuccess, "", "", 0, false)
	r.ReportAttemptHealth(nil, 1, 23, nil, HealthNeutral, "", "", 0, false)
	totals := r.GlobalCounters()
	require.Equal(t, int64(1), totals["health_success_total"])
	require.Equal(t, int64(1), totals["health_neutral_total"])
}

func TestBreakerRegistryIsolation(t *testing.T) {
	var now atomic.Int64
	now.Store(1000)
	r := newTestRegistry(&now)
	for i := 0; i < 5; i++ {
		r.ReportAttemptHealth(nil, 1, 24, nil, HealthFailure, "post_dispatch_uncertain", "", 0, false)
	}
	require.Equal(t, CircuitStateOpen, r.CandidateSnapshot(1, 24, nil).State)
	require.Equal(t, CircuitStateClosed, r.CandidateSnapshot(1, 25, nil).State, "unrelated candidate stays closed")
	require.Equal(t, CircuitStateClosed, r.CandidateSnapshot(2, 24, nil).State, "unrelated chain stays closed")
}
