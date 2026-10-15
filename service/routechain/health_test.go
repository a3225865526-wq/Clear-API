package routechain

import (
	"context"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestClassifyHealthOutcomeMatrix(t *testing.T) {
	normalStream := relaycommon.StreamOutcome{EndReason: relaycommon.StreamEndReasonEOF, Response: relaycommon.ResponseOutcomeCompleted}
	cutStream := relaycommon.StreamOutcome{EndReason: relaycommon.StreamEndReasonScannerErr, Response: relaycommon.ResponseOutcomeIncomplete}
	cutSSE := relaycommon.StreamOutcome{EndReason: relaycommon.StreamEndReasonScannerErr}
	clientGone := relaycommon.StreamOutcome{EndReason: relaycommon.StreamEndReasonClientGone}
	timedOut := relaycommon.StreamOutcome{EndReason: relaycommon.StreamEndReasonTimeout}
	protoFailed := relaycommon.StreamOutcome{EndReason: relaycommon.StreamEndReasonDone, Response: relaycommon.ResponseOutcomeFailed}
	pingFail := relaycommon.StreamOutcome{EndReason: relaycommon.StreamEndReasonPingFail}

	cases := []struct {
		name         string
		signals      HealthSignals
		wantVerdict  HealthOutcome
		wantContains string
	}{
		// §四 A: normal SSE EOF is health success.
		{"success non-stream", HealthSignals{Outcome: OutcomeSuccess}, HealthSuccess, "stream_normal_end"},
		{"success normal eof", HealthSignals{Outcome: OutcomeSuccess, Stream: normalStream}, HealthSuccess, "stream_normal_end"},
		// §四 B: client got output, upstream cut abnormally — health failure
		// even though the handler returned nil and billing may settle (C17).
		{"success sse cut", HealthSignals{Outcome: OutcomeSuccess, Stream: cutSSE, Evidence: Evidence{ClientOutputObserved: true}}, HealthFailure, "stream_abnormal_end"},
		{"success scanner err", HealthSignals{Outcome: OutcomeSuccess, Stream: cutStream}, HealthFailure, "stream_abnormal_end"},
		{"success protocol failed", HealthSignals{Outcome: OutcomeSuccess, Stream: protoFailed}, HealthFailure, "stream_abnormal_end"},
		// §三十九: client-side ends are neutral, never provider failures.
		{"success client gone", HealthSignals{Outcome: OutcomeSuccess, Stream: clientGone}, HealthNeutral, "stream_client_side_end"},
		{"success ping fail", HealthSignals{Outcome: OutcomeSuccess, Stream: pingFail}, HealthNeutral, "stream_client_side_end"},
		{"success context canceled", HealthSignals{Outcome: OutcomeSuccess, ContextErr: context.Canceled}, HealthNeutral, "request_side_termination"},
		// §四十三: overall deadline is not provider evidence; a scanner
		// timeout without a deadline is.
		{"success timeout with deadline", HealthSignals{Outcome: OutcomeSuccess, Stream: timedOut, ContextErr: context.DeadlineExceeded}, HealthNeutral, "request_side_termination"},
		{"uncertain canceled", HealthSignals{Outcome: OutcomeRequestWrittenOutcomeUncertain, Evidence: Evidence{RequestWritten: true}, ContextErr: context.Canceled}, HealthNeutral, "request_side_termination"},
		// §四十二: post-dispatch loss counts even without failover (C13/C14).
		{"uncertain post-dispatch", HealthSignals{Outcome: OutcomeRequestWrittenOutcomeUncertain, Evidence: Evidence{RequestWritten: true}}, HealthFailure, "post_dispatch_uncertain"},
		{"uncertain deadline only", HealthSignals{Outcome: OutcomeRequestWrittenOutcomeUncertain, ContextErr: context.DeadlineExceeded}, HealthNeutral, "request_side_termination"},
		// §十三: pre-dispatch transport failure (C15); abort is neutral.
		{"pre-dispatch transport", HealthSignals{Outcome: OutcomePreDispatchUnavailable}, HealthFailure, "transport_failure"},
		{"pre-dispatch aborted", HealthSignals{Outcome: OutcomePreDispatchUnavailable, ContextErr: context.DeadlineExceeded}, HealthNeutral, "request_side_termination"},
		// §十五/§十六: candidate-scoped credential/config/mapping failures.
		{"401 zero-work", HealthSignals{Outcome: OutcomeAuthoritativeRejectionNoWork, Evidence: Evidence{HTTPStatus: 401}}, HealthFailure, "authoritative_rejection"},
		{"403 zero-work", HealthSignals{Outcome: OutcomeAuthoritativeRejectionNoWork, Evidence: Evidence{HTTPStatus: 403}}, HealthFailure, "authoritative_rejection"},
		{"404 zero-work", HealthSignals{Outcome: OutcomeAuthoritativeRejectionNoWork, Evidence: Evidence{HTTPStatus: 404}}, HealthFailure, "authoritative_rejection"},
		// §四十: pool admission exhaustion is neutral (C11).
		{"pool exhausted", HealthSignals{Outcome: OutcomePoolNoEligibleAccount, Evidence: Evidence{HTTPStatus: 503, ExecutorProvenance: true, UpstreamErrorCode: "auth_unavailable"}}, HealthNeutral, "pool_admission_exhausted"},
		// Post-dispatch work/usage then error: provider-attributable unless
		// the client caused it.
		{"usage observed failure", HealthSignals{Outcome: OutcomeUsageObserved, Evidence: Evidence{RequestWritten: true, UsageObserved: true}}, HealthFailure, "post_dispatch_abnormal_end"},
		{"client cancel after work", HealthSignals{Outcome: OutcomeUpstreamWorkObserved, Evidence: Evidence{RequestWritten: true, WorkObserved: true}, ContextErr: context.Canceled}, HealthNeutral, "request_side_termination"},
		// §十四: user/local causes never count.
		{"request invalid", HealthSignals{Outcome: OutcomeRequestInvalid}, HealthNeutral, "request_local"},
		{"route policy rejected", HealthSignals{Outcome: OutcomeRoutePolicyRejected}, HealthNeutral, "request_local"},
		{"tool chain clean rejection", HealthSignals{Outcome: OutcomeToolOutputObserved, Evidence: Evidence{HTTPStatus: 400}}, HealthNeutral, "tool_chain_no_retry"},
		// C18: the provider evidence of the attempt still counts through the
		// no-retry flag — written 5xx, transport loss, pre-dispatch failure.
		{"tool chain written 5xx", HealthSignals{Outcome: OutcomeToolOutputObserved, Evidence: Evidence{RequestWritten: true, HTTPStatus: 503}}, HealthFailure, "post_dispatch_abnormal_end"},
		{"tool chain transport loss", HealthSignals{Outcome: OutcomeToolOutputObserved, Evidence: Evidence{RequestWritten: true}}, HealthFailure, "post_dispatch_abnormal_end"},
		{"tool chain pre-dispatch transport", HealthSignals{Outcome: OutcomeToolOutputObserved}, HealthFailure, "transport_failure"},
		{"tool chain 401", HealthSignals{Outcome: OutcomeToolOutputObserved, Evidence: Evidence{RequestWritten: true, HTTPStatus: 401}}, HealthFailure, "authoritative_rejection"},
		{"tool chain 429 cooldown", HealthSignals{Outcome: OutcomeToolOutputObserved, Evidence: Evidence{RequestWritten: true, HTTPStatus: 429}}, HealthNeutral, "rate_limited_cooldown"},
		// §十七: 429 is a cooldown effect, never a failure (C19/C20).
		{"429 zero-work", HealthSignals{Outcome: OutcomeRateLimitRejectionNoWork, Evidence: Evidence{HTTPStatus: 429}}, HealthNeutral, "rate_limited_cooldown"},
		{"429 dominates", HealthSignals{Outcome: OutcomeRequestWrittenOutcomeUncertain, Evidence: Evidence{HTTPStatus: 429, RequestWritten: true}}, HealthNeutral, "rate_limited_cooldown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			verdict, reason := ClassifyHealthOutcome(tc.signals)
			require.Equal(t, tc.wantVerdict, verdict)
			require.Equal(t, tc.wantContains, reason)
		})
	}
}

func TestClassifyHealthOutcomeUnknown(t *testing.T) {
	verdict, reason := ClassifyHealthOutcome(HealthSignals{Outcome: Outcome("bogus")})
	require.Equal(t, HealthNeutral, verdict)
	require.Equal(t, "unknown_outcome", reason)
}
