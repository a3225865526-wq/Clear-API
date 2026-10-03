package middleware

import (
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestBillingExecutionCleanupIncludesAllWebSocketReservations(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "execution.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB; common.InitFinanceConfig() })
	t.Setenv("CLEARAPI_FINANCE_ENABLED", "true")
	t.Setenv("CLEARAPI_FINANCE_OWNER_USER_ID", "5")
	t.Setenv("CLEARAPI_DURABLE_WALLET", "true")
	t.Setenv("CLEARAPI_BILLING_RECONCILIATION_SINGLE_INSTANCE", "true")
	common.InitFinanceConfig()
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}))
	require.NoError(t, model.MigrateFinanceRequests(db))
	require.NoError(t, model.MigrateBillingReconciliation(db))
	u := model.User{Username: "execution-user", Quota: 1000, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&u).Error)
	key := model.Token{UserId: u.Id, Key: "synthetic-execution-key", RemainQuota: 1000, ExpiredTime: -1, Status: common.TokenStatusEnabled}
	require.NoError(t, db.Create(&key).Error)
	gin.SetMode(gin.TestMode)
	api := gin.New()
	api.Use(gin.Recovery(), BillingRequestExecution())
	api.POST("/test", func(c *gin.Context) {
		ids := []string{"execution-first", "execution-second"}
		for _, id := range ids {
			require.NoError(t, model.ReserveBillingRequest(id, u.Id, key.Id, 100))
		}
		c.Set(model.BillingExecutionContextKey, ids)
		rows, _, err := model.ListBillingReconciliation(db, "pending", 0, 100)
		require.NoError(t, err)
		for _, r := range rows {
			require.True(t, r.Executing)
		}
		panic("synthetic execution panic")
	})
	response := httptest.NewRecorder()
	api.ServeHTTP(response, httptest.NewRequest("POST", "/test", nil))
	require.Equal(t, 500, response.Code)
	rows, _, err := model.ListBillingReconciliation(db, "pending", 0, 100)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, r := range rows {
		require.False(t, r.Executing)
	}
}
