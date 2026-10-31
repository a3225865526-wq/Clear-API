package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSub2APIAdminOriginAndAccess(t *testing.T) {
	db := modelManagementDB(t, "sqlite", "")
	router := gin.New()
	router.GET("/admin", middleware.AdminAuth(), GetSub2APIAdminAccess)
	for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser} {
		key := "sub2api-common-pat"
		name := "sub2api-common"
		if role == common.RoleAdminUser {
			key, name = "sub2api-admin-pat", "sub2api-admin"
		}
		user := model.User{Username: name, Password: "fixture-only", Role: role, Status: common.UserStatusEnabled, Group: "default", AccessToken: &key, AuthVersion: 1, AffCode: name}
		require.NoError(t, db.Create(&user).Error)
		t.Setenv("SUB2API_ADMIN_URL", "https://admin.example.invalid")
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin?url=https://attacker.invalid", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		router.ServeHTTP(w, req)
		if role == common.RoleCommonUser {
			assert.Equal(t, http.StatusForbidden, w.Code)
			continue
		}
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "https://admin.example.invalid")
		assert.NotContains(t, w.Body.String(), "attacker.invalid")
		assert.Empty(t, w.Header().Get("Location"))
		assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	}
	for _, origin := range []string{"", "javascript:alert(1)", "http://admin.example.invalid", "https://user:password@admin.example.invalid", "https://admin.example.invalid/?token=secret", "https://admin.example.invalid/path", "https://admin.example.invalid/#secret"} {
		t.Setenv("SUB2API_ADMIN_URL", origin)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin", nil)
		GetSub2APIAdminAccess(c)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		assert.NotContains(t, w.Body.String(), "password")
	}
}
