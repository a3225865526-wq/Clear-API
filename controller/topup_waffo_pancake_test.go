package controller

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type pancakeCheckoutTransport func(*http.Request) (*http.Response, error)

func (transport pancakeCheckoutTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestFormatWaffoPancakeAmount_UsesDisplayPriceString(t *testing.T) {
	testCases := []struct {
		name     string
		amount   float64
		expected string
	}{
		{name: "whole amount", amount: 29, expected: "29.00"},
		{name: "decimal amount", amount: 29.9, expected: "29.90"},
		{name: "round half up to cents", amount: 29.999, expected: "30.00"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, formatWaffoPancakeAmount(tc.amount))
		})
	}
}

func TestWaffoPancakePaymentFacts(t *testing.T) {
	for _, tc := range []struct {
		name, amount, tax       string
		subtotal, total, status *string
		accepted                bool
	}{
		{"zero tax", "10.00", "0", nil, nil, nil, true},
		{"tax exclusive amount", "10", "2", common.GetPointer("10"), common.GetPointer("12"), common.GetPointer("succeeded"), true},
		{"tax inclusive amount", "12", "2", common.GetPointer("10"), common.GetPointer("12"), nil, true},
		{"explicit total only", "12", "2", nil, common.GetPointer("12"), nil, true},
		{"explicit subtotal only", "12", "2", common.GetPointer("10"), nil, nil, true},
		{"ambiguous tax", "12", "2", nil, nil, nil, false},
		{"inconsistent total", "12", "2", common.GetPointer("10"), common.GetPointer("13"), nil, false},
		{"unrelated amount", "1", "2", common.GetPointer("10"), common.GetPointer("12"), nil, false},
		{"tax exceeds total", "2", "3", nil, common.GetPointer("2"), nil, false},
		{"negative tax", "10", "-1", nil, nil, nil, false},
		{"empty explicit subtotal", "10", "0", common.GetPointer(""), nil, nil, false},
		{"empty explicit total", "10", "0", nil, common.GetPointer(""), nil, false},
		{"fractional cent", "10.001", "0", nil, nil, nil, false},
		{"failed payment", "10", "0", nil, nil, common.GetPointer("failed"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := &service.WaffoPancakeWebhookEvent{EventType: "order.completed", Data: service.WaffoPancakeWebhookData{OrderMerchantExternalID: "fixture-order", Currency: "USD", Amount: tc.amount, TaxAmount: tc.tax, Subtotal: tc.subtotal, Total: tc.total, PaymentStatus: tc.status}}
			receipt, err := service.WaffoPancakeWebhookReceipt(event)
			if !tc.accepted {
				require.ErrorIs(t, err, model.ErrPaymentReceiptMismatch)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, model.PaymentReceipt{Amount: "10.00", Currency: "USD", Reference: "fixture-order"}, receipt)
		})
	}
	for _, money := range []float64{math.NaN(), math.Inf(1), -1, 0, 0.009} {
		_, err := service.WaffoPancakeCheckoutReceipt(money, "fixture-order")
		require.ErrorIs(t, err, model.ErrPaymentReceiptMismatch)
	}
	receipt, err := service.WaffoPancakeCheckoutReceipt(29.999, "fixture-order")
	require.NoError(t, err)
	assert.Equal(t, "30.00", receipt.Amount)
}

func TestWaffoPancakeSignedWebhookPaymentGuard(t *testing.T) {
	var driver gorm.Dialector = sqlite.Open(":memory:")
	dbType := common.DatabaseTypeSQLite
	switch os.Getenv("TEST_BILLING_DIALECT") {
	case "mysql":
		driver, dbType = mysql.Open(os.Getenv("TEST_BILLING_DSN")), common.DatabaseTypeMySQL
	case "postgres":
		driver, dbType = postgres.Open(os.Getenv("TEST_BILLING_DSN")), common.DatabaseTypePostgreSQL
	case "", "sqlite":
	default:
		t.Fatal("unsupported isolated test dialect")
	}
	db, err := gorm.Open(driver, &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}, &model.SubscriptionOrder{}, &model.SubscriptionPlan{}, &model.UserSubscription{}))
	oldDB, oldLog := model.DB, model.LOG_DB
	oldMainType, oldLogType, oldMaster := common.MainDatabaseType(), common.LogDatabaseType(), common.IsMasterNode
	oldQuota, oldRedis, oldBatch := common.QuotaPerUnit, common.RedisEnabled, common.BatchUpdateEnabled
	oldMerchant, oldPrivate, oldProduct := setting.WaffoPancakeMerchantID, setting.WaffoPancakePrivateKey, setting.WaffoPancakeProductID
	oldPayment := *operation_setting.GetPaymentSetting()
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(dbType, dbType)
	common.IsMasterNode = false
	t.Setenv("LOG_SQL_DSN", "")
	// Initialize dialect symbols using the normal shared-log path. With the
	// master flag off it opens no connection and executes no migration.
	require.NoError(t, model.InitLogDB())
	common.QuotaPerUnit, common.RedisEnabled, common.BatchUpdateEnabled = 5, false, false
	setting.WaffoPancakeMerchantID, setting.WaffoPancakePrivateKey = "synthetic-merchant", "synthetic-unused-private-setting"
	setting.WaffoPancakeProductID = "synthetic-product"
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	t.Cleanup(func() {
		model.DB = oldDB
		common.SetDatabaseTypes(oldMainType, oldLogType)
		_ = model.InitLogDB()
		common.IsMasterNode = oldMaster
		model.LOG_DB = oldLog
		common.QuotaPerUnit, common.RedisEnabled, common.BatchUpdateEnabled = oldQuota, oldRedis, oldBatch
		setting.WaffoPancakeMerchantID, setting.WaffoPancakePrivateKey, setting.WaffoPancakeProductID = oldMerchant, oldPrivate, oldProduct
		*operation_setting.GetPaymentSetting() = oldPayment
	})
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	t.Setenv("WAFFO_WEBHOOK_TEST_PUBLIC_KEY", string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})))
	t.Setenv("WAFFO_WEBHOOK_PUBLIC_KEY", "")
	require.NoError(t, db.Create(&model.User{Id: 730, Username: "synthetic-pancake", AffCode: "synthetic-pancake-aff", Quota: 7, Status: common.UserStatusEnabled}).Error)
	order := model.TopUp{UserId: 730, TradeNo: "WAFFO_PANCAKE-fixture", Amount: 2, Money: 10, PaymentProvider: model.PaymentProviderWaffoPancake, PaymentMethod: model.PaymentMethodWaffoPancake, Status: common.TopUpStatusPending, PaymentReceipt: model.PaymentReceipt{Amount: "10", Currency: "USD", Reference: "WAFFO_PANCAKE-fixture"}}
	require.NoError(t, order.Insert())
	oldAnonymousLimit := constant.AnonymousRequestBodyLimitKB
	constant.AnonymousRequestBodyLimitKB = 512
	t.Cleanup(func() { constant.AnonymousRequestBodyLimitKB = oldAnonymousLimit })
	webhookRouter := gin.New()
	webhookRouter.Use(middleware.APIRequestBodyLimit(), middleware.AnonymousRequestBodyLimit())
	webhookRouter.POST("/api/waffo-pancake/webhook/:env", WaffoPancakeWebhook)
	request := func(tradeNo, amount, currency, identity, mode string, validSignature bool) int {
		payload, err := common.Marshal(map[string]any{"id": "synthetic-event", "eventType": "order.completed", "mode": mode, "data": map[string]any{"orderId": "ORD_synthetic", "orderMerchantExternalId": tradeNo, "merchantProvidedBuyerIdentity": identity, "amount": amount, "taxAmount": "2", "subtotal": "10", "total": "12", "currency": currency, "paymentStatus": "succeeded"}})
		require.NoError(t, err)
		stamp := fmt.Sprint(time.Now().UnixMilli())
		hash := sha256.Sum256(append([]byte(stamp+"."), payload...))
		sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
		require.NoError(t, err)
		header := "t=" + stamp + ",v1=" + base64.StdEncoding.EncodeToString(sig)
		if !validSignature {
			header = "t=" + stamp + ",v1=invalid"
		}
		r := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/waffo-pancake/webhook/test", strings.NewReader(string(payload)))
		req.Header.Set("X-Waffo-Signature", header)
		webhookRouter.ServeHTTP(r, req)
		return r.Code
	}
	identity := service.WaffoPancakeBuyerIdentityFromUserID(730)
	assert.Equal(t, 401, request(order.TradeNo, "12", "USD", identity, "test", false))
	for _, tc := range []struct{ amount, currency, identity string }{{"1", "USD", identity}, {"12", "EUR", identity}, {"12", "USD", "another-buyer"}} {
		assert.Equal(t, 200, request(order.TradeNo, tc.amount, tc.currency, tc.identity, "test", true))
	}
	var user model.User
	require.NoError(t, db.First(&user, 730).Error)
	assert.Equal(t, 7, user.Quota)
	assert.Equal(t, common.TopUpStatusPending, model.GetTopUpByTradeNo(order.TradeNo).Status)
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("pancake_lookup_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "top_ups" {
			tx.AddError(errors.New("synthetic lookup failure"))
		}
	}))
	assert.Equal(t, 500, request(order.TradeNo, "12", "USD", identity, "test", true))
	require.NoError(t, db.Callback().Query().Remove("pancake_lookup_failure"))
	require.NoError(t, db.Model(&order).Update("payment_amount", "20").Error)
	assert.Equal(t, 200, request(order.TradeNo, "12", "USD", identity, "test", true))
	assert.Equal(t, common.TopUpStatusPending, model.GetTopUpByTradeNo(order.TradeNo).Status)
	require.NoError(t, db.Model(&order).Update("payment_amount", "10").Error)
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("pancake_callback_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			tx.AddError(errors.New("synthetic write failure"))
		}
	}))
	assert.Equal(t, 500, request(order.TradeNo, "12", "USD", identity, "test", true))
	require.NoError(t, db.Callback().Update().Remove("pancake_callback_failure"))
	assert.Equal(t, common.TopUpStatusPending, model.GetTopUpByTradeNo(order.TradeNo).Status)
	for range 2 {
		assert.Equal(t, 200, request(order.TradeNo, "12", "USD", identity, "test", true))
	}
	require.NoError(t, db.First(&user, 730).Error)
	assert.Equal(t, 17, user.Quota)
	plan := model.SubscriptionPlan{Title: "synthetic plan", PriceAmount: 10, Currency: "USD", Enabled: true, DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 100}
	require.NoError(t, db.Create(&plan).Error)
	subOrder := model.SubscriptionOrder{UserId: 730, PlanId: plan.Id, TradeNo: "WAFFO_PANCAKE_SUB-fixture", Money: 10, PaymentProvider: model.PaymentProviderWaffoPancake, PaymentMethod: model.PaymentMethodWaffoPancake, Status: common.TopUpStatusPending, PaymentReceipt: model.PaymentReceipt{Amount: "10", Currency: "USD", Reference: "WAFFO_PANCAKE_SUB-fixture"}}
	require.NoError(t, subOrder.Insert())
	for range 2 {
		assert.Equal(t, 200, request(subOrder.TradeNo, "12", "USD", identity, "test", true))
	}
	var count int64
	require.NoError(t, db.Model(&model.UserSubscription{}).Where("user_id = ?", 730).Count(&count).Error)
	assert.EqualValues(t, 1, count)

	t.Run("checkout persists the exact outgoing snapshot", func(t *testing.T) {
		privateDER, err := x509.MarshalPKCS8PrivateKey(key)
		require.NoError(t, err)
		setting.WaffoPancakePrivateKey = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}))
		setting.WaffoPancakeMerchantID = "MER_AbCdEfGhIjKlMnOpQrStUv"
		setting.WaffoPancakeProductID = "PROD_AbCdEfGhIjKlMnOpQrStUv"
		oldPrice, oldMin := setting.WaffoPancakeUnitPrice, setting.WaffoPancakeMinTopUp
		oldClient, oldMemory := http.DefaultClient, common.MemoryCacheEnabled
		setting.WaffoPancakeUnitPrice, setting.WaffoPancakeMinTopUp = 2.9999, 1
		common.MemoryCacheEnabled = false
		oldDisplay := operation_setting.GetGeneralSetting().QuotaDisplayType
		operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
		operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{}
		t.Cleanup(func() {
			setting.WaffoPancakeUnitPrice, setting.WaffoPancakeMinTopUp = oldPrice, oldMin
			http.DefaultClient, common.MemoryCacheEnabled = oldClient, oldMemory
			operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplay
		})
		type checkoutBody struct {
			Currency string                            `json:"currency"`
			TradeNo  string                            `json:"orderMerchantExternalId"`
			Price    service.WaffoPancakePriceSnapshot `json:"priceSnapshot"`
		}
		captures := make(chan checkoutBody, 2)
		http.DefaultClient = &http.Client{Transport: pancakeCheckoutTransport(func(req *http.Request) (*http.Response, error) {
			response := `{"data":{"token":"synthetic-token","expiresAt":"synthetic-expiry"}}`
			switch req.URL.Path {
			case "/v1/actions/checkout/create-session":
				var body checkoutBody
				if err := common.DecodeJson(req.Body, &body); err != nil {
					return nil, err
				}
				var expected model.PaymentReceipt
				if strings.HasPrefix(body.TradeNo, "WAFFO_PANCAKE_SUB-") {
					order := model.GetSubscriptionOrderByTradeNo(body.TradeNo)
					if order == nil {
						return nil, errors.New("subscription was not persisted before checkout")
					}
					expected = order.PaymentReceipt
				} else {
					order := model.GetTopUpByTradeNo(body.TradeNo)
					if order == nil {
						return nil, errors.New("topup was not persisted before checkout")
					}
					expected = order.PaymentReceipt
				}
				if expected.Amount != body.Price.Amount || expected.Currency != body.Currency || expected.Reference != body.TradeNo {
					return nil, errors.New("persisted and outgoing snapshots differ")
				}
				captures <- body
				response = `{"data":{"sessionId":"synthetic-session","checkoutUrl":"https://checkout.invalid/synthetic","expiresAt":"synthetic-expiry"}}`
			case "/v1/actions/auth/issue-session-token":
			default:
				return nil, errors.New("unexpected request; real network access is prohibited")
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(response))}, nil
		})}
		newPlan := model.SubscriptionPlan{Id: 731, Title: "snapshot plan", PriceAmount: 29.999, Currency: "USD", Enabled: true, DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, WaffoPancakeProductId: setting.WaffoPancakeProductID}
		require.NoError(t, db.Create(&newPlan).Error)
		for _, subscription := range []bool{false, true} {
			r := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(r)
			body := `{"amount":10}`
			if subscription {
				body = `{"plan_id":731}`
			}
			c.Request = httptest.NewRequest("POST", "/synthetic-checkout", strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set("id", 730)
			if subscription {
				SubscriptionRequestWaffoPancakePay(c)
			} else {
				RequestWaffoPancakePay(c)
			}
			require.Equal(t, 200, r.Code)
			require.NotContains(t, r.Body.String(), `"message":"error"`, r.Body.String())
			select {
			case captured := <-captures:
				assert.Equal(t, "30.00", captured.Price.Amount)
			default:
				t.Fatal("checkout was not sent to the isolated transport")
			}
		}
	})
}

func TestGetWaffoPancakePayMoney(t *testing.T) {
	originalUnitPrice := setting.WaffoPancakeUnitPrice
	originalQuotaDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	originalDiscounts := make(map[int]float64, len(operation_setting.GetPaymentSetting().AmountDiscount))
	maps.Copy(originalDiscounts, operation_setting.GetPaymentSetting().AmountDiscount)
	originalTopupGroupRatio := common.TopupGroupRatio2JSONString()

	t.Cleanup(func() {
		setting.WaffoPancakeUnitPrice = originalUnitPrice
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalQuotaDisplayType
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscounts
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(originalTopupGroupRatio))
	})

	setting.WaffoPancakeUnitPrice = 2.5
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{
		10:                           0.8,
		int(common.QuotaPerUnit * 3): 0.5,
		20:                           0,
	}
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":1,"vip":1.2}`))

	testCases := []struct {
		name             string
		amount           int64
		group            string
		quotaDisplayType string
		expected         float64
	}{
		{
			name:             "currency display applies unit price group ratio and discount",
			amount:           10,
			group:            "vip",
			quotaDisplayType: operation_setting.QuotaDisplayTypeUSD,
			expected:         24,
		},
		{
			name:             "tokens display converts quota to display units before pricing",
			amount:           int64(common.QuotaPerUnit * 3),
			group:            "vip",
			quotaDisplayType: operation_setting.QuotaDisplayTypeTokens,
			expected:         4.5,
		},
		{
			name:             "non-positive discount falls back to no discount",
			amount:           20,
			group:            "default",
			quotaDisplayType: operation_setting.QuotaDisplayTypeUSD,
			expected:         50,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			operation_setting.GetGeneralSetting().QuotaDisplayType = tc.quotaDisplayType
			actual := getWaffoPancakePayMoney(tc.amount, tc.group)
			require.InDelta(t, tc.expected, actual, 0.000001)
		})
	}
}
