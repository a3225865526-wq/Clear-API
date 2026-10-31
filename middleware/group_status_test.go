package middleware

import (
	"github.com/QuantumNous/new-api/constant"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestGroupStatusAdmissionDatabase(t *testing.T) {
	var dialector gorm.Dialector
	switch os.Getenv("TEST_GROUP_DIALECT") {
	case "mysql":
		dialector = mysql.Open(os.Getenv("TEST_GROUP_DSN"))
	case "postgres":
		dialector = postgres.Open(os.Getenv("TEST_GROUP_DSN"))
	default:
		dialector = sqlite.Open(filepath.Join(t.TempDir(), "groups.db"))
	}
	db, err := gorm.Open(dialector, &gorm.Config{})
	require.NoError(t, err)
	oldDB := model.DB
	model.DB = db
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { model.DB = oldDB; _ = sqlDB.Close() })
	// Existing schema, including an unrelated option, survives repeated startup.
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	require.NoError(t, db.Save(&model.Option{Key: "group-status-history-fixture", Value: "unchanged"}).Error)
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	require.NoError(t, model.CheckGroupsEnabled(db, "legacy-group", "new-group"))
	require.NoError(t, model.SetGroupEnabled("legacy-group", false))
	assert.ErrorIs(t, model.CheckGroupsEnabled(db, "legacy-group"), model.ErrGroupDisabled)
	require.NoError(t, model.CheckGroupsEnabled(db, "new-group"))
	disabled, err := model.GetDisabledGroups()
	require.NoError(t, err)
	assert.True(t, disabled["legacy-group"])

	gin.SetMode(gin.TestMode)
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages", "/v1beta/models/gemini:generateContent", "/v1/images/generations", "/mj/submit/imagine", "/pg/chat/completions"} {
		t.Run(path, func(t *testing.T) {
			called := false
			engine := gin.New()
			engine.POST(path, func(c *gin.Context) {
				if rejectDisabledGroups(c, "legacy-group") {
					return
				}
				c.Next()
			}, func(c *gin.Context) { called = true; c.Status(http.StatusOK) })
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
			assert.Equal(t, http.StatusForbidden, w.Code)
			assert.Contains(t, w.Body.String(), "GROUP_DISABLED")
			assert.False(t, called, "rejected admission must not reach billing or upstream handlers")
		})
	}
	for _, names := range [][2]string{{"CASE-GROUP", "case-group"}, {"café-group", "cafe-group"}} {
		require.NoError(t, model.SetGroupEnabled(names[0], false))
		require.NoError(t, model.SetGroupEnabled(names[1], true))
		assert.ErrorIs(t, model.CheckGroupsEnabled(db, names[0]), model.ErrGroupDisabled, "distinct group identifiers must retain independent state: %q vs %q", names[0], names[1])
		require.NoError(t, model.CheckGroupsEnabled(db, names[1]), "distinct group identifiers must not collide under database collation")
		disabled, err := model.GetDisabledGroups()
		require.NoError(t, err)
		assert.True(t, disabled[names[0]])
		assert.False(t, disabled[names[1]])
	}
	// Re-enabling preserves the original identifier, without deleting its state.
	require.NoError(t, model.SetGroupEnabled("legacy-group", true))
	require.NoError(t, model.CheckGroupsEnabled(db, "legacy-group"))
	var history model.Option
	require.NoError(t, db.Where(map[string]any{"key": "group-status-history-fixture"}).First(&history).Error)
	assert.Equal(t, "unchanged", history.Value)
	// Fail closed on storage failure; never infer availability from a stale cache.
	require.NoError(t, sqlDB.Close())
	assert.Error(t, model.CheckGroupsEnabled(db, "legacy-group"))
}

func TestDisabledGroupRejectsExistingKeysBeforeDownstream(t *testing.T) {
	setupDashboardAuthMiddlewareTest(t)
	model.InitColumnQuotingForTest()
	require.NoError(t, model.DB.AutoMigrate(&model.Option{}, &model.Token{}))
	user := createMiddlewarePATUser(t, "disabled-group-user", "unused-dashboard-pat")
	user.Group = "vip"
	user.Quota = 12345
	require.NoError(t, model.DB.Save(user).Error)
	token := model.Token{UserId: user.Id, Key: "existingcommercialkey", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 2345}
	require.NoError(t, token.Insert())
	ratios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":1,"default":1}`))
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(ratios)) })
	for _, disabled := range []bool{false, true, false} {
		require.NoError(t, model.SetGroupEnabled("vip", !disabled))
		for _, route := range []struct{ path, header, credential string }{
			{"/v1/chat/completions", "Authorization", "Bearer " + token.Key},
			{"/v1/responses", "Authorization", "Bearer " + token.Key},
			{"/v1/messages", "x-api-key", token.Key},
			{"/v1beta/models/gemini:generateContent", "x-goog-api-key", token.Key},
			{"/mj/submit/imagine", "mj-api-secret", token.Key},
		} {
			called := false
			engine := gin.New()
			engine.POST(route.path, TokenAuth(), func(c *gin.Context) { called = true; c.Status(http.StatusNoContent) })
			req := httptest.NewRequest(http.MethodPost, route.path, nil)
			req.Header.Set(route.header, route.credential)
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, req)
			if disabled {
				assert.Equal(t, http.StatusForbidden, w.Code, route.path)
				assert.Contains(t, w.Body.String(), "GROUP_DISABLED")
				assert.False(t, called)
			} else {
				assert.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
				assert.True(t, called)
			}
		}
	}
	// An explicit enabled group is not an automatic fallback from a disabled one.
	require.NoError(t, model.SetGroupEnabled("vip", false))
	token.Group = "default"
	require.NoError(t, model.DB.Save(&token).Error)
	engine := gin.New()
	engine.POST("/v1/chat/completions", TokenAuth(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer "+token.Key)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	var afterUser model.User
	var afterToken model.Token
	require.NoError(t, model.DB.First(&afterUser, user.Id).Error)
	require.NoError(t, model.DB.First(&afterToken, token.Id).Error)
	assert.Equal(t, "vip", afterUser.Group)
	assert.Equal(t, 12345, afterUser.Quota)
	assert.Equal(t, 2345, afterToken.RemainQuota)
	assert.Zero(t, afterToken.UsedQuota)
}

func TestAutoAdmissionRechecksUnfilteredSnapshotAfterDisable(t *testing.T) {
	setupDashboardAuthMiddlewareTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Option{}))
	require.NoError(t, model.SetGroupEnabled("vip", false))
	engine := gin.New()
	engine.POST("/v1/chat/completions", func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyUsingGroup, "auto")
		common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
		common.SetContextKey(c, constant.ContextKeyTokenGroup, "auto")
		common.SetContextKey(c, constant.ContextKeyTokenAutoGroups, []string{"vip", "default"})
	}, Distribute())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"group-guard-model","messages":[{"role":"user","content":"fixture"}]}`))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "GROUP_DISABLED")
}
