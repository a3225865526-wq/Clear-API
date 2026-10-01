package middleware

import (
	"bytes"
	"io"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// FinanceOwnerAuth reuses dashboard credential/session validation but does not
// grant access through shared root/admin roles or write financial audit to the
// general administrator audit table. Financial writes audit in their transaction.
func FinanceOwnerAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		user, identity, useAccessToken, err := authenticateDashboardRequest(c)
		if err != nil {
			writeDashboardAuthError(c, err)
			return
		}
		if user.Status != common.UserStatusEnabled {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false, "code": "AUTH_USER_DISABLED"})
			return
		}
		if !common.IsFinanceOwner(user.Id) || user.Role < common.RoleAdminUser {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "code": "FINANCE_FORBIDDEN"})
			return
		}
		setDashboardAuthContext(c, user, identity, useAccessToken)
		c.Next()
	}
}

// A different superadministrator must not turn account administration into
// finance-owner impersonation. This covers route IDs and existing JSON-ID
// update/manage endpoints; it does not change other users' billing behavior.
func ProtectFinanceOwnerAccount() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !common.FinanceEnabled() || common.IsFinanceOwner(c.GetInt("id")) || c.Request.Method == http.MethodGet {
			c.Next()
			return
		}
		target, _ := strconv.Atoi(c.Param("id"))
		if target == 0 && (c.FullPath() == "/api/user/" && c.Request.Method == http.MethodPut || c.FullPath() == "/api/user/manage") {
			body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024))
			if err != nil {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "code": "INVALID_USER_REQUEST"})
				return
			}
			c.Request.Body = io.NopCloser(bytes.NewReader(body))
			var request struct {
				ID int `json:"id"`
			}
			if err := common.Unmarshal(body, &request); err != nil {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "code": "INVALID_USER_REQUEST"})
				return
			}
			target = request.ID
		}
		if common.IsFinanceOwner(target) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "code": "FINANCE_OWNER_PROTECTED"})
			return
		}
		c.Next()
	}
}
