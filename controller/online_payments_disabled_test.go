package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupOnlinePaymentsGuardTest 提供内存数据库、已登录普通用户和挂在对应路径
// 上的单个支付创建路由，用于验证 DisableOnlinePayments 总开关。
func setupOnlinePaymentsGuardTest(t *testing.T, method, path string, handler gin.HandlerFunc) (*gin.Engine, string) {
	t.Helper()

	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis := common.RedisEnabled

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.TopUp{},
		&model.SubscriptionOrder{},
		&model.SubscriptionPlan{},
	))
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		common.RedisEnabled = previousRedis
		sqlDB.Close()
	})

	previousDisabled := operation_setting.DisableOnlinePayments
	t.Cleanup(func() { operation_setting.DisableOnlinePayments = previousDisabled })

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.RequestId())
	router.Handle(method, path, middleware.UserAuth(), handler)

	token := "online-payments-guard-test-token"
	user := model.User{
		Username:    "online-payments-guard-user",
		Password:    "guard-test-password",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AffCode:     "online-payments-guard-user",
		AccessToken: &token,
	}
	require.NoError(t, db.Create(&user).Error)
	return router, token
}

func doOnlinePaymentsGuardRequest(router *gin.Engine, token, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func decodeOnlinePaymentsGuardResponse(t *testing.T, response *httptest.ResponseRecorder) (success bool, message string) {
	t.Helper()
	var result struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    any    `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	// 两种既有响应格式：common.ApiErrorMsg 的文案在 message；旧式
	// gin.H{"message":"error","data":...} 的文案在 data。
	if result.Message == "error" {
		if text, ok := result.Data.(string); ok {
			return result.Success, text
		}
	}
	return result.Success, result.Message
}

func TestOnlinePaymentCreationRejectedWhenDisabled(t *testing.T) {
	// 当前生产模式：无论凭证/合规如何配置，创建在线支付订单必须被后端拒绝。
	operation_setting.DisableOnlinePayments = true
	confirmPaymentComplianceForTest(t)

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		handle gin.HandlerFunc
	}{
		{"epay", http.MethodPost, "/api/user/pay", `{"amount":100,"payment_method":"alipay"}`, RequestEpay},
		{"stripe", http.MethodPost, "/api/user/stripe/pay", `{"amount":100}`, RequestStripePay},
		{"creem", http.MethodPost, "/api/user/creem/pay", `{"product_id":"prod_x"}`, RequestCreemPay},
		{"waffo", http.MethodPost, "/api/user/waffo/pay", `{"amount":100,"payment_method":"CARD"}`, RequestWaffoPay},
		{"waffo-pancake", http.MethodPost, "/api/user/waffo-pancake/pay", `{"amount":100}`, RequestWaffoPancakePay},
		{"subscription-epay", http.MethodPost, "/api/subscription/epay/pay", `{"plan_id":1,"payment_method":"alipay"}`, SubscriptionRequestEpay},
		{"subscription-stripe", http.MethodPost, "/api/subscription/stripe/pay", `{"plan_id":1}`, SubscriptionRequestStripePay},
		{"subscription-creem", http.MethodPost, "/api/subscription/creem/pay", `{"plan_id":1}`, SubscriptionRequestCreemPay},
		{"subscription-waffo-pancake", http.MethodPost, "/api/subscription/waffo-pancake/pay", `{"plan_id":1}`, SubscriptionRequestWaffoPancakePay},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router, token := setupOnlinePaymentsGuardTest(t, tc.method, tc.path, tc.handle)
			response := doOnlinePaymentsGuardRequest(router, token, tc.method, tc.path, tc.body)
			require.Equal(t, http.StatusOK, response.Code, "业务错误而非 404/500: %s", response.Body.String())
			success, message := decodeOnlinePaymentsGuardResponse(t, response)
			assert.False(t, success)
			assert.Contains(t, message, "在线支付当前未开放")

			var topupCount, subscriptionCount int64
			require.NoError(t, model.DB.Model(&model.TopUp{}).Count(&topupCount).Error)
			require.NoError(t, model.DB.Model(&model.SubscriptionOrder{}).Count(&subscriptionCount).Error)
			assert.Zero(t, topupCount, "不得创建充值订单")
			assert.Zero(t, subscriptionCount, "不得创建订阅订单")
		})
	}
}

func TestOnlinePaymentCreationGuardPlacementWithComplianceAndCredentials(t *testing.T) {
	// 管理员确认合规且配置了全部凭证时，开关仍然优先拦截 —— 防止"配置即上线"。
	operation_setting.DisableOnlinePayments = true
	confirmPaymentComplianceForTest(t)

	previousPayAddress, previousEpayId, previousEpayKey := operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey
	previousStripeSecret, previousStripeWebhook, previousStripePrice := setting.StripeApiSecret, setting.StripeWebhookSecret, setting.StripePriceId
	t.Cleanup(func() {
		operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = previousPayAddress, previousEpayId, previousEpayKey
		setting.StripeApiSecret, setting.StripeWebhookSecret, setting.StripePriceId = previousStripeSecret, previousStripeWebhook, previousStripePrice
	})
	operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = "https://epay.example.com", "partner", "key"
	setting.StripeApiSecret, setting.StripeWebhookSecret, setting.StripePriceId = "sk_test", "whsec_test", "price_test"

	router, token := setupOnlinePaymentsGuardTest(t, http.MethodPost, "/api/user/pay", RequestEpay)
	response := doOnlinePaymentsGuardRequest(router, token, http.MethodPost, "/api/user/pay", `{"amount":100,"payment_method":"alipay"}`)
	require.Equal(t, http.StatusOK, response.Code)
	success, message := decodeOnlinePaymentsGuardResponse(t, response)
	assert.False(t, success)
	assert.Contains(t, message, "在线支付当前未开放")
}

func TestOnlinePaymentCreationAllowedWhenSwitchOff(t *testing.T) {
	// 恢复在线支付：开关关闭后守卫放行，后续逻辑照旧（未配置 Epay 凭证时
	// 返回"当前管理员未配置支付信息"而不是总开关文案）。
	operation_setting.DisableOnlinePayments = false
	confirmPaymentComplianceForTest(t)
	originalPayMethods := operation_setting.PayMethods
	t.Cleanup(func() { operation_setting.PayMethods = originalPayMethods })
	operation_setting.PayMethods = []map[string]string{{"name": "alipay", "type": "alipay"}}

	router, token := setupOnlinePaymentsGuardTest(t, http.MethodPost, "/api/user/pay", RequestEpay)
	response := doOnlinePaymentsGuardRequest(router, token, http.MethodPost, "/api/user/pay", `{"amount":100,"payment_method":"alipay"}`)
	require.Equal(t, http.StatusOK, response.Code)
	success, message := decodeOnlinePaymentsGuardResponse(t, response)
	// 开关关闭后请求穿过守卫；下游因测试环境缺少凭证/分组返回其它业务错误，
	// 这里只断言不是总开关文案。
	assert.NotContains(t, message, "在线支付当前未开放")
	_ = success
}

func TestGetTopUpInfoHidesOnlinePaymentsWhenDisabled(t *testing.T) {
	operation_setting.DisableOnlinePayments = true
	confirmPaymentComplianceForTest(t)
	originalPayMethods := operation_setting.PayMethods
	operation_setting.PayMethods = []map[string]string{{"name": "alipay", "type": "alipay"}}
	t.Cleanup(func() { operation_setting.PayMethods = originalPayMethods })

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/user/topup/info", nil)
	ctx.Set("id", 0)
	GetTopUpInfo(ctx)

	var result struct {
		Success bool `json:"success"`
		Data    struct {
			EnableOnlineTopup        any `json:"enable_online_topup"`
			EnableStripeTopup        any `json:"enable_stripe_topup"`
			EnableCreemTopup         any `json:"enable_creem_topup"`
			EnableWaffoTopup         any `json:"enable_waffo_topup"`
			EnableWaffoPancakeTopup  any `json:"enable_waffo_pancake_topup"`
			PayMethods               any `json:"pay_methods"`
			CreemProducts            any `json:"creem_products"`
			StoreRechargeEnabledView any `json:"store_recharge_enabled"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result))
	require.True(t, result.Success)
	assert.Equal(t, false, result.Data.EnableOnlineTopup, fmt.Sprintf("response=%s", recorder.Body.String()))
	assert.Equal(t, false, result.Data.EnableStripeTopup)
	assert.Equal(t, false, result.Data.EnableCreemTopup)
	assert.Equal(t, false, result.Data.EnableWaffoTopup)
	assert.Equal(t, false, result.Data.EnableWaffoPancakeTopup)
	assert.Empty(t, result.Data.PayMethods)
	assert.Empty(t, result.Data.CreemProducts)
}
