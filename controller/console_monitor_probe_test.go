package controller

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	assert.Equal(t, "error", body.Data.Status)
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
	require.ErrorIs(t, adapter.Authorize(ctx, m), model.ErrMonitorIntegration)
	_, err := adapter.Probe(ctx, m, attempt.ID)
	require.ErrorIs(t, err, model.ErrMonitorIntegration)
	assert.Zero(t, calls.Load())
	m.Published = false
	require.ErrorIs(t, adapter.Authorize(ctx, m), model.ErrMonitorPaused)
	var saved model.Channel
	require.NoError(t, db.First(&saved, 1).Error)
	assert.Equal(t, channel.Status, saved.Status)
	assert.Equal(t, channel.Key, saved.Key)
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
