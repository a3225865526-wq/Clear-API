package service_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
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

func TestProtectedBillingDatabase(t *testing.T) {
	t.Setenv("CLEARAPI_BILLING_PROTECTION", "true")
	oldDB, oldRedis, oldBatch := model.DB, common.RedisEnabled, common.BatchUpdateEnabled
	oldMain, oldLog := common.MainDatabaseType(), common.LogDatabaseType()
	common.RedisEnabled, common.BatchUpdateEnabled = false, false
	t.Cleanup(func() {
		model.DB = oldDB
		common.RedisEnabled = oldRedis
		common.BatchUpdateEnabled = oldBatch
		common.SetDatabaseTypes(oldMain, oldLog)
	})
	dialect, dsn := os.Getenv("TEST_BILLING_DIALECT"), os.Getenv("TEST_BILLING_DSN")
	var driver gorm.Dialector
	switch dialect {
	case "", "sqlite":
		dialect = "sqlite"
		dsn = filepath.Join(t.TempDir(), "billing.db")
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
	model.DB = db
	// Representative existing account tables, before the additive migration.
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.UserSubscription{}))
	u := model.User{Username: "billing-protection-synthetic", Quota: 10000, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&u).Error)
	token := model.Token{UserId: u.Id, Key: "synthetic-billing-protection", RemainQuota: 10000, Status: common.TokenStatusEnabled, ExpiredTime: -1}
	require.NoError(t, db.Create(&token).Error)
	require.Error(t, service.ValidateBillingProtection())
	require.NoError(t, model.MigrateBillingReservations(db))
	require.NoError(t, model.MigrateBillingReservations(db))
	require.NoError(t, service.ValidateBillingProtection())
	require.NoError(t, db.Migrator().DropIndex(&model.BillingReservation{}, "idx_billing_request_id"))
	require.Error(t, service.ValidateBillingProtection())
	require.NoError(t, model.MigrateBillingReservations(db))
	require.NoError(t, service.ValidateBillingProtection())
	var version string
	query := "select version()"
	if dialect == "sqlite" {
		query = "select sqlite_version()"
	}
	require.NoError(t, db.Raw(query).Scan(&version).Error)
	t.Logf("%s %s", dialect, version)
	balance := func(want int) {
		t.Helper()
		var user model.User
		var key model.Token
		require.NoError(t, db.First(&user, u.Id).Error)
		require.NoError(t, db.First(&key, token.Id).Error)
		assert.Equal(t, want, user.Quota)
		assert.Equal(t, want, key.RemainQuota)
		assert.Equal(t, 10000-want, key.UsedQuota)
	}
	balance(10000)
	require.NoError(t, model.ReserveBillingRequest("zero", u.Id, token.Id, 0))
	require.NoError(t, model.RequestBillingCompletion("zero", 0, true))
	_, err = model.ApplyBillingCompletion("zero")
	require.NoError(t, err)
	balance(10000)
	duplicate := model.Token{UserId: u.Id, Key: token.Key}
	require.Error(t, db.Create(&duplicate).Error)
	// Unique request IDs and insufficient token quota roll back wallet+ledger.
	require.NoError(t, model.ReserveBillingRequest("first", u.Id, token.Id, 1000))
	balance(9000)
	require.Error(t, model.ReserveBillingRequest("first", u.Id, token.Id, 1000))
	balance(9000)
	require.Error(t, model.ReserveBillingRequest("insufficient", u.Id, token.Id, 10001))
	balance(9000)
	require.Error(t, model.ReserveBillingRequest("wrong-owner", u.Id, token.Id+100, 100))
	balance(9000)
	var count int64
	require.NoError(t, db.Model(&model.BillingReservation{}).Count(&count).Error)
	assert.Equal(t, int64(2), count)
	// Persisted completion survives loss of all process-local billing sessions.
	require.NoError(t, model.RequestBillingCompletion("first", 300, false))
	require.NoError(t, sqlDB.Close())
	db, err = gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	model.DB = db
	sqlDB, err = db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	if dialect != "sqlite" {
		sqlDB.SetMaxOpenConns(4)
	}
	results := make(chan error, 2)
	for range 2 {
		go func() { _, err := model.ApplyBillingCompletion("first"); results <- err }()
	}
	for range 2 {
		require.NoError(t, <-results)
	}
	balance(9700)
	require.Error(t, model.RequestBillingCompletion("first", 0, true))
	balance(9700)
	// Failed application retains the intent, rolls back both credits and state.
	require.NoError(t, model.ReserveBillingRequest("refund", u.Id, token.Id, 700))
	balance(9000)
	require.NoError(t, model.RequestBillingCompletion("refund", 0, true))
	callback := "billing-protection-test-failure"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "tokens" {
			tx.AddError(errors.New("synthetic storage failure"))
		}
	}))
	_, err = model.ApplyBillingCompletion("refund")
	require.Error(t, err)
	balance(9000)
	var row model.BillingReservation
	require.NoError(t, db.Where("request_id = ?", "refund").First(&row).Error)
	assert.Equal(t, "refund_pending", row.State)
	require.NoError(t, db.Callback().Update().Remove(callback))
	require.NoError(t, model.RecoverBillingCompletions(100))
	require.NoError(t, model.RecoverBillingCompletions(100))
	balance(9700)
	// Unknown interrupted requests and exceeded budgets are never auto-refunded.
	require.NoError(t, model.ReserveBillingRequest("unknown", u.Id, token.Id, 200))
	require.NoError(t, model.ReserveBillingRequest("over-budget", u.Id, token.Id, 100))
	require.NoError(t, model.RequestBillingCompletion("over-budget", 101, false))
	_, err = model.ApplyBillingCompletion("over-budget")
	require.Error(t, err)
	require.NoError(t, model.RecoverBillingCompletions(100))
	balance(9400)
	row = model.BillingReservation{}
	require.NoError(t, db.Where("request_id = ?", "over-budget").First(&row).Error)
	assert.Equal(t, "needs_review", row.State)
	// Public billing session uses the same atomic path, without wallet fallback.
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{UserId: u.Id, TokenId: token.Id, RequestId: "session", ProtectedTextBudget: true, UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
	session, apiErr := service.NewBillingSession(ctx, info, 400)
	require.Nil(t, apiErr)
	balance(9000)
	require.NoError(t, session.Settle(150))
	require.NoError(t, session.Settle(150))
	session.Refund(ctx)
	balance(9250)
	assert.Equal(t, 150, session.ChargedQuota())
	info.RequestId = "refund-session"
	session, apiErr = service.NewBillingSession(ctx, info, 250)
	require.Nil(t, apiErr)
	session.Refund(ctx)
	session.Refund(ctx)
	balance(9250)
	common.RedisEnabled = true
	require.Error(t, service.ValidateBillingProtection())
	common.RedisEnabled = false
	common.BatchUpdateEnabled = true
	require.Error(t, service.ValidateBillingProtection())
	common.BatchUpdateEnabled = false
}

func TestProtectedTextBudget(t *testing.T) {
	t.Setenv("CLEARAPI_BILLING_PROTECTION", "true")
	oldCount := constant.CountToken
	constant.CountToken = true
	t.Cleanup(func() { constant.CountToken = oldCount })
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(k, v string) error { saved[k] = v; return nil }))
	t.Cleanup(func() { require.NoError(t, config.GlobalConfig.LoadFromDB(saved)) })
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"ModelRatio": `{"protected-test":1}`, "CompletionRatio": `{"protected-test":2}`, "group_ratio_setting.group_ratio": `{"default":1}`}))
	oldRatios, oldCompletion := ratio_setting.ModelRatio2JSONString(), ratio_setting.CompletionRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldRatios))
		require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(oldCompletion))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"protected-test":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"protected-test":2}`))
	oldCache, oldCreate := ratio_setting.CacheRatio2JSONString(), ratio_setting.CreateCacheRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateCacheRatioByJSONString(oldCache))
		require.NoError(t, ratio_setting.UpdateCreateCacheRatioByJSONString(oldCreate))
	})
	require.NoError(t, ratio_setting.UpdateCacheRatioByJSONString(`{"protected-test":0.25}`))
	require.NoError(t, ratio_setting.UpdateCreateCacheRatioByJSONString(`{"protected-test":0.5}`))
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	for _, tc := range []struct {
		body string
		ok   bool
	}{
		{`{"model":"protected-test","messages":[{"role":"user","content":"hello"}],"max_tokens":100}`, true},
		{`{"model":"protected-test","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":100}`, true},
		{`{"model":"protected-test","messages":[{"role":"user","content":"hello"}]}`, false},
		{`{"model":"protected-test","messages":[{"role":"user","content":"hello"}],"max_tokens":100,"n":2}`, false},
		{`{"model":"protected-test","messages":[{"role":"user","content":"hello"}],"max_tokens":100,"max_completion_tokens":200}`, false},
		{`{"model":"protected-test","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"synthetic"}}]}],"max_tokens":100}`, false},
		{`{"model":"protected-test","messages":[{"role":"user","content":"hello"}],"max_tokens":100,"tools":[{"type":"web_search"}]}`, false},
		{`{"model":"protected-test-search-preview","messages":[{"role":"user","content":"hello"}],"max_tokens":100}`, false},
	} {
		t.Run(fmt.Sprintf("case-%d", len(tc.body)), func(t *testing.T) {
			var request dto.GeneralOpenAIRequest
			require.NoError(t, json.Unmarshal([]byte(tc.body), &request))
			info := &relaycommon.RelayInfo{Request: &request, RelayFormat: types.RelayFormatOpenAI, OriginModelName: "protected-test", UserGroup: "default", UsingGroup: "default"}
			price, err := helper.ModelPriceHelper(ctx, info, 10, &types.TokenCountMeta{})
			if !tc.ok {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, 210, price.QuotaToPreConsume)
			assert.True(t, info.ProtectedTextBudget)
			require.NoError(t, helper.ValidateProtectedTextBody(info, []byte(tc.body)))
			require.Error(t, helper.ValidateProtectedTextBody(info, []byte(`{"model":"protected-test","messages":[{"role":"user","content":"hello"}],"max_tokens":200}`)))
			info.ChannelMeta = &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI}
			require.NoError(t, helper.ValidateProtectedTextChannel(info))
			info.ParamOverride = map[string]any{"max_tokens": 200}
			require.Error(t, helper.ValidateProtectedTextChannel(info))
		})
	}
}
