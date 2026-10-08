package relay

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	rc "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/routechain"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalResolutionWalletExactlyOnce(t *testing.T) {
	t.Setenv("CLEARAPI_DURABLE_WALLET", "")
	t.Setenv("CLEARAPI_BILLING_PROTECTION", "")
	t.Setenv("CLEARAPI_STRICT_PREPAID", "true")
	var driver gorm.Dialector
	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	t.Cleanup(func() { common.SetDatabaseTypes(oldMainType, oldLogType) })
	dialect, dsn := os.Getenv("TEST_BILLING_DIALECT"), os.Getenv("TEST_BILLING_DSN")
	switch dialect {
	case "", "sqlite":
		driver = sqlite.Open(filepath.Join(t.TempDir(), "refund.db"))
		common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	case "mysql":
		require.Equal(t, "true", os.Getenv("TEST_BILLING_DISPOSABLE"))
		driver = mysql.Open(dsn)
		common.SetDatabaseTypes(common.DatabaseTypeMySQL, common.DatabaseTypeMySQL)
	case "postgres":
		require.Equal(t, "true", os.Getenv("TEST_BILLING_DISPOSABLE"))
		driver = postgres.Open(dsn)
		common.SetDatabaseTypes(common.DatabaseTypePostgreSQL, common.DatabaseTypePostgreSQL)
	default:
		t.Fatal("unsupported disposable test dialect")
	}
	db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	oldDB, oldLog, oldRedis, oldBatch := model.DB, model.LOG_DB, common.RedisEnabled, common.BatchUpdateEnabled
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled, common.BatchUpdateEnabled = false, false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLog
		common.RedisEnabled, common.BatchUpdateEnabled = oldRedis, oldBatch
		sqlDB, e := db.DB()
		if e == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Log{}))
	for _, proven := range []bool{true, false} {
		name := "unproven"
		if proven {
			name = "proven"
		}
		u := model.User{Username: name, AffCode: name, Quota: 10000}
		require.NoError(t, db.Create(&u).Error)
		token := model.Token{UserId: u.Id, Key: "unused-fixture-" + name, RemainQuota: 10000, Status: common.TokenStatusEnabled, ExpiredTime: -1}
		require.NoError(t, db.Create(&token).Error)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
		c.Set("upstream_attempted", true)
		info := &rc.RelayInfo{UserId: u.Id, TokenId: token.Id, TokenKey: token.Key, RequestId: "fixture-" + name}
		info.UserSetting.BillingPreference = "wallet_only"
		require.Nil(t, service.PreConsumeBilling(c, 79, info))
		e := routechain.Evidence{RequestWritten: true, HTTPStatus: 400, UpstreamErrorCode: "model_not_found", ExecutorProvenance: proven, ExecutorZeroDispatch: proven}
		o := routechain.Classify(e, false, false)
		require.False(t, routechain.CanFailover(routechain.RequestProperties{RemainingCandidates: 2}, nil, e, o).Allowed)
		if routechain.ShouldRefundZeroWork(e, o) {
			routechain.ResetPerAttemptFlags(c)
		}
		apiErr := types.NewOpenAIError(errors.New("fixture model_not_found"), types.ErrorCodeBadResponse, 400)
		RefundFailedRequestBilling(c, info, apiErr)
		RefundFailedRequestBilling(c, info, apiErr)
		info.Billing.Refund(c)
		require.NoError(t, db.First(&u, u.Id).Error)
		require.NoError(t, db.First(&token, token.Id).Error)
		want := 9921
		if proven {
			want = 10000
		}
		require.Equal(t, want, u.Quota)
		require.Equal(t, want, token.RemainQuota)
		require.Zero(t, u.UsedQuota)
		require.Zero(t, u.RequestCount)
		var logs int64
		require.NoError(t, db.Model(&model.Log{}).Count(&logs).Error)
		require.Zero(t, logs)
	}
}
