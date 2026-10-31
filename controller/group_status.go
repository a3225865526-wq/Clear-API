package controller

import (
	"net/http"
	"net/url"
	"os"
	"sort"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

func GetGroupStatuses(c *gin.Context) {
	disabled, err := model.GetDisabledGroups()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	type groupStatus struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	rows := make([]groupStatus, 0)
	for name := range ratio_setting.GetGroupRatioCopy() {
		rows = append(rows, groupStatus{Name: name, Enabled: !disabled[name]})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	c.Header("Cache-Control", "no-store")
	common.ApiSuccess(c, rows)
}

func UpdateGroupStatus(c *gin.Context) {
	var req struct {
		Name    string `json:"name"`
		Enabled *bool  `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil || req.Name == "" || !ratio_setting.ContainsGroupRatio(req.Name) {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid group status request"})
		return
	}
	if err := model.SetGroupEnabled(req.Name, *req.Enabled); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "group.status", map[string]any{"group": req.Name, "enabled": *req.Enabled})
	common.ApiSuccess(c, nil)
}

// Configuration is supplied by the operator; callers cannot supply a target.
// Return data to the authenticated UI instead of accepting a redirect URL.
func GetSub2APIAdminAccess(c *gin.Context) {
	u, err := url.Parse(os.Getenv("SUB2API_ADMIN_URL"))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "Sub2API admin origin is not configured"})
		return
	}
	c.Header("Cache-Control", "no-store")
	common.ApiSuccess(c, gin.H{"url": u.String()})
}

func GetOwnGroupAvailability(c *gin.Context) {
	group, err := model.GetUserGroup(c.GetInt("id"), false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	disabled, err := model.GetDisabledGroups()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"enabled": !disabled[group]})
}
