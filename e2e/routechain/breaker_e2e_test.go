package routechain

import (
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/routechain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Stage 5 circuit breaker network E2E (design §2.12, requirement §四十六):
// every scenario drives the real production stack — breaker triage happens
// inside BindCandidate, health reporting inside the relay loop, and the
// verdicts are judged from mock call counters, ledger rows and the in-process
// breaker registry. Not a pure state-machine test: an OPEN candidate must
// provably stop receiving upstream calls over the wire.

// breakerE2ESetup enables the breaker gate for one scenario and guarantees a
// pristine registry (memory-only global; each test is its own "process life").
func breakerE2ESetup(t *testing.T) {
	t.Helper()
	common.SetRouteCircuitBreakerEnabledForTest(true)
	routechain.Breaker.ResetForTest()
	t.Cleanup(func() {
		common.SetRouteCircuitBreakerEnabledForTest(false)
		routechain.Breaker.ResetForTest()
	})
}

// B1 (C1+C2): three consecutive provider 5xx open the circuit; the next
// request skips A at selection level — no upstream call, no attempt row, a
// circuit_open decision record — and B serves the request.
func TestE2EBreakerOpensAfterConsecutiveFailures(t *testing.T) {
	breakerE2ESetup(t)
	h := newHarness(t)
	mockA := newMockUpstream(t,
		mockBehavior{status: 503, body: errorBody(503, "internal_server_error")},
		mockBehavior{status: 503, body: errorBody(503, "internal_server_error")},
		mockBehavior{status: 503, body: errorBody(503, "internal_server_error")},
	)
	mockB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
	h.addChannel(21, mockA.srv.URL, "")
	h.addChannel(22, mockB.srv.URL, "")
	h.setChain(t, false, []model.RouteChainCandidate{
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
	})

	// Requests 1-3: A fails with an ordinary 503 — the request cannot fail
	// over (§六), and each failure counts on the breaker.
	for i := 0; i < 3; i++ {
		status, _, _ := h.postChat(t, e2eModel, "", false)
		require.NotEqual(t, http.StatusOK, status, "request %d must fail", i+1)
	}
	require.Equal(t, 3, mockA.count())
	require.Equal(t, 0, mockB.count())
	snapshot := routechain.Breaker.CandidateSnapshot(1, 1, nil)
	require.Equal(t, routechain.CircuitStateOpen, snapshot.State)
	require.Equal(t, int64(3), snapshot.Counters["health_failure_total"])

	// Request 4: A is skipped without dispatch; B serves.
	status, body, _ := h.postChat(t, e2eModel, "", false)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, "from-b")
	require.Equal(t, 3, mockA.count(), "OPEN candidate must not be called again")
	require.Equal(t, 1, mockB.count())

	// Ledger: 3 failed A attempts + 1 B attempt; the skip is a decision, never
	// an attempt row (§二十七).
	attempts := h.ledgerAttempts(t)
	require.Len(t, attempts, 4)
	require.Equal(t, 21, attempts[0].ChannelId)
	require.Equal(t, 22, attempts[3].ChannelId)
	decisions := h.ledgerDecisions(t)
	openSkips := 0
	for _, decision := range decisions {
		if decision.Reason == routechain.SkipReasonCircuitOpen {
			openSkips++
			require.Equal(t, "selection_skip", decision.Phase)
			require.Equal(t, "skipped", decision.Decision)
		}
	}
	require.Equal(t, 1, openSkips, "exactly one circuit_open decision record")
}

// B2 (C3+C4): cooldown expiry half-opens the circuit, one probe succeeds and
// closes it; the following request reaches A again over the wire.
func TestE2EBreakerHalfOpenProbeSuccessCloses(t *testing.T) {
	breakerE2ESetup(t)
	h := newHarness(t)
	mockA := newMockUpstream(t,
		mockBehavior{status: 503, body: errorBody(503, "internal_server_error")},
		mockBehavior{body: chatOKBody("from-a")},
	)
	mockB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
	h.addChannel(21, mockA.srv.URL, "")
	h.addChannel(22, mockB.srv.URL, "")
	h.setChain(t, false, []model.RouteChainCandidate{
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21, Policy: `{"health_policy":{"failure_threshold":1,"cooldown_seconds":1}}`},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
	})

	// Trip: one failure → OPEN.
	status, _, _ := h.postChat(t, e2eModel, "", false)
	require.NotEqual(t, http.StatusOK, status)
	require.Equal(t, 1, mockA.count())

	// Still OPEN: A is skipped, B serves.
	status, body, _ := h.postChat(t, e2eModel, "", false)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, "from-b")
	require.Equal(t, 1, mockA.count())

	// Cooldown elapses → HALF_OPEN; the next request is the single probe and
	// A now answers normally → CLOSED.
	time.Sleep(1100 * time.Millisecond)
	status, body, _ = h.postChat(t, e2eModel, "", false)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, "from-a", "the probe itself must be the serving attempt")
	require.Equal(t, 2, mockA.count())
	// The health report runs inside the handler after the response bytes are
	// already readable — poll for the close instead of racing it.
	require.Eventually(t, func() bool {
		return routechain.Breaker.CandidateSnapshot(1, 1, nil).State == routechain.CircuitStateClosed
	}, 3*time.Second, 10*time.Millisecond, "probe success must close the circuit")

	// Next request reaches A normally again.
	status, body, _ = h.postChat(t, e2eModel, "", false)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, "from-a")
	require.Equal(t, 3, mockA.count())
}

// B3 (C5): a failed HALF_OPEN probe reopens the circuit with a fresh cooldown.
func TestE2EBreakerHalfOpenProbeFailureReopens(t *testing.T) {
	breakerE2ESetup(t)
	h := newHarness(t)
	mockA := newMockUpstream(t,
		mockBehavior{status: 503, body: errorBody(503, "internal_server_error")},
		mockBehavior{status: 503, body: errorBody(503, "internal_server_error")},
	)
	mockB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
	h.addChannel(21, mockA.srv.URL, "")
	h.addChannel(22, mockB.srv.URL, "")
	h.setChain(t, false, []model.RouteChainCandidate{
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21, Policy: `{"health_policy":{"failure_threshold":1,"cooldown_seconds":1}}`},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
	})

	status, _, _ := h.postChat(t, e2eModel, "", false)
	require.NotEqual(t, http.StatusOK, status)
	time.Sleep(1100 * time.Millisecond)

	// The probe fails again → back OPEN, no failover for this request (§六).
	status, body, _ := h.postChat(t, e2eModel, "", false)
	require.NotEqual(t, http.StatusOK, status)
	require.NotContains(t, body, "from-b")
	require.Equal(t, 2, mockA.count())
	require.Eventually(t, func() bool {
		snapshot := routechain.Breaker.CandidateSnapshot(1, 1, nil)
		return snapshot.State == routechain.CircuitStateOpen && snapshot.Counters["circuit_open_total"] == 2
	}, 3*time.Second, 10*time.Millisecond, "half-open probe failure reopened the circuit")

	// The reopened cooldown holds: A stays skipped while B serves.
	status, body, _ = h.postChat(t, e2eModel, "", false)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, "from-b")
	require.Equal(t, 2, mockA.count(), "reopened circuit must not receive calls")
	require.Equal(t, 1, mockB.count())
}

// B4 (C19/C21/C22/C23): 429 cooldown — the current request still obeys
// CanFailover (policy on → advance), later requests skip A until the bounded
// Retry-After window elapses; a huge Retry-After is capped and recovery
// happens through the single HALF_OPEN probe.
func TestE2EBreaker429RetryAfterCooldown(t *testing.T) {
	breakerE2ESetup(t)
	oldMax := common.RouteChainRetryAfterMaxSeconds
	common.RouteChainRetryAfterMaxSeconds = 1
	t.Cleanup(func() { common.RouteChainRetryAfterMaxSeconds = oldMax })

	h := newHarness(t)
	mockA := newMockUpstream(t,
		mockBehavior{status: 429, body: errorBody(429, "rate_limit_exceeded"), retryAfter: "2"},
		mockBehavior{status: 429, body: errorBody(429, "rate_limit_exceeded"), retryAfter: "999999999"},
		mockBehavior{body: chatOKBody("from-a")},
	)
	mockB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
	h.addChannel(21, mockA.srv.URL, "")
	h.addChannel(22, mockB.srv.URL, "")
	h.setChain(t, false, []model.RouteChainCandidate{
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21, Policy: `{"failure_policy":{"failover_on_429":true}}`},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
	})

	// C19/C23: 429 with policy on — the current request advances to B exactly
	// as Stage 3 locked, and A lands in a bounded cooldown without any
	// failure count.
	status, body, _ := h.postChat(t, e2eModel, "", false)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, "from-b")
	require.Equal(t, 1, mockA.count())
	require.Eventually(t, func() bool {
		snapshot := routechain.Breaker.CandidateSnapshot(1, 1, nil)
		return snapshot.State == routechain.CircuitStateOpen &&
			snapshot.ConsecutiveFailures == 0 &&
			snapshot.Counters["retry_after_cooldown_total"] == 1 &&
			snapshot.Counters["circuit_open_total"] == 0
	}, 3*time.Second, 10*time.Millisecond, "429 opens the cooldown window without any failure count")

	// C22: policy-on advance for THIS request does not weaken the cooldown for
	// the NEXT request — A is skipped while the window holds.
	status, body, _ = h.postChat(t, e2eModel, "", false)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, "from-b")
	require.Equal(t, 1, mockA.count(), "cooled-down candidate must not be called")
	require.Equal(t, 2, mockB.count())

	// Cooldown (2s) elapses → probe; A answers 429 with Retry-After 999999999
	// → capped to RouteChainRetryAfterMaxSeconds=1 (C21). The probe request
	// still advances to B per policy.
	time.Sleep(2100 * time.Millisecond)
	before := time.Now().Unix()
	status, body, _ = h.postChat(t, e2eModel, "", false)
	after := time.Now().Unix()
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, "from-b")
	require.Equal(t, 2, mockA.count())
	require.Eventually(t, func() bool {
		snapshot := routechain.Breaker.CandidateSnapshot(1, 1, nil)
		return snapshot.State == routechain.CircuitStateOpen &&
			snapshot.OpenUntil >= before+1 &&
			snapshot.OpenUntil <= after+1 // Retry-After capped to max seconds
	}, 3*time.Second, 10*time.Millisecond, "capped cooldown window applied")

	// After the capped window the probe succeeds and closes the circuit.
	time.Sleep(1200 * time.Millisecond)
	status, body, _ = h.postChat(t, e2eModel, "", false)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, "from-a")
	require.Equal(t, 3, mockA.count())
	require.Eventually(t, func() bool {
		return routechain.Breaker.CandidateSnapshot(1, 1, nil).State == routechain.CircuitStateClosed
	}, 3*time.Second, 10*time.Millisecond, "capped-window probe success closed the circuit")
}

// B5 (C17): SSE partial output + abnormal upstream cut — the request
// "succeeds" for routing/billing (no replay), but the attempt ledger records
// health_failure with an abnormal stream completion and the breaker counts
// the failure, so the next request skips the candidate.
func TestE2EBreakerSSEPartialCutIsHealthFailure(t *testing.T) {
	breakerE2ESetup(t)
	h := newHarness(t)
	mockA := newMockUpstream(t, mockBehavior{
		sse:         []string{sseChunk("partial-a")},
		cutAfterSSE: true,
	})
	mockB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
	h.addChannel(21, mockA.srv.URL, "")
	h.addChannel(22, mockB.srv.URL, "")
	h.setChain(t, false, []model.RouteChainCandidate{
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21, Policy: `{"health_policy":{"failure_threshold":1,"cooldown_seconds":60}}`},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
	})

	// The cut stream: client sees the partial chunk, the attempt is terminal
	// SUCCEEDED (billing may settle partial usage) — yet health says failure.
	status, _, _ := h.postChat(t, e2eModel, "", true)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, 1, mockA.count())

	attempts := h.ledgerAttempts(t)
	require.Len(t, attempts, 1)
	assert.Equal(t, model.RouteChainTerminalSucceeded, attempts[0].TerminalState)
	assert.Equal(t, string(routechain.HealthFailure), attempts[0].HealthOutcome, "billing success is not provider health (§三)")
	assert.Equal(t, routechain.BreakerEffectFailure, attempts[0].BreakerEffect)
	assert.Equal(t, "abnormal", attempts[0].StreamCompletion)
	require.Eventually(t, func() bool {
		return routechain.Breaker.CandidateSnapshot(1, 1, nil).State == routechain.CircuitStateOpen
	}, 3*time.Second, 10*time.Millisecond, "threshold-1 circuit opened on the abnormal cut")

	// The next request skips the unhealthy candidate.
	status, body, _ := h.postChat(t, e2eModel, "", false)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, "from-b")
	require.Equal(t, 1, mockA.count(), "unhealthy candidate must not be called again")
}

// B6 (C16): a normal SSE stream end is health success — the circuit stays
// closed and the candidate keeps serving.
func TestE2EBreakerNormalSSEIsHealthSuccess(t *testing.T) {
	breakerE2ESetup(t)
	h := newHarness(t)
	mockA := newMockUpstream(t, mockBehavior{sse: []string{sseChunk("a"), sseChunk("b")}})
	h.addChannel(21, mockA.srv.URL, "")
	h.setChain(t, false, []model.RouteChainCandidate{
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21, Policy: `{"health_policy":{"failure_threshold":1,"cooldown_seconds":60}}`},
	})

	status, _, _ := h.postChat(t, e2eModel, "", true)
	require.Equal(t, http.StatusOK, status)
	attempts := h.ledgerAttempts(t)
	require.Len(t, attempts, 1)
	assert.Equal(t, model.RouteChainTerminalSucceeded, attempts[0].TerminalState)
	assert.Equal(t, string(routechain.HealthSuccess), attempts[0].HealthOutcome)
	assert.Equal(t, "normal", attempts[0].StreamCompletion)
	snapshot := routechain.Breaker.CandidateSnapshot(1, 1, nil)
	require.Equal(t, routechain.CircuitStateClosed, snapshot.State)
	require.Equal(t, int64(1), snapshot.Counters["health_success_total"])

	// Still serving.
	status, _, _ = h.postChat(t, e2eModel, "", true)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, 2, mockA.count())
}

// B7 (C29): with the breaker gate off, routing behavior is byte-identical to
// Stage 4 — the same 503 sequence never skips A.
func TestE2EBreakerGateOffIsInert(t *testing.T) {
	common.SetRouteCircuitBreakerEnabledForTest(false)
	t.Cleanup(func() { common.SetRouteCircuitBreakerEnabledForTest(false) })
	h := newHarness(t)
	mockA := newMockUpstream(t, mockBehavior{status: 503, body: errorBody(503, "internal_server_error")})
	mockB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
	h.addChannel(21, mockA.srv.URL, "")
	h.addChannel(22, mockB.srv.URL, "")
	h.setChain(t, false, []model.RouteChainCandidate{
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
	})

	for i := 0; i < 4; i++ {
		status, _, _ := h.postChat(t, e2eModel, "", false)
		require.NotEqual(t, http.StatusOK, status, "request %d must fail identically to Stage 4", i+1)
	}
	require.Equal(t, 4, mockA.count(), "without the breaker every request reaches A")
	require.Equal(t, 0, mockB.count())
}

// B8 (C11): pool admission exhaustion is health-neutral — the pool candidate
// falls to B and the breaker never counts it, so a later recovered pool (or
// new accounts) is not blocked by a phantom failure count.
func TestE2EBreakerPoolExhaustionIsNeutral(t *testing.T) {
	breakerE2ESetup(t)
	h := newHarness(t)
	h.setPoolPublication(t)
	executor := newMockUpstream(t, mockBehavior{poolExhausted: true})
	apiB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
	h.addChannel(21, executor.srv.URL, `{"upstream_pool":"e2e-pool","pool_routing_enabled":true}`)
	h.addChannel(22, apiB.srv.URL, "")
	h.setChain(t, true, []model.RouteChainCandidate{
		{CandidateType: model.RouteChainCandidateTypePool, TargetId: 21},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
	})

	status, body, _ := h.postChat(t, e2eModel, "", false)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, "from-b")
	require.Eventually(t, func() bool {
		snapshot := routechain.Breaker.CandidateSnapshot(1, 1, nil)
		return snapshot.State == routechain.CircuitStateClosed &&
			snapshot.ConsecutiveFailures == 0 &&
			snapshot.Counters["health_neutral_total"] == 1
	}, 3*time.Second, 10*time.Millisecond, "pool no-account must not trip the breaker (§四十)")
}
