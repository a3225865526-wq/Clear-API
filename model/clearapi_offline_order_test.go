package model

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestOfflineOrderDatabaseCompatibility(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(filepath.Join(t.TempDir(), "compatibility.db"))
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN is not configured")
				}
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN is not configured")
				}
				driver = postgres.Open(dsn)
			}
			db, err := gorm.Open(driver, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			previousDB, previousLogDB := DB, LOG_DB
			previousType := common.MainDatabaseType()
			DB, LOG_DB = db, db
			common.SetMainDatabaseType(common.DatabaseType(dialect))
			t.Cleanup(func() {
				DB, LOG_DB = previousDB, previousLogDB
				common.SetMainDatabaseType(previousType)
			})
			for range 2 {
				require.NoError(t, db.AutoMigrate(&User{}, &Redemption{}, &OfflineOrder{}, &OfflineOrderCode{}, &Log{}))
			}
			runID := uuid.NewString()[:8]
			user := &User{Username: "offline-matrix-" + runID, AffCode: runID, Password: "unused", Status: common.UserStatusEnabled}
			require.NoError(t, db.Create(user).Error)
			input := CreateOfflineOrderInput{
				OrderNo: "MATRIX-" + runID, MoneyCents: 1200, QuotaPerCode: 500,
				CodeCount: 1, PaidAt: common.GetTimestamp(), AdminUserID: user.Id,
			}
			order, keys, err := CreateOfflineOrder(input)
			require.NoError(t, err)
			require.Len(t, keys, 1)
			var link OfflineOrderCode
			require.NoError(t, db.Where("order_id = ?", order.Id).First(&link).Error)
			var protectedCode Redemption
			require.NoError(t, db.First(&protectedCode, link.RedemptionID).Error)
			protectedCode.Quota = 900
			require.Error(t, protectedCode.Update())
			_, _, err = CreateOfflineOrder(input)
			require.Error(t, err)
			_, err = Redeem(keys[0], user.Id)
			require.Error(t, err)
			var before User
			require.NoError(t, db.First(&before, user.Id).Error)
			require.Zero(t, before.Quota)
			credited, err := RedeemOffline(keys[0], user.Id)
			require.NoError(t, err)
			require.Equal(t, 500, credited)
			require.Error(t, VoidOfflineOrder(order.Id))
			var updated User
			require.NoError(t, db.First(&updated, user.Id).Error)
			require.Equal(t, 500, updated.Quota)
		})
	}
}

func TestOfflineOrderRedeemsOnceAndCannotVoidAfterUse(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "offline.db")), &gorm.Config{})
	require.NoError(t, err)
	DB, LOG_DB = db, db
	t.Cleanup(func() { DB, LOG_DB = previousDB, previousLogDB })
	require.NoError(t, db.AutoMigrate(&User{}, &Redemption{}, &OfflineOrder{}, &OfflineOrderCode{}, &Log{}))
	user := &User{Username: "offline-test-user", Password: "unused", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	input := CreateOfflineOrderInput{
		OrderNo: "LOCAL-1", MoneyCents: 1200, QuotaPerCode: 500,
		CodeCount: 2, PaidAt: common.GetTimestamp(), AdminUserID: user.Id,
	}
	order, keys, err := CreateOfflineOrder(input)
	require.NoError(t, err)
	require.Len(t, keys, 2)
	require.Equal(t, OfflineOrderPaid, order.Status)
	var protectedCode Redemption
	require.NoError(t, db.Where("`key` = ?", keys[0]).First(&protectedCode).Error)
	protectedCode.Quota = 900
	require.Error(t, protectedCode.Update())
	require.Error(t, protectedCode.Delete())
	_, err = BatchDeleteRedemptions([]int{protectedCode.Id})
	require.Error(t, err)

	_, err = Redeem(keys[0], user.Id)
	require.Error(t, err)
	var unchanged User
	require.NoError(t, db.First(&unchanged, user.Id).Error)
	require.Zero(t, unchanged.Quota)
	require.NoError(t, db.First(&protectedCode, protectedCode.Id).Error)
	require.Equal(t, common.RedemptionCodeStatusEnabled, protectedCode.Status)

	credited, err := RedeemOffline(keys[0], user.Id)
	require.NoError(t, err)
	require.Equal(t, 500, credited)
	_, err = DeleteInvalidRedemptions()
	require.NoError(t, err)
	require.NoError(t, db.First(&protectedCode, protectedCode.Id).Error)
	_, err = RedeemOffline(keys[0], user.Id)
	require.Error(t, err)
	secondUser := &User{Username: "offline-second-user", AffCode: "offline-second", Password: "unused", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(secondUser).Error)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, userID := range []int{user.Id, secondUser.Id} {
		wg.Go(func() {
			_, redeemErr := RedeemOffline(keys[1], userID)
			results <- redeemErr
		})
	}
	wg.Wait()
	close(results)
	successes := 0
	for redeemErr := range results {
		if redeemErr == nil {
			successes++
		}
	}
	require.Equal(t, 1, successes)
	require.Error(t, VoidOfflineOrder(order.Id))
	var updated User
	require.NoError(t, db.First(&updated, user.Id).Error)
	var secondUpdated User
	require.NoError(t, db.First(&secondUpdated, secondUser.Id).Error)
	require.Equal(t, 1000, updated.Quota+secondUpdated.Quota)

	_, _, err = CreateOfflineOrder(input)
	require.Error(t, err)
	var count int64
	require.NoError(t, db.Model(&Redemption{}).Count(&count).Error)
	require.EqualValues(t, 2, count)
}

func TestOfflineOrderVoidBlocksUnusedCodeAndNativeCodeIsNotAccepted(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "offline.db")), &gorm.Config{})
	require.NoError(t, err)
	DB, LOG_DB = db, db
	t.Cleanup(func() { DB, LOG_DB = previousDB, previousLogDB })
	require.NoError(t, db.AutoMigrate(&User{}, &Redemption{}, &OfflineOrder{}, &OfflineOrderCode{}, &Log{}))
	user := &User{Username: "offline-test-user", Password: "unused", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	order, keys, err := CreateOfflineOrder(CreateOfflineOrderInput{
		OrderNo: "LOCAL-2", MoneyCents: 100, QuotaPerCode: 100,
		CodeCount: 1, PaidAt: common.GetTimestamp(), AdminUserID: user.Id,
	})
	require.NoError(t, err)
	require.NoError(t, VoidOfflineOrder(order.Id))
	_, err = RedeemOffline(keys[0], user.Id)
	require.Error(t, err)
	native := &Redemption{Name: "native", Key: "12345678901234567890123456789012", Quota: 100, Status: common.RedemptionCodeStatusEnabled}
	require.NoError(t, db.Create(native).Error)
	_, err = RedeemOffline(native.Key, user.Id)
	require.Error(t, err)
	_, expiringKeys, err := CreateOfflineOrder(CreateOfflineOrderInput{
		OrderNo: "LOCAL-3", MoneyCents: 100, QuotaPerCode: 100,
		CodeCount: 1, PaidAt: common.GetTimestamp(), ExpiredTime: common.GetTimestamp() + 3600,
		AdminUserID: user.Id,
	})
	require.NoError(t, err)
	require.NoError(t, db.Model(&Redemption{}).Where("`key` = ?", expiringKeys[0]).
		Update("expired_time", common.GetTimestamp()-1).Error)
	_, err = RedeemOffline(expiringKeys[0], user.Id)
	require.Error(t, err)
	var updated User
	require.NoError(t, db.First(&updated, user.Id).Error)
	require.Zero(t, updated.Quota)
}
