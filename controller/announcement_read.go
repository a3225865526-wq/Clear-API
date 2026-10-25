package controller

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/console_setting"
	"github.com/gin-gonic/gin"
)

func GetAnnouncementReads(c *gin.Context) {
	keys := currentAnnouncementReadKeys()
	data, err := model.GetAnnouncementReads(c.Request.Context(), c.GetInt("id"), keys)
	if err != nil {
		c.JSON(500, gin.H{"success": false, "message": "Unable to load read status"})
		return
	}
	noticeKey := ""
	for _, key := range keys {
		if strings.HasPrefix(key, "notice:") {
			noticeKey = key
		}
	}
	c.JSON(200, gin.H{"success": true, "data": data, "notice_key": noticeKey, "notice_content": currentNoticeContent()})
}

func MarkAnnouncementsRead(c *gin.Context) {
	var req struct {
		Keys []string `json:"keys" binding:"required,min=1,max=101"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"success": false, "message": "Invalid announcement keys"})
		return
	}
	allowed := currentAnnouncementReadKeys()
	valid := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		valid[key] = true
	}
	seen := map[string]bool{}
	keys := []string{}
	for _, key := range req.Keys {
		if !valid[key] {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Announcement unavailable"})
			return
		}
		if !seen[key] {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	// Ownership comes only from the authenticated principal, never request JSON.
	if err := model.MarkAnnouncementsRead(c.Request.Context(), c.GetInt("id"), keys); err != nil {
		c.JSON(500, gin.H{"success": false, "message": "Unable to save read status"})
		return
	}
	c.JSON(200, gin.H{"success": true})
}

func currentNoticeContent() string {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return strings.TrimSpace(common.OptionMap["Notice"])
}

func currentAnnouncementReadKeys() []string {
	keys := []string{}
	if console_setting.GetConsoleSetting().AnnouncementsEnabled {
		for _, item := range console_setting.GetAnnouncements() {
			keys = append(keys, item["read_key"].(string))
		}
	}
	common.OptionMapRWMutex.RLock()
	notice := strings.TrimSpace(common.OptionMap["Notice"])
	common.OptionMapRWMutex.RUnlock()
	if notice != "" {
		keys = append(keys, console_setting.NoticeReadKey(notice))
	}
	return keys
}
