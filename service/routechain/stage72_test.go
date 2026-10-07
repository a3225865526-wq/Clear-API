package routechain

import "testing"

func TestStage72RefundIndependentOfFailover(t *testing.T) {
	e := Evidence{RequestWritten: true, HTTPStatus: 503, UpstreamErrorCode: ExecutorCodePoolNoEligibleAccount, ExecutorProvenance: true}
	o := Classify(e, false, false)
	if o != OutcomePoolNoEligibleAccount || CanFailover(RequestProperties{}, nil, e, o).Allowed || !ShouldRefundZeroWork(e, o) {
		t.Fatal("single candidate did not independently qualify for refund")
	}
	if !CanFailover(RequestProperties{RemainingCandidates: 1}, nil, e, o).Allowed {
		t.Fatal("multi candidate did not qualify for advance")
	}
	ordinary := Evidence{RequestWritten: true, HTTPStatus: 429}
	if ShouldRefundZeroWork(ordinary, Classify(ordinary, false, false)) {
		t.Fatal("ordinary rejection terminal billing changed")
	}
	for _, mutate := range []func(*Evidence){
		func(e *Evidence) { e.ExecutorProvenance = false },
		func(e *Evidence) { e.HTTPStatus = 500 },
		func(e *Evidence) { e.UpstreamErrorCode = "prefix_pool_no_eligible_account" },
		func(e *Evidence) { e.WorkObserved = true },
		func(e *Evidence) { e.UsageObserved = true },
		func(e *Evidence) { e.ClientOutputObserved = true },
		func(e *Evidence) { e.ToolOutputObserved = true },
	} {
		bad := e
		mutate(&bad)
		outcome := Classify(bad, false, false)
		if ShouldRefundZeroWork(bad, outcome) || CanFailover(RequestProperties{RemainingCandidates: 1}, nil, bad, outcome).Allowed {
			t.Fatalf("unsafe evidence refunded or replayed: %+v", bad)
		}
	}
}
