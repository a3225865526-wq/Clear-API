package middleware

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func rejectDisabledGroups(c *gin.Context, groups ...string) bool {
	err := model.CheckGroupsEnabled(model.DB, groups...)
	if err == nil {
		return false
	}
	status, code, message := http.StatusServiceUnavailable, "GROUP_STATUS_UNAVAILABLE", "Group availability could not be verified"
	if errors.Is(err, model.ErrGroupDisabled) {
		status, code, message = http.StatusForbidden, "GROUP_DISABLED", model.ErrGroupDisabled.Error()
	}
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"message": message, "type": "permission_error", "code": code}})
	return true
}
