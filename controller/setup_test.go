package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupPostSetupTest(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "setup.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Option{}, &model.Setup{}))
	oldDB := model.DB
	oldSetup := constant.Setup
	t.Cleanup(func() {
		model.DB = oldDB
		constant.Setup = oldSetup
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	model.DB = db
	constant.Setup = false
	// Runtime parity: the option map is always initialized before HTTP setup
	// can run; tests must mirror that before UpdateOption dispatches.
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}

	router := gin.New()
	router.POST("/api/setup", PostSetup)
	return router
}

func postSetupRequest(t *testing.T, router *gin.Engine, body string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	return response
}

// TestPostSetupCreatesRootViaSharedHelper verifies the HTTP setup flow keeps
// its historical behavior while routing creation through the shared
// model.CreateRootAccount helper (Amendment 4).
func TestPostSetupCreatesRootViaSharedHelper(t *testing.T) {
	router := setupPostSetupTest(t)
	response := postSetupRequest(t, router, `{
		"username": "root",
		"password": "setup-password-1",
		"confirmPassword": "setup-password-1",
		"SelfUseModeEnabled": false,
		"DemoSiteEnabled": false
	}`)
	require.Equal(t, true, response["success"])

	var user model.User
	require.NoError(t, model.DB.Where("username = ?", "root").First(&user).Error)
	require.Equal(t, common.RoleRootUser, user.Role)
	require.Equal(t, common.UserStatusEnabled, user.Status)
	require.Equal(t, "Root User", user.DisplayName)
	require.Equal(t, 100000000, user.Quota, "HTTP setup keeps its historical initial quota")
	require.True(t, bytes.HasPrefix([]byte(user.Password), []byte("$argon2id$")))

	value, found, err := model.ReadBootstrapOption(model.DB, "SelfUseModeEnabled")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "false", value)

	var setupCount int64
	require.NoError(t, model.DB.Model(&model.Setup{}).Count(&setupCount).Error)
	require.Equal(t, int64(1), setupCount)
	require.True(t, constant.Setup)
}

func TestPostSetupRejectsLongUsernameWithHistoricalMessage(t *testing.T) {
	router := setupPostSetupTest(t)
	response := postSetupRequest(t, router, `{
		"username": "thirteen-chars",
		"password": "setup-password-1",
		"confirmPassword": "setup-password-1"
	}`)
	require.Equal(t, false, response["success"])
	require.Equal(t, "用户名长度不能超过12个字符", response["message"])
	var count int64
	require.NoError(t, model.DB.Model(&model.User{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestPostSetupRejectsPasswordMismatch(t *testing.T) {
	router := setupPostSetupTest(t)
	response := postSetupRequest(t, router, `{
		"username": "root",
		"password": "setup-password-1",
		"confirmPassword": "setup-password-2"
	}`)
	require.Equal(t, false, response["success"])
	require.Equal(t, "两次输入的密码不一致", response["message"])
}

func TestPostSetupRejectsWeakPassword(t *testing.T) {
	router := setupPostSetupTest(t)
	response := postSetupRequest(t, router, `{
		"username": "root",
		"password": "short",
		"confirmPassword": "short"
	}`)
	require.Equal(t, false, response["success"])
	require.Equal(t, common.ErrAccountPasswordLength.Error(), response["message"])
}

func TestPostSetupRejectsWhenAlreadyInitialized(t *testing.T) {
	router := setupPostSetupTest(t)
	constant.Setup = true
	response := postSetupRequest(t, router, `{
		"username": "root",
		"password": "setup-password-1",
		"confirmPassword": "setup-password-1"
	}`)
	require.Equal(t, false, response["success"])
	require.Equal(t, "系统已经初始化完成", response["message"])
}
