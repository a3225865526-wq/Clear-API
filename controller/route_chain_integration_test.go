package controller

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service/routechain"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type chainTestEnv struct {
	db  *gorm.DB
	ctx *gin.Context
}

func newChainTestEnv(t *testing.T) *chainTestEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "chain_it.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, database.AutoMigrate(&model.Channel{}, &model.Ability{}))
	oldDB := model.DB
	model.DB = database
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	model.InitColumnQuotingForTest()
	t.Cleanup(func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	return &chainTestEnv{db: database, ctx: ctx}
}

func (e *chainTestEnv) addChannel(t *testing.T, id int) {
	t.Helper()
	require.NoError(t, e.db.Create(&model.Channel{
		Id:     id,
		Type:   1, // OpenAI
		Key:    "sk-test",
		Status: common.ChannelStatusEnabled,
		Name:   "chain-channel",
	}).Error)
	require.NoError(t, e.db.Create(&model.Ability{
		Group:     "default",
		Model:     "gpt-chain",
		ChannelId: id,
		Enabled:   true,
	}).Error)
}

func (e *chainTestEnv) chainState(t *testing.T, candidates []routechain.Candidate) *routechain.RequestState {
	t.Helper()
	state := &routechain.RequestState{
		Snapshot: &routechain.Snapshot{
			ChainId: 1, Version: 1, Model: "gpt-chain", Group: "default",
			Candidates: candidates,
		},
	}
	e.ctx.Set("route_chain_request_state", state)
	return state
}

func chainInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
}

// R3: the retry path binds the exact candidate target — never a weighted
// re-pick from the selection pool.
func TestChainGetChannelBindsExactCandidate(t *testing.T) {
	env := newChainTestEnv(t)
	env.addChannel(t, 21)
	env.addChannel(t, 22)
	state := env.chainState(t, []routechain.Candidate{
		{Id: 1, Type: model.RouteChainCandidateTypeAPIChannel, TargetId: 21, Priority: 2},
		{Id: 2, Type: model.RouteChainCandidateTypeAPIChannel, TargetId: 22, Priority: 1},
	})
	state.MarkAttempt(env.ctx, 0) // Distribute bound candidate 0 (channel 21)

	channel, channelErr := getChannel(env.ctx, chainInfo(), nil)
	// require.Nil (reflection-based): a nil *types.NewAPIError converts to a
	// non-nil error interface, so require.NoError would false-fail here.
	require.Nil(t, channelErr)
	require.NotNil(t, channel)
	require.Equal(t, 22, channel.Id, "second candidate must be the exact-bound channel")
	require.Equal(t, 22, env.ctx.GetInt("channel_id"))
	require.Equal(t, 2, state.AttemptsMade(), "distribute bound candidate 0, retry bound candidate 1")
}

// R28: a POOL candidate pointing at a plain API channel is a configuration
// error — fail closed by skipping the candidate, never dispatching to it.
func TestChainGetChannelSkipsMismatchedCandidate(t *testing.T) {
	env := newChainTestEnv(t)
	env.addChannel(t, 21)
	env.addChannel(t, 22)
	state := env.chainState(t, []routechain.Candidate{
		{Id: 1, Type: model.RouteChainCandidateTypePool, TargetId: 21, Priority: 2},
		{Id: 2, Type: model.RouteChainCandidateTypeAPIChannel, TargetId: 22, Priority: 1},
	})
	state.MarkAttempt(env.ctx, 0)

	channel, channelErr := getChannel(env.ctx, chainInfo(), nil)
	require.Nil(t, channelErr)
	require.NotNil(t, channel)
	require.Equal(t, 22, channel.Id, "mismatched pool candidate must be skipped")
	require.Equal(t, 22, env.ctx.GetInt("channel_id"))
}

// R5-R19 at decision level: evidence flags + classified outcome drive the
// centralized policy; no status branching outside it.
func TestChainRetryDecisionMatrix(t *testing.T) {
	baseState := func(t *testing.T, ctx *gin.Context, policy *model.RouteCandidatePolicy) *routechain.RequestState {
		t.Helper()
		state := &routechain.RequestState{
			Snapshot: &routechain.Snapshot{
				ChainId: 1, Version: 1, Model: "gpt-chain", Group: "default",
				Candidates: []routechain.Candidate{
					{Id: 1, Type: model.RouteChainCandidateTypeAPIChannel, TargetId: 21, Priority: 2, Policy: policy},
					{Id: 2, Type: model.RouteChainCandidateTypeAPIChannel, TargetId: 22, Priority: 1, Policy: policy},
				},
			},
		}
		ctx.Set("route_chain_request_state", state)
		return state
	}
	openAIError := func(status int, code string) *types.NewAPIError {
		return types.WithOpenAIError(types.OpenAIError{Message: "upstream", Type: "server_error", Code: code}, status)
	}

	cases := []struct {
		name           string
		attempted      bool
		usageUncertain bool
		clientOutput   bool
		toolChain      bool
		policy         *model.RouteCandidatePolicy
		status         int
		code           string
		wantAction     string
		wantReason     string
	}{
		{"pre-dispatch 5xx advances", false, false, false, false, nil, 500, "internal_server_error", "retry", "pre_dispatch_failure"},
		{"post-write transport failure stops", true, false, false, false, nil, 500, "internal_server_error", "stop", "upstream_outcome_uncertain_no_replay"},
		{"pool structured exhaustion advances", true, false, false, false, nil, 503, "auth_unavailable", "retry", "pool_no_eligible_account"},
		{"pool auth_not_found advances", true, false, false, false, nil, 503, "auth_not_found", "retry", "pool_no_eligible_account"},
		{"ordinary upstream 503 stops", true, false, false, false, nil, 503, "internal_server_error", "stop", "upstream_outcome_uncertain_no_replay"},
		{"401 explicit no-work advances", true, false, false, false, nil, 401, "invalid_api_key", "retry", "authoritative_rejection_no_work"},
		{"403 explicit no-work advances", true, false, false, false, nil, 403, "insufficient_quota", "retry", "authoritative_rejection_no_work"},
		{"404 default policy advances", true, false, false, false, nil, 404, "model_not_found", "retry", "authoritative_rejection_no_work"},
		{"429 default policy stops", true, false, false, false, nil, 429, "rate_limit_exceeded", "stop", "failover_on_429_disabled"},
		{"429 with policy on advances", true, false, false, false, &model.RouteCandidatePolicy{Failure: &model.RouteFailurePolicy{FailoverOn429: boolPtr(true)}}, 429, "rate_limit_exceeded", "retry", "rate_limit_rejection_no_work"},
		{"404 with policy off stops", true, false, false, false, &model.RouteCandidatePolicy{Failure: &model.RouteFailurePolicy{FailoverOn404: boolPtr(false)}}, 404, "model_not_found", "stop", "failover_on_404_disabled"},
		{"client output locks route", true, false, true, false, nil, 500, "internal_server_error", "stop", "route_locked_after_client_output"},
		{"tool chain request never advances", false, false, false, true, nil, 500, "internal_server_error", "stop", "tool_chain_not_idempotent"},
		{"usage uncertainty stops", true, true, false, false, nil, 500, "internal_server_error", "stop", "usage_observed_no_replay"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newChainTestEnv(t)
			state := baseState(t, env.ctx, tc.policy)
			state.MarkAttempt(env.ctx, 0)
			env.ctx.Set("upstream_attempted", tc.attempted)
			env.ctx.Set("upstream_usage_uncertain", tc.usageUncertain)
			env.ctx.Set("tool_chain_no_retry", tc.toolChain)
			if tc.clientOutput {
				_, _ = env.ctx.Writer.WriteString("data: {\"delta\":1}\n\n")
			}
			decision := chainRetryDecision(env.ctx, state, openAIError(tc.status, tc.code), "")
			require.Equal(t, tc.wantAction, decision.Action, tc.name)
			require.Equal(t, tc.wantReason, decision.Reason, tc.name)
			require.Equal(t, "route_chain", decision.Source)
			if decision.Action == "retry" {
				require.False(t, env.ctx.GetBool("upstream_attempted"), "advance path must reset per-attempt dispatch flags")
			}
		})
	}
}

// R21: no candidate budget left → deterministic exhaustion decision.
func TestChainRetryDecisionExhausted(t *testing.T) {
	env := newChainTestEnv(t)
	state := &routechain.RequestState{
		Snapshot: &routechain.Snapshot{
			ChainId: 1, Version: 1, Model: "gpt-chain", Group: "default",
			Candidates: []routechain.Candidate{{Id: 1, Type: model.RouteChainCandidateTypeAPIChannel, TargetId: 21, Priority: 1}},
		},
	}
	env.ctx.Set("route_chain_request_state", state)
	state.MarkAttempt(env.ctx, 0)
	decision := chainRetryDecision(env.ctx, state, types.WithOpenAIError(types.OpenAIError{Code: "x"}, 500), "")
	require.Equal(t, "stop", decision.Action)
	require.Equal(t, "route_exhausted", decision.Reason)
}

// R29: the exhaustion error is sanitized and protocol-compatible.
func TestRouteExhaustedClientErrorShape(t *testing.T) {
	err := routechain.RouteExhaustedClientError()
	require.NotNil(t, err)
	require.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
	require.True(t, types.IsSkipRetryError(err))
	require.Equal(t, types.ErrorCodeRouteExhausted, err.GetErrorCode())
	require.NotContains(t, err.Error(), "channel")
	require.NotContains(t, err.Error(), "credential")
}

// R30 (structural): the chain path never re-prepares billing — the only
// PrepareRequestBilling call site sits before the candidate loop.
func TestChainBillingBoundarySingleReservationCallSite(t *testing.T) {
	source, err := os.ReadFile("relay.go")
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(source), "relay.PrepareRequestBilling("), "billing must be prepared exactly once per request, outside the candidate loop")
}

func boolPtr(v bool) *bool {
	return &v
}
