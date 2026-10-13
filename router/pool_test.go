package router_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/router"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/accountpool"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const poolRawIdentity = "secret-identity@example-upstream.com"

// Run against the isolated Executor production-handler bridge. Both sides use
// their real policy, transport, receipt and billing paths; only providers mock.
func TestPG03ZeroDispatchRefundBilling(t *testing.T) {
	endpoint := os.Getenv("PG03_REFUND_EXECUTOR_URL")
	if endpoint == "" {
		t.Skip("requires isolated Executor production-handler bridge")
	}
	t.Setenv("CLEARAPI_BILLING_PROTECTION", "true")
	t.Setenv("CLEARAPI_PG03_POLICY_KEY", "synthetic-pg03-policy-key-012345678901234567890123456789")
	t.Setenv("CLEARAPI_PG02_GATEWAY_PROOF_KEY", "synthetic-pg03-proof-key-for-unit-tests-only")
	oldCount, oldBatch, oldLog := constant.CountToken, common.BatchUpdateEnabled, common.LogConsumeEnabled
	constant.CountToken, common.BatchUpdateEnabled, common.LogConsumeEnabled = true, false, false
	oldTimeout, oldRetries := constant.StreamingTimeout, common.RetryTimes
	constant.StreamingTimeout, common.RetryTimes = 10, 0
	oldRatio, oldCompletion := ratio_setting.ModelRatio2JSONString(), ratio_setting.CompletionRatio2JSONString()
	t.Cleanup(func() {
		constant.CountToken, common.BatchUpdateEnabled, common.LogConsumeEnabled = oldCount, oldBatch, oldLog
		constant.StreamingTimeout, common.RetryTimes = oldTimeout, oldRetries
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldRatio))
		require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(oldCompletion))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"synthetic-native":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"synthetic-native":1}`))
	service.InitTokenEncoders()
	for _, scenario := range []string{"all-reject", "success", "started", "unknown", "tools", "multimodal", "over-budget", "insufficient-quota"} {
		t.Run(scenario, func(t *testing.T) {
			setup := newPoolTestSetup(t, true)
			require.NoError(t, setup.db.AutoMigrate(&model.Token{}, &model.UserSubscription{}, &model.Channel{}, &model.Log{}, &model.BillingReviewMarker{}))
			require.NoError(t, model.MigrateBillingReservations(setup.db))
			require.NoError(t, model.MigrateUpstreamPool(setup.db))
			require.NoError(t, model.MigratePoolFallback(setup.db))
			channel := model.Channel{Type: constant.ChannelTypeOpenAI, Name: "pg03-refund", BaseURL: &endpoint, OtherSettings: `{"upstream_pool":"codex-pool","pool_routing_enabled":true}`}
			require.NoError(t, setup.db.Create(&channel).Error)
			input := accountpool.FallbackPolicyInput{PoolId: "codex-pool", ModelName: "synthetic-native", MaxFallbackCount: 3, Enabled: true}
			for i := range 4 {
				account := model.UpstreamPoolAccount{Platform: "codex", ExecutorId: accountpool.ExecutorCliproxyAPI, CredentialRef: fmt.Sprintf("pg03-%c", 'A'+i)}
				require.NoError(t, setup.db.Create(&account).Error)
				input.Members = append(input.Members, accountpool.FallbackMemberInput{AccountId: account.Id, Priority: 100 - 10*i, Enabled: true})
			}
			require.Equal(t, http.StatusOK, setup.request("PUT", "/api/owner/pool/fallback-policy", "synthetic-owner-pat", input).Code)
			user := model.User{Id: 20, Username: "pg03-refund-" + scenario, Quota: 1000, Status: common.UserStatusEnabled, AffCode: "pg03-" + scenario}
			require.NoError(t, setup.db.Create(&user).Error)
			token := model.Token{UserId: user.Id, Key: "synthetic-refund-" + scenario, RemainQuota: 1000, Status: common.TokenStatusEnabled, ExpiredTime: -1}
			require.NoError(t, setup.db.Create(&token).Error)
			stream, limit := true, uint(99)
			request := &dto.OpenAIResponsesRequest{Model: "synthetic-native", Stream: &stream, MaxOutputTokens: &limit, Input: []byte(`"synthetic"`)}
			switch scenario {
			case "tools":
				request.Tools = []byte(`[{"type":"web_search"}]`)
			case "multimodal":
				request.Input = []byte(`[{"role":"user","content":[{"type":"input_image","image_url":"synthetic"}]}]`)
			case "over-budget":
				limit = 1 << 31
			case "insufficient-quota":
				limit = 2000
			}
			body, err := common.Marshal(request)
			require.NoError(t, err)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			common.SetContextKey(c, constant.ContextKeyChannelId, channel.Id)
			common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, endpoint)
			common.SetContextKey(c, constant.ContextKeyOriginalModel, request.Model)
			info := relaycommon.GenRelayInfoResponses(c, request)
			info.UserId, info.TokenId, info.RequestId = user.Id, token.Id, "pg03-billing-"+scenario
			info.UserQuota, info.UserSetting.QuotaWarningThreshold = 1000, 1
			info.UsingGroup, info.UserGroup = "default", "default"
			info.UserSetting.BillingPreference = "wallet_only"
			apiErr := relay.PrepareRequestBilling(c, info)
			if scenario == "tools" || scenario == "multimodal" || scenario == "over-budget" || scenario == "insufficient-quota" {
				require.NotNil(t, apiErr)
				require.Nil(t, info.Billing)
				var count int64
				require.NoError(t, setup.db.Model(&model.BillingReservation{}).Where("request_id = ?", info.RequestId).Count(&count).Error)
				assert.Zero(t, count)
				var after model.User
				require.NoError(t, setup.db.First(&after, user.Id).Error)
				assert.Equal(t, 1000, after.Quota)
				t.Logf("scenario=%s rejected before reservation and Executor dispatch", scenario)
				return
			}
			require.Nil(t, apiErr)
			require.True(t, info.ProtectedTextBudget)
			require.NotNil(t, info.Billing)
			require.Contains(t, c.GetStringSlice(model.BillingExecutionContextKey), info.RequestId)
			billing := info.Billing
			var before model.BillingReservation
			require.NoError(t, setup.db.Where("request_id = ?", info.RequestId).First(&before).Error)
			require.Equal(t, info.PriceData.QuotaToPreConsume, before.Reserved)
			require.Greater(t, before.Reserved, 0)
			// Configure only the isolated provider mock. The actual request keeps
			// /v1/responses and does not need channel/header overrides.
			control, err := http.Post(endpoint+"/rc/scenario/"+scenario, "application/json", nil)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, control.StatusCode)
			require.NoError(t, control.Body.Close())
			apiErr = relay.ResponsesHelper(c, info)
			if scenario == "success" {
				require.Nil(t, apiErr)
			} else {
				require.NotNil(t, apiErr)
			}
			relay.RefundFailedRequestBilling(c, info, apiErr)
			relay.RefundFailedRequestBilling(c, info, apiErr)
			if scenario == "success" {
				billing.Refund(c)
				billing.Refund(c)
			}
			var rows []model.BillingReservation
			require.NoError(t, setup.db.Where("request_id = ?", info.RequestId).Find(&rows).Error)
			require.Len(t, rows, 1)
			var after model.User
			var afterToken model.Token
			require.NoError(t, setup.db.First(&after, user.Id).Error)
			require.NoError(t, setup.db.First(&afterToken, token.Id).Error)
			charge, state := before.Reserved, "settled"
			if scenario == "all-reject" {
				charge, state = 0, "refunded"
			}
			if scenario == "success" {
				charge = 2
			}
			assert.Equal(t, 1000-charge, after.Quota)
			assert.Equal(t, after.Quota, afterToken.RemainQuota)
			assert.Equal(t, charge, afterToken.UsedQuota)
			assert.Equal(t, charge, rows[0].Applied)
			assert.Equal(t, state, rows[0].State)
			response, err := http.Get(endpoint + "/rc/result/" + scenario)
			require.NoError(t, err)
			defer response.Body.Close()
			var evidence struct {
				Order    []string `json:"order"`
				Dispatch int      `json:"dispatch"`
			}
			require.NoError(t, common.DecodeJson(response.Body, &evidence))
			order, dispatch := []string{"pg03-A"}, 0
			if scenario == "all-reject" {
				order = []string{"pg03-A", "pg03-B", "pg03-C", "pg03-D"}
			}
			if scenario == "success" {
				order, dispatch = []string{"pg03-A", "pg03-B", "pg03-C"}, 1
			}
			if scenario == "started" {
				dispatch = 1
			}
			assert.Equal(t, order, evidence.Order)
			assert.Equal(t, dispatch, evidence.Dispatch)
			t.Logf("scenario=%s reservation=%d reserved=%d quota=1000->%d applied=%d state=%s dispatch=%d order=%v", scenario, len(rows), before.Reserved, after.Quota, rows[0].Applied, rows[0].State, evidence.Dispatch, evidence.Order)
			if scenario == "success" || scenario == "all-reject" {
				// Exercise the actual client Relay entry as well, including parsing,
				// protection admission, reservation, dispatch and its billing defer.
				clientUser := model.User{Username: "client-relay-" + scenario, Quota: 1000, Status: common.UserStatusEnabled, AffCode: "client-" + scenario}
				require.NoError(t, setup.db.Create(&clientUser).Error)
				clientToken := model.Token{UserId: clientUser.Id, Key: "synthetic-client-" + scenario, RemainQuota: 1000, Status: common.TokenStatusEnabled, ExpiredTime: -1}
				require.NoError(t, setup.db.Create(&clientToken).Error)
				recorder := httptest.NewRecorder()
				client, _ := gin.CreateTestContext(recorder)
				client.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
				client.Request.Header.Set("Content-Type", "application/json")
				requestID := "pg03-client-" + scenario
				common.SetContextKey(client, common.RequestIdKey, requestID)
				common.SetContextKey(client, constant.ContextKeyUserId, clientUser.Id)
				common.SetContextKey(client, constant.ContextKeyTokenId, clientToken.Id)
				common.SetContextKey(client, constant.ContextKeyUserQuota, 1000)
				common.SetContextKey(client, constant.ContextKeyUserGroup, "default")
				common.SetContextKey(client, constant.ContextKeyUsingGroup, "default")
				common.SetContextKey(client, constant.ContextKeyUserSetting, dto.UserSetting{BillingPreference: "wallet_only", QuotaWarningThreshold: 1})
				common.SetContextKey(client, constant.ContextKeyOriginalModel, request.Model)
				common.SetContextKey(client, constant.ContextKeyChannelId, channel.Id)
				common.SetContextKey(client, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
				common.SetContextKey(client, constant.ContextKeyChannelBaseUrl, endpoint)
				controller.Relay(client, relaytypes.RelayFormatOpenAIResponses)
				var clientRows []model.BillingReservation
				require.NoError(t, setup.db.Where("request_id = ?", requestID).Find(&clientRows).Error)
				require.Len(t, clientRows, 1)
				assert.Equal(t, state, clientRows[0].State)
				assert.Equal(t, charge, clientRows[0].Applied)
				require.NoError(t, setup.db.First(&clientUser, clientUser.Id).Error)
				assert.Equal(t, 1000-charge, clientUser.Quota)
				if scenario == "success" {
					assert.Equal(t, http.StatusOK, recorder.Code)
					assert.Contains(t, recorder.Body.String(), "response.completed")
				} else {
					assert.GreaterOrEqual(t, recorder.Code, http.StatusBadRequest)
				}
				response, err := http.Get(endpoint + "/rc/result/" + scenario)
				require.NoError(t, err)
				require.NoError(t, common.DecodeJson(response.Body, &evidence))
				require.NoError(t, response.Body.Close())
				assert.Equal(t, order, evidence.Order)
				assert.Equal(t, dispatch, evidence.Dispatch)
				t.Logf("client Relay scenario=%s reservation=1 applied=%d state=%s dispatch=%d order=%v", scenario, clientRows[0].Applied, clientRows[0].State, evidence.Dispatch, evidence.Order)
			}
		})
	}
}

type pg03WireAdaptor struct {
	relaychannel.Adaptor
	url string
}

func (a pg03WireAdaptor) GetRequestURL(*relaycommon.RelayInfo) (string, error) { return a.url, nil }
func (a pg03WireAdaptor) SetupRequestHeader(c *gin.Context, header *http.Header, _ *relaycommon.RelayInfo) error {
	*header = c.Request.Header.Clone()
	return nil
}

func TestPG03PolicySnapshotWiring(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, setup.db.AutoMigrate(&model.Channel{}))
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	require.NoError(t, model.MigratePoolFallback(setup.db))
	t.Setenv("CLEARAPI_PG03_POLICY_KEY", "synthetic-pg03-policy-key-012345678901234567890123456789")
	t.Setenv("CLEARAPI_PG02_GATEWAY_PROOF_KEY", "synthetic-pg03-proof-key-for-unit-tests-only")
	endpoint := "http://synthetic.invalid"
	channel := model.Channel{Type: 1, Name: "pg03", BaseURL: &endpoint, OtherSettings: `{"upstream_pool":"codex-pool","pool_routing_enabled":true}`}
	require.NoError(t, setup.db.Create(&channel).Error)
	input := accountpool.FallbackPolicyInput{PoolId: "codex-pool", ModelName: "synthetic-native", MaxFallbackCount: 3, Enabled: true}
	for id := 1; id <= 4; id++ {
		ref := fmt.Sprintf("pg03-%c", 'A'+id-1)
		require.NoError(t, setup.db.Create(&model.UpstreamPoolAccount{Id: id, Platform: "codex", ExecutorId: accountpool.ExecutorCliproxyAPI, CredentialRef: ref}).Error)
		input.Members = append(input.Members, accountpool.FallbackMemberInput{AccountId: id, Priority: 110 - 10*id, Enabled: true})
	}
	path := "/api/owner/pool/fallback-policy"
	require.Equal(t, http.StatusOK, setup.request("PUT", path, "synthetic-owner-pat", input).Code)
	body := `{"model":"synthetic-native","stream":true}`
	newContext := func() *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
		c.Request.Header.Set(accountpool.PG03PolicyHeader, "client-forged")
		common.SetContextKey(c, constant.ContextKeyChannelId, channel.Id)
		common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
		common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, endpoint)
		common.SetContextKey(c, constant.ContextKeyOriginalModel, input.ModelName)
		return c
	}
	prepare := func(c *gin.Context) {
		stream := true
		request := &dto.OpenAIResponsesRequest{Model: input.ModelName, Stream: &stream, Input: []byte(`"synthetic"`)}
		info := relaycommon.GenRelayInfoResponses(c, request)
		_, _, closer, apiErr := relay.PrepareResponsesRequest(c, info, request)
		require.Nil(t, apiErr)
		if closer != nil {
			require.NoError(t, closer.Close())
		}
	}
	sign := func(c *gin.Context, name string) []string {
		var req *http.Request
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			req = r.Clone(r.Context())
			data, err := io.ReadAll(r.Body)
			if err != nil || string(data) != body {
				t.Errorf("outbound body=%s err=%v", data, err)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: synthetic\n\n")
		}))
		defer server.Close()
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{HeadersOverride: map[string]any{accountpool.PG03SignatureHeader: "override-forged", accountpool.PG03PolicyHeader: "override-forged"}}}
		response, err := relaychannel.DoApiRequest(pg03WireAdaptor{url: server.URL + "/v1/responses"}, c, info, strings.NewReader(body))
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		require.NotNil(t, req)
		payload, err := base64.RawURLEncoding.DecodeString(req.Header.Get(accountpool.PG03PolicyHeader))
		require.NoError(t, err)
		var snapshot struct {
			AccountOrder []string `json:"account_order"`
			RequestID    string   `json:"request_id"`
			BodyDigest   string   `json:"body_digest"`
		}
		require.NoError(t, common.Unmarshal(payload, &snapshot))
		assert.Equal(t, snapshot.RequestID, req.Header.Get(accountpool.PG03RequestHeader))
		assert.Equal(t, snapshot.RequestID, req.Header.Get("X-ClearAPI-PG02-Request"))
		digest := sha256.Sum256([]byte(body))
		assert.Equal(t, hex.EncodeToString(digest[:]), snapshot.BodyDigest)
		mac := hmac.New(sha256.New, []byte(os.Getenv("CLEARAPI_PG03_POLICY_KEY")))
		mac.Write([]byte("clearapi-pg03-policy-v1\n"))
		mac.Write(payload)
		assert.Equal(t, hex.EncodeToString(mac.Sum(nil)), req.Header.Get(accountpool.PG03SignatureHeader))
		if directory := os.Getenv("PG03_WIRE_EVIDENCE_DIR"); directory != "" {
			wire, err := common.Marshal(struct {
				Headers http.Header `json:"headers"`
				Body    string      `json:"body"`
			}{req.Header, body})
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(directory, name+".json"), wire, 0600))
		}
		return snapshot.AccountOrder
	}
	old := newContext()
	prepare(old)
	assert.Equal(t, []string{"pg03-A", "pg03-B", "pg03-C", "pg03-D"}, sign(old, "gateway-ABCD"))
	for i := range input.Members {
		input.Members[i].Priority = 70 + 10*i
	}
	require.Equal(t, http.StatusOK, setup.request("PUT", path, "synthetic-owner-pat", input).Code)
	prepare(old)
	assert.Equal(t, []string{"pg03-A", "pg03-B", "pg03-C", "pg03-D"}, sign(old, "gateway-old-ABCD"))
	fresh := newContext()
	prepare(fresh)
	assert.Equal(t, []string{"pg03-D", "pg03-C", "pg03-B", "pg03-A"}, sign(fresh, "gateway-DCBA"))
	changed, err := http.NewRequest(http.MethodPost, endpoint+"/v1/responses", strings.NewReader(body+" "))
	require.NoError(t, err)
	require.Error(t, accountpool.ApplyPG03Policy(old, changed))
	for _, ref := range []string{"", "pg03-A"} {
		require.NoError(t, setup.db.Model(&model.UpstreamPoolAccount{}).Where("id = ?", 2).Update("credential_ref", ref).Error)
		require.Error(t, accountpool.FreezePG03Policy(newContext(), channel.Id, input.ModelName))
	}
	require.NoError(t, setup.db.Model(&model.UpstreamPoolAccount{}).Where("id = ?", 2).Update("credential_ref", "pg03-B").Error)
	// Absence is a frozen decision too: enabling a policy cannot affect an old request.
	input.Enabled = false
	require.Equal(t, http.StatusOK, setup.request("PUT", path, "synthetic-owner-pat", input).Code)
	absent := newContext()
	require.NoError(t, accountpool.FreezePG03Policy(absent, channel.Id, input.ModelName))
	input.Enabled = true
	require.Equal(t, http.StatusOK, setup.request("PUT", path, "synthetic-owner-pat", input).Code)
	require.NoError(t, accountpool.FreezePG03Policy(absent, channel.Id, input.ModelName))
	// A legacy channel retry must retain frozen absence even if its pool now has a policy.
	retryChannel := channel
	retryChannel.Id = 0
	retryChannel.Name = "pg03-retry"
	require.NoError(t, setup.db.Create(&retryChannel).Error)
	common.SetContextKey(absent, constant.ContextKeyChannelId, retryChannel.Id)
	prepare(absent)
	require.Error(t, accountpool.FreezePG03Policy(old, retryChannel.Id, input.ModelName))
	req, err := http.NewRequest(http.MethodPost, endpoint+"/v1/responses", strings.NewReader(body))
	require.NoError(t, err)
	req.Header = absent.Request.Header.Clone()
	req.Header.Set(accountpool.PG03SignatureHeader, "client-forged")
	req.Header.Set(accountpool.PG03RequestHeader, "client-forged")
	require.NoError(t, accountpool.ApplyPG03Policy(absent, req))
	assert.Empty(t, req.Header.Get(accountpool.PG03PolicyHeader))
	assert.Empty(t, req.Header.Get(accountpool.PG03SignatureHeader))
	assert.Empty(t, req.Header.Get(accountpool.PG03RequestHeader))
	data, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	assert.Equal(t, body, string(data))
}

// mockPoolExecutor implements accountpool.ExecutorClient with configurable
// outcomes; counters let tests assert the executor really was consulted.
// The credential listing is mutable so tests can simulate the executor-side
// state changing between the baseline snapshot (task start) and completion.
type mockPoolExecutor struct {
	healthErr     error
	listErr       error
	list          func() ([]accountpool.CredentialEntry, error)
	startAuthErr  error
	startAuth     func(req accountpool.StartAuthRequest) (accountpool.StartAuthResult, error)
	completeErr   error
	complete      func(req accountpool.CompleteAuthRequest) error
	authState     accountpool.AuthState
	authStatusErr error
	cancelErr     error
	deleteErr     error
	pauseErr      error
	resumeErr     error
	credStatusErr error
	credState     string
	files         []accountpool.CredentialEntry

	startCalls    int
	completeCalls int
	cancelCalls   int
	deleteCalls   []string
	pauseCalls    []string
	resumeCalls   []string
}

func (m *mockPoolExecutor) Health(context.Context) error { return m.healthErr }

func (m *mockPoolExecutor) StartAuth(_ context.Context, req accountpool.StartAuthRequest) (accountpool.StartAuthResult, error) {
	m.startCalls++
	if m.startAuthErr != nil {
		return accountpool.StartAuthResult{}, m.startAuthErr
	}
	if m.startAuth != nil {
		return m.startAuth(req)
	}
	return accountpool.StartAuthResult{
		Method:   accountpool.AuthMethodOAuthBrowser,
		AuthURL:  "https://upstream.example/authorize?state=synthetic-state",
		StateRef: "synthetic-state-" + fmt.Sprint(m.startCalls),
	}, nil
}

func (m *mockPoolExecutor) AuthStatus(context.Context, string) (accountpool.AuthState, error) {
	if m.authStatusErr != nil {
		return accountpool.AuthStateUnknown, m.authStatusErr
	}
	if m.authState == "" {
		return accountpool.AuthStateWaiting, nil
	}
	return m.authState, nil
}

func (m *mockPoolExecutor) CompleteAuth(_ context.Context, req accountpool.CompleteAuthRequest) error {
	m.completeCalls++
	if m.completeErr != nil {
		return m.completeErr
	}
	if m.complete != nil {
		return m.complete(req)
	}
	return nil
}

func (m *mockPoolExecutor) ListCredentials(context.Context) ([]accountpool.CredentialEntry, error) {
	if m.list != nil {
		return m.list()
	}
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.files, nil
}

func (m *mockPoolExecutor) CancelAuth(context.Context, string) error {
	m.cancelCalls++
	return m.cancelErr
}

func (m *mockPoolExecutor) DeleteCredential(_ context.Context, ref string) error {
	m.deleteCalls = append(m.deleteCalls, ref)
	return m.deleteErr
}

func (m *mockPoolExecutor) PauseCredential(_ context.Context, ref string) error {
	m.pauseCalls = append(m.pauseCalls, ref)
	return m.pauseErr
}

func (m *mockPoolExecutor) ResumeCredential(_ context.Context, ref string) error {
	m.resumeCalls = append(m.resumeCalls, ref)
	return m.resumeErr
}

func (m *mockPoolExecutor) CredentialStatus(context.Context, string) (accountpool.ExecutorCredentialStatus, error) {
	if m.credStatusErr != nil {
		return accountpool.ExecutorCredentialStatus{}, m.credStatusErr
	}
	if m.credState == "" {
		m.credState = accountpool.CredentialStateActive
	}
	return accountpool.ExecutorCredentialStatus{State: m.credState}, nil
}

type poolTestSetup struct {
	api      *gin.Engine
	db       *gorm.DB
	executor *mockPoolExecutor
	request  func(method, path, token string, body any) *httptest.ResponseRecorder
}

func newPoolTestSetup(t *testing.T, enabled bool) *poolTestSetup {
	t.Helper()
	db := poolFixtureDB(t)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedis, previousMemory := common.RedisEnabled, common.MemoryCacheEnabled
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled, common.MemoryCacheEnabled = false, false
	previousType := common.MainDatabaseType()
	switch os.Getenv("FINANCE_TEST_DIALECT") {
	case "mysql":
		common.SetMainDatabaseType(common.DatabaseTypeMySQL)
	case "postgres":
		common.SetMainDatabaseType(common.DatabaseTypePostgreSQL)
	default:
		common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	}
	t.Setenv("CLEARAPI_POOL_ENABLED", "true")
	if !enabled {
		t.Setenv("CLEARAPI_POOL_ENABLED", "false")
	}
	// Fixture attests executor isolation so authorization flows run; the
	// unattested-default behavior has its own test below.
	t.Setenv("CLEARAPI_POOL_CLIPROXY_DEDICATED", "true")
	common.InitPoolConfig()
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.AuditLog{}))
	for _, user := range []struct {
		id, role, status int
		token            string
	}{
		{1, common.RoleRootUser, common.UserStatusEnabled, "synthetic-owner-pat"},
		{6, common.RoleRootUser, common.UserStatusEnabled, "synthetic-other-root-pat"},
		{7, common.RoleAdminUser, common.UserStatusEnabled, "synthetic-admin-pat"},
		{8, common.RoleCommonUser, common.UserStatusEnabled, "synthetic-user-pat"},
		{9, common.RoleRootUser, common.UserStatusDisabled, "synthetic-disabled-pat"},
	} {
		token := user.token
		require.NoError(t, db.Create(&model.User{Id: user.id, Username: fmt.Sprintf("synthetic-%d", user.id), Password: "synthetic-unused", Role: user.role, Status: user.status, Quota: 1, AuthVersion: 1, AccessToken: &token, AffCode: fmt.Sprintf("synthetic-aff-%d", user.id)}).Error)
	}
	gin.SetMode(gin.TestMode)
	api := gin.New()
	router.RegisterUpstreamPoolRoutes(api.Group("/api"))
	executor := &mockPoolExecutor{}
	previousFactory := accountpool.ExecutorFactory
	accountpool.ExecutorFactory = func(id string) (accountpool.ExecutorClient, error) {
		if id == accountpool.ExecutorGrok2API {
			return accountpool.NewOfflineExecutor(), nil
		}
		return executor, nil
	}
	t.Cleanup(func() {
		accountpool.ExecutorFactory = previousFactory
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled, common.MemoryCacheEnabled = previousRedis, previousMemory
		common.SetMainDatabaseType(previousType)
	})
	setup := &poolTestSetup{api: api, db: db, executor: executor}
	setup.request = func(method, path, token string, body any) *httptest.ResponseRecorder {
		t.Helper()
		encoded, err := common.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest(method, path, bytes.NewReader(encoded))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		api.ServeHTTP(response, req)
		return response
	}
	return setup
}

// poolFixtureDB creates independent fixtures only inside explicitly isolated
// test services. Nothing points to an existing business database.
func poolFixtureDB(t *testing.T) *gorm.DB {
	t.Helper()
	dialect := os.Getenv("FINANCE_TEST_DIALECT")
	if dialect != "" && dialect != "sqlite" {
		require.Equal(t, "1", os.Getenv("POOL_ISOLATED_TEST"), "external engines require explicit isolated-test mode")
	}
	base := financeTestDB(t)
	if dialect == "" || dialect == "sqlite" {
		return base
	}
	require.Equal(t, "1", os.Getenv("POOL_ISOLATED_TEST"), "external engines require explicit isolated-test mode")
	name := fmt.Sprintf("pool_fixture_%d", time.Now().UnixNano())
	dsn := os.Getenv("FINANCE_TEST_DSN")
	var dialector gorm.Dialector
	switch dialect {
	case "mysql":
		require.Contains(t, dsn, "/pool_base?")
		require.NoError(t, base.Exec("CREATE DATABASE `"+name+"`").Error)
		dialector = mysql.Open(strings.Replace(dsn, "/pool_base?", "/"+name+"?", 1))
	case "postgres":
		require.Contains(t, dsn, "dbname=pool_base")
		require.NoError(t, base.Exec("CREATE SCHEMA "+name).Error)
		dialector = postgres.Open(dsn + " search_path=" + name)
	default:
		t.Fatal("unsupported pool test dialect")
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(4)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	return db
}

func TestPoolQuotaMigrationLegacyIdempotenceAndPersistence(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, setup.db.AutoMigrate(&model.UpstreamPoolAccount{}, &model.UpstreamPoolAuthTask{}, &model.UpstreamPoolAuthClaim{}))
	require.ErrorIs(t, model.UpstreamPoolReady(setup.db), model.ErrPoolUnavailable)
	legacy := model.UpstreamPoolAccount{Platform: "codex", AccessType: "oauth_browser", ExecutorId: accountpool.ExecutorCliproxyAPI, CredentialRef: "synthetic-legacy", Note: "keep"}
	require.NoError(t, setup.db.Create(&legacy).Error)
	for range 2 {
		require.NoError(t, model.MigrateUpstreamPool(setup.db))
	}
	require.NoError(t, model.UpstreamPoolReady(setup.db))
	var retained model.UpstreamPoolAccount
	require.NoError(t, setup.db.First(&retained, legacy.Id).Error)
	require.Equal(t, "keep", retained.Note)
	payload := `{"query_capability":"observations_only","result":"success","metrics":[{"kind":"remaining_percent","value":75,"unit":"%","scope":"shared_pool","pool":"primary","source":"upstream_derived","updated_at":1}],"next_refresh_at":0}`
	require.NoError(t, setup.db.Create(&model.UpstreamPoolQuotaSnapshot{AccountId: legacy.Id, CredentialRef: legacy.CredentialRef, Payload: payload}).Error)
	view, err := accountpool.QuotaForAccount(&retained, false)
	require.NoError(t, err)
	require.True(t, view.Stale)
	require.Equal(t, float64(75), view.Metrics[0].Value)
	response := setup.request("GET", fmt.Sprintf("/api/owner/pool/accounts/%d/quota", legacy.Id), "synthetic-owner-pat", nil)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "unsupported")
}

// STEP 4 semantics: a pool-bound channel is a normal creatable/storable
// channel; request admission is decided by the layered gate (master switch,
// channel routing opt-in, publication). With everything closed the pool stays
// unreachable and ordinary channels keep working.
func TestPoolChannelServerGatePersistenceSelectionAndEnable(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, setup.db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	endpoint := "http://synthetic-pool.invalid:8317/v1"
	t.Setenv("CLEARAPI_POOL_CLIPROXY_URL", endpoint)
	channel := model.Channel{Type: 1, Name: "synthetic-pool", Status: common.ChannelStatusManuallyDisabled, BaseURL: &endpoint, OtherSettings: `{"upstream_pool":{}}`}
	require.NoError(t, channel.Insert())
	require.NoError(t, model.BatchInsertChannels([]model.Channel{{Type: 1, Name: "synthetic-pool-batch", Status: common.ChannelStatusManuallyDisabled, BaseURL: &endpoint, OtherSettings: `{"upstream_pool":{}}`}}))
	channel.Status = common.ChannelStatusEnabled
	require.NoError(t, channel.Update())
	stripped := model.Channel{Id: channel.Id, Status: common.ChannelStatusEnabled}
	require.NoError(t, stripped.Update())
	require.NoError(t, stripped.Save())
	require.NoError(t, setup.db.Model(&model.Channel{}).Where("id = ?", channel.Id).Update("status", common.ChannelStatusManuallyDisabled).Error)
	require.True(t, model.UpdateChannelStatus(channel.Id, "", common.ChannelStatusEnabled, "manual enable"))
	var stored model.Channel
	require.NoError(t, setup.db.First(&stored, channel.Id).Error)
	require.Equal(t, common.ChannelStatusEnabled, stored.Status)
	// The pool master switch is closed on a fresh deployment: the bound
	// channel is enabled, but an unconfigured model request is still denied at
	// the gate and no executor is contacted.
	require.False(t, model.PoolMasterEnabled())
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"unregistered","tools":[{}],"messages":[{"content":[{"type":"image_url"}]}]}`))
	require.NotNil(t, middleware.SetupContextForSelectedChannel(context, &stored, "unregistered"))
	require.Equal(t, 0, setup.executor.startCalls)
	// A pool-routing claim without a pool binding is an incoherent
	// configuration and is rejected wherever it is saved.
	unbound := model.Channel{Type: 1, Name: "synthetic-unbound", OtherSettings: `{"pool_routing_enabled":true}`}
	require.ErrorIs(t, unbound.CheckPoolChannelAdmission(), model.ErrPoolRoutingRequiresBinder)
	ordinary := model.Channel{Type: 1, Name: "synthetic-ordinary", Status: common.ChannelStatusEnabled, Key: "synthetic-unused"}
	require.NoError(t, ordinary.CheckPoolChannelAdmission())
	require.NoError(t, ordinary.Save())
	ordinary.Name = "synthetic-ordinary-edited"
	require.NoError(t, ordinary.Save())
	var ordinaryStored model.Channel
	require.NoError(t, setup.db.First(&ordinaryStored, ordinary.Id).Error)
	require.Equal(t, ordinary.Name, ordinaryStored.Name)
}

// The STEP 4 control plane: master switch read/flip (fail-closed default),
// channel routing opt-in with CAS + read-back, and the availability hint that
// stays an observability projection.
func TestPoolControlPlaneMasterChannelsAndAvailability(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, setup.db.AutoMigrate(&model.Channel{}, &model.Option{}))
	endpoint := "http://synthetic-pool.invalid:8317/v1"
	t.Setenv("CLEARAPI_POOL_CLIPROXY_URL", endpoint)
	channel := model.Channel{Type: 1, Name: "synthetic-pool", Status: common.ChannelStatusEnabled, BaseURL: &endpoint, OtherSettings: `{"upstream_pool":{}}`, Models: "synthetic-pool-model"}
	require.NoError(t, setup.db.Create(&channel).Error)
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		delete(common.OptionMap, model.PoolMasterOptionKey)
		common.OptionMapRWMutex.Unlock()
		model.DB.Where("key = ?", model.PoolMasterOptionKey).Delete(&model.Option{})
		model.DB.Unscoped().Where("id = ?", channel.Id).Delete(&model.Channel{})
	})

	fetched := setup.request("GET", "/api/owner/pool/master", "synthetic-owner-pat", nil)
	require.Equal(t, http.StatusOK, fetched.Code, fetched.Body.String())
	require.Contains(t, fetched.Body.String(), `"enabled":false`)

	// A non-owner root user cannot flip the switch.
	denied := setup.request("PUT", "/api/owner/pool/master", "synthetic-other-root-pat", map[string]any{"enabled": true})
	require.Equal(t, http.StatusForbidden, denied.Code, denied.Body.String())

	opened := setup.request("PUT", "/api/owner/pool/master", "synthetic-owner-pat", map[string]any{"enabled": true})
	require.Equal(t, http.StatusOK, opened.Code, opened.Body.String())
	require.True(t, model.PoolMasterEnabled())
	// The persisted row is what a restart would load back.
	var storedOption model.Option
	require.NoError(t, model.DB.Where(&model.Option{Key: model.PoolMasterOptionKey}).First(&storedOption).Error)
	require.Equal(t, "true", storedOption.Value)

	closing := setup.request("PUT", "/api/owner/pool/master", "synthetic-owner-pat", map[string]any{"enabled": false})
	require.Equal(t, http.StatusOK, closing.Code, closing.Body.String())
	require.False(t, model.PoolMasterEnabled())

	listed := setup.request("GET", "/api/owner/pool/channels", "synthetic-owner-pat", nil)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), `"pool_routing_enabled":false`)

	enabled := setup.request("PUT", fmt.Sprintf("/api/owner/pool/channels/%d/pool-routing", channel.Id), "synthetic-owner-pat", map[string]any{"enabled": true})
	require.Equal(t, http.StatusOK, enabled.Code, enabled.Body.String())
	var stored model.Channel
	require.NoError(t, setup.db.First(&stored, channel.Id).Error)
	require.True(t, stored.PoolRoutingEnabled())
	require.Contains(t, stored.OtherSettings, `"pool_routing_enabled":true`)

	// Availability is a hint backed by the executor snapshot; the mock
	// executor has no snapshot data, so the fetcher is the injection point
	// rather than a live call.
	previousFetcher := accountpool.PoolModelSnapshotFetcher
	accountpool.PoolModelSnapshotFetcher = func(context.Context) (accountpool.PoolExecutorModelSnapshot, error) {
		return accountpool.PoolExecutorModelSnapshot{
			Revision:      4,
			TrustRevision: 4,
			Identities: []accountpool.PoolExecutorTrustIdentity{
				{AuthID: "a1", Provider: "codex", CredentialDigest: "d1", State: "trusted", AuthorizationVersion: 4, Generation: 2},
			},
			Capabilities: []accountpool.PoolExecutorCapability{
				{AuthID: "a1", Provider: "codex", CredentialDigest: "d1", Model: "synthetic-pool-model", State: "verified", Generation: 2},
			},
		}, nil
	}
	t.Cleanup(func() { accountpool.PoolModelSnapshotFetcher = previousFetcher })
	availability := setup.request("GET", "/api/owner/pool/model-availability", "synthetic-owner-pat", nil)
	require.Equal(t, http.StatusOK, availability.Code, availability.Body.String())
	require.Contains(t, availability.Body.String(), `"synthetic-pool-model":1`)
}

func ownerAccount(t *testing.T, setup *poolTestSetup, platform, accessType string) map[string]any {
	t.Helper()
	created := setup.request("POST", "/api/owner/pool/accounts", "synthetic-owner-pat", map[string]any{
		"platform": platform, "access_type": accessType, "note": "synthetic",
	})
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	var decoded struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, common.Unmarshal(created.Body.Bytes(), &decoded))
	return decoded.Data
}

func accountField(account map[string]any, key string) float64 {
	value, _ := account[key].(float64)
	return value
}

// startAuthAndComplete runs a full authorization round against the mock
// executor. listAfterStart replaces the credential listing observed during
// completion (the baseline was captured at start time).
func startAuthAndComplete(t *testing.T, setup *poolTestSetup, id int, listAfterStart []accountpool.CredentialEntry) *httptest.ResponseRecorder {
	t.Helper()
	start := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, start.Code, start.Body.String())
	setup.executor.files = listAfterStart
	return setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{
		"callback_url": "https://platform.claude.com/oauth/code/callback?code=synthetic-code-value&state=synthetic-state-1",
	})
}

func TestPoolModuleDisabledReturns404ForOwner(t *testing.T) {
	setup := newPoolTestSetup(t, false)
	for _, endpoint := range []struct{ method, path string }{
		{"GET", "/api/owner/pool/capability"},
		{"GET", "/api/owner/pool/accounts"},
		{"POST", "/api/owner/pool/accounts"},
		{"GET", "/api/owner/pool/fallback-policy"},
		{"PUT", "/api/owner/pool/fallback-policy"},
	} {
		result := setup.request(endpoint.method, endpoint.path, "synthetic-owner-pat", map[string]any{})
		assert.Equal(t, http.StatusNotFound, result.Code, endpoint.path)
	}
}

func TestPoolFallbackPolicyRoundTrip(t *testing.T) {
	t.Run("fresh_migration", func(t *testing.T) {
		db := poolFixtureDB(t)
		for range 2 {
			require.NoError(t, model.MigratePoolFallback(db))
		}
		assert.True(t, db.Migrator().HasTable(&model.PoolFallbackPolicy{}))
		assert.True(t, db.Migrator().HasTable(&model.PoolFallbackMember{}))
	})
	setup := newPoolTestSetup(t, true)
	var engineVersion string
	versionQuery := "SELECT version()"
	if setup.db.Dialector.Name() == "sqlite" {
		versionQuery = "SELECT sqlite_version()"
	}
	require.NoError(t, setup.db.Raw(versionQuery).Scan(&engineVersion).Error)
	t.Logf("database=%s version=%s", setup.db.Dialector.Name(), engineVersion)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	// Representative pre-PG03 account data must survive repeated migration.
	for id := 1; id <= 4; id++ {
		require.NoError(t, setup.db.Create(&model.UpstreamPoolAccount{Id: id, Platform: "codex", ExecutorId: accountpool.ExecutorCliproxyAPI, CredentialRef: fmt.Sprintf("synthetic-%d", id), Note: "preserve"}).Error)
	}
	require.NoError(t, model.MigratePoolFallback(setup.db))
	require.NoError(t, model.MigratePoolFallback(setup.db))
	var retained model.UpstreamPoolAccount
	require.NoError(t, setup.db.First(&retained, 1).Error)
	assert.Equal(t, "preserve", retained.Note)

	readPath := "/api/owner/pool/fallback-policy?pool_id=codex-pool&model=codex"
	missing := setup.request("GET", readPath, "synthetic-owner-pat", nil)
	require.Equal(t, http.StatusOK, missing.Code)
	assert.Contains(t, missing.Body.String(), `"data":null`)

	input := accountpool.FallbackPolicyInput{PoolId: "codex-pool", ModelName: "codex", MaxFallbackCount: 3, Enabled: true}
	for id := 1; id <= 4; id++ {
		input.Members = append(input.Members, accountpool.FallbackMemberInput{AccountId: id, Priority: 110 - 10*id, Enabled: true})
	}
	saved := setup.request("PUT", "/api/owner/pool/fallback-policy", "synthetic-owner-pat", input)
	require.Equal(t, http.StatusOK, saved.Code, saved.Body.String())
	read := setup.request("GET", readPath, "synthetic-owner-pat", nil)
	require.Equal(t, http.StatusOK, read.Code, read.Body.String())
	var result struct {
		Data model.PoolFallbackPolicy `json:"data"`
	}
	require.NoError(t, common.Unmarshal(read.Body.Bytes(), &result))
	assert.Equal(t, 3, result.Data.MaxFallbackCount)
	require.Len(t, result.Data.Members, 4)
	for i, member := range result.Data.Members {
		assert.Equal(t, i+1, member.AccountId)
	}
	policyID := result.Data.Id
	// Same-policy replacement is atomic and new reads immediately see the order.
	for i := range input.Members {
		input.Members[i].Priority = 70 + 10*i
	}
	saved = setup.request("PUT", "/api/owner/pool/fallback-policy", "synthetic-owner-pat", input)
	require.Equal(t, http.StatusOK, saved.Code, saved.Body.String())
	read = setup.request("GET", readPath, "synthetic-owner-pat", nil)
	require.NoError(t, common.Unmarshal(read.Body.Bytes(), &result))
	assert.Equal(t, policyID, result.Data.Id)
	for i, member := range result.Data.Members {
		assert.Equal(t, 4-i, member.AccountId)
	}
	// Uniqueness constraints survive migrations without discarding policy data.
	require.NoError(t, model.MigratePoolFallback(setup.db))
	require.Error(t, setup.db.Create(&model.PoolFallbackPolicy{PoolId: "codex-pool", ModelName: "codex"}).Error)
	require.Error(t, setup.db.Create(&model.PoolFallbackMember{PolicyId: policyID, AccountId: 1}).Error)
	for _, tc := range []struct {
		name   string
		mutate func(*accountpool.FallbackPolicyInput)
	}{
		{"over_limit", func(p *accountpool.FallbackPolicyInput) { p.MaxFallbackCount = 4 }},
		{"negative_limit", func(p *accountpool.FallbackPolicyInput) { p.MaxFallbackCount = -1 }},
		{"duplicate", func(p *accountpool.FallbackPolicyInput) { p.Members[1].AccountId = p.Members[0].AccountId }},
		{"unknown_account", func(p *accountpool.FallbackPolicyInput) { p.Members[0].AccountId = 999 }},
		{"wildcard", func(p *accountpool.FallbackPolicyInput) { p.ModelName = "*" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invalid := input
			invalid.Members = append([]accountpool.FallbackMemberInput(nil), input.Members...)
			tc.mutate(&invalid)
			rejected := setup.request("PUT", "/api/owner/pool/fallback-policy", "synthetic-owner-pat", invalid)
			assert.Equal(t, http.StatusBadRequest, rejected.Code, rejected.Body.String())
		})
	}
	// A member insert failure must roll back the policy update and member deletion.
	require.NoError(t, setup.db.Callback().Create().Before("gorm:create").Register("pg03_member_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "pool_fallback_member" {
			tx.AddError(fmt.Errorf("synthetic member failure"))
		}
	}))
	input.Enabled = false
	failed := setup.request("PUT", "/api/owner/pool/fallback-policy", "synthetic-owner-pat", input)
	assert.Equal(t, http.StatusInternalServerError, failed.Code)
	require.NoError(t, setup.db.Callback().Create().Remove("pg03_member_failure"))
	read = setup.request("GET", readPath, "synthetic-owner-pat", nil)
	require.NoError(t, common.Unmarshal(read.Body.Bytes(), &result))
	assert.True(t, result.Data.Enabled)
	require.Len(t, result.Data.Members, 4)
	assert.Equal(t, 4, result.Data.Members[0].AccountId)
	input.MaxFallbackCount = 0
	input.Members[0].Enabled = false
	saved = setup.request("PUT", "/api/owner/pool/fallback-policy", "synthetic-owner-pat", input)
	require.Equal(t, http.StatusOK, saved.Code, saved.Body.String())
	require.NoError(t, common.Unmarshal(saved.Body.Bytes(), &result))
	assert.False(t, result.Data.Enabled)
	assert.Zero(t, result.Data.MaxFallbackCount)
	var accounts int64
	require.NoError(t, setup.db.Model(&model.UpstreamPoolAccount{}).Count(&accounts).Error)
	assert.EqualValues(t, 4, accounts)
}

func TestPoolRequiresOwnerIdentityOnEveryEndpoint(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	endpoints := []struct{ method, path string }{
		{"GET", "/capability"}, {"GET", "/executors"}, {"GET", "/catalog"},
		{"GET", "/accounts"}, {"POST", "/accounts"},
		{"GET", "/accounts/1"}, {"GET", "/accounts/1/capabilities"},
		{"POST", "/accounts/1/auth/start"}, {"POST", "/accounts/1/auth/complete"},
		{"POST", "/accounts/1/auth/cancel"}, {"POST", "/accounts/1/pause"},
		{"POST", "/accounts/1/resume"}, {"POST", "/accounts/1/reauth"},
		{"POST", "/accounts/1/delete"}, {"POST", "/accounts/1/sync"},
		{"GET", "/accounts/1/quota"},
		{"GET", "/fallback-policy"}, {"PUT", "/fallback-policy"},
	}
	actors := []struct {
		token  string
		status int
	}{{"", 401}, {"synthetic-other-root-pat", 403}, {"synthetic-admin-pat", 403}, {"synthetic-user-pat", 403}, {"synthetic-disabled-pat", 401}}
	for _, endpoint := range endpoints {
		for _, actor := range actors {
			result := setup.request(endpoint.method, "/api/owner/pool"+endpoint.path, actor.token, map[string]any{})
			assert.Equal(t, actor.status, result.Code, "%s %s as %s", endpoint.method, endpoint.path, actor.token)
		}
	}
	// Owner identity is server-side: a forged id in the body changes nothing.
	forged := setup.request("GET", "/api/owner/pool/accounts", "synthetic-admin-pat", map[string]any{"user_id": 1})
	assert.Equal(t, http.StatusForbidden, forged.Code)
	owner := setup.request("GET", "/api/owner/pool/capability", "synthetic-owner-pat", nil)
	assert.Equal(t, http.StatusOK, owner.Code)
}

func TestPoolUnavailableBeforeMigration(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	result := setup.request("GET", "/api/owner/pool/accounts", "synthetic-owner-pat", nil)
	assert.Equal(t, http.StatusServiceUnavailable, result.Code)
	assert.Contains(t, result.Body.String(), "POOL_UNAVAILABLE")
	capability := setup.request("GET", "/api/owner/pool/capability", "synthetic-owner-pat", nil)
	assert.Equal(t, http.StatusOK, capability.Code)
	assert.Contains(t, capability.Body.String(), "false")
}

func TestPoolCatalogAndCreateValidation(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	catalog := setup.request("GET", "/api/owner/pool/catalog", "synthetic-owner-pat", nil)
	require.Equal(t, http.StatusOK, catalog.Code)
	// Disabled platforms keep their reasons visible but cannot be created.
	for _, platform := range []string{"gemini_code_assist", "qwen_web", "deepseek_web"} {
		created := setup.request("POST", "/api/owner/pool/accounts", "synthetic-owner-pat", map[string]any{
			"platform": platform, "access_type": map[string]string{
				"gemini_code_assist": "oauth_browser", "qwen_web": "browser_bridge", "deepseek_web": "cookie_import",
			}[platform],
		})
		assert.Equal(t, http.StatusBadRequest, created.Code, platform)
	}
	assert.Equal(t, http.StatusBadRequest, setup.request("POST", "/api/owner/pool/accounts", "synthetic-owner-pat", map[string]any{"platform": "unknown", "access_type": "oauth_browser"}).Code)
	assert.Equal(t, http.StatusBadRequest, setup.request("POST", "/api/owner/pool/accounts", "synthetic-owner-pat", map[string]any{"platform": "claude", "access_type": "oauth_device"}).Code)
	assert.Equal(t, http.StatusBadRequest, setup.request("POST", "/api/owner/pool/accounts", "synthetic-owner-pat", map[string]any{"platform": "claude", "access_type": "oauth_browser", "executor_id": "grok2api"}).Code)
	account := ownerAccount(t, setup, "claude", "oauth_browser")
	assert.Equal(t, "enabled", account["admin_status"])
	assert.Equal(t, "awaiting_auth", account["run_status"])
	// Grok sources are separate access types, never merged.
	for _, platform := range []string{"grok_build", "grok_web_sso", "grok_console"} {
		accessType := "oauth_device"
		if platform != "grok_build" {
			accessType = "cookie_import"
		}
		created := setup.request("POST", "/api/owner/pool/accounts", "synthetic-owner-pat", map[string]any{"platform": platform, "access_type": accessType})
		assert.Equal(t, http.StatusCreated, created.Code, platform)
	}
	// The executor listing carries the isolation attestation and actions.
	executors := setup.request("GET", "/api/owner/pool/executors", "synthetic-owner-pat", nil)
	assert.Contains(t, executors.Body.String(), "isolation_attested")
}

func TestPoolAuthorizationBlockedWithoutIsolationAttestation(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	t.Setenv("CLEARAPI_POOL_CLIPROXY_DEDICATED", "false")
	account := ownerAccount(t, setup, "claude", "oauth_browser")
	id := int(accountField(account, "id"))
	start := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{})
	assert.Equal(t, http.StatusConflict, start.Code)
	assert.Contains(t, start.Body.String(), "POOL_ISOLATION_REQUIRED")
	assert.Zero(t, setup.executor.startCalls, "no executor call may happen without the isolation attestation")
}

func TestPoolAuthorizationLifecycle(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account := ownerAccount(t, setup, "claude", "oauth_browser")
	id := int(accountField(account, "id"))

	start := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, start.Code, start.Body.String())
	assert.Contains(t, start.Body.String(), "authorize")
	var started struct {
		Data struct {
			TaskId int `json:"task_id"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(start.Body.Bytes(), &started))
	// The authorization URL is one-time output and never persisted.
	var task model.UpstreamPoolAuthTask
	require.NoError(t, setup.db.First(&task, started.Data.TaskId).Error)
	assert.Equal(t, model.PoolTaskStatusPending, task.Status)
	assert.NotContains(t, fmt.Sprintf("%v", task), "authorize")
	assert.Equal(t, 1, setup.executor.startCalls)

	// Duplicate start while a task is pending conflicts.
	assert.Equal(t, http.StatusConflict, setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{}).Code)
	// A second account cannot open a concurrent authorization on the same
	// executor: completion correlates credentials by listing diff, which is
	// only unambiguous with one active task per executor.
	other := ownerAccount(t, setup, "codex", "oauth_device")
	otherID := int(accountField(other, "id"))
	assert.Equal(t, http.StatusConflict, setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", otherID), "synthetic-owner-pat", map[string]any{}).Code)

	// Complete the still-pending task with a pasted full callback URL: the
	// callback is submitted, the session must reach the terminal completed
	// state, and the new credential is correlated by the baseline diff.
	setup.executor.complete = func(req accountpool.CompleteAuthRequest) error {
		assert.NotEmpty(t, req.StateRef, "executor session ref is submitted")
		return nil
	}
	// Malformed callback input is rejected without touching the executor.
	calls := setup.executor.completeCalls
	assert.Equal(t, http.StatusBadRequest, setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"callback_url": "not-a-url"}).Code)
	assert.Equal(t, calls, setup.executor.completeCalls)

	setup.executor.files = []accountpool.CredentialEntry{
		{Name: "synthetic-auth-file.json", Email: poolRawIdentity, UpdatedAt: 100},
	}
	completed := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{
		"callback_url": "https://platform.claude.com/oauth/code/callback?code=synthetic-code-value&state=synthetic-state-1",
	})
	require.Equal(t, http.StatusOK, completed.Code, completed.Body.String())
	var done struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, common.Unmarshal(completed.Body.Bytes(), &done))
	assert.Equal(t, "not_admitted", done.Data["run_status"])
	assert.Equal(t, "enabled", done.Data["admin_status"])
	assert.Equal(t, "s***@e***", done.Data["identity_masked"])
	assert.Equal(t, "synthetic-auth-file.json", done.Data["credential_ref"])
	assert.NotContains(t, completed.Body.String(), poolRawIdentity)
	assert.NotContains(t, completed.Body.String(), "synthetic-code-value")
	// Defense in depth: the fresh credential is parked at the executor
	// (disabled) until admission.
	assert.Equal(t, []string{"synthetic-auth-file.json"}, setup.executor.pauseCalls)

	// Duplicate callback submission conflicts.
	assert.Equal(t, http.StatusConflict, setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"callback_url": "https://x.example/cb?code=a&state=b"}).Code)

	// After completion there is no pending task anymore: further submissions
	// conflict instead of re-running the executor.
	assert.Equal(t, http.StatusConflict, setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"callback_url": "https://x.example/cb?code=a&state=b"}).Code)
}

func TestPoolCorrelationFailsClosed(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account := ownerAccount(t, setup, "claude", "oauth_browser")
	id := int(accountField(account, "id"))

	// Two new files after the baseline: ambiguous, nothing deleted, no
	// first-entry guess.
	start := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, start.Code)
	setup.executor.files = []accountpool.CredentialEntry{
		{Name: "pre-existing.json", Email: "pre@existing.test", UpdatedAt: 1},
		{Name: "new-one.json", Email: "new@one.test", UpdatedAt: 2},
		{Name: "new-two.json", Email: "new@two.test", UpdatedAt: 3},
	}
	ambiguous := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"callback_url": "https://x.example/cb?code=a&state=b"})
	assert.Equal(t, http.StatusBadGateway, ambiguous.Code)
	assert.Contains(t, ambiguous.Body.String(), "POOL_CORRELATION_FAILED")
	assert.Empty(t, setup.executor.deleteCalls, "ambiguous correlation must not delete anything")
	var after model.UpstreamPoolAccount
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Empty(t, after.CredentialRef)
	assert.Equal(t, "correlation_failed", after.LastError)

	// Zero new files: same failure mode.
	setup.executor.files = []accountpool.CredentialEntry{{Name: "pre-existing.json", Email: "pre@existing.test", UpdatedAt: 1}}
	start = setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, start.Code)
	none := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"callback_url": "https://x.example/cb?code=a&state=b"})
	assert.Equal(t, http.StatusBadGateway, none.Code)
	assert.Empty(t, setup.executor.deleteCalls)
}

func TestPoolAuthExpiryCancelAndPlatformMismatch(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account := ownerAccount(t, setup, "claude", "oauth_browser")
	id := int(accountField(account, "id"))

	// Expiry: a pending task past its deadline is marked expired, not completed.
	start := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, start.Code)
	require.NoError(t, setup.db.Model(&model.UpstreamPoolAuthTask{}).Where("account_id = ?", id).Update("expires_at", time.Now().Add(-time.Minute).Unix()).Error)
	expired := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"callback_url": "https://x.example/cb?code=a&state=b"})
	assert.Equal(t, http.StatusGone, expired.Code)
	var expiredTask model.UpstreamPoolAuthTask
	require.NoError(t, setup.db.Where("account_id = ?", id).First(&expiredTask).Error)
	assert.Equal(t, model.PoolTaskStatusExpired, expiredTask.Status)
	assert.Zero(t, setup.executor.completeCalls)
	assert.Equal(t, http.StatusConflict, setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{}).Code)

	// Separate executor fixture: an expired remote session remains unresolved.
	setup = newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account = ownerAccount(t, setup, "claude", "oauth_browser")
	id = int(accountField(account, "id"))
	// Cancel while waiting: cancellation is attempted, termination unconfirmed.
	start = setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, start.Code)
	var started struct {
		Data struct {
			TaskId int `json:"task_id"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(start.Body.Bytes(), &started))
	cancelled := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/cancel", id), "synthetic-owner-pat", map[string]any{"task_id": started.Data.TaskId})
	assert.Equal(t, http.StatusConflict, cancelled.Code)
	assert.Equal(t, 1, setup.executor.cancelCalls)
	assert.Equal(t, http.StatusConflict, setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"callback_url": "https://x.example/cb?code=a&state=b"}).Code)
	var cancelledTask model.UpstreamPoolAuthTask
	require.NoError(t, setup.db.Where("account_id = ?", id).Order("id desc").First(&cancelledTask).Error)
	assert.Equal(t, model.PoolTaskStatusFailed, cancelledTask.Status)
	assert.Equal(t, "remote_cancel_unconfirmed", cancelledTask.FailReason)
	assert.Equal(t, http.StatusConflict, setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{}).Code)

	// Cancel after the session silently completed: the correlated orphan
	// credential is deleted, the pre-existing one is untouched.
	setup = newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account = ownerAccount(t, setup, "claude", "oauth_browser")
	id = int(accountField(account, "id"))
	setup.executor.files = []accountpool.CredentialEntry{
		{Name: "kept-existing.json", Email: "kept@existing.test", UpdatedAt: 1},
	}
	start = setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, start.Code)
	require.NoError(t, common.Unmarshal(start.Body.Bytes(), &started))
	setup.executor.authState = accountpool.AuthStateCompleted
	setup.executor.files = []accountpool.CredentialEntry{
		{Name: "kept-existing.json", Email: "kept@existing.test", UpdatedAt: 1},
		{Name: "orphan-completed.json", Email: "orphan@completed.test", UpdatedAt: 2},
	}
	cancelled = setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/cancel", id), "synthetic-owner-pat", map[string]any{"task_id": started.Data.TaskId})
	assert.Equal(t, http.StatusOK, cancelled.Code)
	assert.Equal(t, []string{"orphan-completed.json"}, setup.executor.deleteCalls)

	// Platform mismatch between the pending task and the account fails closed.
	setup.executor.authState = accountpool.AuthStateWaiting
	start = setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, start.Code)
	require.NoError(t, setup.db.Model(&model.UpstreamPoolAuthTask{}).Where("account_id = ?", id).Update("platform", "codex").Error)
	mismatch := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"callback_url": "https://x.example/cb?code=a&state=b"})
	assert.Equal(t, http.StatusConflict, mismatch.Code)
	var mismatchTask model.UpstreamPoolAuthTask
	require.NoError(t, setup.db.Where("account_id = ?", id).Order("id desc").First(&mismatchTask).Error)
	assert.Equal(t, model.PoolTaskFailPlatformMismatch, mismatchTask.FailReason)
}

func TestPoolReauthIdentityMismatchKeepsOriginal(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account := ownerAccount(t, setup, "claude", "oauth_browser")
	id := int(accountField(account, "id"))

	// Initial authorization.
	completed := startAuthAndComplete(t, setup, id, []accountpool.CredentialEntry{
		{Name: "synthetic-auth-file.json", Email: poolRawIdentity, UpdatedAt: 100},
	})
	require.Equal(t, http.StatusOK, completed.Code, completed.Body.String())

	// Re-auth must reach the executor and preserve id, note and admin status.
	// The submitted identity has the SAME masked display value but a
	// different fingerprint — the fingerprint, not the display value, must
	// detect the mismatch.
	sameMaskedDifferentAccount := "secret-identity@evil-domain.test"
	require.Equal(t, accountpool.MaskIdentity(poolRawIdentity), accountpool.MaskIdentity(sameMaskedDifferentAccount))
	reauth := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/reauth", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, reauth.Code)
	setup.executor.files = []accountpool.CredentialEntry{
		{Name: "synthetic-auth-file.json", Email: poolRawIdentity, UpdatedAt: 100},
		{Name: "other-auth-file.json", Email: sameMaskedDifferentAccount, UpdatedAt: 200},
	}
	mismatch := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"code": "abc", "state": "b"})
	assert.Equal(t, http.StatusConflict, mismatch.Code)
	assert.NotContains(t, mismatch.Body.String(), sameMaskedDifferentAccount)
	var after model.UpstreamPoolAccount
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, "synthetic-auth-file.json", after.CredentialRef, "original credential must survive")
	assert.Equal(t, "s***@e***", after.IdentityMasked)
	// The task-created orphan is deleted; the original credential is not.
	assert.Equal(t, []string{"other-auth-file.json"}, setup.executor.deleteCalls)
	var failedTask model.UpstreamPoolAuthTask
	require.NoError(t, setup.db.Where("account_id = ?", id).Order("id desc").First(&failedTask).Error)
	assert.Equal(t, model.PoolTaskFailIdentityMismatch, failedTask.FailReason)
	assert.Equal(t, model.PoolAdminStatusEnabled, after.AdminStatus)

	// An in-place refresh of an existing file (same name, new timestamp)
	// cannot prove task ownership — background refresh mutates timestamps
	// too — so completion fails closed: no correlation, no deletion, the
	// original credential and its fingerprint stay intact.
	reauth = setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/reauth", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, reauth.Code)
	inPlace := len(setup.executor.deleteCalls)
	setup.executor.files = []accountpool.CredentialEntry{
		{Name: "synthetic-auth-file.json", Email: poolRawIdentity, UpdatedAt: 300},
	}
	refreshed := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"code": "abc2", "state": "b"})
	assert.Equal(t, http.StatusBadGateway, refreshed.Code)
	assert.Contains(t, refreshed.Body.String(), "POOL_CORRELATION_FAILED")
	assert.Len(t, setup.executor.deleteCalls, inPlace, "an existing file must never be deleted as an orphan")
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, id, after.Id)
	assert.Equal(t, "synthetic-auth-file.json", after.CredentialRef)
	assert.NotEmpty(t, after.IdentityFingerprint)
	assert.Equal(t, "correlation_failed", after.LastError)
	var staleTask model.UpstreamPoolAuthTask
	require.NoError(t, setup.db.Where("account_id = ?", id).Order("id desc").First(&staleTask).Error)
	assert.Equal(t, model.PoolTaskFailCorrelation, staleTask.FailReason)
	// A provably new file still correlates and re-auths the account.
	reauth = setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/reauth", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, reauth.Code)
	setup.executor.files = []accountpool.CredentialEntry{
		{Name: "synthetic-auth-file.json", Email: poolRawIdentity, UpdatedAt: 300},
		{Name: "renamed-auth-file.json", Email: poolRawIdentity, UpdatedAt: 400},
	}
	refreshed = setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"code": "abc3", "state": "b"})
	require.Equal(t, http.StatusOK, refreshed.Code, refreshed.Body.String())
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, "renamed-auth-file.json", after.CredentialRef)
	assert.Equal(t, model.PoolRunStatusNotAdmitted, after.RunStatus)
	assert.NotEmpty(t, after.IdentityFingerprint)
}

func TestPoolCredentialTakenConflictsWithoutDeleting(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	first := ownerAccount(t, setup, "claude", "oauth_browser")
	firstID := int(accountField(first, "id"))
	completed := startAuthAndComplete(t, setup, firstID, []accountpool.CredentialEntry{
		{Name: "shared-auth-file.json", Email: poolRawIdentity, UpdatedAt: 100},
	})
	require.Equal(t, http.StatusOK, completed.Code)

	// A second account completes and its correlated credential equals the
	// first account's reference. Nothing may be deleted: the file could be
	// the other account's credential.
	second := ownerAccount(t, setup, "claude", "oauth_browser")
	secondID := int(accountField(second, "id"))
	setup.executor.files = nil
	start := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", secondID), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, start.Code)
	setup.executor.files = []accountpool.CredentialEntry{
		{Name: "shared-auth-file.json", Email: poolRawIdentity, UpdatedAt: 999},
	}
	taken := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", secondID), "synthetic-owner-pat", map[string]any{"callback_url": "https://x.example/cb?code=a&state=b"})
	assert.Equal(t, http.StatusConflict, taken.Code)
	assert.Empty(t, setup.executor.deleteCalls)
	var secondRow model.UpstreamPoolAccount
	require.NoError(t, setup.db.First(&secondRow, secondID).Error)
	assert.Empty(t, secondRow.CredentialRef)
	assert.Equal(t, "credential_ref_taken", secondRow.LastError)
}

func TestPoolLocalCommitFailureCleansCorrelatedOrphan(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account := ownerAccount(t, setup, "claude", "oauth_browser")
	id := int(accountField(account, "id"))
	setup.executor.files = []accountpool.CredentialEntry{
		{Name: "baseline-kept.json", Email: "baseline@kept.test", UpdatedAt: 5},
	}
	start := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, start.Code)
	setup.executor.files = []accountpool.CredentialEntry{
		{Name: "orphan-commit.json", Email: poolRawIdentity, UpdatedAt: 10},
		{Name: "baseline-kept.json", Email: "baseline@kept.test", UpdatedAt: 5},
	}
	// Fail the local account write after the executor already created the
	// credential: the response must not be a success and the correlated
	// orphan is cleaned up while no other credential is touched.
	callback := "synthetic_local_commit_failure"
	require.NoError(t, setup.db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "upstream_pool_accounts" {
			tx.AddError(fmt.Errorf("synthetic local commit failure"))
		}
	}))
	failed := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"callback_url": "https://x.example/cb?code=a&state=b"})
	require.NoError(t, setup.db.Callback().Update().Remove(callback))
	assert.NotEqual(t, http.StatusOK, failed.Code)
	assert.Equal(t, []string{"orphan-commit.json"}, setup.executor.deleteCalls, "only the task-created orphan is cleaned; the baseline file must never be touched")
	var after model.UpstreamPoolAccount
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Empty(t, after.CredentialRef)
	var task model.UpstreamPoolAuthTask
	require.NoError(t, setup.db.Where("account_id = ?", id).Order("id desc").First(&task).Error)
	assert.NotEqual(t, model.PoolTaskStatusCompleted, task.Status)
}

func TestPoolPauseResumeAndConcurrentWriters(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account := ownerAccount(t, setup, "codex", "oauth_device")
	id := int(accountField(account, "id"))
	version := int(accountField(account, "version"))

	// No credential yet: pause is a local-only flip.
	paused := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/pause", id), "synthetic-owner-pat", map[string]any{"expected_version": version})
	require.Equal(t, http.StatusOK, paused.Code, paused.Body.String())
	var after model.UpstreamPoolAccount
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, model.PoolAdminStatusPaused, after.AdminStatus)
	assert.Equal(t, model.PoolRunStatusAwaitingAuth, after.RunStatus)
	assert.Empty(t, setup.executor.pauseCalls)

	// Resume is gated on admission: phase two has no admission, so resume
	// fails explicitly, never touches the executor and never lifts the pause.
	resumed := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/resume", id), "synthetic-owner-pat", map[string]any{"expected_version": after.Version})
	assert.Equal(t, http.StatusConflict, resumed.Code)
	assert.Contains(t, resumed.Body.String(), "POOL_NOT_ADMITTED")
	assert.Empty(t, setup.executor.resumeCalls)
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, model.PoolAdminStatusPaused, after.AdminStatus)

	// With a credential, a later pause reaches the executor (remote first).
	require.NoError(t, setup.db.Model(&after).Update("credential_ref", "synthetic-auth-file.json").Error)
	require.NoError(t, setup.db.First(&after, id).Error)
	setup.executor.pauseCalls = nil
	paused = setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/pause", id), "synthetic-owner-pat", map[string]any{"expected_version": after.Version})
	require.Equal(t, http.StatusOK, paused.Code, paused.Body.String())
	assert.Equal(t, []string{"synthetic-auth-file.json"}, setup.executor.pauseCalls)

	// Remote pause failure leaves the local row untouched: no fake success.
	setup.executor.pauseErr = accountpool.ErrPoolExecutorOffline
	require.NoError(t, setup.db.First(&after, id).Error)
	require.NoError(t, setup.db.Model(&after).Update("admin_status", model.PoolAdminStatusEnabled).Error)
	require.NoError(t, setup.db.First(&after, id).Error)
	failedPause := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/pause", id), "synthetic-owner-pat", map[string]any{"expected_version": after.Version})
	assert.Equal(t, http.StatusBadGateway, failedPause.Code)
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, model.PoolAdminStatusEnabled, after.AdminStatus, "failed remote pause must not flip the local row")
	setup.executor.pauseErr = nil

	// A stale version after a successful remote flip is resolved by one
	// fresh re-apply (single authorized writer of the admin column).
	require.NoError(t, setup.db.First(&after, id).Error)
	staleVersion := after.Version - 1
	require.Greater(t, staleVersion, 0)
	stale := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/pause", id), "synthetic-owner-pat", map[string]any{"expected_version": staleVersion})
	assert.Equal(t, http.StatusOK, stale.Code, stale.Body.String())
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, model.PoolAdminStatusPaused, after.AdminStatus)

	// Two concurrent writers holding the same version: the version guard
	// serializes them; the loser re-reads and re-applies the same target
	// value once (single authorized writer of the admin column), so both
	// responses are honest and the final state is paused.
	concurrentVersion := after.Version
	pauseResults := make(chan int, 2)
	for range 2 {
		go func() {
			pauseResults <- setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/pause", id), "synthetic-owner-pat", map[string]any{"expected_version": concurrentVersion}).Code
		}()
	}
	wins := 0
	for range 2 {
		if code := <-pauseResults; code < 400 {
			wins++
		}
	}
	assert.GreaterOrEqual(t, wins, 1)
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, model.PoolAdminStatusPaused, after.AdminStatus)
}

func TestPoolAuthorizationAfterPauseKeepsPause(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account := ownerAccount(t, setup, "codex", "oauth_device")
	id := int(accountField(account, "id"))

	// Pause, then complete authorization: the pause column is untouched by
	// the authorization path and the fresh credential is parked remotely.
	paused := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/pause", id), "synthetic-owner-pat", map[string]any{"expected_version": int(accountField(account, "version"))})
	require.Equal(t, http.StatusOK, paused.Code)
	completed := startAuthAndComplete(t, setup, id, []accountpool.CredentialEntry{
		{Name: "parked-auth-file.json", Email: poolRawIdentity, UpdatedAt: 100},
	})
	require.Equal(t, http.StatusOK, completed.Code, completed.Body.String())
	var after model.UpstreamPoolAccount
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, model.PoolAdminStatusPaused, after.AdminStatus, "authorization never writes the admin column")
	assert.Equal(t, model.PoolRunStatusNotAdmitted, after.RunStatus)
	// Pausing again on the authorized account disables the credential remotely.
	setup.executor.pauseCalls = nil
	paused = setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/pause", id), "synthetic-owner-pat", map[string]any{"expected_version": after.Version})
	require.Equal(t, http.StatusOK, paused.Code)
	assert.Equal(t, []string{"parked-auth-file.json"}, setup.executor.pauseCalls)
}

func TestPoolExecutorOfflineAndFailureRetention(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account := ownerAccount(t, setup, "claude", "oauth_browser")
	id := int(accountField(account, "id"))

	// Executor unreachable at baseline listing: start fails offline and only
	// records the fixed run status.
	setup.executor.listErr = accountpool.ErrPoolExecutorOffline
	offline := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{})
	assert.Equal(t, http.StatusBadGateway, offline.Code)
	assert.Contains(t, offline.Body.String(), "POOL_EXECUTOR_OFFLINE")
	var after model.UpstreamPoolAccount
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, model.PoolRunStatusExecutorOffline, after.RunStatus)
	assert.Equal(t, model.PoolAdminStatusEnabled, after.AdminStatus, "executor failure must not change admin status")
	assert.Equal(t, "executor_offline", after.LastError)
	setup.executor.listErr = nil

	// Completion failure state is retained, not reported as success.
	require.NoError(t, setup.db.Model(&after).Update("run_status", model.PoolRunStatusAuthorizing).Error)
	require.NoError(t, setup.db.Create(&model.UpstreamPoolAuthTask{AccountId: id, OwnerUserId: 1, Platform: "claude", AccessType: "oauth_browser", Kind: "initial", Method: "oauth_browser", ExecutorId: "cliproxyapi", ExecutorStateRef: "s", BaselineRefs: "{}", Status: model.PoolTaskStatusPending, PrevRunStatus: model.PoolRunStatusAwaitingAuth, ExpiresAt: time.Now().Add(time.Hour).Unix()}).Error)
	setup.executor.completeErr = accountpool.ErrPoolExecutorFailed
	failed := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"code": "c", "state": "s"})
	assert.Equal(t, http.StatusBadGateway, failed.Code)
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.NotEqual(t, model.PoolRunStatusNotAdmitted, after.RunStatus)
	assert.Equal(t, "executor_error", after.LastError)
	assert.NotContains(t, after.LastError, "synthetic")

	// Grok2API executor is catalog-only: every operation reports offline.
	grok := setup.request("POST", "/api/owner/pool/accounts", "synthetic-owner-pat", map[string]any{"platform": "grok_build", "access_type": "oauth_device"})
	require.Equal(t, http.StatusCreated, grok.Code)
	var decoded struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, common.Unmarshal(grok.Body.Bytes(), &decoded))
	grokID := int(accountField(decoded.Data, "id"))
	grokStart := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", grokID), "synthetic-owner-pat", map[string]any{})
	assert.Equal(t, http.StatusBadGateway, grokStart.Code)
	executors := setup.request("GET", "/api/owner/pool/executors", "synthetic-owner-pat", nil)
	assert.Contains(t, executors.Body.String(), "\"connected\":false")
}

func TestPoolDeleteRequiresExecutorConfirmation(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account := ownerAccount(t, setup, "claude", "oauth_browser")
	id := int(accountField(account, "id"))

	// With a credential, deletion only happens after executor confirmation.
	require.NoError(t, setup.db.Model(&model.UpstreamPoolAccount{}).Where("id = ?", id).Update("credential_ref", "synthetic-auth-file.json").Error)
	setup.executor.deleteErr = accountpool.ErrPoolExecutorOffline
	blocked := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/delete", id), "synthetic-owner-pat", map[string]any{})
	assert.Equal(t, http.StatusBadGateway, blocked.Code)
	require.Len(t, setup.executor.deleteCalls, 1)
	var stillThere model.UpstreamPoolAccount
	require.NoError(t, setup.db.First(&stillThere, id).Error)

	setup.executor.deleteErr = nil
	setup.executor.deleteCalls = nil
	deleted := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/delete", id), "synthetic-owner-pat", map[string]any{})
	assert.Equal(t, http.StatusOK, deleted.Code)
	assert.Equal(t, []string{"synthetic-auth-file.json"}, setup.executor.deleteCalls)
	var count int64
	require.NoError(t, setup.db.Model(&model.UpstreamPoolAccount{}).Where("id = ?", id).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, setup.db.Model(&model.UpstreamPoolAuthTask{}).Where("account_id = ?", id).Count(&count).Error)
	assert.Zero(t, count)

	// Without a credential the row is removed without touching the executor.
	account = ownerAccount(t, setup, "claude", "oauth_browser")
	id = int(accountField(account, "id"))
	calls := len(setup.executor.deleteCalls)
	require.Equal(t, http.StatusOK, setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/delete", id), "synthetic-owner-pat", map[string]any{}).Code)
	assert.Equal(t, calls, len(setup.executor.deleteCalls))
}

func TestPoolSyncRunStatusPreservesAdminStatus(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account := ownerAccount(t, setup, "kimi_coding", "oauth_device")
	id := int(accountField(account, "id"))
	require.NoError(t, setup.db.Model(&model.UpstreamPoolAccount{}).Where("id = ?", id).Updates(map[string]any{"credential_ref": "kimi.json", "run_status": model.PoolRunStatusNotAdmitted, "admin_status": model.PoolAdminStatusPaused}).Error)

	// A credential parked by the admission gate maps to not_admitted, not to
	// a re-auth requirement.
	setup.executor.credState = accountpool.CredentialStateDisabled
	synced := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/sync", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, synced.Code)
	var after model.UpstreamPoolAccount
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, model.PoolRunStatusNotAdmitted, after.RunStatus)

	setup.executor.credState = accountpool.CredentialStateCooldown
	synced = setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/sync", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, synced.Code)
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, model.PoolRunStatusCooldown, after.RunStatus)
	assert.Equal(t, model.PoolAdminStatusPaused, after.AdminStatus, "sync must never lift an owner pause")

	setup.executor.credState = accountpool.CredentialStateInvalid
	synced = setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/sync", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, synced.Code)
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, model.PoolRunStatusReauthRequired, after.RunStatus)

	setup.executor.healthErr = accountpool.ErrPoolExecutorOffline
	synced = setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/sync", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, synced.Code)
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, model.PoolRunStatusExecutorOffline, after.RunStatus)
	assert.Equal(t, model.PoolAdminStatusPaused, after.AdminStatus)

	// Masked output check across a list response.
	list := setup.request("GET", "/api/owner/pool/accounts", "synthetic-owner-pat", nil)
	require.Equal(t, http.StatusOK, list.Code)
	assert.NotContains(t, list.Body.String(), poolRawIdentity)
	assert.NotContains(t, strings.ToLower(list.Body.String()), "token", "no credential fields leak")
}

func TestPoolConcurrentStartAtomicClaim(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	first := ownerAccount(t, setup, "claude", "oauth_browser")
	second := ownerAccount(t, setup, "codex", "oauth_browser")
	firstID, secondID := int(accountField(first, "id")), int(accountField(second, "id"))

	// Two accounts start authorization at the same time on the same
	// executor: the atomic claim admits exactly one, across processes too.
	results := make(chan int, 2)
	for _, id := range []int{firstID, secondID} {
		go func(accountID int) {
			results <- setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", accountID), "synthetic-owner-pat", map[string]any{}).Code
		}(id)
	}
	wins, conflicts := 0, 0
	for range 2 {
		switch code := <-results; code {
		case http.StatusOK:
			wins++
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("unexpected status %d", code)
		}
	}
	assert.Equal(t, 1, wins)
	assert.Equal(t, 1, conflicts)
	assert.Equal(t, 1, setup.executor.startCalls, "the losing request must never start a remote session")
	var pendingTasks, claims int64
	require.NoError(t, setup.db.Model(&model.UpstreamPoolAuthTask{}).Where("status = ?", model.PoolTaskStatusPending).Count(&pendingTasks).Error)
	require.NoError(t, setup.db.Model(&model.UpstreamPoolAuthClaim{}).Count(&claims).Error)
	assert.EqualValues(t, 1, pendingTasks)
	assert.EqualValues(t, 1, claims)

	// The losing account can start once the winner's claim is released.
	require.NoError(t, setup.db.Model(&model.UpstreamPoolAuthTask{}).Where("status = ?", model.PoolTaskStatusPending).Update("status", model.PoolTaskStatusCancelled).Error)
	require.NoError(t, setup.db.Where("1 = 1").Delete(&model.UpstreamPoolAuthClaim{}).Error)
	started := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", secondID), "synthetic-owner-pat", map[string]any{})
	assert.Equal(t, http.StatusOK, started.Code, started.Body.String())
}

func TestPoolClaimExpiryAndOrphanFailClosed(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account := ownerAccount(t, setup, "claude", "oauth_browser")
	other := ownerAccount(t, setup, "claude", "oauth_browser")
	id, otherID := int(accountField(account, "id")), int(accountField(other, "id"))

	// A leftover from a dead process: expired claim on a stale pending task.
	staleTask := &model.UpstreamPoolAuthTask{AccountId: id, OwnerUserId: 1, Platform: "claude", AccessType: "oauth_browser", Kind: "initial", Method: "oauth_browser", ExecutorId: "cliproxyapi", ExecutorStateRef: "stale", Status: model.PoolTaskStatusPending, ExpiresAt: time.Now().Add(-time.Hour).Unix()}
	require.NoError(t, setup.db.Create(staleTask).Error)
	require.NoError(t, setup.db.Create(&model.UpstreamPoolAuthClaim{ExecutorId: "cliproxyapi", AccountId: id, TaskId: staleTask.Id, ExpiresAt: time.Now().Add(-time.Hour).Unix()}).Error)

	// A claim orphaned by a crash after its task settled.
	require.NoError(t, setup.db.Create(&model.UpstreamPoolAuthClaim{ExecutorId: "other-executor", AccountId: id, TaskId: 999999, ExpiresAt: time.Now().Add(time.Hour).Unix()}).Error)

	started := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", otherID), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusConflict, started.Code, started.Body.String())
	assert.Zero(t, setup.executor.startCalls, "expiry does not prove the abandoned remote worker stopped")
	var stale model.UpstreamPoolAuthTask
	require.NoError(t, setup.db.First(&stale, staleTask.Id).Error)
	assert.Equal(t, model.PoolTaskStatusPending, stale.Status)
	var claims []model.UpstreamPoolAuthClaim
	require.NoError(t, setup.db.Find(&claims).Error)
	require.Len(t, claims, 2, "neither expiry nor a missing local task proves remote termination")
}

func TestPoolReservationCommittedBeforeRemoteOperations(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	first := ownerAccount(t, setup, "claude", "oauth_browser")
	second := ownerAccount(t, setup, "codex", "oauth_browser")
	firstID, secondID := int(accountField(first, "id")), int(accountField(second, "id"))
	entered, release := make(chan struct{}), make(chan struct{})
	baselineCalls := 0
	setup.executor.list = func() ([]accountpool.CredentialEntry, error) {
		baselineCalls++
		var claim model.UpstreamPoolAuthClaim
		if err := setup.db.First(&claim, "executor_id = ?", "cliproxyapi").Error; err != nil {
			return nil, err
		}
		var task model.UpstreamPoolAuthTask
		if err := setup.db.First(&task, claim.TaskId).Error; err != nil {
			return nil, err
		}
		if task.Method != "reserved" || task.AccountId != firstID {
			return nil, fmt.Errorf("reservation not committed before baseline")
		}
		return nil, nil
	}
	setup.executor.startAuth = func(accountpool.StartAuthRequest) (accountpool.StartAuthResult, error) {
		close(entered)
		<-release
		return accountpool.StartAuthResult{Method: "oauth_browser", StateRef: "synthetic-held", AuthURL: "https://example.test/auth"}, nil
	}
	finished := make(chan error, 1)
	go func() {
		_, err := accountpool.StartAuth(1, firstID, "initial")
		finished <- err
	}()
	<-entered
	_, losingErr := accountpool.StartAuth(1, secondID, "initial")
	var task model.UpstreamPoolAuthTask
	readErr := setup.db.Where("account_id = ?", firstID).First(&task).Error
	// Cancellation/completion cannot release a reservation while the remote
	// response is in flight, even if the task id is guessed by the owner.
	cancelErr := accountpool.CancelAuth(1, firstID, task.Id)
	_, completeErr := accountpool.CompleteAuth(1, firstID, accountpool.CompleteInput{})
	close(release)
	require.NoError(t, <-finished)
	require.NoError(t, readErr)
	assert.ErrorIs(t, losingErr, accountpool.ErrPoolConflict)
	assert.ErrorIs(t, cancelErr, accountpool.ErrPoolConflict)
	assert.ErrorIs(t, completeErr, accountpool.ErrPoolConflict)
	assert.Equal(t, 1, baselineCalls)
	assert.Equal(t, 1, setup.executor.startCalls)
}

func TestPoolRemoteStartLostResponseRetainsClaim(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	first := ownerAccount(t, setup, "claude", "oauth_browser")
	second := ownerAccount(t, setup, "codex", "oauth_browser")
	firstID, secondID := int(accountField(first, "id")), int(accountField(second, "id"))
	remoteActive := false
	setup.executor.startAuth = func(accountpool.StartAuthRequest) (accountpool.StartAuthResult, error) {
		remoteActive = true // upstream created a session but the response was lost
		return accountpool.StartAuthResult{}, accountpool.ErrPoolExecutorOffline
	}
	_, err := accountpool.StartAuth(1, firstID, "initial")
	require.Error(t, err)
	_, err = accountpool.StartAuth(1, secondID, "initial")
	assert.ErrorIs(t, err, accountpool.ErrPoolConflict)
	assert.True(t, remoteActive)
	assert.Equal(t, 1, setup.executor.startCalls)
	assert.Zero(t, setup.executor.cancelCalls, "an unknown state cannot be guessed")
	var task model.UpstreamPoolAuthTask
	require.NoError(t, setup.db.Where("account_id = ?", firstID).First(&task).Error)
	assert.Equal(t, "remote_start_uncertain", task.FailReason)
	assert.ErrorIs(t, accountpool.DeleteAccount(1, firstID), accountpool.ErrPoolConflict, "registry deletion cannot discard the remote reservation")
}

func TestPoolRemoteStartPersistenceFailureRetainsClaim(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account := ownerAccount(t, setup, "claude", "oauth_browser")
	id := int(accountField(account, "id"))
	const callback = "synthetic:reject_remote_state_save"
	require.NoError(t, setup.db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if fields, ok := tx.Statement.Dest.(map[string]any); ok && fields["executor_state_ref"] != nil {
			tx.AddError(fmt.Errorf("synthetic persistence failure"))
		}
	}))
	t.Cleanup(func() { setup.db.Callback().Update().Remove(callback) })
	_, err := accountpool.StartAuth(1, id, "initial")
	require.ErrorContains(t, err, "synthetic persistence failure")
	assert.Equal(t, 1, setup.executor.startCalls)
	assert.Equal(t, 1, setup.executor.cancelCalls, "known remote session gets a best effort cancel")
	_, err = accountpool.StartAuth(1, id, "initial")
	assert.ErrorIs(t, err, accountpool.ErrPoolConflict)
	assert.Equal(t, 1, setup.executor.startCalls, "failed local save cannot permit another remote start")
}

func TestPoolReservationCommitFailureNeverTouchesExecutor(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account := ownerAccount(t, setup, "claude", "oauth_browser")
	id := int(accountField(account, "id"))
	const callback = "synthetic:reject_claim"
	require.NoError(t, setup.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*model.UpstreamPoolAuthClaim); ok {
			tx.AddError(fmt.Errorf("synthetic reservation failure"))
		}
	}))
	t.Cleanup(func() { setup.db.Callback().Create().Remove(callback) })
	baselineCalls := 0
	setup.executor.list = func() ([]accountpool.CredentialEntry, error) {
		baselineCalls++
		return nil, nil
	}
	_, err := accountpool.StartAuth(1, id, "initial")
	require.ErrorContains(t, err, "synthetic reservation failure")
	assert.Zero(t, baselineCalls)
	assert.Zero(t, setup.executor.startCalls)
}

func TestPoolBaselineFailureNeverStartsRemote(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account := ownerAccount(t, setup, "claude", "oauth_browser")
	id := int(accountField(account, "id"))
	setup.executor.listErr = accountpool.ErrPoolExecutorOffline
	_, err := accountpool.StartAuth(1, id, "initial")
	require.Error(t, err)
	assert.Zero(t, setup.executor.startCalls)
	setup.executor.listErr = nil
	_, err = accountpool.StartAuth(1, id, "initial")
	require.NoError(t, err, "a baseline failure did not create a remote session")
	assert.Equal(t, 1, setup.executor.startCalls)
}

func TestPoolRemoteCompletionTimeoutAndCancelFailureRetainClaim(t *testing.T) {
	for _, operation := range []string{"complete", "cancel"} {
		t.Run(operation, func(t *testing.T) {
			setup := newPoolTestSetup(t, true)
			require.NoError(t, model.MigrateUpstreamPool(setup.db))
			account := ownerAccount(t, setup, "claude", "oauth_browser")
			id := int(accountField(account, "id"))
			started, err := accountpool.StartAuth(1, id, "initial")
			require.NoError(t, err)
			if operation == "complete" {
				setup.executor.completeErr = accountpool.ErrPoolAuthTimeout
				_, err = accountpool.CompleteAuth(1, id, accountpool.CompleteInput{Code: "synthetic-code", State: "synthetic-state"})
				assert.ErrorIs(t, err, accountpool.ErrPoolAuthTimeout)
			} else {
				setup.executor.cancelErr = accountpool.ErrPoolExecutorOffline
				err = accountpool.CancelAuth(1, id, started.TaskId)
				assert.ErrorIs(t, err, accountpool.ErrPoolExecutorOffline)
			}
			_, err = accountpool.StartAuth(1, id, "initial")
			assert.ErrorIs(t, err, accountpool.ErrPoolConflict)
			assert.Equal(t, 1, setup.executor.startCalls)
			assert.ErrorIs(t, accountpool.DeleteAccount(1, id), accountpool.ErrPoolConflict)
		})
	}
}

func TestPoolInterruptedStartAndCancelledWorkerRemainBlocked(t *testing.T) {
	for _, method := range []string{"starting", "oauth_browser"} {
		t.Run(method, func(t *testing.T) {
			setup := newPoolTestSetup(t, true)
			require.NoError(t, model.MigrateUpstreamPool(setup.db))
			account := ownerAccount(t, setup, "claude", "oauth_browser")
			id := int(accountField(account, "id"))
			// Durable state left by a process exit: a remote worker can outlive
			// the process and the local TTL. No real process or account is used.
			task := model.UpstreamPoolAuthTask{AccountId: id, ExecutorId: "cliproxyapi", Method: method, ExecutorStateRef: "synthetic-survivor", Status: "pending", ExpiresAt: time.Now().Add(-time.Hour).Unix()}
			require.NoError(t, setup.db.Create(&task).Error)
			require.NoError(t, setup.db.Create(&model.UpstreamPoolAuthClaim{ExecutorId: "cliproxyapi", AccountId: id, TaskId: task.Id, ExpiresAt: task.ExpiresAt}).Error)
			setup.executor.authState = accountpool.AuthStateWaiting
			err := accountpool.CancelAuth(1, id, task.Id)
			assert.ErrorIs(t, err, accountpool.ErrPoolConflict)
			_, err = accountpool.StartAuth(1, id, "initial")
			assert.ErrorIs(t, err, accountpool.ErrPoolConflict)
			assert.Zero(t, setup.executor.startCalls)
			if method == "oauth_browser" {
				assert.Equal(t, 1, setup.executor.cancelCalls, "remote cancellation attempted but not assumed to stop the worker")
			} else {
				assert.Zero(t, setup.executor.cancelCalls)
			}
		})
	}
}

func startedTaskID(t *testing.T, setup *poolTestSetup, accountID int) int {
	t.Helper()
	var task model.UpstreamPoolAuthTask
	require.NoError(t, setup.db.Where("account_id = ? AND status = ?", accountID, model.PoolTaskStatusPending).First(&task).Error)
	return task.Id
}

func TestPoolCancelCompleteRace(t *testing.T) {
	for round := 0; round < 6; round++ {
		setup := newPoolTestSetup(t, true)
		require.NoError(t, model.MigrateUpstreamPool(setup.db))
		account := ownerAccount(t, setup, "claude", "oauth_browser")
		id := int(accountField(account, "id"))
		setup.executor.files = []accountpool.CredentialEntry{
			{Name: "baseline-kept.json", Email: "baseline@kept.test", UpdatedAt: 5},
		}
		start := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{})
		require.Equal(t, http.StatusOK, start.Code)
		var started struct {
			Data struct {
				TaskId int `json:"task_id"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(start.Body.Bytes(), &started))
		setup.executor.files = []accountpool.CredentialEntry{
			{Name: "baseline-kept.json", Email: "baseline@kept.test", UpdatedAt: 5},
			{Name: "race-new-file.json", Email: poolRawIdentity, UpdatedAt: 6},
		}
		cancelDone := make(chan int, 1)
		completeDone := make(chan int, 1)
		go func() {
			cancelDone <- setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/cancel", id), "synthetic-owner-pat", map[string]any{"task_id": started.Data.TaskId}).Code
		}()
		go func() {
			completeDone <- setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"callback_url": "https://x.example/cb?code=a&state=b"}).Code
		}()
		cancelCode, completeCode := <-cancelDone, <-completeDone
		var after model.UpstreamPoolAccount
		require.NoError(t, setup.db.First(&after, id).Error)
		deletedBaseline := false
		for _, ref := range setup.executor.deleteCalls {
			if ref == "baseline-kept.json" {
				deletedBaseline = true
			}
		}
		assert.False(t, deletedBaseline, "the baseline credential is never deleted, in any race outcome")
		if completeCode < 400 {
			// Completion won: the credential is claimed and parked; the
			// cancel must report failure, not success, and must not delete
			// the credential the completion just claimed.
			assert.Equal(t, http.StatusConflict, cancelCode)
			assert.Equal(t, "race-new-file.json", after.CredentialRef)
			assert.Equal(t, model.PoolRunStatusNotAdmitted, after.RunStatus)
		} else {
			assert.Empty(t, after.CredentialRef)
			if len(setup.executor.deleteCalls) > 0 {
				// Proven completion lost the local commit: its new orphan is
				// cleaned without touching the baseline credential.
				assert.Equal(t, []string{"race-new-file.json"}, setup.executor.deleteCalls)
			} else {
				// Cancellation interrupted local completion without proving
				// remote worker termination. Keep the reservation and reject
				// any further authorization, rather than claiming cleanup.
				assert.Equal(t, http.StatusConflict, cancelCode)
				_, err := accountpool.StartAuth(1, id, "initial")
				assert.ErrorIs(t, err, accountpool.ErrPoolConflict)
			}
		}
	}
}

func TestPoolRefreshInterferenceDoesNotBreakCorrelation(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account := ownerAccount(t, setup, "claude", "oauth_browser")
	id := int(accountField(account, "id"))
	setup.executor.files = []accountpool.CredentialEntry{
		{Name: "old-credential.json", Email: "old@keeper.test", UpdatedAt: 100},
	}
	start := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, start.Code)
	// Background token refresh touches the old file during the task; the new
	// authorization file still correlates unambiguously.
	setup.executor.files = []accountpool.CredentialEntry{
		{Name: "old-credential.json", Email: "old@keeper.test", UpdatedAt: 999},
		{Name: "fresh-credential.json", Email: poolRawIdentity, UpdatedAt: 1000},
	}
	completed := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"callback_url": "https://x.example/cb?code=a&state=b"})
	require.Equal(t, http.StatusOK, completed.Code, completed.Body.String())
	var after model.UpstreamPoolAccount
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, "fresh-credential.json", after.CredentialRef)
	assert.Equal(t, "s***@e***", after.IdentityMasked)
	// The refreshed-but-pre-existing file is never treated as the new
	// credential nor cleaned up.
	assert.NotContains(t, setup.executor.deleteCalls, "old-credential.json")
}
