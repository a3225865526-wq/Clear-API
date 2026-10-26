package controller

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	consoleProbeModel    = "gpt-6-luna"
	consoleProbeProtocol = "openai_responses"
)

func TestConsoleMonitorTriggerRecordsCostDenialWithoutDispatch(t *testing.T) {
	db := modelManagementDB(t, "sqlite", "")
	require.NoError(t, model.MigrateConsoleMonitoringDB(db))
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer upstream.Close()
	channel := model.Channel{Id: 1, Name: "isolated", Key: "fixture-only", Status: common.ChannelStatusEnabled, Models: consoleProbeModel, BaseURL: &upstream.URL}
	require.NoError(t, db.Create(&channel).Error)
	m := model.ConsoleMonitor{ChannelID: 1, TargetKind: "channel", TargetRef: "1", Model: consoleProbeModel, Protocol: consoleProbeProtocol, DisplayName: "isolated", IntervalSeconds: 60, TimeoutSeconds: 1, StaleSeconds: 900, DailyRequests: 2, RequestCostMicros: 100}
	ctx := context.Background()
	require.NoError(t, model.SaveConsoleMonitor(ctx, db, &m))
	require.NoError(t, model.SetConsoleMonitorAllowlisted(ctx, db, m.ID, true))
	require.NoError(t, model.SetConsoleMonitorSwitch(ctx, db, m.ID, "published", true))
	require.NoError(t, model.SetConsoleMonitorSwitch(ctx, db, m.ID, "enabled", true))
	require.NoError(t, model.SaveConsoleMonitorPolicy(ctx, db, model.ConsoleMonitorPolicy{ID: 1, Enabled: true, MaxConcurrent: 1, DailyRequests: 2, DailyCostMicros: 1000, RetentionDays: 90}))
	router := gin.New()
	router.POST("/monitors/:id/probe", ConsoleMonitorWriteGuard, TriggerConsoleMonitorAdmin)
	for _, example := range []struct {
		path, body, header string
		status             int
	}{
		{"/monitors/0/probe", `{"confirm":true}`, "1", 400},
		{"/monitors/1/probe", `{`, "1", 400},
		{"/monitors/1/probe", `{"confirm":false}`, "1", 400},
		{"/monitors/1/probe", `{"confirm":true}`, "", 403},
	} {
		req := httptest.NewRequest(http.MethodPost, example.path, bytes.NewBufferString(example.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-ClearAPI-Monitor", example.header)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		assert.Equal(t, example.status, response.Code)
	}
	var attempts int64
	require.NoError(t, db.Model(&model.ConsoleMonitorAttempt{}).Count(&attempts).Error)
	assert.Zero(t, attempts)
	req := httptest.NewRequest(http.MethodPost, "/monitors/1/probe", bytes.NewBufferString(`{"confirm":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ClearAPI-Monitor", "1")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var body struct {
		Success bool
		Data    service.ConsoleProbeResult
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
	require.True(t, body.Success)
	assert.Equal(t, "blocked", body.Data.Status)
	assert.Equal(t, "MONITOR_COST_UNVERIFIED", body.Data.ErrorCode)
	var probe model.ConsoleProbe
	require.NoError(t, db.First(&probe, body.Data.ProbeID).Error)
	assert.Equal(t, body.Data.Status, probe.Status)
	assert.Equal(t, body.Data.ErrorCode, probe.ErrorCode)
	var attempt model.ConsoleMonitorAttempt
	require.NoError(t, db.First(&attempt, body.Data.AttemptID).Error)
	assert.Positive(t, attempt.FinishedAt)
	assert.EqualValues(t, 100, attempt.ReservedCostMicros)
	assert.Zero(t, calls.Load(), "a recorded cost denial must never contact an upstream")
	m.Allowlisted, m.Published, m.Enabled = true, true, true
	adapter := channelConsoleProbeAdapter{}
	require.ErrorIs(t, adapter.Authorize(ctx, m), service.ErrMonitorCostUnverified)
	_, err := adapter.Probe(ctx, m, attempt.ID)
	require.ErrorIs(t, err, service.ErrMonitorCostUnverified)
	assert.Zero(t, calls.Load())
	m.Published = false
	require.ErrorIs(t, adapter.Authorize(ctx, m), model.ErrMonitorPaused)
	var saved model.Channel
	require.NoError(t, db.First(&saved, 1).Error)
	assert.Equal(t, channel.Status, saved.Status)
	assert.Equal(t, channel.Key, saved.Key)
}

// Full host HTTP path with a synthetic provider, not a supplier acceptance test.
func TestConsoleMonitorConfiguredHTTPFlow(t *testing.T) {
	db := modelManagementDB(t, "sqlite", "")
	require.NoError(t, model.MigrateConsoleMonitoringDB(db))
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.Equal(t, "Bearer fixture-only", r.Header.Get("Authorization"))
		assert.Equal(t, "/v1/responses", r.URL.Path)
		_, _ = w.Write([]byte(`{"id":"resp-http","status":"completed","model":"gpt-6-luna","usage":{"input_tokens":4,"output_tokens":2}}`))
	}))
	defer upstream.Close()
	channel := model.Channel{Id: 1, Name: "isolated", Key: "fixture-only", Status: common.ChannelStatusEnabled, Models: consoleProbeModel, BaseURL: &upstream.URL}
	require.NoError(t, db.Create(&channel).Error)
	m := model.ConsoleMonitor{ChannelID: 1, TargetKind: "channel", TargetRef: "1", Model: consoleProbeModel, Protocol: consoleProbeProtocol, DisplayName: "isolated", IntervalSeconds: 60, TimeoutSeconds: 1, StaleSeconds: 900, DailyRequests: 2, RequestCostMicros: 100}
	ctx := context.Background()
	require.NoError(t, model.SaveConsoleMonitor(ctx, db, &m))
	require.NoError(t, model.SetConsoleMonitorAllowlisted(ctx, db, m.ID, true))
	require.NoError(t, model.SetConsoleMonitorSwitch(ctx, db, m.ID, "published", true))
	router := gin.New()
	router.PUT("/monitors/:id/cost-contract", ConsoleMonitorWriteGuard, SaveConsoleMonitorCostContract)
	router.PUT("/policy", ConsoleMonitorWriteGuard, SaveConsoleMonitorPolicyAdmin)
	router.PUT("/monitors/:id/:action", ConsoleMonitorWriteGuard, SwitchConsoleMonitorAdmin)
	router.POST("/monitors/:id/probe", ConsoleMonitorWriteGuard, TriggerConsoleMonitorAdmin)
	router.GET("/monitors", ListConsoleMonitorAdmin)
	router.GET("/monitoring", GetConsoleMonitoring)
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		raw, err := common.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-ClearAPI-Monitor", "1")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		require.Equal(t, 200, response.Code, response.Body.String())
		return response
	}
	contract := service.ConsoleProbeCostContract{Source: "synthetic HTTP fixture, not supplier evidence", ValidFrom: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour), InputTokenLimit: 64, InputMicros: 1, OutputMicros: 2, FixedFeeMicros: 4, HardCeilingMicros: 100}
	request(http.MethodPut, "/monitors/1/cost-contract", gin.H{"confirm": true, "contract": contract})
	request(http.MethodPut, "/policy", gin.H{"confirm": true, "policy": model.ConsoleMonitorPolicy{ID: 1, Enabled: true, MaxConcurrent: 1, DailyRequests: 2, DailyCostMicros: 200, RetentionDays: 90}})
	request(http.MethodPut, "/monitors/1/enabled", gin.H{"confirm": true, "value": true})
	assert.Contains(t, request(http.MethodGet, "/monitors", nil).Body.String(), `"real_probe_ready":true`)
	response := request(http.MethodPost, "/monitors/1/probe", gin.H{"confirm": true})
	assert.Contains(t, response.Body.String(), `"status":"normal"`)
	assert.Contains(t, response.Body.String(), `"upstream_response_id":"resp-http"`)
	var public struct{ Data model.ConsoleMonitorData }
	require.NoError(t, common.Unmarshal(request(http.MethodGet, "/monitoring", nil).Body.Bytes(), &public))
	require.Len(t, public.Data.Channels, 1)
	require.Len(t, public.Data.Channels[0].Models, 1)
	assert.Equal(t, "normal", public.Data.Channels[0].Models[0].Status)
	assert.EqualValues(t, 1, calls.Load())
	assert.True(t, (consoleMonitorHandler{}).Enabled())
	require.NoError(t, model.SaveConsoleMonitorPolicy(ctx, db, model.ConsoleMonitorPolicy{ID: 1, MaxConcurrent: 1, RetentionDays: 90}))
	assert.True(t, (consoleMonitorHandler{}).Enabled(), "daily observations continue while paid execution is paused")
}

func TestConsoleMonitorEnableDenials(t *testing.T) {
	db := modelManagementDB(t, "sqlite", "")
	require.NoError(t, model.MigrateConsoleMonitoringDB(db))
	channel := model.Channel{Id: 1, Key: "fixture-only", Status: common.ChannelStatusEnabled, Models: "gpt-6-luna,gpt-6.1-sol"}
	require.NoError(t, db.Create(&channel).Error)
	m := model.ConsoleMonitor{ChannelID: 1, TargetKind: "channel", TargetRef: "1", Model: consoleProbeModel, Protocol: consoleProbeProtocol, DisplayName: "isolated", IntervalSeconds: 60, TimeoutSeconds: 1, StaleSeconds: 900, DailyRequests: 2, RequestCostMicros: 100}
	require.NoError(t, model.SaveConsoleMonitor(context.Background(), db, &m))
	router := gin.New()
	router.PUT("/monitors/:id/:action", ConsoleMonitorWriteGuard, SwitchConsoleMonitorAdmin)
	for _, tc := range []struct {
		name, model, protocol, contract, code string
		allowlisted, published                bool
		status                                int
	}{
		{"reported target", "gpt-6.1-sol", "openai_chat", "", "MONITOR_PAUSED", false, true, 409},
		{"not allowlisted", consoleProbeModel, consoleProbeProtocol, "", "MONITOR_PAUSED", false, true, 409},
		{"not published", consoleProbeModel, consoleProbeProtocol, "", "MONITOR_PAUSED", true, false, 409},
		{"missing contract", consoleProbeModel, consoleProbeProtocol, "", "MONITOR_COST_UNVERIFIED", true, true, 409},
		{"invalid contract", consoleProbeModel, consoleProbeProtocol, "{", "MONITOR_COST_UNVERIFIED", true, true, 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, db.Model(&m).Updates(map[string]any{"model": tc.model, "protocol": tc.protocol, "allowlisted": tc.allowlisted, "published": tc.published}).Error)
			if tc.contract != "" {
				require.NoError(t, db.Create(&model.Option{Key: "console_monitor.cost.1", Value: tc.contract}).Error)
			}
			req := httptest.NewRequest(http.MethodPut, "/monitors/1/enabled", bytes.NewBufferString(`{"confirm":true,"value":true}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-ClearAPI-Monitor", "1")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			assert.Equal(t, tc.status, response.Code)
			var body struct{ Code, Message string }
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
			assert.Equal(t, tc.code, body.Code)
			assert.NotEqual(t, body.Code, body.Message, "the response must explain the actual denial")
			var saved model.ConsoleMonitor
			require.NoError(t, db.First(&saved, m.ID).Error)
			assert.False(t, saved.Enabled)
		})
	}
	var attempts int64
	require.NoError(t, db.Model(&model.ConsoleMonitorAttempt{}).Count(&attempts).Error)
	assert.Zero(t, attempts, "enabling never dispatches or reserves a probe")
}

func TestDeleteArchivedConsoleMonitorDatabaseMatrix(t *testing.T) {
	for _, dialect := range []struct{ kind, env string }{{"sqlite", ""}, {"mysql", "TEST_MYSQL_DSN"}, {"postgres", "TEST_POSTGRES_DSN"}} {
		t.Run(dialect.kind, func(t *testing.T) {
			if dialect.env != "" && os.Getenv(dialect.env) == "" {
				t.Skip("set " + dialect.env + " to run this database")
			}
			db := modelManagementDB(t, dialect.kind, os.Getenv(dialect.env))
			if dialect.kind == "sqlite" {
				connection, err := db.DB()
				require.NoError(t, err)
				// modelManagementDB reopens the fixture with a plain file path.
				// Serialize its SQLite writers as in consoleMonitorFixture.
				connection.SetMaxOpenConns(1)
			}
			ctx := context.Background()
			require.NoError(t, model.MigrateConsoleMonitoringDB(db))
			require.NoError(t, model.MigrateConsoleMonitoringDB(db))
			channel := model.Channel{Id: 1, Name: "preserved upstream", Key: "synthetic-only", Status: common.ChannelStatusEnabled, Models: consoleProbeModel}
			require.NoError(t, db.Create(&channel).Error)
			m := model.ConsoleMonitor{ChannelID: 1, TargetKind: "channel", TargetRef: "1", Model: consoleProbeModel, Protocol: consoleProbeProtocol, DisplayName: "Archived directory", IntervalSeconds: 60, TimeoutSeconds: 1, StaleSeconds: 900}
			require.NoError(t, model.SaveConsoleMonitor(ctx, db, &m))
			require.NoError(t, model.SetConsoleMonitorSwitch(ctx, db, m.ID, "archived", true))
			probe := model.ConsoleProbe{MonitorID: m.ID, CheckedAt: 100, Status: "normal", SourceChannelID: 1}
			require.NoError(t, db.Create(&probe).Error)
			attempt := model.ConsoleMonitorAttempt{MonitorID: m.ID, StartedAt: 90, FinishedAt: 100, ReservedCostMicros: 7, CleanupState: "released"}
			require.NoError(t, db.Create(&attempt).Error)
			// Reconstruct the released schema: the only difference is deleted_at.
			// Upgrade twice with real directory/history rows and its unique identity.
			require.NoError(t, db.Migrator().DropColumn(&model.ConsoleMonitor{}, "deleted_at"))
			require.False(t, model.ConsoleMonitoringReady(db))
			require.ErrorIs(t, model.DeleteArchivedConsoleMonitor(ctx, db, m.ID), model.ErrMonitorNotReady)
			require.NoError(t, model.MigrateConsoleMonitoringDB(db))
			require.NoError(t, model.MigrateConsoleMonitoringDB(db))
			var upgraded model.ConsoleMonitor
			require.NoError(t, db.First(&upgraded, m.ID).Error)
			assert.Equal(t, m.DisplayName, upgraded.DisplayName)
			assert.Equal(t, m.Identity, upgraded.Identity)
			assert.True(t, upgraded.Archived)
			duplicate := upgraded
			duplicate.ID = 0
			require.Error(t, db.Create(&duplicate).Error, "migration must retain identity uniqueness")
			for _, tc := range []struct {
				name                         string
				archived, enabled, published bool
			}{
				{"restored since list read", false, false, false},
				{"enabled", true, true, false},
				{"published reference", true, false, true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					require.NoError(t, db.Model(&m).Updates(map[string]any{"archived": tc.archived, "enabled": tc.enabled, "published": tc.published}).Error)
					require.ErrorIs(t, model.DeleteArchivedConsoleMonitor(ctx, db, m.ID), model.ErrMonitorDeleteState)
				})
			}
			require.NoError(t, db.Model(&m).Updates(map[string]any{"archived": true, "enabled": false, "published": false}).Error)
			active := model.ConsoleMonitorAttempt{MonitorID: m.ID, StartedAt: 1, LeaseUntil: 2, CleanupState: "cleanup_pending"}
			require.NoError(t, db.Create(&active).Error)
			require.ErrorIs(t, model.DeleteArchivedConsoleMonitor(ctx, db, m.ID), model.ErrMonitorLease, "expired but unfinished probes still protect the entry")
			require.NoError(t, db.Model(&active).Updates(map[string]any{"finished_at": 3, "cleanup_state": "released"}).Error)
			injected := errors.New("synthetic delete failure")
			require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register("test:monitor_delete_failure", func(tx *gorm.DB) {
				if tx.Statement.Table == "console_monitors" {
					tx.AddError(injected)
				}
			}))
			err := model.DeleteArchivedConsoleMonitor(ctx, db, m.ID)
			require.NoError(t, db.Callback().Delete().Remove("test:monitor_delete_failure"))
			require.ErrorIs(t, err, injected)
			var retained model.ConsoleMonitor
			require.NoError(t, db.First(&retained, m.ID).Error)
			assert.Equal(t, m.Identity, retained.Identity, "identity release must roll back with deletion")
			assert.False(t, retained.DeletedAt.Valid)
			// Snapshots cover the actual upstream, user funds and separate log DB.
			require.NoError(t, db.AutoMigrate(&model.FinanceWallet{}, &model.FinanceCashRefund{}, &model.BillingReservation{}))
			wallet := model.FinanceWallet{UserID: 1, RemainingQuota: 500}
			user := model.User{Id: 19, Username: "preserved-user", AffCode: "preserved-user", Quota: 300, UsedQuota: 20}
			refund := model.FinanceCashRefund{EventKey: "preserved-refund", State: "succeeded", AmountCNY: "2"}
			reservation := model.BillingReservation{RequestID: "preserved-request", UserID: 1, Reserved: 7, Applied: 7, State: "settled"}
			require.NoError(t, db.Create(&wallet).Error)
			require.NoError(t, db.Create(&user).Error)
			require.NoError(t, db.Create(&refund).Error)
			require.NoError(t, db.Create(&reservation).Error)
			logDB, _ := newAuditTestDatabase(t, dialect.kind, os.Getenv(dialect.env))
			require.NoError(t, logDB.AutoMigrate(&model.Log{}))
			model.LOG_DB = logDB
			log := model.Log{ChannelId: 1, RequestId: "preserved-request", PromptTokens: 12, CompletionTokens: 3, Quota: 7}
			require.NoError(t, logDB.Create(&log).Error)
			var workers sync.WaitGroup
			errors := make(chan error, 2)
			for range 2 {
				workers.Go(func() { errors <- model.DeleteArchivedConsoleMonitor(ctx, db, m.ID) })
			}
			workers.Wait()
			close(errors)
			for err := range errors {
				require.NoError(t, err, "concurrent duplicate deletes are idempotent")
			}
			require.ErrorIs(t, db.First(&model.ConsoleMonitor{}, m.ID).Error, gorm.ErrRecordNotFound)
			require.NoError(t, db.Unscoped().First(&retained, m.ID).Error)
			assert.True(t, retained.DeletedAt.Valid)
			assert.Equal(t, m.DisplayName, retained.DisplayName)
			assert.Nil(t, retained.Identity)
			var preservedChannel model.Channel
			require.NoError(t, db.First(&preservedChannel, 1).Error)
			assert.Equal(t, channel, preservedChannel)
			var preservedProbe model.ConsoleProbe
			require.NoError(t, db.First(&preservedProbe, probe.ID).Error)
			assert.Equal(t, probe, preservedProbe)
			var preservedAttempt model.ConsoleMonitorAttempt
			require.NoError(t, db.First(&preservedAttempt, attempt.ID).Error)
			assert.Equal(t, attempt, preservedAttempt)
			var preservedWallet model.FinanceWallet
			require.NoError(t, db.First(&preservedWallet, 1).Error)
			assert.Equal(t, wallet, preservedWallet)
			var preservedUser model.User
			require.NoError(t, db.First(&preservedUser, user.Id).Error)
			assert.Equal(t, user.Quota, preservedUser.Quota)
			assert.Equal(t, user.UsedQuota, preservedUser.UsedQuota)
			var preservedRefund model.FinanceCashRefund
			require.NoError(t, db.First(&preservedRefund, refund.ID).Error)
			assert.Equal(t, refund, preservedRefund)
			var preservedReservation model.BillingReservation
			require.NoError(t, db.First(&preservedReservation, reservation.ID).Error)
			assert.Equal(t, reservation, preservedReservation)
			var preservedLog model.Log
			require.NoError(t, logDB.First(&preservedLog, log.Id).Error)
			assert.Equal(t, log, preservedLog)
			// Re-adding the same target gets a new identity/ID, never old history.
			m.ID = 0
			m.Archived, m.Published, m.Enabled = false, false, false
			require.NoError(t, model.SaveConsoleMonitor(ctx, db, &m))
			assert.NotEqual(t, retained.ID, m.ID)
			var history int64
			require.NoError(t, db.Model(&model.ConsoleProbe{}).Where("monitor_id = ?", m.ID).Count(&history).Error)
			assert.Zero(t, history)
			assert.Error(t, model.SetConsoleMonitorSwitch(ctx, db, retained.ID, "archived", false))
			t.Logf("preserved deleted monitor %d; new target %d", retained.ID, m.ID)
		})
	}
}

func TestManualChannelStatusSynchronizesCacheAndRouting(t *testing.T) {
	for _, dialect := range []struct{ kind, env string }{{"sqlite", ""}, {"mysql", "TEST_MYSQL_DSN"}, {"postgres", "TEST_POSTGRES_DSN"}} {
		t.Run(dialect.kind, func(t *testing.T) {
			if dialect.env != "" && os.Getenv(dialect.env) == "" {
				t.Skip("isolated database not configured")
			}
			db := modelManagementDB(t, dialect.kind, os.Getenv(dialect.env))
			common.MemoryCacheEnabled = true
			t.Cleanup(func() { common.MemoryCacheEnabled = false; model.InitChannelCache() })
			channel := model.Channel{Id: 1, Name: "isolated-routing", Key: "fixture-only", Type: 1, Status: common.ChannelStatusEnabled, Models: "routing-model", Group: "default"}
			model.InitChannelCache()
			created := postAddChannel(t, 1, common.RoleRootUser, `{"mode":"single","channel":{"name":"isolated-routing","key":"fixture-only","type":1,"status":1,"models":"routing-model","group":"default"}}`)
			require.Equal(t, 200, created.Code)
			var createBody struct{ Success bool }
			require.NoError(t, common.Unmarshal(created.Body.Bytes(), &createBody))
			require.True(t, createBody.Success)
			selected, err := model.GetRandomSatisfiedChannel("default", "routing-model", 0, nil)
			require.NoError(t, err)
			require.NotNil(t, selected)
			assert.Equal(t, 1, selected.Id)
			for _, status := range []int{common.ChannelStatusManuallyDisabled, common.ChannelStatusEnabled} {
				requestBody, err := common.Marshal(map[string]int{"status": status})
				require.NoError(t, err)
				response := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(response)
				c.Params = gin.Params{{Key: "id", Value: "1"}}
				c.Request = httptest.NewRequest(http.MethodPut, "/channels/1/status", bytes.NewReader(requestBody))
				c.Set("role", common.RoleRootUser)
				UpdateChannelStatus(c)
				require.Equal(t, 200, response.Code)
				var body struct{ Success, Data bool }
				require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
				require.True(t, body.Success)
				require.True(t, body.Data)
				var saved model.Channel
				require.NoError(t, db.First(&saved, 1).Error)
				assert.Equal(t, status, saved.Status)
				var ability model.Ability
				require.NoError(t, db.First(&ability).Error)
				assert.Equal(t, status == common.ChannelStatusEnabled, ability.Enabled)
				selected, err = model.GetRandomSatisfiedChannel("default", "routing-model", 0, nil)
				require.NoError(t, err)
				if status == common.ChannelStatusEnabled {
					require.NotNil(t, selected)
					assert.Equal(t, 1, selected.Id)
				} else {
					assert.Nil(t, selected)
				}
				assert.Equal(t, channel.Key, saved.Key)
				assert.Equal(t, channel.Models, saved.Models)
			}
		})
	}
}
