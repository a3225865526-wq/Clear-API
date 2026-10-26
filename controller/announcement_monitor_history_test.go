package controller

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/console_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Released probe schema, before task/attempt attribution was added.
type releasedConsoleProbe struct {
	ID              int64 `gorm:"primaryKey"`
	MonitorID       int   `gorm:"index:idx_console_probe_time,priority:1"`
	CheckedAt       int64 `gorm:"index:idx_console_probe_time,priority:2"`
	Status          string
	LatencyMS       *int64
	EndpointMS      *int64
	ErrorCode       string
	SourceChannelID int
	SourceKind      string `gorm:"size:32"`
}

func (releasedConsoleProbe) TableName() string { return "console_probes" }

func noticeHistoryRequest(t *testing.T, r http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := common.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	out := httptest.NewRecorder()
	r.ServeHTTP(out, req)
	return out
}

func TestAnnouncementAndDailyHistoryMatrix(t *testing.T) {
	for _, engine := range []struct{ kind, env string }{{"sqlite", ""}, {"mysql", "TEST_MYSQL_DSN"}, {"postgres", "TEST_POSTGRES_DSN"}} {
		t.Run(engine.kind, func(t *testing.T) {
			if engine.env != "" && os.Getenv(engine.env) == "" {
				t.Skip("isolated database not configured")
			}
			db := modelManagementDB(t, engine.kind, os.Getenv(engine.env))
			require.NoError(t, db.AutoMigrate(&model.AnnouncementRead{}, &model.SystemTask{}, &model.SystemTaskLock{}))
			// Fresh migrations are repeated; the upgrade fixture then uses the actual released probe shape.
			require.NoError(t, model.MigrateConsoleMonitoringDB(db))
			require.NoError(t, model.MigrateConsoleMonitoringDB(db))
			require.NoError(t, db.Migrator().DropTable(&model.ConsoleProbe{}))
			require.NoError(t, db.AutoMigrate(&releasedConsoleProbe{}))
			latency := int64(12)
			oldProbe := releasedConsoleProbe{MonitorID: 1, CheckedAt: time.Now().Add(-48 * time.Hour).Unix(), Status: "normal", LatencyMS: &latency}
			require.NoError(t, db.Create(&oldProbe).Error)
			previousMonitor := model.ConsoleMonitor{ID: 99, ChannelID: 99, DisplayName: "preserved archived target", Model: "legacy-model", Archived: true, IntervalSeconds: 600, StaleSeconds: 900}
			require.NoError(t, db.Create(&previousMonitor).Error)
			require.False(t, model.ConsoleMonitoringReady(db))
			require.NoError(t, model.MigrateConsoleMonitoringDB(db))
			require.NoError(t, model.MigrateConsoleMonitoringDB(db))
			var preservedMonitor model.ConsoleMonitor
			require.NoError(t, db.First(&preservedMonitor, 99).Error)
			assert.Equal(t, previousMonitor.DisplayName, preservedMonitor.DisplayName)
			assert.True(t, preservedMonitor.Archived)
			assert.False(t, preservedMonitor.Enabled)
			assert.False(t, preservedMonitor.Published)
			var retained model.ConsoleProbe
			require.NoError(t, db.First(&retained, oldProbe.ID).Error)
			assert.Equal(t, oldProbe.CheckedAt, retained.CheckedAt)
			assert.Equal(t, "normal", retained.Status)
			require.NoError(t, db.AutoMigrate(&model.AnnouncementRead{}))

			rootToken, userAToken, userBToken := "isolated-root", "isolated-user-a", "isolated-user-b"
			users := []model.User{
				{Username: "notice-root", AffCode: "notice-root", Role: common.RoleRootUser, Status: common.UserStatusEnabled, AccessToken: &rootToken},
				{Username: "notice-a", AffCode: "notice-a", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AccessToken: &userAToken},
				{Username: "notice-b", AffCode: "notice-b", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AccessToken: &userBToken},
			}
			require.NoError(t, db.Create(&users).Error)
			r := gin.New()
			r.PUT("/api/option/", middleware.RootAuth(), UpdateOption)
			r.GET("/reads", middleware.UserAuth(), GetAnnouncementReads)
			r.POST("/reads", middleware.UserAuth(), middleware.SessionCookieOriginGuard(), MarkAnnouncementsRead)
			r.GET("/history/:id", middleware.UserAuth(), GetConsoleMonitorHistory)
			r.GET("/monitoring", middleware.UserAuth(), GetConsoleMonitoring)
			r.GET("/status", GetStatus)
			records := []map[string]any{
				{"id": 1, "content": "legacy", "publishDate": "2020-01-01T00:00:00Z", "type": "default"},
				{"id": 2, "content": "notice", "publishDate": "2026-10-20T00:00:00Z", "type": "default", "status": "published", "display": "notice"},
				{"id": 3, "content": "timeline", "publishDate": "2026-10-21T00:00:00Z", "type": "default", "status": "published", "display": "timeline"},
				{"id": 4, "content": "both", "publishDate": "2026-10-22T00:00:00Z", "type": "default", "status": "published", "display": "both"},
				{"id": 5, "content": "draft", "publishDate": "2026-10-23T00:00:00Z", "status": "draft", "display": "both"},
			}
			raw, err := common.Marshal(records)
			require.NoError(t, err)
			response := noticeHistoryRequest(t, r, "PUT", "/api/option/", rootToken, gin.H{"key": "console_setting.announcements", "value": string(raw)})
			require.Equal(t, 200, response.Code, response.Body.String())
			require.Contains(t, response.Body.String(), `"success":true`)
			require.NoError(t, model.UpdateOption("console_setting.announcements_enabled", "true"))
			assert.Len(t, console_setting.GetAnnouncementsFor("notice"), 2)
			assert.Len(t, console_setting.GetAnnouncementsFor("timeline"), 3)
			notice := console_setting.GetAnnouncementsFor("notice")
			timeline := console_setting.GetAnnouncementsFor("timeline")
			assert.Equal(t, notice[0]["id"], timeline[0]["id"])
			key := notice[0]["read_key"].(string)
			for range 2 {
				response = noticeHistoryRequest(t, r, "POST", "/reads", userAToken, gin.H{"keys": []string{key}, "user_id": users[2].Id})
				require.Equal(t, 200, response.Code, response.Body.String())
			}
			var count int64
			require.NoError(t, db.Model(&model.AnnouncementRead{}).Where("user_id = ?", users[1].Id).Count(&count).Error)
			assert.EqualValues(t, 1, count)
			require.NoError(t, db.Model(&model.AnnouncementRead{}).Where("user_id = ?", users[2].Id).Count(&count).Error)
			assert.Zero(t, count)
			assert.Contains(t, noticeHistoryRequest(t, r, "GET", "/reads", userAToken, nil).Body.String(), key)
			assert.NotContains(t, noticeHistoryRequest(t, r, "GET", "/reads", userBToken, nil).Body.String(), key)
			assert.Equal(t, 401, noticeHistoryRequest(t, r, "POST", "/reads", "", gin.H{"keys": []string{key}}).Code)
			assert.Equal(t, 403, noticeHistoryRequest(t, r, "PUT", "/api/option/", userAToken, gin.H{"key": "console_setting.announcements", "value": "[]"}).Code)
			assert.Equal(t, 400, noticeHistoryRequest(t, r, "POST", "/reads", userAToken, gin.H{"keys": []string{"announcement:unknown"}}).Code)
			records[3]["display"] = "notice"
			raw, err = common.Marshal(records)
			require.NoError(t, err)
			response = noticeHistoryRequest(t, r, "PUT", "/api/option/", rootToken, gin.H{"key": "console_setting.announcements", "value": string(raw)})
			require.Contains(t, response.Body.String(), `"success":true`)
			assert.Len(t, console_setting.GetAnnouncementsFor("timeline"), 2)
			assert.Equal(t, key, console_setting.GetAnnouncementsFor("notice")[0]["read_key"])
			var stored model.Option
			require.NoError(t, db.Where(map[string]any{"key": "console_setting.announcements"}).First(&stored).Error)
			assert.Equal(t, string(raw), stored.Value)
			require.NoError(t, db.Callback().Create().Before("gorm:create").Register("read_failure", func(tx *gorm.DB) {
				if tx.Statement.Table == "announcement_reads" {
					tx.AddError(errors.New("isolated read write failure"))
				}
			}))
			assert.Equal(t, 500, noticeHistoryRequest(t, r, "POST", "/reads", userBToken, gin.H{"keys": []string{key}}).Code)
			require.NoError(t, db.Callback().Create().Remove("read_failure"))
			assert.NotContains(t, noticeHistoryRequest(t, r, "GET", "/reads", userBToken, nil).Body.String(), key)

			var calls atomic.Int64
			fail := atomic.Bool{}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				calls.Add(1)
				assert.Equal(t, "/v1/responses", req.URL.Path)
				if fail.Load() {
					w.WriteHeader(503)
					return
				}
				_, _ = w.Write([]byte(`{"id":"resp-isolated-daily","status":"completed","model":"gpt-6-luna","usage":{"input_tokens":4,"output_tokens":2}}`))
			}))
			defer upstream.Close()
			channel := model.Channel{Id: 1, Name: "daily-isolated", Key: "synthetic-only", Status: common.ChannelStatusEnabled, Models: consoleProbeModel, BaseURL: &upstream.URL}
			require.NoError(t, db.Create(&channel).Error)
			m := model.ConsoleMonitor{ID: 1, ChannelID: 1, DisplayName: "daily-isolated", TargetKind: "channel", TargetRef: "1", Model: consoleProbeModel, Protocol: consoleProbeProtocol, Published: true, Enabled: true, Allowlisted: true, IntervalSeconds: 60, TimeoutSeconds: 2, StaleSeconds: 900, DailyRequests: 5, RequestCostMicros: 100}
			require.NoError(t, db.Create(&m).Error)
			policy := model.ConsoleMonitorPolicy{ID: 1, Enabled: true, MaxConcurrent: 1, DailyRequests: 5, DailyCostMicros: 500, RetentionDays: 90}
			require.NoError(t, model.SaveConsoleMonitorPolicy(context.Background(), db, policy))
			contract := service.ConsoleProbeCostContract{Source: "synthetic local HTTP fixture; no supplier fees", ValidFrom: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour), InputTokenLimit: 64, InputMicros: 1, OutputMicros: 2, FixedFeeMicros: 4, HardCeilingMicros: 100}
			require.NoError(t, service.SaveConsoleProbeCostContract(context.Background(), db, m, contract))
			handler := consoleMonitorHandler{}
			assert.True(t, handler.Enabled())
			assert.Equal(t, 24*time.Hour, handler.Interval())
			// Real task row and lease, actual production handler, local HTTP, durable result.
			task, err := model.CreateSystemTask(model.SystemTaskTypeConsoleMonitor, nil, nil)
			require.NoError(t, err)
			claimed, ok, err := model.ClaimSystemTask(task.ID, task.Type, "isolated-daily", time.Now().Add(time.Minute).Unix())
			require.NoError(t, err)
			require.True(t, ok)
			handler.Run(context.Background(), claimed, "isolated-daily")
			finished, err := model.GetSystemTaskByTaskID(task.TaskID)
			require.NoError(t, err)
			assert.Equal(t, model.SystemTaskStatusSucceeded, finished.Status)
			assert.EqualValues(t, 1, calls.Load())
			var real model.ConsoleProbe
			require.NoError(t, db.Where("task_id = ?", task.TaskID).First(&real).Error)
			assert.Equal(t, "normal", real.Status)
			assert.Positive(t, real.AttemptID)
			assert.NotNil(t, real.LatencyMS)
			// Budget refusal dispatches nothing and remains visible with no paid attempt.
			executor := service.ConsoleProbeExecutor{DB: db, TaskID: "isolated-budget", Adapters: map[string]service.ConsoleProbeAdapter{"channel": channelConsoleProbeAdapter{}}}
			result, err := executor.RunWithResult(context.Background(), m.ID)
			require.ErrorIs(t, err, model.ErrMonitorBudget)
			assert.Equal(t, "blocked", result.Status)
			assert.Zero(t, result.AttemptID)
			assert.EqualValues(t, 1, calls.Load())
			policy.Enabled = false
			require.NoError(t, model.SaveConsoleMonitorPolicy(context.Background(), db, policy))
			executor.TaskID = "isolated-paused"
			summary, err := executor.RunDailyScheduled(context.Background())
			require.NoError(t, err)
			assert.Equal(t, 1, summary.Skipped)
			assert.EqualValues(t, 1, calls.Load())
			policy.Enabled = true
			require.NoError(t, model.SaveConsoleMonitorPolicy(context.Background(), db, policy))
			// Age only this isolated attempt to admit an independent failed transport.
			require.NoError(t, db.Model(&model.ConsoleMonitorAttempt{}).Where("id = ?", real.AttemptID).Update("started_at", time.Now().Add(-time.Hour).Unix()).Error)
			fail.Store(true)
			executor.TaskID = "isolated-transport-failure"
			result, err = executor.RunWithResult(context.Background(), m.ID)
			require.ErrorIs(t, err, service.ErrMonitorLeaseUnresolved)
			assert.Equal(t, "unknown", result.Status)
			assert.EqualValues(t, 2, calls.Load())
			response = noticeHistoryRequest(t, r, "GET", "/history/1?page=1&page_size=2", userAToken, nil)
			require.Equal(t, 200, response.Code, response.Body.String())
			var history struct {
				Success bool
				Data    model.ConsoleMonitorHistory
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &history))
			assert.True(t, history.Success)
			require.Len(t, history.Data.Items, 2)
			assert.EqualValues(t, 5, history.Data.Total)
			assert.Greater(t, history.Data.Items[0].ID, history.Data.Items[1].ID)
			assert.Equal(t, "Asia/Shanghai", history.Data.Timezone)
			assert.NotContains(t, response.Body.String(), "synthetic-only")
			assert.NotContains(t, response.Body.String(), upstream.URL)
			assert.Equal(t, 400, noticeHistoryRequest(t, r, "GET", "/history/1?page=0", userAToken, nil).Code)
			assert.Equal(t, 400, noticeHistoryRequest(t, r, "GET", "/history/1?from=not-a-date", userAToken, nil).Code)
			assert.Equal(t, 401, noticeHistoryRequest(t, r, "GET", "/history/1", "", nil).Code)
			var saved model.Channel
			require.NoError(t, db.First(&saved, 1).Error)
			assert.Equal(t, channel.Status, saved.Status)
			// Calendar filtering at the UTC+8 boundary, independent of the engine timezone.
			for _, stamp := range []string{"2026-10-10T15:59:59Z", "2026-10-10T16:00:00Z"} {
				at, err := time.Parse(time.RFC3339, stamp)
				require.NoError(t, err)
				require.NoError(t, db.Create(&model.ConsoleProbe{MonitorID: 1, CheckedAt: at.Unix(), Status: "skipped", ErrorCode: "MONITOR_PAUSED"}).Error)
			}
			response = noticeHistoryRequest(t, r, "GET", "/history/1?from=2026-10-11&to=2026-10-11", userAToken, nil)
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &history))
			assert.EqualValues(t, 1, history.Data.Total)
			// Reopen the database connection and repeat additive migrations: historical results/reads survive.
			original, err := db.DB()
			require.NoError(t, err)
			require.NoError(t, original.Close())
			t.Setenv("SQL_DSN", os.Getenv("SQL_DSN"))
			require.NoError(t, model.InitDB())
			db = model.DB
			model.LOG_DB = db
			reopened, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = reopened.Close() })
			require.NoError(t, db.AutoMigrate(&model.AnnouncementRead{}))
			require.NoError(t, model.MigrateConsoleMonitoringDB(db))
			assert.Contains(t, noticeHistoryRequest(t, r, "GET", "/reads", userAToken, nil).Body.String(), key)
			response = noticeHistoryRequest(t, r, "GET", "/history/1?page=2&page_size=2", userAToken, nil)
			require.Equal(t, 200, response.Code, response.Body.String())
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &history))
			assert.EqualValues(t, 7, history.Data.Total)
			require.Len(t, history.Data.Items, 2)
			assert.Equal(t, "blocked", history.Data.Items[0].Status)
			require.NoError(t, db.Model(&m).Update("published", false).Error)
			assert.Equal(t, 404, noticeHistoryRequest(t, r, "GET", "/history/1", userAToken, nil).Code)
			t.Logf("%s fresh + released upgrade + restart, announcements/read isolation, daily HTTP/task/history: PASS", engine.kind)
		})
	}
}
