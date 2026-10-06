package routechain

import (
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func snapshotTestContext(t *testing.T) (*gin.Context, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "routechain.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.RouteChain{}, &model.RouteChainCandidate{}))
	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = oldDB })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	return c, db
}

func insertChain(t *testing.T, db *gorm.DB, chain *model.RouteChain, candidates []model.RouteChainCandidate) {
	t.Helper()
	require.NoError(t, db.Create(chain).Error)
	for i := range candidates {
		candidates[i].ChainId = chain.Id
		require.NoError(t, db.Create(&candidates[i]).Error)
	}
}

func TestResolveInertCases(t *testing.T) {
	c, db := snapshotTestContext(t)
	common.SetRouteChainEnabledForTest(false)
	require.Nil(t, Resolve(c, "gpt-x", "default"), "gate off must be inert (R2)")

	common.SetRouteChainEnabledForTest(true)
	t.Cleanup(func() { common.SetRouteChainEnabledForTest(false) })

	require.Nil(t, Resolve(c, "gpt-x", "default"), "no chain row must be inert (R1)")

	insertChain(t, db, &model.RouteChain{Model: "gpt-x", Group: "default", Enabled: false, Version: 1},
		[]model.RouteChainCandidate{{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 1, Priority: 1, Enabled: true}})
	require.Nil(t, Resolve(c, "gpt-x", "default"), "disabled chain must be inert")

	insertChain(t, db, &model.RouteChain{Model: "gpt-bad", Group: "default", Enabled: true, Version: 1},
		[]model.RouteChainCandidate{{CandidateType: "magic", TargetId: 1, Priority: 1, Enabled: true}})
	require.Nil(t, Resolve(c, "gpt-bad", "default"), "unknown candidate type must fail closed to inert")

	insertChain(t, db, &model.RouteChain{Model: "gpt-bad-policy", Group: "default", Enabled: true, Version: 1},
		[]model.RouteChainCandidate{{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 1, Priority: 1, Enabled: true, Policy: "{not-json"}})
	require.Nil(t, Resolve(c, "gpt-bad-policy", "default"), "invalid policy must fail closed to inert")

	insertChain(t, db, &model.RouteChain{Model: "gpt-mixed", Group: "default", Enabled: true, Version: 1},
		[]model.RouteChainCandidate{
			{CandidateType: model.RouteChainCandidateTypePool, TargetId: 1, Priority: 2, Enabled: true},
			{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 2, Priority: 1, Enabled: true},
		})
	require.Nil(t, Resolve(c, "gpt-mixed", "default"), "mixed sources without cross-source flag must be inert (R25)")
}

func TestResolveActiveSnapshot(t *testing.T) {
	c, db := snapshotTestContext(t)
	common.SetRouteChainEnabledForTest(true)
	t.Cleanup(func() { common.SetRouteChainEnabledForTest(false) })

	// Duplicate priorities from dirty data must still resolve deterministically
	// (stable secondary key id ASC, Amendment 2 C6 / R24).
	insertChain(t, db, &model.RouteChain{Model: "gpt-x", Group: "default", Enabled: true, Version: 7},
		[]model.RouteChainCandidate{
			{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 3, Priority: 5, Enabled: true},
			{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 2, Priority: 9, Enabled: true},
			{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 1, Priority: 5, Enabled: true},
			{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 4, Priority: 1, Enabled: false},
		})
	snapshot := Resolve(c, "gpt-x", "default")
	require.NotNil(t, snapshot)
	require.Equal(t, 7, snapshot.Version)
	require.Len(t, snapshot.Candidates, 3, "disabled candidate excluded")
	require.Equal(t, 2, snapshot.Candidates[0].TargetId)
	require.Equal(t, 3, snapshot.Candidates[1].TargetId, "priority tie broken by id ASC: target 3 holds the lower row id")
	require.Equal(t, 1, snapshot.Candidates[2].TargetId)

	// Cross-source chain is active only with the explicit flag (R26 snapshot rule).
	insertChain(t, db, &model.RouteChain{Model: "gpt-cross", Group: "default", Enabled: true, Version: 1, AllowCrossSource: true},
		[]model.RouteChainCandidate{
			{CandidateType: model.RouteChainCandidateTypePool, TargetId: 1, Priority: 2, Enabled: true},
			{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 2, Priority: 1, Enabled: true},
		})
	require.NotNil(t, Resolve(c, "gpt-cross", "default"))

	// Homogeneous pool chain on a pool-published model is active.
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	common.OptionMap[model.PoolRoutingOptionPrefix+"gpt-pool"] = `{"route":{"model":"gpt-pool","source":"pool","enabled":true,"version":1}}`
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		delete(common.OptionMap, model.PoolRoutingOptionPrefix+"gpt-pool")
		common.OptionMapRWMutex.Unlock()
	})
	insertChain(t, db, &model.RouteChain{Model: "gpt-pool", Group: "default", Enabled: true, Version: 1},
		[]model.RouteChainCandidate{{CandidateType: model.RouteChainCandidateTypePool, TargetId: 1, Priority: 1, Enabled: true}})
	require.NotNil(t, Resolve(c, "gpt-pool", "default"))

	// Pool candidate on an api-source model fails closed to inert.
	insertChain(t, db, &model.RouteChain{Model: "gpt-api-model", Group: "default", Enabled: true, Version: 1},
		[]model.RouteChainCandidate{{CandidateType: model.RouteChainCandidateTypePool, TargetId: 1, Priority: 1, Enabled: true}})
	require.Nil(t, Resolve(c, "gpt-api-model", "default"))

	// A chain enabled with zero enabled candidates stays ACTIVE so the
	// iterator exhausts deterministically instead of silently falling back.
	insertChain(t, db, &model.RouteChain{Model: "gpt-drained", Group: "default", Enabled: true, Version: 1},
		[]model.RouteChainCandidate{{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 1, Priority: 1, Enabled: false}})
	drained := Resolve(c, "gpt-drained", "default")
	require.NotNil(t, drained)
	require.Empty(t, drained.Candidates)
}
