package routechain

import (
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// R23: an admin config change (CAS reorder/delete) while a request is active
// must never affect that request's immutable snapshot.
func TestSnapshotImmutableUnderAdminEdit(t *testing.T) {
	common.SetRouteChainEnabledForTest(true)
	t.Cleanup(func() { common.SetRouteChainEnabledForTest(false) })
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "immutable.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.RouteChain{}, &model.RouteChainCandidate{}))
	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = oldDB })

	chain := &model.RouteChainWithCandidates{
		RouteChain: model.RouteChain{Model: "gpt-x", Group: "default", Enabled: true, Version: 1},
		Candidates: []model.RouteChainCandidate{
			{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21, Priority: 2, Enabled: true},
			{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22, Priority: 1, Enabled: true},
		},
	}
	require.NoError(t, model.UpsertRouteChain(chain, 0))

	c := stateTestContext()
	state := BeginRequest(c, "gpt-x", "default")
	require.NotNil(t, state)
	require.Len(t, state.Snapshot.Candidates, 2)
	originalFirstTarget := state.Snapshot.Candidates[0].TargetId
	originalVersion := state.Snapshot.Version

	// Admin reorders and disables a candidate mid-request.
	require.NoError(t, model.ReorderRouteChainCandidates(chain.Id, []int{chain.Candidates[1].Id, chain.Candidates[0].Id}, chain.Version))
	require.NoError(t, model.SetRouteChainCandidateEnabled(chain.Id, chain.Candidates[0].Id, false, chain.Version+1))

	require.Equal(t, originalFirstTarget, state.Snapshot.Candidates[0].TargetId, "active request keeps its original snapshot order")
	require.Equal(t, originalVersion, state.Snapshot.Version)
	require.Len(t, state.Snapshot.Candidates, 2)
	require.True(t, state.Snapshot.Candidates[0].Priority >= state.Snapshot.Candidates[1].Priority)

	// The NEXT request sees the new version.
	next := BeginRequest(stateTestContext(), "gpt-x", "default")
	require.NotNil(t, next)
	require.NotEqual(t, originalVersion, next.Snapshot.Version)
	require.Len(t, next.Snapshot.Candidates, 1, "disabled candidate excluded for new requests")
}
