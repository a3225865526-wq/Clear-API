package relay

import (
	"context"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCancelledDurableChargeHasPersistentReviewEvidence(t *testing.T) {
	oldDB, oldRedis, oldBatch := model.DB, common.RedisEnabled, common.BatchUpdateEnabled
	modelDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "cancel-review.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	model.DB, common.RedisEnabled, common.BatchUpdateEnabled = modelDB, false, false
	t.Cleanup(func() { model.DB = oldDB; common.RedisEnabled, common.BatchUpdateEnabled = oldRedis, oldBatch })
	t.Setenv("CLEARAPI_DURABLE_WALLET", "true")
	t.Setenv("CLEARAPI_BILLING_PROTECTION", "false")
	t.Setenv("BATCH_UPDATE_ENABLED", "false")
	require.NoError(t, modelDB.AutoMigrate(&model.User{}, &model.Token{}))
	require.NoError(t, model.MigrateFinanceRequests(modelDB))
	require.NoError(t, model.MigrateBillingReconciliation(modelDB))
	u := model.User{Username: "cancel-review-user", Quota: 1000, Status: common.UserStatusEnabled}
	require.NoError(t, modelDB.Create(&u).Error)
	key := model.Token{UserId: u.Id, Key: "synthetic-cancel-review-key", RemainQuota: 1000, ExpiredTime: -1, Status: common.TokenStatusEnabled}
	require.NoError(t, modelDB.Create(&key).Error)
	for _, withMarkerSchema := range []bool{true, false} {
		if !withMarkerSchema {
			require.NoError(t, modelDB.Migrator().DropTable(&model.BillingReviewMarker{}))
		}
		id := "cancel-review-traceable"
		if !withMarkerSchema {
			id = "cancel-review-unmigrated"
		}
		t.Cleanup(func() { model.EndBillingRequestExecution(id) })
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil).WithContext(ctx)
		c.Set("upstream_attempted", true)
		info := &relaycommon.RelayInfo{RequestId: id, UserId: u.Id, TokenId: key.Id, UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
		require.Nil(t, service.PreConsumeBilling(c, 100, info))
		RefundFailedRequestBilling(c, info, types.NewError(errors.New("synthetic cancellation"), types.ErrorCodeDoRequestFailed))
		var reservation model.BillingReservation
		require.NoError(t, modelDB.Where("request_id = ?", id).First(&reservation).Error)
		if withMarkerSchema {
			require.Equal(t, "settled", reservation.State)
			var marker model.BillingReviewMarker
			require.NoError(t, modelDB.Where("reservation_id = ?", reservation.ID).First(&marker).Error)
			require.Equal(t, "upstream_usage_unknown", marker.Reason)
		} else {
			require.Equal(t, "active", reservation.State)
		}
		require.Equal(t, 100, reservation.Applied)
	}
	var user model.User
	require.NoError(t, modelDB.First(&user, u.Id).Error)
	require.Equal(t, 800, user.Quota)
}
