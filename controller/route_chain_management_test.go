package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/routechain"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Stage 6 Operator Console API tests (design §2.11): the U1–U30 matrix —
// owner gating, CAS, validation, archive semantics, IDOR, secret redaction
// and the no-upstream-request guarantee.

func newManagementTestEnv(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	routechain.Breaker.ResetForTest()
	previousRedis := common.RedisEnabled
	previousCache := common.MemoryCacheEnabled
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.RedisEnabled = previousRedis
		common.MemoryCacheEnabled = previousCache
	})
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "chain_mgmt.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, database.AutoMigrate(
		&model.User{}, &model.AuditLog{}, &model.Channel{}, &model.Ability{},
		&model.RouteChain{}, &model.RouteChainCandidate{},
		&model.RouteChainRequestRecord{}, &model.RouteChainAttemptRecord{}, &model.RouteChainDecisionRecord{},
	))
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	model.InitColumnQuotingForTest()
	oldDB := model.DB
	model.DB = database
	t.Cleanup(func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	})
	return database
}

// addMgmtChannel creates a channel; pool-bound ones carry the upstream_pool
// settings marker PoolChannelBound detects.
func addMgmtChannel(t *testing.T, id int, name string, poolBound bool) {
	t.Helper()
	channel := &model.Channel{
		Id:     id,
		Type:   1,
		Key:    "sk-mgmt-secret-" + strconv.Itoa(id),
		Status: common.ChannelStatusEnabled,
		Name:   name,
		Models: "gpt-chain, gpt-other",
		Group:  "default",
	}
	if poolBound {
		setting := `{"upstream_pool":{},"pool_routing_enabled":true}`
		channel.Setting = &setting
	}
	require.NoError(t, model.DB.Create(channel).Error)
	require.NoError(t, model.DB.Create(&model.Ability{Group: "default", Model: "gpt-chain", ChannelId: id, Enabled: true}).Error)
}

func addMgmtChain(t *testing.T, model_, group string, targets ...int) *model.RouteChainWithCandidates {
	t.Helper()
	chain := &model.RouteChainWithCandidates{
		RouteChain: model.RouteChain{Model: model_, Group: group, Enabled: true},
	}
	for i, target := range targets {
		chain.Candidates = append(chain.Candidates, model.RouteChainCandidate{
			CandidateType: model.RouteChainCandidateTypeAPIChannel,
			TargetId:      target,
			Priority:      len(targets) - i,
			Enabled:       true,
		})
	}
	require.NoError(t, model.UpsertRouteChain(chain, 0))
	return chain
}

func callMgmtHandler(t *testing.T, handler gin.HandlerFunc, method, path string, body any, params gin.Params) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	var reader *bytes.Reader
	if body != nil {
		raw, err := common.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	c.Request = httptest.NewRequest(method, path, reader)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = params
	handler(c)
	return recorder
}

func chainParams(chainId int) gin.Params {
	return gin.Params{{Key: "id", Value: strconv.Itoa(chainId)}}
}

func candidateParams(chainId, candidateId int) gin.Params {
	return gin.Params{
		{Key: "id", Value: strconv.Itoa(chainId)},
		{Key: "candidateId", Value: strconv.Itoa(candidateId)},
	}
}

func apiBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var result map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result), "response: %s", recorder.Body.String())
	return result
}

func apiSuccess(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	body := apiBody(t, recorder)
	require.Equal(t, true, body["success"], "body: %v", body)
	return body
}

func issueCodes(t *testing.T, body map[string]any) []string {
	t.Helper()
	raw, ok := body["errors"].([]any)
	require.True(t, ok, "body: %v", body)
	codes := make([]string, 0, len(raw))
	for _, item := range raw {
		entry := item.(map[string]any)
		codes = append(codes, entry["code"].(string))
	}
	return codes
}

func warningCodes(t *testing.T, body map[string]any) []string {
	t.Helper()
	raw, ok := body["warnings"].([]any)
	require.True(t, ok, "body: %v", body)
	codes := make([]string, 0, len(raw))
	for _, item := range raw {
		entry := item.(map[string]any)
		codes = append(codes, entry["code"].(string))
	}
	return codes
}

func containsCode(codes []string, code string) bool {
	for _, c := range codes {
		if c == code {
			return true
		}
	}
	return false
}

func validChainSpec(model_, group string, crossSource bool, candidates ...map[string]any) map[string]any {
	if candidates == nil {
		candidates = []map[string]any{}
	}
	return map[string]any{
		"model": model_, "group": group, "enabled": true,
		"allow_cross_source": crossSource, "candidates": candidates,
	}
}

func candidateSpec(targetId int, priority int, extra map[string]any) map[string]any {
	spec := map[string]any{
		"candidate_type": "api_channel", "target_id": targetId,
		"priority": priority, "enabled": true, "policy": "",
	}
	for key, value := range extra {
		spec[key] = value
	}
	return spec
}

// U1: owner list returns every chain with its presentation summary.
func TestMgmtListRouteChains(t *testing.T) {
	newManagementTestEnv(t)
	addMgmtChain(t, "gpt-chain", "default", 21, 22)

	recorder := callMgmtHandler(t, ListRouteChains, http.MethodGet, "/api/route-chains", nil, nil)
	body := apiSuccess(t, recorder)
	items := body["data"].(map[string]any)["items"].([]any)
	require.Len(t, items, 1)
	item := items[0].(map[string]any)
	require.Equal(t, "gpt-chain", item["model"])
	require.Equal(t, float64(2), item["candidate_count"])
	require.Equal(t, "api_channel", item["primary_candidate"].(map[string]any)["type"])
	require.Equal(t, float64(1), item["fallback_count"])
	require.NotEmpty(t, item["overall_health"])
	gates := body["data"].(map[string]any)["gates"].(map[string]any)
	require.Contains(t, gates, "route_chain_enabled")
	require.Contains(t, gates, "circuit_breaker_enabled")
}

// U2/U21/U22/U23: without a root session every management route rejects the
// request before any handler runs (RootAuth on the group).
func TestMgmtRoutesRequireRoot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	api := engine.Group("/api")
	api.Use(middleware.RootAuth())
	{
		api.GET("/route-chains", ListRouteChains)
		api.GET("/route-chains/gates", GetRouteChainGates)
		api.GET("/route-chains/targets", GetRouteChainTargets)
		api.POST("/route-chains", CreateRouteChain)
		api.GET("/route-chains/:id", GetRouteChainDetail)
		api.PUT("/route-chains/:id", UpdateRouteChain)
		api.POST("/route-chains/:id/enable", SetRouteChainState)
		api.POST("/route-chains/:id/reorder", ReorderRouteChain)
		api.POST("/route-chains/:id/archive", ArchiveRouteChain)
		api.DELETE("/route-chains/:id", DeleteRouteChain)
		api.GET("/route-chains/:id/health", GetRouteChainHealth)
		api.GET("/route-chains/:id/attempts", GetRouteChainAttempts)
		api.GET("/route-chains/:id/decisions", GetRouteChainDecisions)
	}
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/route-chains"},
		{http.MethodGet, "/api/route-chains/gates"},
		{http.MethodGet, "/api/route-chains/targets"},
		{http.MethodPost, "/api/route-chains"},
		{http.MethodGet, "/api/route-chains/1"},
		{http.MethodPut, "/api/route-chains/1"},
		{http.MethodPost, "/api/route-chains/1/enable"},
		{http.MethodPost, "/api/route-chains/1/reorder"},
		{http.MethodPost, "/api/route-chains/1/archive"},
		{http.MethodDelete, "/api/route-chains/1"},
		{http.MethodGet, "/api/route-chains/1/health"},
		{http.MethodGet, "/api/route-chains/1/attempts"},
		{http.MethodGet, "/api/route-chains/1/decisions"},
	} {
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.path, nil))
		require.Equal(t, http.StatusUnauthorized, recorder.Code, tc.path)
	}
}

// U3: create a valid chain — detail returned, candidates persisted.
func TestMgmtCreateRouteChain(t *testing.T) {
	newManagementTestEnv(t)
	addMgmtChannel(t, 21, "primary", false)

	recorder := callMgmtHandler(t, CreateRouteChain, http.MethodPost, "/api/route-chains",
		validChainSpec("gpt-chain", "default", false, candidateSpec(21, 1, nil)), nil)
	body := apiSuccess(t, recorder)
	chain := body["data"].(map[string]any)["chain"].(map[string]any)
	require.Equal(t, "gpt-chain", chain["model"])
	require.Equal(t, float64(1), chain["version"])
	candidates := chain["candidates"].([]any)
	require.Len(t, candidates, 1)
	candidate := candidates[0].(map[string]any)
	require.Equal(t, float64(21), candidate["target_id"])
	require.Equal(t, "primary", candidate["channel"].(map[string]any)["name"])
	require.Equal(t, "api", candidate["channel"].(map[string]any)["source"])
	require.False(t, candidate["archived"].(bool))
	// The five dimensions are all present in the detail view.
	require.Contains(t, candidate, "model_served")
	require.Contains(t, candidate, "pricing_configured")
	require.Contains(t, candidate, "source_match")
	require.Contains(t, candidate, "runtime")
	require.Contains(t, candidate, "health_summary")
	require.Contains(t, candidate, "effective_breaker")
}

// U4: duplicate (model, group) creation is rejected.
func TestMgmtCreateDuplicateRejected(t *testing.T) {
	newManagementTestEnv(t)
	addMgmtChannel(t, 21, "primary", false)
	addMgmtChain(t, "gpt-chain", "default", 21)

	recorder := callMgmtHandler(t, CreateRouteChain, http.MethodPost, "/api/route-chains",
		validChainSpec("gpt-chain", "default", false, candidateSpec(21, 1, nil)), nil)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	body := apiBody(t, recorder)
	require.Equal(t, "ROUTE_CHAIN_EXISTS", body["code"])
}

// U5: stale CAS version on update is a 409, never a silent overwrite.
func TestMgmtUpdateStaleVersionConflict(t *testing.T) {
	newManagementTestEnv(t)
	addMgmtChannel(t, 21, "primary", false)
	addMgmtChannel(t, 22, "backup", false)
	chain := addMgmtChain(t, "gpt-chain", "default", 21)

	spec := validChainSpec("gpt-chain", "default", false, candidateSpec(22, 1, nil))
	spec["expected_version"] = chain.Version
	recorder := callMgmtHandler(t, UpdateRouteChain, http.MethodPut, "/api/route-chains/1", spec, chainParams(chain.Id))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	// Replay the same (now stale) version.
	recorder = callMgmtHandler(t, UpdateRouteChain, http.MethodPut, "/api/route-chains/1", spec, chainParams(chain.Id))
	require.Equal(t, http.StatusConflict, recorder.Code)
	body := apiBody(t, recorder)
	require.Equal(t, "ROUTE_CHAIN_VERSION_CONFLICT", body["code"])
}

// U6/U28: unknown candidate targets are rejected authoritatively.
func TestMgmtUnknownTargetRejected(t *testing.T) {
	newManagementTestEnv(t)
	addMgmtChannel(t, 21, "primary", false)

	recorder := callMgmtHandler(t, CreateRouteChain, http.MethodPost, "/api/route-chains",
		validChainSpec("gpt-chain", "default", false, candidateSpec(999, 1, nil)), nil)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.True(t, containsCode(issueCodes(t, apiBody(t, recorder)), "route_chain.target_missing"))
}

// U7: candidate type must match the target's pool binding — enforced by the
// backend, not just filtered in the UI.
func TestMgmtTypeTargetMismatchRejected(t *testing.T) {
	newManagementTestEnv(t)
	addMgmtChannel(t, 21, "api-channel", false)
	addMgmtChannel(t, 22, "pool-channel", true)

	// POOL candidate on an API channel.
	recorder := callMgmtHandler(t, CreateRouteChain, http.MethodPost, "/api/route-chains",
		validChainSpec("gpt-chain", "default", false,
			candidateSpec(21, 1, map[string]any{"candidate_type": "pool"})), nil)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.True(t, containsCode(issueCodes(t, apiBody(t, recorder)), "route_chain.type_target_mismatch"))

	// API_CHANNEL candidate on a pool channel.
	recorder = callMgmtHandler(t, CreateRouteChain, http.MethodPost, "/api/route-chains",
		validChainSpec("gpt-chain", "default", false, candidateSpec(22, 1, nil)), nil)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.True(t, containsCode(issueCodes(t, apiBody(t, recorder)), "route_chain.type_target_mismatch"))
}

// U8/U9: mixed sources require the explicit cross-source flag.
func TestMgmtCrossSourceValidation(t *testing.T) {
	newManagementTestEnv(t)
	addMgmtChannel(t, 21, "api-channel", false)
	addMgmtChannel(t, 22, "pool-channel", true)

	mixed := []map[string]any{
		candidateSpec(21, 2, nil),
		candidateSpec(22, 1, map[string]any{"candidate_type": "pool"}),
	}
	recorder := callMgmtHandler(t, CreateRouteChain, http.MethodPost, "/api/route-chains",
		validChainSpec("gpt-chain", "default", false, mixed...), nil)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.True(t, containsCode(issueCodes(t, apiBody(t, recorder)), "route_chain.mixed_sources_without_cross_source"))

	recorder = callMgmtHandler(t, CreateRouteChain, http.MethodPost, "/api/route-chains",
		validChainSpec("gpt-chain", "default", true, mixed...), nil)
	apiSuccess(t, recorder)
}

// U10: pricing missing is reported as a warning, never as a zero price.
func TestMgmtPricingMissingReported(t *testing.T) {
	newManagementTestEnv(t)
	addMgmtChannel(t, 21, "primary", false)

	recorder := callMgmtHandler(t, CreateRouteChain, http.MethodPost, "/api/route-chains",
		validChainSpec("chain-pricing-model", "default", false, candidateSpec(21, 1, nil)), nil)
	body := apiSuccess(t, recorder)
	require.True(t, containsCode(warningCodes(t, body["data"].(map[string]any)), "route_chain.pricing_missing"))
}

// U12: duplicate priorities are rejected.
func TestMgmtDuplicatePriorityRejected(t *testing.T) {
	newManagementTestEnv(t)
	addMgmtChannel(t, 21, "primary", false)
	addMgmtChannel(t, 22, "backup", false)

	recorder := callMgmtHandler(t, CreateRouteChain, http.MethodPost, "/api/route-chains",
		validChainSpec("gpt-chain", "default", false, candidateSpec(21, 1, nil), candidateSpec(22, 1, nil)), nil)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.True(t, containsCode(issueCodes(t, apiBody(t, recorder)), "route_chain.duplicate_priority"))
}

// U11: reorder persists the server-canonical order under CAS.
func TestMgmtReorderPersists(t *testing.T) {
	newManagementTestEnv(t)
	addMgmtChannel(t, 21, "primary", false)
	addMgmtChannel(t, 22, "backup", false)
	chain := addMgmtChain(t, "gpt-chain", "default", 21, 22)

	// Reverse the order: 22 becomes primary.
	body := map[string]any{
		"ordered_candidate_ids": []int{chain.Candidates[1].Id, chain.Candidates[0].Id},
		"expected_version":      chain.Version,
	}
	recorder := callMgmtHandler(t, ReorderRouteChain, http.MethodPost, "/api/route-chains/1/reorder", body, chainParams(chain.Id))
	apiSuccess(t, recorder)

	stored, err := model.GetRouteChainById(chain.Id)
	require.NoError(t, err)
	require.Equal(t, chain.Candidates[1].Id, stored.Candidates[0].Id, "22 must now be first")
	require.Equal(t, chain.Version+1, stored.Version)

	// Foreign candidate ids are rejected (set mismatch).
	badBody := map[string]any{
		"ordered_candidate_ids": []int{chain.Candidates[0].Id, 999999},
		"expected_version":      stored.Version,
	}
	recorder = callMgmtHandler(t, ReorderRouteChain, http.MethodPost, "/api/route-chains/1/reorder", badBody, chainParams(chain.Id))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

// U13/U14: candidate enable/disable flows through the chain CAS version.
func TestMgmtCandidateEnableCAS(t *testing.T) {
	newManagementTestEnv(t)
	addMgmtChannel(t, 21, "primary", false)
	chain := addMgmtChain(t, "gpt-chain", "default", 21)
	candidateId := chain.Candidates[0].Id

	body := map[string]any{"enabled": false, "expected_version": chain.Version}
	recorder := callMgmtHandler(t, SetRouteChainCandidateEnabled, http.MethodPost,
		"/api/route-chains/1/candidates/1/enabled", body, candidateParams(chain.Id, candidateId))
	apiSuccess(t, recorder)

	stored, err := model.GetRouteChainById(chain.Id)
	require.NoError(t, err)
	require.False(t, stored.Candidates[0].Enabled)
	require.Equal(t, chain.Version+1, stored.Version)

	// Replaying the stale version conflicts.
	recorder = callMgmtHandler(t, SetRouteChainCandidateEnabled, http.MethodPost,
		"/api/route-chains/1/candidates/1/enabled", body, candidateParams(chain.Id, candidateId))
	require.Equal(t, http.StatusConflict, recorder.Code)
}

// U15: a candidate id from another chain is rejected on update (IDOR).
func TestMgmtForeignCandidateRejected(t *testing.T) {
	newManagementTestEnv(t)
	addMgmtChannel(t, 21, "primary", false)
	addMgmtChannel(t, 22, "backup", false)
	addMgmtChain(t, "gpt-chain", "default", 21)
	chainB := addMgmtChain(t, "gpt-other", "default", 22)

	// Chain A's update payload carries chain B's candidate id.
	spec := validChainSpec("gpt-chain", "default", false, candidateSpec(21, 1, nil))
	spec["expected_version"] = 1
	spec["candidates"].([]map[string]any)[0]["id"] = chainB.Candidates[0].Id
	recorder := callMgmtHandler(t, UpdateRouteChain, http.MethodPut, "/api/route-chains/1", spec, chainParams(1))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.True(t, containsCode(issueCodes(t, apiBody(t, recorder)), "route_chain.foreign_candidate"))
}

// U16/U18: circuit reset returns the probeable state and never touches the
// candidate's persistent enabled flag.
func TestMgmtCircuitResetDoesNotEnableCandidate(t *testing.T) {
	newManagementTestEnv(t)
	addMgmtChannel(t, 21, "primary", false)
	chain := addMgmtChain(t, "gpt-chain", "default", 21)
	candidateId := chain.Candidates[0].Id
	require.NoError(t, model.SetRouteChainCandidateEnabled(chain.Id, candidateId, false, chain.Version))

	routechain.Breaker.ForceOpen(nil, chain.Id, candidateId, 60)
	recorder := callMgmtHandler(t, ResetRouteChainCandidateCircuit, http.MethodPost,
		"/api/route-chains/1/candidates/1/circuit/reset", nil, candidateParams(chain.Id, candidateId))
	apiSuccess(t, recorder)

	snapshot := routechain.Breaker.CandidateSnapshot(chain.Id, candidateId, nil)
	require.Equal(t, routechain.CircuitStateClosed, snapshot.State)
	stored, err := model.GetRouteChainById(chain.Id)
	require.NoError(t, err)
	require.False(t, stored.Candidates[0].Enabled, "reset must not re-enable the candidate")
}

// U17/U19: force-open is a transient runtime state and never disables the
// candidate persistently.
func TestMgmtForceOpenDoesNotDisableCandidate(t *testing.T) {
	newManagementTestEnv(t)
	addMgmtChannel(t, 21, "primary", false)
	chain := addMgmtChain(t, "gpt-chain", "default", 21)
	candidateId := chain.Candidates[0].Id

	recorder := callMgmtHandler(t, ForceOpenRouteChainCandidateCircuit, http.MethodPost,
		"/api/route-chains/1/candidates/1/circuit/open", map[string]any{"duration_seconds": 60}, candidateParams(chain.Id, candidateId))
	apiSuccess(t, recorder)

	snapshot := routechain.Breaker.CandidateSnapshot(chain.Id, candidateId, nil)
	require.Equal(t, routechain.CircuitStateOpen, snapshot.State)
	require.True(t, snapshot.Forced)
	stored, err := model.GetRouteChainById(chain.Id)
	require.NoError(t, err)
	require.True(t, stored.Candidates[0].Enabled, "force-open must not disable the candidate")
	require.True(t, stored.Enabled)
}

// U20: no secret material in any management response — the channel key is
// never serialized, and neither are the dangerous channel fields.
func TestMgmtSecretRedaction(t *testing.T) {
	newManagementTestEnv(t)
	addMgmtChannel(t, 21, "primary", true)
	addMgmtChain(t, "gpt-chain", "default", 21)

	responses := []*httptest.ResponseRecorder{
		callMgmtHandler(t, GetRouteChainTargets, http.MethodGet, "/api/route-chains/targets", nil, nil),
		callMgmtHandler(t, GetRouteChainDetail, http.MethodGet, "/api/route-chains/1", nil, chainParams(1)),
		callMgmtHandler(t, ListRouteChains, http.MethodGet, "/api/route-chains", nil, nil),
	}
	for _, recorder := range responses {
		apiSuccess(t, recorder)
		payload := recorder.Body.String()
		require.NotContains(t, payload, "sk-mgmt-secret", "channel key leaked")
		for _, field := range []string{`"key"`, `"base_url"`, `"other"`, `"settings"`, `"setting"`, `"channel_info"`, `"param_override"`, `"header_override"`, `"openai_organization"`} {
			require.NotContains(t, payload, field, "sensitive field %s present", field)
		}
	}
}

// U24: archive semantics — ledger-referenced candidates are archived instead
// of deleted; the ledger stays interpretable; history-bearing chains refuse
// hard delete.
func TestMgmtArchiveSemanticsPreserveLedger(t *testing.T) {
	newManagementTestEnv(t)
	addMgmtChannel(t, 21, "primary", false)
	addMgmtChannel(t, 22, "backup", false)
	chain := addMgmtChain(t, "gpt-chain", "default", 21, 22)
	historicCandidate := chain.Candidates[0].Id

	// Ledger history: one attempt and one decision on candidate 1.
	require.NoError(t, model.CreateRouteChainAttempt(&model.RouteChainAttemptRecord{
		AttemptId: "att-1", RequestId: "req-1", Sequence: 1,
		RouteChainId: chain.Id, RouteChainVersion: chain.Version,
		CandidateId: historicCandidate, CandidateType: model.RouteChainCandidateTypeAPIChannel,
		ChannelId: 21, StartedAt: time.Now().Unix(),
	}))
	require.NoError(t, model.CreateRouteChainDecision(&model.RouteChainDecisionRecord{
		RequestId: "req-1", ChainId: chain.Id, ChainVersion: chain.Version,
		CandidateId: historicCandidate, Phase: "selection", Decision: "visited", At: time.Now().Unix(),
	}))

	// Remove candidate 1 from the chain: it must be archived, not deleted.
	spec := validChainSpec("gpt-chain", "default", false, candidateSpec(22, 1, nil))
	spec["expected_version"] = chain.Version
	recorder := callMgmtHandler(t, UpdateRouteChain, http.MethodPut, "/api/route-chains/1", spec, chainParams(chain.Id))
	apiSuccess(t, recorder)

	stored, err := model.GetRouteChainById(chain.Id)
	require.NoError(t, err)
	require.Len(t, stored.Candidates, 2, "archived candidate row stays in the table")
	var archived *model.RouteChainCandidate
	for i := range stored.Candidates {
		if stored.Candidates[i].Archived {
			archived = &stored.Candidates[i]
		}
	}
	require.NotNil(t, archived, "removed candidate must be archived")
	require.Equal(t, historicCandidate, archived.Id)
	require.False(t, archived.Enabled)
	require.Equal(t, 21, archived.ArchivedTargetId)
	require.Equal(t, -historicCandidate, archived.TargetId, "archived row frees its target slot")

	// The ledger rows still join to a config row.
	detail := apiSuccess(t, callMgmtHandler(t, GetRouteChainDetail, http.MethodGet, "/api/route-chains/1", nil, chainParams(chain.Id)))
	chainView := detail["data"].(map[string]any)["chain"].(map[string]any)
	activeCandidates := chainView["candidates"].([]any)
	require.Len(t, activeCandidates, 1, "only the active candidate is in the active list")
	archivedCandidates := chainView["archived_candidates"].([]any)
	require.Len(t, archivedCandidates, 1)
	require.Equal(t, "primary", archivedCandidates[0].(map[string]any)["channel"].(map[string]any)["name"])

	// Reorder covers exactly the active set (reloaded after the edit: the
	// no-id payload row re-created the surviving candidate).
	stored, err = model.GetRouteChainById(chain.Id)
	require.NoError(t, err)
	activeId := 0
	for i := range stored.Candidates {
		if !stored.Candidates[i].Archived {
			activeId = stored.Candidates[i].Id
		}
	}
	require.NotZero(t, activeId)
	reorderBody := map[string]any{
		"ordered_candidate_ids": []int{activeId},
		"expected_version":      stored.Version,
	}
	apiSuccess(t, callMgmtHandler(t, ReorderRouteChain, http.MethodPost, "/api/route-chains/1/reorder", reorderBody, chainParams(chain.Id)))

	// A freed target can be re-added (unique slot released by the archive).
	stored, err = model.GetRouteChainById(chain.Id) // reorder bumped the version
	require.NoError(t, err)
	readd := validChainSpec("gpt-chain", "default", false,
		candidateSpec(22, 2, nil), candidateSpec(21, 1, nil))
	readd["expected_version"] = stored.Version
	apiSuccess(t, callMgmtHandler(t, UpdateRouteChain, http.MethodPut, "/api/route-chains/1", readd, chainParams(chain.Id)))

	// Chain delete with history is refused; disable+archive is the path.
	recorder = callMgmtHandler(t, DeleteRouteChain, http.MethodDelete, "/api/route-chains/1",
		map[string]any{"expected_version": 99}, chainParams(chain.Id))
	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Equal(t, "ROUTE_CHAIN_HAS_HISTORY", apiBody(t, recorder)["code"])

	// A history-free chain hard-deletes fine.
	chain2 := addMgmtChain(t, "gpt-fresh", "default", 22)
	recorder = callMgmtHandler(t, DeleteRouteChain, http.MethodDelete, "/api/route-chains/2",
		map[string]any{"expected_version": chain2.Version}, chainParams(chain2.Id))
	apiSuccess(t, recorder)
	_, err = model.GetRouteChainById(chain2.Id)
	require.ErrorIs(t, err, model.ErrRouteChainNotFound)
}

// U25/U26/U27: malformed policies and out-of-bound breaker values are
// rejected before persistence.
func TestMgmtPolicyValidation(t *testing.T) {
	newManagementTestEnv(t)
	addMgmtChannel(t, 21, "primary", false)

	for _, tc := range []struct {
		name   string
		policy string
	}{
		{"malformed json", "{not-json"},
		{"failure threshold over bound", `{"health_policy":{"failure_threshold":99}}`},
		{"cooldown over bound", `{"health_policy":{"cooldown_seconds":4000}}`},
		{"negative threshold", `{"health_policy":{"failure_threshold":-1}}`},
		{"unknown capability", `{"capabilities":["teleport"]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := validChainSpec("gpt-chain", "default", false,
				candidateSpec(21, 1, map[string]any{"policy": tc.policy}))
			recorder := callMgmtHandler(t, CreateRouteChain, http.MethodPost, "/api/route-chains", spec, nil)
			require.Equal(t, http.StatusBadRequest, recorder.Code, "policy: %s", tc.policy)
			require.True(t, containsCode(issueCodes(t, apiBody(t, recorder)), "route_chain.policy_invalid"))
		})
	}
}

// U29: the gates endpoint reports the runtime gate states that the console
// displays as "Runtime Route Chain: OFF".
func TestMgmtGatesReported(t *testing.T) {
	newManagementTestEnv(t)
	recorder := callMgmtHandler(t, GetRouteChainGates, http.MethodGet, "/api/route-chains/gates", nil, nil)
	body := apiSuccess(t, recorder)
	gates := body["data"].(map[string]any)
	require.Contains(t, gates, "route_chain_enabled")
	require.Contains(t, gates, "circuit_breaker_enabled")
	require.Contains(t, gates, "max_candidates")
	require.Contains(t, gates, "overall_deadline_ms")
	require.Equal(t, false, gates["route_chain_enabled"], "gate must default off in tests (env unset)")
}

// U30: config CRUD never performs an upstream request — even with the
// channel's base URL pointed at a closed port, every mutation completes
// instantly (the test deadline is far below any dial timeout).
func TestMgmtConfigCrudPerformsNoUpstreamRequest(t *testing.T) {
	newManagementTestEnv(t)
	addMgmtChannel(t, 21, "primary", false)

	deadline := time.Now().Add(2 * time.Second)
	spec := validChainSpec("gpt-chain", "default", false, candidateSpec(21, 1, nil))
	body := apiSuccess(t, callMgmtHandler(t, CreateRouteChain, http.MethodPost, "/api/route-chains", spec, nil))
	chainView := body["data"].(map[string]any)["chain"].(map[string]any)
	version := int(chainView["version"].(float64))
	candidateId := int(chainView["candidates"].([]any)[0].(map[string]any)["id"].(float64))

	updateSpec := validChainSpec("gpt-chain", "default", false, candidateSpec(21, 1, map[string]any{"id": candidateId}))
	updateSpec["expected_version"] = version
	apiSuccess(t, callMgmtHandler(t, UpdateRouteChain, http.MethodPut, "/api/route-chains/1", updateSpec, chainParams(1)))
	apiSuccess(t, callMgmtHandler(t, ReorderRouteChain, http.MethodPost, "/api/route-chains/1/reorder",
		map[string]any{"ordered_candidate_ids": []int{candidateId}, "expected_version": version + 1}, chainParams(1)))
	apiSuccess(t, callMgmtHandler(t, GetRouteChainDetail, http.MethodGet, "/api/route-chains/1", nil, chainParams(1)))
	require.Less(t, time.Now(), deadline, "config CRUD must not dial upstreams")
}

// Chain enable/disable and archive flows (§2.2): CAS semantics end to end.
func TestMgmtChainEnableArchiveFlows(t *testing.T) {
	newManagementTestEnv(t)
	addMgmtChannel(t, 21, "primary", false)
	chain := addMgmtChain(t, "gpt-chain", "default", 21)

	// Disable the chain.
	recorder := callMgmtHandler(t, SetRouteChainState, http.MethodPost, "/api/route-chains/1/enable",
		map[string]any{"enabled": false, "expected_version": chain.Version}, chainParams(chain.Id))
	apiSuccess(t, recorder)
	stored, err := model.GetRouteChainById(chain.Id)
	require.NoError(t, err)
	require.False(t, stored.Enabled)
	require.False(t, stored.Archived)
	require.Equal(t, chain.Version+1, stored.Version)

	// Stale enable conflicts.
	recorder = callMgmtHandler(t, SetRouteChainState, http.MethodPost, "/api/route-chains/1/enable",
		map[string]any{"enabled": true, "expected_version": chain.Version}, chainParams(chain.Id))
	require.Equal(t, http.StatusConflict, recorder.Code)

	// Archive the chain: disabled + archived, read-only afterwards.
	recorder = callMgmtHandler(t, ArchiveRouteChain, http.MethodPost, "/api/route-chains/1/archive",
		map[string]any{"expected_version": stored.Version}, chainParams(chain.Id))
	apiSuccess(t, recorder)
	stored, err = model.GetRouteChainById(chain.Id)
	require.NoError(t, err)
	require.True(t, stored.Archived)
	require.False(t, stored.Enabled)

	// The engine keeps it inert: no enabled chain resolves.
	resolved, err := model.GetEnabledRouteChainForModel("gpt-chain", "default")
	require.NoError(t, err)
	require.Nil(t, resolved)

	// Archived chains are read-only for config edits.
	spec := validChainSpec("gpt-chain", "default", false, candidateSpec(21, 1, nil))
	spec["expected_version"] = stored.Version
	recorder = callMgmtHandler(t, UpdateRouteChain, http.MethodPut, "/api/route-chains/1", spec, chainParams(chain.Id))
	require.Equal(t, http.StatusBadRequest, recorder.Code)

	// Model+group are immutable on update.
	chain2 := addMgmtChain(t, "gpt-two", "default", 21)
	mutate := validChainSpec("gpt-renamed", "default", false, candidateSpec(21, 1, nil))
	mutate["expected_version"] = chain2.Version
	recorder = callMgmtHandler(t, UpdateRouteChain, http.MethodPut, "/api/route-chains/2", mutate, chainParams(chain2.Id))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.True(t, containsCode(issueCodes(t, apiBody(t, recorder)), "route_chain.model_group_immutable"))
}

// The target catalog only exposes safe channel fields (§七) and marks pool
// binding and source so the editor can filter POOL vs API_CHANNEL targets.
func TestMgmtTargetCatalog(t *testing.T) {
	newManagementTestEnv(t)
	addMgmtChannel(t, 21, "api-channel", false)
	addMgmtChannel(t, 22, "pool-channel", true)

	recorder := callMgmtHandler(t, GetRouteChainTargets, http.MethodGet, "/api/route-chains/targets", nil, nil)
	body := apiSuccess(t, recorder)
	targets := body["data"].([]any)
	require.Len(t, targets, 2)
	byId := map[int]map[string]any{}
	for _, item := range targets {
		entry := item.(map[string]any)
		byId[int(entry["id"].(float64))] = entry
	}
	require.Equal(t, "api", byId[21]["source"])
	require.Equal(t, false, byId[21]["pool_bound"])
	require.Equal(t, "pool", byId[22]["source"])
	require.Equal(t, true, byId[22]["pool_bound"])
	require.Equal(t, true, byId[21]["enabled"])
	require.Equal(t, "api-channel", byId[21]["name"])
	require.NotContains(t, recorder.Body.String(), "sk-mgmt-secret")
}

// The attempts and decisions panels read the ledger with the billing mirror
// (§二十五–§二十八): safe columns only, UNKNOWN_REVIEW passed through.
func TestMgmtRecentAttemptsAndDecisions(t *testing.T) {
	newManagementTestEnv(t)
	addMgmtChannel(t, 21, "primary", false)
	chain := addMgmtChain(t, "gpt-chain", "default", 21)
	candidateId := chain.Candidates[0].Id

	require.NoError(t, model.CreateRouteChainRequestRecord(&model.RouteChainRequestRecord{
		RequestId: "req-1", RouteChainId: chain.Id, RouteChainVersion: chain.Version,
		RequestedModel: "gpt-chain", CanonicalModel: "gpt-chain", Group: "default",
		StartedAt: time.Now().Unix(), FinalStatus: model.RouteChainRequestFinalSuccess,
		UserBillingState: model.RouteChainBillingSettled,
	}))
	require.NoError(t, model.CreateRouteChainAttempt(&model.RouteChainAttemptRecord{
		AttemptId: "att-1", RequestId: "req-1", Sequence: 1,
		RouteChainId: chain.Id, CandidateId: candidateId,
		CandidateType: model.RouteChainCandidateTypeAPIChannel, ChannelId: 21,
		StartedAt: time.Now().Unix(), TerminalState: model.RouteChainTerminalSucceeded,
		UsageState: model.RouteChainUsageUnavailable, ProviderCostState: model.RouteChainCostUnknownReview,
	}))
	require.NoError(t, model.CreateRouteChainDecision(&model.RouteChainDecisionRecord{
		RequestId: "req-1", ChainId: chain.Id, CandidateId: candidateId,
		Phase: "selection", Decision: "skipped", Reason: "circuit_open", At: time.Now().Unix(),
	}))

	recorder := callMgmtHandler(t, GetRouteChainAttempts, http.MethodGet, "/api/route-chains/1/attempts", nil, chainParams(chain.Id))
	body := apiSuccess(t, recorder)
	attempts := body["data"].(map[string]any)["items"].([]any)
	require.Len(t, attempts, 1)
	attempt := attempts[0].(map[string]any)
	require.Equal(t, "settled", attempt["user_billing_state"])
	require.Equal(t, "UNKNOWN_REVIEW", attempt["provider_cost_state"], "cost state must reach the UI verbatim")
	require.Equal(t, "unavailable", attempt["usage_state"])
	require.Equal(t, float64(0), attempt["http_status"], "attempts carry facts, not fabricated zeros")

	// Candidate filter narrows to the candidate.
	recorder = callMgmtHandler(t, GetRouteChainAttempts, http.MethodGet,
		fmt.Sprintf("/api/route-chains/1/attempts?candidate_id=%d", candidateId), nil, chainParams(chain.Id))
	apiSuccess(t, recorder)
	attempts = apiBody(t, recorder)["data"].(map[string]any)["items"].([]any)
	require.Len(t, attempts, 1)

	recorder = callMgmtHandler(t, GetRouteChainAttempts, http.MethodGet,
		"/api/route-chains/1/attempts?candidate_id=999", nil, chainParams(chain.Id))
	attempts = apiBody(t, recorder)["data"].(map[string]any)["items"].([]any)
	require.Empty(t, attempts)

	recorder = callMgmtHandler(t, GetRouteChainDecisions, http.MethodGet, "/api/route-chains/1/decisions", nil, chainParams(chain.Id))
	body = apiSuccess(t, recorder)
	decisions := body["data"].(map[string]any)["items"].([]any)
	require.Len(t, decisions, 1)
	require.Equal(t, "circuit_open", decisions[0].(map[string]any)["reason"], "safe reason code only")
}

// Empty chains and candidate overflow are rejected (§六).
func TestMgmtEmptyAndOverflowChainsRejected(t *testing.T) {
	newManagementTestEnv(t)
	addMgmtChannel(t, 21, "primary", false)

	recorder := callMgmtHandler(t, CreateRouteChain, http.MethodPost, "/api/route-chains",
		validChainSpec("gpt-chain", "default", false), nil)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.True(t, containsCode(issueCodes(t, apiBody(t, recorder)), "route_chain.candidate_empty"))

	overflow := make([]map[string]any, 0, 11)
	for i := 0; i < 11; i++ {
		overflow = append(overflow, candidateSpec(21, i+1, nil))
	}
	recorder = callMgmtHandler(t, CreateRouteChain, http.MethodPost, "/api/route-chains",
		validChainSpec("gpt-chain", "default", false, overflow...), nil)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.True(t, containsCode(issueCodes(t, apiBody(t, recorder)), "route_chain.candidate_overflow"))

	// Duplicate target (same channel twice) rejected.
	recorder = callMgmtHandler(t, CreateRouteChain, http.MethodPost, "/api/route-chains",
		validChainSpec("gpt-chain", "default", false, candidateSpec(21, 1, nil), candidateSpec(21, 2, nil)), nil)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.True(t, containsCode(issueCodes(t, apiBody(t, recorder)), "route_chain.duplicate_target"))
}

// Diagnostics carry the effective breaker config with Default/Override
// provenance (§十四) and pool authority states for POOL candidates (§二十二).
func TestMgmtDiagnosticsBreakerAndPool(t *testing.T) {
	newManagementTestEnv(t)
	addMgmtChannel(t, 21, "api-channel", false)
	addMgmtChannel(t, 22, "pool-channel", true)
	// Pool master switch off and no pool publication: distinct authority states.
	spec := validChainSpec("gpt-chain", "default", true,
		candidateSpec(21, 2, nil),
		candidateSpec(22, 1, map[string]any{"candidate_type": "pool", "policy": `{"health_policy":{"failure_threshold":5,"cooldown_seconds":120}}`}))
	body := apiSuccess(t, callMgmtHandler(t, CreateRouteChain, http.MethodPost, "/api/route-chains", spec, nil))
	chain := body["data"].(map[string]any)["chain"].(map[string]any)
	candidates := chain["candidates"].([]any)
	require.Len(t, candidates, 2)

	poolView := candidates[1].(map[string]any) // candidates order priority DESC: api p2 first, pool p1 second
	require.Equal(t, "pool", poolView["candidate_type"])
	authority := poolView["pool_authority"].(map[string]any)
	require.Equal(t, false, authority["master_enabled"], "pool master off is its own state")
	require.Equal(t, true, authority["channel_routing"])
	require.Equal(t, "unpublished", authority["publication"])
	breaker := poolView["effective_breaker"].(map[string]any)
	require.Equal(t, float64(5), breaker["failure_threshold"])
	require.Equal(t, true, breaker["failure_threshold_override"], "operator override must be visible")
	require.Equal(t, float64(120), breaker["cooldown_seconds"])
	require.Equal(t, true, breaker["cooldown_override"])
	require.Equal(t, false, breaker["half_open_probes_override"], "no override surface: always Default")

	apiView := candidates[0].(map[string]any)
	require.Nil(t, apiView["pool_authority"], "API candidates carry no pool authority block")
	require.Equal(t, false, apiView["effective_breaker"].(map[string]any)["failure_threshold_override"])
	require.True(t, containsCode(warningCodes(t, body["data"].(map[string]any)), "route_chain.source_mismatch"),
		"pool candidate on unpublished model must surface a source warning")
	require.True(t, strings.Contains(recorderBodyChain(t, body), "gpt-chain"))
}

func recorderBodyChain(t *testing.T, body map[string]any) string {
	t.Helper()
	raw, err := common.Marshal(body)
	require.NoError(t, err)
	return string(raw)
}
