package model

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestProductionRepairsDatabase(t *testing.T) {
	t.Cleanup(common.InitFinanceConfig)
	dialect, dsn := os.Getenv("TEST_BILLING_DIALECT"), os.Getenv("TEST_BILLING_DSN")
	var driver gorm.Dialector
	switch dialect {
	case "", "sqlite":
		driver = sqlite.Open(filepath.Join(t.TempDir(), "production.db"))
	case "mysql":
		require.NotEmpty(t, dsn)
		driver = mysql.Open(dsn)
	case "postgres":
		require.NotEmpty(t, dsn)
		driver = postgres.Open(dsn)
	default:
		t.Fatal("unsupported isolated database")
	}
	db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	oldDB, oldLogs, oldType := DB, LOG_DB, common.MainDatabaseType()
	DB, LOG_DB = db, db
	switch dialect {
	case "mysql":
		common.SetMainDatabaseType(common.DatabaseTypeMySQL)
	case "postgres":
		common.SetMainDatabaseType(common.DatabaseTypePostgreSQL)
	default:
		common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	}
	initCol()
	t.Cleanup(func() { DB, LOG_DB = oldDB, oldLogs; common.SetMainDatabaseType(oldType); initCol() })
	var version string
	query := "select version()"
	if dialect == "" || dialect == "sqlite" {
		query = "select sqlite_version()"
	}
	require.NoError(t, db.Raw(query).Scan(&version).Error)
	t.Logf("database %s %s", dialect, version)
	require.NoError(t, db.AutoMigrate(&User{}, &Token{}, &Task{}, &Channel{}, &Midjourney{}, &Log{}))
	user := User{Username: "repair-synthetic", AffCode: "repair-synthetic-aff", Quota: 100, UsedQuota: 50, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	token := Token{UserId: user.Id, Key: "synthetic-production-key", RemainQuota: 100, UsedQuota: 50, Status: common.TokenStatusEnabled, ExpiredTime: -1}
	require.NoError(t, db.Create(&token).Error)
	channel := Channel{UsedQuota: 50}
	require.NoError(t, db.Create(&channel).Error)
	task := Task{TaskID: "synthetic-production-task", UserId: user.Id, ChannelId: channel.Id, Quota: 50, Status: TaskStatusFailure, PrivateData: TaskPrivateData{TokenId: token.Id, BillingSource: "wallet"}}
	require.NoError(t, db.Create(&task).Error)
	stale := task
	changed, err := AdjustLegacyTaskAccounting(&task, 0)
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = AdjustLegacyTaskAccounting(&stale, 0)
	require.NoError(t, err)
	require.False(t, changed)
	require.NoError(t, db.First(&user, user.Id).Error)
	require.Equal(t, 150, user.Quota)
	require.Zero(t, user.UsedQuota)
	require.NoError(t, db.First(&token, token.Id).Error)
	require.Equal(t, 150, token.RemainQuota)
	require.Zero(t, token.UsedQuota)
	// Wallet insufficient or token insufficient: no marker/stat/funds mutation.
	task.Quota = 10
	require.NoError(t, db.Model(&task).Update("quota", 10).Error)
	_, err = AdjustLegacyTaskAccounting(&task, 1000)
	require.Error(t, err)
	require.NoError(t, db.First(&user, user.Id).Error)
	require.Equal(t, 150, user.Quota)
	require.NoError(t, db.Model(&token).Update("remain_quota", 1).Error)
	_, err = AdjustLegacyTaskAccounting(&task, 30)
	require.Error(t, err)
	require.NoError(t, db.First(&user, user.Id).Error)
	require.Equal(t, 150, user.Quota)
	require.NoError(t, db.First(&task, task.ID).Error)
	require.Equal(t, 10, task.Quota)
	// Marker failure rolls back even after quota SQL ran successfully.
	require.NoError(t, db.Model(&token).Update("remain_quota", 150).Error)
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("repair_marker_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "tasks" {
			tx.AddError(errors.New("synthetic marker failure"))
		}
	}))
	_, err = AdjustLegacyTaskAccounting(&task, 30)
	require.Error(t, err)
	require.NoError(t, db.Callback().Update().Remove("repair_marker_failure"))
	require.NoError(t, db.First(&user, user.Id).Error)
	require.Equal(t, 150, user.Quota)
	require.NoError(t, db.First(&token, token.Id).Error)
	require.Zero(t, token.UsedQuota)
	mj := Midjourney{UserId: user.Id, ChannelId: channel.Id, BillingChannelId: channel.Id, Quota: 7}
	require.NoError(t, db.Create(&mj).Error)
	changed, err = ChargeLegacyMidjourneyAccounting(&mj, token.Id, "synthetic-mj-charge")
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = ChargeLegacyMidjourneyAccounting(&mj, token.Id, "synthetic-mj-charge")
	require.NoError(t, err)
	require.False(t, changed)
	staleMJ := mj
	changed, err = RefundLegacyMidjourneyAccounting(&mj)
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = RefundLegacyMidjourneyAccounting(&staleMJ)
	require.NoError(t, err)
	require.False(t, changed)
	require.NoError(t, db.First(&user, user.Id).Error)
	require.Equal(t, 150, user.Quota)
	// Encryption upgrade preserves existing credentials and ownership.
	keyFile := filepath.Join(t.TempDir(), "synthetic-key")
	require.NoError(t, os.WriteFile(keyFile, []byte(strings.Repeat("a1", 32)), 0600))
	t.Setenv("CLEARAPI_TOKEN_ENCRYPTION_KEY_FILE", keyFile)
	t.Setenv("CLEARAPI_TOKEN_ENCRYPTION", "true")
	require.Error(t, TokenEncryptionReady(db))
	// The standalone migration CLI has not initialized application SQL columns.
	previousKeyColumn := commonKeyCol
	t.Cleanup(func() { commonKeyCol = previousKeyColumn })
	commonKeyCol = ""
	require.NoError(t, MigrateTokenEncryption(db))
	require.NoError(t, MigrateTokenEncryption(db))
	commonKeyCol = previousKeyColumn
	require.NoError(t, TokenEncryptionReady(db))
	var stored string
	require.NoError(t, db.Table("tokens").Select(commonKeyCol).Where("id = ?", token.Id).Scan(&stored).Error)
	require.True(t, strings.HasPrefix(stored, "enc1:"))
	require.NotContains(t, stored, "synthetic-production-key")
	loaded, err := GetTokenByKey("synthetic-production-key", true)
	require.NoError(t, err)
	require.Equal(t, token.Id, loaded.Id)
	require.Equal(t, "synthetic-production-key", loaded.GetFullKey())
	_, err = GetTokenByIds(token.Id, user.Id+1)
	require.Error(t, err)
	// New credentials are encrypted, authentication still works.
	newToken := Token{UserId: user.Id, Key: "synthetic-production-new", RemainQuota: 100, Status: common.TokenStatusEnabled, ExpiredTime: -1}
	require.NoError(t, newToken.Insert())
	loaded, err = GetTokenByKey(newToken.Key, true)
	require.NoError(t, err)
	require.Equal(t, newToken.Key, loaded.Key)
	require.NoError(t, loaded.Delete())
	_, err = GetTokenByKey(newToken.Key, true)
	require.Error(t, err)
	newKeyFile := filepath.Join(t.TempDir(), "synthetic-new-key")
	require.NoError(t, os.WriteFile(newKeyFile, []byte(strings.Repeat("b2", 32)), 0600))
	t.Setenv("CLEARAPI_TOKEN_ENCRYPTION_OLD_KEY_FILE", keyFile)
	t.Setenv("CLEARAPI_TOKEN_ENCRYPTION_KEY_FILE", newKeyFile)
	require.Error(t, TokenEncryptionReady(db))
	require.NoError(t, RotateTokenEncryption(db))
	require.NoError(t, RotateTokenEncryption(db))
	loaded, err = GetTokenByKey("synthetic-production-key", true)
	require.NoError(t, err)
	require.Equal(t, token.Id, loaded.Id)
	// A durable canonical bill survives absent log delivery, and replay is unique.
	t.Setenv("CLEARAPI_FINANCE_ENABLED", "true")
	t.Setenv("CLEARAPI_FINANCE_OWNER_USER_ID", "5")
	common.InitFinanceConfig()
	require.NoError(t, MigrateFinanceRequests(db))
	require.NoError(t, MigrateFinanceSync(db))
	t.Setenv("CLEARAPI_BILLING_OUTBOX", "true")
	require.NoError(t, MigrateBillingReservations(db))
	require.NoError(t, MigrateBillingCompletionOutbox(db))
	require.NoError(t, MigrateBillingCompletionOutbox(db))
	require.NoError(t, MigrateBillingCompletionReceipts(db))
	require.NoError(t, MigrateBillingCompletionReceipts(db))
	require.NoError(t, BillingCompletionOutboxReady(db, db))
	require.NoError(t, ReserveBillingRequest("repair-canonical-request", user.Id, token.Id, 20))
	t.Cleanup(func() { EndBillingRequestExecution("repair-canonical-request") })
	require.NoError(t, RequestBillingCompletion("repair-canonical-request", 12, false))
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("repair_finance_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "finance_sync_events" {
			tx.AddError(errors.New("synthetic finance outbox failure"))
		}
	}))
	_, err = ApplyBillingCompletion("repair-canonical-request")
	require.Error(t, err)
	require.NoError(t, db.Callback().Create().Remove("repair_finance_failure"))
	require.NoError(t, db.First(&user, user.Id).Error)
	require.Equal(t, 130, user.Quota)
	_, err = ApplyBillingCompletion("repair-canonical-request")
	require.NoError(t, err)
	_, err = ApplyBillingCompletion("repair-canonical-request")
	require.NoError(t, err)
	var canonical BillingCompletionOutbox
	require.NoError(t, db.Where("request_id = ?", "repair-canonical-request").First(&canonical).Error)
	require.Equal(t, "pending", canonical.State)
	require.NoError(t, RecoverBillingCompletionLogs(100))
	require.NoError(t, RecoverBillingCompletionLogs(100))
	var count int64
	require.NoError(t, db.Model(&Log{}).Where("request_id = ?", "repair-canonical-request").Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.Model(&FinanceRequestEvent{}).Where("event_key = ?", "billing-committed-repair-canonical-request").Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.Model(&FinanceSyncEvent{}).Where("event_key = ?", "auto-text-settlement-repair-canonical-request").Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.First(&user, user.Id).Error)
	require.Equal(t, 138, user.Quota)
	require.NoError(t, db.Table("tokens").Where("id = ?", token.Id).Update("key", "enc1:invalid").Error)
	_, err = GetTokenByKey("synthetic-production-key", true)
	require.ErrorIs(t, err, ErrTokenStorage)
}
