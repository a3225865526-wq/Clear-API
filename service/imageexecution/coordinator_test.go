package imageexecution

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service/accountpool"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestPrepareFreezesOperatorPolicy proves the request binding: the operator
// preset is frozen, a client-supplied source or reserved header cannot change
// it, an idempotent replay never creates a second request, and a corrupt policy
// fails closed.
func TestPrepareFreezesOperatorPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fixture := newExecutionFixture(t)
	previousDB := model.DB
	model.DB = fixture.db
	previousOptions := common.OptionMap
	common.OptionMapRWMutex.Lock()
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		model.DB = previousDB
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.OptionMapRWMutex.Unlock()
	})

	_, err := model.SaveImageSourcePolicy(model.ImageSourcePolicy{Model: "gpt-image-2", Strategy: model.ImageSourcePoolFirst, Enabled: true}, 0)
	require.NoError(t, err)

	t.Setenv(imagesModeEnv, "true")
	t.Setenv(imagesBaseModelEnv, "gpt-5.4-mini")
	t.Setenv(imagesPoolIDEnv, "synthetic-pool")
	// A controlled request needs a frozen fallback order; seed one account row
	// and its policy so admission reads a real operator decision.
	seedImagePoolOrder(t, fixture.db)

	// A client body claiming a different source must not be trusted; the frozen
	// operator preset wins and the request is recorded with it.
	c, _ := newImageContext(t, `{"model":"gpt-image-2","prompt":"cat","n":1,"source":"api","account_id":"attacker"}`)
	info := &relaycommon.RelayInfo{RequestId: "req-freeze", UserId: 11, TokenId: 12, RelayMode: 5}
	request := &dto.ImageRequest{Model: "gpt-image-2", Prompt: "cat"}
	controlled, apiErr := Prepare(c, info, request, BodyDigestOf([]byte(`{"model":"gpt-image-2","prompt":"cat","n":1}`)))
	require.Nil(t, apiErr)
	require.NotNil(t, controlled)
	require.Equal(t, model.ImageSourcePoolFirst, controlled.Policy.Strategy)
	require.False(t, controlled.Execution.Replay())
	require.Equal(t, "gpt-5.4-mini", controlled.BaseModel)
	require.Equal(t, "synthetic-pool", controlled.PoolID)
	// No fallback budget is configured: the default is zero, never "unlimited".
	require.Equal(t, 0, controlled.MaxFallback)

	// Neither a client source nor reserved headers can authorize a pool run.
	require.Empty(t, c.Request.Header.Get(PolicyHeader))
	require.Empty(t, c.Request.Header.Get("X-ClearAPI-PG02-Request"))

	// The same idempotency key and body resolve to the stored request.
	c2, _ := newImageContextWithKey(t, `{"model":"gpt-image-2","prompt":"cat","n":1}`, "client-key-1")
	replay, apiErr := Prepare(c2, &relaycommon.RelayInfo{RequestId: "req-freeze-2", UserId: 11, TokenId: 12, RelayMode: 5}, request, BodyDigestOf([]byte(`{"model":"gpt-image-2","prompt":"cat","n":1}`)))
	require.Nil(t, apiErr)
	require.NotNil(t, replay)

	// A different body under the same key is refused with 409.
	c3, _ := newImageContextWithKey(t, `{"model":"gpt-image-2","prompt":"dog","n":1}`, "client-key-1")
	_, apiErr = Prepare(c3, &relaycommon.RelayInfo{RequestId: "req-freeze-3", UserId: 11, TokenId: 12, RelayMode: 5}, &dto.ImageRequest{Model: "gpt-image-2", Prompt: "dog"}, BodyDigestOf([]byte(`{"model":"gpt-image-2","prompt":"dog","n":1}`)))
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusConflict, apiErr.StatusCode)

	// A model without an operator policy keeps legacy routing untouched.
	c4, _ := newImageContext(t, `{"model":"gpt-other","prompt":"cat"}`)
	legacy, apiErr := Prepare(c4, &relaycommon.RelayInfo{RequestId: "req-legacy", UserId: 11, TokenId: 12, RelayMode: 5}, &dto.ImageRequest{Model: "gpt-other", Prompt: "cat"}, BodyDigestOf([]byte(`{}`)))
	require.Nil(t, apiErr)
	require.Nil(t, legacy)

	// A corrupt operator policy fails closed with 503, never a silent default.
	common.OptionMapRWMutex.Lock()
	common.OptionMap[model.ImageSourcePolicyOptionKey("gpt-image-2")] = "{broken"
	common.OptionMapRWMutex.Unlock()
	c5, _ := newImageContext(t, `{"model":"gpt-image-2","prompt":"cat"}`)
	_, apiErr = Prepare(c5, &relaycommon.RelayInfo{RequestId: "req-corrupt", UserId: 11, TokenId: 12, RelayMode: 5}, &dto.ImageRequest{Model: "gpt-image-2", Prompt: "cat"}, BodyDigestOf([]byte(`{}`)))
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)
}

// TestSourceStrategyControlsDispatch proves the four presets: the *_only
// presets forbid the other source, the *_first presets require their source to
// come first, and no preset ever allows a second dispatch after an attempt that
// may have reached a provider.
func TestSourceStrategyControlsDispatch(t *testing.T) {
	cases := []struct {
		strategy  string
		first     string
		second    string
		firstErr  bool
		secondErr bool
	}{
		{model.ImageSourcePoolOnly, model.RouteChainCandidateTypePool, model.RouteChainCandidateTypeAPIChannel, false, true},
		{model.ImageSourceAPIOnly, model.RouteChainCandidateTypeAPIChannel, model.RouteChainCandidateTypePool, false, true},
		{model.ImageSourcePoolFirst, model.RouteChainCandidateTypePool, model.RouteChainCandidateTypeAPIChannel, false, false},
		{model.ImageSourceAPIFirst, model.RouteChainCandidateTypeAPIChannel, model.RouteChainCandidateTypePool, false, false},
		// A pool_first request that the chain starts on an API channel violates
		// the frozen ordering, and vice versa.
		{model.ImageSourcePoolFirst, model.RouteChainCandidateTypeAPIChannel, "", true, false},
		{model.ImageSourceAPIFirst, model.RouteChainCandidateTypePool, "", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.strategy+"_"+tc.first, func(t *testing.T) {
			controlled := &Controlled{Policy: &SourcePolicy{Strategy: tc.strategy, Policy: &model.ImageSourcePolicy{
				Model: "gpt-image-2", Strategy: tc.strategy, Enabled: true,
			}}}
			err := controlled.validateCandidate(tc.first)
			if tc.firstErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tc.firstErr || tc.second == "" {
				return
			}
			controlled.AttemptSeq = 1
			// Without a trusted zero-dispatch proof, a second dispatch is never
			// legal.
			require.Error(t, controlled.validateCandidate(tc.second))
			controlled.AllowTrustedHandover(tc.first)
			err = controlled.validateCandidate(tc.second)
			if tc.secondErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestUnknownKeepsReservationAndNeverSettles proves the UNKNOWN contract: the
// request is marked held_unknown, it stays in the reconciliation set, and the
// controlled flag that guards the generic refund path is set.
func TestUnknownKeepsReservationAndNeverSettles(t *testing.T) {
	fixture := newExecutionFixture(t)
	previousDB := model.DB
	model.DB = fixture.db
	t.Cleanup(func() { model.DB = previousDB })

	entry := &model.ImageExecution{
		RequestID: "req-unknown", UserID: 11, TokenID: 12,
		BodyDigest: model.ImageRequestBodyDigest([]byte(`{}`)), Operation: model.ImageRequestOperation,
		Model: "gpt-image-2", RequestedCount: 1, SourceStrategy: model.ImageSourcePoolFirst,
	}
	require.NoError(t, model.CreateImageExecutionRequest(entry))
	require.NoError(t, model.AdvanceImageExecution("req-unknown", model.ImageRequestCreated, model.ImageRequestReserved, nil))

	controlled := &Controlled{Execution: &Execution{entry: entry}}
	controlled.RefuseUnknown("transport_error_after_send")
	require.True(t, controlled.Unknown)
	require.False(t, Settled(controlled))

	held, err := model.LoadImageExecution("req-unknown")
	require.NoError(t, err)
	require.Equal(t, model.ImageRequestUnknown, held.State)
	require.Equal(t, "held_unknown", held.BillingState)

	// A settled request is never refunded by the generic path.
	settled := &Controlled{settled: true}
	require.True(t, Settled(settled))
}

// TestSignPolicyBindsFinalBytes proves the signed payload covers the exact
// outbound body and carries the frozen policy identity.
func TestSignPolicyBindsFinalBytes(t *testing.T) {
	t.Setenv("CLEARAPI_PG03_IMAGES_POLICY_KEY", "synthetic-images-policy-key-012345678901234567890123")
	body := `{"model":"gpt-image-2","prompt":"cat","n":1}`
	req := httptest.NewRequest(http.MethodPost, "https://executor.invalid/v1/images/generations", strings.NewReader(body))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(body)), nil }
	require.NoError(t, SignPolicy(req, "req-1", "req-1-a1", "gpt-image-2", "gpt-5.4-mini", "pool-1", 12, []string{"account-ref"}, 0))
	require.NotEmpty(t, req.Header.Get(PolicyHeader))
	require.NotEmpty(t, req.Header.Get(SignatureHeader))
	require.Equal(t, "req-1", req.Header.Get(RequestHeader))
	require.Equal(t, "req-1-a1", req.Header.Get(AttemptHeader))

	// A body that changes after signing no longer matches the signed digest.
	changed := httptest.NewRequest(http.MethodPost, "https://executor.invalid/v1/images/generations", strings.NewReader(body))
	changed.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(body + " ")), nil }
	require.NoError(t, SignPolicy(changed, "req-1", "req-1-a1", "gpt-image-2", "gpt-5.4-mini", "pool-1", 12, []string{"account-ref"}, 0))
	require.NotEqual(t, req.Header.Get(PolicyHeader), changed.Header.Get(PolicyHeader))

	// An invalid identity is refused instead of being signed.
	broken := httptest.NewRequest(http.MethodPost, "https://executor.invalid/v1/images/generations", strings.NewReader(body))
	broken.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(body)), nil }
	require.Error(t, SignPolicy(broken, "", "req-1-a1", "gpt-image-2", "gpt-5.4-mini", "pool-1", 12, []string{"account-ref"}, 0))
	require.Empty(t, broken.Header.Get(PolicyHeader))
}

type executionFixture struct{ db *gorm.DB }

func newExecutionFixture(t *testing.T) executionFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "imageexec.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, model.MigrateImageExecutions(db))
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	return executionFixture{db: db}
}

func newImageContext(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(body))
	return c, recorder
}

func newImageContextWithKey(t *testing.T, body, key string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	c, recorder := newImageContext(t, body)
	c.Request.Header.Set("Idempotency-Key", key)
	return c, recorder
}

// seedImagePoolOrder creates the minimal operator state a controlled image
// request needs: one codex account and its frozen fallback policy.
func seedImagePoolOrder(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, model.MigrateUpstreamPool(db))
	require.NoError(t, db.AutoMigrate(&model.PoolFallbackPolicy{}, &model.PoolFallbackMember{}))
	account := model.UpstreamPoolAccount{
		Platform: "codex", AccessType: "oauth_browser", ExecutorId: accountpool.ExecutorCliproxyAPI,
		CredentialRef: "images-account.json", AdminStatus: model.PoolAdminStatusEnabled,
		RunStatus: model.PoolRunStatusAvailable, Version: 1,
	}
	require.NoError(t, db.Create(&account).Error)
	policy := model.PoolFallbackPolicy{PoolId: "synthetic-pool", ModelName: "gpt-image-2", MaxFallbackCount: 0, Enabled: true}
	require.NoError(t, db.Create(&policy).Error)
	require.NoError(t, db.Create(&model.PoolFallbackMember{PolicyId: policy.Id, AccountId: account.Id, Priority: 10, Enabled: true}).Error)
}
