package routechain

import (
	"github.com/QuantumNous/new-api/relaykit/types"
)

// Outcome classifies what an attempt (or selection phase) produced. The
// semantics are binding (Amendment 2); the string values are identifiers only.
type Outcome string

const (
	OutcomeSuccess                        Outcome = "success"
	OutcomeRequestInvalid                 Outcome = "request_invalid"
	OutcomePreDispatchUnavailable         Outcome = "pre_dispatch_unavailable"
	OutcomePoolNoEligibleAccount          Outcome = "pool_no_eligible_account"
	OutcomeAuthoritativeRejectionNoWork   Outcome = "authoritative_rejection_no_work"
	OutcomeRateLimitRejectionNoWork       Outcome = "rate_limit_rejection_no_work"
	OutcomeRequestWrittenOutcomeUncertain Outcome = "request_written_outcome_uncertain"
	OutcomeUpstreamWorkObserved           Outcome = "upstream_work_observed"
	OutcomeUsageObserved                  Outcome = "usage_observed"
	OutcomeClientOutputObserved           Outcome = "client_output_observed"
	OutcomeToolOutputObserved             Outcome = "tool_output_observed"
	OutcomeRoutePolicyRejected            Outcome = "route_policy_rejected"
)

// Evidence is the per-attempt observation set (Amendment 1 §2.4). Attempt
// existence is never work evidence — only these flags decide, and attempt_id
// presence proves neither work nor no-work (C5).
type Evidence struct {
	CandidateSelected    bool
	DispatchStarted      bool
	RequestWritten       bool // upstream bytes written (the WroteRequest fence point)
	WorkObserved         bool // upstream work signal for THIS attempt (e.g. codex-metering attempt delta)
	UsageObserved        bool // usage present or usage-uncertain
	ClientOutputObserved bool // first client byte written
	ToolOutputObserved   bool // tool-continuation semantics (request-level, set pre-loop)
	HTTPStatus           int
	UpstreamErrorCode    string // structured error.code from the upstream envelope (exact match only)
	ExecutorProvenance   bool   // error envelope carries the trusted executor provenance origin (Stage 4 contract)
}

// Executor pool-exhaustion codes come from the CLIProxyAPI wire contract
// (sdk/cliproxy/auth Error.Code, carried into the error body's error.code
// field). Exact match only — substring matching is forbidden (C4).
const (
	ExecutorCodeAuthUnavailable = "auth_unavailable"
	ExecutorCodeAuthNotFound    = "auth_not_found"
)

// IsExecutorPoolExhaustionCode reports whether a structured upstream error
// code identifies executor-side pool exhaustion (no eligible credential).
func IsExecutorPoolExhaustionCode(code string) bool {
	return code == ExecutorCodeAuthUnavailable || code == ExecutorCodeAuthNotFound
}

// ExecutorProvenanceOrigin is the trusted origin value the executor puts on
// envelopes it synthesizes itself (Stage 4 provenance contract). A real
// provider echoing an exhaustion code cannot hold it, and new-api must never
// classify pool exhaustion from the code without it.
const ExecutorProvenanceOrigin = "local_executor"

// authoritativeRejectionStatuses are the upstream statuses treated as an
// explicit, definitive rejection when the zero-work predicate holds
// (requirement §6/§7). 429 is separate policy-gated.
func authoritativeRejectionStatus(status int) bool {
	switch status {
	case 401, 403, 404, 429:
		return true
	}
	return false
}

func (e Evidence) zeroWork() bool {
	return !e.WorkObserved && !e.UsageObserved && !e.ClientOutputObserved && !e.ToolOutputObserved
}

// Classify is the single total classification function (Amendment 1): the
// outcome derives from the evidence set alone, and no decision site reads raw
// evidence ad hoc. success and localInvalid come from the caller: success =
// the handler returned without error; localInvalid = the error carries the
// skip-retry option from local validation. The check order encodes
// precedence — wherever evidence is ambiguous the classification lands on the
// no-failover side.
func Classify(evidence Evidence, success bool, localInvalid bool) Outcome {
	if success {
		return OutcomeSuccess
	}
	if evidence.ToolOutputObserved {
		return OutcomeToolOutputObserved
	}
	if evidence.ClientOutputObserved {
		return OutcomeClientOutputObserved
	}
	if evidence.UsageObserved {
		return OutcomeUsageObserved
	}
	if evidence.WorkObserved {
		return OutcomeUpstreamWorkObserved
	}
	if !evidence.RequestWritten {
		if localInvalid {
			return OutcomeRequestInvalid
		}
		return OutcomePreDispatchUnavailable
	}
	// Bytes reached an upstream. Only a structured, zero-work rejection with
	// trusted executor provenance may classify as safe; everything else stays
	// uncertain (C4, R8, Stage 4 provenance contract: a provider could echo
	// the exhaustion code, so the code alone is never sufficient).
	if evidence.HTTPStatus == 503 && evidence.ExecutorProvenance && IsExecutorPoolExhaustionCode(evidence.UpstreamErrorCode) {
		return OutcomePoolNoEligibleAccount
	}
	if evidence.zeroWork() && authoritativeRejectionStatus(evidence.HTTPStatus) {
		if evidence.HTTPStatus == 429 {
			return OutcomeRateLimitRejectionNoWork
		}
		return OutcomeAuthoritativeRejectionNoWork
	}
	return OutcomeRequestWrittenOutcomeUncertain
}

// ExtractUpstreamErrorCode pulls the structured error.code the upstream put
// into an OpenAI-style error envelope. Upstreams occasionally send numeric
// codes; only string codes are returned so consumers can exact-match (C4).
func ExtractUpstreamErrorCode(err *types.NewAPIError) string {
	if err == nil {
		return ""
	}
	openaiError, ok := err.RelayError.(types.OpenAIError)
	if !ok {
		return ""
	}
	code, _ := openaiError.Code.(string)
	return code
}

// HasExecutorProvenance reports whether the upstream error response carries
// the trusted executor provenance (Stage 7.1 contract). The trust root is the
// executor's reserved transport header (types.ExecutorErrorOriginHeader ==
// types.ExecutorErrorOriginLocal) which CLIProxyAPI stamps only on envelopes
// it synthesized itself and strips from every forwarded provider response.
// The body-level error.origin is advisory only — a provider can echo it inside
// a verbatim JSON body — so when the body carries an origin at all it must
// agree with the executor's marker, otherwise the envelope is spoof-shaped and
// fails closed.
func HasExecutorProvenance(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	if err.GetExecutorErrorOrigin() != types.ExecutorErrorOriginLocal {
		return false
	}
	if openaiError, ok := err.RelayError.(types.OpenAIError); ok {
		if openaiError.Origin != "" && openaiError.Origin != ExecutorProvenanceOrigin {
			return false
		}
	}
	return true
}
