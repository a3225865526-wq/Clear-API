package service_test

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relayapi "github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
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
	// The DB matrix hands every package one shared database, and the model
	// package (which runs first in the same job) migrates the additive
	// reservation schema into it. The fail-closed precondition asserted below
	// must be constructed by this test, never assumed: drop any leftover
	// billing_reservations table so the "missing schema" assertion holds
	// regardless of package order or prior database state.
	if db.Migrator().HasTable(&model.BillingReservation{}) {
		require.NoError(t, db.Migrator().DropTable(&model.BillingReservation{}))
	}
	// Representative existing account tables, before the additive migration.
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.UserSubscription{}))
	// Fixture identity must be collision-proof against any prior state in a
	// shared database (unique username / token key / aff_code indexes).
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	u := model.User{Username: "billing-protection-synthetic-" + suffix, AffCode: suffix, Quota: 10000, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&u).Error)
	token := model.Token{UserId: u.Id, Key: "sbp" + suffix, RemainQuota: 10000, Status: common.TokenStatusEnabled, ExpiredTime: -1}
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
	info := &relaycommon.RelayInfo{UserId: u.Id, TokenId: token.Id, RequestId: "session", UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
	session, apiErr := service.NewBillingSession(ctx, info, 400)
	require.Nil(t, apiErr)
	balance(9000)
	require.NoError(t, session.Settle(150))
	require.NoError(t, session.Settle(150))
	session.Refund(ctx)
	balance(9250)
	assert.Equal(t, 150, session.ChargedQuota())
	info.IsPlayground = true
	_, apiErr = service.NewBillingSession(ctx, info, 1)
	require.NotNil(t, apiErr)
	assert.Equal(t, 400, apiErr.StatusCode)
	info.IsPlayground = false
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
			info := &relaycommon.RelayInfo{UserId: user.Id, TokenId: key.Id, RequestId: tc.name, UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
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

// One protocol matrix shares the real wallet lifecycle. Providers are represented
// by deterministic protocol usage fixtures; no remote provider is contacted.
func TestProtocolBillingCompatibility(t *testing.T) {
	oldMain, oldLog := common.MainDatabaseType(), common.LogDatabaseType()
	t.Cleanup(func() { common.SetDatabaseTypes(oldMain, oldLog) })
	db := newHardeningDB(t)
	require.NoError(t, db.AutoMigrate(&model.Channel{}))
	require.NoError(t, model.MigrateBillingCompletionReceipts(db))
	t.Setenv("CLEARAPI_BILLING_PROTECTION", "true")
	t.Setenv("CLEARAPI_BILLING_OUTBOX", "true")
	oldRedis, oldBatch, oldCount := common.RedisEnabled, common.BatchUpdateEnabled, constant.CountToken
	common.RedisEnabled, common.BatchUpdateEnabled, constant.CountToken = false, false, true
	t.Cleanup(func() {
		common.RedisEnabled, common.BatchUpdateEnabled, constant.CountToken = oldRedis, oldBatch, oldCount
	})
	protectedEncoderInit.Do(service.InitTokenEncoders)
	previous := config.GlobalConfig.ExportAllConfigs()
	oldRatio, oldCompletion := ratio_setting.ModelRatio2JSONString(), ratio_setting.CompletionRatio2JSONString()
	oldCache, oldCreate := ratio_setting.CacheRatio2JSONString(), ratio_setting.CreateCacheRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(previous))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldRatio))
		require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(oldCompletion))
		require.NoError(t, ratio_setting.UpdateCacheRatioByJSONString(oldCache))
		require.NoError(t, ratio_setting.UpdateCreateCacheRatioByJSONString(oldCreate))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"protocol-fixture":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"protocol-fixture":2}`))
	require.NoError(t, ratio_setting.UpdateCacheRatioByJSONString(`{"protocol-fixture":0.25}`))
	require.NoError(t, ratio_setting.UpdateCreateCacheRatioByJSONString(`{"protocol-fixture":0.5}`))
	profile := coldProfile(100)
	profile["model_context_max"], profile["model_output_max"] = 16384, 1024
	setPolicy(t, operation_setting.ReserveModeOff, profilesJSON(t, map[string]map[string]any{"model::protocol-fixture": profile}))
	openAIUsage := &dto.Usage{PromptTokens: 1000, CompletionTokens: 20, TotalTokens: 1020, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 200}}
	for _, tc := range []struct {
		name, path, body string
		format           types.RelayFormat
		mode             int
		request          dto.Request
		usage            *dto.BillingUsage
	}{
		{"chat-json", "/v1/chat/completions", `{"model":"protocol-fixture","messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"a","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"a","content":"result"}],"max_tokens":1024,"max_completion_tokens":512,"n":2,"response_format":{"type":"json_object"},"metadata":{"case":"fixture"}}`, types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions, &dto.GeneralOpenAIRequest{}, dto.NewOpenAIChatBillingUsage(openAIUsage)},
		{"chat-sse", "/v1/chat/completions", `{"model":"protocol-fixture","stream":true,"messages":[{"role":"user","content":"hello"}],"max_completion_tokens":1024,"reasoning_effort":"high"}`, types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions, &dto.GeneralOpenAIRequest{}, dto.NewOpenAIChatBillingUsage(openAIUsage)},
		{"completions", "/v1/completions", `{"model":"protocol-fixture","prompt":"hello","max_tokens":1024}`, types.RelayFormatOpenAI, relayconstant.RelayModeCompletions, &dto.GeneralOpenAIRequest{}, dto.NewOpenAIChatBillingUsage(openAIUsage)},
		{"responses-json", "/v1/responses", `{"model":"protocol-fixture","input":[{"type":"function_call","call_id":"a","name":"lookup","arguments":"{\"key\":\"argument-fixture\"}"},{"type":"function_call_output","call_id":"a","output":"result-fixture"},{"role":"assistant","content":[{"type":"output_text","text":"assistant-fixture","annotations":[]}]}],"max_output_tokens":1024,"reasoning":{"effort":"high"},"text":{"format":{"type":"json_object"}},"store":false,"temperature":0,"top_p":1,"metadata":{"tag":"fixture"}}`, types.RelayFormatOpenAIResponses, relayconstant.RelayModeResponses, &dto.OpenAIResponsesRequest{}, dto.NewOpenAIResponsesBillingUsage(openAIUsage)},
		{"responses-sse", "/v1/responses", `{"model":"protocol-fixture","stream":true,"input":"hello encrypted_content item_reference","max_output_tokens":1024,"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],"include":["reasoning.encrypted_content"]}`, types.RelayFormatOpenAIResponses, relayconstant.RelayModeResponses, &dto.OpenAIResponsesRequest{}, dto.NewOpenAIResponsesBillingUsage(openAIUsage)},
		{"claude-json", "/v1/messages", `{"model":"protocol-fixture","system":"system-fixture","max_tokens":1024,"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"a","name":"lookup","input":{"key":"argument-fixture"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"a","content":"result-fixture"}]}],"metadata":{"user_id":"fixture"},"thinking":{"type":"enabled","budget_tokens":100}}`, types.RelayFormatClaude, relayconstant.RelayModeChatCompletions, &dto.ClaudeRequest{}, dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{InputTokens: 800, CacheReadInputTokens: 200, OutputTokens: 20})},
		{"claude-sse", "/v1/messages", `{"model":"protocol-fixture","stream":true,"max_tokens":1024,"messages":[{"role":"user","content":"hello"}]}`, types.RelayFormatClaude, relayconstant.RelayModeChatCompletions, &dto.ClaudeRequest{}, dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{InputTokens: 800, CacheReadInputTokens: 200, OutputTokens: 20})},
		{"gemini-json", "/v1beta/models/protocol-fixture:generateContent", `{"systemInstruction":{"parts":[{"text":"system-fixture"}]},"contents":[{"role":"model","parts":[{"functionCall":{"name":"lookup","args":{"key":"argument-fixture"}}}]},{"role":"user","parts":[{"functionResponse":{"name":"lookup","response":{"result":"result-fixture"}}}]}],"generationConfig":{"maxOutputTokens":1024},"tools":[{"functionDeclarations":[{"name":"lookup","parameters":{"type":"OBJECT"}}]}]}`, types.RelayFormatGemini, relayconstant.RelayModeGemini, &dto.GeminiChatRequest{}, dto.NewGeminiChatBillingUsage(&dto.GeminiUsageMetadata{PromptTokenCount: 1000, CachedContentTokenCount: 200, CandidatesTokenCount: 20, TotalTokenCount: 1020})},
		{"gemini-sse", "/v1beta/models/protocol-fixture:streamGenerateContent?alt=sse", `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"maxOutputTokens":1024}}`, types.RelayFormatGemini, relayconstant.RelayModeGemini, &dto.GeminiChatRequest{}, dto.NewGeminiChatBillingUsage(&dto.GeminiUsageMetadata{PromptTokenCount: 1000, CachedContentTokenCount: 200, CandidatesTokenCount: 20, TotalTokenCount: 1020})},
		{"embedding", "/v1/embeddings", `{"model":"protocol-fixture","input":"hello"}`, types.RelayFormatEmbedding, relayconstant.RelayModeEmbeddings, &dto.EmbeddingRequest{}, nil},
		{"rerank", "/v1/rerank", `{"model":"protocol-fixture","query":"hello","documents":["document"]}`, types.RelayFormatRerank, relayconstant.RelayModeRerank, &dto.RerankRequest{}, nil},
		{"moderation", "/v1/moderations", `{"model":"protocol-fixture","input":"hello"}`, types.RelayFormatOpenAI, relayconstant.RelayModeModerations, &dto.GeneralOpenAIRequest{}, nil},
		{"audio-speech", "/v1/audio/speech", `{"model":"protocol-fixture","input":"hello","instructions":"speak calmly","voice":"fixture","response_format":"pcm"}`, types.RelayFormatOpenAIAudio, relayconstant.RelayModeAudioSpeech, &dto.AudioRequest{}, nil},
		{"compact-json", "/v1/responses/compact", `{"model":"protocol-fixture","input":[{"role":"user","content":"hello"}],"instructions":"summarize"}`, types.RelayFormatOpenAIResponsesCompaction, relayconstant.RelayModeResponsesCompact, &dto.OpenAIResponsesCompactionRequest{}, dto.NewOpenAIResponsesBillingUsage(openAIUsage)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, common.Unmarshal([]byte(tc.body), tc.request))
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", tc.path, nil)
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "protocol-fixture")
			u := createHardeningUser(t, db, fmt.Sprintf("pc%x", time.Now().UnixNano()), 100000)
			info := &relaycommon.RelayInfo{Request: tc.request, RequestId: fmt.Sprintf("protocol-%d", h2FlowCounter.Add(1)), UserId: u.UserID, TokenId: u.TokenID, OriginModelName: "protocol-fixture", UserGroup: "default", UsingGroup: "default", RelayMode: tc.mode, RelayFormat: tc.format, RequestURLPath: c.Request.URL.Path, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, UpstreamModelName: "protocol-fixture"}}
			info.UserSetting.BillingPreference = "wallet_only"
			require.NoError(t, helper.PrepareBillingRequest(c, info, true))
			meta := tc.request.GetTokenCountMeta()
			if tc.name == "responses-json" || tc.name == "gemini-json" || tc.name == "claude-json" {
				assert.Contains(t, meta.CombineText, "argument-fixture")
				assert.Contains(t, meta.CombineText, "result-fixture")
			}
			require.Nil(t, relayapi.PrepareRequestBilling(c, info))
			body, err := common.Marshal(tc.request)
			require.NoError(t, err)
			require.NoError(t, helper.PrepareOutboundTextBudget(c, info, body))
			reserved := info.Billing.GetPreConsumedQuota()
			expected := 890
			usage := &dto.Usage{BillingUsage: tc.usage}
			if tc.usage == nil {
				expected = info.GetEstimatePromptTokens()
				usage = &dto.Usage{PromptTokens: expected, TotalTokens: expected}
			}
			if tc.name == "audio-speech" {
				expected += 20
				usage.CompletionTokens, usage.TotalTokens = 20, expected
				usage.PromptTokensDetails.TextTokens = usage.PromptTokens
				usage.CompletionTokenDetails.AudioTokens = 20
				service.PostAudioConsumeQuota(c, info, usage, "")
			} else {
				service.PostTextConsumeQuota(c, info, usage, nil)
			}
			require.GreaterOrEqual(t, reserved, expected)
			assert.Equal(t, expected, reservationOf(t, info.RequestId).Applied)
			require.NoError(t, info.Billing.Settle(expected))
			info.Billing.Refund(c)
			assert.Equal(t, walletState{100000 - expected, 100000 - expected}, walletOf(t, u))
			require.NoError(t, model.RecoverBillingCompletionLogs(100))
			require.NoError(t, model.RecoverBillingCompletionLogs(100))
			var logs int64
			require.NoError(t, db.Model(&model.Log{}).Where("request_id = ?", info.RequestId).Count(&logs).Error)
			assert.EqualValues(t, 1, logs)
			// A conversion/override can grow the budget, but must reserve the
			// increase before dispatch. An unaffordable increase never sends.
			info.RequestId += "-raise"
			info.Billing = nil
			require.Nil(t, service.PreConsumeBilling(c, 10, info))
			require.NoError(t, helper.PrepareOutboundTextBudget(c, info, body))
			assert.GreaterOrEqual(t, info.Billing.GetPreConsumedQuota(), 10)
			require.Error(t, info.Billing.Reserve(200000))
			assert.False(t, c.GetBool("upstream_attempted"))
			info.Billing.Refund(c)
			assert.Equal(t, walletState{100000 - expected, 100000 - expected}, walletOf(t, u))
			// The same public failure path releases a pre-dispatch reservation.
			info.RequestId += "-fail"
			info.Billing = nil
			require.Nil(t, service.PreConsumeBilling(c, reserved, info))
			apiErr := types.NewError(fmt.Errorf("fixture failed before dispatch"), types.ErrorCodeInvalidRequest)
			relayapi.RefundFailedRequestBilling(c, info, apiErr)
			relayapi.RefundFailedRequestBilling(c, info, apiErr)
			assert.Equal(t, walletState{100000 - expected, 100000 - expected}, walletOf(t, u))
		})
	}
	// Legacy ratio retries do not yet create a session when the first group is
	// free. Preserve that real accounting boundary rather than admitting a
	// later paid attempt with no reservation.
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"group_ratio_setting.group_ratio": `{"default":0}`}))
	operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = false
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	limit := uint(100)
	info := &relaycommon.RelayInfo{Request: &dto.GeneralOpenAIRequest{Model: "protocol-fixture", Messages: []dto.Message{{Role: "user", Content: "hello"}}, MaxTokens: &limit}, OriginModelName: "protocol-fixture", TokenGroup: "auto", UserGroup: "default", UsingGroup: "default", RelayMode: relayconstant.RelayModeChatCompletions, RelayFormat: types.RelayFormatOpenAI}
	apiErr := relayapi.PrepareRequestBilling(c, info)
	require.NotNil(t, apiErr)
	assert.Contains(t, apiErr.Error(), "free-to-paid")
	assert.Nil(t, info.Billing)
}

func TestProtocolBillingUnmeasuredInputs(t *testing.T) {
	t.Setenv("CLEARAPI_BILLING_PROTECTION", "true")
	setPolicy(t, operation_setting.ReserveModeOff, "{}")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	for _, tc := range []struct {
		name, body, reason string
		request            dto.Request
	}{
		{"previous response", `{"input":"hello","previous_response_id":"resp_1"}`, "previous_response_id", &dto.OpenAIResponsesRequest{}},
		{"encrypted history", `{"input":[{"type":"reasoning","encrypted_content":"opaque"}]}`, "encrypted_content", &dto.OpenAIResponsesRequest{}},
		{"item reference", `{"input":[{"type":"item_reference","id":"msg_1"}]}`, "item_reference", &dto.OpenAIResponsesRequest{}},
		{"cached context", `{"cachedContent":"cachedContents/fixture"}`, "cachedContent", &dto.GeminiChatRequest{}},
		{"MCP", `{"mcp_servers":[{"type":"url","url":"https://fixture.invalid"}]}`, "MCP", &dto.ClaudeRequest{}},
		{"compact budget", `{"input":"hello"}`, "model_output_max", &dto.OpenAIResponsesCompactionRequest{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, common.Unmarshal([]byte(tc.body), tc.request))
			info := &relaycommon.RelayInfo{Request: tc.request, OriginModelName: "protocol-fixture"}
			require.ErrorContains(t, helper.PrepareBillingRequest(c, info, true), tc.reason)
			assert.Nil(t, info.Billing)
			assert.False(t, c.GetBool("upstream_attempted"))
		})
	}
	profile := coldProfile(100)
	profile["model_context_max"], profile["model_output_max"] = 16384, 1024
	setPolicy(t, operation_setting.ReserveModeEnforce, profilesJSON(t, map[string]map[string]any{"model::protocol-fixture": profile}))
	r := &dto.OpenAIResponsesRequest{Model: "protocol-fixture", Input: []byte(`"hello"`), PreviousResponseID: "resp_1"}
	info := &relaycommon.RelayInfo{Request: r, OriginModelName: r.Model}
	require.NoError(t, helper.PrepareBillingRequest(c, info, true))
	require.NotNil(t, r.MaxOutputTokens)
	assert.EqualValues(t, 1024, *r.MaxOutputTokens)
	// Outbound validation cannot silently assume a cap that is absent on wire.
	r.MaxOutputTokens = nil
	require.NoError(t, helper.PrepareBillingRequest(c, info, false))
	assert.Nil(t, r.MaxOutputTokens)
	r.ServiceTier = "priority"
	require.ErrorContains(t, helper.PrepareBillingRequest(c, info, false), "service_tier")
}
