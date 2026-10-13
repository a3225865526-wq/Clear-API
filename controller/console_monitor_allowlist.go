package controller

import (
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// Explicit fields exclude execution and cost controls from this management API.
type monitorAllowlistInput struct {
	ID              int    `json:"id"`
	TargetType      string `json:"target_type"`
	TargetRef       string `json:"target_ref"`
	ChannelID       int    `json:"channel_id"`
	Model           string `json:"model"`
	Protocol        string `json:"protocol"`
	DisplayName     string `json:"display_name"`
	Platform        string `json:"platform"`
	IntervalSeconds int64  `json:"interval_seconds"`
	TimeoutSeconds  int64  `json:"timeout_seconds"`
	StaleSeconds    int64  `json:"stale_seconds"`
}

func ListConsoleMonitorAllowlist(c *gin.Context) {
	if !model.ConsoleMonitorAllowlistReady(model.DB) {
		monitorAdminError(c, model.ErrMonitorNotReady)
		return
	}
	var monitors []model.ConsoleMonitor
	var policy model.ConsoleMonitorPolicy
	db := model.DB.WithContext(c.Request.Context())
	if err := db.Order("id").Find(&monitors).Error; err != nil {
		monitorAdminError(c, err)
		return
	}
	if err := db.First(&policy, 1).Error; err != nil {
		monitorAdminError(c, err)
		return
	}
	type entry struct {
		model.ConsoleMonitor
		Enabled          bool   `json:"enabled"`
		TargetType       string `json:"target_type"`
		MaxConcurrency   int64  `json:"max_concurrency"`
		DailyBudgetLimit int64  `json:"daily_budget_limit"` // micro USD, global only
		CreatedBy        int    `json:"created_by"`
		CreatedAt        int64  `json:"created_at"`
	}
	entries := make([]entry, 0, len(monitors))
	for _, m := range monitors {
		entries = append(entries, entry{m, m.Allowlisted, m.TargetKind, policy.MaxConcurrent, policy.DailyCostMicros, m.CreatedBy, m.CreatedAt})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"monitors": entries, "policy": policy, "real_probe_ready": false}})
}

func SaveConsoleMonitorAllowlist(c *gin.Context) {
	if !model.ConsoleMonitorAllowlistReady(model.DB) {
		monitorAdminError(c, model.ErrMonitorNotReady)
		return
	}
	var input monitorAllowlistInput
	if c.ShouldBindJSON(&input) != nil {
		monitorAdminError(c, model.ErrMonitorInvalid)
		return
	}
	var m model.ConsoleMonitor
	if c.Request.Method == http.MethodPut {
		if input.ID <= 0 {
			monitorAdminError(c, model.ErrMonitorInvalid)
			return
		}
		m.ID = input.ID
	} else if input.ID != 0 {
		monitorAdminError(c, model.ErrMonitorInvalid)
		return
	}
	m.ChannelID, m.TargetKind, m.TargetRef = input.ChannelID, input.TargetType, input.TargetRef
	m.Model, m.Protocol = input.Model, input.Protocol
	m.DisplayName, m.Platform = input.DisplayName, input.Platform
	m.IntervalSeconds, m.TimeoutSeconds, m.StaleSeconds = input.IntervalSeconds, input.TimeoutSeconds, input.StaleSeconds
	var err error
	if c.Request.Method == http.MethodPut {
		err = model.UpdateConsoleMonitorAllowlist(c.Request.Context(), model.DB, &m)
	} else {
		m.CreatedBy, m.CreatedAt = c.GetInt("id"), time.Now().Unix()
		err = model.SaveConsoleMonitor(c.Request.Context(), model.DB, &m)
	}
	if err != nil {
		monitorAdminError(c, err)
		return
	}
	recordManageAudit(c, "monitor.allowlist_configuration", map[string]any{"monitor_id": m.ID, "target_kind": m.TargetKind})
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"id": m.ID}})
}

func SwitchConsoleMonitorAllowlist(c *gin.Context) {
	if !model.ConsoleMonitorAllowlistReady(model.DB) {
		monitorAdminError(c, model.ErrMonitorNotReady)
		return
	}
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
	switch action {
	case "enabled":
		err = model.SetConsoleMonitorAllowlisted(c.Request.Context(), model.DB, id, input.Value)
	case "published":
		err = model.SetConsoleMonitorSwitch(c.Request.Context(), model.DB, id, action, input.Value)
	default:
		err = model.ErrMonitorInvalid
	}
	if err != nil {
		monitorAdminError(c, err)
		return
	}
	recordManageAudit(c, "monitor.allowlist_switch", map[string]any{"monitor_id": id, "action": action, "value": input.Value})
	c.JSON(http.StatusOK, gin.H{"success": true})
}
