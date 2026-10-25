package service_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
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
// PrepareOutboundTextBudget -> PostTextConsumeQuota against a durable wallet
// reservation in an isolated database. No real provider is contacted.
// ---------------------------------------------------------------------------

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
	return &hardeningFlow{RequestID: info.RequestId, Info: info, Ctx: c, OutboundBody: outboundBody, InitialPrompt: promptTokens}
}

func (f *hardeningFlow) dispatch(t testing.TB) error {
	t.Helper()
	_, err := helper.PrepareOutboundTextBudget(f.Ctx, f.Info, []byte(f.OutboundBody))
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

func enforceColdPolicy(t *testing.T, extraProfile func(profile map[string]any)) {
	t.Helper()
	profileA := coldProfile(4480)
	profileB := coldProfile(4480)
	if extraProfile != nil {
		extraProfile(profileA)
		extraProfile(profileB)
	}
	setPolicy(t, operation_setting.ReserveModeEnforce, profilesJSON(t, map[string]map[string]any{
		"channel::1::model::gpt-6-luna":  profileA,
		"channel::2::model::gpt-6.1-sol": profileB,
	}))
}

// TestHardeningFixtureAFullChain is the automated regression for blocker
// fixture A: a tiny visible request whose provider reports 4396 prompt tokens
// with 3840 cached. The cold profile must reserve at least the eventual
// target before dispatch, settle at the real target and refund the unused
// reservation. The historical needs_review outcome must not reproduce.
func TestHardeningFixtureAFullChain(t *testing.T) {
	newHardeningDB(t)
	configureTieredModel(t, h2ModelA, fixtureAExpr)
	enforceColdPolicy(t, nil)
	u := createHardeningUser(t, model.DB, "fixture-a", 10000)

	flow := startHardeningFlow(t, u, h2ChannelA, h2ModelA, h2OutboundBodyA)
	initialReserved := reservationOf(t, flow.RequestID).Reserved
	require.NoError(t, flow.dispatch(t))
	row := reservationOf(t, flow.RequestID)

	require.Equal(t, "active", row.State)
	require.GreaterOrEqual(t, row.Reserved, 52, "cold reserve must reach the H1 verified floor of 52")
	require.GreaterOrEqual(t, row.Reserved, initialReserved)
	require.Equal(t, row.Reserved, row.Applied)
	t.Logf("fixture A: visible estimate=%d initial reserve=%d dispatch reserve=%d", flow.InitialPrompt, initialReserved, row.Reserved)

	// The dispatch authorization snapshot must be durable before any send.
	snapshot, err := model.GetBillingAuthSnapshot(flow.RequestID)
	require.NoError(t, err)
	assert.Equal(t, "h2-reserve-v1", snapshot.EstimatorVersion)
	assert.Equal(t, "channel::1::model::gpt-6-luna", snapshot.ProfileSource)
	assert.Equal(t, int64(4480), snapshot.HiddenInputMax)
	assert.Equal(t, row.Reserved, snapshot.ReservedQuota)

	flow.settle(t, hiddenUsageFixtureA())
	row = reservationOf(t, flow.RequestID)
	assert.Equal(t, "settled", row.State, "fixture A must settle instead of needs_review")
	assert.Equal(t, 10, row.Target, "actual tier1 pricing of 4396/3840/5 costs exactly 10 quota")
	assert.Equal(t, 10, row.Applied)

	wallet := walletOf(t, u)
	assert.Equal(t, u.Balance-10, wallet.UserQuota, "only the actual target is charged; the unused reservation is refunded")
	assert.Equal(t, u.Balance-10, wallet.TokenRemain)

	var log model.Log
	require.NoError(t, model.DB.Where("request_id = ?", flow.RequestID).First(&log).Error)
	assert.Equal(t, 10, log.Quota)
}

// TestHardeningFixtureBFullChain is the automated regression for blocker
// fixture B (4394 prompt / 3840 cached / 5 completion on gpt-6.1-sol).
func TestHardeningFixtureBFullChain(t *testing.T) {
	newHardeningDB(t)
	configureTieredModel(t, h2ModelB, fixtureBExpr)
	enforceColdPolicy(t, nil)
	u := createHardeningUser(t, model.DB, "fixture-b", 10000)

	flow := startHardeningFlow(t, u, h2ChannelB, h2ModelB, h2OutboundBodyB)
	require.NoError(t, flow.dispatch(t))
	row := reservationOf(t, flow.RequestID)
	require.Equal(t, "active", row.State)
	// V=7 (tiny body) prices 613; the H1 verified 614 assumes V=16.
	require.GreaterOrEqual(t, row.Reserved, 613, "cold reserve must cover the full hidden allowance")
	t.Logf("fixture B: visible estimate=%d dispatch reserve=%d", flow.InitialPrompt, row.Reserved)

	flow.settle(t, hiddenUsageFixtureB())
	row = reservationOf(t, flow.RequestID)
	assert.Equal(t, "settled", row.State)
	assert.Equal(t, 93, row.Target)
	assert.Equal(t, 93, row.Applied)

	wallet := walletOf(t, u)
	assert.Equal(t, u.Balance-93, wallet.UserQuota)
	assert.Equal(t, u.Balance-93, wallet.TokenRemain)

	ratio := float64(row.Reserved) / float64(row.Target)
	t.Logf("fixture B cold reserve/target ratio: reserved=%d target=%d ratio=%.2fx", row.Reserved, row.Target, ratio)
}

// TestHardeningNoHiddenBaseline keeps the provider honest: usage equals the
// visible estimate. Hardening must not explode the reservation, and the
// over-reserve ratio must stay bounded by the hidden allowance.
func TestHardeningNoHiddenBaseline(t *testing.T) {
	newHardeningDB(t)
	configureTieredModel(t, h2ModelA, fixtureAExpr)
	enforceColdPolicy(t, nil)
	u := createHardeningUser(t, model.DB, "no-hidden", 10000)

	legacyFlow := startHardeningFlow(t, u, h2ChannelA, h2ModelA, h2OutboundBodyA)
	legacyReserved := reservationOf(t, legacyFlow.RequestID).Reserved

	hardenedUser := createHardeningUser(t, model.DB, "no-hidden-hardened", 10000)
	flow := startHardeningFlow(t, hardenedUser, h2ChannelA, h2ModelA, h2OutboundBodyA)
	require.NoError(t, flow.dispatch(t))
	reserved := reservationOf(t, flow.RequestID).Reserved
	assert.GreaterOrEqual(t, reserved, legacyReserved)
	assert.LessOrEqual(t, reserved, legacyReserved+52, "no-hidden reservation grows by at most the cold hidden allowance")

	// Same visible request returning exactly the visible usage.
	usage := &dto.Usage{PromptTokens: 1000, CompletionTokens: 50, TotalTokens: 1050}
	flow.settle(t, usage)
	row := reservationOf(t, flow.RequestID)
	assert.Equal(t, "settled", row.State)
	wallet := walletOf(t, hardenedUser)
	assert.Equal(t, hardenedUser.Balance-row.Target, wallet.UserQuota)
	assert.LessOrEqual(t, row.Target, row.Reserved)
	ratio := float64(row.Reserved) / float64(row.Target)
	t.Logf("no-hidden baseline: legacy=%d hardened=%d target=%d over-reserve=%.2fx", legacyReserved, reserved, row.Target, ratio)
}

// TestHardeningFalseRejectionTable produces the designed admission curve for
// balances around the required reservation with target=100 (fixture B cold).
func TestHardeningFalseRejectionTable(t *testing.T) {
	newHardeningDB(t)
	configureTieredModel(t, h2ModelB, fixtureBExpr)
	enforceColdPolicy(t, nil)

	usage := &dto.Usage{PromptTokens: 833, CompletionTokens: 0, TotalTokens: 833}
	results := map[int]bool{}
	for _, balance := range []int{100, 110, 120, 150, 200, 500, 614, 700} {
		u := createHardeningUser(t, model.DB, fmt.Sprintf("balance-%d", balance), balance)
		flow := startHardeningFlow(t, u, h2ChannelB, h2ModelB, h2OutboundBodyB)
		err := flow.dispatch(t)
		if err != nil {
			require.ErrorContains(t, err, "quota")
			results[balance] = false
			flow.Info.Billing.Refund(flow.Ctx)
			assert.Equal(t, "refunded", reservationOf(t, flow.RequestID).State)
			continue
		}
		results[balance] = true
		flow.settle(t, usage)
		row := reservationOf(t, flow.RequestID)
		assert.Equal(t, "settled", row.State)
		assert.Equal(t, balance-100, walletOf(t, u).UserQuota)
	}
	t.Logf("false rejection table (target=100, cold reserve ~613): %v", results)
	assert.False(t, results[100], "balance below the required reservation is rejected before dispatch")
	assert.False(t, results[500], "balance above target but below the required reservation is rejected")
	assert.True(t, results[700], "sufficient balance is admitted")
}

// TestHardeningCapRejectionBeforeDispatch proves the authorization cap is a
// rejection gate: with a tiny cap the request is refused before any dispatch
// instead of being clamped.
func TestHardeningCapRejectionBeforeDispatch(t *testing.T) {
	newHardeningDB(t)
	configureTieredModel(t, h2ModelA, fixtureAExpr)
	enforceColdPolicy(t, func(profile map[string]any) {
		profile["max_request_quota"] = 5
	})
	u := createHardeningUser(t, model.DB, "cap-reject", 10000)

	flow := startHardeningFlow(t, u, h2ChannelA, h2ModelA, h2OutboundBodyA)
	err := flow.dispatch(t)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authorization cap")
	row := reservationOf(t, flow.RequestID)
	assert.Equal(t, "active", row.State)
	assert.Equal(t, row.Reserved, row.Applied)
}

// TestHardeningQuarantineBeforeDispatch proves unknown channel/model pairs
// are rejected before dispatch in enforce mode.
func TestHardeningQuarantineBeforeDispatch(t *testing.T) {
	newHardeningDB(t)
	configureTieredModel(t, h2ModelA, fixtureAExpr)
	setPolicy(t, operation_setting.ReserveModeEnforce, "{}")
	u := createHardeningUser(t, model.DB, "quarantine", 10000)

	flow := startHardeningFlow(t, u, h2ChannelA, h2ModelA, h2OutboundBodyA)
	err := flow.dispatch(t)
	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrReserveQuarantine)
	row := reservationOf(t, flow.RequestID)
	assert.Equal(t, "active", row.State)
	assert.Equal(t, row.Reserved, row.Applied)
}

// TestHardeningHistoricalNegativeControl re-runs the historical failure with
// the feature gate off: the visible-only reserve of 4 cannot cover target 10
// and the request must still land in needs_review, never auto-extended.
func TestHardeningHistoricalNegativeControl(t *testing.T) {
	newHardeningDB(t)
	configureTieredModel(t, h2ModelA, fixtureAExpr)
	setPolicy(t, operation_setting.ReserveModeOff, "{}")
	u := createHardeningUser(t, model.DB, "negative-control", 10000)

	flow := startHardeningFlow(t, u, h2ChannelA, h2ModelA, h2OutboundBodyA)
	require.NoError(t, flow.dispatch(t))
	row := reservationOf(t, flow.RequestID)
	require.Equal(t, 4, row.Reserved, "legacy all-branch visible reserve must rebuild the historical 4")

	flow.settle(t, hiddenUsageFixtureA())
	row = reservationOf(t, flow.RequestID)
	assert.Equal(t, "needs_review", row.State)
	assert.Equal(t, 10, row.Target)
	assert.Equal(t, 4, row.Applied)
	wallet := walletOf(t, u)
	assert.Equal(t, u.Balance-4, wallet.UserQuota)
}

// TestHardeningAnomalyRetainsReservation: hidden usage beyond the approved
// domain is an anomaly. While the target fits the reservation, the
// reservation is retained for review and parked out of normal income.
func TestHardeningAnomalyRetainsReservation(t *testing.T) {
	newHardeningDB(t)
	configureTieredModel(t, h2ModelA, fixtureAExpr)
	enforceColdPolicy(t, nil)
	u := createHardeningUser(t, model.DB, "anomaly", 10000)

	flow := startHardeningFlow(t, u, h2ChannelA, h2ModelA, h2OutboundBodyA)
	require.NoError(t, flow.dispatch(t))
	reserved := reservationOf(t, flow.RequestID).Reserved
	require.Greater(t, reserved, 0)

	// The provider reports 5000 hidden tokens on top of the visible estimate.
	usage := &dto.Usage{
		PromptTokens:     flow.InitialPrompt + 5000,
		CompletionTokens: 5,
		TotalTokens:      flow.InitialPrompt + 5005,
	}
	flow.settle(t, usage)
	row := reservationOf(t, flow.RequestID)
	assert.Equal(t, "settled", row.State)
	assert.Equal(t, reserved, row.Target, "the reservation is retained, not the observed usage")
	assert.Equal(t, reserved, row.Applied)

	var marker model.BillingReviewMarker
	require.NoError(t, model.DB.Where("reservation_id = ?", row.ID).First(&marker).Error)
	assert.Equal(t, "hidden_usage_exceeds_domain", marker.Reason)

	wallet := walletOf(t, u)
	assert.Equal(t, u.Balance-reserved, wallet.UserQuota)

	var log model.Log
	require.NoError(t, model.DB.Where("request_id = ?", flow.RequestID).First(&log).Error)
	assert.Contains(t, log.Other, "reserve_anomaly")
}

// TestHardeningMassiveUsageNeedsReview: a provider claiming 10,000,000 prompt
// tokens must fail closed into needs_review without any automatic reserve
// expansion or wallet penetration.
func TestHardeningMassiveUsageNeedsReview(t *testing.T) {
	newHardeningDB(t)
	configureTieredModel(t, h2ModelA, fixtureAExpr)
	enforceColdPolicy(t, nil)
	u := createHardeningUser(t, model.DB, "massive", 10000)

	flow := startHardeningFlow(t, u, h2ChannelA, h2ModelA, h2OutboundBodyA)
	require.NoError(t, flow.dispatch(t))
	reserved := reservationOf(t, flow.RequestID).Reserved

	usage := &dto.Usage{PromptTokens: 10000000, CompletionTokens: 5, TotalTokens: 10000005}
	flow.settle(t, usage)
	row := reservationOf(t, flow.RequestID)
	assert.Equal(t, "needs_review", row.State)
	assert.Greater(t, row.Target, reserved, "10M prompt tokens price far beyond the reservation")
	assert.Equal(t, reserved, row.Applied)
	wallet := walletOf(t, u)
	assert.Equal(t, u.Balance-reserved, wallet.UserQuota, "no charge beyond the approved reservation")
}

// TestHardeningSlightlyOverReservedNeedsReview: target = reserved + 1 stays
// needs_review at the model layer.
func TestHardeningSlightlyOverReservedNeedsReview(t *testing.T) {
	newHardeningDB(t)
	enforceColdPolicy(t, nil)
	u := createHardeningUser(t, model.DB, "plus-one", 10000)

	reserved := 40
	require.NoError(t, model.ReserveBillingRequest("h2-plus-one", u.UserID, u.TokenID, reserved))
	require.NoError(t, model.RequestBillingCompletion("h2-plus-one", reserved+1, false))
	row := reservationOf(t, "h2-plus-one")
	assert.Equal(t, "needs_review", row.State)
	assert.Equal(t, reserved+1, row.Target)
	assert.Equal(t, reserved, row.Applied)
	wallet := walletOf(t, u)
	assert.Equal(t, u.Balance-reserved, wallet.UserQuota)
}

// TestHardeningClientCancelRefundBeforeDispatch: a pre-dispatch failure
// refunds the whole reservation exactly once.
func TestHardeningClientCancelRefundBeforeDispatch(t *testing.T) {
	newHardeningDB(t)
	configureTieredModel(t, h2ModelA, fixtureAExpr)
	enforceColdPolicy(t, nil)
	u := createHardeningUser(t, model.DB, "cancel", 10000)

	flow := startHardeningFlow(t, u, h2ChannelA, h2ModelA, h2OutboundBodyA)
	require.NoError(t, flow.dispatch(t))
	flow.Info.Billing.Refund(flow.Ctx)
	row := reservationOf(t, flow.RequestID)
	assert.Equal(t, "refunded", row.State)
	wallet := walletOf(t, u)
	assert.Equal(t, u.Balance, wallet.UserQuota, "full refund restores wallet and token quota")
	assert.Equal(t, u.Balance, wallet.TokenRemain)
	flow.Info.Billing.Refund(flow.Ctx) // idempotent
	assert.Equal(t, "refunded", reservationOf(t, flow.RequestID).State)
	assert.Equal(t, u.Balance, walletOf(t, u).UserQuota)
}

// TestHardeningUnknownUsageRetained: SettleUncertain keeps the approved
// reservation with an append-only usage-unknown marker.
func TestHardeningUnknownUsageRetained(t *testing.T) {
	newHardeningDB(t)
	configureTieredModel(t, h2ModelA, fixtureAExpr)
	enforceColdPolicy(t, nil)
	u := createHardeningUser(t, model.DB, "unknown-usage", 10000)

	flow := startHardeningFlow(t, u, h2ChannelA, h2ModelA, h2OutboundBodyA)
	require.NoError(t, flow.dispatch(t))
	reserved := reservationOf(t, flow.RequestID).Reserved
	require.NoError(t, flow.Info.Billing.(*service.BillingSession).SettleUncertain())
	row := reservationOf(t, flow.RequestID)
	assert.Equal(t, "settled", row.State)
	assert.Equal(t, reserved, row.Applied)
	var marker model.BillingReviewMarker
	require.NoError(t, model.DB.Where("reservation_id = ?", row.ID).First(&marker).Error)
	assert.Equal(t, "upstream_usage_unknown", marker.Reason)
}

// TestHardeningRetryDoesNotDoubleReserve: internal retries re-run the
// outbound budget on the same session; the reservation raises to the maximum
// once instead of reserving twice.
func TestHardeningRetryDoesNotDoubleReserve(t *testing.T) {
	newHardeningDB(t)
	configureTieredModel(t, h2ModelA, fixtureAExpr)
	enforceColdPolicy(t, nil)
	u := createHardeningUser(t, model.DB, "retry", 10000)

	flow := startHardeningFlow(t, u, h2ChannelA, h2ModelA, h2OutboundBodyA)
	require.NoError(t, flow.dispatch(t))
	first := reservationOf(t, flow.RequestID)
	before := walletOf(t, u)
	require.NoError(t, flow.dispatch(t)) // retry with the same candidate
	second := reservationOf(t, flow.RequestID)
	assert.Equal(t, first.Reserved, second.Reserved)
	assert.Equal(t, before, walletOf(t, u), "an idempotent raise must not debit again")
}

// TestHardeningRatioModelRegression: legacy ratio pricing reserves, settles
// and refunds through the same hardening pipeline.
func TestHardeningRatioModelRegression(t *testing.T) {
	newHardeningDB(t)
	configureRatioModel(t, "ratio-test-model", 0.04)
	obj := config.GlobalConfig.Get("billing_setting")
	require.NoError(t, config.UpdateConfigFromMap(obj, map[string]string{billing_setting.BillingModeField: `{"ratio-test-model":"ratio"}`}))
	setPolicy(t, operation_setting.ReserveModeEnforce, profilesJSON(t, map[string]map[string]any{
		"channel::1::model::ratio-test-model": coldProfile(4480),
	}))
	t.Cleanup(func() {
		_ = config.UpdateConfigFromMap(obj, map[string]string{billing_setting.BillingModeField: "{}"})
	})
	u := createHardeningUser(t, model.DB, "ratio-regression", 10000)

	flow := startHardeningFlow(t, u, h2ChannelA, "ratio-test-model", `{"model":"ratio-test-model","messages":[{"role":"user","content":"hi"}],"max_tokens":30}`)
	require.NoError(t, flow.dispatch(t))
	row := reservationOf(t, flow.RequestID)
	require.Equal(t, "active", row.State)

	// Usage with a cache hit: reads price at the cheap cache ratio; the
	// settlement must land inside the reservation and refund the rest.
	usage := &dto.Usage{
		PromptTokens:     4000,
		CompletionTokens: 0,
		TotalTokens:      4000,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 3500,
		},
	}
	flow.settle(t, usage)
	row = reservationOf(t, flow.RequestID)
	assert.Equal(t, "settled", row.State)
	assert.Greater(t, row.Target, 0)
	assert.LessOrEqual(t, row.Target, row.Reserved, "ratio settlement must fit the reserved envelope")
	wallet := walletOf(t, u)
	assert.Equal(t, u.Balance-row.Target, wallet.UserQuota)
	t.Logf("ratio regression: reserved=%d target=%d", row.Reserved, row.Target)
}

// TestHardeningStreamSettleMatchesNonStream: the same usage delivered as a
// stream settles with the same wallet outcome; hardening adds no per-chunk
// wallet work.
func TestHardeningStreamSettleMatchesNonStream(t *testing.T) {
	newHardeningDB(t)
	configureTieredModel(t, h2ModelA, fixtureAExpr)
	enforceColdPolicy(t, nil)
	u := createHardeningUser(t, model.DB, "stream", 10000)

	flow := startHardeningFlow(t, u, h2ChannelA, h2ModelA, h2OutboundBodyA)
	flow.Info.IsStream = true
	require.NoError(t, flow.dispatch(t))
	reserved := reservationOf(t, flow.RequestID).Reserved

	// A stream delivers N chunks but performs no wallet work per chunk; only
	// the final usage observation settles.
	flow.settle(t, hiddenUsageFixtureA())

	row := reservationOf(t, flow.RequestID)
	assert.Equal(t, "settled", row.State)
	assert.Equal(t, 10, row.Target)
	assert.GreaterOrEqual(t, reserved, 52)
	wallet := walletOf(t, u)
	assert.Equal(t, u.Balance-10, wallet.UserQuota)
}

// TestHardeningInsufficientBalanceRefusesRaise: a user who cannot afford the
// required reservation is refused before dispatch; the initial reservation
// stays intact and refundable.
func TestHardeningInsufficientBalanceRefusesRaise(t *testing.T) {
	newHardeningDB(t)
	configureTieredModel(t, h2ModelB, fixtureBExpr)
	enforceColdPolicy(t, nil)
	u := createHardeningUser(t, model.DB, "insufficient-raise", 100)

	flow := startHardeningFlow(t, u, h2ChannelB, h2ModelB, h2OutboundBodyB)
	err := flow.dispatch(t)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "billing admission reservation failed")
	row := reservationOf(t, flow.RequestID)
	assert.Equal(t, "active", row.State)
	assert.Equal(t, row.Reserved, row.Applied)
	flow.Info.Billing.Refund(flow.Ctx)
	assert.Equal(t, "refunded", reservationOf(t, flow.RequestID).State)
	assert.Equal(t, u.Balance, walletOf(t, u).UserQuota)
}
