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
	"github.com/shopspring/decimal"
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
		assert.Equal(t, http.StatusServiceUnavailable, request("GET", "/api/owner/finance/profit-summary?from=1&to=2", owner, nil).Code)
		assert.Equal(t, http.StatusServiceUnavailable, request("GET", "/api/owner/finance/sync-events", owner, nil).Code)
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
			{"GET", "/billing-reservations"}, {"POST", "/billing-reservations/1/decision"},
			{"GET", "/task-settlements"}, {"POST", "/task-settlements/1/retry"},
			{"GET", "/capability"}, {"GET", "/versions/cost"}, {"POST", "/versions/cost"},
			{"GET", "/versions/fx"}, {"POST", "/versions/fx"}, {"GET", "/versions/pool"}, {"POST", "/versions/pool"},
			{"GET", "/expenses"}, {"POST", "/expenses"}, {"GET", "/expenses/summary?from=1&to=2"}, {"GET", "/audit"},
			{"POST", "/revenue/preview"},
			{"POST", "/wallets/5/events"}, {"GET", "/wallets/5/events"}, {"GET", "/wallets/5/summary"}, {"GET", "/wallets/5/allocations?event_id=1"},
			{"GET", "/request-events"}, {"GET", "/capture-health"},
			{"GET", "/profit-summary?from=1&to=2"},
			{"POST", "/supplier-bills"}, {"GET", "/supplier-bills?channel_id=91"}, {"GET", "/supplier-bills/reconciliation?channel_id=91&from=100&to=300"},
			{"POST", "/cash-receipts"}, {"GET", "/cash-receipts"}, {"GET", "/cash-receipts/summary?from=1&to=2"},
			{"POST", "/cash-refunds"}, {"GET", "/cash-refunds"}, {"GET", "/sync-health"}, {"GET", "/sync-events"},
			{"POST", "/wallets/5/offline-sources/preview"}, {"POST", "/wallets/5/offline-sources/import"},
			{"POST", "/wallets/5/consumptions/preview"}, {"POST", "/wallets/5/consumptions/import"}, {"GET", "/wallets/5/balance-check"},
			{"POST", "/wallets/5/rebuild/preview"}, {"POST", "/wallets/5/rebuild/apply"},
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
		// Partial reversal of one captured consumption is now a supported fact.
		partial := service.FinanceRevenueWrite{ExpectedRevision: 6, Event: service.FinanceRevenueEvent{Key: "partial-undo", At: 7, Kind: "reverse", Quota: 1, Reverses: "usage-again"}, Evidence: "synthetic partial"}
		require.Equal(t, http.StatusCreated, request("POST", "/api/owner/finance/wallets/8/events", owner, partial).Code)
		overReverse := service.FinanceRevenueWrite{ExpectedRevision: 7, Event: service.FinanceRevenueEvent{Key: "double-reversal", At: 8, Kind: "reverse", Quota: 80, Reverses: "usage"}, Evidence: "synthetic over-reversal"}
		assert.Equal(t, http.StatusBadRequest, request("POST", "/api/owner/finance/wallets/8/events", owner, overReverse).Code)
		var count int64
		require.NoError(t, db.Model(&model.FinanceRevenueRecord{}).Where("user_id = ?", 8).Count(&count).Error)
		assert.Equal(t, int64(7), count)
		summary, err = service.SummarizeFinanceWallet(db, 8)
		require.NoError(t, err)
		callbackName := "synthetic_revenue_audit_failure"
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
			if tx.Statement.Table == "finance_audits" {
				tx.AddError(fmt.Errorf("synthetic audit failure"))
			}
		}))
		rollback := service.FinanceRevenueWrite{ExpectedRevision: 7, Event: service.FinanceRevenueEvent{Key: "rollback", At: 9, Kind: "consume", Quota: 10}, Evidence: "synthetic"}
		failed := request("POST", "/api/owner/finance/wallets/8/events", owner, rollback)
		require.NoError(t, db.Callback().Create().Remove(callbackName))
		assert.Equal(t, http.StatusServiceUnavailable, failed.Code)
		after, err := service.SummarizeFinanceWallet(db, 8)
		require.NoError(t, err)
		assert.Equal(t, summary.Wallet, after.Wallet)
		require.NoError(t, db.Model(&model.FinanceRevenueRecord{}).Where("user_id = ?", 8).Count(&count).Error)
		assert.Equal(t, int64(7), count)
		var audits int64
		require.NoError(t, db.Model(&model.FinanceAudit{}).Where("action = ?", "revenue.append").Count(&audits).Error)
		assert.Equal(t, int64(7), audits)
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
	t.Run("supplier bills replace estimates with immutable corrections", func(t *testing.T) {
		assert.Equal(t, http.StatusServiceUnavailable, request("POST", "/api/owner/finance/supplier-bills", owner, nil).Code)
		require.NoError(t, model.MigrateFinanceSupplier(db))
		require.NoError(t, model.MigrateFinanceSupplier(db))
		require.NoError(t, model.MigrateFinanceReceipts(db))
		require.NoError(t, model.FinanceReceiptsReady(db))
		var schema model.FinanceSchema
		require.NoError(t, db.First(&schema, 1).Error)
		assert.Equal(t, 5, schema.Version)
		var fx model.FinanceVersion
		require.NoError(t, db.Where("kind = ?", "fx").Order("id asc").First(&fx).Error)
		input := service.FinanceSupplierInput{Scope: "all_channel_request_costs", EventKey: "supplier-original", ChannelID: 91, PeriodFrom: 100, PeriodTo: 200, Currency: "USD", Amount: "10", FXVersionID: fx.ID, Evidence: "synthetic complete request-cost invoice"}
		created := request("POST", "/api/owner/finance/supplier-bills", owner, input)
		require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
		assert.Contains(t, created.Header().Get("Cache-Control"), "no-store")
		var first model.FinanceSupplierBill
		require.NoError(t, db.Where("event_key = ?", input.EventKey).First(&first).Error)
		require.NotNil(t, first.AmountCNY)
		assert.Equal(t, "70", *first.AmountCNY) // Newer FX is 9; frozen original is 7.
		assert.Equal(t, http.StatusConflict, request("POST", "/api/owner/finance/supplier-bills", owner, input).Code)
		input.ExpectedID, input.EventKey = first.ID, "supplier-overlap"
		assert.Equal(t, http.StatusConflict, request("POST", "/api/owner/finance/supplier-bills", owner, input).Code)
		input.EventKey, input.ReplacesID, input.Amount = "supplier-correction", first.ID, "8"
		replacement, err := service.RegisterFinanceSupplierBill(db, input, 5, "synthetic")
		require.NoError(t, err)
		assert.Equal(t, "56", *replacement.AmountCNY)
		assert.Equal(t, first.ID, replacement.ReplacesID)
		input.ExpectedID = replacement.ID
		_, err = service.RegisterFinanceSupplierBill(db, input, 5, "synthetic")
		assert.ErrorIs(t, err, model.ErrFinanceConflict) // Same key even with current expected ID.
		input.ExpectedID, input.EventKey = replacement.ID, "supplier-stale-original"
		_, err = service.RegisterFinanceSupplierBill(db, input, 5, "synthetic")
		assert.ErrorIs(t, err, model.ErrFinanceConflict)
		input.ReplacesID, input.PeriodTo = replacement.ID, 201
		_, err = service.RegisterFinanceSupplierBill(db, input, 5, "synthetic")
		assert.ErrorIs(t, err, model.ErrFinanceInvalid)
		input.PeriodTo, input.FXVersionID = 200, 0
		_, err = service.RegisterFinanceSupplierBill(db, input, 5, "synthetic")
		assert.ErrorIs(t, err, model.ErrFinanceInvalid)
		input.FXVersionID, input.EventKey = fx.ID, "supplier-audit-failure"
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register("synthetic_supplier_audit_failure", func(tx *gorm.DB) {
			if tx.Statement.Table == "finance_audits" {
				tx.AddError(fmt.Errorf("synthetic audit failure"))
			}
		}))
		_, err = service.RegisterFinanceSupplierBill(db, input, 5, "synthetic")
		assert.Error(t, err)
		require.NoError(t, db.Callback().Create().Remove("synthetic_supplier_audit_failure"))
		var count int64
		require.NoError(t, db.Model(&model.FinanceSupplierBill{}).Where("channel_id = ?", 91).Count(&count).Error)
		assert.EqualValues(t, 2, count)
		forty, three, ninetyNine := "40", "3", "99"
		for _, observation := range []struct {
			at     int64
			amount *string
		}{{100, &forty}, {199, nil}, {200, &three}, {250, nil}, {300, &ninetyNine}} {
			id := fmt.Sprintf("supplier-at-%d", observation.at)
			require.NoError(t, db.Create(&model.FinanceRequestEvent{EventKey: id + "-start", RequestID: id, AttemptID: id, Phase: "attempt_start", ChannelID: 91, At: observation.at, Payload: "{}"}).Error)
			if observation.amount != nil {
				payload, err := common.Marshal(service.FinanceUsageSnapshot{CostCNY: observation.amount})
				require.NoError(t, err)
				// Late usage still belongs to the send's original period.
				require.NoError(t, db.Create(&model.FinanceRequestEvent{EventKey: id + "-usage", RequestID: id, AttemptID: id, Phase: "usage", ChannelID: 91, At: 400, Payload: string(payload)}).Error)
			}
		}
		summary, err := service.ReconcileFinanceSupplier(db, 91, 100, 300)
		require.NoError(t, err)
		assert.Equal(t, "59", summary.KnownSelectedCNY)
		assert.Equal(t, "56", summary.KnownBilledCNY)
		assert.Equal(t, "43", summary.KnownObservedEstimateCNY)
		assert.Equal(t, "3", summary.KnownUncoveredEstimateCNY)
		assert.Equal(t, 4, summary.ObservedAttempts)
		assert.Equal(t, 2, summary.UnknownEstimateAttempts)
		assert.Equal(t, 1, summary.UnknownSelectedItems)
		assert.Equal(t, int64(100), summary.UncoveredSeconds)
		assert.Nil(t, summary.TotalCNY)
		assert.Equal(t, "partial", summary.Status)
		require.Len(t, summary.Bills, 1)
		assert.Equal(t, replacement.ID, summary.Bills[0].Bill.ID)
		clipped, err := service.ReconcileFinanceSupplier(db, 91, 150, 200)
		require.NoError(t, err)
		assert.Equal(t, "0", clipped.KnownSelectedCNY)
		assert.Equal(t, 1, clipped.UnknownSelectedItems)
		assert.Equal(t, "partial_period_amount_unknown", clipped.Bills[0].Status)
		empty, err := service.ReconcileFinanceSupplier(db, 92, 100, 300)
		require.NoError(t, err)
		assert.Nil(t, empty.TotalCNY)
		assert.Equal(t, int64(200), empty.UncoveredSeconds)
		unknown := service.FinanceSupplierInput{Scope: "all_channel_request_costs", EventKey: "supplier-unknown-fx", ChannelID: 92, PeriodFrom: 100, PeriodTo: 300, Amount: "1", Currency: "EUR", Evidence: "synthetic"}
		unknownBill, err := service.RegisterFinanceSupplierBill(db, unknown, 5, "synthetic")
		require.NoError(t, err)
		assert.Nil(t, unknownBill.AmountCNY)
		unknownSummary, err := service.ReconcileFinanceSupplier(db, 92, 100, 300)
		require.NoError(t, err)
		assert.Equal(t, 1, unknownSummary.UnknownSelectedItems)
		assert.Equal(t, "unknown_fx", unknownSummary.Bills[0].Status)
		for _, amount := range []string{"-1", "NaN", "1e100"} {
			unknown.Amount = amount
			_, err := service.RegisterFinanceSupplierBill(db, unknown, 5, "synthetic")
			assert.ErrorIs(t, err, model.ErrFinanceInvalid)
		}
		zero := service.FinanceSupplierInput{Scope: "all_channel_request_costs", EventKey: "supplier-zero", ChannelID: 93, PeriodFrom: 100, PeriodTo: 300, Amount: "0", Currency: "CNY", Evidence: "synthetic explicit zero bill"}
		zero.Scope = "partial_model_costs"
		_, err = service.RegisterFinanceSupplierBill(db, zero, 5, "synthetic")
		assert.ErrorIs(t, err, model.ErrFinanceInvalid)
		zero.Scope = "all_channel_request_costs"
		_, err = service.RegisterFinanceSupplierBill(db, zero, 5, "synthetic")
		require.NoError(t, err)
		// Re-running additive v5 and older migrations retains bills and indexes.
		require.NoError(t, model.MigrateFinanceSupplier(db))
		require.NoError(t, model.MigrateFinance(db))
		require.NoError(t, model.FinanceSupplierReady(db))
		require.NoError(t, db.Model(&model.FinanceSupplierBill{}).Where("channel_id = ?", 91).Count(&count).Error)
		assert.EqualValues(t, 2, count)
		zero.ChannelID, zero.EventKey = 94, "supplier-concurrent-base"
		base, err := service.RegisterFinanceSupplierBill(db, zero, 5, "synthetic")
		require.NoError(t, err)
		if dialect := os.Getenv("FINANCE_TEST_DIALECT"); dialect == "mysql" || dialect == "postgres" {
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(2)
			defer sqlDB.SetMaxOpenConns(1)
		}
		results := make(chan error, 2)
		for _, key := range []string{"supplier-concurrent-one", "supplier-concurrent-two"} {
			go func(eventKey string) {
				change := zero
				change.EventKey, change.ExpectedID, change.ReplacesID = eventKey, base.ID, base.ID
				_, err := service.RegisterFinanceSupplierBill(db, change, 5, "synthetic")
				results <- err
			}(key)
		}
		committed := 0
		for range 2 {
			if err := <-results; err == nil {
				committed++
			}
		}
		assert.Equal(t, 1, committed)
		require.NoError(t, db.Model(&model.FinanceSupplierBill{}).Where("channel_id = ?", 94).Count(&count).Error)
		assert.EqualValues(t, 2, count)
		assert.Equal(t, http.StatusOK, request("GET", "/api/owner/finance/supplier-bills?channel_id=91", owner, nil).Code)
		assert.Equal(t, http.StatusOK, request("GET", "/api/owner/finance/supplier-bills/reconciliation?channel_id=91&from=100&to=300", owner, nil).Code)
		var retained model.FinanceSupplierBill
		require.NoError(t, db.First(&retained, first.ID).Error)
		assert.Equal(t, "70", *retained.AmountCNY)
		var ownerRecord model.User
		require.NoError(t, db.First(&ownerRecord, 5).Error)
		assert.Equal(t, 1234567, ownerRecord.Quota)
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
	t.Run("persistent partial reversals conserve cumulative value", func(t *testing.T) {
		userID := 13
		require.NoError(t, db.Create(&model.User{Id: userID, Username: "synthetic-partial-reversal", AffCode: "synthetic-aff-13", Quota: 555, Status: common.UserStatusEnabled}).Error)
		three := "3"
		facts := []service.FinanceRevenueEvent{
			{Key: "package", At: 1, Kind: "credit", Source: "paid", Quota: 30, ValueCNY: &three},
			{Key: "use", At: 2, Kind: "consume", Quota: 30},
			{Key: "undo-first", At: 3, Kind: "reverse", Quota: 10, Reverses: "use"},
			{Key: "undo-second", At: 4, Kind: "reverse", Quota: 10, Reverses: "use"},
		}
		for i, event := range facts {
			_, err := service.AppendFinanceRevenue(db, userID, 5, service.FinanceRevenueWrite{ExpectedRevision: int64(i), Event: event, Evidence: "synthetic partial reversal"}, "synthetic")
			require.NoError(t, err)
		}
		summary, err := service.SummarizeFinanceWallet(db, userID)
		require.NoError(t, err)
		assert.Equal(t, "1", *summary.ConsumptionCNY) // 3 recognized, 2 restored
		assert.Equal(t, "3", summary.Wallet.KnownBackingCNY)
		assert.Equal(t, int64(20), summary.Wallet.RemainingQuota)
		over := service.FinanceRevenueWrite{ExpectedRevision: 4, Event: service.FinanceRevenueEvent{Key: "undo-too-much", At: 5, Kind: "reverse", Quota: 20, Reverses: "use"}, Evidence: "synthetic"}
		_, err = service.AppendFinanceRevenue(db, userID, 5, over, "synthetic")
		assert.ErrorIs(t, err, model.ErrFinanceInvalid)
		_, err = service.AppendFinanceRevenue(db, userID, 5, service.FinanceRevenueWrite{ExpectedRevision: 4, Event: service.FinanceRevenueEvent{Key: "undo-rest", At: 5, Kind: "reverse", Quota: 10, Reverses: "use"}, Evidence: "synthetic"}, "synthetic")
		require.NoError(t, err)
		summary, err = service.SummarizeFinanceWallet(db, userID)
		require.NoError(t, err)
		assert.Equal(t, "0", *summary.ConsumptionCNY)
		assert.Equal(t, int64(30), summary.Wallet.RemainingQuota)
		var lot model.FinanceRevenueLot
		require.NoError(t, db.Where("user_id = ?", userID).First(&lot).Error)
		assert.Equal(t, "0", lot.RecognizedCNY)
		// Re-consuming the restored quota recognizes exactly the original values.
		_, err = service.AppendFinanceRevenue(db, userID, 5, service.FinanceRevenueWrite{ExpectedRevision: 5, Event: service.FinanceRevenueEvent{Key: "reconsume", At: 6, Kind: "consume", Quota: 30}, Evidence: "synthetic"}, "synthetic")
		require.NoError(t, err)
		summary, err = service.SummarizeFinanceWallet(db, userID)
		require.NoError(t, err)
		assert.Equal(t, "3", *summary.ConsumptionCNY)
		var negative int64
		require.NoError(t, db.Model(&model.FinanceRevenueAllocation{}).Joins("JOIN finance_revenue_records ON finance_revenue_records.id = finance_revenue_allocations.event_id").Where("finance_revenue_records.user_id = ? AND finance_revenue_allocations.quota < 0", userID).Count(&negative).Error)
		assert.Equal(t, int64(3), negative)
		var user model.User
		require.NoError(t, db.First(&user, userID).Error)
		assert.Equal(t, 555, user.Quota)
	})
	t.Run("manual adjusts never create cash revenue or business balance", func(t *testing.T) {
		userID := 14
		require.NoError(t, db.Create(&model.User{Id: userID, Username: "synthetic-adjust-facts", AffCode: "synthetic-aff-14", Quota: 777, Status: common.UserStatusEnabled}).Error)
		zero, one := "0", "1"
		facts := []service.FinanceRevenueEvent{
			{Key: "grant-gift", At: 1, Kind: "adjust", Quota: 20, Source: "gift", ValueCNY: &zero},
			{Key: "grant-unknown", At: 2, Kind: "adjust", Quota: 10, Source: "unknown"},
			{Key: "forfeit", At: 3, Kind: "adjust", Quota: -15},
		}
		for i, event := range facts {
			_, err := service.AppendFinanceRevenue(db, userID, 5, service.FinanceRevenueWrite{ExpectedRevision: int64(i), Event: event, Evidence: "synthetic adjust"}, "synthetic")
			require.NoError(t, err)
		}
		for _, invalid := range []service.FinanceRevenueEvent{
			{Key: "bad-paid", At: 4, Kind: "adjust", Quota: 5, Source: "paid", ValueCNY: &one},
			{Key: "bad-value", At: 4, Kind: "adjust", Quota: 5, Source: "gift", ValueCNY: &one},
			{Key: "bad-zero", At: 4, Kind: "adjust", Quota: 0},
			{Key: "bad-forfeit", At: 4, Kind: "adjust", Quota: -16},
		} {
			_, err := service.AppendFinanceRevenue(db, userID, 5, service.FinanceRevenueWrite{ExpectedRevision: 3, Event: invalid, Evidence: "synthetic"}, "synthetic")
			assert.ErrorIs(t, err, model.ErrFinanceInvalid, invalid.Key)
		}
		_, err := service.AppendFinanceRevenue(db, userID, 5, service.FinanceRevenueWrite{ExpectedRevision: 3, Event: service.FinanceRevenueEvent{Key: "use", At: 4, Kind: "consume", Quota: 15}, Evidence: "synthetic"}, "synthetic")
		require.NoError(t, err)
		summary, err := service.SummarizeFinanceWallet(db, userID)
		require.NoError(t, err)
		assert.Nil(t, summary.ConsumptionCNY) // unknown-source quota was consumed
		assert.Equal(t, "0", summary.Wallet.KnownRevenueCNY)
		assert.Equal(t, int64(10), summary.Wallet.UnknownConsumedQuota)
		assert.Equal(t, int64(1), summary.Wallet.UnknownCreditCount)
		assert.Equal(t, "0", summary.Wallet.KnownBackingCNY)
		assert.Equal(t, int64(0), summary.Wallet.RemainingQuota)
		var user model.User
		require.NoError(t, db.First(&user, userID).Error)
		assert.Equal(t, 777, user.Quota)
	})
	t.Run("cash refund lifecycle separates pending from succeeded net", func(t *testing.T) {
		assert.Equal(t, http.StatusServiceUnavailable, request("POST", "/api/owner/finance/cash-refunds", owner, nil).Code)
		require.NoError(t, model.MigrateFinanceRefunds(db))
		require.NoError(t, model.MigrateFinanceRefunds(db))
		require.NoError(t, model.FinanceRefundsReady(db))
		order, _, err := model.CreateOfflineOrder(model.CreateOfflineOrderInput{OrderNo: "synthetic-finance-refunds", MoneyCents: 100, QuotaPerCode: 60, CodeCount: 1, PaidAt: time.Now().Unix() - 100000, AdminUserID: 5})
		require.NoError(t, err)
		confirmed, err := service.ConfirmFinanceOfflineReceipt(db, 5, service.FinanceReceiptConfirmation{OrderID: order.Id, ConfirmedAmountCNY: "1", Evidence: "synthetic receipt"}, "synthetic")
		require.NoError(t, err)
		base := service.FinanceCashRefundInput{ReceiptID: confirmed.ID, AmountCNY: "0.4", RefundedAt: order.PaidAt + 10, Evidence: "synthetic refund"}
		base.State, base.EventKey = "pending", "synthetic-refund-pending"
		pending, err := service.RegisterFinanceCashRefund(db, 5, base, "synthetic")
		require.NoError(t, err)
		assert.Equal(t, order.Id, pending.OrderID)
		succeeded := base
		succeeded.State, succeeded.EventKey, succeeded.AmountCNY = "succeeded", "synthetic-refund-succeeded", "0.5"
		_, err = service.RegisterFinanceCashRefund(db, 5, succeeded, "synthetic")
		require.NoError(t, err)
		// Applied, queued and succeeded facts reserve against the receipt cap.
		extra := base
		extra.State, extra.EventKey, extra.AmountCNY = "queued", "synthetic-refund-extra", "0.2"
		_, err = service.RegisterFinanceCashRefund(db, 5, extra, "synthetic")
		assert.ErrorIs(t, err, model.ErrFinanceInvalid)
		// A failed attempt stays as history without reserving.
		failedAttempt := base
		failedAttempt.State, failedAttempt.EventKey, failedAttempt.AmountCNY = "failed", "synthetic-refund-failed", "0.3"
		_, err = service.RegisterFinanceCashRefund(db, 5, failedAttempt, "synthetic")
		require.NoError(t, err)
		_, err = service.RegisterFinanceCashRefund(db, 5, succeeded, "synthetic")
		assert.ErrorIs(t, err, model.ErrFinanceConflict)
		for _, bad := range []service.FinanceCashRefundInput{
			{EventKey: "synthetic-refund-bad", ReceiptID: confirmed.ID, State: "refunded", AmountCNY: "0.1", RefundedAt: order.PaidAt, Evidence: "synthetic"},
			{EventKey: "synthetic-refund-bad", ReceiptID: confirmed.ID, State: "succeeded", AmountCNY: "0", RefundedAt: order.PaidAt, Evidence: "synthetic"},
			{EventKey: "synthetic-refund-bad", ReceiptID: confirmed.ID, State: "succeeded", AmountCNY: "-1", RefundedAt: order.PaidAt, Evidence: "synthetic"},
			{EventKey: "synthetic-refund-bad", ReceiptID: confirmed.ID, State: "succeeded", AmountCNY: "0.1", RefundedAt: time.Now().Unix() + 3600, Evidence: "synthetic"},
			{EventKey: "synthetic-refund-bad", ReceiptID: 999999, State: "succeeded", AmountCNY: "0.1", RefundedAt: order.PaidAt, Evidence: "synthetic"},
			{EventKey: "offline-refund-1", ReceiptID: confirmed.ID, State: "succeeded", AmountCNY: "0.1", RefundedAt: order.PaidAt, Evidence: "synthetic"},
			{EventKey: "request-refund-1", ReceiptID: confirmed.ID, State: "succeeded", AmountCNY: "0.1", RefundedAt: order.PaidAt, Evidence: "synthetic"},
			{EventKey: "synthetic-refund-bad", ReceiptID: confirmed.ID, State: "succeeded", AmountCNY: "0.1", RefundedAt: order.PaidAt, Evidence: ""},
		} {
			_, err := service.RegisterFinanceCashRefund(db, 5, bad, "synthetic")
			assert.ErrorIs(t, err, model.ErrFinanceInvalid, bad.EventKey)
		}
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register("synthetic_refund_audit_fail", func(tx *gorm.DB) {
			if tx.Statement.Table == "finance_audits" {
				tx.AddError(fmt.Errorf("synthetic audit failure"))
			}
		}))
		_, err = service.RegisterFinanceCashRefund(db, 5, service.FinanceCashRefundInput{EventKey: "synthetic-refund-rollback", ReceiptID: confirmed.ID, State: "pending", AmountCNY: "0.05", RefundedAt: order.PaidAt, Evidence: "synthetic"}, "synthetic")
		require.Error(t, err)
		require.NoError(t, db.Callback().Create().Remove("synthetic_refund_audit_fail"))
		created := request("POST", "/api/owner/finance/cash-refunds", owner, service.FinanceCashRefundInput{EventKey: "synthetic-refund-api", ReceiptID: confirmed.ID, State: "succeeded", AmountCNY: "0.1", RefundedAt: order.PaidAt + 12, Evidence: "synthetic"})
		require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
		assert.Contains(t, created.Header().Get("Cache-Control"), "no-store")
		assert.Equal(t, http.StatusOK, request("GET", "/api/owner/finance/cash-refunds", owner, nil).Code)
		cash, err := service.SummarizeFinanceCash(db, order.PaidAt, order.PaidAt+3600)
		require.NoError(t, err)
		assert.Equal(t, "1", cash.KnownReceiptsCNY)
		assert.Equal(t, "0.6", cash.KnownRefundsCNY) // pending and failed never net
		assert.Equal(t, "0.4", cash.NetKnownReceiptsCNY)
		assert.Nil(t, cash.TotalReceiptsCNY)
		assert.Equal(t, "partial", cash.Status)
		// The receipt period is reported unchanged; refunds belong to their own period.
		receiptPeriod, err := service.SummarizeFinanceCash(db, order.PaidAt-1, order.PaidAt+1)
		require.NoError(t, err)
		assert.Equal(t, "1", receiptPeriod.NetKnownReceiptsCNY)
		refundPeriod, err := service.SummarizeFinanceCash(db, order.PaidAt+9, order.PaidAt+13)
		require.NoError(t, err)
		assert.Equal(t, "0", refundPeriod.KnownReceiptsCNY)
		assert.Equal(t, "0.6", refundPeriod.KnownRefundsCNY)
		assert.Equal(t, "-0.6", refundPeriod.NetKnownReceiptsCNY)
	})
	t.Run("reliable sync outbox recovers retries and parks the unresolvable", func(t *testing.T) {
		assert.Equal(t, http.StatusServiceUnavailable, request("GET", "/api/owner/finance/sync-health", owner, nil).Code)
		require.NoError(t, model.MigrateFinanceSync(db))
		require.NoError(t, model.MigrateFinanceSync(db))
		require.NoError(t, model.FinanceSyncReady(db))
		now := time.Now().Unix()
		syncEvents := func() int64 {
			var count int64
			require.NoError(t, db.Model(&model.FinanceSyncEvent{}).Count(&count).Error)
			return count
		}
		// Emission is atomic with the offline redemption and skips plain codes.
		user15, user16, user17, user20 := 15, 16, 17, 20
		require.NoError(t, db.Create(&model.User{Id: user15, Username: "synthetic-sync-15", AffCode: "synthetic-aff-15", Quota: 500, Status: common.UserStatusEnabled}).Error)
		require.NoError(t, db.Create(&model.User{Id: user16, Username: "synthetic-sync-16", AffCode: "synthetic-aff-16", Quota: 500, Status: common.UserStatusEnabled}).Error)
		require.NoError(t, db.Create(&model.User{Id: user17, Username: "synthetic-sync-17", AffCode: "synthetic-aff-17", Quota: 500, Status: common.UserStatusEnabled}).Error)
		require.NoError(t, db.Create(&model.User{Id: user20, Username: "synthetic-sync-20", AffCode: "synthetic-aff-20", Quota: 500, Status: common.UserStatusEnabled}).Error)
		order, keys, err := model.CreateOfflineOrder(model.CreateOfflineOrderInput{OrderNo: "synthetic-sync-one", MoneyCents: 100, QuotaPerCode: 60, CodeCount: 1, PaidAt: now - 100000, AdminUserID: 5})
		require.NoError(t, err)
		var businessBefore model.User
		require.NoError(t, db.First(&businessBefore, user15).Error)
		quota, err := model.RedeemOffline(keys[0], user15)
		require.NoError(t, err)
		assert.Equal(t, 60, quota)
		var businessAfter model.User
		require.NoError(t, db.First(&businessAfter, user15).Error)
		assert.Equal(t, 60, businessAfter.Quota-businessBefore.Quota) // redeem credits the business wallet
		var link model.OfflineOrderCode
		require.NoError(t, db.Where("order_id = ?", order.Id).First(&link).Error)
		var emitted model.FinanceSyncEvent
		require.NoError(t, db.Where("event_key = ?", fmt.Sprintf("auto-offline-redemption-%d", link.RedemptionID)).First(&emitted).Error)
		assert.Equal(t, "pending", emitted.Status)
		assert.Equal(t, user15, emitted.UserID)
		plain := &model.Redemption{UserId: 5, Name: "plain", Key: "synthetic-sync-plain-key", Status: common.RedemptionCodeStatusEnabled, Quota: 30, CreatedTime: now}
		require.NoError(t, db.Create(plain).Error)
		_, err = model.Redeem(plain.Key, user15)
		require.NoError(t, err)
		assert.Equal(t, int64(1), syncEvents()) // original codes stay outside finance
		// Unconfirmed receipts block instead of inventing a value.
		processed, err := service.RunFinanceSyncPass(db, 100)
		require.NoError(t, err)
		assert.Equal(t, 1, processed)
		require.NoError(t, db.First(&emitted, emitted.ID).Error)
		assert.Equal(t, "blocked", emitted.Status)
		assert.Equal(t, "receipt_unconfirmed", emitted.ErrorClass)
		var wallets int64
		require.NoError(t, db.Model(&model.FinanceWallet{}).Where("user_id = ?", user15).Count(&wallets).Error)
		assert.Zero(t, wallets)
		_, err = service.ConfirmFinanceOfflineReceipt(db, 5, service.FinanceReceiptConfirmation{OrderID: order.Id, ConfirmedAmountCNY: "1", Evidence: "synthetic receipt"}, "synthetic")
		require.NoError(t, err)
		require.NoError(t, db.First(&emitted, emitted.ID).Error)
		assert.Equal(t, "pending", emitted.Status) // unblocked by the confirmation
		processed, err = service.RunFinanceSyncPass(db, 100)
		require.NoError(t, err)
		assert.Equal(t, 1, processed)
		require.NoError(t, db.First(&emitted, emitted.ID).Error)
		assert.Equal(t, "done", emitted.Status)
		var wallet model.FinanceWallet
		require.NoError(t, db.First(&wallet, "user_id = ?", user15).Error)
		assert.Equal(t, "1", wallet.KnownBackingCNY)
		assert.Equal(t, int64(60), wallet.RemainingQuota)
		require.NoError(t, db.First(&businessAfter, user15).Error)
		assert.Equal(t, businessBefore.Quota+90, businessAfter.Quota) // 60 offline + 30 plain; import never touches the business wallet
		var autoAudits int64
		require.NoError(t, db.Model(&model.FinanceAudit{}).Where("action = ?", "revenue.auto_sync").Count(&autoAudits).Error)
		assert.Equal(t, int64(1), autoAudits)
		processed, err = service.RunFinanceSyncPass(db, 100) // idempotent replay
		require.NoError(t, err)
		assert.Zero(t, processed)
		require.NoError(t, db.First(&wallet, "user_id = ?", user15).Error)
		assert.Equal(t, int64(1), wallet.Revision)
		// A late source whose time precedes the ledger head parks for review.
		order2, keys2, err := model.CreateOfflineOrder(model.CreateOfflineOrderInput{OrderNo: "synthetic-sync-late", MoneyCents: 100, QuotaPerCode: 60, CodeCount: 1, PaidAt: now - 100000, AdminUserID: 5})
		require.NoError(t, err)
		_, err = model.RedeemOffline(keys2[0], user16)
		require.NoError(t, err)
		_, err = service.ConfirmFinanceOfflineReceipt(db, 5, service.FinanceReceiptConfirmation{OrderID: order2.Id, ConfirmedAmountCNY: "1", Evidence: "synthetic receipt"}, "synthetic")
		require.NoError(t, err)
		value := "3"
		_, err = service.AppendFinanceRevenue(db, user16, 5, service.FinanceRevenueWrite{Event: service.FinanceRevenueEvent{Key: "late-head", At: now + 1000, Kind: "credit", Source: "paid", Quota: 30, ValueCNY: &value}, Evidence: "synthetic"}, "synthetic")
		require.NoError(t, err)
		processed, err = service.RunFinanceSyncPass(db, 100)
		require.NoError(t, err)
		assert.Equal(t, 1, processed)
		var lateEvent model.FinanceSyncEvent
		require.NoError(t, db.Where("topic = ? AND user_id = ?", "offline_redemption", user16).First(&lateEvent).Error)
		assert.Equal(t, "needs_review", lateEvent.Status)
		assert.Equal(t, "out_of_order", lateEvent.ErrorClass)
		wallet = model.FinanceWallet{}
		require.NoError(t, db.First(&wallet, "user_id = ?", user16).Error)
		assert.Equal(t, int64(1), wallet.Revision) // no silent history rewrite
		// Consumption facts apply through the same keyed machinery.
		six := "6"
		_, err = service.AppendFinanceRevenue(db, user17, 5, service.FinanceRevenueWrite{Event: service.FinanceRevenueEvent{Key: "consume-source", At: now - 2000, Kind: "credit", Source: "paid", Quota: 120, ValueCNY: &six}, Evidence: "synthetic"}, "synthetic")
		require.NoError(t, err)
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader("{}"))
		info := &relaycommon.RelayInfo{RequestId: "synthetic-sync-consume", UserId: user17, IsPlayground: true, OriginModelName: "public-finance-model", UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 21, UpstreamModelName: "supplier-real-model"}}
		info.PriceData.UsePrice, info.PriceData.ModelPrice, info.PriceData.GroupRatioInfo.GroupRatio = true, 0.00006, 1
		require.Nil(t, service.PreConsumeBilling(ctx, 30, info))
		service.PostTextConsumeQuota(ctx, info, &dto.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}, nil)
		var observation model.FinanceRequestEvent
		require.NoError(t, db.Where("request_id = ? AND phase = ?", info.RequestId, "text_settlement").First(&observation).Error)
		// Direct-write settlements emit their fact automatically.
		var autoEvent model.FinanceSyncEvent
		require.NoError(t, db.Where("event_key = ?", "auto-text-settlement-"+info.RequestId).First(&autoEvent).Error)
		assert.Equal(t, "pending", autoEvent.Status)
		assert.Equal(t, observation.ID, autoEvent.RefID)
		processed, err = service.RunFinanceSyncPass(db, 100)
		require.NoError(t, err)
		assert.Equal(t, 1, processed)
		wallet = model.FinanceWallet{}
		require.NoError(t, db.First(&wallet, "user_id = ?", user17).Error)
		assert.Equal(t, "1.5", wallet.KnownRevenueCNY) // 6 CNY over 120 quota, 30 consumed
		service.CaptureFinanceBilling(info, "funding_refund", nil, service.BillingSourceWallet, "refund_failed_unresolved")
		require.NoError(t, db.Create(&model.FinanceSyncEvent{EventKey: "auto-consume-refund", Topic: "consumption", UserID: user17, RefID: observation.ID, At: observation.At, Payload: "{}", Status: model.FinanceSyncPending, NextAttemptAt: now, CreatedAt: now}).Error)
		unresolvedInfo := &relaycommon.RelayInfo{RequestId: "synthetic-sync-unresolved", UserId: user17, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 21, UpstreamModelName: "supplier-real-model"}}
		failed := 7
		service.CaptureFinanceBilling(unresolvedInfo, "text_settlement", &failed, service.BillingSourceWallet, "settlement_failed_charge_observed")
		var unresolvedFact model.FinanceRequestEvent
		require.NoError(t, db.Where("request_id = ? AND phase = ?", unresolvedInfo.RequestId, "text_settlement").First(&unresolvedFact).Error)
		require.NoError(t, db.Create(&model.FinanceSyncEvent{EventKey: "auto-consume-unresolved", Topic: "consumption", UserID: user17, RefID: unresolvedFact.ID, At: unresolvedFact.At, Payload: "{}", Status: model.FinanceSyncPending, NextAttemptAt: now, CreatedAt: now}).Error)
		zeroCharge := 0
		zeroInfo := &relaycommon.RelayInfo{RequestId: "synthetic-sync-zero", UserId: user17, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 21, UpstreamModelName: "supplier-real-model"}}
		service.CaptureFinanceBilling(zeroInfo, "text_settlement", &zeroCharge, service.BillingSourceWallet, "settled")
		var zeroFact model.FinanceRequestEvent
		require.NoError(t, db.Where("request_id = ? AND phase = ?", zeroInfo.RequestId, "text_settlement").First(&zeroFact).Error)
		require.NoError(t, db.Create(&model.FinanceSyncEvent{EventKey: "auto-consume-zero", Topic: "consumption", UserID: user17, RefID: zeroFact.ID, At: zeroFact.At, Payload: "{}", Status: model.FinanceSyncPending, NextAttemptAt: now, CreatedAt: now}).Error)
		_, err = service.RunFinanceSyncPass(db, 100)
		require.NoError(t, err)
		var refundEvent, unresolvedEvent, zeroEvent model.FinanceSyncEvent
		require.NoError(t, db.Where("event_key = ?", "auto-consume-refund").First(&refundEvent).Error)
		assert.Equal(t, "needs_review", refundEvent.Status)
		assert.Equal(t, "refund_requires_review", refundEvent.ErrorClass)
		require.NoError(t, db.Where("event_key = ?", "auto-consume-unresolved").First(&unresolvedEvent).Error)
		assert.Equal(t, "needs_review", unresolvedEvent.Status)
		assert.Equal(t, "settlement_unresolved", unresolvedEvent.ErrorClass)
		require.NoError(t, db.Where("event_key = ?", "auto-consume-zero").First(&zeroEvent).Error)
		assert.Equal(t, "done", zeroEvent.Status)
		assert.Equal(t, "no_charge", zeroEvent.ErrorClass)
		// A crashed worker's lease is reclaimed and the fact still lands once.
		order3, keys3, err := model.CreateOfflineOrder(model.CreateOfflineOrderInput{OrderNo: "synthetic-sync-crash", MoneyCents: 100, QuotaPerCode: 60, CodeCount: 1, PaidAt: now - 100000, AdminUserID: 5})
		require.NoError(t, err)
		_, err = model.RedeemOffline(keys3[0], user15)
		require.NoError(t, err)
		var crashed model.FinanceSyncEvent
		require.NoError(t, db.Where("topic = ? AND user_id = ?", "offline_redemption", user15).Order("id desc").First(&crashed).Error)
		require.NoError(t, db.Model(&crashed).Updates(map[string]any{"status": model.FinanceSyncProcessing, "claimed_at": now - 120}).Error)
		_, err = service.ConfirmFinanceOfflineReceipt(db, 5, service.FinanceReceiptConfirmation{OrderID: order3.Id, ConfirmedAmountCNY: "1", Evidence: "synthetic receipt"}, "synthetic")
		require.NoError(t, err)
		_, err = service.RunFinanceSyncPass(db, 100)
		require.NoError(t, err)
		require.NoError(t, db.First(&crashed, crashed.ID).Error)
		assert.Equal(t, "done", crashed.Status)
		// Bounded retries park a persistently failing fact for the owner.
		order4, keys4, err := model.CreateOfflineOrder(model.CreateOfflineOrderInput{OrderNo: "synthetic-sync-stuck", MoneyCents: 100, QuotaPerCode: 60, CodeCount: 1, PaidAt: now - 100000, AdminUserID: 5})
		require.NoError(t, err)
		_, err = model.RedeemOffline(keys4[0], user15)
		require.NoError(t, err)
		var stuck model.FinanceSyncEvent
		require.NoError(t, db.Where("topic = ? AND user_id = ?", "offline_redemption", user15).Order("id desc").First(&stuck).Error)
		_, err = service.ConfirmFinanceOfflineReceipt(db, 5, service.FinanceReceiptConfirmation{OrderID: order4.Id, ConfirmedAmountCNY: "1", Evidence: "synthetic receipt"}, "synthetic")
		require.NoError(t, err)
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register("synthetic_sync_audit_fail", func(tx *gorm.DB) {
			if tx.Statement.Table == "finance_audits" {
				tx.AddError(fmt.Errorf("synthetic audit failure"))
			}
		}))
		for range 5 { // financeSyncMaxAttempts

			require.NoError(t, db.Model(&stuck).Update("next_attempt_at", 0).Error)
			_, err = service.RunFinanceSyncPass(db, 100)
			require.NoError(t, err)
		}
		require.NoError(t, db.Callback().Create().Remove("synthetic_sync_audit_fail"))
		require.NoError(t, db.First(&stuck, stuck.ID).Error)
		assert.Equal(t, "needs_review", stuck.Status)
		assert.Equal(t, "attempts_exhausted", stuck.ErrorClass)
		var recordsBefore int64
		require.NoError(t, db.Model(&model.FinanceRevenueRecord{}).Where("user_id = ?", user15).Count(&recordsBefore).Error)
		assert.Equal(t, int64(2), recordsBefore) // orders one and three only
		wallet = model.FinanceWallet{}
		require.NoError(t, db.First(&wallet, "user_id = ?", user15).Error)
		var links4 []model.OfflineOrderCode
		require.NoError(t, db.Where("order_id = ?", order4.Id).Find(&links4).Error)
		_, err = service.ImportFinanceOfflineSources(db, user15, 5, service.FinanceOfflineImport{RedemptionIDs: []int{links4[0].RedemptionID}, ExpectedRevision: wallet.Revision, Evidence: "synthetic manual resolution"}, "synthetic")
		require.NoError(t, err) // the owner resolves the parked fact manually
		// Two workers competing for one event commit it exactly once.
		order5, keys5, err := model.CreateOfflineOrder(model.CreateOfflineOrderInput{OrderNo: "synthetic-sync-race", MoneyCents: 100, QuotaPerCode: 60, CodeCount: 1, PaidAt: now - 100000, AdminUserID: 5})
		require.NoError(t, err)
		_, err = model.RedeemOffline(keys5[0], user20)
		require.NoError(t, err)
		_, err = service.ConfirmFinanceOfflineReceipt(db, 5, service.FinanceReceiptConfirmation{OrderID: order5.Id, ConfirmedAmountCNY: "1", Evidence: "synthetic receipt"}, "synthetic")
		require.NoError(t, err)
		if dialect := os.Getenv("FINANCE_TEST_DIALECT"); dialect == "mysql" || dialect == "postgres" {
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(2)
			defer sqlDB.SetMaxOpenConns(1)
			results := make(chan error, 2)
			for range 2 {
				go func() {
					_, err := service.RunFinanceSyncPass(db, 100)
					results <- err
				}()
			}
			for range 2 {
				require.NoError(t, <-results)
			}
		} else {
			_, err := service.RunFinanceSyncPass(db, 100)
			require.NoError(t, err)
		}
		wallet = model.FinanceWallet{}
		require.NoError(t, db.First(&wallet, "user_id = ?", user20).Error)
		assert.Equal(t, "1", wallet.KnownBackingCNY)
		var raced int64
		require.NoError(t, db.Model(&model.FinanceRevenueRecord{}).Where("user_id = ?", user20).Count(&raced).Error)
		assert.Equal(t, int64(1), raced)
		// Persistent health metrics and balance context.
		health, err := service.FinanceSyncHealth(db)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, health.Counts["done"], int64(4))
		assert.GreaterOrEqual(t, health.Counts["needs_review"], int64(4))
		assert.Zero(t, health.Counts["blocked"])
		assert.NotZero(t, health.WatermarkID)
		assert.Contains(t, health.Warnings, "consumption_emission_not_connected_to_settlement_boundary")
		healthView := request("GET", "/api/owner/finance/sync-health", owner, nil)
		require.Equal(t, http.StatusOK, healthView.Code)
		assert.Contains(t, healthView.Body.String(), "watermark_id")
		assert.Contains(t, healthView.Header().Get("Cache-Control"), "no-store")
		balance, err := service.CheckFinanceBalance(db, user15)
		require.NoError(t, err)
		assert.Equal(t, "mismatch", balance.Status)
		require.NotNil(t, balance.NeedsReviewSyncEvents)
		assert.Equal(t, int64(1), *balance.NeedsReviewSyncEvents)
		require.NotNil(t, balance.PendingSyncEvents)
		assert.Zero(t, *balance.PendingSyncEvents)
		require.NoError(t, db.Create(&model.FinanceSyncEvent{EventKey: "auto-pending-context", Topic: "consumption", UserID: user17, RefID: 999999, At: now, Payload: "{}", Status: model.FinanceSyncPending, NextAttemptAt: now + 100000, CreatedAt: now}).Error)
		balance, err = service.CheckFinanceBalance(db, user17)
		require.NoError(t, err)
		assert.Equal(t, "mismatch", balance.Status)
		assert.Equal(t, int64(1), *balance.PendingSyncEvents)
		assert.Equal(t, int64(2), *balance.NeedsReviewSyncEvents) // refund and unresolved
	})
	t.Run("settlement emission and controlled rebuild stay bounded and auditable", func(t *testing.T) {
		previousBatch := common.BatchUpdateEnabled
		common.BatchUpdateEnabled = false
		defer func() { common.BatchUpdateEnabled = previousBatch }()
		assert.Equal(t, http.StatusServiceUnavailable, request("POST", "/api/owner/finance/wallets/21/rebuild/preview", owner, nil).Code)
		require.NoError(t, model.MigrateFinanceRebuilds(db))
		require.NoError(t, model.MigrateFinanceRebuilds(db))
		require.NoError(t, model.FinanceRebuildsReady(db))
		now := time.Now().Unix()
		user21, user22, user23 := 21, 22, 23
		for _, user := range []int{user21, user22, user23} {
			require.NoError(t, db.Create(&model.User{Id: user, Username: fmt.Sprintf("synthetic-rebuild-%d", user), AffCode: fmt.Sprintf("synthetic-aff-%d", user), Quota: 500, Status: common.UserStatusEnabled}).Error)
		}
		settle := func(userID int, requestID string, charge int) model.FinanceRequestEvent {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader("{}"))
			info := &relaycommon.RelayInfo{RequestId: requestID, UserId: userID, IsPlayground: true, OriginModelName: "public-finance-model", UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 21, UpstreamModelName: "supplier-real-model"}}
			info.PriceData.UsePrice, info.PriceData.ModelPrice, info.PriceData.GroupRatioInfo.GroupRatio = true, float64(charge)/common.QuotaPerUnit, 1
			require.Nil(t, service.PreConsumeBilling(ctx, charge, info))
			service.PostTextConsumeQuota(ctx, info, &dto.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}, nil)
			var observation model.FinanceRequestEvent
			require.NoError(t, db.Where("request_id = ? AND phase = ?", requestID, "text_settlement").First(&observation).Error)
			return observation
		}
		// Direct-write settlements emit and import automatically.
		six := "6"
		_, err := service.AppendFinanceRevenue(db, user21, 5, service.FinanceRevenueWrite{Event: service.FinanceRevenueEvent{Key: "emission-source", At: now - 3000, Kind: "credit", Source: "paid", Quota: 120, ValueCNY: &six}, Evidence: "synthetic"}, "synthetic")
		require.NoError(t, err)
		var businessBefore model.User
		require.NoError(t, db.First(&businessBefore, user21).Error)
		observation := settle(user21, "synthetic-emission-direct", 30)
		var emitted model.FinanceSyncEvent
		require.NoError(t, db.Where("event_key = ?", "auto-text-settlement-synthetic-emission-direct").First(&emitted).Error)
		assert.Equal(t, observation.ID, emitted.RefID)
		_, err = service.RunFinanceSyncPass(db, 100)
		require.NoError(t, err)
		var wallet model.FinanceWallet
		require.NoError(t, db.First(&wallet, "user_id = ?", user21).Error)
		assert.Equal(t, "1.5", wallet.KnownRevenueCNY)
		var businessAfter model.User
		require.NoError(t, db.First(&businessAfter, user21).Error)
		assert.Equal(t, businessBefore.Quota-30, businessAfter.Quota)
		// Batch mode keeps the quota in memory: no reliable fact is claimed.
		common.BatchUpdateEnabled = true
		settle(user21, "synthetic-emission-batch", 20)
		var batchEvent int64
		require.NoError(t, db.Model(&model.FinanceSyncEvent{}).Where("event_key = ?", "auto-text-settlement-synthetic-emission-batch").Count(&batchEvent).Error)
		assert.Zero(t, batchEvent)
		common.BatchUpdateEnabled = false
		// A failing emission never changes the existing charge behavior.
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register("synthetic_emission_fail", func(tx *gorm.DB) {
			if tx.Statement.Table == "finance_sync_events" {
				tx.AddError(fmt.Errorf("synthetic emission failure"))
			}
		}))
		var quotaBefore model.User
		require.NoError(t, db.First(&quotaBefore, user21).Error)
		settle(user21, "synthetic-emission-fail", 10)
		require.NoError(t, db.Callback().Create().Remove("synthetic_emission_fail"))
		var quotaAfter model.User
		require.NoError(t, db.First(&quotaAfter, user21).Error)
		assert.Equal(t, 10, quotaBefore.Quota-quotaAfter.Quota)
		var failEvent int64
		require.NoError(t, db.Model(&model.FinanceSyncEvent{}).Where("event_key = ?", "auto-text-settlement-synthetic-emission-fail").Count(&failEvent).Error)
		assert.Zero(t, failEvent)
		// The batch-mode settle above is a visible, bounded reconciliation gap.
		health, err := service.FinanceSyncHealth(db)
		require.NoError(t, err)
		assert.Equal(t, "active", health.SettlementEmission)
		assert.GreaterOrEqual(t, health.UnimportedSettlementSample, 1)
		assert.GreaterOrEqual(t, health.SettlementSampleSize, 1)
		// Controlled rebuild: the late offline redemption returns to its
		// historical FIFO position after preview, snapshot and hash checks.
		orderA, keysA, err := model.CreateOfflineOrder(model.CreateOfflineOrderInput{OrderNo: "synthetic-rebuild-late", MoneyCents: 100, QuotaPerCode: 60, CodeCount: 1, PaidAt: now - 100000, AdminUserID: 5})
		require.NoError(t, err)
		_, err = model.RedeemOffline(keysA[0], user22)
		require.NoError(t, err)
		// The test runs within one second; backdate the redemption so its fact
		// is genuinely earlier than the later consumption (synthetic data only).
		var linkA model.OfflineOrderCode
		require.NoError(t, db.Where("order_id = ?", orderA.Id).First(&linkA).Error)
		require.NoError(t, db.Model(&model.Redemption{}).Where("id = ?", linkA.RedemptionID).Update("redeemed_time", now-5000).Error)
		orderB, keysB, err := model.CreateOfflineOrder(model.CreateOfflineOrderInput{OrderNo: "synthetic-rebuild-base", MoneyCents: 200, QuotaPerCode: 120, CodeCount: 1, PaidAt: now - 100000, AdminUserID: 5})
		require.NoError(t, err)
		_, err = model.RedeemOffline(keysB[0], user22)
		require.NoError(t, err)
		_, err = service.ConfirmFinanceOfflineReceipt(db, 5, service.FinanceReceiptConfirmation{OrderID: orderB.Id, ConfirmedAmountCNY: "2", Evidence: "synthetic receipt"}, "synthetic")
		require.NoError(t, err)
		_, err = service.RunFinanceSyncPass(db, 100) // imports order B's source
		require.NoError(t, err)
		settle(user22, "synthetic-rebuild-consume", 30)
		_, err = service.RunFinanceSyncPass(db, 100) // consumes 0.5 CNY from B
		require.NoError(t, err)
		_, err = service.ConfirmFinanceOfflineReceipt(db, 5, service.FinanceReceiptConfirmation{OrderID: orderA.Id, ConfirmedAmountCNY: "1", Evidence: "synthetic receipt"}, "synthetic")
		require.NoError(t, err)
		_, err = service.RunFinanceSyncPass(db, 100) // A is late: parks for review
		require.NoError(t, err)
		var lateEvent model.FinanceSyncEvent
		require.NoError(t, db.Where("topic = ? AND user_id = ?", "offline_redemption", user22).Order("id asc").First(&lateEvent).Error)
		assert.Equal(t, "needs_review", lateEvent.Status)
		assert.Equal(t, "out_of_order", lateEvent.ErrorClass)
		preview, err := service.PreviewFinanceWalletRebuild(db, user22, []int64{lateEvent.ID})
		require.NoError(t, err)
		assert.Equal(t, int64(2), preview.ExpectedRevision)
		assert.Equal(t, []int64{lateEvent.ID}, preview.IncorporatedSyncEvents)
		assert.Equal(t, 3, preview.FactCount)
		assert.Equal(t, int64(90), preview.CurrentRemainingQuota)
		assert.Equal(t, int64(150), preview.RebuiltRemainingQuota)
		require.NotNil(t, preview.RebuiltBackingCNY)
		assert.Equal(t, "3", *preview.RebuiltBackingCNY)
		require.NotNil(t, preview.RebuiltRevenueCNY)
		assert.Equal(t, "0.5", *preview.RebuiltRevenueCNY)
		assert.Equal(t, http.StatusBadRequest, request("POST", "/api/owner/finance/wallets/22/rebuild/apply", owner, service.FinanceRebuildInput{SyncEventIDs: []int64{lateEvent.ID, lateEvent.ID}, ExpectedRevision: preview.ExpectedRevision, SnapshotHash: preview.SnapshotHash, Evidence: "synthetic rebuild"}).Code)
		apply := request("POST", "/api/owner/finance/wallets/22/rebuild/apply", owner, service.FinanceRebuildInput{SyncEventIDs: []int64{lateEvent.ID}, ExpectedRevision: preview.ExpectedRevision, SnapshotHash: preview.SnapshotHash, Evidence: "synthetic rebuild"})
		require.Equal(t, http.StatusOK, apply.Code, apply.Body.String())
		wallet = model.FinanceWallet{}
		require.NoError(t, db.First(&wallet, "user_id = ?", user22).Error)
		assert.Equal(t, int64(150), wallet.RemainingQuota)
		assert.Equal(t, "3", wallet.KnownBackingCNY)
		assert.Equal(t, "0.5", wallet.KnownRevenueCNY)
		assert.Equal(t, int64(5), wallet.Revision) // old revision + fact count stays monotonic
		var snapshot model.FinanceRebuildSnapshot
		require.NoError(t, db.Where("user_id = ?", user22).First(&snapshot).Error)
		assert.Equal(t, preview.SnapshotHash, snapshot.Hash)
		assert.Equal(t, 3, snapshot.FactCount)
		var rebuildAudits int64
		require.NoError(t, db.Model(&model.FinanceAudit{}).Where("action = ?", "revenue.rebuild").Count(&rebuildAudits).Error)
		assert.GreaterOrEqual(t, rebuildAudits, int64(4)) // three replayed facts plus the summary
		require.NoError(t, db.First(&lateEvent, lateEvent.ID).Error)
		assert.Equal(t, "done", lateEvent.Status)
		var consumeRecord model.FinanceRevenueRecord
		require.NoError(t, db.Where("user_id = ? AND event_key LIKE ?", user22, "request-charge-%").First(&consumeRecord).Error)
		var consumeParts []model.FinanceRevenueAllocation
		require.NoError(t, db.Where("event_id = ?", consumeRecord.ID).Order("id asc").Find(&consumeParts).Error)
		require.Len(t, consumeParts, 1)
		var lateLot model.FinanceRevenueLot
		require.NoError(t, db.First(&lateLot, consumeParts[0].LotID).Error) // consumption now draws from the late source
		var business22 model.User
		require.NoError(t, db.First(&business22, user22).Error)
		assert.Equal(t, 650, business22.Quota) // 500 + 60 + 120 - 30; finance never moves it
		// The incorporated event no longer parks: rebuilding again is rejected.
		_, err = service.PreviewFinanceWalletRebuild(db, user22, []int64{lateEvent.ID})
		assert.ErrorIs(t, err, model.ErrFinanceInvalid)
		// Stale revisions and stale hashes are rejected before any write.
		orderC, keysC, err := model.CreateOfflineOrder(model.CreateOfflineOrderInput{OrderNo: "synthetic-rebuild-conflict", MoneyCents: 100, QuotaPerCode: 60, CodeCount: 1, PaidAt: now - 100000, AdminUserID: 5})
		require.NoError(t, err)
		_, err = model.RedeemOffline(keysC[0], user23)
		require.NoError(t, err)
		var linkC model.OfflineOrderCode
		require.NoError(t, db.Where("order_id = ?", orderC.Id).First(&linkC).Error)
		require.NoError(t, db.Model(&model.Redemption{}).Where("id = ?", linkC.RedemptionID).Update("redeemed_time", now-5000).Error)
		two := "2"
		_, err = service.AppendFinanceRevenue(db, user23, 5, service.FinanceRevenueWrite{Event: service.FinanceRevenueEvent{Key: "conflict-source", At: now - 4000, Kind: "credit", Source: "paid", Quota: 50, ValueCNY: &two}, Evidence: "synthetic"}, "synthetic")
		require.NoError(t, err)
		_, err = service.ConfirmFinanceOfflineReceipt(db, 5, service.FinanceReceiptConfirmation{OrderID: orderC.Id, ConfirmedAmountCNY: "1", Evidence: "synthetic receipt"}, "synthetic")
		require.NoError(t, err)
		_, err = service.RunFinanceSyncPass(db, 100) // parks the late source for review
		require.NoError(t, err)
		var conflictEvent model.FinanceSyncEvent
		require.NoError(t, db.Where("topic = ? AND user_id = ?", "offline_redemption", user23).First(&conflictEvent).Error)
		require.Equal(t, "needs_review", conflictEvent.Status)
		stale, err := service.PreviewFinanceWalletRebuild(db, user23, []int64{conflictEvent.ID})
		require.NoError(t, err)
		one := "1"
		_, err = service.AppendFinanceRevenue(db, user23, 5, service.FinanceRevenueWrite{ExpectedRevision: 1, Event: service.FinanceRevenueEvent{Key: "conflict-head", At: now + 2000, Kind: "credit", Source: "paid", Quota: 10, ValueCNY: &one}, Evidence: "synthetic"}, "synthetic")
		require.NoError(t, err)
		assert.Equal(t, http.StatusConflict, request("POST", "/api/owner/finance/wallets/23/rebuild/apply", owner, service.FinanceRebuildInput{SyncEventIDs: []int64{conflictEvent.ID}, ExpectedRevision: stale.ExpectedRevision, SnapshotHash: stale.SnapshotHash, Evidence: "synthetic"}).Code)
		fresh, err := service.PreviewFinanceWalletRebuild(db, user23, []int64{conflictEvent.ID})
		require.NoError(t, err)
		assert.Equal(t, http.StatusConflict, request("POST", "/api/owner/finance/wallets/23/rebuild/apply", owner, service.FinanceRebuildInput{SyncEventIDs: []int64{conflictEvent.ID}, ExpectedRevision: fresh.ExpectedRevision, SnapshotHash: stale.SnapshotHash, Evidence: "synthetic"}).Code)
		// Scope bounds are enforced before any resolution.
		manyIDs := make([]int64, 51)
		for i := range manyIDs {
			manyIDs[i] = int64(i + 1)
		}
		assert.Equal(t, http.StatusBadRequest, request("POST", "/api/owner/finance/wallets/23/rebuild/preview", owner, service.FinanceRebuildInput{SyncEventIDs: manyIDs}).Code)
	})
	t.Run("phase three covers transports failures multimodal tasks and pool costs", func(t *testing.T) {
		require.NoError(t, model.MigrateFinanceRequests(db))
		now := time.Now().Unix()
		// Pool version and cost rule sharing the pool key for channel 31.
		poolRaw := []byte(`{"name":"synthetic-pool","platform":"synthetic","channel_ids":[31]}`)
		poolVersion := model.FinanceVersion{Kind: "pool", RecordKey: "synthetic-pool", EffectiveFrom: now - 1000, EffectiveTo: now + 100000, Active: true, Source: "synthetic pool", CreatedBy: 5}
		require.NoError(t, service.ValidateFinanceVersion(&poolVersion, poolRaw))
		require.NoError(t, model.AppendFinanceVersion(db, &poolVersion, 0, "synthetic"))
		rule := service.FinanceCostRule{ChannelID: 31, UpstreamModel: "supplier-pool-model", PoolKey: "synthetic-pool", Currency: "USD", Prices: []service.FinanceUnitPrice{
			{Meter: "input", Unit: "token", Per: "1000000", Price: "3"},
			{Meter: "output", Unit: "token", Per: "1000000", Price: "12"},
			{Meter: "cache_read", Unit: "token", Per: "1000000", Price: "0.5"},
		}}
		ruleRaw, err := common.Marshal(rule)
		require.NoError(t, err)
		ruleVersion := model.FinanceVersion{Kind: "cost", RecordKey: "pool-cost", EffectiveFrom: now - 1000, EffectiveTo: now + 100000, Active: true, Source: "synthetic procurement", CreatedBy: 5}
		require.NoError(t, service.ValidateFinanceVersion(&ruleVersion, ruleRaw))
		require.NoError(t, model.AppendFinanceVersion(db, &ruleVersion, 0, "synthetic"))
		info := &relaycommon.RelayInfo{RequestId: "synthetic-retry-close", UserId: 8, OriginModelName: "public-finance-model", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 31, UpstreamModelName: "supplier-pool-model", ChannelMultiKeyIndex: 3}}
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader("{}"))
		// A still-open attempt is closed as failed by the next gateway attempt.
		first := service.BeginFinanceAttempt(ctx, info)
		require.NotEmpty(t, first)
		second := service.BeginFinanceAttempt(ctx, info)
		require.NotEmpty(t, second)
		require.NotEqual(t, first, second)
		var closed model.FinanceRequestEvent
		require.NoError(t, db.Where("attempt_id = ? AND phase = ?", first, "attempt_finish").First(&closed).Error)
		assert.Contains(t, closed.Payload, `"failed":true`)
		var noUsage model.FinanceRequestEvent
		require.NoError(t, db.Where("attempt_id = ? AND phase = ?", first, "usage").First(&noUsage).Error)
		var noUsageSnapshot service.FinanceUsageSnapshot
		require.NoError(t, common.UnmarshalJsonStr(noUsage.Payload, &noUsageSnapshot))
		assert.Equal(t, "no_usage_failed_attempt", noUsageSnapshot.Source)
		assert.Empty(t, noUsageSnapshot.Quantities)
		assert.Equal(t, "unknown", noUsageSnapshot.Estimate.Status)
		closed = model.FinanceRequestEvent{}
		require.NoError(t, db.Where("attempt_id = ? AND phase = ?", first, "attempt_start").First(&closed).Error)
		var firstSnapshot service.FinanceAttemptSnapshot
		require.NoError(t, common.UnmarshalJsonStr(closed.Payload, &firstSnapshot))
		assert.Equal(t, 3, firstSnapshot.ChannelAccountIndex)
		assert.Equal(t, "synthetic-pool", firstSnapshot.PoolKey)
		// A successful close without settlement keeps the explicit no-usage mark.
		service.FinishFinanceAttempt(ctx, info, false)
		var successNoUsage model.FinanceRequestEvent
		require.NoError(t, db.Where("attempt_id = ? AND phase = ?", second, "usage").First(&successNoUsage).Error)
		assert.Contains(t, successNoUsage.Payload, "no_usage_without_settlement")
		// Multimodal sub-category tokens stay observable without poisoning the
		// input/output estimate (image/audio tokens are inside upstream totals).
		info.RequestId = "synthetic-multimodal"
		multiAttempt := service.BeginFinanceAttempt(ctx, info)
		require.NotEmpty(t, multiAttempt)
		service.CaptureFinanceTextUsage(ctx, info, &dto.Usage{PromptTokens: 1000, CompletionTokens: 500, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 200, ImageTokens: 300}, CompletionTokenDetails: dto.OutputTokenDetails{AudioTokens: 100}})
		var multiRecord model.FinanceRequestEvent
		require.NoError(t, db.Where("attempt_id = ? AND phase = ?", multiAttempt, "usage").First(&multiRecord).Error)
		var multiSnapshot service.FinanceUsageSnapshot
		require.NoError(t, common.UnmarshalJsonStr(multiRecord.Payload, &multiSnapshot))
		require.NotNil(t, multiSnapshot.Quantities["input"])
		assert.Equal(t, "800", *multiSnapshot.Quantities["input"])
		require.NotNil(t, multiSnapshot.Observed["image_input_tokens"])
		assert.Equal(t, "300", *multiSnapshot.Observed["image_input_tokens"])
		require.NotNil(t, multiSnapshot.Observed["audio_output_tokens"])
		assert.Equal(t, "100", *multiSnapshot.Observed["audio_output_tokens"])
		require.NotNil(t, multiSnapshot.Estimate.Amount)
		assert.Equal(t, "0.0085", *multiSnapshot.Estimate.Amount)
		service.FinishFinanceAttempt(ctx, info, false)
		// WebSocket transports freeze attempts the same way; realtime usage
		// follows OpenAI semantics with audio details in Observed.
		info.RequestId = "synthetic-ws-usage"
		wsAttempt := service.BeginFinanceAttempt(ctx, info)
		require.NotEmpty(t, wsAttempt)
		service.CaptureFinanceTransport(info, wsAttempt, 101, false)
		service.CaptureFinanceRealtimeUsage(ctx, info, &dto.RealtimeUsage{InputTokens: 1000, OutputTokens: 500, InputTokenDetails: dto.InputTokenDetails{CachedTokens: 100, AudioTokens: 400}, OutputTokenDetails: dto.OutputTokenDetails{AudioTokens: 200}})
		var wsRecord model.FinanceRequestEvent
		require.NoError(t, db.Where("attempt_id = ? AND phase = ?", wsAttempt, "usage").First(&wsRecord).Error)
		var wsSnapshot service.FinanceUsageSnapshot
		require.NoError(t, common.UnmarshalJsonStr(wsRecord.Payload, &wsSnapshot))
		require.NotNil(t, wsSnapshot.Quantities["input"])
		assert.Equal(t, "900", *wsSnapshot.Quantities["input"])
		require.NotNil(t, wsSnapshot.Observed["audio_input_tokens"])
		assert.Equal(t, "400", *wsSnapshot.Observed["audio_input_tokens"])
		service.FinishFinanceAttempt(ctx, info, false)
		// Realtime settlement rides the same text_settlement fact and outbox.
		// Token-ratio pricing keeps the expected charge deterministic; the
		// per-request UsePrice branch of PostWssConsumeQuota has a preexisting
		// zero-charge defect (QuotaInfo never receives ModelPrice) that stays
		// untouched here because changing it would alter real user billing.
		info.RequestId = "synthetic-ws-settlement"
		info.IsPlayground = true
		info.UserSetting = dto.UserSetting{BillingPreference: "wallet_only"}
		info.PriceData.UsePrice, info.PriceData.ModelRatio, info.PriceData.GroupRatioInfo.GroupRatio = false, 1, 1
		// In production WssHelper dials (and freezes the attempt) before the
		// session settles; mirror that order here.
		require.NotEmpty(t, service.BeginFinanceAttempt(ctx, info))
		require.Nil(t, service.PreConsumeBilling(ctx, 20, info))
		service.PostWssConsumeQuota(ctx, info, "public-finance-model", &dto.RealtimeUsage{InputTokens: 500, OutputTokens: 200, TotalTokens: 700, InputTokenDetails: dto.InputTokenDetails{CachedTokens: 50, AudioTokens: 400}, OutputTokenDetails: dto.OutputTokenDetails{AudioTokens: 200}}, "")
		var wsSettlement model.FinanceRequestEvent
		require.NoError(t, db.Where("request_id = ? AND phase = ?", info.RequestId, "text_settlement").First(&wsSettlement).Error)
		assert.Contains(t, wsSettlement.Payload, `"charged_quota":600`)
		assert.Contains(t, wsSettlement.Payload, `"status":"settled"`)
		var wsOutbox model.FinanceSyncEvent
		require.NoError(t, db.Where("event_key = ?", "auto-text-settlement-synthetic-ws-settlement").First(&wsOutbox).Error)
		assert.Equal(t, wsSettlement.ID, wsOutbox.RefID)
		// Task lifecycle facts: submission, idempotent delta, and refund.
		task := &model.Task{TaskID: "task_syntheticfinance01", Platform: constant.TaskPlatformSuno, UserId: 8, ChannelId: 31, Action: "song", SubmitTime: now - 100, FinishTime: now - 50, Properties: model.Properties{UpstreamModelName: "supplier-pool-model"}}
		task.PrivateData.FinanceAttemptID = wsAttempt
		service.CaptureFinanceTaskSubmission(task, common.GetPointer(30), "wallet", "settled", task.PrivateData.FinanceAttemptID)
		service.CaptureFinanceTaskSubmission(task, common.GetPointer(30), "wallet", "settled", task.PrivateData.FinanceAttemptID) // replay cannot double-count
		var submissions int64
		require.NoError(t, db.Model(&model.FinanceRequestEvent{}).Where("request_id = ? AND phase = ?", "task:"+task.TaskID, "task_submission").Count(&submissions).Error)
		assert.Equal(t, int64(1), submissions)
		var submission model.FinanceRequestEvent
		require.NoError(t, db.Where("request_id = ? AND phase = ?", "task:"+task.TaskID, "task_submission").First(&submission).Error)
		assert.Equal(t, task.SubmitTime, submission.At)
		assert.Contains(t, submission.Payload, `"finance_attempt_id":"`+wsAttempt+`"`)
		service.CaptureFinanceTaskAdjustment(task, 45, 30, "token重算", map[string]any{"total_tokens": 123.0})
		service.CaptureFinanceTaskAdjustment(task, 45, 30, "token重算", map[string]any{"total_tokens": 123.0}) // same outcome replays idempotently
		var adjustments int64
		require.NoError(t, db.Model(&model.FinanceRequestEvent{}).Where("request_id = ? AND phase = ?", "task:"+task.TaskID, "task_adjustment").Count(&adjustments).Error)
		assert.Equal(t, int64(1), adjustments)
		var adjustment model.FinanceRequestEvent
		require.NoError(t, db.Where("request_id = ? AND phase = ?", "task:"+task.TaskID, "task_adjustment").First(&adjustment).Error)
		assert.Equal(t, task.FinishTime, adjustment.At)
		assert.Contains(t, adjustment.Payload, `"total_tokens":123`)
		service.CaptureFinanceTaskAdjustment(task, 50, 30, "token重算", nil) // a different outcome is a new fact
		var distinct int64
		require.NoError(t, db.Model(&model.FinanceRequestEvent{}).Where("request_id = ? AND phase = ?", "task:"+task.TaskID, "task_adjustment").Count(&distinct).Error)
		assert.Equal(t, int64(2), distinct)
		service.CaptureFinanceTaskRefund(task, 50, "任务失败", true)
		var refund model.FinanceRequestEvent
		require.NoError(t, db.Where("request_id = ? AND phase = ?", "task:"+task.TaskID, "task_refund").First(&refund).Error)
		assert.Contains(t, refund.Payload, `"refunded":true`)
		// Pool expense registered in USD with a frozen FX reference.
		fxVersion := model.FinanceVersion{Kind: "fx", RecordKey: "pool-usd", EffectiveFrom: now - 1000, EffectiveTo: now + 100000, Active: true, Source: "synthetic fx", CreatedBy: 5}
		require.NoError(t, service.ValidateFinanceVersion(&fxVersion, []byte(`{"currency":"USD","cny_per_unit":"7"}`)))
		require.NoError(t, model.AppendFinanceVersion(db, &fxVersion, 0, "synthetic"))
		expense := model.FinanceExpense{EventKey: "synthetic-pool-expense", Ledger: "direct", Category: "pool_subscription", PoolKey: "synthetic-pool", Amount: "10", Currency: "USD", FXVersionID: fxVersion.ID, PeriodFrom: now - 10, PeriodTo: now + 10, Recurrence: "once", Source: "synthetic supplier invoice", CreatedBy: 5}
		require.NoError(t, service.ValidateFinanceExpense(db, &expense))
		require.NoError(t, model.AppendFinanceExpense(db, &expense, "synthetic"))
		summary, err := service.SummarizeFinancePoolCosts(db, now-10, now+10)
		require.NoError(t, err)
		var poolEntry *service.FinancePoolCostEntry
		for i := range summary.Pools {
			if summary.Pools[i].PoolKey == "synthetic-pool" {
				poolEntry = &summary.Pools[i]
			}
		}
		require.NotNil(t, poolEntry)
		assert.Equal(t, "synthetic-pool", poolEntry.PoolKey)
		require.NotNil(t, poolEntry.ExpenseCNY)
		assert.Equal(t, "70", *poolEntry.ExpenseCNY)
		assert.False(t, poolEntry.ExpenseMissing)
		// Two no-usage attempts stay unknown; multimodal, ws usage, and the
		// realtime settlement are known estimates.
		assert.Equal(t, 3, poolEntry.RequestKnown)
		assert.Equal(t, 2, poolEntry.RequestUnknown)
		require.NotNil(t, poolEntry.RequestCostCNY)
		// The pooled total posts once; unpooled keeps its reconciliation row.
		assert.Contains(t, summary.Warnings, "pool_fees_post_only_in_expense_ledger")
		assert.Contains(t, summary.Warnings, "request_estimates_are_reconciliation_only_not_profit")
		summaryView := request("GET", "/api/owner/finance/pool-cost-summary?from="+strconv.FormatInt(now-10, 10)+"&to="+strconv.FormatInt(now+10, 10), owner, nil)
		require.Equal(t, http.StatusOK, summaryView.Code)
		assert.Contains(t, summaryView.Body.String(), "synthetic-pool")
		for _, expected := range []struct {
			token    string
			expected int
		}{
			{"", http.StatusUnauthorized},
			{"synthetic-admin-pat", http.StatusForbidden},
		} {
			assert.Equal(t, expected.expected, request("GET", "/api/owner/finance/pool-cost-summary?from=1&to=2", expected.token, nil).Code)
		}
	})
	t.Run("phase four supplier bill lines reconcile without double posting", func(t *testing.T) {
		assert.Equal(t, http.StatusServiceUnavailable, request("GET", "/api/owner/finance/supplier-bill-lines?bill_id=1", owner, nil).Code)
		require.NoError(t, model.MigrateFinanceSupplierLines(db))
		require.NoError(t, model.MigrateFinanceSupplierLines(db))
		require.NoError(t, model.FinanceSupplierLinesReady(db))
		now := time.Now().Unix()
		usdFX := model.FinanceVersion{Kind: "fx", RecordKey: "lines-usd", EffectiveFrom: now - 1000, EffectiveTo: now + 100000, Active: true, Source: "synthetic fx", CreatedBy: 5}
		require.NoError(t, service.ValidateFinanceVersion(&usdFX, []byte(`{"currency":"USD","cny_per_unit":"7"}`)))
		require.NoError(t, model.AppendFinanceVersion(db, &usdFX, 0, "synthetic"))
		// The bill keeps its posting total; lines never rewrite it.
		bill, err := service.RegisterFinanceSupplierBill(db, service.FinanceSupplierInput{Scope: "all_channel_request_costs", EventKey: "synthetic-bill-lines-root", ChannelID: 41, PeriodFrom: now - 1000, PeriodTo: now + 10000, Currency: "USD", Amount: "10", FXVersionID: usdFX.ID, Evidence: "synthetic invoice"}, 5, "synthetic")
		require.NoError(t, err)
		require.NotNil(t, bill.AmountCNY)
		// Facts to match against: one retried HTTP request and one task.
		info := &relaycommon.RelayInfo{RequestId: "synthetic-line-req", UserId: 8, OriginModelName: "public-finance-model", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 41, UpstreamModelName: "supplier-lines-model"}}
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader("{}"))
		service.BeginFinanceAttempt(ctx, info)
		service.CaptureFinanceTextUsage(ctx, info, &dto.Usage{PromptTokens: 1000, CompletionTokens: 500})
		info.RetryIndex = 1
		service.BeginFinanceAttempt(ctx, info)
		service.CaptureFinanceTextUsage(ctx, info, &dto.Usage{PromptTokens: 100, CompletionTokens: 50})
		service.FinishFinanceAttempt(ctx, info, false)
		task := &model.Task{TaskID: "task_syntheticlines01", Platform: constant.TaskPlatformSuno, UserId: 8, ChannelId: 41, SubmitTime: now - 10, Properties: model.Properties{UpstreamModelName: "supplier-lines-model"}}
		task.PrivateData.FinanceAttemptID = "unlinked"
		service.CaptureFinanceTaskSubmission(task, common.GetPointer(12), "wallet", "settled", task.PrivateData.FinanceAttemptID)
		linePost := func(body service.FinanceSupplierLineInput, token string) *httptest.ResponseRecorder {
			return request("POST", "/api/owner/finance/supplier-bill-lines", token, body)
		}
		base := func(key string) service.FinanceSupplierLineInput {
			return service.FinanceSupplierLineInput{Scope: "supplier_bill_detail", EventKey: key, BillID: bill.ID, ChannelID: 41, Category: "usage", UpstreamModel: "supplier-lines-model", UsageFrom: now - 500, UsageTo: now + 500, Currency: "USD", Evidence: "synthetic line evidence"}
		}
		lineByKey := func(key string) model.FinanceSupplierBillLine {
			var line model.FinanceSupplierBillLine
			require.NoError(t, db.Where("event_key = ?", key).First(&line).Error)
			return line
		}
		// Usage line in USD with frozen FX; reference matches the retried request.
		usage := base("synthetic-line-usage")
		usage.Amount = "6"
		usage.FXVersionID = usdFX.ID
		usage.Reference = "synthetic-line-req"
		response := linePost(usage, owner)
		require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
		usageLine := lineByKey("synthetic-line-usage")
		require.NotNil(t, usageLine.AmountCNY)
		assert.Equal(t, "42", *usageLine.AmountCNY)
		// Duplicate import of the same line key is rejected.
		assert.Equal(t, http.StatusConflict, linePost(usage, owner).Code)
		// Fixed line in CNY; a credit offsets part of the usage line.
		fixed := base("synthetic-line-fixed")
		fixed.Category, fixed.Currency, fixed.Amount = "fixed", "CNY", "20"
		require.Equal(t, http.StatusCreated, linePost(fixed, owner).Code)
		fixedLine := lineByKey("synthetic-line-fixed")
		credit := base("synthetic-line-credit")
		credit.Category, credit.Currency, credit.Amount = "credit", "USD", "1"
		credit.FXVersionID, credit.ReferencesLineID = usdFX.ID, usageLine.ID
		require.Equal(t, http.StatusCreated, linePost(credit, owner).Code)
		creditLine := lineByKey("synthetic-line-credit")
		// Accumulated credits may not exceed the original line amount.
		excess := base("synthetic-line-credit2")
		excess.Category, excess.Currency, excess.Amount = "credit", "USD", "6"
		excess.FXVersionID, excess.ReferencesLineID = usdFX.ID, usageLine.ID
		assert.Equal(t, http.StatusBadRequest, linePost(excess, owner).Code)
		// Credits cannot reference credits; usage lines cannot reference lines.
		creditOnCredit := base("synthetic-line-credit3")
		creditOnCredit.Category, creditOnCredit.Currency, creditOnCredit.Amount = "credit", "USD", "0.5"
		creditOnCredit.FXVersionID, creditOnCredit.ReferencesLineID = usdFX.ID, creditLine.ID
		assert.Equal(t, http.StatusBadRequest, linePost(creditOnCredit, owner).Code)
		usageWithRef := base("synthetic-line-usageref")
		usageWithRef.Amount, usageWithRef.Currency, usageWithRef.ReferencesLineID = "1", "CNY", usageLine.ID
		assert.Equal(t, http.StatusBadRequest, linePost(usageWithRef, owner).Code)
		// Replacement keeps category, currency, FX and usage period; one line
		// is superseded exactly once.
		replacement := base("synthetic-line-fixed-r2")
		replacement.Category, replacement.Currency, replacement.Amount = "fixed", "CNY", "25"
		replacement.ReplacesLineID = fixedLine.ID
		require.Equal(t, http.StatusCreated, linePost(replacement, owner).Code)
		mismatched := base("synthetic-line-fixed-r3")
		mismatched.Category, mismatched.Currency, mismatched.Amount = "fixed", "CNY", "25"
		mismatched.ReplacesLineID = usageLine.ID
		assert.Equal(t, http.StatusBadRequest, linePost(mismatched, owner).Code)
		again := base("synthetic-line-fixed-r4")
		again.Category, again.Currency, again.Amount = "fixed", "CNY", "26"
		again.ReplacesLineID = fixedLine.ID
		assert.Equal(t, http.StatusConflict, linePost(again, owner).Code)
		// Unmatched reference and an out-of-period line stay review items.
		late := base("synthetic-line-late")
		late.Amount, late.FXVersionID = "1", usdFX.ID
		late.Reference, late.UsageTo = "missing-request", now+20000
		require.Equal(t, http.StatusCreated, linePost(late, owner).Code)
		// EUR line without FX stays unknown, never zero.
		euro := base("synthetic-line-euro")
		euro.Currency, euro.Amount = "EUR", "2"
		require.Equal(t, http.StatusCreated, linePost(euro, owner).Code)
		// Task-referenced line in CNY.
		taskLine := base("synthetic-line-task")
		taskLine.Currency, taskLine.Amount = "CNY", "3"
		taskLine.Reference = "task:task_syntheticlines01"
		require.Equal(t, http.StatusCreated, linePost(taskLine, owner).Code)
		// Audit failure rolls the line back completely.
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register("synthetic_line_audit_fail", func(tx *gorm.DB) {
			if tx.Statement.Table == "finance_audits" {
				tx.AddError(fmt.Errorf("synthetic audit failure"))
			}
		}))
		auditFail := base("synthetic-line-audit-fail")
		auditFail.Amount, auditFail.Currency = "1", "CNY"
		assert.Equal(t, http.StatusServiceUnavailable, linePost(auditFail, owner).Code)
		require.NoError(t, db.Callback().Create().Remove("synthetic_line_audit_fail"))
		var auditFailCount int64
		require.NoError(t, db.Model(&model.FinanceSupplierBillLine{}).Where("event_key = ?", "synthetic-line-audit-fail").Count(&auditFailCount).Error)
		assert.Zero(t, auditFailCount)
		// Summary: the bill total remains the only posting value; category
		// nets include credits; unmatched and out-of-period stay partial.
		summaryResponse := request("GET", "/api/owner/finance/supplier-bill-lines/summary?bill_id="+strconv.FormatInt(bill.ID, 10), owner, nil)
		require.Equal(t, http.StatusOK, summaryResponse.Code, summaryResponse.Body.String())
		var summary service.FinanceSupplierLinesSummary
		require.NoError(t, common.UnmarshalJsonStr(summaryResponse.Body.String(), &struct {
			Data *service.FinanceSupplierLinesSummary `json:"data"`
		}{&summary}))
		assert.Equal(t, 7, summary.TotalLines)
		assert.Equal(t, 6, summary.EffectiveLines)
		assert.Equal(t, 1, summary.CreditLines)
		assert.Equal(t, 1, summary.UnknownFXLines)
		assert.Equal(t, 1, summary.OutOfBillPeriodLines)
		require.NotNil(t, summary.NetCNY)
		assert.Equal(t, "70", *summary.NetCNY) // 42 usage + 25 fixed + 7 late + 3 task - 7 credit; EUR unknown excluded
		// The EUR unknown-FX line keeps the variance uncomputable rather than
		// pretending the bill is explained.
		assert.Nil(t, summary.VarianceCNY)
		require.NotNil(t, summary.UsageNetCNY)
		assert.Equal(t, "45", *summary.UsageNetCNY)
		require.NotNil(t, summary.NonUsageNetCNY)
		assert.Equal(t, "25", *summary.NonUsageNetCNY)
		assert.Equal(t, 1, summary.MatchStats["matched_request"])
		assert.Equal(t, 1, summary.MatchStats["matched_task"])
		assert.Equal(t, 1, summary.MatchStats["unmatched_ref"])
		assert.Equal(t, 3, summary.MatchStats["no_reference"])
		// A bill whose lines are fully known reconciles to a zero variance.
		cnyBill, err := service.RegisterFinanceSupplierBill(db, service.FinanceSupplierInput{Scope: "all_channel_request_costs", EventKey: "synthetic-bill-lines-cny", ChannelID: 42, PeriodFrom: now - 1000, PeriodTo: now + 10000, Currency: "CNY", Amount: "50", Evidence: "synthetic invoice"}, 5, "synthetic")
		require.NoError(t, err)
		cnyLine := base("synthetic-line-cny-clean")
		cnyLine.BillID, cnyLine.ChannelID, cnyLine.Currency, cnyLine.Amount = cnyBill.ID, 42, "CNY", "50"
		require.Equal(t, http.StatusCreated, linePost(cnyLine, owner).Code)
		cleanSummary := request("GET", "/api/owner/finance/supplier-bill-lines/summary?bill_id="+strconv.FormatInt(cnyBill.ID, 10), owner, nil)
		require.Equal(t, http.StatusOK, cleanSummary.Code)
		var clean service.FinanceSupplierLinesSummary
		require.NoError(t, common.UnmarshalJsonStr(cleanSummary.Body.String(), &struct {
			Data *service.FinanceSupplierLinesSummary `json:"data"`
		}{&clean}))
		require.NotNil(t, clean.NetCNY)
		assert.Equal(t, "50", *clean.NetCNY)
		require.NotNil(t, clean.VarianceCNY)
		assert.Equal(t, "0", *clean.VarianceCNY)
		listResponse := request("GET", "/api/owner/finance/supplier-bill-lines?bill_id="+strconv.FormatInt(bill.ID, 10), owner, nil)
		require.Equal(t, http.StatusOK, listResponse.Code)
		assert.Contains(t, listResponse.Body.String(), `"status":"matched_task"`)
		assert.Contains(t, listResponse.Body.String(), `"superseded":true`)
		// Non-owner access stays denied.
		assert.Equal(t, http.StatusForbidden, linePost(usage, "synthetic-admin-pat").Code)
		assert.Equal(t, http.StatusUnauthorized, request("GET", "/api/owner/finance/supplier-bill-lines?bill_id="+strconv.FormatInt(bill.ID, 10), "", nil).Code)
		// The bill amount itself is unchanged by its lines.
		var unchanged model.FinanceSupplierBill
		require.NoError(t, db.First(&unchanged, bill.ID).Error)
		assert.Equal(t, "10", unchanged.Amount)
	})
	t.Run("owner profit API preserves null totals and rejects invalid selection", func(t *testing.T) {
		for _, path := range []string{"?from=100&to=300", "?period=today", "?period=month"} {
			response := request("GET", "/api/owner/finance/profit-summary"+path, owner, nil)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			assert.Contains(t, response.Header().Get("Cache-Control"), "no-store")
			var decoded struct {
				Data service.FinanceProfitSummary `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &decoded))
			assert.False(t, decoded.Data.Complete)
			assert.Nil(t, decoded.Data.Metrics["operating_profit"].Total)
			assert.Len(t, decoded.Data.Snapshot, 64)
		}
		for _, path := range []string{"", "?from=abc&to=300", "?period=month&from=0", "?from=300&to=100", "?period=year", "?from=100&to=300&trend=hour"} {
			assert.Equal(t, http.StatusBadRequest, request("GET", "/api/owner/finance/profit-summary"+path, owner, nil).Code, path)
		}
	})
	t.Run("owner sync discovery is paginated filtered metadata only and read only", func(t *testing.T) {
		for _, status := range []string{model.FinanceSyncBlocked, model.FinanceSyncNeedsReview} {
			require.NoError(t, db.Create(&model.FinanceSyncEvent{EventKey: "ui-discovery-" + status, Topic: "consumption", UserID: 12345, RefID: 321, At: 100, Payload: `{"marker":"synthetic-private-payload-marker"}`, Status: status, ErrorClass: "out_of_order", NextAttemptAt: 200}).Error)
		}
		var auditBefore, auditAfter int64
		require.NoError(t, db.Model(&model.FinanceAudit{}).Count(&auditBefore).Error)
		response := request("GET", "/api/owner/finance/sync-events?user_id=12345&p=1&page_size=1", owner, nil)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		assert.Contains(t, response.Header().Get("Cache-Control"), "no-store")
		assert.NotContains(t, response.Body.String(), "payload")
		assert.NotContains(t, response.Body.String(), "synthetic-private-payload-marker")
		var decoded struct {
			Data struct {
				Items []model.FinanceSyncEvent `json:"items"`
				Total int64                    `json:"total"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &decoded))
		assert.Equal(t, int64(2), decoded.Data.Total)
		require.Len(t, decoded.Data.Items, 1)
		assert.Equal(t, model.FinanceSyncNeedsReview, decoded.Data.Items[0].Status)
		assert.Equal(t, int64(321), decoded.Data.Items[0].RefID)
		response = request("GET", "/api/owner/finance/sync-events?user_id=12345&status=blocked", owner, nil)
		require.Equal(t, http.StatusOK, response.Code)
		decoded.Data.Items = nil
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &decoded))
		assert.Equal(t, int64(1), decoded.Data.Total)
		require.Len(t, decoded.Data.Items, 1)
		assert.Equal(t, model.FinanceSyncBlocked, decoded.Data.Items[0].Status)
		for _, path := range []string{"?user_id=0", "?user_id=abc", "?user_id=2147483648", "?status=invalid", "?page_size=-1"} {
			assert.Equal(t, http.StatusBadRequest, request("GET", "/api/owner/finance/sync-events"+path, owner, nil).Code)
		}
		require.NoError(t, db.Model(&model.FinanceAudit{}).Count(&auditAfter).Error)
		assert.Equal(t, auditBefore, auditAfter)
		var unchanged model.FinanceSyncEvent
		require.NoError(t, db.Where("event_key = ?", "ui-discovery-blocked").First(&unchanged).Error)
		assert.Equal(t, model.FinanceSyncBlocked, unchanged.Status)
		assert.Zero(t, unchanged.Attempts)
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
	t.Run("repeated partial reversals stay within the original consumption and conserve value", func(t *testing.T) {
		input := service.FinanceRevenuePreview{Events: []service.FinanceRevenueEvent{
			credit("paid", "paid", 30, value("3")), consume("use", 30),
			reverse("first-part", "use", 10), reverse("second-part", "use", 10), reverse("last-part", "use", 10),
		}}
		result, err := service.PreviewFinanceRevenue(input)
		require.NoError(t, err)
		assert.Equal(t, "0", *result.ConsumptionCNY)
		assert.Equal(t, "3", *result.BackingValueCNY)
		assert.Equal(t, int64(30), result.RemainingQuota)
		require.Len(t, result.Allocations[1].Parts, 1)
		assert.Equal(t, "-1", *result.Allocations[1].Parts[0].ValueCNY)
		assert.Equal(t, "-1", *result.Allocations[2].Parts[0].ValueCNY)
		assert.Equal(t, "-1", *result.Allocations[3].Parts[0].ValueCNY)
		// Reversed quota consumed again recognizes exactly the original values.
		again := service.FinanceRevenuePreview{Events: append(input.Events, service.FinanceRevenueEvent{Key: "reconsume", At: 4, Kind: "consume", Quota: 30})}
		reconsumed, err := service.PreviewFinanceRevenue(again)
		require.NoError(t, err)
		assert.Equal(t, "3", *reconsumed.ConsumptionCNY)
		assert.Equal(t, int64(0), reconsumed.RemainingQuota)
	})
	t.Run("manual adjust facts grant without cash value and forfeit without revenue", func(t *testing.T) {
		result, err := service.PreviewFinanceRevenue(service.FinanceRevenuePreview{Events: []service.FinanceRevenueEvent{
			{Key: "grant-gift", At: 1, Kind: "adjust", Quota: 20, Source: "gift", ValueCNY: value("0")},
			{Key: "grant-unknown", At: 2, Kind: "adjust", Quota: 10, Source: "unknown"},
			{Key: "forfeit", At: 3, Kind: "adjust", Quota: -15},
			{Key: "use", At: 4, Kind: "consume", Quota: 15},
		}})
		require.NoError(t, err)
		assert.Equal(t, "partial", result.Status)
		assert.Equal(t, "0", result.KnownBackingValueCNY)
		assert.Nil(t, result.BackingValueCNY)
		assert.Nil(t, result.ConsumptionCNY) // unknown-source quota was consumed
		assert.Equal(t, int64(10), result.UnknownConsumedQuota)
		assert.Equal(t, int64(0), result.RemainingQuota)
		require.Len(t, result.Allocations[0].Parts, 1) // forfeit took from the gift lot only
		assert.Equal(t, "grant-gift", result.Allocations[0].Parts[0].LotKey)
		assert.Nil(t, result.Allocations[0].Parts[0].ValueCNY)
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
		{"over reversal", []service.FinanceRevenueEvent{credit("g", "gift", 10, value("0")), consume("c", 10), reverse("r", "c", 10), reverse("r2", "c", 10)}},
		{"paid manual grant", []service.FinanceRevenueEvent{{Key: "a", At: 1, Kind: "adjust", Quota: 5, Source: "paid", ValueCNY: value("1")}}},
		{"gift adjust with value", []service.FinanceRevenueEvent{{Key: "a", At: 1, Kind: "adjust", Quota: 5, Source: "gift", ValueCNY: value("1")}}},
		{"zero adjust", []service.FinanceRevenueEvent{{Key: "a", At: 1, Kind: "adjust", Quota: 0}}},
		{"forfeit beyond balance", []service.FinanceRevenueEvent{credit("g", "gift", 5, value("0")), {Key: "a", At: 2, Kind: "adjust", Quota: -6}}},
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
	require.NoError(t, model.MigrateFinanceSupplier(db))
	require.NoError(t, model.MigrateFinanceSupplier(db))
	require.NoError(t, model.FinanceSupplierReady(db))
	assert.True(t, db.Migrator().HasTable(&model.FinanceSupplierBill{}))
	require.NoError(t, model.MigrateFinanceRefunds(db))
	require.NoError(t, model.MigrateFinanceRefunds(db))
	require.NoError(t, model.FinanceRefundsReady(db))
	assert.True(t, db.Migrator().HasTable(&model.FinanceCashRefund{}))
	require.NoError(t, model.MigrateFinanceSync(db))
	require.NoError(t, model.MigrateFinanceSync(db))
	require.NoError(t, model.FinanceSyncReady(db))
	assert.True(t, db.Migrator().HasTable(&model.FinanceSyncEvent{}))
	require.NoError(t, model.MigrateFinanceRebuilds(db))
	require.NoError(t, model.MigrateFinanceRebuilds(db))
	require.NoError(t, model.FinanceRebuildsReady(db))
	assert.True(t, db.Migrator().HasTable(&model.FinanceRebuildSnapshot{}))
	assert.False(t, db.Migrator().HasTable(&model.User{}))
	require.NoError(t, model.MigrateFinanceSupplierLines(db))
	require.NoError(t, model.MigrateFinanceSupplierLines(db))
	t.Run("profit snapshot stock flows gates and reconciliation", func(t *testing.T) {
		previousDB := model.DB
		model.DB = db
		defer func() { model.DB = previousDB }()
		t.Setenv("CLEARAPI_FINANCE_ENABLED", "true")
		t.Setenv("CLEARAPI_FINANCE_OWNER_USER_ID", "5")
		common.InitFinanceConfig()
		require.NoError(t, db.AutoMigrate(&model.User{}))
		require.NoError(t, db.Create(&model.User{Id: 5, Username: "synthetic-profit-owner", AffCode: "profit-aff", Quota: 777}).Error)
		beijing := time.FixedZone("Asia/Shanghai", 8*3600)
		from := time.Date(2024, 1, 31, 0, 0, 0, 0, beijing).Unix()
		to := from + 2*86400
		value := func(text string) *string { return &text }
		empty, err := service.SummarizeFinanceProfit(db, from, to, "")
		require.NoError(t, err)
		assert.Nil(t, empty.Metrics["operating_expenses"].RecordedTotal)
		assert.Nil(t, empty.Metrics["operating_profit"].Total)
		assert.Nil(t, empty.RecordedMargin)
		assert.False(t, empty.Complete)
		for i, event := range []service.FinanceRevenueEvent{
			{Key: "profit-source", At: from - 1, Kind: "credit", Source: "paid", Quota: 100, ValueCNY: value("100")},
			{Key: "profit-consume", At: from, Kind: "consume", Quota: 30},
			{Key: "profit-reverse", At: from + 86400, Kind: "reverse", Quota: 10, Reverses: "profit-consume"},
			{Key: "profit-outside", At: to, Kind: "consume", Quota: 15},
		} {
			_, err := service.AppendFinanceRevenue(db, 5, 5, service.FinanceRevenueWrite{ExpectedRevision: int64(i), Event: event, Evidence: "synthetic profit test"}, "profit-test")
			require.NoError(t, err)
		}
		require.NoError(t, db.Create(&model.FinanceCashReceipt{OrderID: 1, AmountCNY: "150", PaidAt: from, Evidence: "synthetic receipt"}).Error)
		require.NoError(t, db.Create(&model.FinanceCashReceipt{OrderID: 2, AmountCNY: "999", PaidAt: to, Evidence: "outside period"}).Error)
		require.NoError(t, db.Create(&model.FinanceCashRefund{EventKey: "profit-refund", ReceiptID: 1, State: "succeeded", AmountCNY: "5", RefundedAt: from + 86400}).Error)
		require.NoError(t, db.Create(&model.FinanceCashRefund{EventKey: "profit-pending", ReceiptID: 1, State: "pending", AmountCNY: "9", RefundedAt: from}).Error)
		fx := model.FinanceVersion{Kind: "fx", RecordKey: "profit-fx", Revision: 1, EffectiveFrom: from - 86400, EffectiveTo: to + 86400, Active: true, Payload: `{"currency":"USD","cny_per_unit":"7"}`}
		require.NoError(t, db.Create(&fx).Error)
		require.NoError(t, db.Create(&model.FinanceVersion{Kind: "fx", RecordKey: "profit-fx", Revision: 2, EffectiveFrom: from, EffectiveTo: to + 86400, Active: true, Payload: `{"currency":"USD","cny_per_unit":"9"}`}).Error)
		require.NoError(t, db.Create(&model.FinanceVersion{Kind: "pool", RecordKey: "profit-pool", Revision: 1, EffectiveFrom: from, EffectiveTo: to, Active: true, Payload: `{"name":"synthetic pool","platform":"synthetic","channel_ids":[91]}`}).Error)
		require.NoError(t, db.Create(&model.FinanceExpense{EventKey: "profit-pool-fee", Ledger: "direct", PoolKey: "profit-pool", Category: "pool", Amount: "2", Currency: "CNY", PeriodFrom: from, PeriodTo: to, Recurrence: "once"}).Error)
		require.NoError(t, db.Create(&model.FinanceExpense{EventKey: "profit-operating", Ledger: "operating", Category: "hosting", Amount: "3", Currency: "USD", FXVersionID: fx.ID, PeriodFrom: from, PeriodTo: to + 86400, Recurrence: "once"}).Error)
		original := model.FinanceSupplierBill{EventKey: "profit-bill-old", Scope: "all_channel_request_costs", ChannelID: 91, Revision: 1, PeriodFrom: from, PeriodTo: to, Currency: "CNY", Amount: "40", AmountCNY: value("40")}
		require.NoError(t, db.Create(&original).Error)
		bill := model.FinanceSupplierBill{EventKey: "profit-bill", Scope: "all_channel_request_costs", ChannelID: 91, Revision: 2, ReplacesID: original.ID, PeriodFrom: from, PeriodTo: to, Currency: "CNY", Amount: "30", AmountCNY: value("30")}
		require.NoError(t, db.Create(&bill).Error)
		require.NoError(t, db.Create(&model.FinanceSupplierBillLine{EventKey: "profit-usage-line", BillID: bill.ID, ChannelID: 91, Category: "usage", UsageFrom: from, UsageTo: to, Currency: "CNY", Amount: "30", AmountCNY: value("30")}).Error)
		require.NoError(t, db.Create(&model.FinanceRequestEvent{EventKey: "profit-attempt", RequestID: "profit-request", AttemptID: "profit-attempt", ChannelID: 91, UpstreamModel: "synthetic-model", Phase: "attempt_start", At: from, Payload: `{"pool_key":"profit-pool"}`}).Error)
		require.NoError(t, db.Create(&model.FinanceRequestEvent{EventKey: "profit-usage", RequestID: "profit-request", AttemptID: "profit-attempt", ChannelID: 91, UpstreamModel: "synthetic-model", Phase: "usage", At: to + 100, Payload: `{"estimated_cost_cny":"99"}`}).Error)
		var auditCount int64
		require.NoError(t, db.Model(&model.FinanceAudit{}).Count(&auditCount).Error)
		result, err := service.SummarizeFinanceProfit(db, from, to, "day")
		require.NoError(t, err)
		for key, expected := range map[string]string{"cash_receipts": "150", "cash_refunds": "5", "net_cash_receipts": "145", "opening_source_value": "100", "closing_source_value": "80", "consumption_gross": "30", "consumption_adjustments": "-10", "consumption_revenue": "20", "direct_cost": "32", "gross_profit": "-12", "operating_expenses": "14", "operating_profit": "-26", "request_cost_estimate": "99"} {
			assert.Equal(t, expected, result.Metrics[key].KnownSubtotal, key)
			require.NotNil(t, result.Metrics[key].RecordedTotal, key)
			assert.Equal(t, expected, *result.Metrics[key].RecordedTotal, key)
			assert.Nil(t, result.Metrics[key].Total, key)
		}
		require.NotNil(t, result.RecordedMargin)
		assert.Equal(t, "-1.3", *result.RecordedMargin)
		assert.Nil(t, result.ProfitMargin)
		require.Len(t, result.Trend, 2)
		assert.Nil(t, result.Trend[0].Metrics["direct_cost"].RecordedTotal) // cannot prorate the bill
		assert.Equal(t, "30", result.Trend[0].Metrics["consumption_revenue"].KnownSubtotal)
		assert.Equal(t, "-10", result.Trend[1].Metrics["consumption_revenue"].KnownSubtotal)
		operatingTrend := decimal.Zero
		for _, bucket := range result.Trend {
			amount, err := decimal.NewFromString(bucket.Metrics["operating_expenses"].KnownSubtotal)
			require.NoError(t, err)
			operatingTrend = operatingTrend.Add(amount)
		}
		assert.Equal(t, "14", operatingTrend.String())
		for _, dimension := range []string{"channel", "upstream_model", "pool"} {
			total := decimal.Zero
			for _, entry := range result.Dimensions {
				if entry.Dimension == dimension {
					amount, err := decimal.NewFromString(entry.Metrics["direct_cost"].KnownSubtotal)
					require.NoError(t, err)
					total = total.Add(amount)
				}
			}
			assert.Equal(t, "32", total.String(), dimension)
		}
		same, err := service.SummarizeFinanceProfit(db, from, to, "")
		require.NoError(t, err)
		assert.Equal(t, result.Snapshot, same.Snapshot)
		var afterAudit int64
		require.NoError(t, db.Model(&model.FinanceAudit{}).Count(&afterAudit).Error)
		assert.Equal(t, auditCount, afterAudit)
		var user struct{ Quota int }
		require.NoError(t, db.Model(&model.User{}).Select("quota").Where("id = ?", 5).Scan(&user).Error)
		assert.Equal(t, 777, user.Quota)
		// Unreferenced direct expense cannot be summed with a bill safely.
		require.NoError(t, db.Create(&model.FinanceExpense{EventKey: "profit-duplicate-risk", Ledger: "direct", Category: "request", Amount: "30", Currency: "CNY", PeriodFrom: from, PeriodTo: to, Recurrence: "once"}).Error)
		require.NoError(t, db.Create(&model.FinanceExpense{EventKey: "profit-unknown-fx", Ledger: "operating", Category: "support", Amount: "1", Currency: "EUR", PeriodFrom: from, PeriodTo: to, Recurrence: "once"}).Error)
		require.NoError(t, db.Create(&model.FinanceRequestEvent{EventKey: "profit-unbilled", RequestID: "unbilled", AttemptID: "unbilled", ChannelID: 92, Phase: "attempt_start", At: from + 10, Payload: `{}`}).Error)
		partial, err := service.SummarizeFinanceProfit(db, from, to, "")
		require.NoError(t, err)
		assert.Equal(t, "32", partial.Metrics["direct_cost"].KnownSubtotal)
		assert.Equal(t, 2, partial.Metrics["direct_cost"].UnknownItems)
		assert.Nil(t, partial.Metrics["gross_profit"].RecordedTotal)
		assert.Nil(t, partial.Metrics["operating_profit"].RecordedTotal)
		assert.Equal(t, 1, partial.Metrics["operating_expenses"].UnknownItems)
		assert.NotEqual(t, result.Snapshot, partial.Snapshot)
		// Lateness changes the report hash and period posting, never cash.
		require.NoError(t, db.Create(&model.FinanceExpense{EventKey: "profit-late-adjustment", Ledger: "operating", Category: "hosting", Amount: "-1", Currency: "USD", FXVersionID: fx.ID, AdjustsID: 2, PeriodFrom: from, PeriodTo: to, Recurrence: "once"}).Error)
		late, err := service.SummarizeFinanceProfit(db, from, to, "")
		require.NoError(t, err)
		assert.Equal(t, "7", late.Metrics["operating_expenses"].KnownSubtotal)
		assert.Equal(t, "145", late.Metrics["net_cash_receipts"].KnownSubtotal)
		assert.NotEqual(t, partial.Snapshot, late.Snapshot)
		// A partially remaining unknown lot poisons stock and consumption.
		_, err = service.AppendFinanceRevenue(db, 5, 5, service.FinanceRevenueWrite{ExpectedRevision: 4, Event: service.FinanceRevenueEvent{Key: "profit-unknown-source", At: to + 1, Kind: "credit", Source: "unknown", Quota: 100}, Evidence: "synthetic"}, "profit")
		require.NoError(t, err)
		_, err = service.AppendFinanceRevenue(db, 5, 5, service.FinanceRevenueWrite{ExpectedRevision: 5, Event: service.FinanceRevenueEvent{Key: "profit-unknown-consume", At: to + 2, Kind: "consume", Quota: 70}, Evidence: "synthetic"}, "profit")
		require.NoError(t, err)
		unknown, err := service.SummarizeFinanceProfit(db, to, to+3, "")
		require.NoError(t, err)
		assert.Equal(t, 1, unknown.Metrics["consumption_revenue"].UnknownItems)
		assert.Nil(t, unknown.Metrics["consumption_revenue"].RecordedTotal)
		assert.Nil(t, unknown.Metrics["closing_source_value"].RecordedTotal)
		historical, err := service.SummarizeFinanceProfit(db, from, to, "")
		require.NoError(t, err)
		assert.Equal(t, "80", historical.Metrics["closing_source_value"].KnownSubtotal)
		// Idle pools remain a fee gap even when no request was sent.
		require.NoError(t, db.Create(&model.FinanceVersion{Kind: "pool", RecordKey: "profit-idle", Revision: 1, EffectiveFrom: from, EffectiveTo: to, Active: true, Payload: `{"name":"idle","platform":"synthetic","channel_ids":[93]}`}).Error)
		require.NoError(t, db.Create(&model.FinanceExpense{EventKey: "profit-rounding", Ledger: "operating", Category: "rounding", Amount: "1", Currency: "CNY", PeriodFrom: from, PeriodTo: to + 86400, Recurrence: "once"}).Error)
		rounded, err := service.SummarizeFinanceProfit(db, from, to, "day")
		require.NoError(t, err)
		assert.Contains(t, rounded.Metrics["direct_cost"].Coverage, "pool_expense_registration_missing")
		trendTotal := decimal.Zero
		for _, bucket := range rounded.Trend {
			amount, err := decimal.NewFromString(bucket.Metrics["operating_expenses"].KnownSubtotal)
			require.NoError(t, err)
			trendTotal = trendTotal.Add(amount)
		}
		assert.Equal(t, rounded.Metrics["operating_expenses"].KnownSubtotal, trendTotal.String())
		// Beijing calendar selection and strict half-open boundaries.
		start, end, err := service.FinanceProfitRange("today", 0, 0, time.Unix(from+3600, 0))
		require.NoError(t, err)
		assert.Equal(t, from, start)
		assert.Equal(t, from+86400, end)
		start, end, err = service.FinanceProfitRange("month", 0, 0, time.Unix(from, 0))
		require.NoError(t, err)
		assert.Equal(t, time.Date(2024, 1, 1, 0, 0, 0, 0, beijing).Unix(), start)
		assert.Equal(t, from+86400, end)
		_, _, err = service.FinanceProfitRange("today", from, to, time.Now())
		assert.ErrorIs(t, err, model.ErrFinanceInvalid)
		_, err = service.SummarizeFinanceProfit(db, from, to, "hour")
		assert.ErrorIs(t, err, model.ErrFinanceInvalid)
	})
}
