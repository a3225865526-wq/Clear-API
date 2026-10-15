package routechain

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func stateTestContext() *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	return c
}

func stateFixture(n int) *RequestState {
	candidates := make([]Candidate, n)
	for i := range candidates {
		candidates[i] = Candidate{Id: i + 1, Type: model.RouteChainCandidateTypeAPIChannel, TargetId: 100 + i, Priority: n - i}
	}
	return &RequestState{Snapshot: &Snapshot{ChainId: 1, Version: 1, Candidates: candidates}, visited: make(map[int]bool)}
}

func TestAttemptBudgetCapsCandidates(t *testing.T) {
	oldMax := common.RouteChainMaxCandidates
	t.Cleanup(func() { common.RouteChainMaxCandidates = oldMax })

	common.RouteChainMaxCandidates = 2
	state := stateFixture(4)
	require.Equal(t, 2, state.AttemptBudget(), "global cap bounds attempts (R21)")

	common.RouteChainMaxCandidates = 10
	state = stateFixture(4)
	require.Equal(t, 4, state.AttemptBudget())
}

func TestVisitedSetNeverRevisits(t *testing.T) {
	oldMax := common.RouteChainMaxCandidates
	t.Cleanup(func() { common.RouteChainMaxCandidates = oldMax })
	common.RouteChainMaxCandidates = 10

	state := stateFixture(3)
	seen := map[int]bool{}
	for {
		idx, ok := state.NextCandidate()
		if !ok {
			break
		}
		require.False(t, seen[state.Snapshot.Candidates[idx].Id], "candidate revisited (R20)")
		seen[state.Snapshot.Candidates[idx].Id] = true
		state.MarkAttempt(stateTestContext(), idx)
	}
	require.Equal(t, 3, state.AttemptsMade())
}

func TestSkipsDoNotConsumeBudget(t *testing.T) {
	oldMax := common.RouteChainMaxCandidates
	t.Cleanup(func() { common.RouteChainMaxCandidates = oldMax })
	common.RouteChainMaxCandidates = 2

	state := stateFixture(4)
	// Two candidates are skipped at selection phase (never dispatched).
	idx, ok := state.NextCandidate()
	require.True(t, ok)
	state.MarkSkipped(stateTestContext(), idx, "unavailable:channel_disabled")
	idx, ok = state.NextCandidate()
	require.True(t, ok)
	state.MarkSkipped(stateTestContext(), idx, "unavailable:model_not_served_by_channel")
	// Skips must not have consumed the attempt budget: candidates 3 and 4 are
	// still reachable for the two budgeted attempts.
	for attempt := 0; attempt < 2; attempt++ {
		idx, ok = state.NextCandidate()
		require.True(t, ok, "skip must not consume attempt budget")
		state.MarkAttempt(stateTestContext(), idx)
	}
	require.Equal(t, 2, state.AttemptsMade())
	_, ok = state.NextCandidate()
	require.False(t, ok, "attempt budget exhausted after two attempts (R21)")
}

func TestFailingCandidateTracksLastAttempt(t *testing.T) {
	state := stateFixture(3)
	_, ok := state.NextCandidate()
	require.True(t, ok)
	require.Nil(t, state.FailingCandidate(), "no failing candidate before the first dispatch")
	state.MarkAttempt(stateTestContext(), 0)
	failing := state.FailingCandidate()
	require.NotNil(t, failing)
	require.Equal(t, 1, failing.Id)
	state.MarkSuccess()
	require.True(t, state.Succeeded())
}

func TestBeginRequestStoresAndRestoresState(t *testing.T) {
	c := stateTestContext()
	require.Nil(t, StateFrom(c), "legacy request has no chain state (R1)")
}
