package routechain

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
)

func policyProps(remaining int) RequestProperties {
	return RequestProperties{RemainingCandidates: remaining}
}

func TestCanFailoverBlockedOutcomes(t *testing.T) {
	blockedOutcomes := []Outcome{
		OutcomeSuccess,
		OutcomeToolOutputObserved,
		OutcomeClientOutputObserved,
		OutcomeUsageObserved,
		OutcomeUpstreamWorkObserved,
		OutcomeRequestInvalid,
		OutcomeRoutePolicyRejected,
		OutcomeRequestWrittenOutcomeUncertain,
	}
	for _, outcome := range blockedOutcomes {
		decision := CanFailover(policyProps(3), &model.RouteCandidatePolicy{}, Evidence{HTTPStatus: 500}, outcome)
		if decision.Allowed || !decision.Terminal {
			t.Errorf("%s: expected blocked terminal decision, got %+v", outcome, decision)
		}
	}
}

func TestCanFailoverAdvanceOutcomes(t *testing.T) {
	advanceable := []struct {
		outcome Outcome
		status  int
	}{
		{OutcomePreDispatchUnavailable, 500},
		{OutcomePoolNoEligibleAccount, 503},
		{OutcomeAuthoritativeRejectionNoWork, 401},
		{OutcomeAuthoritativeRejectionNoWork, 403},
		{OutcomeAuthoritativeRejectionNoWork, 404},
	}
	for _, tc := range advanceable {
		decision := CanFailover(policyProps(2), &model.RouteCandidatePolicy{}, Evidence{HTTPStatus: tc.status}, tc.outcome)
		if !decision.Allowed || decision.Terminal {
			t.Errorf("%s(%d): expected advance, got %+v", tc.outcome, tc.status, decision)
		}
	}
}

func TestCanFailoverExhaustion(t *testing.T) {
	for _, outcome := range []Outcome{OutcomePreDispatchUnavailable, OutcomePoolNoEligibleAccount, OutcomeAuthoritativeRejectionNoWork, OutcomeRateLimitRejectionNoWork} {
		decision := CanFailover(policyProps(0), &model.RouteCandidatePolicy{}, Evidence{}, outcome)
		if decision.Allowed || decision.Reason != "route_exhausted" {
			t.Errorf("%s: expected route_exhausted, got %+v", outcome, decision)
		}
	}
}

func TestCanFailover429PolicyGate(t *testing.T) {
	evidence := Evidence{HTTPStatus: 429}
	// Default policy: 429 never advances (R13).
	decision := CanFailover(policyProps(2), &model.RouteCandidatePolicy{}, evidence, OutcomeRateLimitRejectionNoWork)
	if decision.Allowed || decision.Reason != "failover_on_429_disabled" {
		t.Fatalf("429 must default to no failover, got %+v", decision)
	}
	// Explicit policy enable plus zero-work classification: advance (R14).
	enabled := &model.RouteCandidatePolicy{Failure: &model.RouteFailurePolicy{FailoverOn429: boolPtr(true)}}
	decision = CanFailover(policyProps(2), enabled, evidence, OutcomeRateLimitRejectionNoWork)
	if !decision.Allowed {
		t.Fatalf("429 with policy on must advance, got %+v", decision)
	}
}

func TestCanFailover404PolicyGate(t *testing.T) {
	evidence := Evidence{HTTPStatus: 404}
	// Default policy allows 404 model-reject failover (R12).
	decision := CanFailover(policyProps(2), &model.RouteCandidatePolicy{}, evidence, OutcomeAuthoritativeRejectionNoWork)
	if !decision.Allowed {
		t.Fatalf("404 with default policy must advance, got %+v", decision)
	}
	// Operator can disable 404 failover per candidate.
	disabled := &model.RouteCandidatePolicy{Failure: &model.RouteFailurePolicy{FailoverOn404: boolPtr(false)}}
	decision = CanFailover(policyProps(2), disabled, evidence, OutcomeAuthoritativeRejectionNoWork)
	if decision.Allowed || decision.Reason != "failover_on_404_disabled" {
		t.Fatalf("404 with disabled policy must block, got %+v", decision)
	}
	// 401 has no policy gate — explicit no-work rejection always advances (R9).
	decision = CanFailover(policyProps(2), disabled, Evidence{HTTPStatus: 401}, OutcomeAuthoritativeRejectionNoWork)
	if !decision.Allowed {
		t.Fatalf("401 must advance regardless of 404 policy, got %+v", decision)
	}
}

func TestCanFailoverDeadlineBlocksEverything(t *testing.T) {
	props := RequestProperties{RemainingCandidates: 3, DeadlineExceeded: true}
	for _, outcome := range []Outcome{OutcomePreDispatchUnavailable, OutcomePoolNoEligibleAccount, OutcomeRateLimitRejectionNoWork} {
		decision := CanFailover(props, &model.RouteCandidatePolicy{}, Evidence{}, outcome)
		if decision.Allowed || decision.Reason != "overall_deadline_exhausted" {
			t.Errorf("%s: deadline must block advance, got %+v", outcome, decision)
		}
	}
}

func boolPtr(v bool) *bool {
	return &v
}
