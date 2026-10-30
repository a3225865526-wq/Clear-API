package service_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/sub2api"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ---------------------------------------------------------------------------
// Full-chain hardening fixtures: ModelPriceHelper -> PreConsumeBilling ->
// ValidateOutboundBilling -> PostTextConsumeQuota against a durable wallet
// reservation in an isolated database. No real provider is contacted.
// ---------------------------------------------------------------------------

const fixtureAExpr = `len <= 272000 ? tier("upstream_1", p*0.02 + cr*0.002 + cc*0.025 + cc1h*0.025 + c*0.10) : tier("upstream_2", p*0.04 + cr*0.04 + cc*0.05 + cc1h*0.05 + c*0.15)`

const (
	h2ChannelA      = 1
	h2ChannelB      = 2
	h2ModelA        = "gpt-6-luna"
	h2ModelB        = "gpt-6.1-sol"
	h2OutboundBodyA = `{"model":"gpt-6-luna","messages":[{"role":"user","content":"hi"}],"max_tokens":30}`
	h2OutboundBodyB = `{"model":"gpt-6.1-sol","messages":[{"role":"user","content":"hi"}],"max_tokens":30}`
)

func newHardeningDB(t testing.TB) *gorm.DB {
	t.Helper()
	// Hardening only admits durable wallet sessions.
	t.Setenv("CLEARAPI_DURABLE_WALLET", "true")
	dialect, dsn := os.Getenv("TEST_BILLING_DIALECT"), os.Getenv("TEST_BILLING_DSN")
	var driver gorm.Dialector
	switch dialect {
	case "", "sqlite":
		dialect = "sqlite"
		dsn = filepath.Join(t.TempDir(), "hardening.db")
		driver = sqlite.Open(dsn)
		common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	case "mysql":
		require.NotEmpty(t, dsn)
		driver = mysql.Open(dsn)
		common.SetDatabaseTypes(common.DatabaseTypeMySQL, common.DatabaseTypeMySQL)
	case "postgres":
		require.NotEmpty(t, dsn)
		driver = postgres.Open(dsn)
		common.SetDatabaseTypes(common.DatabaseTypePostgreSQL, common.DatabaseTypePostgreSQL)
	default:
		t.Fatal("unknown isolated test dialect")
	}
	db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })

	oldDB, oldLogDB := model.DB, model.LOG_DB
	model.DB = db
	model.LOG_DB = db // consume logs land in the same isolated database
	t.Cleanup(func() { model.DB, model.LOG_DB = oldDB, oldLogDB })
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Log{}, &model.BillingCompletionOutbox{}))
	require.NoError(t, db.AutoMigrate(&model.BillingReviewMarker{}, &model.FinanceRequestEvent{}, &model.FinanceSyncEvent{}))
	require.NoError(t, model.MigrateBillingReservations(db))
	require.NoError(t, model.MigrateBillingReservations(db), "reservation migration must be idempotent")
	require.NoError(t, model.MigrateBillingAuthSnapshots(db))
	require.NoError(t, model.MigrateBillingAuthSnapshots(db), "auth snapshot migration must be idempotent")
	require.NoError(t, model.BillingAuthSnapshotsReady(db))
	return db
}

func configureTieredModel(t testing.TB, name, expr string) {
	t.Helper()
	modeJSON, _ := json.Marshal(map[string]string{name: "tiered_expr"})
	exprJSON, _ := json.Marshal(map[string]string{name: expr})
	obj := config.GlobalConfig.Get("billing_setting")
	require.NoError(t, config.UpdateConfigFromMap(obj, map[string]string{
		billing_setting.BillingModeField: string(modeJSON),
		billing_setting.BillingExprField: string(exprJSON),
	}))
	t.Cleanup(func() {
		_ = config.UpdateConfigFromMap(obj, map[string]string{
			billing_setting.BillingModeField: "{}",
			billing_setting.BillingExprField: "{}",
		})
	})
}

func configureRatioModel(t testing.TB, name string, modelRatio float64) {
	t.Helper()
	ratioJSON, _ := json.Marshal(map[string]float64{name: modelRatio})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(ratioJSON)))
}

var h2FlowCounter atomic.Int64

type hardeningUser struct {
	UserID  int
	TokenID int
	Balance int
}

func createHardeningUser(t testing.TB, db *gorm.DB, name string, balance int) hardeningUser {
	t.Helper()
	user := model.User{Username: name, AffCode: name, Quota: balance, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	token := model.Token{UserId: user.Id, Key: name + "-key", RemainQuota: balance, Status: common.TokenStatusEnabled, ExpiredTime: -1}
	require.NoError(t, db.Create(&token).Error)
	return hardeningUser{UserID: user.Id, TokenID: token.Id, Balance: balance}
}

type walletState struct {
	UserQuota   int
	TokenRemain int
}

func walletOf(t *testing.T, u hardeningUser) walletState {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.First(&user, u.UserID).Error)
	var token model.Token
	require.NoError(t, model.DB.First(&token, u.TokenID).Error)
	return walletState{UserQuota: user.Quota, TokenRemain: token.RemainQuota}
}

func reservationOf(t *testing.T, requestID string) model.BillingReservation {
	t.Helper()
	var row model.BillingReservation
	require.NoError(t, model.DB.Where("request_id = ?", requestID).First(&row).Error)
	return row
}

type hardeningFlow struct {
	RequestID     string
	Info          *relaycommon.RelayInfo
	Ctx           *gin.Context
	OutboundBody  string
	InitialPrompt int
}

// startHardeningFlow runs the real pre-dispatch billing chain:
// ModelPriceHelper -> PreConsumeBilling (durable wallet).
func startHardeningFlow(t testing.TB, u hardeningUser, channelID int, modelName, outboundBody string) *hardeningFlow {
	t.Helper()
	// Token estimation is a billing prerequisite for paid generation; other
	// tests may flip the global, so pin it for the duration of this test.
	oldCount := constant.CountToken
	constant.CountToken = true
	t.Cleanup(func() { constant.CountToken = oldCount })
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	// Unique per flow: the process-level execution registry rejects duplicate
	// request ids for the lifetime of the test binary.
	requestID := fmt.Sprintf("h2-%s-%d-%d", modelName, u.UserID, h2FlowCounter.Add(1))
	c.Set(common.RequestIdKey, requestID)
	maxTokens := uint(30)
	request := &dto.GeneralOpenAIRequest{Model: modelName, MaxTokens: &maxTokens}

	info := &relaycommon.RelayInfo{
		RequestId:       requestID,
		UserId:          u.UserID,
		TokenId:         u.TokenID,
		UsingGroup:      "default",
		TokenGroup:      "default",
		OriginModelName: modelName,
		RelayMode:       relayconstant.RelayModeChatCompletions,
		RelayFormat:     types.RelayFormatOpenAI,
	}
	info.FinalRequestRelayFormat = types.RelayFormatOpenAI
	info.Request = request
	info.UserSetting.BillingPreference = "wallet_only"
	info.ChannelMeta = &relaycommon.ChannelMeta{ChannelId: channelID, UpstreamModelName: modelName}

	meta := request.GetTokenCountMeta()
	promptTokens, err := service.EstimateRequestToken(c, meta, info)
	require.NoError(t, err)
	info.SetEstimatePromptTokens(promptTokens)

	priceData, err := helper.ModelPriceHelper(c, info, promptTokens, meta)
	require.NoError(t, err)
	require.False(t, priceData.FreeModel)
	if apiErr := service.PreConsumeBilling(c, priceData.QuotaToPreConsume, info); apiErr != nil {
		t.Logf("PreConsumeBilling failed: %q type=%T code=%v", apiErr.Error(), apiErr, apiErr.GetErrorCode())
		require.NoError(t, apiErr)
	}
	require.NotNil(t, info.Billing)
	// Interrupted-usage fixtures need a known held amount independently of
	// the gateway's ordinary input estimate or optional output limit.
	require.NoError(t, info.Billing.Reserve(100))
	return &hardeningFlow{RequestID: info.RequestId, Info: info, Ctx: c, OutboundBody: outboundBody, InitialPrompt: promptTokens}
}

func (f *hardeningFlow) dispatch(t testing.TB) error {
	t.Helper()
	_, err := helper.ValidateOutboundBilling(f.Ctx, f.Info, []byte(f.OutboundBody))
	return err
}

func (f *hardeningFlow) settle(t testing.TB, usage *dto.Usage) {
	t.Helper()
	service.PostTextConsumeQuota(f.Ctx, f.Info, usage, nil)
}

func hiddenUsageFixtureA() *dto.Usage {
	return &dto.Usage{
		PromptTokens:     4396,
		CompletionTokens: 5,
		TotalTokens:      4401,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 3840,
		},
	}
}

func hiddenUsageFixtureB() *dto.Usage {
	return &dto.Usage{
		PromptTokens:     4394,
		CompletionTokens: 5,
		TotalTokens:      4399,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 3840,
		},
	}
}

// Basic reservations must not impose profile/model admission on normal usage.
func TestBasicBillingSettlement(t *testing.T) {
	db := newHardeningDB(t)
	t.Setenv("CLEARAPI_BILLING_PROTECTION", "true")
	oldRedis, oldBatch := common.RedisEnabled, common.BatchUpdateEnabled
	common.RedisEnabled, common.BatchUpdateEnabled = false, false
	t.Cleanup(func() { common.RedisEnabled, common.BatchUpdateEnabled = oldRedis, oldBatch })

	for _, tc := range []struct {
		name                                    string
		balance, tokenBalance, reserved, actual int
		review, refund                          bool
	}{
		{"actual-above-estimate", 1000, 1000, 100, 300, false, false},
		{"actual-below-estimate", 1000, 1000, 300, 100, false, false},
		{"insufficient-wallet", 200, 1000, 100, 300, true, false},
		{"insufficient-key", 1000, 200, 100, 300, true, false},
		{"storage-failure", 1000, 1000, 100, 300, true, false},
		{"insufficient-preconsume", 100, 100, 200, 0, false, false},
		{"refund", 1000, 1000, 100, 0, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := createHardeningUser(t, db, tc.name, tc.balance)
			require.NoError(t, db.Model(&model.Token{}).Where("id = ?", u.TokenID).Update("remain_quota", tc.tokenBalance).Error)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			info := &relaycommon.RelayInfo{RequestId: "basic-" + tc.name, UserId: u.UserID, TokenId: u.TokenID}
			info.UserSetting.BillingPreference = "wallet_only"
			apiErr := service.PreConsumeBilling(ctx, tc.reserved, info)
			if tc.name == "insufficient-preconsume" {
				require.NotNil(t, apiErr)
				assert.Equal(t, 403, apiErr.StatusCode)
				assert.Nil(t, info.Billing)
				assert.Equal(t, walletState{tc.balance, tc.tokenBalance}, walletOf(t, u))
				return
			}
			require.Nil(t, apiErr)
			if tc.name == "storage-failure" {
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register("basic-reserve-failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "tokens" {
						tx.AddError(fmt.Errorf("synthetic reserve failure"))
					}
				}))
			}
			if tc.refund {
				info.Billing.Refund(ctx)
				info.Billing.Refund(ctx)
			} else {
				err := info.Billing.Settle(tc.actual)
				if tc.name == "storage-failure" {
					require.ErrorContains(t, err, "synthetic reserve failure")
					require.NoError(t, db.Callback().Update().Remove("basic-reserve-failure"))
				}
				if tc.review {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
					require.NoError(t, info.Billing.Settle(tc.actual))
				}
				info.Billing.Refund(ctx)
			}
			want, state := tc.actual, "settled"
			if tc.review {
				want, state = tc.reserved, "needs_review"
			}
			if tc.refund {
				want, state = 0, "refunded"
			}
			assert.Equal(t, walletState{tc.balance - want, tc.tokenBalance - want}, walletOf(t, u))
			r := reservationOf(t, info.RequestId)
			assert.Equal(t, state, r.State)
			assert.Equal(t, tc.actual, r.Target)
			require.NoError(t, model.RecoverBillingCompletions(100))
			assert.Equal(t, walletState{tc.balance - want, tc.tokenBalance - want}, walletOf(t, u))
		})
	}
}

// A real type=59 adapter and HTTP mock feed the ordinary durable wallet path.
// This covers Gateway-side accounting only, not Sub2API's own service/DB.
func TestSub2APIMockUsageWalletAndConsumeLog(t *testing.T) {
	db := newHardeningDB(t)
	configureTieredModel(t, h2ModelA, fixtureAExpr)
	oldRedis, oldBatch, oldLog, oldQuota := common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, common.QuotaPerUnit
	common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, common.QuotaPerUnit = false, false, true, 500000
	t.Cleanup(func() {
		common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, common.QuotaPerUnit = oldRedis, oldBatch, oldLog, oldQuota
	})
	require.NoError(t, db.AutoMigrate(&model.Channel{}))
	channel := model.Channel{Id: h2ChannelA, Type: constant.ChannelTypeSub2API, Key: "internal-fixture-key"}
	require.NoError(t, db.Create(&channel).Error)
	u := createHardeningUser(t, db, "sub2-mock-wallet", 10000)
	flow := startHardeningFlow(t, u, channel.Id, h2ModelA, h2OutboundBodyA)
	flow.Info.StartTime = time.Now()
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)
		assert.Equal(t, "Bearer internal-fixture-key", r.Header.Get("Authorization"))
		_, err := io.Copy(io.Discard, r.Body)
		assert.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"fixture","model":"gpt-6-luna","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1000,"completion_tokens":20,"total_tokens":1020,"prompt_tokens_details":{"cached_tokens":200}}}`)
	}))
	t.Cleanup(upstream.Close)
	flow.Info.ChannelType = constant.ChannelTypeSub2API
	flow.Info.ChannelBaseUrl = upstream.URL
	flow.Info.ApiKey = channel.Key
	flow.Info.RequestURLPath = "/v1/chat/completions"
	flow.Ctx.Request = httptest.NewRequest(http.MethodPost, flow.Info.RequestURLPath, strings.NewReader(h2OutboundBodyA))
	flow.Ctx.Request.Header.Set("Content-Type", "application/json")
	flow.Ctx.Request.Header.Set("Authorization", "Bearer clearapi-user-fixture")
	adaptor := &sub2api.Adaptor{}
	adaptor.Init(flow.Info)
	response, err := adaptor.DoRequest(flow.Ctx, flow.Info, strings.NewReader(flow.OutboundBody))
	require.NoError(t, err)
	resp, ok := response.(*http.Response)
	require.True(t, ok)
	t.Cleanup(func() { _ = resp.Body.Close() })
	usageValue, apiErr := adaptor.DoResponse(flow.Ctx, resp, flow.Info)
	require.Nil(t, apiErr)
	usage, ok := usageValue.(*dto.Usage)
	require.True(t, ok)
	require.NotNil(t, usage)
	assert.Equal(t, 1000, usage.PromptTokens)
	assert.Equal(t, 20, usage.CompletionTokens)
	assert.Equal(t, 200, usage.PromptTokensDetails.CachedTokens)
	flow.settle(t, usage)
	// Repeated final wallet operations must not apply another charge/refund.
	require.NoError(t, flow.Info.Billing.Settle(9))
	flow.Info.Billing.Refund(flow.Ctx)
	require.NoError(t, model.RecoverBillingCompletions(100))
	assert.Equal(t, int32(1), calls.Load())
	assert.Equal(t, walletState{9991, 9991}, walletOf(t, u))
	reservation := reservationOf(t, flow.RequestID)
	assert.Equal(t, "settled", reservation.State)
	assert.Equal(t, 9, reservation.Applied) // ((1000-200)*.02 + 200*.002 + 20*.10) * .5, rounded.
	var user model.User
	require.NoError(t, db.First(&user, u.UserID).Error)
	assert.Equal(t, 9, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	require.NoError(t, db.First(&channel, channel.Id).Error)
	assert.Equal(t, int64(9), channel.UsedQuota)
	var logs []model.Log
	require.NoError(t, db.Where("request_id = ? AND type = ?", flow.RequestID, model.LogTypeConsume).Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, 9, logs[0].Quota)
	assert.Equal(t, 1000, logs[0].PromptTokens)
	assert.Equal(t, 20, logs[0].CompletionTokens)
	assert.Equal(t, h2ModelA, logs[0].ModelName)
}
