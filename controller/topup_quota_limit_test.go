package controller

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestStripeWebhookAcknowledgesOnlyCommittedCredits(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}, &model.TopUp{}, &model.SubscriptionOrder{}))
	oldDB, oldLog := model.DB, model.LOG_DB
	oldQuota := common.QuotaPerUnit
	oldRedis, oldBatch := common.RedisEnabled, common.BatchUpdateEnabled
	oldSecret, oldAPI, oldPrice := setting.StripeWebhookSecret, setting.StripeApiSecret, setting.StripePriceId
	oldPayment := *operation_setting.GetPaymentSetting()
	model.DB, model.LOG_DB = db, db
	common.QuotaPerUnit = 5
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	setting.StripeWebhookSecret = "synthetic-webhook-secret"
	setting.StripeApiSecret = "synthetic-api-setting"
	setting.StripePriceId = "synthetic-price"
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLog
		common.QuotaPerUnit = oldQuota
		common.RedisEnabled, common.BatchUpdateEnabled = oldRedis, oldBatch
		setting.StripeWebhookSecret, setting.StripeApiSecret, setting.StripePriceId = oldSecret, oldAPI, oldPrice
		*operation_setting.GetPaymentSetting() = oldPayment
	})
	require.NoError(t, db.Create(&model.User{Id: 81, Username: "synthetic-stripe-user", Status: common.UserStatusEnabled, Quota: 7}).Error)
	order := model.TopUp{UserId: 81, TradeNo: "fixture-order", Money: 10, Amount: 10, PaymentProvider: model.PaymentProviderStripe, PaymentMethod: model.PaymentMethodStripe, Status: common.TopUpStatusPending, PaymentReceipt: model.PaymentReceipt{Amount: "1000", Currency: "usd", Reference: "cs-fixture"}}
	require.NoError(t, order.Insert())
	request := func(amount int, validSignature bool) int {
		payload := fmt.Sprintf(`{"id":"evt-fixture","type":"checkout.session.completed","data":{"object":{"id":"cs-fixture","client_reference_id":"fixture-order","customer":"customer-fixture","status":"complete","payment_status":"paid","amount_subtotal":%d,"amount_total":%d,"currency":"usd"}}}`, amount, amount)
		stamp := fmt.Sprint(time.Now().Unix())
		mac := hmac.New(sha256.New, []byte("synthetic-webhook-secret"))
		_, err := mac.Write([]byte(stamp + "." + payload))
		require.NoError(t, err)
		sig := fmt.Sprintf("t=%s,v1=%x", stamp, mac.Sum(nil))
		if !validSignature {
			sig = "t=" + stamp + ",v1=invalid"
		}
		r := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(r)
		c.Request = httptest.NewRequest("POST", "/stripe/webhook", strings.NewReader(payload))
		c.Request.Header.Set("Stripe-Signature", sig)
		StripeWebhook(c)
		c.Writer.WriteHeaderNow()
		return r.Code
	}
	assert.Equal(t, 400, request(1000, false))
	assert.Equal(t, 503, request(1, true))
	for range 2 {
		assert.Equal(t, 200, request(1000, true))
	}
	var user model.User
	require.NoError(t, db.First(&user, 81).Error)
	assert.Equal(t, 57, user.Quota)
	require.NoError(t, db.Migrator().DropTable(&model.TopUp{}))
	assert.Equal(t, 503, request(1000, true), "database failure must ask the provider to retry")
}

func TestAdminCompleteTopUpRequiresComplianceBeforeDatabaseAccess(t *testing.T) {
	old := *operation_setting.GetPaymentSetting()
	operation_setting.GetPaymentSetting().ComplianceConfirmed = false
	t.Cleanup(func() { *operation_setting.GetPaymentSetting() = old })
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/api/user/topup/complete", strings.NewReader(`{"trade_no":"test"}`))
	AdminCompleteTopUp(ctx)
	assert.Contains(t, recorder.Body.String(), `"success":false`)
}

func TestTopUpQuotaValidation(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	common.QuotaPerUnit = 500000
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
	})

	testCases := []struct {
		name        string
		displayType string
		amount      int64
		wantQuota   int
		wantErr     bool
	}{
		{
			name:        "currency amount below limit",
			displayType: operation_setting.QuotaDisplayTypeUSD,
			amount:      4294,
			wantQuota:   2_147_000_000,
		},
		{
			name:        "currency amount above limit",
			displayType: operation_setting.QuotaDisplayTypeUSD,
			amount:      4295,
			wantQuota:   2_147_500_000,
		},
		{
			name:        "token amount preserves settlement truncation",
			displayType: operation_setting.QuotaDisplayTypeTokens,
			amount:      2_147_500_000,
			wantQuota:   2_147_500_000,
		},
		{
			name:        "token amount above legacy int32 range",
			displayType: operation_setting.QuotaDisplayTypeTokens,
			amount:      4_294_500_000,
			wantQuota:   4_294_500_000,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			operation_setting.GetGeneralSetting().QuotaDisplayType = tc.displayType
			quota, err := getTopUpQuota(tc.amount)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantQuota, quota)
		})
	}
}

func TestValidateTopUpQuotaReturnsMaximumAmount(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
	})

	maxAmount := decimal.NewFromInt(common.MaxWalletQuota).
		Div(decimal.NewFromFloat(common.QuotaPerUnit)).
		Floor().IntPart()

	_, err := validateTopUpQuota(maxAmount)
	require.NoError(t, err)
	_, err = validateTopUpQuota(maxAmount + 1)
	require.EqualError(t, err, fmt.Sprintf("单笔充值数量不能大于 %d", maxAmount))
}

func TestRequestAmountRejectsTopUpThatCannotBeSettled(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
	})

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	maxAmount := decimal.NewFromInt(common.MaxWalletQuota).
		Div(decimal.NewFromFloat(common.QuotaPerUnit)).
		Floor().IntPart()
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/user/amount",
		strings.NewReader(fmt.Sprintf(`{"amount":%d}`, maxAmount+1)),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	RequestAmount(ctx)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, fmt.Sprintf(`{"message":"error","data":"单笔充值数量不能大于 %d"}`, maxAmount), recorder.Body.String())
}

func TestRequestAmountRejectsTopUpThatWouldOverflowWallet(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	oldDB := model.DB
	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	model.DB = db
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
		model.DB = oldDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})

	require.NoError(t, model.DB.Create(&model.User{
		Id:       42,
		Username: "topup_capacity_user",
		Quota:    common.MaxWalletQuota - 100_000,
		Status:   common.UserStatusEnabled,
	}).Error)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 42)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/user/amount",
		strings.NewReader(`{"amount":1}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	RequestAmount(ctx)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, `{"message":"error","data":"top-up quota limit exceeded"}`, recorder.Body.String())
}

func TestValidateCreditedQuotaRejectsOverflow(t *testing.T) {
	_, err := validateCreditedQuota(decimal.NewFromInt(int64(common.MaxWalletQuota / 2)))
	require.NoError(t, err)
	_, err = validateCreditedQuota(decimal.Zero)
	require.EqualError(t, err, "充值额度必须大于 0")
	_, err = validateCreditedQuota(decimal.NewFromInt(common.MaxWalletQuota + 1))
	require.EqualError(
		t,
		err,
		"充值额度超出系统可表示范围",
	)
}

func TestStripeCreditedQuotaIncludesGroupRatio(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldTopupGroupRatio := common.TopupGroupRatio2JSONString()
	common.QuotaPerUnit = 500000
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"vip":2}`))
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(oldTopupGroupRatio))
	})

	_, err := validateCreditedQuota(getStripeCreditedQuota(2147, "vip"))
	require.NoError(t, err)
	_, err = validateCreditedQuota(getStripeCreditedQuota(2148, "vip"))
	require.NoError(t, err)
	_, err = validateCreditedQuota(getStripeCreditedQuota(int64(common.MaxWalletQuota), "vip"))
	require.Error(t, err)

	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"free":0}`))
	assert.True(t, decimal.NewFromInt(500000).Equal(getStripeCreditedQuota(1, "free")))
}
