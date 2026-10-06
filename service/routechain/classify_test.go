package routechain

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

func TestClassifyMatrix(t *testing.T) {
	cases := []struct {
		name         string
		evidence     Evidence
		success      bool
		localInvalid bool
		want         Outcome
	}{
		{"success", Evidence{CandidateSelected: true, RequestWritten: true}, true, false, OutcomeSuccess},
		{"tool evidence dominates", Evidence{RequestWritten: true, ToolOutputObserved: true, HTTPStatus: 500}, false, false, OutcomeToolOutputObserved},
		{"client output locks route", Evidence{RequestWritten: true, ClientOutputObserved: true, HTTPStatus: 500}, false, false, OutcomeClientOutputObserved},
		{"usage observed", Evidence{RequestWritten: true, UsageObserved: true, HTTPStatus: 500}, false, false, OutcomeUsageObserved},
		{"work observed", Evidence{RequestWritten: true, WorkObserved: true, HTTPStatus: 500}, false, false, OutcomeUpstreamWorkObserved},
		{"local invalid pre-dispatch", Evidence{CandidateSelected: true, HTTPStatus: 413}, false, true, OutcomeRequestInvalid},
		{"pre-dispatch unavailability", Evidence{CandidateSelected: true, HTTPStatus: 500}, false, false, OutcomePreDispatchUnavailable},
		{"pool structured exhaustion", Evidence{CandidateSelected: true, RequestWritten: true, HTTPStatus: 503, UpstreamErrorCode: ExecutorCodeAuthUnavailable}, false, false, OutcomePoolNoEligibleAccount},
		{"pool auth_not_found", Evidence{CandidateSelected: true, RequestWritten: true, HTTPStatus: 503, UpstreamErrorCode: ExecutorCodeAuthNotFound}, false, false, OutcomePoolNoEligibleAccount},
		{"ordinary upstream 503 not pool exhaustion", Evidence{CandidateSelected: true, RequestWritten: true, HTTPStatus: 503, UpstreamErrorCode: "internal_server_error"}, false, false, OutcomeRequestWrittenOutcomeUncertain},
		{"503 without code not pool exhaustion", Evidence{CandidateSelected: true, RequestWritten: true, HTTPStatus: 503}, false, false, OutcomeRequestWrittenOutcomeUncertain},
		{"401 explicit no-work", Evidence{CandidateSelected: true, RequestWritten: true, HTTPStatus: 401}, false, false, OutcomeAuthoritativeRejectionNoWork},
		{"401 with work evidence", Evidence{CandidateSelected: true, RequestWritten: true, WorkObserved: true, HTTPStatus: 401}, false, false, OutcomeUpstreamWorkObserved},
		{"403 explicit no-work", Evidence{CandidateSelected: true, RequestWritten: true, HTTPStatus: 403}, false, false, OutcomeAuthoritativeRejectionNoWork},
		{"404 explicit no-work", Evidence{CandidateSelected: true, RequestWritten: true, HTTPStatus: 404}, false, false, OutcomeAuthoritativeRejectionNoWork},
		{"429 zero-work", Evidence{CandidateSelected: true, RequestWritten: true, HTTPStatus: 429}, false, false, OutcomeRateLimitRejectionNoWork},
		{"429 with usage", Evidence{CandidateSelected: true, RequestWritten: true, UsageObserved: true, HTTPStatus: 429}, false, false, OutcomeUsageObserved},
		{"500 after request written", Evidence{CandidateSelected: true, RequestWritten: true, HTTPStatus: 500}, false, false, OutcomeRequestWrittenOutcomeUncertain},
		{"400 after request written", Evidence{CandidateSelected: true, RequestWritten: true, HTTPStatus: 400}, false, false, OutcomeRequestWrittenOutcomeUncertain},
		{"empty evidence is uncertain once written", Evidence{CandidateSelected: true, RequestWritten: true}, false, false, OutcomeRequestWrittenOutcomeUncertain},
	}
	for _, tc := range cases {
		if got := Classify(tc.evidence, tc.success, tc.localInvalid); got != tc.want {
			t.Errorf("%s: Classify = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestExtractUpstreamErrorCode(t *testing.T) {
	require_empty := func(err *types.NewAPIError) {
		if got := ExtractUpstreamErrorCode(err); got != "" {
			t.Fatalf("expected empty code, got %q", got)
		}
	}
	require_empty(nil)
	require_empty(&types.NewAPIError{StatusCode: 503})
	require_empty(&types.NewAPIError{StatusCode: 503, RelayError: types.OpenAIError{Code: 12345}})

	err := &types.NewAPIError{StatusCode: 503, RelayError: types.OpenAIError{Code: "auth_unavailable"}}
	if got := ExtractUpstreamErrorCode(err); got != "auth_unavailable" {
		t.Fatalf("expected auth_unavailable, got %q", got)
	}
}

func TestExecutorPoolExhaustionCodesExactMatch(t *testing.T) {
	require.True(t, IsExecutorPoolExhaustionCode("auth_unavailable"))
	require.True(t, IsExecutorPoolExhaustionCode("auth_not_found"))
	// C4: no substring matching — near-misses must not classify.
	require.False(t, IsExecutorPoolExhaustionCode("xauth_unavailablex"))
	require.False(t, IsExecutorPoolExhaustionCode("auth_unavailable_x"))
	require.False(t, IsExecutorPoolExhaustionCode("no auth available"))
	require.False(t, IsExecutorPoolExhaustionCode(""))
}
