package middleware

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// PoolOwnerAuth guards the site-owner upstream account pool. The module can be
// switched off with CLEARAPI_POOL_ENABLED (404 for everyone), but the manager
// identity itself is fixed policy: authenticated user ID 1 holding the project
// root role. No option, Casbin grant or alternate owner ID can widen it, and a
// plain admin (role 10) is rejected exactly like any other account.
func PoolOwnerAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !common.PoolEnabled() {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"success": false, "code": "NOT_FOUND"})
			return
		}
		user, identity, useAccessToken, err := authenticateDashboardRequest(c)
		if err != nil {
			writeDashboardAuthError(c, err)
			return
		}
		if user.Status != common.UserStatusEnabled {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false, "code": "AUTH_USER_DISABLED"})
			return
		}
		if user.Id != common.PoolOwnerUserID || user.Role < common.RoleRootUser {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "code": "POOL_FORBIDDEN"})
			return
		}
		setDashboardAuthContext(c, user, identity, useAccessToken)
		// Same fallback audit as AdminAuth/RootAuth write paths; pool handlers
		// keep authorization codes and credentials out of bodies they log and
		// the audit entry only stores method/route/route-params metadata.
		auditWriter := beginAdminAudit(c)
		c.Next()
		finishAdminAudit(c, auditWriter)
	}
}
