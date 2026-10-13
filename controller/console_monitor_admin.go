package controller

import (
	"errors"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// RootAuth supplies server-verified identity and the existing audit fallback.
// Reject simple request content types independently of cookie policy settings.
func ConsoleMonitorWriteGuard(c *gin.Context) {
	if c.Request.Method == http.MethodGet {
		c.Next()
		return
	}
	contentType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || contentType != "application/json" || c.GetHeader("X-ClearAPI-Monitor") != "1" {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "code": "MONITOR_REQUEST_FORBIDDEN", "message": "MONITOR_REQUEST_FORBIDDEN"})
		return
	}
	c.Next()
}

func monitorAdminError(c *gin.Context, err error) {
	status, code := http.StatusInternalServerError, "MONITOR_INTERNAL"
	switch {
	case errors.Is(err, model.ErrMonitorInvalid):
		status, code = 400, "MONITOR_INVALID"
	case errors.Is(err, model.ErrMonitorDenied):
		status, code = 403, "MONITOR_TARGET_DENIED"
	case errors.Is(err, model.ErrMonitorNotReady):
		status, code = 409, "MONITOR_NOT_INITIALIZED"
	case errors.Is(err, model.ErrMonitorBudget):
		status, code = 409, "MONITOR_BUDGET_OR_CONCURRENCY"
	case errors.Is(err, model.ErrMonitorPaused):
		status, code = 409, "MONITOR_PAUSED"
	case errors.Is(err, model.ErrMonitorIntegration):
		status, code = 501, "MONITOR_INTEGRATION_PENDING"
	case errors.Is(err, model.ErrMonitorLease):
		status, code = 409, "MONITOR_LEASE_UNRESOLVED"
	}
	c.JSON(status, gin.H{"success": false, "code": code, "message": code})
}

// Discovery never serializes Channel, credentials, endpoints or pool identities.
func DiscoverConsoleMonitorTargets(c *gin.Context) {
	var channels []struct {
		ID        int     `json:"id"`
		Name      string  `json:"name"`
		Status    int     `json:"status"`
		Models    string  `json:"models"`
		TestModel *string `json:"test_model"`
	}
	if err := model.DB.WithContext(c.Request.Context()).Model(&model.Channel{}).Select("id,name,status,models,test_model").Order("id").Scan(&channels).Error; err != nil {
		monitorAdminError(c, err)
		return
	}
	var groups []struct {
		Group string `json:"group"`
		Model string `json:"model"`
	}
	if err := model.DB.WithContext(c.Request.Context()).Model(&model.Ability{}).Where("enabled = ?", true).Distinct("group", "model").Find(&groups).Error; err != nil {
		monitorAdminError(c, err)
		return
	}
	c.JSON(200, gin.H{"success": true, "data": gin.H{"channels": channels, "groups": groups, "account_pool": "integration_pending", "real_probe_ready": false}})
}

func ListConsoleMonitorAdmin(c *gin.Context) {
	if !model.ConsoleMonitoringReady(model.DB) {
		monitorAdminError(c, model.ErrMonitorNotReady)
		return
	}
	var monitors []model.ConsoleMonitor
	var policy model.ConsoleMonitorPolicy
	if err := model.DB.WithContext(c.Request.Context()).Order("id").Find(&monitors).Error; err != nil {
		monitorAdminError(c, err)
		return
	}
	if err := model.DB.WithContext(c.Request.Context()).First(&policy, 1).Error; err != nil {
		monitorAdminError(c, err)
		return
	}
	c.JSON(200, gin.H{"success": true, "data": gin.H{"monitors": monitors, "policy": policy, "real_probe_ready": false}})
}

func SaveConsoleMonitorAdmin(c *gin.Context) {
	var m model.ConsoleMonitor
	if err := c.ShouldBindJSON(&m); err != nil {
		monitorAdminError(c, model.ErrMonitorInvalid)
		return
	}
	var err error
	if c.Request.Method == http.MethodPut {
		err = model.UpdateConsoleMonitor(c.Request.Context(), model.DB, &m)
	} else {
		err = model.SaveConsoleMonitor(c.Request.Context(), model.DB, &m)
	}
	if err != nil {
		monitorAdminError(c, err)
		return
	}
	recordManageAudit(c, "monitor.configuration", map[string]any{"monitor_id": m.ID, "target_kind": m.TargetKind})
	c.JSON(200, gin.H{"success": true, "data": gin.H{"id": m.ID}})
}

func SwitchConsoleMonitorAdmin(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	var input struct {
		Value   bool `json:"value"`
		Confirm bool `json:"confirm"`
	}
	if err != nil || id <= 0 || c.ShouldBindJSON(&input) != nil || !input.Confirm {
		monitorAdminError(c, model.ErrMonitorInvalid)
		return
	}
	action := c.Param("action")
	// No production transport is registered yet; do not accept a misleading
	// enable request that could start executing when an adapter is later added.
	if action == "enabled" && input.Value {
		monitorAdminError(c, model.ErrMonitorIntegration)
		return
	}
	if err := model.SetConsoleMonitorSwitch(c.Request.Context(), model.DB, id, action, input.Value); err != nil {
		monitorAdminError(c, err)
		return
	}
	recordManageAudit(c, "monitor.switch", map[string]any{"monitor_id": id, "action": action, "value": input.Value})
	c.JSON(200, gin.H{"success": true})
}

func SaveConsoleMonitorPolicyAdmin(c *gin.Context) {
	var input struct {
		Policy  model.ConsoleMonitorPolicy `json:"policy"`
		Confirm bool                       `json:"confirm"`
	}
	if c.ShouldBindJSON(&input) != nil || !input.Confirm {
		monitorAdminError(c, model.ErrMonitorInvalid)
		return
	}
	if input.Policy.Enabled {
		monitorAdminError(c, model.ErrMonitorIntegration)
		return
	}
	if err := model.SaveConsoleMonitorPolicy(c.Request.Context(), model.DB, input.Policy); err != nil {
		monitorAdminError(c, err)
		return
	}
	recordManageAudit(c, "monitor.policy", map[string]any{"enabled": input.Policy.Enabled, "daily_requests": input.Policy.DailyRequests, "daily_cost_micros": input.Policy.DailyCostMicros})
	c.JSON(200, gin.H{"success": true})
}

func TriggerConsoleMonitorAdmin(c *gin.Context) { monitorAdminError(c, model.ErrMonitorIntegration) }

func ConsoleMonitorHistoryAdmin(c *gin.Context) {
	if !model.ConsoleMonitoringReady(model.DB) {
		monitorAdminError(c, model.ErrMonitorNotReady)
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		monitorAdminError(c, model.ErrMonitorInvalid)
		return
	}
	var probes []struct {
		model.ConsoleProbe
		SourceChannelID int    `json:"source_channel_id"`
		SourceKind      string `json:"source_kind"`
	}
	if err := model.DB.WithContext(c.Request.Context()).Model(&model.ConsoleProbe{}).Where("monitor_id = ?", id).Order("checked_at DESC,id DESC").Limit(100).Scan(&probes).Error; err != nil {
		monitorAdminError(c, err)
		return
	}
	for i := range probes {
		probes[i].SafeResult()
	}
	c.JSON(200, gin.H{"success": true, "data": probes})
}

// RecoverConsoleMonitorAttempt reopens only a stale reconciliation claim. It
// cannot finish an attempt or release capacity; an operator must still obtain
// an authoritative stopped confirmation through the pool boundary.
func RecoverConsoleMonitorAttempt(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	var input struct {
		Reason  string `json:"reason"`
		Confirm bool   `json:"confirm"`
	}
	if err != nil || id <= 0 || c.ShouldBindJSON(&input) != nil || !input.Confirm || strings.TrimSpace(input.Reason) == "" {
		monitorAdminError(c, model.ErrMonitorInvalid)
		return
	}
	if err := model.RecoverConsoleMonitorAttempt(c.Request.Context(), model.DB, id, input.Reason, time.Now()); err != nil {
		monitorAdminError(c, err)
		return
	}
	recordManageAudit(c, "monitor.attempt_recover", map[string]any{"attempt_id": id, "reason_provided": true})
	c.JSON(http.StatusOK, gin.H{"success": true})
}
