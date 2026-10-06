package routechain

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// §四 (highest priority): chain A→B with a legacy channel C that the old
// weighted selector could pick. Nested legacy retry must not exist.
func TestE2ENestedLegacyRetryDoesNotExist(t *testing.T) {
	h := newHarness(t)
	mockA := newMockUpstream(t, mockBehavior{status: 401, body: errorBody(401, "invalid_api_key")})
	mockB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
	mockC := newMockUpstream(t, mockBehavior{body: chatOKBody("from-c")})
	h.addChannel(21, mockA.srv.URL, "")
	h.addChannel(22, mockB.srv.URL, "")
	h.addChannel(23, mockC.srv.URL, "") // legacy channel C: same group/model ability
	h.setChain(t, false, []model.RouteChainCandidate{
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
	})

	status, body, _ := h.postChat(t, e2eModel, "", false)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, "from-b")

	require.Equal(t, 1, mockA.count(), "candidate A called exactly once")
	require.Equal(t, 1, mockB.count(), "candidate B called exactly once")
	require.Equal(t, 0, mockC.count(), "legacy channel C must NEVER be called")
	require.Equal(t, int64(1), h.countReservations())
	require.Equal(t, int64(1), h.eventuallyConsumeLogs(1))
	// Stage 4 L26: the ledger records exactly the two chain attempts — C is
	// absent from the ledger as well.
	h.assertLedgerShape(t, model.RouteChainRequestFinalSuccess, model.RouteChainBillingSettled,
		[]string{model.RouteChainTerminalAuthoritativeRej, model.RouteChainTerminalSucceeded})
}

// §三: exact candidate binding — the configured candidate is the only
// dispatch target; no hidden third channel.
func TestE2EExactCandidateBinding(t *testing.T) {
	h := newHarness(t)
	mockA := newMockUpstream(t, mockBehavior{body: chatOKBody("from-a")})
	mockB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
	mockC := newMockUpstream(t, mockBehavior{body: chatOKBody("from-c")})
	h.addChannel(21, mockA.srv.URL, "")
	h.addChannel(22, mockB.srv.URL, "")
	h.addChannel(23, mockC.srv.URL, "")
	h.setChain(t, false, []model.RouteChainCandidate{
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
	})

	status, body, _ := h.postChat(t, e2eModel, "", false)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, "from-a")
	require.Equal(t, 1, mockA.count())
	require.Equal(t, 0, mockB.count())
	require.Equal(t, 0, mockC.count())

	// A safe failure advances to exactly B — total upstream attempts = 2.
	// Drain phase 1 first: its settlement goroutines must not race phase 2's
	// global initialization.
	h.Close()
	h2 := newHarness(t)
	phaseA := newMockUpstream(t, mockBehavior{status: 401, body: errorBody(401, "invalid_api_key")})
	phaseB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
	phaseC := newMockUpstream(t, mockBehavior{body: chatOKBody("from-c")})
	h2.addChannel(21, phaseA.srv.URL, "")
	h2.addChannel(22, phaseB.srv.URL, "")
	h2.addChannel(23, phaseC.srv.URL, "")
	h2.setChain(t, false, []model.RouteChainCandidate{
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
	})
	status, body, _ = h2.postChat(t, e2eModel, "", false)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, "from-b")
	require.Equal(t, 1, phaseA.count())
	require.Equal(t, 1, phaseB.count())
	require.Equal(t, 0, phaseC.count(), "no third channel may appear")
}

// §五: pool no-eligible (typed executor contract) → API candidate B.
func TestE2EPoolNoEligibleFallsToAPI(t *testing.T) {
	h := newHarness(t)
	h.setPoolPublication(t)
	executor := newMockUpstream(t, mockBehavior{poolExhausted: true})
	apiB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
	// Pool-bound channel: upstream_pool marker + pool_routing_enabled.
	h.addChannel(21, executor.srv.URL, `{"upstream_pool":"e2e-pool","pool_routing_enabled":true}`)
	h.addChannel(22, apiB.srv.URL, "")
	h.setChain(t, true, []model.RouteChainCandidate{
		{CandidateType: model.RouteChainCandidateTypePool, TargetId: 21},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
	})

	status, body, _ := h.postChat(t, e2eModel, "", false)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, "from-b")
	require.Equal(t, 1, executor.count(), "executor hit exactly once, no downstream dispatch")
	require.Equal(t, 1, apiB.count(), "API candidate B served the request")
	// Stage 4 L6: provenance-backed pool exhaustion recorded as an
	// authoritative pre-dispatch rejection with provable zero cost.
	h.assertLedgerShape(t, model.RouteChainRequestFinalSuccess, model.RouteChainBillingSettled,
		[]string{model.RouteChainTerminalAuthoritativeRej, model.RouteChainTerminalSucceeded})
	attempts := h.ledgerAttempts(t)
	assert.Equal(t, model.RouteChainCostZeroConfirmed, attempts[0].ProviderCostState)
	assert.Equal(t, 21, attempts[0].ChannelId)
	// The executor is itself an HTTP upstream: new-api writes the request to
	// it, but the provenance-backed contract proves no credential was ever
	// dispatched downstream — zero cost is provable.
	assert.True(t, attempts[0].RequestWritten)
	assert.True(t, attempts[0].AuthoritativeRejection)
}

// L7 (Stage 4 provenance contract): a provider echoing error.code
// auth_unavailable WITHOUT the executor provenance origin must NOT classify
// as pool exhaustion — the request stays put with today's uncertain
// post-dispatch semantics.
func TestE2EUnprovenedExhaustionCodeDoesNotFailover(t *testing.T) {
	h := newHarness(t)
	h.setPoolPublication(t)
	executor := newMockUpstream(t, mockBehavior{unprovened: true})
	apiB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
	h.addChannel(21, executor.srv.URL, `{"upstream_pool":"e2e-pool","pool_routing_enabled":true}`)
	h.addChannel(22, apiB.srv.URL, "")
	h.setChain(t, true, []model.RouteChainCandidate{
		{CandidateType: model.RouteChainCandidateTypePool, TargetId: 21},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
	})

	status, body, _ := h.postChat(t, e2eModel, "", false)
	require.NotEqual(t, http.StatusOK, status, "unprovened exhaustion code must not advance")
	require.NotContains(t, body, "from-b")
	require.Equal(t, 1, executor.count())
	require.Equal(t, 0, apiB.count(), "missing provenance ⇒ no pool-exhaustion classification ⇒ no advance") // Stage 4 L7: classified outcome-uncertain - the ledger shows it.
	h.assertLedgerShape(t, model.RouteChainRequestFinalFailed, model.RouteChainBillingSettledUncertain,
		[]string{model.RouteChainTerminalOutcomeUncertain})
	assert.Equal(t, model.RouteChainCostUnknownReview, h.ledgerAttempts(t)[0].ProviderCostState)
}

// §六: ordinary upstream 503 — request was written, outcome uncertain, NO failover.
func TestE2EOrdinary503DoesNotFailover(t *testing.T) {
	h := newHarness(t)
	mockA := newMockUpstream(t, mockBehavior{status: 503, body: errorBody(503, "internal_server_error")})
	mockB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
	h.addChannel(21, mockA.srv.URL, "")
	h.addChannel(22, mockB.srv.URL, "")
	h.setChain(t, false, []model.RouteChainCandidate{
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
	})

	status, body, _ := h.postChat(t, e2eModel, "", false)
	require.NotEqual(t, http.StatusOK, status, "client must see a failure")
	require.NotContains(t, body, "from-b")
	require.Equal(t, 1, mockA.count())
	require.Equal(t, 0, mockB.count(), "ordinary 503 must never fail over")
	// Only one candidate was consumed (the policy refused to advance), so the
	// final error keeps today's legacy passthrough semantics; the sanitized
	// route_exhausted error applies once multiple candidates were consumed
	// (see the exhaustion test).
}

// §七: 401/403/404 authoritative no-work rejections advance.
func TestE2EAuthoritativeRejectionsAdvance(t *testing.T) {
	cases := []struct {
		status int
		code   string
	}{
		{401, "invalid_api_key"},
		{403, "insufficient_quota"},
		{404, "model_not_found"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%d", tc.status), func(t *testing.T) {
			h := newHarness(t)
			mockA := newMockUpstream(t, mockBehavior{status: tc.status, body: errorBody(tc.status, tc.code)})
			mockB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
			h.addChannel(21, mockA.srv.URL, "")
			h.addChannel(22, mockB.srv.URL, "")
			h.setChain(t, false, []model.RouteChainCandidate{
				{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21},
				{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
			})

			status, body, _ := h.postChat(t, e2eModel, "", false)
			require.Equal(t, http.StatusOK, status)
			require.Contains(t, body, "from-b")
			require.Equal(t, 1, mockA.count())
			require.Equal(t, 1, mockB.count())
		})
	}
	// 404 with policy off stays put.
	t.Run("404-policy-off", func(t *testing.T) {
		h := newHarness(t)
		mockA := newMockUpstream(t, mockBehavior{status: 404, body: errorBody(404, "model_not_found")})
		mockB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
		h.addChannel(21, mockA.srv.URL, "")
		h.addChannel(22, mockB.srv.URL, "")
		h.setChain(t, false, []model.RouteChainCandidate{
			{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21, Policy: `{"failure_policy":{"failover_on_404":false}}`},
			{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
		})
		status, _, _ := h.postChat(t, e2eModel, "", false)
		require.Equal(t, http.StatusNotFound, status, "policy-off 404 keeps legacy passthrough (single consumed candidate)")
		require.Equal(t, 1, mockA.count())
		require.Equal(t, 0, mockB.count())
	})
	// Work-evidence variants are not constructible at this wire level: an
	// error status carries no usage envelope and nothing reaches the client,
	// so zero-work holds by protocol. That part of §七 is covered by the
	// classifier unit tests (upstream_work_observed dominates) — recorded as
	// a wire-contract limitation, not a PASS.
}

// §八: 429 policy matrix at the wire level.
func TestE2E429Policy(t *testing.T) {
	t.Run("policy-off-stops", func(t *testing.T) {
		h := newHarness(t)
		mockA := newMockUpstream(t, mockBehavior{status: 429, body: errorBody(429, "rate_limit_exceeded")})
		mockB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
		h.addChannel(21, mockA.srv.URL, "")
		h.addChannel(22, mockB.srv.URL, "")
		h.setChain(t, false, []model.RouteChainCandidate{
			{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21},
			{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
		})
		status, body, _ := h.postChat(t, e2eModel, "", false)
		require.NotEqual(t, http.StatusOK, status)
		require.NotContains(t, body, "from-b")
		require.Equal(t, 1, mockA.count())
		require.Equal(t, 0, mockB.count())
		// Stage 4 L11: 429 zero-work with policy off - an independent
		// authoritative-rejection attempt record, no second attempt.
		h.assertLedgerShape(t, model.RouteChainRequestFinalFailed, model.RouteChainBillingSettledUncertain,
			[]string{model.RouteChainTerminalAuthoritativeRej})
		assert.Equal(t, model.RouteChainCostZeroConfirmed, h.ledgerAttempts(t)[0].ProviderCostState)
	})
	t.Run("policy-on-zero-work-advances", func(t *testing.T) {
		h := newHarness(t)
		mockA := newMockUpstream(t, mockBehavior{status: 429, body: errorBody(429, "rate_limit_exceeded")})
		mockB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
		h.addChannel(21, mockA.srv.URL, "")
		h.addChannel(22, mockB.srv.URL, "")
		h.setChain(t, false, []model.RouteChainCandidate{
			{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21, Policy: `{"failure_policy":{"failover_on_429":true}}`},
			{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
		})
		status, body, _ := h.postChat(t, e2eModel, "", false)
		require.Equal(t, http.StatusOK, status)
		require.Contains(t, body, "from-b")
		require.Equal(t, 1, mockA.count())
		require.Equal(t, 1, mockB.count())
		// Stage 4 L12: zero-work 429 rejection recorded, then B success.
		h.assertLedgerShape(t, model.RouteChainRequestFinalSuccess, model.RouteChainBillingSettled,
			[]string{model.RouteChainTerminalAuthoritativeRej, model.RouteChainTerminalSucceeded})
	})
	// 429 + work evidence is not constructible at this wire level (an error
	// status carries no usage envelope); covered by classifier unit tests.
	// Recorded as limitation, not fabricated PASS.
}

// §九: connection failure before dispatch — nothing reaches candidate A.
func TestE2EPreDispatchConnectFailureAdvances(t *testing.T) {
	h := newHarness(t)
	deadAddr := unusedUpstreamAddr(t)
	mockB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
	h.addChannel(21, deadAddr, "")
	h.addChannel(22, mockB.srv.URL, "")
	h.setChain(t, false, []model.RouteChainCandidate{
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
	})

	status, body, _ := h.postChat(t, e2eModel, "", false)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, "from-b")
	require.Equal(t, 1, mockB.count())
	// Candidate A has no listener at all — the connect refusal is provably
	// pre-dispatch (no handler could ever count a request).
	// Stage 4 L3: the refused attempt is FAILED_SAFE_TO_ADVANCE with no
	// upstream contact (cost NONE); B succeeds.
	h.assertLedgerShape(t, model.RouteChainRequestFinalSuccess, model.RouteChainBillingSettled,
		[]string{model.RouteChainTerminalFailedSafeToAdvance, model.RouteChainTerminalSucceeded})
	attempts := h.ledgerAttempts(t)
	assert.False(t, attempts[0].RequestWritten, "nothing reached candidate A")
	assert.Equal(t, model.RouteChainCostNone, attempts[0].ProviderCostState)
}

// §十 (highest priority): post-dispatch disconnect — bytes written, no
// response, NO failover.
func TestE2EPostDispatchDisconnectDoesNotFailover(t *testing.T) {
	h := newHarness(t)
	mockA := newMockUpstream(t, mockBehavior{hijackClose: true})
	mockB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
	h.addChannel(21, mockA.srv.URL, "")
	h.addChannel(22, mockB.srv.URL, "")
	h.setChain(t, false, []model.RouteChainCandidate{
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
	})

	status, body, _ := h.postChat(t, e2eModel, "", false)
	require.NotEqual(t, http.StatusOK, status, "client must see a failure")
	require.NotContains(t, body, "from-b")
	require.Equal(t, 1, mockA.count(), "A received the full request")
	require.Equal(t, 0, mockB.count(), "missing response bytes never prove safety")
	// Stage 4 L4: one attempt, no advance. The Stage 3 classifier maps the
	// post-dispatch disconnect (usage-uncertain flag set) to the
	// usage_observed outcome — terminal FAILED_NO_ADVANCE, cost under review.
	h.assertLedgerShape(t, model.RouteChainRequestFinalFailed, model.RouteChainBillingSettledUncertain,
		[]string{model.RouteChainTerminalFailedNoAdvance})
	attempts := h.ledgerAttempts(t)
	assert.True(t, attempts[0].RequestWritten)
	assert.True(t, attempts[0].UsageObserved)
	assert.True(t, attempts[0].OutcomeUncertain == false && attempts[0].UsageState == model.RouteChainUsageUnavailable,
		"usage unavailable is an explicit state; the uncertain flag rides on the usage evidence")
	assert.Equal(t, model.RouteChainCostUnknownReview, attempts[0].ProviderCostState)
}

// §十一: SSE pre-output authoritative rejection → candidate B serves the stream.
func TestE2ESSEPreOutputRejectionAdvances(t *testing.T) {
	h := newHarness(t)
	mockA := newMockUpstream(t, mockBehavior{status: 401, body: errorBody(401, "invalid_api_key")})
	mockB := newMockUpstream(t, mockBehavior{sse: []string{sseChunk("hello "), sseChunk("from-b")}})
	h.addChannel(21, mockA.srv.URL, "")
	h.addChannel(22, mockB.srv.URL, "")
	h.setChain(t, false, []model.RouteChainCandidate{
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
	})

	status, body, header := h.postChat(t, e2eModel, "", true)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "text/event-stream", header.Get("Content-Type"))
	require.Contains(t, body, "from-b")
	require.NotContains(t, body, "invalid_api_key", "A's rejection body must not leak to the client")
	require.True(t, strings.HasSuffix(strings.TrimSpace(body), "data: [DONE]"))
	require.Equal(t, 1, mockA.count())
	require.Equal(t, 1, mockB.count())
	// Stage 4 L14: pre-output rejection advanced; the SSE success attempt is
	// recorded with the sse transport on the request row.
	h.assertLedgerShape(t, model.RouteChainRequestFinalSuccess, model.RouteChainBillingSettled,
		[]string{model.RouteChainTerminalAuthoritativeRej, model.RouteChainTerminalSucceeded})
	requests := h.ledgerRequests(t)
	assert.Equal(t, "sse", requests[0].Transport)
}

// §十二: SSE post-output failure — one chunk emitted then cut; NO failover.
func TestE2ESSEPostOutputDoesNotFailover(t *testing.T) {
	h := newHarness(t)
	mockA := newMockUpstream(t, mockBehavior{sse: []string{sseChunk("partial")}, cutAfterSSE: true})
	mockB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
	h.addChannel(21, mockA.srv.URL, "")
	h.addChannel(22, mockB.srv.URL, "")
	h.setChain(t, false, []model.RouteChainCandidate{
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
	})

	status, body, _ := h.postChat(t, e2eModel, "", true)
	// Client sees A's partial chunk and the stream terminates — B is never opened.
	require.NotContains(t, body, "from-b")
	require.Equal(t, 1, mockA.count())
	require.Equal(t, 0, mockB.count(), "output observed locks the route")
	_ = status
	// Stage 4 L15: no second attempt exists. The stream handler settles the
	// partial usage internally and returns without an error, so the request
	// completes on the engine's success path (route locked, B never
	// dispatched — the routing assertions above are the safety contract).
	h.assertLedgerShape(t, model.RouteChainRequestFinalSuccess, model.RouteChainBillingSettled,
		[]string{model.RouteChainTerminalSucceeded})
}

// §十三: SSE ping fires pre-dispatch → Written() locks the route
// (over-conservative, accepted limitation — must NOT be changed here).
func TestE2ESSEPingLocksRoutePreDispatch(t *testing.T) {
	generalSetting := operation_setting.GetGeneralSetting()
	oldPing, oldInterval := generalSetting.PingIntervalEnabled, generalSetting.PingIntervalSeconds
	generalSetting.PingIntervalEnabled = true
	generalSetting.PingIntervalSeconds = 1
	t.Cleanup(func() {
		generalSetting.PingIntervalEnabled = oldPing
		generalSetting.PingIntervalSeconds = oldInterval
	})
	h := newHarness(t)
	mockA := newMockUpstream(t, mockBehavior{status: 401, body: errorBody(401, "invalid_api_key"), delay: 1500 * time.Millisecond})
	mockB := newMockUpstream(t, mockBehavior{sse: []string{sseChunk("from-b")}})
	h.addChannel(21, mockA.srv.URL, "")
	h.addChannel(22, mockB.srv.URL, "")
	h.setChain(t, false, []model.RouteChainCandidate{
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
	})

	status, body, _ := h.postChat(t, e2eModel, "", true)
	// The ping writes before dispatch → Written() true → route locked →
	// the authoritative 401 cannot advance even though it is safe.
	require.Equal(t, 1, mockA.count())
	require.Equal(t, 0, mockB.count(), "pre-dispatch ping conservatively locks the route")
	require.NotContains(t, body, "from-b")
	_ = status
}

// §十四: tool-chain requests never cross candidates.
func TestE2EToolChainNeverAdvances(t *testing.T) {
	h := newHarness(t)
	mockA := newMockUpstream(t, mockBehavior{status: 401, body: errorBody(401, "invalid_api_key")})
	mockB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
	h.addChannel(21, mockA.srv.URL, "")
	h.addChannel(22, mockB.srv.URL, "")
	h.setChain(t, false, []model.RouteChainCandidate{
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
	})

	toolBody := `,"tools":[{"type":"function","function":{"name":"x"}}],"messages":[{"role":"tool","tool_call_id":"1","content":"prior result"}]`
	status, body, _ := h.postChat(t, e2eModel, toolBody, false)
	require.Equal(t, http.StatusUnauthorized, status)
	require.Contains(t, body, "invalid_api_key")
	require.Equal(t, 1, mockA.count())
	require.Equal(t, 0, mockB.count(), "tool-chain requests must not fail over")
	// Stage 4 L16: the attempt is terminal no-advance with tool evidence.
	h.assertLedgerShape(t, model.RouteChainRequestFinalFailed, model.RouteChainBillingSettledUncertain,
		[]string{model.RouteChainTerminalFailedNoAdvance})
	attempts := h.ledgerAttempts(t)
	assert.True(t, attempts[0].ToolOutputObserved)
}

// §十五: in-flight request keeps its immutable snapshot while config changes.
func TestE2EImmutableSnapshotUnderConfigChange(t *testing.T) {
	h := newHarness(t)
	mockA := newMockUpstream(t, mockBehavior{status: 401, body: errorBody(401, "invalid_api_key"), delay: 500 * time.Millisecond})
	mockB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
	mockC := newMockUpstream(t, mockBehavior{body: chatOKBody("from-c")})
	h.addChannel(21, mockA.srv.URL, "")
	h.addChannel(22, mockB.srv.URL, "")
	h.addChannel(23, mockC.srv.URL, "")
	chain := h.setChain(t, false, []model.RouteChainCandidate{
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
	})

	done := make(chan struct{})
	var status int
	var body string
	go func() {
		defer close(done)
		status, body, _ = h.postChat(t, e2eModel, "", false)
	}()
	time.Sleep(200 * time.Millisecond) // request in flight, A delaying

	// Admin rewrites the chain mid-request via CAS: C first, then A.
	require.NoError(h.t, model.UpsertRouteChain(&model.RouteChainWithCandidates{
		RouteChain: model.RouteChain{Id: chain.Id, Model: e2eModel, Group: "default", Enabled: true, Version: chain.Version},
		Candidates: []model.RouteChainCandidate{
			{Id: 0, CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 23, Priority: 2, Enabled: true},
			{Id: 0, CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21, Priority: 1, Enabled: true},
		},
	}, chain.Version))
	<-done

	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, "from-b", "in-flight request must keep its original snapshot (A→B)")
	require.Equal(t, 1, mockA.count())
	require.Equal(t, 1, mockB.count())
	require.Equal(t, 0, mockC.count(), "the reordered snapshot must not affect the active request")

	// The NEXT request reads the new configuration (C first).
	status, body, _ = h.postChat(t, e2eModel, "", false)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, "from-c")
	require.Equal(t, 1, mockC.count())
}

// §十六: attempt budget caps upstream attempts; selection skips do not count.
func TestE2EAttemptBudgetCapsAttempts(t *testing.T) {
	oldMax := common.RouteChainMaxCandidates
	common.RouteChainMaxCandidates = 3
	t.Cleanup(func() { common.RouteChainMaxCandidates = oldMax })

	h := newHarness(t)
	mocks := make([]*mockUpstream, 5)
	for i := range mocks {
		mocks[i] = newMockUpstream(t, mockBehavior{status: 401, body: errorBody(401, "invalid_api_key")})
	}
	// Candidate 1 is disabled (selection-level skip), 2-5 would each reject.
	h.addChannel(21, mocks[0].srv.URL, "")
	h.db.Model(&model.Channel{}).Where("id = ?", 21).Update("status", common.ChannelStatusManuallyDisabled)
	h.addChannel(22, mocks[1].srv.URL, "")
	h.addChannel(23, mocks[2].srv.URL, "")
	h.addChannel(24, mocks[3].srv.URL, "")
	h.addChannel(25, mocks[4].srv.URL, "")
	candidates := []model.RouteChainCandidate{
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 23},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 24},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 25},
	}
	h.setChain(t, false, candidates)

	status, body, _ := h.postChat(t, e2eModel, "", false)
	require.Equal(t, http.StatusServiceUnavailable, status)
	require.Contains(t, body, "route_exhausted")
	require.Equal(t, 0, mocks[0].count(), "disabled candidate skipped without dispatch")
	require.Equal(t, 1, mocks[1].count())
	require.Equal(t, 1, mocks[2].count())
	require.Equal(t, 1, mocks[3].count(), "attempts stop at the max budget")
	require.Equal(t, 0, mocks[4].count(), "no candidate beyond the budget is touched")
	totalAttempts := mocks[1].count() + mocks[2].count() + mocks[3].count() + mocks[4].count()
	require.Equal(t, common.RouteChainMaxCandidates, totalAttempts)
}

// §十七: real deadline timing — B inherits the remaining budget only.
func TestE2EDeadlineInheritanceRealTiming(t *testing.T) {
	oldDeadline := common.RouteChainOverallDeadlineMs
	common.RouteChainOverallDeadlineMs = 2000
	t.Cleanup(func() { common.RouteChainOverallDeadlineMs = oldDeadline })

	h := newHarness(t)
	mockA := newMockUpstream(t, mockBehavior{status: 401, body: errorBody(401, "invalid_api_key"), delay: 1500 * time.Millisecond})
	mockB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b"), delay: 30 * time.Second})
	h.addChannel(21, mockA.srv.URL, "")
	h.addChannel(22, mockB.srv.URL, "")
	h.setChain(t, false, []model.RouteChainCandidate{
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
	})

	started := time.Now()
	status, body, _ := h.postChat(t, e2eModel, "", false)
	elapsed := time.Since(started)

	require.Equal(t, 1, mockA.count())
	require.Equal(t, 1, mockB.count(), "B is dispatched with the remaining budget")
	require.Equal(t, http.StatusServiceUnavailable, status)
	require.Contains(t, body, "route_exhausted")
	require.Less(t, elapsed, 10*time.Second,
		"total wall-clock must reflect inherited remaining budget (~2s), not A delay + a fresh full B timeout (~31.5s)")
	require.Less(t, elapsed, 8*time.Second, "deadline must have killed B's wait")
}

// §十八: exactly one reservation / one settlement across a failover.
func TestE2ESingleReservationAcrossFailover(t *testing.T) {
	h := newHarness(t)
	quotaBefore := h.userQuota()
	mockA := newMockUpstream(t, mockBehavior{status: 401, body: errorBody(401, "invalid_api_key")})
	mockB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
	h.addChannel(21, mockA.srv.URL, "")
	h.addChannel(22, mockB.srv.URL, "")
	h.setChain(t, false, []model.RouteChainCandidate{
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
	})

	status, _, _ := h.postChat(t, e2eModel, "", false)
	require.Equal(t, http.StatusOK, status)

	require.Equal(t, int64(1), h.countReservations(), "exactly one billing reservation per request")
	require.Equal(t, int64(1), h.eventuallyConsumeLogs(1), "exactly one user settlement")
	quotaAfter := h.userQuota()
	require.Less(t, quotaAfter, quotaBefore, "user charged exactly once for the final settlement")
	require.Greater(t, quotaBefore-quotaAfter, 0)
	var reservation model.BillingReservation
	require.NoError(h.t, h.db.First(&reservation).Error)
	require.Equal(t, "settled", reservation.State)
}

// §十九: full exhaustion returns the sanitized deterministic error.
func TestE2EAllCandidatesExhaustedSanitizedError(t *testing.T) {
	h := newHarness(t)
	mockA := newMockUpstream(t, mockBehavior{status: 401, body: errorBody(401, "invalid_api_key")})
	mockB := newMockUpstream(t, mockBehavior{status: 403, body: errorBody(403, "insufficient_quota")})
	h.addChannel(21, mockA.srv.URL, "")
	h.addChannel(22, mockB.srv.URL, "")
	h.setChain(t, false, []model.RouteChainCandidate{
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
	})

	status, body, _ := h.postChat(t, e2eModel, "", false)
	require.Equal(t, http.StatusServiceUnavailable, status)
	require.Contains(t, body, "route_exhausted")
	require.NotContains(t, body, mockA.srv.URL)
	require.NotContains(t, body, mockB.srv.URL)
	require.NotContains(t, body, "insufficient_quota", "last candidate's raw error must not leak")
	require.Equal(t, 1, mockA.count())
	require.Equal(t, 1, mockB.count())
}

// §二十: legacy compatibility — no chain and gate-off both behave legacy.
func TestE2ELegacyCompatibility(t *testing.T) {
	t.Run("model-without-chain", func(t *testing.T) {
		h := newHarness(t)
		mockA := newMockUpstream(t, mockBehavior{status: 401, body: errorBody(401, "invalid_api_key")})
		mockB := newMockUpstream(t, mockBehavior{status: 401, body: errorBody(401, "invalid_api_key")})
		h.addChannel(21, mockA.srv.URL, "")
		h.addChannel(22, mockB.srv.URL, "")
		// gpt-legacy has abilities on both channels but NO chain: legacy
		// single-attempt behavior (RetryTimes=0), error passthrough. The
		// weighted pick between the two channels is random, so both reject.
		status, body, _ := h.postChat(t, "gpt-legacy", "", false)
		require.Equal(t, http.StatusUnauthorized, status)
		require.Contains(t, body, "invalid_api_key")
		total := mockA.count() + mockB.count()
		require.Equal(t, 1, total, "legacy RetryTimes=0 means exactly one upstream attempt")
		// Stage 4 L25: no chain means no ledger rows.
		require.Empty(t, h.ledgerRequests(t))
	})
	t.Run("gate-off-chain-inert", func(t *testing.T) {
		h := newHarness(t)
		common.SetRouteChainEnabledForTest(false)
		mockA := newMockUpstream(t, mockBehavior{status: 401, body: errorBody(401, "invalid_api_key")})
		mockB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
		// Only channel 21 can serve the model, so the inert (legacy) path is
		// deterministic: exactly one attempt against A, B untouched.
		h.addChannel(21, mockA.srv.URL, "")
		h.setChain(t, false, []model.RouteChainCandidate{
			{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21},
			{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
		})
		status, body, _ := h.postChat(t, e2eModel, "", false)
		require.Equal(t, http.StatusUnauthorized, status, "gate off: chain config must be fully inert")
		require.Contains(t, body, "invalid_api_key")
		require.Equal(t, 1, mockA.count())
		require.Equal(t, 0, mockB.count())
		// Stage 4 L25: the gate off means the ledger is fully inert too.
		require.Empty(t, h.ledgerRequests(t))
		require.Empty(t, h.ledgerAttempts(t))
		require.Empty(t, h.ledgerDecisions(t))
	})
}

// §二十一: cross-source gate at the network level.
func TestE2ECrossSourceGate(t *testing.T) {
	t.Run("pool-dispatch-then-failure-flag-true-still-stops", func(t *testing.T) {
		h := newHarness(t)
		h.setPoolPublication(t)
		// Executor accepts the request (dispatch happened) then dies without
		// a response — post-dispatch uncertainty, no failover even with the flag.
		executor := newMockUpstream(t, mockBehavior{hijackClose: true})
		apiB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
		h.addChannel(21, executor.srv.URL, `{"upstream_pool":"e2e-pool","pool_routing_enabled":true}`)
		h.addChannel(22, apiB.srv.URL, "")
		h.setChain(t, true, []model.RouteChainCandidate{
			{CandidateType: model.RouteChainCandidateTypePool, TargetId: 21},
			{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
		})
		status, body, _ := h.postChat(t, e2eModel, "", false)
		require.NotEqual(t, http.StatusOK, status)
		require.NotContains(t, body, "from-b")
		require.Equal(t, 1, executor.count())
		require.Equal(t, 0, apiB.count(), "pool post-dispatch failure never fails over")
	})
	t.Run("untyped-pool-503-not-exhaustion", func(t *testing.T) {
		h := newHarness(t)
		h.setPoolPublication(t)
		// Executor relays a REAL provider 503 (generic body) — must NOT be
		// classified as pool exhaustion even with the cross-source flag.
		executor := newMockUpstream(t, mockBehavior{status: 503, body: errorBody(503, "internal_server_error")})
		apiB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
		h.addChannel(21, executor.srv.URL, `{"upstream_pool":"e2e-pool","pool_routing_enabled":true}`)
		h.addChannel(22, apiB.srv.URL, "")
		h.setChain(t, true, []model.RouteChainCandidate{
			{CandidateType: model.RouteChainCandidateTypePool, TargetId: 21},
			{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
		})
		status, body, _ := h.postChat(t, e2eModel, "", false)
		require.NotEqual(t, http.StatusOK, status)
		require.NotContains(t, body, "from-b")
		require.Equal(t, 1, executor.count())
		require.Equal(t, 0, apiB.count(), "ordinary provider 503 through the executor is not pool exhaustion")
	})
}

// §二十二: pricing fail-closed — an unpriced model never dispatches anything.
func TestE2EPricingFailClosed(t *testing.T) {
	h := newHarness(t)
	mockA := newMockUpstream(t, mockBehavior{status: 401, body: errorBody(401, "invalid_api_key")})
	mockB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
	h.addChannel(21, mockA.srv.URL, "")
	h.addChannel(22, mockB.srv.URL, "")
	h.setChain(t, false, []model.RouteChainCandidate{
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
	})
	// Remove pricing for the chain model: fail closed before ANY dispatch.
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-legacy":1}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(fmt.Sprintf(`{%q:1,%q:1}`, e2eModel, "gpt-legacy")))
	})

	status, body, _ := h.postChat(t, e2eModel, "", false)
	require.NotEqual(t, http.StatusOK, status)
	require.NotContains(t, body, "from-b")
	require.Equal(t, 0, mockA.count(), "no dispatch may happen without pricing")
	require.Equal(t, 0, mockB.count())
	require.Equal(t, int64(0), h.countReservations(), "no reservation for a rejected request")
}

// Stage 4 (L1/L2/L27/L28/L29): the full ledger for a safe A→B failover —
// one request record, two attempts sharing one request id with unique
// attempt ids, distinct cost states, one failover decision.
func TestE2EAttemptLedgerRecordsTheChain(t *testing.T) {
	h := newHarness(t)
	mockA := newMockUpstream(t, mockBehavior{status: 401, body: errorBody(401, "invalid_api_key")})
	mockB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
	h.addChannel(21, mockA.srv.URL, "")
	h.addChannel(22, mockB.srv.URL, "")
	chain := h.setChain(t, false, []model.RouteChainCandidate{
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
	})

	status, body, header := h.postChat(t, e2eModel, "", false)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, "from-b")
	require.Equal(t, int64(1), h.countReservations(), "L27: one reservation across A→B")

	// The request record finalizes in a post-response defer — wait for it.
	require.Eventually(t, func() bool {
		rows := h.ledgerRequests(t)
		return len(rows) == 1 && rows[0].FinalStatus != ""
	}, 3*time.Second, 10*time.Millisecond)
	requests := h.ledgerRequests(t)
	require.Len(t, requests, 1)
	request := requests[0]
	assert.Equal(t, model.RouteChainRequestFinalSuccess, request.FinalStatus)
	assert.Equal(t, model.RouteChainBillingSettled, request.UserBillingState)
	assert.Equal(t, chain.Candidates[1].Id, request.FinalCandidateId, "final candidate is the succeeding B (candidate row id)")
	assert.Equal(t, "http", request.Transport)
	assert.Equal(t, header.Get(common.RequestIdKey), request.RequestId, "the ledger request id is the client-visible request id")

	attempts := h.ledgerAttempts(t)
	require.Len(t, attempts, 2)
	// Attempt 1: zero-work authoritative rejection.
	a := attempts[0]
	assert.Equal(t, 21, a.ChannelId)
	assert.Equal(t, model.RouteChainTerminalAuthoritativeRej, a.TerminalState)
	assert.True(t, a.RequestWritten)
	assert.True(t, a.AuthoritativeRejection)
	assert.Equal(t, 401, a.HttpStatus)
	assert.Equal(t, model.RouteChainCostZeroConfirmed, a.ProviderCostState, "zero-work rejection is provably zero cost")
	assert.Equal(t, model.RouteChainUsageUnavailable, a.UsageState, "no usage — explicit unavailable, never exact zero")
	// Attempt 2: success with authoritative usage.
	b := attempts[1]
	assert.Equal(t, 22, b.ChannelId)
	assert.Equal(t, model.RouteChainTerminalSucceeded, b.TerminalState)
	assert.Equal(t, a.RequestId, b.RequestId, "L28: one request id across attempts")
	assert.NotEqual(t, a.AttemptId, b.AttemptId, "L29: canonical attempt ids are unique")
	assert.Equal(t, request.FinalAttemptId, b.AttemptId)
	assert.Equal(t, model.RouteChainUsageObserved, b.UsageState)
	assert.Equal(t, int64(10), b.InputTokens, "mock usage prompt_tokens=10 recorded as-is")
	assert.Equal(t, int64(5), b.OutputTokens)
	assert.Equal(t, model.RouteChainCostUnknownReview, b.ProviderCostState, "usage known but no finance cost rule configured in the harness")

	decisions := h.ledgerDecisions(t)
	require.Len(t, decisions, 1)
	assert.Equal(t, "failover_decision", decisions[0].Phase)
	assert.Equal(t, "advance", decisions[0].Decision)
	assert.Equal(t, chain.Candidates[0].Id, decisions[0].CandidateId, "the decision names the failing candidate row")
	assert.Equal(t, model.RouteChainRequestFinalSuccess, requests[0].FinalStatus)
}

// Stage 4 L21: consume-log capture failing (disabled) must not affect the
// canonical ledger or billing state — settlement and ledger finalization are
// independent of consume-log delivery.
func TestE2EConsumeLogFailureLeavesCanonicalStateIntact(t *testing.T) {
	h := newHarness(t)
	oldLogConsume := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() { common.LogConsumeEnabled = oldLogConsume })
	mockA := newMockUpstream(t, mockBehavior{status: 401, body: errorBody(401, "invalid_api_key")})
	mockB := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
	h.addChannel(21, mockA.srv.URL, "")
	h.addChannel(22, mockB.srv.URL, "")
	h.setChain(t, false, []model.RouteChainCandidate{
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21},
		{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22},
	})

	status, body, _ := h.postChat(t, e2eModel, "", false)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, "from-b")
	// Settlement (intent + apply) and the consume log write can still be in
	// flight when the response bytes reach the client — poll, don't race.
	require.Eventually(t, func() bool {
		return h.countConsumeLogs() == 0 && h.reservationState() == "settled"
	}, 3*time.Second, 10*time.Millisecond, "canonical settlement must land without any consume log")
	require.Equal(t, int64(1), h.countReservations(), "the canonical reservation settled exactly once")
	// The ledger finalized independently of the consume log.
	h.assertLedgerShape(t, model.RouteChainRequestFinalSuccess, model.RouteChainBillingSettled,
		[]string{model.RouteChainTerminalAuthoritativeRej, model.RouteChainTerminalSucceeded})
}
