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
	// Stage 2: one claim covers finalize, refund and all accounting side effects.
	require.NoError(t, db.AutoMigrate(&model.BillingReviewMarker{}))
	for _, tc := range []struct {
		name                string
		reserved, actual    int
		race, unknown, fail bool
	}{
		{name: "codex-less", reserved: 400, actual: 150},
		{name: "codex-more", reserved: 100, actual: 150},
		{name: "codex-concurrent", reserved: 400, actual: 150, race: true},
		{name: "codex-unknown", reserved: 400, unknown: true},
		{name: "codex-db-failure", reserved: 400, actual: 150, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user := model.User{Username: tc.name, AffCode: tc.name, Quota: 10000, Status: common.UserStatusEnabled}
			require.NoError(t, db.Create(&user).Error)
			key := model.Token{UserId: user.Id, Key: "synthetic-" + tc.name, RemainQuota: 10000, Status: common.TokenStatusEnabled, ExpiredTime: -1}
			require.NoError(t, db.Create(&key).Error)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{UserId: user.Id, TokenId: key.Id, RequestId: tc.name, ProtectedTextBudget: true, UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
			session, apiErr := service.NewBillingSession(ctx, info, tc.reserved)
			require.Nil(t, apiErr)
			meter := dto.ParseCodexMetering([]byte(`{"input_tokens":100,"output_tokens":20,"total_tokens":120,"input_tokens_details":{"cached_tokens":30},"output_tokens_details":{"reasoning_tokens":7}}`))
			if tc.unknown {
				meter = dto.ParseCodexMetering(nil)
			}
			claimed := make(chan bool, 2)
			gate := make(chan struct{})
			for range 2 {
				go func() { <-gate; claimed <- session.ClaimCodexFinalization(meter) }()
			}
			close(gate)
			a, b := <-claimed, <-claimed
			require.NotEqual(t, a, b)
			if tc.fail {
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register("codex-fail", func(tx *gorm.DB) {
					if tx.Statement.Table == "tokens" {
						tx.AddError(errors.New("synthetic settlement failure"))
					}
				}))
			}
			result := make(chan error, 1)
			go func() {
				if tc.unknown {
					result <- session.SettleUncertain()
				} else {
					result <- session.Settle(tc.actual)
				}
			}()
			if tc.race {
				session.Refund(ctx)
			}
			settlementErr := <-result
			want := tc.actual
			if tc.unknown {
				want = tc.reserved
			}
			if tc.actual > tc.reserved {
				require.Error(t, settlementErr)
				want = tc.reserved
			} else if tc.fail {
				require.Error(t, settlementErr)
				require.NoError(t, db.Callback().Update().Remove("codex-fail"))
				require.NoError(t, model.RecoverBillingCompletions(100))
				require.NoError(t, session.Settle(tc.actual))
			} else {
				require.NoError(t, settlementErr)
			}
			session.Refund(ctx)
			session.Refund(ctx)
			var after model.User
			var afterKey model.Token
			require.NoError(t, db.First(&after, user.Id).Error)
			require.NoError(t, db.First(&afterKey, key.Id).Error)
			assert.Equal(t, 10000-want, after.Quota)
			assert.Equal(t, after.Quota, afterKey.RemainQuota)
			assert.Equal(t, want, afterKey.UsedQuota)
			var reservation model.BillingReservation
			require.NoError(t, db.Where("request_id = ?", tc.name).First(&reservation).Error)
			if tc.actual > tc.reserved {
				assert.Equal(t, "needs_review", reservation.State)
			} else {
				assert.Equal(t, "settled", reservation.State)
			}
			if tc.unknown {
				var marker model.BillingReviewMarker
				require.NoError(t, db.Where("reservation_id = ?", reservation.ID).First(&marker).Error)
				assert.Equal(t, "upstream_usage_unknown", marker.Reason)
			}
		})
	}
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

func TestProtectedResponsesTextBudget(t *testing.T) {
	t.Setenv("CLEARAPI_BILLING_PROTECTION", "true")
	oldCount := constant.CountToken
	constant.CountToken = true
	t.Cleanup(func() { constant.CountToken = oldCount })
	oldRatio, oldCompletion := ratio_setting.ModelRatio2JSONString(), ratio_setting.CompletionRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldRatio))
		require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(oldCompletion))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"protected-responses-test":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"protected-responses-test":2}`))
	for _, tc := range []struct {
		name, input, extra string
		limit              uint
		ok                 bool
	}{
		{name: "text", input: `"hello"`, limit: 100, ok: true},
		{name: "instructions", input: `"hello"`, extra: `,"instructions":"be concise"`, limit: 100, ok: true},
		{name: "text messages", input: `[{"role":"user","content":[{"type":"input_text","text":"hello"}]}]`, limit: 100, ok: true},
		{name: "string messages", input: `[{"type":"message","role":"user","content":"hello"}]`, limit: 100, ok: true},
		{name: "null text type", input: `[{"role":"user","content":[{"type":"input_text","text":"hello"},{"type":null,"text":"unknown"}]}]`, limit: 100},
		{name: "tools", input: `"hello"`, extra: `,"tools":[{"type":"web_search"}]`, limit: 100},
		{name: "tool call", input: `[{"type":"function_call","call_id":"call","name":"lookup","arguments":"{}"}]`, limit: 100},
		{name: "image", input: `[{"role":"user","content":[{"type":"input_image","image_url":"synthetic"}]}]`, limit: 100},
		{name: "file", input: `[{"role":"user","content":[{"type":"input_file","file_id":"synthetic"}]}]`, limit: 100},
		{name: "item reference", input: `[{"type":"item_reference","id":"synthetic"}]`, limit: 100},
		{name: "message reference", input: `[{"role":"user","content":"hello","id":"synthetic"}]`, limit: 100},
		{name: "previous response", input: `"hello"`, extra: `,"previous_response_id":"synthetic"`, limit: 100},
		{name: "conversation", input: `"hello"`, extra: `,"conversation":"synthetic"`, limit: 100},
		{name: "prompt reference", input: `"hello"`, extra: `,"prompt":{"id":"synthetic"}`, limit: 100},
		{name: "filtered optional field", input: `"hello"`, extra: `,"thinking_budget":1`, limit: 100},
		{name: "instructions reference", input: `"hello"`, extra: `,"instructions":{"id":"synthetic"}`, limit: 100},
		{name: "null instructions", input: `"hello"`, extra: `,"instructions":null`, limit: 100},
		{name: "missing budget", input: `"hello"`},
		{name: "over budget", input: `"hello"`, limit: 1 << 31},
		{name: "wrapped budget", input: `"hello"`, limit: ^uint(0)},
		{name: "empty input", input: `[]`, limit: 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"model":"protected-responses-test","stream":true,"input":%s,"max_output_tokens":%d%s}`, tc.input, tc.limit, tc.extra))
			var request dto.OpenAIResponsesRequest
			require.NoError(t, common.Unmarshal(body, &request))
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{Request: &request, RelayFormat: types.RelayFormatOpenAIResponses, OriginModelName: request.Model, UserGroup: "default", UsingGroup: "default"}
			price, err := helper.ModelPriceHelper(ctx, info, 10, request.GetTokenCountMeta())
			if !tc.ok {
				require.Error(t, err)
				assert.False(t, info.ProtectedTextBudget)
				return
			}
			require.NoError(t, err)
			assert.GreaterOrEqual(t, price.QuotaToPreConsume, 210)
			assert.True(t, info.ProtectedTextBudget)
			require.NoError(t, helper.ValidateProtectedTextBody(info, body))
			for _, outbound := range []string{
				`{"model":"protected-responses-test","stream":true,"input":"changed","max_output_tokens":100}`,
				`{"model":"protected-responses-test","stream":true,"input":"hello","max_output_tokens":101}`,
				`{"model":"protected-responses-test","stream":true,"input":"hello","instructions":"changed","max_output_tokens":100}`,
				`{"model":"protected-responses-test","stream":true,"input":"hello","max_output_tokens":100,"unknown_budget":1}`,
			} {
				require.Error(t, helper.ValidateProtectedTextBody(info, []byte(outbound)))
			}
			stream := false
			request.Stream = &stream
			_, err = helper.ModelPriceHelper(ctx, info, 10, request.GetTokenCountMeta())
			require.Error(t, err, "non-streaming Responses admission remains closed")
		})
	}
}
