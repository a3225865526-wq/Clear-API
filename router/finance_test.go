package router_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/router"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func financeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	var dialector gorm.Dialector = sqlite.Open(":memory:")
	dialect := os.Getenv("FINANCE_TEST_DIALECT")
	dsn := os.Getenv("FINANCE_TEST_DSN")
	switch dialect {
	case "mysql":
		dialector = mysql.Open(dsn)
	case "postgres":
		dialector = postgres.Open(dsn)
	case "", "sqlite":
	default:
		t.Fatal("unsupported test dialect")
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	return db
}

func TestFinanceStageOne(t *testing.T) {
	db := financeTestDB(t)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousType := common.MainDatabaseType()
	previousRedis, previousMemory := common.RedisEnabled, common.MemoryCacheEnabled
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled, common.MemoryCacheEnabled = false, false
	switch os.Getenv("FINANCE_TEST_DIALECT") {
	case "mysql":
		common.SetMainDatabaseType(common.DatabaseTypeMySQL)
	case "postgres":
		common.SetMainDatabaseType(common.DatabaseTypePostgreSQL)
	default:
		common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	}
	t.Setenv("CLEARAPI_FINANCE_ENABLED", "true")
	t.Setenv("CLEARAPI_FINANCE_OWNER_USER_ID", "5")
	common.InitFinanceConfig()
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled, common.MemoryCacheEnabled = previousRedis, previousMemory
		common.SetMainDatabaseType(previousType)
	})
	gin.SetMode(gin.TestMode)
	// Existing-table fixture exercises additive upgrades and unchanged balances.
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.AuditLog{}, &model.Log{}, &model.Option{}))
	for _, user := range []struct {
		id, role, status int
		token            string
	}{
		{5, common.RoleRootUser, common.UserStatusEnabled, "synthetic-owner-pat"},
		{6, common.RoleRootUser, common.UserStatusEnabled, "synthetic-other-root-pat"},
		{7, common.RoleAdminUser, common.UserStatusEnabled, "synthetic-admin-pat"},
		{8, common.RoleCommonUser, common.UserStatusEnabled, "synthetic-user-pat"},
		{9, common.RoleRootUser, common.UserStatusDisabled, "synthetic-disabled-pat"},
	} {
		token := user.token
		require.NoError(t, db.Create(&model.User{Id: user.id, Username: fmt.Sprintf("synthetic-%d", user.id), Password: "synthetic-unused", Role: user.role, Status: user.status, Quota: 1234567, AuthVersion: 1, AccessToken: &token, AffCode: fmt.Sprintf("synthetic-aff-%d", user.id)}).Error)
	}
	api := gin.New()
	router.RegisterFinanceRoutes(api.Group("/api"))
	request := func(method, path, token string, body any) *httptest.ResponseRecorder {
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
	owner := "synthetic-owner-pat"
	t.Run("dashboard session validation and revocation", func(t *testing.T) {
		previousSecret := common.SessionSecret
		common.SessionSecret = "synthetic-finance-signing-secret"
		defer func() { common.SessionSecret = previousSecret }()
		now := time.Now().Unix()
		session := &model.UserSession{SID: "synthetic-finance-session", UserID: 5, Version: 1, UserAuthVersion: 1, Status: model.UserSessionStatusActive, RefreshHash: "synthetic-unused-digest", LoginMethod: "password", LastActiveAt: now, ExpiresAt: now + 3600}
		require.NoError(t, db.Create(session).Error)
		token, _, err := service.IssueAccessToken(service.AuthIdentity{UserID: 5, SessionID: session.SID, UserAuthVersion: 1, SessionVersion: 1})
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, request("GET", "/api/owner/finance/capability", token, nil).Code)
		require.NoError(t, db.Model(session).Update("status", model.UserSessionStatusRevoked).Error)
		assert.Equal(t, http.StatusUnauthorized, request("GET", "/api/owner/finance/capability", token, nil).Code)
	})
	t.Run("owner requires explicit migration", func(t *testing.T) {
		assert.ErrorIs(t, model.FinanceReady(db), model.ErrFinanceUnavailable)
		assert.Equal(t, http.StatusServiceUnavailable, request("GET", "/api/owner/finance/expenses", owner, nil).Code)
		assert.False(t, db.Migrator().HasTable(&model.FinanceExpense{}))
	})
	t.Run("owner revenue preview is stateless and uncached", func(t *testing.T) {
		body := map[string]any{"events": []map[string]any{
			{"key": "package", "at": 1, "kind": "credit", "quota": 120, "source": "paid", "value_cny": "60"},
			{"key": "usage", "at": 2, "kind": "consume", "quota": 30},
		}}
		response := request("POST", "/api/owner/finance/revenue/preview", owner, body)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var decoded struct {
			Data service.FinanceRevenueResult `json:"data"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &decoded))
		assert.True(t, decoded.Data.Simulation)
		assert.Equal(t, "15", decoded.Data.KnownConsumptionCNY)
		assert.Contains(t, response.Header().Get("Cache-Control"), "no-store")
		assert.False(t, db.Migrator().HasTable(&model.FinanceExpense{}))
		var ownerRecord model.User
		require.NoError(t, db.First(&ownerRecord, 5).Error)
		assert.Equal(t, 1234567, ownerRecord.Quota)
		assert.Equal(t, http.StatusBadRequest, request("POST", "/api/owner/finance/revenue/preview", owner, map[string]any{"events": []any{}}).Code)
	})
	t.Run("all routes deny nonowners before schema access", func(t *testing.T) {
		for _, endpoint := range []struct{ method, path string }{
			{"GET", "/capability"}, {"GET", "/versions/cost"}, {"POST", "/versions/cost"},
			{"GET", "/versions/fx"}, {"POST", "/versions/fx"}, {"GET", "/versions/pool"}, {"POST", "/versions/pool"},
			{"GET", "/expenses"}, {"POST", "/expenses"}, {"GET", "/expenses/summary?from=1&to=2"}, {"GET", "/audit"},
			{"POST", "/revenue/preview"},
			{"POST", "/wallets/5/events"}, {"GET", "/wallets/5/events"}, {"GET", "/wallets/5/summary"}, {"GET", "/wallets/5/allocations?event_id=1"},
			{"GET", "/request-events"}, {"GET", "/capture-health"},
			{"POST", "/cash-receipts"}, {"GET", "/cash-receipts"}, {"GET", "/cash-receipts/summary?from=1&to=2"},
			{"POST", "/wallets/5/offline-sources/preview"}, {"POST", "/wallets/5/offline-sources/import"},
			{"POST", "/wallets/5/consumptions/preview"}, {"POST", "/wallets/5/consumptions/import"}, {"GET", "/wallets/5/balance-check"},
		} {
			for _, actor := range []struct {
				token  string
				status int
			}{{"", 401}, {"synthetic-other-root-pat", 403}, {"synthetic-admin-pat", 403}, {"synthetic-user-pat", 403}, {"synthetic-disabled-pat", 401}, {"sk-synthetic-api-key", 401}} {
				result := request(endpoint.method, "/api/owner/finance"+endpoint.path, actor.token, map[string]any{"id": 5})
				assert.Equal(t, actor.status, result.Code, "%s %s", endpoint.method, endpoint.path)
				assert.NotContains(t, result.Body.String(), "payload")
				assert.NotContains(t, result.Body.String(), "amount")
				assert.Contains(t, result.Header().Get("Cache-Control"), "no-store")
			}
		}
	})
	require.NoError(t, model.MigrateFinance(db))
	t.Run("append versions preserve history and audit", func(t *testing.T) {
		body := map[string]any{"record_key": "usd-cny", "expected_id": 0, "effective_from": 100, "effective_to": 2000000000, "active": true, "source": "synthetic fixed rate", "payload": map[string]any{"currency": "USD", "cny_per_unit": "7", "api_key": "discarded-test-field"}}
		created := request("POST", "/api/owner/finance/versions/fx", owner, body)
		require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
		assert.NotContains(t, created.Body.String(), "discarded-test-field")
		var first model.FinanceVersion
		require.NoError(t, db.Where("kind = ?", "fx").First(&first).Error)
		body["expected_id"], body["effective_from"] = first.ID, 200
		body["payload"] = map[string]any{"currency": "USD", "cny_per_unit": "9"}
		require.Equal(t, http.StatusCreated, request("POST", "/api/owner/finance/versions/fx", owner, body).Code)
		assert.Equal(t, http.StatusConflict, request("POST", "/api/owner/finance/versions/fx", owner, body).Code)
		var retained model.FinanceVersion
		require.NoError(t, db.First(&retained, first.ID).Error)
		assert.Equal(t, first.Payload, retained.Payload)
		rate, err := service.FinanceFrozenRate(db, first.ID, "USD", 300)
		require.NoError(t, err)
		assert.Equal(t, "7", rate.String())
		var auditCount int64
		require.NoError(t, db.Model(&model.FinanceAudit{}).Count(&auditCount).Error)
		assert.EqualValues(t, 2, auditCount)
		var generalCount int64
		require.NoError(t, db.Model(&model.AuditLog{}).Count(&generalCount).Error)
		assert.Zero(t, generalCount)
	})
	t.Run("input validation cannot store credential payloads or invalid numbers", func(t *testing.T) {
		for _, rate := range []string{"NaN", "Inf", "-1", "0", "1e999", "0.0000000000001"} {
			body := map[string]any{"record_key": "invalid", "effective_from": 100, "effective_to": 1000, "active": true, "source": "test", "payload": map[string]any{"currency": "USD", "cny_per_unit": rate}}
			assert.Equal(t, http.StatusBadRequest, request("POST", "/api/owner/finance/versions/fx", owner, body).Code)
		}
	})
	t.Run("cost input output cache and unknown usage", func(t *testing.T) {
		prices := []service.FinanceUnitPrice{{"input", "token", "1000000", "2"}, {"output", "token", "1000000", "8"}, {"cache_read", "token", "1000000", "0.5"}, {"cache_write_5m", "token", "1000000", "3"}}
		rule := service.FinanceCostRule{ChannelID: 10, UpstreamModel: "executed-model", Currency: "USD", Prices: prices}
		payload, err := common.Marshal(rule)
		require.NoError(t, err)
		entry := model.FinanceVersion{Kind: "cost", RecordKey: "channel10-model", CreatedBy: 5, EffectiveFrom: 100, EffectiveTo: 1000, Active: true, Source: "test purchase quote"}
		require.NoError(t, service.ValidateFinanceVersion(&entry, payload))
		input, output, read, write := "1000", "500", "200", "100"
		usage := map[string]*string{"input": &input, "output": &output, "cache_read": &read, "cache_write_5m": &write}
		result, err := service.EstimateFinanceCost(rule, usage)
		require.NoError(t, err)
		require.NotNil(t, result.Amount)
		assert.Equal(t, "0.0064", *result.Amount)
		assert.Equal(t, "estimated", result.Status)
		// A failed attempt with known usage has exactly this cost too; no quota
		// or successful-user-charge condition participates in the function.
		delete(usage, "output")
		result, err = service.EstimateFinanceCost(rule, usage)
		require.NoError(t, err)
		assert.Nil(t, result.Amount)
		assert.Equal(t, "unknown", result.Status)
		assert.Equal(t, []string{"output"}, result.Missing)
	})
	t.Run("pool expense FX missing rate and adjustments", func(t *testing.T) {
		pool := map[string]any{"record_key": "pool-a", "effective_from": 100, "effective_to": 2000000000, "active": true, "source": "test pool", "payload": map[string]any{"name": "Synthetic pool", "platform": "test", "channel_ids": []int{10}}}
		require.Equal(t, http.StatusCreated, request("POST", "/api/owner/finance/versions/pool", owner, pool).Code)
		var fx model.FinanceVersion
		require.NoError(t, db.Where("kind = ?", "fx").Order("id").First(&fx).Error)
		entries := []model.FinanceExpense{
			{EventKey: "pool-fixed", Ledger: "direct", Category: "subscription", PoolKey: "pool-a", AccountRef: "internal-account-1", Amount: "10", Currency: "USD", FXVersionID: fx.ID, PeriodFrom: 300, PeriodTo: 400, Recurrence: "once", Source: "test invoice"},
			{EventKey: "server", Ledger: "operating", Category: "server", Amount: "30", Currency: "CNY", PeriodFrom: 300, PeriodTo: 400, Recurrence: "once", Source: "test invoice"},
			{EventKey: "unknown-fx", Ledger: "direct", Category: "variable", Amount: "5", Currency: "EUR", PeriodFrom: 300, PeriodTo: 400, Recurrence: "once", Source: "test invoice"},
		}
		for _, expense := range entries {
			require.Equal(t, http.StatusCreated, request("POST", "/api/owner/finance/expenses", owner, expense).Code)
		}
		var original model.FinanceExpense
		require.NoError(t, db.Where("event_key = ?", "pool-fixed").First(&original).Error)
		adjustment := original
		adjustment.ID, adjustment.EventKey, adjustment.Amount, adjustment.AdjustsID, adjustment.Note = 0, "pool-refund", "-2", original.ID, "test supplier refund"
		require.Equal(t, http.StatusCreated, request("POST", "/api/owner/finance/expenses", owner, adjustment).Code)
		result, err := service.SummarizeFinanceExpenses(db, 300, 400)
		require.NoError(t, err)
		assert.Equal(t, "56", result.DirectCNY)
		assert.Equal(t, "30", result.OperatingCNY)
		assert.Equal(t, 1, result.UnknownFXCount)
		assert.Equal(t, "partial", result.Status)
		assert.Contains(t, result.Warnings, "request_costs_not_connected")
		// A duplicate fee is rejected rather than counted a second time.
		assert.NotEqual(t, http.StatusCreated, request("POST", "/api/owner/finance/expenses", owner, entries[0]).Code)
		var retained model.FinanceExpense
		require.NoError(t, db.First(&retained, original.ID).Error)
		assert.Equal(t, "10", retained.Amount)
	})
	t.Run("migration twice preserves financial and existing records", func(t *testing.T) {
		var before int64
		require.NoError(t, db.Model(&model.FinanceExpense{}).Count(&before).Error)
		require.NoError(t, model.MigrateFinance(db))
		var after int64
		require.NoError(t, db.Model(&model.FinanceExpense{}).Count(&after).Error)
		assert.Equal(t, before, after)
		var user model.User
		require.NoError(t, db.First(&user, 5).Error)
		assert.Equal(t, 1234567, user.Quota)
	})
	t.Run("audit failure rolls back financial write", func(t *testing.T) {
		callbackName := "synthetic_finance_audit_failure"
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
			if tx.Statement.Table == "finance_audits" {
				tx.AddError(fmt.Errorf("synthetic audit failure"))
			}
		}))
		defer func() { require.NoError(t, db.Callback().Create().Remove(callbackName)) }()
		body := model.FinanceExpense{EventKey: "audit-rollback", Ledger: "operating", Category: "server", Amount: "10", Currency: "CNY", PeriodFrom: 300, PeriodTo: 400, Recurrence: "once", Source: "test invoice"}
		assert.Equal(t, http.StatusServiceUnavailable, request("POST", "/api/owner/finance/expenses", owner, body).Code)
		var count int64
		require.NoError(t, db.Model(&model.FinanceExpense{}).Where("event_key = ?", body.EventKey).Count(&count).Error)
		assert.Zero(t, count)
	})
	t.Run("negative pagination cannot disable the limit", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, request("GET", "/api/owner/finance/expenses?page_size=-1", owner, nil).Code)
	})
	t.Run("other roots cannot take over the owner account", func(t *testing.T) {
		protected := gin.New()
		admin := protected.Group("/api/user", middleware.AdminAuth(), middleware.ProtectFinanceOwnerAccount())
		for _, path := range []string{"/:id", "/:id/reset_passkey", "/:id/2fa", "/:id/bindings/:binding_type", "/:id/oauth/bindings/:provider_id"} {
			admin.DELETE(path, func(c *gin.Context) { c.Status(http.StatusNoContent) })
		}
		admin.PUT("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		admin.POST("/manage", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		for _, endpoint := range []struct{ method, path, body string }{{"DELETE", "/api/user/5", ""}, {"DELETE", "/api/user/5/reset_passkey", ""}, {"DELETE", "/api/user/5/2fa", ""}, {"DELETE", "/api/user/5/bindings/github", ""}, {"DELETE", "/api/user/5/oauth/bindings/1", ""}, {"PUT", "/api/user/", `{"id":5}`}, {"POST", "/api/user/manage", `{"id":5,"action":"promote"}`}} {
			req := httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader(endpoint.body))
			req.Header.Set("Authorization", "Bearer synthetic-other-root-pat")
			result := httptest.NewRecorder()
			protected.ServeHTTP(result, req)
			assert.Equal(t, http.StatusForbidden, result.Code, endpoint.path)
		}
	})
	t.Run("general log interfaces expose no financial payload", func(t *testing.T) {
		ordinary := gin.New()
		ordinary.GET("/logs", middleware.AdminAuth(), controller.GetAllLogs)
		req := httptest.NewRequest("GET", "/logs", nil)
		req.Header.Set("Authorization", "Bearer synthetic-other-root-pat")
		result := httptest.NewRecorder()
		ordinary.ServeHTTP(result, req)
		require.Equal(t, http.StatusOK, result.Code)
		assert.NotContains(t, result.Body.String(), "cny_per_unit")
		assert.NotContains(t, result.Body.String(), "pool-fixed")
		assert.NotContains(t, result.Body.String(), "Synthetic pool")
	})
	t.Run("configured identity cannot change through live settings", func(t *testing.T) {
		require.NoError(t, db.Create(&model.Option{Key: "CLEARAPI_FINANCE_OWNER_USER_ID", Value: "6"}).Error)
		t.Setenv("CLEARAPI_FINANCE_OWNER_USER_ID", "6")
		assert.True(t, common.IsFinanceOwner(5))
		assert.False(t, common.IsFinanceOwner(6))
		assert.Equal(t, http.StatusOK, request("GET", "/api/owner/finance/capability", owner, nil).Code)
	})
	t.Run("durable FIFO ledger is additive auditable and isolated from billing", func(t *testing.T) {
		assert.Equal(t, http.StatusServiceUnavailable, request("GET", "/api/owner/finance/wallets/8/summary", owner, nil).Code)
		assert.False(t, db.Migrator().HasTable(&model.FinanceWallet{}))
		require.NoError(t, model.MigrateFinanceRevenue(db))
		require.NoError(t, model.MigrateFinanceRevenue(db))
		value := func(s string) *string { return &s }
		facts := []service.FinanceRevenueEvent{
			{Key: "opening", At: 1, Kind: "credit", Source: "unknown", Quota: 50},
			{Key: "package", At: 2, Kind: "credit", Source: "paid", Quota: 120, ValueCNY: value("60")},
			{Key: "gift", At: 3, Kind: "credit", Source: "gift", Quota: 20, ValueCNY: value("0")},
			{Key: "usage", At: 4, Kind: "consume", Quota: 80},
			{Key: "undo", At: 5, Kind: "reverse", Quota: 80, Reverses: "usage"},
			{Key: "usage-again", At: 6, Kind: "consume", Quota: 80},
		}
		var recorded []model.FinanceRevenueRecord
		for i, event := range facts {
			input := service.FinanceRevenueWrite{ExpectedRevision: int64(i), Event: event, Evidence: "synthetic source fixture"}
			response := request("POST", "/api/owner/finance/wallets/8/events", owner, input)
			require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
			var decoded struct {
				Data model.FinanceRevenueRecord `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &decoded))
			assert.Equal(t, 5, decoded.Data.CreatedBy)
			recorded = append(recorded, decoded.Data)
		}
		preview, err := service.PreviewFinanceRevenue(service.FinanceRevenuePreview{Events: facts})
		require.NoError(t, err)
		summary, err := service.SummarizeFinanceWallet(db, 8)
		require.NoError(t, err)
		assert.Equal(t, preview.KnownConsumptionCNY, summary.Wallet.KnownRevenueCNY)
		assert.Equal(t, preview.KnownBackingValueCNY, summary.Wallet.KnownBackingCNY)
		assert.Equal(t, preview.UnknownConsumedQuota, summary.Wallet.UnknownConsumedQuota)
		assert.Equal(t, preview.RemainingQuota, summary.Wallet.RemainingQuota)
		assert.Nil(t, summary.ConsumptionCNY)
		assert.Nil(t, summary.BackingCNY)
		assert.Equal(t, "partial", summary.Status)
		assert.Equal(t, "manual_facts_only", summary.Coverage)
		assert.Equal(t, http.StatusOK, request("GET", "/api/owner/finance/wallets/8/summary", owner, nil).Code)
		allocations := request("GET", fmt.Sprintf("/api/owner/finance/wallets/8/allocations?event_id=%d", recorded[3].ID), owner, nil)
		require.Equal(t, http.StatusOK, allocations.Code)
		var parts []model.FinanceRevenueAllocation
		require.NoError(t, db.Where("event_id = ?", recorded[3].ID).Order("id asc").Find(&parts).Error)
		require.Len(t, parts, 2)
		assert.Nil(t, parts[0].ValueCNY)
		assert.Equal(t, int64(50), parts[0].Quota)
		assert.Equal(t, "15", *parts[1].ValueCNY) // original remains intact after reversal
		assert.Equal(t, http.StatusBadRequest, request("GET", fmt.Sprintf("/api/owner/finance/wallets/7/allocations?event_id=%d", recorded[3].ID), owner, nil).Code)
		other := request("GET", "/api/owner/finance/wallets/7/events", owner, nil)
		require.Equal(t, http.StatusOK, other.Code)
		assert.NotContains(t, other.Body.String(), "package")
		absent, err := service.SummarizeFinanceWallet(db, 7)
		require.NoError(t, err)
		assert.Nil(t, absent.Wallet)
		assert.Nil(t, absent.ConsumptionCNY)
		stale := service.FinanceRevenueWrite{ExpectedRevision: 5, Event: service.FinanceRevenueEvent{Key: "stale", At: 7, Kind: "consume", Quota: 1}, Evidence: "synthetic"}
		assert.Equal(t, http.StatusConflict, request("POST", "/api/owner/finance/wallets/8/events", owner, stale).Code)
		duplicate := service.FinanceRevenueWrite{ExpectedRevision: 6, Event: facts[5], Evidence: "synthetic duplicate"}
		assert.Equal(t, http.StatusConflict, request("POST", "/api/owner/finance/wallets/8/events", owner, duplicate).Code)
		invalid := service.FinanceRevenueWrite{ExpectedRevision: 6, Event: service.FinanceRevenueEvent{Key: "partial", At: 7, Kind: "reverse", Quota: 1, Reverses: "usage-again"}, Evidence: "synthetic partial"}
		assert.Equal(t, http.StatusBadRequest, request("POST", "/api/owner/finance/wallets/8/events", owner, invalid).Code)
		invalid.Event.Key, invalid.Event.Quota, invalid.Event.Reverses = "double-reversal", 80, "usage"
		assert.Equal(t, http.StatusConflict, request("POST", "/api/owner/finance/wallets/8/events", owner, invalid).Code)
		var count int64
		require.NoError(t, db.Model(&model.FinanceRevenueRecord{}).Where("user_id = ?", 8).Count(&count).Error)
		assert.Equal(t, int64(6), count)
		callbackName := "synthetic_revenue_audit_failure"
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
			if tx.Statement.Table == "finance_audits" {
				tx.AddError(fmt.Errorf("synthetic audit failure"))
			}
		}))
		rollback := service.FinanceRevenueWrite{ExpectedRevision: 6, Event: service.FinanceRevenueEvent{Key: "rollback", At: 7, Kind: "consume", Quota: 10}, Evidence: "synthetic"}
		failed := request("POST", "/api/owner/finance/wallets/8/events", owner, rollback)
		require.NoError(t, db.Callback().Create().Remove(callbackName))
		assert.Equal(t, http.StatusServiceUnavailable, failed.Code)
		after, err := service.SummarizeFinanceWallet(db, 8)
		require.NoError(t, err)
		assert.Equal(t, summary.Wallet, after.Wallet)
		require.NoError(t, db.Model(&model.FinanceRevenueRecord{}).Where("user_id = ?", 8).Count(&count).Error)
		assert.Equal(t, int64(6), count)
		var audits int64
		require.NoError(t, db.Model(&model.FinanceAudit{}).Where("action = ?", "revenue.append").Count(&audits).Error)
		assert.Equal(t, int64(6), audits)
		require.NoError(t, model.MigrateFinance(db)) // old approval cannot downgrade v2
		require.NoError(t, model.MigrateFinanceRevenue(db))
		require.NoError(t, model.FinanceRevenueReady(db))
		var user model.User
		require.NoError(t, db.First(&user, 8).Error)
		assert.Equal(t, 1234567, user.Quota)
		assert.Equal(t, http.StatusBadRequest, request("GET", "/api/owner/finance/wallets/8/events?page_size=-1", owner, nil).Code)
		logs, _, err := model.GetAllLogs(model.LogTypeUnknown, 0, time.Now().Unix()+3600, "", "", "", 0, 100, 0, "", "", "")
		require.NoError(t, err)
		publicData, err := common.Marshal(logs)
		require.NoError(t, err)
		assert.NotContains(t, string(publicData), "synthetic source fixture")
		assert.NotContains(t, string(publicData), "known_backing_cny")
	})
	t.Run("durable rounding and stale concurrent writers", func(t *testing.T) {
		value := "1"
		facts := []service.FinanceRevenueEvent{
			{Key: "thirds", At: 1, Kind: "credit", Source: "paid", Quota: 3, ValueCNY: &value},
			{Key: "a", At: 2, Kind: "consume", Quota: 1},
			{Key: "b", At: 3, Kind: "consume", Quota: 1},
			{Key: "undo-a", At: 4, Kind: "reverse", Quota: 1, Reverses: "a"},
			{Key: "final", At: 5, Kind: "consume", Quota: 2},
		}
		for i, event := range facts {
			_, err := service.AppendFinanceRevenue(db, 7, 5, service.FinanceRevenueWrite{ExpectedRevision: int64(i), Event: event, Evidence: "synthetic thirds"}, "synthetic")
			require.NoError(t, err)
		}
		summary, err := service.SummarizeFinanceWallet(db, 7)
		require.NoError(t, err)
		assert.Equal(t, "1", *summary.ConsumptionCNY)
		assert.Equal(t, int64(0), summary.Wallet.RemainingQuota)
		var lot model.FinanceRevenueLot
		require.NoError(t, db.Where("user_id = ?", 7).First(&lot).Error)
		assert.Equal(t, "1", lot.RecognizedCNY)
		// Two writers using the same existing revision may not both commit.
		if dialect := os.Getenv("FINANCE_TEST_DIALECT"); dialect == "mysql" || dialect == "postgres" {
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(2)
			defer sqlDB.SetMaxOpenConns(1)
		}
		results := make(chan error, 2)
		for _, key := range []string{"concurrent-one", "concurrent-two"} {
			go func(eventKey string) {
				zero := "0"
				_, err := service.AppendFinanceRevenue(db, 7, 5, service.FinanceRevenueWrite{ExpectedRevision: 5, Event: service.FinanceRevenueEvent{Key: eventKey, At: 6, Kind: "credit", Source: "gift", Quota: 1, ValueCNY: &zero}, Evidence: "synthetic concurrent"}, "synthetic")
				results <- err
			}(key)
		}
		var committed, conflicts int
		for range 2 {
			err := <-results
			if err == nil {
				committed++
			} else {
				assert.ErrorIs(t, err, model.ErrFinanceConflict)
				conflicts++
			}
		}
		assert.Equal(t, 1, committed)
		assert.Equal(t, 1, conflicts)
		after, err := service.SummarizeFinanceWallet(db, 7)
		require.NoError(t, err)
		assert.Equal(t, int64(6), after.Wallet.Revision)
		assert.Equal(t, int64(1), after.Wallet.RemainingQuota)
	})
	t.Run("real transport capture freezes cost and leaves billing unchanged", func(t *testing.T) {
		assert.Equal(t, http.StatusServiceUnavailable, request("GET", "/api/owner/finance/request-events", owner, nil).Code)
		require.NoError(t, model.MigrateFinanceRequests(db))
		require.NoError(t, model.MigrateFinanceRequests(db))
		// These are synthetic records; the HTTP server has no external network.
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/failed" {
				w.WriteHeader(500)
				return
			}
			w.WriteHeader(200)
			_, _ = w.Write([]byte("{}"))
		}))
		defer server.Close()
		now := time.Now().Unix()
		rule := service.FinanceCostRule{ChannelID: 21, UpstreamModel: "supplier-real-model", Currency: "USD", Prices: []service.FinanceUnitPrice{
			{Meter: "input", Unit: "token", Per: "1000000", Price: "2"},
			{Meter: "output", Unit: "token", Per: "1000000", Price: "8"},
			{Meter: "cache_read", Unit: "token", Per: "1000000", Price: "0.5"},
			{Meter: "cache_write", Unit: "token", Per: "1000000", Price: "3"},
		}}
		raw, err := common.Marshal(rule)
		require.NoError(t, err)
		version := model.FinanceVersion{Kind: "cost", RecordKey: "transport-cost", EffectiveFrom: now - 100, EffectiveTo: now + 10000, Active: true, Source: "synthetic procurement", CreatedBy: 5}
		require.NoError(t, service.ValidateFinanceVersion(&version, raw))
		require.NoError(t, model.AppendFinanceVersion(db, &version, 0, "synthetic"))
		info := &relaycommon.RelayInfo{RequestId: "synthetic-real-transport", UserId: 8, OriginModelName: "public-finance-model", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 21, UpstreamModelName: "supplier-real-model", ApiKey: "synthetic-do-not-copy-key"}}
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader("synthetic-do-not-copy-body"))
		upstreamReq, err := http.NewRequest("POST", server.URL, strings.NewReader("{}"))
		require.NoError(t, err)
		response, err := channel.DoRequest(ctx, upstreamReq, info)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		var start model.FinanceRequestEvent
		require.NoError(t, db.Where("request_id = ? AND phase = ?", info.RequestId, "attempt_start").First(&start).Error)
		assert.Equal(t, 21, start.ChannelID)
		assert.Equal(t, "supplier-real-model", start.UpstreamModel)
		var snapshot service.FinanceAttemptSnapshot
		require.NoError(t, common.UnmarshalJsonStr(start.Payload, &snapshot))
		assert.Equal(t, version.ID, snapshot.CostVersionID)
		require.Positive(t, snapshot.FXVersionID)
		// Change procurement and FX after the send: its existing snapshot wins.
		rule.Prices[0].Price = "200"
		raw, err = common.Marshal(rule)
		require.NoError(t, err)
		next := version
		next.EffectiveFrom = now - 1
		require.NoError(t, service.ValidateFinanceVersion(&next, raw))
		require.NoError(t, model.AppendFinanceVersion(db, &next, version.ID, "synthetic"))
		var oldFX model.FinanceVersion
		require.NoError(t, db.First(&oldFX, snapshot.FXVersionID).Error)
		nextFX := oldFX
		nextFX.EffectiveFrom = now - 1
		require.NoError(t, service.ValidateFinanceVersion(&nextFX, []byte(`{"currency":"USD","cny_per_unit":"11"}`)))
		require.NoError(t, model.AppendFinanceVersion(db, &nextFX, oldFX.ID, "synthetic"))
		usage := &dto.Usage{PromptTokens: 1000, CompletionTokens: 500, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 200, CachedCreationTokens: 100}}
		service.CaptureFinanceTextUsage(ctx, info, usage)
		service.FinishFinanceAttempt(ctx, info, false)
		var used model.FinanceRequestEvent
		require.NoError(t, db.Where("attempt_id = ? AND phase = ?", start.AttemptID, "usage").First(&used).Error)
		var estimate service.FinanceUsageSnapshot
		require.NoError(t, common.UnmarshalJsonStr(used.Payload, &estimate))
		require.NotNil(t, estimate.Estimate.Amount)
		assert.Equal(t, "0.0058", *estimate.Estimate.Amount)
		require.NotNil(t, estimate.CostCNY)
		assert.Equal(t, "0.0522", *estimate.CostCNY) // captured USD/CNY=9, not new 11
		// Failed attempts are retained with unknown cost, not charged zero.
		info.ChannelId, info.UpstreamModelName = 22, "failed-supplier-model"
		info.RetryIndex = 1
		ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader("{}"))
		upstreamReq, err = http.NewRequest("POST", server.URL+"/failed", strings.NewReader("{}"))
		require.NoError(t, err)
		response, err = channel.DoRequest(ctx, upstreamReq, info)
		require.NoError(t, err)
		assert.Equal(t, 500, response.StatusCode)
		require.NoError(t, response.Body.Close())
		service.CaptureFinanceTextUsage(ctx, info, nil)
		service.FinishFinanceAttempt(ctx, info, true)
		var starts int64
		require.NoError(t, db.Model(&model.FinanceRequestEvent{}).Where("request_id = ? AND phase = ?", info.RequestId, "attempt_start").Count(&starts).Error)
		assert.Equal(t, int64(2), starts)
		used = model.FinanceRequestEvent{}
		require.NoError(t, db.Where("request_id = ? AND channel_id = ? AND phase = ?", info.RequestId, 22, "usage").First(&used).Error)
		require.NoError(t, common.UnmarshalJsonStr(used.Payload, &estimate))
		assert.Nil(t, estimate.Estimate.Amount)
		assert.Nil(t, estimate.CostCNY)
		assert.Equal(t, "unknown", estimate.Estimate.Status)
		var events []model.FinanceRequestEvent
		require.NoError(t, db.Where("request_id = ?", info.RequestId).Find(&events).Error)
		for _, event := range events {
			assert.NotContains(t, event.Payload, "synthetic-do-not-copy-key")
			assert.NotContains(t, event.Payload, "synthetic-do-not-copy-body")
		}
		ownerView := request("GET", "/api/owner/finance/request-events?request_id="+info.RequestId, owner, nil)
		require.Equal(t, http.StatusOK, ownerView.Code)
		assert.Contains(t, ownerView.Body.String(), "0.0522")
		assert.Contains(t, ownerView.Header().Get("Cache-Control"), "no-store")
		// An observation write failure must not make the transport fail.
		failuresBefore := service.FinanceCaptureHealth()["capture_failures_since_process_start"].(int64)
		callback := "synthetic_capture_failure"
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
			if tx.Statement.Table == "finance_request_events" {
				tx.AddError(fmt.Errorf("synthetic capture failure"))
			}
		}))
		ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader("{}"))
		upstreamReq, err = http.NewRequest("POST", server.URL, strings.NewReader("{}"))
		require.NoError(t, err)
		response, err = channel.DoRequest(ctx, upstreamReq, info)
		require.NoError(t, db.Callback().Create().Remove(callback))
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		assert.Equal(t, 200, response.StatusCode)
		assert.Greater(t, service.FinanceCaptureHealth()["capture_failures_since_process_start"].(int64), failuresBefore)
		for _, test := range []struct {
			name  string
			usage *dto.Usage
			local bool
		}{
			{"missing", nil, false},
			{"local-estimate", &dto.Usage{PromptTokens: 100, CompletionTokens: 50}, true},
			{"unreported-cache-zero", &dto.Usage{PromptTokens: 100, CompletionTokens: 50}, false},
			{"negative", &dto.Usage{PromptTokens: -1, CompletionTokens: 50}, false},
			{"inconsistent-cache", &dto.Usage{PromptTokens: 100, CompletionTokens: 50, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 200}}, false},
		} {
			info.RequestId, info.ChannelId, info.UpstreamModelName = "synthetic-usage-"+test.name, 21, "supplier-real-model"
			common.SetContextKey(ctx, constant.ContextKeyLocalCountTokens, test.local)
			attempt := service.BeginFinanceAttempt(ctx, info)
			require.NotEmpty(t, attempt)
			service.CaptureFinanceTextUsage(ctx, info, test.usage)
			service.CaptureFinanceTextUsage(ctx, info, test.usage) // duplicate observation cannot double-count
			var record model.FinanceRequestEvent
			require.NoError(t, db.Where("attempt_id = ? AND phase = ?", attempt, "usage").First(&record).Error)
			var missing service.FinanceUsageSnapshot
			require.NoError(t, common.UnmarshalJsonStr(record.Payload, &missing))
			assert.Nil(t, missing.Estimate.Amount, test.name)
			assert.Nil(t, missing.CostCNY, test.name)
			assert.Equal(t, "unknown", missing.Estimate.Status, test.name)
			var count int64
			require.NoError(t, db.Model(&model.FinanceRequestEvent{}).Where("attempt_id = ? AND phase = ?", attempt, "usage").Count(&count).Error)
			assert.Equal(t, int64(1), count, test.name)
			service.FinishFinanceAttempt(ctx, info, false)
		}
		common.SetContextKey(ctx, constant.ContextKeyLocalCountTokens, false)
		info.RequestId = "synthetic-claude-semantics"
		claudeAttempt := service.BeginFinanceAttempt(ctx, info)
		service.CaptureFinanceTextUsage(ctx, info, &dto.Usage{UsageSemantic: "anthropic", PromptTokens: 100, CompletionTokens: 50, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 200, CachedCreationTokens: 100}})
		var claudeRecord model.FinanceRequestEvent
		require.NoError(t, db.Where("attempt_id = ? AND phase = ?", claudeAttempt, "usage").First(&claudeRecord).Error)
		var claudeCost service.FinanceUsageSnapshot
		require.NoError(t, common.UnmarshalJsonStr(claudeRecord.Payload, &claudeCost))
		require.NotNil(t, claudeCost.Quantities["input"])
		assert.Equal(t, "100", *claudeCost.Quantities["input"], "Anthropic input already excludes cache")
		require.NotNil(t, claudeCost.Estimate.Amount)
		assert.Equal(t, "0.0208", *claudeCost.Estimate.Amount)
		service.FinishFinanceAttempt(ctx, info, false)
		require.NoError(t, model.MigrateFinanceRevenue(db))
		require.NoError(t, model.MigrateFinanceRequests(db))
		require.NoError(t, model.FinanceRequestsReady(db))
	})
	t.Run("finance capture enabled disabled or failing does not alter wallet charge", func(t *testing.T) {
		require.NoError(t, db.AutoMigrate(&model.Channel{}))
		require.NoError(t, db.Create(&model.Channel{Id: 21, Name: "synthetic-finance-channel", Type: 1, Key: "synthetic-unused"}).Error)
		for _, mode := range []string{"disabled", "enabled", "write-fails"} {
			t.Run(mode, func(t *testing.T) {
				t.Setenv("CLEARAPI_FINANCE_ENABLED", strconv.FormatBool(mode != "disabled"))
				common.InitFinanceConfig()
				defer func() { os.Setenv("CLEARAPI_FINANCE_ENABLED", "true"); common.InitFinanceConfig() }()
				var before model.User
				require.NoError(t, db.First(&before, 8).Error)
				ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
				ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader("{}"))
				info := &relaycommon.RelayInfo{RequestId: "synthetic-wallet-" + mode, UserId: 8, IsPlayground: true, OriginModelName: "public-finance-model", StartTime: time.Now(), UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 21, UpstreamModelName: "supplier-real-model"}}
				info.PriceData.UsePrice, info.PriceData.ModelPrice, info.PriceData.GroupRatioInfo.GroupRatio = true, 0.0002, 1
				callback := "synthetic_billing_capture_failure"
				if mode == "write-fails" {
					require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
						if tx.Statement.Table == "finance_request_events" {
							tx.AddError(fmt.Errorf("synthetic capture failure"))
						}
					}))
					defer func() { require.NoError(t, db.Callback().Create().Remove(callback)) }()
				}
				require.Nil(t, service.PreConsumeBilling(ctx, 100, info))
				service.PostTextConsumeQuota(ctx, info, &dto.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}, nil)
				var after model.User
				require.NoError(t, db.First(&after, 8).Error)
				assert.Equal(t, 100, before.Quota-after.Quota)
				if mode == "enabled" {
					var record model.FinanceRequestEvent
					require.NoError(t, db.Where("request_id = ? AND phase = ?", info.RequestId, "text_settlement").First(&record).Error)
					var billing service.FinanceBillingSnapshot
					require.NoError(t, common.UnmarshalJsonStr(record.Payload, &billing))
					require.NotNil(t, billing.ChargedQuota)
					assert.Equal(t, 100, *billing.ChargedQuota)
					assert.Equal(t, "not_allocated_to_sources", billing.AllocationStatus)
				}
			})
		}
	})
	t.Run("asynchronous refund records actual success or unresolved failure", func(t *testing.T) {
		previousBatch := common.BatchUpdateEnabled
		common.BatchUpdateEnabled = false
		defer func() { common.BatchUpdateEnabled = previousBatch }()
		for _, failed := range []bool{false, true} {
			requestID := fmt.Sprintf("synthetic-refund-%t", failed)
			var before model.User
			require.NoError(t, db.First(&before, 8).Error)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader("{}"))
			info := &relaycommon.RelayInfo{RequestId: requestID, UserId: 8, IsPlayground: true, OriginModelName: "public-finance-model", UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 21, UpstreamModelName: "supplier-real-model"}}
			require.Nil(t, service.PreConsumeBilling(ctx, 100, info))
			completed := make(chan struct{}, 1)
			require.NoError(t, db.Callback().Create().After("gorm:commit_or_rollback_transaction").Register("synthetic_refund_recorded", func(tx *gorm.DB) {
				if record, ok := tx.Statement.Dest.(*model.FinanceRequestEvent); ok && record.RequestID == requestID && record.Phase == "funding_refund" && tx.Error == nil {
					completed <- struct{}{}
				}
			}))
			if failed {
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register("synthetic_refund_db_failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "users" {
						tx.AddError(fmt.Errorf("synthetic refund failure"))
					}
				}))
			}
			info.Billing.Refund(ctx)
			select {
			case <-completed:
			case <-time.After(3 * time.Second):
				t.Fatal("isolated asynchronous refund did not finish")
			}
			if failed {
				require.NoError(t, db.Callback().Update().Remove("synthetic_refund_db_failure"))
			}
			require.NoError(t, db.Callback().Create().Remove("synthetic_refund_recorded"))
			var after model.User
			require.NoError(t, db.First(&after, 8).Error)
			var record model.FinanceRequestEvent
			require.NoError(t, db.Where("request_id = ? AND phase = ?", requestID, "funding_refund").First(&record).Error)
			var refund service.FinanceBillingSnapshot
			require.NoError(t, common.UnmarshalJsonStr(record.Payload, &refund))
			if failed {
				assert.Nil(t, refund.ChargedQuota)
				assert.Equal(t, "refund_failed_unresolved", refund.Status)
				assert.Equal(t, 100, before.Quota-after.Quota)
			} else {
				require.NotNil(t, refund.ChargedQuota)
				assert.Zero(t, *refund.ChargedQuota)
				assert.Equal(t, before.Quota, after.Quota)
			}
		}
	})
	t.Run("offline cash and redeemed sources are distinct frozen and auditable", func(t *testing.T) {
		assert.Equal(t, http.StatusServiceUnavailable, request("GET", "/api/owner/finance/cash-receipts", owner, nil).Code)
		require.NoError(t, db.AutoMigrate(&model.OfflineOrder{}, &model.OfflineOrderCode{}, &model.Redemption{}))
		require.NoError(t, model.MigrateFinanceReceipts(db))
		require.NoError(t, model.MigrateFinanceReceipts(db))
		require.NoError(t, model.FinanceReceiptsReady(db))
		require.NoError(t, model.MigrateFinanceRequests(db))
		require.NoError(t, model.MigrateFinanceRevenue(db))
		require.NoError(t, model.FinanceReceiptsReady(db))
		require.NoError(t, db.Create(&model.User{Id: 10, Username: "synthetic-offline-finance", Password: "synthetic-unused", AffCode: "synthetic-aff-10", Quota: 100, Status: common.UserStatusEnabled}).Error)
		order, keys, err := model.CreateOfflineOrder(model.CreateOfflineOrderInput{OrderNo: "synthetic-finance-thirds", MoneyCents: 100, QuotaPerCode: 120, CodeCount: 3, PaidAt: time.Now().Unix() - 30, AdminUserID: 5})
		require.NoError(t, err)
		var links []model.OfflineOrderCode
		require.NoError(t, db.Where("order_id = ?", order.Id).Order("id asc").Find(&links).Error)
		require.Len(t, links, 3)
		quota, err := model.RedeemOffline(keys[0], 10)
		require.NoError(t, err)
		assert.Equal(t, 120, quota)
		unknown, err := service.PreviewFinanceOfflineSources(db, 10, []int{links[0].RedemptionID})
		require.NoError(t, err)
		require.Len(t, unknown, 1)
		assert.Equal(t, "unknown", unknown[0].Event.Source)
		assert.Nil(t, unknown[0].Event.ValueCNY)
		_, err = service.ImportFinanceOfflineSources(db, 10, 5, service.FinanceOfflineImport{RedemptionIDs: []int{links[0].RedemptionID}, Evidence: "synthetic unconfirmed"}, "synthetic")
		assert.ErrorIs(t, err, model.ErrFinanceInvalid)
		var walletCount int64
		require.NoError(t, db.Model(&model.FinanceWallet{}).Where("user_id = ?", 10).Count(&walletCount).Error)
		assert.Zero(t, walletCount)
		badConfirmation := service.FinanceReceiptConfirmation{OrderID: order.Id, ConfirmedAmountCNY: "2", Evidence: "synthetic receipt"}
		assert.Equal(t, http.StatusBadRequest, request("POST", "/api/owner/finance/cash-receipts", owner, badConfirmation).Code)
		for _, amount := range []string{"0", "-1", "NaN", "100000000000000000000"} {
			badConfirmation.ConfirmedAmountCNY = amount
			assert.Equal(t, http.StatusBadRequest, request("POST", "/api/owner/finance/cash-receipts", owner, badConfirmation).Code)
		}
		badConfirmation.ConfirmedAmountCNY = "1"
		confirmed := request("POST", "/api/owner/finance/cash-receipts", owner, badConfirmation)
		require.Equal(t, http.StatusCreated, confirmed.Code, confirmed.Body.String())
		assert.Contains(t, confirmed.Body.String(), "owner_confirmed_manual")
		assert.Equal(t, http.StatusConflict, request("POST", "/api/owner/finance/cash-receipts", owner, badConfirmation).Code)
		cash, err := service.SummarizeFinanceCash(db, order.PaidAt, order.PaidAt+1)
		require.NoError(t, err)
		assert.Equal(t, "1", cash.KnownReceiptsCNY) // all cash, including two unredeemed codes
		assert.Nil(t, cash.TotalReceiptsCNY)
		_, err = service.PreviewFinanceOfflineSources(db, 10, []int{links[1].RedemptionID})
		assert.ErrorIs(t, err, model.ErrFinanceInvalid)
		_, err = service.PreviewFinanceOfflineSources(db, 8, []int{links[0].RedemptionID})
		assert.ErrorIs(t, err, model.ErrFinanceInvalid)
		for _, key := range keys[1:] {
			quota, err = model.RedeemOffline(key, 10)
			require.NoError(t, err)
			assert.Equal(t, 120, quota)
		}
		ids := []int{links[2].RedemptionID, links[0].RedemptionID, links[1].RedemptionID}
		var before model.User
		require.NoError(t, db.First(&before, 10).Error)
		preview := request("POST", "/api/owner/finance/wallets/10/offline-sources/preview", owner, service.FinanceOfflineImport{RedemptionIDs: ids})
		require.Equal(t, http.StatusOK, preview.Code, preview.Body.String())
		assert.Contains(t, preview.Header().Get("Cache-Control"), "no-store")
		for _, key := range keys {
			assert.NotContains(t, preview.Body.String(), key)
		}
		input := service.FinanceOfflineImport{RedemptionIDs: ids, Evidence: "synthetic confirmed sources"}
		imported := request("POST", "/api/owner/finance/wallets/10/offline-sources/import", owner, input)
		require.Equal(t, http.StatusOK, imported.Code, imported.Body.String())
		var wallet model.FinanceWallet
		require.NoError(t, db.First(&wallet, "user_id = ?", 10).Error)
		assert.Equal(t, int64(360), wallet.RemainingQuota)
		assert.Equal(t, "1", wallet.KnownBackingCNY)
		assert.Equal(t, "0", wallet.KnownRevenueCNY)
		var sourceRecords []model.FinanceRevenueRecord
		require.NoError(t, db.Where("user_id = ?", 10).Find(&sourceRecords).Error)
		require.Len(t, sourceRecords, 3)
		for _, record := range sourceRecords {
			assert.Contains(t, record.Evidence, "cash_receipt:")
			assert.Contains(t, record.Evidence, "offline_order:")
		}
		assert.Equal(t, http.StatusOK, request("GET", "/api/owner/finance/cash-receipts", owner, nil).Code)
		assert.Equal(t, http.StatusBadRequest, request("GET", "/api/owner/finance/cash-receipts?page_size=-1", owner, nil).Code)
		assert.Equal(t, http.StatusOK, request("GET", fmt.Sprintf("/api/owner/finance/cash-receipts/summary?from=%d&to=%d", order.PaidAt, order.PaidAt+1), owner, nil).Code)
		assert.Equal(t, http.StatusBadRequest, request("POST", "/api/owner/finance/wallets/10/offline-sources/preview", owner, service.FinanceOfflineImport{RedemptionIDs: []int{ids[0], ids[0]}}).Code)
		input.ExpectedRevision = 3
		repeated, err := service.ImportFinanceOfflineSources(db, 10, 5, input, "synthetic repeat")
		require.NoError(t, err)
		assert.Empty(t, repeated)
		input.ExpectedRevision = 0
		_, err = service.ImportFinanceOfflineSources(db, 10, 5, input, "synthetic stale")
		assert.ErrorIs(t, err, model.ErrFinanceConflict)
		_, err = service.AppendFinanceRevenue(db, 10, 5, service.FinanceRevenueWrite{ExpectedRevision: 3, Event: unknown[0].Event, Evidence: "synthetic forged source"}, "synthetic")
		assert.ErrorIs(t, err, model.ErrFinanceInvalid)
		consumed, err := service.AppendFinanceRevenue(db, 10, 5, service.FinanceRevenueWrite{ExpectedRevision: 3, Event: service.FinanceRevenueEvent{Key: "offline-consume", At: time.Now().Unix(), Kind: "consume", Quota: 60}, Evidence: "synthetic consume"}, "synthetic")
		require.NoError(t, err)
		var allocations []model.FinanceRevenueAllocation
		require.NoError(t, db.Where("event_id = ?", consumed.ID).Find(&allocations).Error)
		require.Len(t, allocations, 1)
		assert.Equal(t, "0.166666666667", *allocations[0].ValueCNY)
		var after model.User
		require.NoError(t, db.First(&after, 10).Error)
		assert.Equal(t, before.Quota, after.Quota)
		cash, err = service.SummarizeFinanceCash(db, order.PaidAt, order.PaidAt+1)
		require.NoError(t, err)
		assert.Equal(t, "1", cash.KnownReceiptsCNY) // no double counting on redeem/import/consume
		outside, err := service.SummarizeFinanceCash(db, order.PaidAt+1, order.PaidAt+2)
		require.NoError(t, err)
		assert.Equal(t, "0", outside.KnownReceiptsCNY)
		// Synthetic mutation checks source conflicts without rewriting historical cash.
		require.NoError(t, db.Model(order).Update("money_cents", 200).Error)
		_, err = service.PreviewFinanceOfflineSources(db, 10, ids)
		assert.ErrorIs(t, err, model.ErrFinanceConflict)
		require.NoError(t, db.Model(order).Update("money_cents", 100).Error)
		require.NoError(t, db.Model(order).Update("status", model.OfflineOrderVoid).Error)
		cash, err = service.SummarizeFinanceCash(db, order.PaidAt, order.PaidAt+1)
		require.NoError(t, err)
		assert.Equal(t, "1", cash.KnownReceiptsCNY) // void is not a cash refund
		second, secondKeys, err := model.CreateOfflineOrder(model.CreateOfflineOrderInput{OrderNo: "synthetic-finance-audit", MoneyCents: 200, QuotaPerCode: 120, CodeCount: 2, PaidAt: time.Now().Unix() - 20, AdminUserID: 5})
		require.NoError(t, err)
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register("synthetic_offline_audit_fail", func(tx *gorm.DB) {
			if tx.Statement.Table == "finance_audits" {
				tx.AddError(fmt.Errorf("synthetic audit failure"))
			}
		}))
		_, err = service.ConfirmFinanceOfflineReceipt(db, 5, service.FinanceReceiptConfirmation{OrderID: second.Id, ConfirmedAmountCNY: "2", Evidence: "synthetic"}, "synthetic")
		require.Error(t, err)
		var receiptCount int64
		require.NoError(t, db.Model(&model.FinanceCashReceipt{}).Where("order_id = ?", second.Id).Count(&receiptCount).Error)
		assert.Zero(t, receiptCount)
		require.NoError(t, db.Callback().Create().Remove("synthetic_offline_audit_fail"))
		_, err = service.ConfirmFinanceOfflineReceipt(db, 5, service.FinanceReceiptConfirmation{OrderID: second.Id, ConfirmedAmountCNY: "2", Evidence: "synthetic"}, "synthetic")
		require.NoError(t, err)
		for _, key := range secondKeys {
			_, err = model.RedeemOffline(key, 10)
			require.NoError(t, err)
		}
		var secondLinks []model.OfflineOrderCode
		require.NoError(t, db.Where("order_id = ?", second.Id).Order("id asc").Find(&secondLinks).Error)
		// Failing the second append rolls back the entire import and its first lot.
		calls := 0
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register("synthetic_offline_second_audit_fail", func(tx *gorm.DB) {
			if tx.Statement.Table == "finance_audits" {
				calls++
				if calls == 2 {
					tx.AddError(fmt.Errorf("synthetic second audit failure"))
				}
			}
		}))
		_, err = service.ImportFinanceOfflineSources(db, 10, 5, service.FinanceOfflineImport{ExpectedRevision: 4, RedemptionIDs: []int{secondLinks[0].RedemptionID, secondLinks[1].RedemptionID}, Evidence: "synthetic"}, "synthetic")
		require.Error(t, err)
		require.NoError(t, db.Callback().Create().Remove("synthetic_offline_second_audit_fail"))
		wallet = model.FinanceWallet{}
		require.NoError(t, db.First(&wallet, "user_id = ?", 10).Error)
		assert.Equal(t, int64(4), wallet.Revision)
		assert.Equal(t, "1", wallet.KnownBackingCNY)
	})
	t.Run("captured consumption imports FIFO without touching business balance", func(t *testing.T) {
		userID := 12
		require.NoError(t, db.Create(&model.User{Id: userID, Username: "synthetic-consumption-bridge", AffCode: "synthetic-aff-12", Quota: 140, Status: common.UserStatusEnabled}).Error)
		missing, err := service.CheckFinanceBalance(db, userID)
		require.NoError(t, err)
		assert.Nil(t, missing.LedgerQuota)
		assert.Equal(t, "unknown", missing.Status)
		now := time.Now().Unix()
		paid := "60"
		for i, event := range []service.FinanceRevenueEvent{{Key: "bridge-paid", At: now - 20, Kind: "credit", Source: "paid", Quota: 120, ValueCNY: &paid}, {Key: "bridge-unknown", At: now - 10, Kind: "credit", Source: "unknown", Quota: 20}} {
			_, err := service.AppendFinanceRevenue(db, userID, 5, service.FinanceRevenueWrite{ExpectedRevision: int64(i), Event: event, Evidence: "synthetic source"}, "synthetic")
			require.NoError(t, err)
		}
		var firstInfo *relaycommon.RelayInfo
		for i, charge := range []int{30, 110} {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader("{}"))
			info := &relaycommon.RelayInfo{RequestId: fmt.Sprintf("synthetic-bridge-%d", i), UserId: userID, IsPlayground: true, OriginModelName: "public-finance-model", UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 21, UpstreamModelName: "supplier-real-model"}}
			info.PriceData.UsePrice, info.PriceData.ModelPrice, info.PriceData.GroupRatioInfo.GroupRatio = true, float64(charge)/common.QuotaPerUnit, 1
			require.Nil(t, service.PreConsumeBilling(ctx, charge, info))
			service.PostTextConsumeQuota(ctx, info, &dto.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}, nil)
			var observation model.FinanceRequestEvent
			require.NoError(t, db.Where("request_id = ? AND phase = ?", info.RequestId, "text_settlement").First(&observation).Error)
			preview, err := service.PreviewFinanceConsumptions(db, userID, []int64{observation.ID})
			require.NoError(t, err)
			require.Len(t, preview, 1)
			assert.Equal(t, "ready", preview[0].Status)
			assert.Equal(t, http.StatusOK, request("POST", fmt.Sprintf("/api/owner/finance/wallets/%d/consumptions/preview", userID), owner, service.FinanceConsumptionImport{ObservationIDs: []int64{observation.ID}}).Code)
			_, err = service.PreviewFinanceConsumptions(db, 8, []int64{observation.ID})
			assert.ErrorIs(t, err, model.ErrFinanceInvalid)
			var before model.User
			require.NoError(t, db.First(&before, userID).Error)
			input := service.FinanceConsumptionImport{ObservationIDs: []int64{observation.ID}, ExpectedRevision: int64(2 + i), Evidence: "synthetic observed consumption"}
			if i == 0 {
				stale := input
				stale.ExpectedRevision = 0
				_, err = service.ImportFinanceConsumptions(db, userID, 5, stale, "synthetic")
				assert.ErrorIs(t, err, model.ErrFinanceConflict)
				require.NoError(t, db.Callback().Create().Before("gorm:create").Register("synthetic_consumption_audit_failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "finance_audits" {
						tx.AddError(fmt.Errorf("synthetic consumption audit failure"))
					}
				}))
				_, err = service.ImportFinanceConsumptions(db, userID, 5, input, "synthetic")
				require.Error(t, err)
				require.NoError(t, db.Callback().Create().Remove("synthetic_consumption_audit_failure"))
				var wallet model.FinanceWallet
				require.NoError(t, db.First(&wallet, "user_id = ?", userID).Error)
				assert.Equal(t, int64(2), wallet.Revision)
				assert.Equal(t, int64(140), wallet.RemainingQuota)
			}
			imported, err := service.ImportFinanceConsumptions(db, userID, 5, input, "synthetic")
			require.NoError(t, err)
			require.Len(t, imported, 1)
			input.ExpectedRevision++
			repeated, err := service.ImportFinanceConsumptions(db, userID, 5, input, "synthetic")
			require.NoError(t, err)
			assert.Empty(t, repeated)
			assert.Equal(t, http.StatusOK, request("POST", fmt.Sprintf("/api/owner/finance/wallets/%d/consumptions/import", userID), owner, input).Code)
			var after model.User
			require.NoError(t, db.First(&after, userID).Error)
			assert.Equal(t, before.Quota, after.Quota)
			balance, err := service.CheckFinanceBalance(db, userID)
			require.NoError(t, err)
			assert.Equal(t, "matching_unverified", balance.Status)
			assert.Zero(t, *balance.Difference)
			summary, err := service.SummarizeFinanceWallet(db, userID)
			require.NoError(t, err)
			if i == 0 {
				assert.Equal(t, "15", *summary.ConsumptionCNY)
				firstInfo = info
			} else {
				assert.Nil(t, summary.ConsumptionCNY)
				assert.Equal(t, "60", summary.Wallet.KnownRevenueCNY)
				assert.Equal(t, int64(20), summary.Wallet.UnknownConsumedQuota)
			}
		}
		service.CaptureFinanceBilling(firstInfo, "funding_refund", nil, service.BillingSourceWallet, "refund_failed_unresolved")
		var original model.FinanceRequestEvent
		require.NoError(t, db.Where("request_id = ? AND phase = ?", firstInfo.RequestId, "text_settlement").First(&original).Error)
		preview, err := service.PreviewFinanceConsumptions(db, userID, []int64{original.ID})
		require.NoError(t, err)
		assert.Equal(t, "refund_requires_review", preview[0].Status)
		assert.NotZero(t, preview[0].ExistingEventID)
		assert.Nil(t, preview[0].Event)
		_, err = service.ImportFinanceConsumptions(db, userID, 5, service.FinanceConsumptionImport{ObservationIDs: []int64{original.ID}, ExpectedRevision: 4, Evidence: "synthetic"}, "synthetic")
		assert.ErrorIs(t, err, model.ErrFinanceInvalid)
		// Point-in-time differences are reported, never fixed with invented credits.
		require.NoError(t, db.Model(&model.User{}).Where("id = ?", userID).Update("quota", 7).Error)
		balance, err := service.CheckFinanceBalance(db, userID)
		require.NoError(t, err)
		assert.Equal(t, "mismatch", balance.Status)
		assert.Equal(t, int64(7), *balance.Difference)
		assert.Equal(t, http.StatusOK, request("GET", fmt.Sprintf("/api/owner/finance/wallets/%d/balance-check", userID), owner, nil).Code)
		zero, negative, positive := 0, -1, 7
		for _, test := range []struct {
			name, funding, status, want string
			charged                     *int
		}{
			{"missing", service.BillingSourceWallet, "settled", "unknown", nil},
			{"negative", service.BillingSourceWallet, "settled", "unknown", &negative},
			{"subscription", service.BillingSourceSubscription, "settled", "unknown", &positive},
			{"failed", service.BillingSourceWallet, "settlement_failed_charge_observed", "unknown", &positive},
			{"zero", service.BillingSourceWallet, "settled", "no_charge", &zero},
			{"no_sources", service.BillingSourceWallet, "settled", "ready", &positive},
		} {
			info := &relaycommon.RelayInfo{RequestId: "synthetic-bridge-" + test.name, UserId: userID, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 21, UpstreamModelName: "supplier-real-model"}}
			service.CaptureFinanceBilling(info, "text_settlement", test.charged, test.funding, test.status)
			var fact model.FinanceRequestEvent
			require.NoError(t, db.Where("request_id = ? AND phase = ?", info.RequestId, "text_settlement").First(&fact).Error)
			preview, err := service.PreviewFinanceConsumptions(db, userID, []int64{fact.ID})
			require.NoError(t, err)
			assert.Equal(t, test.want, preview[0].Status)
			_, err = service.ImportFinanceConsumptions(db, userID, 5, service.FinanceConsumptionImport{ObservationIDs: []int64{fact.ID}, ExpectedRevision: 4, Evidence: "synthetic"}, "synthetic")
			if test.want == "no_charge" {
				require.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, model.ErrFinanceInvalid)
			}
		}
	})
}

func TestFinanceCalendarAccrual(t *testing.T) {
	beijing := time.FixedZone("Asia/Shanghai", 8*3600)
	date := func(year, month, day int) int64 {
		return time.Date(year, time.Month(month), day, 0, 0, 0, 0, beijing).Unix()
	}
	for _, test := range []struct {
		name     string
		fee      model.FinanceExpense
		from, to int64
		want     string
	}{
		{"fixed crosses month and idle account", model.FinanceExpense{Amount: "300", PeriodFrom: date(2026, 1, 15), PeriodTo: date(2026, 2, 14), Recurrence: "once"}, date(2026, 1, 15), date(2026, 2, 1), "170"},
		{"remaining cross month", model.FinanceExpense{Amount: "300", PeriodFrom: date(2026, 1, 15), PeriodTo: date(2026, 2, 14), Recurrence: "once"}, date(2026, 2, 1), date(2026, 2, 14), "130"},
		{"monthly end of month anchor", model.FinanceExpense{Amount: "100", PeriodFrom: date(2026, 1, 31), PeriodTo: date(2026, 3, 31), Recurrence: "month"}, date(2026, 1, 31), date(2026, 3, 31), "200"},
		{"annual fee", model.FinanceExpense{Amount: "365", PeriodFrom: date(2026, 1, 1), PeriodTo: date(2027, 1, 1), Recurrence: "year"}, date(2026, 2, 1), date(2026, 3, 1), "28"},
		{"refund remains negative", model.FinanceExpense{Amount: "-30", AdjustsID: 1, PeriodFrom: date(2026, 1, 1), PeriodTo: date(2026, 1, 31), Recurrence: "once"}, date(2026, 1, 1), date(2026, 1, 31), "-30"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := service.AccrueFinanceExpense(test.fee, test.from, test.to)
			require.NoError(t, err)
			assert.Equal(t, test.want, result.String())
		})
	}
}

func TestFinanceRevenueFIFO(t *testing.T) {
	value := func(s string) *string { return &s }
	credit := func(key, source string, quota int64, amount *string) service.FinanceRevenueEvent {
		return service.FinanceRevenueEvent{Key: key, At: 1, Kind: "credit", Source: source, Quota: quota, ValueCNY: amount}
	}
	consume := func(key string, quota int64) service.FinanceRevenueEvent {
		return service.FinanceRevenueEvent{Key: key, At: 2, Kind: "consume", Quota: quota}
	}
	reverse := func(key, original string, quota int64) service.FinanceRevenueEvent {
		return service.FinanceRevenueEvent{Key: key, At: 3, Kind: "reverse", Reverses: original, Quota: quota}
	}
	t.Run("package total quota and standalone gifts have different values", func(t *testing.T) {
		result, err := service.PreviewFinanceRevenue(service.FinanceRevenuePreview{Events: []service.FinanceRevenueEvent{
			credit("package", "paid", 120, value("60")), // 100 purchased + 20 package gift
			credit("gift", "gift", 20, value("0")),
			consume("first", 130),
		}})
		require.NoError(t, err)
		assert.Equal(t, "60", *result.BackingValueCNY)
		assert.Equal(t, "60", *result.ConsumptionCNY)
		assert.Equal(t, int64(10), result.RemainingQuota)
		assert.Equal(t, int64(120), result.Allocations[0].Parts[0].Quota)
		assert.Equal(t, "gift", result.Allocations[0].Parts[1].Source)
		assert.Equal(t, "0", *result.Lots[1].ValueCNY)
	})
	t.Run("unknown is neither zero revenue nor inferred cash", func(t *testing.T) {
		result, err := service.PreviewFinanceRevenue(service.FinanceRevenuePreview{NominalCNYPerQuota: value("0.01"), Events: []service.FinanceRevenueEvent{
			credit("historical", "unknown", 50, nil), credit("package", "paid", 120, value("60")), consume("usage", 80),
		}})
		require.NoError(t, err)
		assert.Equal(t, "partial", result.Status)
		assert.Nil(t, result.ConsumptionCNY)
		assert.Nil(t, result.BackingValueCNY)
		assert.Equal(t, "60", result.KnownBackingValueCNY)
		assert.Equal(t, "15", result.KnownConsumptionCNY)
		assert.Equal(t, int64(50), result.UnknownConsumedQuota)
		assert.Equal(t, "0.5", *result.NominalUnknownCNY)
		assert.Nil(t, result.Allocations[0].Parts[0].ValueCNY)
	})
	t.Run("reverse restores original lots rather than repricing as a gift", func(t *testing.T) {
		input := service.FinanceRevenuePreview{Events: []service.FinanceRevenueEvent{
			credit("gift", "gift", 10, value("0")), credit("paid", "paid", 30, value("60")), consume("first", 20), consume("second", 10), reverse("refund", "first", 20),
		}}
		result, err := service.PreviewFinanceRevenue(input)
		require.NoError(t, err)
		assert.Equal(t, "20", *result.ConsumptionCNY)
		assert.Equal(t, "60", *result.BackingValueCNY) // quota refund is not a cash refund
		assert.Equal(t, int64(30), result.RemainingQuota)
		assert.Equal(t, int64(10), result.Lots[0].Quota)
		assert.Equal(t, "40", *result.Lots[1].ValueCNY)
		assert.Equal(t, "-20", *result.Allocations[2].Parts[1].ValueCNY)
		assert.Equal(t, int64(20), input.Events[2].Quota) // caller facts are untouched
	})
	t.Run("repeating unit value conserves cash after nonlatest reversal", func(t *testing.T) {
		input := service.FinanceRevenuePreview{Events: []service.FinanceRevenueEvent{
			credit("thirds", "paid", 3, value("1")), consume("a", 1), consume("b", 1), reverse("undo-a", "a", 1),
			{Key: "final", At: 4, Kind: "consume", Quota: 2},
		}}
		result, err := service.PreviewFinanceRevenue(input)
		require.NoError(t, err)
		assert.Equal(t, "1", *result.ConsumptionCNY)
		assert.Equal(t, "0", *result.Lots[0].ValueCNY)
		assert.Equal(t, int64(0), result.RemainingQuota)
	})
	t.Run("unknown reversal restores uncertainty to the remaining balance", func(t *testing.T) {
		result, err := service.PreviewFinanceRevenue(service.FinanceRevenuePreview{Events: []service.FinanceRevenueEvent{
			credit("old", "unknown", 10, nil), consume("use", 10), reverse("undo", "use", 10),
		}})
		require.NoError(t, err)
		assert.Equal(t, "0", *result.ConsumptionCNY)
		assert.Nil(t, result.BackingValueCNY)
		assert.Nil(t, result.Lots[0].ValueCNY)
		assert.Equal(t, int64(0), result.UnknownConsumedQuota)
	})
	for _, test := range []struct {
		name   string
		events []service.FinanceRevenueEvent
	}{
		{"missing paid value", []service.FinanceRevenueEvent{credit("p", "paid", 10, nil)}},
		{"gift has cash value", []service.FinanceRevenueEvent{credit("g", "gift", 10, value("1"))}},
		{"unknown given fake zero", []service.FinanceRevenueEvent{credit("u", "unknown", 10, value("0"))}},
		{"duplicate fact", []service.FinanceRevenueEvent{credit("p", "paid", 10, value("10")), consume("p", 1)}},
		{"quota overflow", []service.FinanceRevenueEvent{credit("p", "paid", common.MaxWalletQuota, value("10")), credit("g", "gift", 1, value("0"))}},
		{"overconsume", []service.FinanceRevenueEvent{credit("g", "gift", 10, value("0")), consume("c", 11)}},
		{"partial refund", []service.FinanceRevenueEvent{credit("g", "gift", 10, value("0")), consume("c", 10), reverse("r", "c", 5)}},
		{"double refund", []service.FinanceRevenueEvent{credit("g", "gift", 10, value("0")), consume("c", 10), reverse("r", "c", 10), reverse("r2", "c", 10)}},
		{"no original refund", []service.FinanceRevenueEvent{reverse("r", "absent", 10)}},
		{"out of order", []service.FinanceRevenueEvent{{Key: "g", At: 3, Kind: "credit", Source: "gift", Quota: 10, ValueCNY: value("0")}, consume("c", 1)}},
		{"unsupported cash refund", []service.FinanceRevenueEvent{{Key: "r", At: 1, Kind: "cash_refund", Quota: 1}}},
		{"negative money", []service.FinanceRevenueEvent{credit("p", "paid", 1, value("-1"))}},
		{"nonfinite money", []service.FinanceRevenueEvent{credit("p", "paid", 1, value("NaN"))}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := service.PreviewFinanceRevenue(service.FinanceRevenuePreview{Events: test.events})
			assert.ErrorIs(t, err, model.ErrFinanceInvalid)
			assert.Nil(t, result)
		})
	}
}

func TestFinanceFreshSchema(t *testing.T) {
	db := financeTestDB(t)
	if os.Getenv("FINANCE_TEST_DIALECT") == "" || os.Getenv("FINANCE_TEST_DIALECT") == "sqlite" {
		var version string
		require.NoError(t, db.Raw("SELECT sqlite_version()").Scan(&version).Error)
		t.Log("SQLite", version)
	}
	assert.False(t, db.Migrator().HasTable(&model.FinanceVersion{}))
	assert.False(t, db.Migrator().HasTable(&model.User{}))
	require.NoError(t, model.MigrateFinance(db))
	require.NoError(t, model.MigrateFinance(db))
	require.NoError(t, model.FinanceReady(db))
	assert.True(t, db.Migrator().HasTable(&model.FinanceExpense{}))
	assert.False(t, db.Migrator().HasTable(&model.User{}))
	require.NoError(t, model.MigrateFinanceRevenue(db))
	require.NoError(t, model.MigrateFinanceRevenue(db))
	require.NoError(t, model.FinanceRevenueReady(db))
	assert.True(t, db.Migrator().HasTable(&model.FinanceWallet{}))
	assert.True(t, db.Migrator().HasTable(&model.FinanceRevenueLot{}))
	assert.True(t, db.Migrator().HasTable(&model.FinanceRevenueRecord{}))
	assert.True(t, db.Migrator().HasTable(&model.FinanceRevenueAllocation{}))
	assert.False(t, db.Migrator().HasTable(&model.User{}))
	require.NoError(t, model.MigrateFinanceRequests(db))
	require.NoError(t, model.MigrateFinanceRequests(db))
	require.NoError(t, model.FinanceRequestsReady(db))
	assert.True(t, db.Migrator().HasTable(&model.FinanceRequestEvent{}))
	require.NoError(t, model.MigrateFinanceReceipts(db))
	require.NoError(t, model.MigrateFinanceReceipts(db))
	require.NoError(t, model.FinanceReceiptsReady(db))
	assert.True(t, db.Migrator().HasTable(&model.FinanceCashReceipt{}))
	assert.False(t, db.Migrator().HasTable(&model.User{}))
}
