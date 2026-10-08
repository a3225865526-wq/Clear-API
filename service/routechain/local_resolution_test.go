package routechain

import (
	"github.com/QuantumNous/new-api/relaykit/types"
	"testing"
)

func TestLocalResolutionProofPolicy(t *testing.T) {
	base := Evidence{RequestWritten: true, DispatchStarted: true, HTTPStatus: 400, UpstreamErrorCode: "model_not_found", ExecutorProvenance: true, ExecutorZeroDispatch: true}
	if o := Classify(base, false, false); o != OutcomeExecutorLocalRejectedNoWork || !ShouldRefundZeroWork(base, o) || CanFailover(RequestProperties{RemainingCandidates: 2}, nil, base, o).Allowed {
		t.Fatal("local proof policy")
	}
	for _, mutate := range []func(*Evidence){func(e *Evidence) { e.ExecutorProvenance = false }, func(e *Evidence) { e.ExecutorZeroDispatch = false }, func(e *Evidence) { e.UpstreamErrorCode = "other" }, func(e *Evidence) { e.HTTPStatus = 500 }, func(e *Evidence) { e.WorkObserved = true }, func(e *Evidence) { e.UsageObserved = true }, func(e *Evidence) { e.ClientOutputObserved = true }, func(e *Evidence) { e.ToolOutputObserved = true }} {
		e := base
		mutate(&e)
		o := Classify(e, false, false)
		if ShouldRefundZeroWork(e, o) || CanFailover(RequestProperties{RemainingCandidates: 2}, nil, e, o).Allowed {
			t.Fatalf("contaminated proof accepted: %+v", e)
		}
	}
	for _, phase := range []string{"", "provider_execution", "model_resolution"} {
		for _, transport := range []string{"", "attempted", "unknown", "not_started"} {
			err := types.InitOpenAIError(types.ErrorCodeBadResponse, 400, types.WithExecutorErrorOrigin("local"), types.WithExecutorDispatchProof(phase, transport))
			if err.HasExecutorModelResolutionProof() != (phase == "model_resolution" && transport == "not_started") {
				t.Fatal("proof header value accepted")
			}
		}
	}
}
