package routechain

import (
	"github.com/QuantumNous/new-api/model"
)

// RequestProperties are the request-scoped inputs to the failover policy.
type RequestProperties struct {
	RemainingCandidates int
	DeadlineExceeded    bool
}

// Decision is the verdict of the centralized failover policy.
type Decision struct {
	Allowed  bool
	Reason   string
	Terminal bool
}

func blocked(reason string) Decision {
	return Decision{Allowed: false, Reason: reason, Terminal: true}
}

func exhausted() Decision {
	return Decision{Allowed: false, Reason: "route_exhausted", Terminal: true}
}

// CanFailover is THE centralized failover policy (Amendment 2): every
// chain-mode advance passes through it and no status-specific branching lives
// in controller code. Inputs follow the agreed shape — route policy (the
// failing candidate's), request properties, the attempt evidence and the
// classified outcome.
//
// Per-class rules (Amendment 2 / requirement §6-§10):
//   - success, work/usage/client-output/tool evidence ⇒ never advance
//   - request-invalid and route-policy-rejected ⇒ never advance
//   - post-dispatch uncertainty ⇒ never advance (PG-06)
//   - pre-dispatch unavailability ⇒ advance (matrix row 1)
//   - typed pool exhaustion (executor contract guarantees no credential
//     dispatch; zero-work verified by the classifier) ⇒ advance (row 3)
//   - authoritative 401/403 rejection with zero work ⇒ advance (row 4);
//     404 additionally requires the failing candidate's failover_on_404
//   - 429 only with the failing candidate's failover_on_429 policy on (row 5)
//   - an exhausted overall deadline blocks every advance (row U/R22)
func CanFailover(props RequestProperties, policy *model.RouteCandidatePolicy, evidence Evidence, outcome Outcome) Decision {
	if props.DeadlineExceeded {
		return blocked("overall_deadline_exhausted")
	}
	switch outcome {
	case OutcomeSuccess:
		return blocked("success")
	case OutcomeToolOutputObserved:
		return blocked("tool_chain_not_idempotent")
	case OutcomeClientOutputObserved:
		return blocked("route_locked_after_client_output")
	case OutcomeUsageObserved:
		return blocked("usage_observed_no_replay")
	case OutcomeUpstreamWorkObserved:
		return blocked("upstream_work_observed_no_replay")
	case OutcomeRequestInvalid:
		return blocked("request_invalid")
	case OutcomeExecutorLocalRejectedNoWork:
		return blocked("executor_local_rejection_no_replay")
	case OutcomeRoutePolicyRejected:
		return blocked("route_policy_rejected")
	case OutcomeRequestWrittenOutcomeUncertain:
		return blocked("upstream_outcome_uncertain_no_replay")
	case OutcomePreDispatchUnavailable:
		if props.RemainingCandidates <= 0 {
			return exhausted()
		}
		return Decision{Allowed: true, Reason: "pre_dispatch_failure", Terminal: false}
	case OutcomePoolNoEligibleAccount:
		if props.RemainingCandidates <= 0 {
			return exhausted()
		}
		return Decision{Allowed: true, Reason: "pool_no_eligible_account", Terminal: false}
	case OutcomeAuthoritativeRejectionNoWork:
		if props.RemainingCandidates <= 0 {
			return exhausted()
		}
		if evidence.HTTPStatus == 404 && !policy.FailoverOn404() {
			return blocked("failover_on_404_disabled")
		}
		return Decision{Allowed: true, Reason: "authoritative_rejection_no_work", Terminal: false}
	case OutcomeRateLimitRejectionNoWork:
		if props.RemainingCandidates <= 0 {
			return exhausted()
		}
		if !policy.FailoverOn429() {
			return blocked("failover_on_429_disabled")
		}
		return Decision{Allowed: true, Reason: "rate_limit_rejection_no_work", Terminal: false}
	default:
		return blocked("unknown_outcome")
	}
}

// ShouldRefundZeroWork is independent of candidate budget and failover policy.
// Usage availability and refund eligibility are separate dimensions.
func ShouldRefundZeroWork(evidence Evidence, outcome Outcome) bool {
	if !evidence.zeroWork() {
		return false
	}
	switch outcome {
	case OutcomeExecutorLocalRejectedNoWork:
		return evidence.HTTPStatus == 400 && evidence.UpstreamErrorCode == "model_not_found" && evidence.ExecutorProvenance && evidence.ExecutorZeroDispatch
	case OutcomePoolNoEligibleAccount:
		return evidence.HTTPStatus == 503 && evidence.ExecutorProvenance && IsExecutorPoolExhaustionCode(evidence.UpstreamErrorCode)
	case OutcomePreDispatchUnavailable, OutcomeRequestInvalid:
		return !evidence.RequestWritten
	}
	return false
}
