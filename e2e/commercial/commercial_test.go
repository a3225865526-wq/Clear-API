package commercial

import (
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
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/router"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/setting"
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

// This acceptance uses production HTTP routes, synthetic accounts, a fresh
// database and local HTTP providers. It never reads production credentials.
// SMTP delivery and real provider metering require separate external evidence.
func TestCommercialLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	t.Setenv("CLEARAPI_FINANCE_ENABLED", "true")
	t.Setenv("CLEARAPI_FINANCE_OWNER_USER_ID", "1")
	t.Setenv("CLEARAPI_DURABLE_WALLET", "true")
	t.Setenv("CLEARAPI_BILLING_RECONCILIATION_SINGLE_INSTANCE", "true")
	common.InitFinanceConfig()
	common.RedisEnabled, common.BatchUpdateEnabled = false, false
	common.RegisterEnabled, common.PasswordRegisterEnabled = true, true
	common.PasswordLoginEnabled, common.PasswordLoginEncryptionEnabled = true, false
	common.EmailVerificationEnabled = true
	common.QuotaForNewUser = 0
	common.SessionSecret = "isolated-commercial-acceptance-session-secret"
	constant.CountToken, constant.StreamingTimeout = true, 300

	dialect := os.Getenv("TEST_COMMERCIAL_DIALECT")
	if dialect == "" {
		dialect = "sqlite"
	}
	dsn := os.Getenv("TEST_" + strings.ToUpper(dialect) + "_DSN")
	if dialect == "sqlite" {
		dsn = filepath.Join(t.TempDir(), "commercial.db") + "?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate"
	}
	require.NotEmpty(t, dsn, "explicit isolated database required")
	var driver gorm.Dialector
	switch dialect {
	case "sqlite":
		driver = sqlite.Open(dsn)
	case "mysql":
		driver = mysql.Open(dsn)
	case "postgres":
		driver = postgres.Open(dsn)
	default:
		t.Fatalf("unsupported test database: %s", dialect)
	}
	db, err := gorm.Open(driver, &gorm.Config{})
	require.NoError(t, err)
	db.Logger = logger.Default.LogMode(logger.Silent)
	connection, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseType(dialect), common.DatabaseType(dialect))
	model.InitColumnQuotingForTest()
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.UserSession{}, &model.TwoFA{}, &model.TwoFABackupCode{},
		&model.PasskeyCredential{}, &model.AuthFlow{}, &model.UserOAuthBinding{},
		&model.Token{}, &model.Channel{}, &model.Ability{}, &model.Option{},
		&model.Log{}, &model.AuditLog{}, &model.Redemption{}, &model.OfflineOrder{}, &model.OfflineOrderCode{},
		&model.BillingReservation{}, &model.BillingReviewMarker{}, &model.BillingReservationDecision{}, &model.FinanceVersion{},
		&model.RouteChain{}, &model.RouteChainCandidate{}, &model.RouteChainRequestRecord{}, &model.RouteChainAttemptRecord{}, &model.RouteChainDecisionRecord{},
		&model.UserSubscription{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.SubscriptionPreConsumeRecord{},
		&model.Model{}, &model.Vendor{}, &model.CasbinRule{}, &model.AuthzRole{},
	))
	require.NoError(t, model.MigrateFinanceSync(db))
	model.InitOptionMap()
	require.NoError(t, authz.Init(db))
	service.InitTokenEncoders()
	require.NoError(t, model.UpdateOption("ModelRatio", `{"commercial-a":1,"commercial-b":2}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"commercial-a":2,"commercial-b":2}`))
	require.NoError(t, model.UpdateOption("GroupRatio", `{"default":1,"premium":2}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","premium":"Premium"}`))

	password, err := common.Password2Hash("commercial-test-password")
	require.NoError(t, err)
	admin := &model.User{Username: "commercial-admin", Password: password, Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1, AffCode: "admin"}
	require.NoError(t, db.Create(admin).Error)
	var calls atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(string(body), "reject-request") {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"error":{"message":"synthetic rejection","type":"invalid_request_error","code":"invalid_api_key"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"commercial-fixture","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
	}))
	t.Cleanup(provider.Close)
	for _, spec := range []struct {
		id             int
		models, groups string
	}{{11, "commercial-a,commercial-b", "default"}, {12, "commercial-a", "premium"}} {
		url := provider.URL
		channel := &model.Channel{Id: spec.id, Name: fmt.Sprint("provider-", spec.id), Type: 1, Key: "synthetic-provider-key", Status: common.ChannelStatusEnabled, BaseURL: &url, Models: spec.models, Group: spec.groups}
		require.NoError(t, channel.Insert())
	}
	model.InvalidatePricingCache()
	engine := gin.New()
	engine.Use(middleware.RequestId())
	router.SetApiRouter(engine)
	router.SetRelayRouter(engine)
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)

	request := func(method, path string, payload any, token string) (int, map[string]any) {
		t.Helper()
		body, marshalErr := common.Marshal(payload)
		require.NoError(t, marshalErr)
		req, reqErr := http.NewRequest(method, server.URL+path, strings.NewReader(string(body)))
		require.NoError(t, reqErr)
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, reqErr := server.Client().Do(req)
		require.NoError(t, reqErr)
		defer res.Body.Close()
		var result map[string]any
		require.NoError(t, common.DecodeJson(res.Body, &result))
		return res.StatusCode, result
	}
	login := func(username string) string {
		t.Helper()
		status, result := request("POST", "/api/user/login", map[string]any{"username": username, "password": "commercial-test-password"}, "")
		require.Equal(t, 200, status)
		require.Equal(t, true, result["success"], "password login must succeed")
		return result["data"].(map[string]any)["access_token"].(string)
	}
	adminToken := login(admin.Username)
	email := "commercial-user@example.com"
	register := map[string]any{"email": email, "password": "commercial-test-password", "verification_code": "123456"}
	_, result := request("POST", "/api/user/register", register, "")
	require.Equal(t, false, result["success"], "registration requires valid email proof")
	require.True(t, common.RegisterVerificationCodeWithKey(email, "123456", common.EmailVerificationPurpose))
	_, result = request("POST", "/api/user/register", register, "")
	require.Equal(t, true, result["success"], "verified registration")
	userToken := login(email)
	var user model.User
	require.NoError(t, db.Where("email = ?", email).First(&user).Error)
	require.Zero(t, user.Quota)
	status, _ := request("GET", "/api/user/", nil, userToken)
	require.Contains(t, []int{401, 403}, status, "normal user cannot access admin")
	status, result = request("GET", "/api/user/", nil, adminToken)
	require.Equal(t, 200, status)
	require.Equal(t, true, result["success"], "basic admin user list")
	_, result = request("POST", "/api/clearapi/offline-orders/", map[string]any{"order_no": "COMMERCIAL-SYNTHETIC-1", "money_cents": 100, "quota_per_code": 10000, "count": 1, "paid_at": time.Now().Unix()}, adminToken)
	require.Equal(t, true, result["success"], "admin creates synthetic paid offline order")
	code := result["data"].(map[string]any)["codes"].([]any)[0].(string)
	_, result = request("POST", "/api/clearapi/redeem", map[string]any{"key": code}, userToken)
	require.Equal(t, true, result["success"], "redeem offline code")
	_, result = request("POST", "/api/clearapi/redeem", map[string]any{"key": code}, userToken)
	require.Equal(t, false, result["success"], "redeem replay rejected")
	_, result = request("GET", "/api/user/self", nil, userToken)
	require.Equal(t, float64(10000), result["data"].(map[string]any)["quota"])
	var incomeEvents int64
	require.NoError(t, db.Model(&model.FinanceSyncEvent{}).Where("user_id = ? AND topic = ?", user.Id, "offline_redemption").Count(&incomeEvents).Error)
	assert.Equal(t, int64(1), incomeEvents, "redemption records one durable financial source event")

	_, result = request("POST", "/api/token/", map[string]any{"name": "commercial-key", "expired_time": -1, "remain_quota": 10000, "group": "default"}, userToken)
	require.Equal(t, true, result["success"])
	var key model.Token
	require.NoError(t, db.Where("user_id = ?", user.Id).First(&key).Error)
	status, _ = request("GET", "/v1/models", nil, "sk-"+key.Key)
	require.Equal(t, 200, status, "new API key authenticates")
	status, result = request("GET", "/api/pricing", nil, userToken)
	require.Equal(t, 200, status)
	require.Equal(t, true, result["success"])
	require.Contains(t, result["group_ratio"], "default")
	chat := func(prompt string) (int, map[string]any) {
		return request("POST", "/v1/chat/completions", map[string]any{"model": "commercial-a", "max_tokens": 16, "messages": []map[string]string{{"role": "user", "content": prompt}}}, "sk-"+key.Key)
	}
	status, result = chat("hello")
	require.Equal(t, 200, status, "paid API request")
	require.Equal(t, float64(15), result["usage"].(map[string]any)["total_tokens"])
	require.Eventually(t, func() bool { db.First(&user, user.Id); return user.Quota == 9980 }, 5*time.Second, 10*time.Millisecond)
	var consume model.Log
	require.Eventually(t, func() bool {
		return db.Where("user_id = ? AND type = ?", user.Id, model.LogTypeConsume).First(&consume).Error == nil
	}, 5*time.Second, 10*time.Millisecond)
	assert.Equal(t, 10, consume.PromptTokens)
	assert.Equal(t, 5, consume.CompletionTokens)
	assert.Equal(t, 20, consume.Quota)
	require.NoError(t, db.First(&key, key.Id).Error)
	assert.Equal(t, 9980, key.RemainQuota)
	_, result = request("PUT", "/api/token/", map[string]any{"id": key.Id, "name": key.Name, "expired_time": -1, "remain_quota": 9980, "group": "premium"}, userToken)
	require.Equal(t, true, result["success"], "same model selectable in another group")
	status, _ = chat("premium-price")
	require.Equal(t, 200, status)
	require.Eventually(t, func() bool { db.First(&user, user.Id); return user.Quota == 9940 }, 5*time.Second, 10*time.Millisecond)
	var premium model.Log
	require.Eventually(t, func() bool {
		return db.Where("user_id = ? AND channel_id = ? AND type = ?", user.Id, 12, model.LogTypeConsume).First(&premium).Error == nil
	}, 5*time.Second, 10*time.Millisecond)
	assert.Equal(t, 40, premium.Quota, "same usage with group multiplier 2")
	assert.Equal(t, "premium", premium.Group)
	require.NoError(t, db.First(&key, key.Id).Error)
	assert.Equal(t, 9940, key.RemainQuota)
	status, _ = chat("reject-request")
	require.NotEqual(t, 200, status)
	require.Eventually(t, func() bool { db.First(&user, user.Id); return user.Quota == 9940 }, 5*time.Second, 10*time.Millisecond)
	var charges int64
	require.NoError(t, db.Model(&model.Log{}).Where("user_id = ? AND type = ?", user.Id, model.LogTypeConsume).Count(&charges).Error)
	assert.Equal(t, int64(2), charges, "rejected request must not create a charge")
	require.NoError(t, db.First(&key, key.Id).Error)
	assert.Equal(t, 9940, key.RemainQuota, "rejected request restores key quota")
	_, result = request("PUT", "/api/token/", map[string]any{"id": key.Id, "name": key.Name, "expired_time": -1, "remain_quota": 9940, "group": "default"}, userToken)
	require.Equal(t, true, result["success"])
	status, _ = request("POST", "/v1/chat/completions", map[string]any{"model": "commercial-b", "max_tokens": 16, "messages": []map[string]string{{"role": "user", "content": "second-model"}}}, "sk-"+key.Key)
	require.Equal(t, 200, status, "second model works without deployment-code changes")
	require.Eventually(t, func() bool { db.First(&user, user.Id); return user.Quota == 9900 }, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, db.Model(&user).Update("quota", 0).Error)
	before := calls.Load()
	status, _ = chat("insufficient-balance")
	require.NotEqual(t, 200, status)
	assert.Equal(t, before, calls.Load(), "insufficient balance must block upstream dispatch")
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Zero(t, user.Quota)
	t.Logf("%s: verified registration, login, admin, key, redemption/replay, pricing, usage, exact charge, rejection refund and zero-balance dispatch guard", dialect)
}
