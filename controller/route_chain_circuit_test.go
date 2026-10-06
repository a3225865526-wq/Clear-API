package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/routechain"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Stage 5 Operator Control API tests: owner-gated circuit observation and
// control, CAS-enforced candidate enable/disable (C24–C27).

func newCircuitAPITestEnv(t *testing.T) (*gorm.DB, model.RouteChainWithCandidates) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	routechain.Breaker.ResetForTest()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "circuit-api.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, model.MigrateRouteChain(db))
	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = oldDB })

	chain := &model.RouteChainWithCandidates{
		RouteChain: model.RouteChain{Model: "gpt-chain", Group: "default", Enabled: true},
		Candidates: []model.RouteChainCandidate{
			{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 21, Priority: 2, Enabled: true},
			{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22, Priority: 1, Enabled: false},
		},
	}
	require.NoError(t, model.UpsertRouteChain(chain, 0))

	stored, err := model.GetRouteChainForModel("gpt-chain", "default")
	require.NoError(t, err)
	return db, *stored
}

func circuitHandlerParams(chainId, candidateId int) gin.Params {
	return gin.Params{
		{Key: "id", Value: strconv.Itoa(chainId)},
		{Key: "candidateId", Value: strconv.Itoa(candidateId)},
	}
}

// C27: without a root session the endpoints are unreachable — the router
// group's RootAuth rejects the request before any handler runs.
func TestRouteChainCircuitAPIRequiresRoot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	api := engine.Group("/api")
	api.Use(middleware.RootAuth())
	{
		api.GET("/route-chains/:id/health", GetRouteChainHealth)
		api.POST("/route-chains/:id/candidates/:candidateId/circuit/reset", ResetRouteChainCandidateCircuit)
		api.POST("/route-chains/:id/candidates/:candidateId/circuit/open", ForceOpenRouteChainCandidateCircuit)
		api.POST("/route-chains/:id/candidates/:candidateId/enabled", SetRouteChainCandidateEnabled)
	}
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/route-chains/1/health"},
		{http.MethodPost, "/api/route-chains/1/candidates/1/circuit/reset"},
		{http.MethodPost, "/api/route-chains/1/candidates/1/circuit/open"},
		{http.MethodPost, "/api/route-chains/1/candidates/1/enabled"},
	} {
		request := httptest.NewRequest(tc.method, tc.path, nil)
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusUnauthorized, recorder.Code, tc.path)
	}
}

// C24: reset returns the probeable closed state without touching the
// candidate's persistent enabled flag, and cross-chain ids are rejected.
func TestResetRouteChainCandidateCircuit(t *testing.T) {
	_, chain := newCircuitAPITestEnv(t)
	candidateId := chain.Candidates[0].Id

	routechain.Breaker.ForceOpen(nil, chain.Id, candidateId, 60)
	require.Equal(t, routechain.CircuitStateOpen, routechain.Breaker.CandidateSnapshot(chain.Id, candidateId, nil).State)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/reset", nil)
	c.Params = circuitHandlerParams(chain.Id, candidateId)
	ResetRouteChainCandidateCircuit(c)

	snapshot := routechain.Breaker.CandidateSnapshot(chain.Id, candidateId, nil)
	require.Equal(t, routechain.CircuitStateClosed, snapshot.State)
	require.Equal(t, "UNKNOWN", snapshot.HealthSummary())

	// Candidate enabled stays untouched (persistent config separate).
	updated, err := model.GetRouteChainForModel("gpt-chain", "default")
	require.NoError(t, err)
	require.True(t, updated.Candidates[0].Enabled)

	// A candidate id from outside this chain cannot be steered.
	recorder2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(recorder2)
	c2.Request = httptest.NewRequest(http.MethodPost, "/reset", nil)
	c2.Params = circuitHandlerParams(chain.Id, 999999)
	ResetRouteChainCandidateCircuit(c2)
	require.Contains(t, recorder2.Body.String(), "candidate not found in this chain")
}

// C25: force-open is runtime-only and reports its restart semantics.
func TestForceOpenRouteChainCandidateCircuit(t *testing.T) {
	_, chain := newCircuitAPITestEnv(t)
	candidateId := chain.Candidates[0].Id

	body, err := json.Marshal(forceOpenCircuitRequest{DurationSeconds: 30})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/open", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = circuitHandlerParams(chain.Id, candidateId)
	ForceOpenRouteChainCandidateCircuit(c)

	snapshot := routechain.Breaker.CandidateSnapshot(chain.Id, candidateId, nil)
	require.Equal(t, routechain.CircuitStateOpen, snapshot.State)
	require.True(t, snapshot.Forced)

	var response struct {
		Data struct {
			RestartSemantics string `json:"restart_semantics"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Contains(t, response.Data.RestartSemantics, "memory-only")
}

// C26: candidate enable/disable is persistent and CAS-enforced — a stale
// expected_version yields a conflict, and the winner's change is durable.
func TestSetRouteChainCandidateEnabledCAS(t *testing.T) {
	_, chain := newCircuitAPITestEnv(t)
	candidateId := chain.Candidates[0].Id
	staleVersion := chain.Version - 1

	call := func(expectedVersion int, enabled bool) *httptest.ResponseRecorder {
		body, err := json.Marshal(setCandidateEnabledRequest{Enabled: enabled, ExpectedVersion: expectedVersion})
		require.NoError(t, err)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/enabled", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Params = circuitHandlerParams(chain.Id, candidateId)
		SetRouteChainCandidateEnabled(c)
		return recorder
	}

	recorder := call(staleVersion, false)
	require.Equal(t, http.StatusConflict, recorder.Code)

	recorder = call(chain.Version, false)
	require.Equal(t, http.StatusOK, recorder.Code)
	updated, err := model.GetRouteChainForModel("gpt-chain", "default")
	require.NoError(t, err)
	require.False(t, updated.Candidates[0].Enabled)
	require.Equal(t, chain.Version+1, updated.Version)

	// A concurrent second writer with the same version conflicts.
	recorder = call(chain.Version, true)
	require.Equal(t, http.StatusConflict, recorder.Code)
}

// C26b: the health view derives DISABLED from persistent config while the
// breaker state stays independent.
func TestGetRouteChainHealthView(t *testing.T) {
	_, chain := newCircuitAPITestEnv(t)
	disabledCandidate := chain.Candidates[1]
	enabledCandidate := chain.Candidates[0]

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/health", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.Itoa(chain.Id)}}
	GetRouteChainHealth(c)
	require.Equal(t, http.StatusOK, recorder.Code)

	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Candidates []struct {
				Id            int    `json:"id"`
				Enabled       bool   `json:"enabled"`
				HealthSummary string `json:"health_summary"`
			} `json:"candidates"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Len(t, response.Data.Candidates, 2)
	summaries := map[int]string{}
	for _, cand := range response.Data.Candidates {
		summaries[cand.Id] = cand.HealthSummary
	}
	require.Equal(t, "DISABLED", summaries[disabledCandidate.Id])
	require.Equal(t, "UNKNOWN", summaries[enabledCandidate.Id], "never-observed candidate reports UNKNOWN")
}
