package routechain

import (
	"context"
	"errors"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// HealthOutcome is the independent provider-health verdict for one upstream
// attempt (Stage 5 design §2.2, requirement §三: health ≠ billing). It is
// derived from transport result, stream completion status and evidence —
// never from user_billing_state, the consume log, or the attempt ledger's
// SUCCEEDED terminal. A stream the client legally paid for can still be a
// provider-health failure, and a client cancellation is never one.
type HealthOutcome string

const (
	HealthSuccess HealthOutcome = "health_success"
	HealthFailure HealthOutcome = "health_failure"
	HealthNeutral HealthOutcome = "health_neutral"
)

// HealthSignals carries the per-attempt facts ClassifyHealthOutcome reads.
// It is a pure input type: no billing state, no ledger state.
type HealthSignals struct {
	Outcome    Outcome                   // classified routing outcome (Stage 3)
	Evidence   Evidence                  // per-attempt evidence set
	Stream     relaycommon.StreamOutcome // zero value for non-stream attempts
	ContextErr error                     // c.Request.Context().Err() at decision time
}

// clientOrDeadline reports whether the attempt ended because the client went
// away or the overall route-chain deadline expired (§三十九, §四十三). Both
// are request-side causes that say nothing about provider health.
func (s HealthSignals) clientOrDeadline() bool {
	return errors.Is(s.ContextErr, context.Canceled) || errors.Is(s.ContextErr, context.DeadlineExceeded)
}

// streamHealth classifies the stream termination alone. Callers may still
// downgrade to neutral when the request context says the client or the
// overall deadline ended the attempt first.
func streamHealth(s relaycommon.StreamOutcome) HealthOutcome {
	switch s.EndReason {
	case relaycommon.StreamEndReasonClientGone, relaycommon.StreamEndReasonPingFail:
		// The client-side write path failed — request-driven.
		return HealthNeutral
	case relaycommon.StreamEndReasonTimeout, relaycommon.StreamEndReasonScannerErr, relaycommon.StreamEndReasonPanic:
		// The upstream stopped producing or stalled mid-stream (§四 B:
		// abnormal stream termination even though the client already got
		// output; NO REPLAY semantics and settled billing stay untouched).
		return HealthFailure
	case relaycommon.StreamEndReasonDone, relaycommon.StreamEndReasonEOF, relaycommon.StreamEndReasonHandlerStop:
		// Normal EOF / terminal event is HEALTH_SUCCESS (§四 A) — unless the
		// protocol layer marked the response failed or incomplete (a terminal
		// event the protocol requires never arrived).
		switch s.Response {
		case relaycommon.ResponseOutcomeFailed, relaycommon.ResponseOutcomeIncomplete:
			return HealthFailure
		}
		return HealthSuccess
	}
	// No end reason recorded: absence of contrary evidence on a nil-error
	// return — normal completion.
	return HealthSuccess
}

func streamHealthReason(verdict HealthOutcome) string {
	switch verdict {
	case HealthSuccess:
		return "stream_normal_end"
	case HealthFailure:
		return "stream_abnormal_end"
	default:
		return "stream_client_side_end"
	}
}

// ClassifyHealthOutcome maps one finished attempt onto the provider-health
// verdict. Deterministic, precedence-ordered, and conservative in the
// breaker's favor only where the evidence is provider-attributable — a
// client cancel or overall deadline never fuses a candidate (§三十九), and
// a 429 never increments failure counters (§十七: it feeds the cooldown
// effect instead, the provider answered, it is alive).
func ClassifyHealthOutcome(signals HealthSignals) (HealthOutcome, string) {
	if signals.Evidence.HTTPStatus == 429 {
		return HealthNeutral, "rate_limited_cooldown"
	}
	if signals.clientOrDeadline() {
		// Request-side termination dominates everything else (§三十九):
		// whatever the classified outcome says, the client going away or the
		// overall deadline expiring is not provider evidence.
		return HealthNeutral, "request_side_termination"
	}
	switch signals.Outcome {
	case OutcomeSuccess:
		verdict := streamHealth(signals.Stream)
		return verdict, streamHealthReason(verdict)
	case OutcomeRequestWrittenOutcomeUncertain:
		// Post-dispatch loss / written 5xx / unprovenanced 503 (§四十二):
		// counts as a health failure even though the current request cannot
		// fail over — that is exactly the breaker's value for later requests.
		return HealthFailure, "post_dispatch_uncertain"
	case OutcomePreDispatchUnavailable:
		// Attempt-level pre-dispatch is always a transport failure here —
		// config failures are excluded earlier as selection skips (§十三).
		return HealthFailure, "transport_failure"
	case OutcomeAuthoritativeRejectionNoWork:
		// 401/403 credential/config health failure (§十五) and 404
		// candidate-local model mapping failure (§十六). The breaker key is
		// candidate-level, so neither can disable the provider's other models;
		// legacy persistent auto-disable keeps its separate ownership.
		return HealthFailure, "authoritative_rejection"
	case OutcomePoolNoEligibleAccount:
		// Admission exhaustion, not provider health (§四十): a circuit OPEN
		// here would keep blocking the candidate after new accounts arrive.
		return HealthNeutral, "pool_admission_exhausted"
	case OutcomeUsageObserved, OutcomeUpstreamWorkObserved, OutcomeClientOutputObserved:
		// Post-dispatch attempt ended in error after real work — the
		// post-dispatch transport-loss signature lands in this bucket via
		// upstream_usage_uncertain (design Part 1 Q6).
		return HealthFailure, "post_dispatch_abnormal_end"
	case OutcomeToolOutputObserved:
		// tool_chain_no_retry is a request property, not provider evidence.
		return HealthNeutral, "tool_chain_no_retry"
	case OutcomeRequestInvalid, OutcomeRoutePolicyRejected:
		// User/local causes (§十四): never prove the provider unhealthy.
		return HealthNeutral, "request_local"
	}
	return HealthNeutral, "unknown_outcome"
}
