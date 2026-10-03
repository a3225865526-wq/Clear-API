package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupStoreControllerTest(t *testing.T) (*gin.Engine, *model.User, string) {
	t.Helper()

	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis := common.RedisEnabled

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Redemption{},
		&model.RedemptionBatch{},
		&model.VIPLevel{},
		&model.BalanceTransaction{},
		&model.AuditLog{},
	))
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		common.RedisEnabled = previousRedis
		sqlDB.Close()
	})

	previousFlag := operation_setting.StoreRechargeEnabled
	operation_setting.StoreRechargeEnabled = true
	t.Cleanup(func() { operation_setting.StoreRechargeEnabled = previousFlag })

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.RequestId())
	router.POST("/api/clearapi/store/redeem", middleware.UserAuth(), RedeemStoreCode)

	token := "store-redeem-test-token"
	user := model.User{
		Username:    "store-redeem-user",
		Password:    "store-test-password",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AffCode:     "store-redeem-user",
		AccessToken: &token,
	}
	require.NoError(t, db.Create(&user).Error)

	code := model.Redemption{
		Name:           "store-test",
		Key:            "controller-code-0000000000000000000001",
		Status:         common.RedemptionCodeStatusEnabled,
		Quota:          1000000,
		CreatedTime:    common.GetTimestamp(),
		FaceValueCents: 10000,
		Type:           model.RedemptionTypeBalance,
		Source:         model.RedemptionSourceStore,
	}
	require.NoError(t, db.Create(&code).Error)
	return router, &user, code.Key
}

func postStoreRedeem(router *gin.Engine, token string, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/clearapi/store/redeem", bytes.NewBufferString(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestRedeemStoreCodeHandler(t *testing.T) {
	router, user, key := setupStoreControllerTest(t)

	t.Run("unauthenticated rejected", func(t *testing.T) {
		response := postStoreRedeem(router, "", `{"key":"controller-code-0000000000000000000001"}`)
		assert.Equal(t, http.StatusUnauthorized, response.Code)
	})

	t.Run("redeem success", func(t *testing.T) {
		response := postStoreRedeem(router, "store-redeem-test-token", `{"key":"controller-code-0000000000000000000001"}`)
		require.Equal(t, http.StatusOK, response.Code)
		var result struct {
			Success bool `json:"success"`
			Data    struct {
				BaseQuota      int    `json:"base_quota"`
				BonusQuota     int    `json:"bonus_quota"`
				TotalQuota     int    `json:"total_quota"`
				Balance        int    `json:"balance"`
				FaceValueCents int64  `json:"face_value_cents"`
				VIPName        string `json:"vip_name"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
		assert.True(t, result.Success)
		assert.Equal(t, 1000000, result.Data.BaseQuota)
		assert.Equal(t, 1000000, result.Data.Balance)
		assert.EqualValues(t, 10000, result.Data.FaceValueCents)

		var entries []model.BalanceTransaction
		require.NoError(t, model.DB.Where("user_id = ?", user.Id).Find(&entries).Error)
		require.Len(t, entries, 1)
		assert.Equal(t, model.BalanceTxTypeRecharge, entries[0].Type)
	})

	t.Run("second redeem fails with generic message", func(t *testing.T) {
		response := postStoreRedeem(router, "store-redeem-test-token", `{"key":"controller-code-0000000000000000000001"}`)
		require.Equal(t, http.StatusOK, response.Code)
		var result struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
		assert.False(t, result.Success)
		assert.NotContains(t, result.Message, key)
	})

	t.Run("missing key rejected", func(t *testing.T) {
		response := postStoreRedeem(router, "store-redeem-test-token", `{}`)
		assert.Equal(t, http.StatusOK, response.Code)
		var result struct {
			Success bool `json:"success"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
		assert.False(t, result.Success)
	})
}

func TestRedeemStoreCodeHandlerDisabledFlag(t *testing.T) {
	router, _, _ := setupStoreControllerTest(t)
	operation_setting.StoreRechargeEnabled = false
	t.Cleanup(func() { operation_setting.StoreRechargeEnabled = true })

	response := postStoreRedeem(router, "store-redeem-test-token", `{"key":"controller-code-0000000000000000000001"}`)
	require.Equal(t, http.StatusOK, response.Code)
	var result struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "商城充值当前未开启")
}

func TestStoreAdminRoutesRequireAdmin(t *testing.T) {
	_, _, _ = setupStoreControllerTest(t)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/redemption/batches", middleware.AdminAuth(), GetAllRedemptionBatches)
	router.POST("/api/clearapi/store/redeem", middleware.UserAuth(), RedeemStoreCode)

	// 普通用户 token 调用管理员批次接口被拒。
	request := httptest.NewRequest(http.MethodGet, "/api/redemption/batches", nil)
	request.Header.Set("Authorization", "Bearer store-redeem-test-token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assert.NotEqual(t, http.StatusOK, response.Code)

	// 未登录调用商城兑换被拒。
	request = httptest.NewRequest(http.MethodPost, "/api/clearapi/store/redeem", bytes.NewBufferString(`{"key":"x"}`))
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assert.Equal(t, http.StatusUnauthorized, response.Code)
}
