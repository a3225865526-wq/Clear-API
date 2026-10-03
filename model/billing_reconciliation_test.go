package model

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestBillingReconciliationDatabase(t *testing.T) {
	t.Cleanup(common.InitFinanceConfig)
	t.Setenv("CLEARAPI_FINANCE_ENABLED", "true")
	t.Setenv("CLEARAPI_FINANCE_OWNER_USER_ID", "5")
	t.Setenv("CLEARAPI_DURABLE_WALLET", "true")
	t.Setenv("CLEARAPI_BILLING_RECONCILIATION_SINGLE_INSTANCE", "true")
	common.InitFinanceConfig()
	oldDB, oldRedis, oldBatch := DB, common.RedisEnabled, common.BatchUpdateEnabled
	oldType := common.MainDatabaseType()
	common.RedisEnabled, common.BatchUpdateEnabled = false, false
	t.Cleanup(func() {
		DB = oldDB
		common.RedisEnabled, common.BatchUpdateEnabled = oldRedis, oldBatch
		common.SetMainDatabaseType(oldType)
	})
	dialect, dsn := os.Getenv("TEST_BILLING_DIALECT"), os.Getenv("TEST_BILLING_DSN")
	var driver gorm.Dialector
	switch dialect {
	case "", "sqlite":
		driver = sqlite.Open(filepath.Join(t.TempDir(), "review.db"))
		common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	case "mysql":
		require.NotEmpty(t, dsn)
		driver = mysql.Open(dsn)
		common.SetMainDatabaseType(common.DatabaseTypeMySQL)
	case "postgres":
		require.NotEmpty(t, dsn)
		driver = postgres.Open(dsn)
		common.SetMainDatabaseType(common.DatabaseTypePostgreSQL)
	default:
		t.Fatal("unsupported dialect")
	}
	db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = sqlDB.Close() })
	DB = db
	var version string
	query := "select version()"
	if dialect == "" || dialect == "sqlite" {
		query = "select sqlite_version()"
	}
	require.NoError(t, db.Raw(query).Scan(&version).Error)
	t.Logf("database %s %s", dialect, version)
	require.NoError(t, db.AutoMigrate(&User{}, &Token{}))
	require.NoError(t, MigrateBillingReservations(db))
	legacy := BillingReservation{RequestID: "review-legacy", UserID: 200, TokenID: 200, Reserved: 100, Applied: 100, Target: 120, State: "needs_review", UpdatedAt: 123}
	require.NoError(t, db.Create(&legacy).Error)
	require.Error(t, BillingReconciliationReady(db))
	require.Error(t, MigrateBillingReconciliation(db)) // finance schema must already exist
	require.NoError(t, MigrateFinanceRequests(db))
	require.NoError(t, MigrateFinanceSync(db))
	require.NoError(t, MigrateBillingReconciliation(db))
	require.NoError(t, MigrateBillingReconciliation(db))
	require.NoError(t, BillingReconciliationReady(db))
	var preserved BillingReservation
	require.NoError(t, db.First(&preserved, legacy.ID).Error)
	require.Equal(t, legacy, preserved)
	var fixture int
	reserve := func(t *testing.T) (*User, *Token, BillingReservation) {
		t.Helper()
		fixture++
		user := &User{Username: fmt.Sprintf("review-user-%d", fixture), Quota: 1000, Status: common.UserStatusEnabled, AffCode: fmt.Sprintf("review-aff-%d", fixture)}
		require.NoError(t, db.Create(user).Error)
		token := &Token{UserId: user.Id, Key: fmt.Sprintf("synthetic-review-%d", fixture), RemainQuota: 1000, Status: common.TokenStatusEnabled, ExpiredTime: -1}
		require.NoError(t, db.Create(token).Error)
		id := fmt.Sprintf("review-request-%d", fixture)
		require.NoError(t, ReserveBillingRequest(id, user.Id, token.Id, 100))
		t.Cleanup(func() { EndBillingRequestExecution(id) })
		var r BillingReservation
		require.NoError(t, db.Where("request_id = ?", id).First(&r).Error)
		return user, token, r
	}
	input := func(r BillingReservation, refund int) BillingDecisionInput {
		return BillingDecisionInput{ExpectedState: r.State, ExpectedApplied: r.Applied, ExpectedTarget: r.Target, RefundQuota: refund, Reason: "provider receipt verified", ExecutionEnded: true}
	}
	balance := func(t *testing.T, user *User, token *Token, quota int) {
		t.Helper()
		require.NoError(t, db.Unscoped().First(user, user.Id).Error)
		require.Equal(t, quota, user.Quota)
		if token != nil {
			require.NoError(t, db.Unscoped().First(token, token.Id).Error)
			require.Equal(t, quota, token.RemainQuota)
			require.Equal(t, 1000-quota, token.UsedQuota)
		}
	}
	t.Run("live request cannot be decided", func(t *testing.T) {
		_, _, r := reserve(t)
		_, err := DecideBillingReservation(db, r.ID, 5, input(r, 100), "review-op")
		require.ErrorIs(t, err, ErrFinanceConflict)
	})
	t.Run("partial refund is atomic idempotent and preserves original intent", func(t *testing.T) {
		u, key, r := reserve(t)
		require.NoError(t, RequestBillingCompletion(r.RequestID, 150, false))
		require.NoError(t, db.First(&r, r.ID).Error)
		EndBillingRequestExecution(r.RequestID)
		in := input(r, 40)
		d, err := DecideBillingReservation(db, r.ID, 5, in, "review-op")
		require.NoError(t, err)
		require.Equal(t, 60, d.FinalQuota)
		require.Equal(t, 150, d.Target)
		balance(t, u, key, 940)
		repeat, err := DecideBillingReservation(db, r.ID, 5, in, "retry-op")
		require.NoError(t, err)
		require.Equal(t, d.ID, repeat.ID)
		balance(t, u, key, 940)
		in.RefundQuota = 41
		_, err = DecideBillingReservation(db, r.ID, 5, in, "other-op")
		require.ErrorIs(t, err, ErrFinanceConflict)
		var audits int64
		require.NoError(t, db.Model(&FinanceAudit{}).Where("action = ? AND entity_id = ?", "billing.decide", d.ID).Count(&audits).Error)
		require.EqualValues(t, 1, audits)
		var outbox FinanceSyncEvent
		require.NoError(t, db.Where("topic = ? AND ref_id = ?", "billing_reconciliation", d.ID).First(&outbox).Error)
		require.Equal(t, FinanceSyncNeedsReview, outbox.Status)
		require.Equal(t, "refund_requires_review", outbox.ErrorClass)
		history, _, err := ListBillingReconciliation(db, "reviewed", 0, 100)
		require.NoError(t, err)
		require.Len(t, history, 1)
		require.Equal(t, 150, history[0].Target)
		require.NotNil(t, history[0].Decision)
	})
	t.Run("unknown usage stays queryable after full settlement", func(t *testing.T) {
		u, key, r := reserve(t)
		require.NoError(t, MarkBillingUsageUncertain(r.RequestID))
		require.NoError(t, MarkBillingUsageUncertain(r.RequestID))
		require.NoError(t, RequestBillingCompletion(r.RequestID, 100, false))
		_, err := ApplyBillingCompletion(r.RequestID)
		require.NoError(t, err)
		EndBillingRequestExecution(r.RequestID)
		rows, _, err := ListBillingReconciliation(db, "pending", 0, 100)
		require.NoError(t, err)
		found := false
		for _, v := range rows {
			if v.ID == r.ID {
				found = true
				require.Equal(t, "upstream_usage_unknown", v.Reason)
			}
		}
		require.True(t, found)
		require.NoError(t, db.First(&r, r.ID).Error)
		d, err := DecideBillingReservation(db, r.ID, 5, input(r, 100), "review-op")
		require.NoError(t, err)
		require.Zero(t, d.FinalQuota)
		balance(t, u, key, 1000)
	})
	t.Run("normal settled charge cannot be refunded through exception workflow", func(t *testing.T) {
		_, _, r := reserve(t)
		require.NoError(t, RequestBillingCompletion(r.RequestID, 80, false))
		_, err := ApplyBillingCompletion(r.RequestID)
		require.NoError(t, err)
		EndBillingRequestExecution(r.RequestID)
		require.NoError(t, db.First(&r, r.ID).Error)
		_, err = DecideBillingReservation(db, r.ID, 5, input(r, 10), "review-op")
		require.ErrorIs(t, err, ErrFinanceConflict)
	})
	t.Run("keep charge seals abandoned reservation", func(t *testing.T) {
		u, key, r := reserve(t)
		EndBillingRequestExecution(r.RequestID)
		d, err := DecideBillingReservation(db, r.ID, 5, input(r, 0), "review-op")
		require.NoError(t, err)
		require.Equal(t, 100, d.FinalQuota)
		balance(t, u, key, 900)
		require.ErrorIs(t, RequestBillingCompletion(r.RequestID, 0, true), ErrBillingReservationConflict)
	})
	t.Run("stale recovery snapshot is rejected", func(t *testing.T) {
		u, key, r := reserve(t)
		require.NoError(t, RequestBillingCompletion(r.RequestID, 80, false))
		require.NoError(t, db.First(&r, r.ID).Error)
		stale := input(r, 100)
		EndBillingRequestExecution(r.RequestID)
		_, err := ApplyBillingCompletion(r.RequestID)
		require.NoError(t, err)
		_, err = DecideBillingReservation(db, r.ID, 5, stale, "review-op")
		require.ErrorIs(t, err, ErrFinanceConflict)
		balance(t, u, key, 920)
	})
	t.Run("soft deleted token still receives quota correction", func(t *testing.T) {
		u, key, r := reserve(t)
		EndBillingRequestExecution(r.RequestID)
		require.NoError(t, db.Delete(key).Error)
		d, err := DecideBillingReservation(db, r.ID, 5, input(r, 100), "review-op")
		require.NoError(t, err)
		require.Equal(t, "credited", d.TokenRefund)
		balance(t, u, key, 1000)
	})
	t.Run("hard deleted token does not block wallet refund", func(t *testing.T) {
		u, key, r := reserve(t)
		EndBillingRequestExecution(r.RequestID)
		require.NoError(t, db.Unscoped().Delete(key).Error)
		d, err := DecideBillingReservation(db, r.ID, 5, input(r, 100), "review-op")
		require.NoError(t, err)
		require.Equal(t, "token_missing", d.TokenRefund)
		balance(t, u, nil, 1000)
	})
	t.Run("quota ceiling and invalid token counters roll back whole decision", func(t *testing.T) {
		u, key, r := reserve(t)
		EndBillingRequestExecution(r.RequestID)
		require.NoError(t, db.Model(u).Update("quota", common.MaxWalletQuota).Error)
		_, err := DecideBillingReservation(db, r.ID, 5, input(r, 1), "review-op")
		require.ErrorIs(t, err, ErrFinanceConflict)
		require.NoError(t, db.Model(u).Update("quota", 900).Error)
		require.NoError(t, db.Model(key).Update("used_quota", 0).Error)
		_, err = DecideBillingReservation(db, r.ID, 5, input(r, 1), "review-op")
		require.ErrorIs(t, err, ErrFinanceConflict)
		var current User
		require.NoError(t, db.First(&current, u.Id).Error)
		require.Equal(t, 900, current.Quota)
		require.NoError(t, db.First(&r, r.ID).Error)
		require.Equal(t, "active", r.State)
	})
	t.Run("audit failure rolls back refund and can be retried", func(t *testing.T) {
		u, key, r := reserve(t)
		EndBillingRequestExecution(r.RequestID)
		require.NoError(t, db.Callback().Create().After("gorm:create").Register("review:fail_audit", func(tx *gorm.DB) {
			if tx.Statement.Table == "finance_audits" {
				tx.AddError(errors.New("synthetic audit failure"))
			}
		}))
		_, err := DecideBillingReservation(db, r.ID, 5, input(r, 100), "review-op")
		require.Error(t, err)
		require.NoError(t, db.Callback().Create().Remove("review:fail_audit"))
		balance(t, u, key, 900)
		require.NoError(t, db.First(&r, r.ID).Error)
		require.Equal(t, "active", r.State)
		_, err = DecideBillingReservation(db, r.ID, 5, input(r, 100), "retry-op")
		require.NoError(t, err)
		balance(t, u, key, 1000)
	})
	t.Run("parallel final decisions refund exactly once", func(t *testing.T) {
		u, key, r := reserve(t)
		EndBillingRequestExecution(r.RequestID)
		in := input(r, 30)
		var wg sync.WaitGroup
		ids := make(chan int64, 6)
		for range 6 {
			wg.Go(func() {
				d, err := DecideBillingReservation(db, r.ID, 5, in, "parallel-op")
				if err == nil {
					ids <- d.ID
				}
			})
		}
		wg.Wait()
		close(ids)
		var first int64
		count := 0
		for id := range ids {
			if first == 0 {
				first = id
			}
			require.Equal(t, first, id)
			count++
		}
		require.Equal(t, 6, count)
		balance(t, u, key, 930)
	})
	t.Run("recovery competing with decision never double credits", func(t *testing.T) {
		u, key, r := reserve(t)
		require.NoError(t, RequestBillingCompletion(r.RequestID, 0, true))
		require.NoError(t, db.First(&r, r.ID).Error)
		EndBillingRequestExecution(r.RequestID)
		var wg sync.WaitGroup
		wg.Go(func() { _, _ = ApplyBillingCompletion(r.RequestID) })
		wg.Go(func() { _, _ = DecideBillingReservation(db, r.ID, 5, input(r, 100), "race-op") })
		wg.Wait()
		balance(t, u, key, 1000)
	})
	t.Run("nonowner missing confirmation excess refund and disabled switch reject", func(t *testing.T) {
		_, _, r := reserve(t)
		EndBillingRequestExecution(r.RequestID)
		in := input(r, 100)
		_, err := DecideBillingReservation(db, r.ID, 6, in, "review-op")
		require.ErrorIs(t, err, ErrFinanceInvalid)
		in.ExecutionEnded = false
		_, err = DecideBillingReservation(db, r.ID, 5, in, "review-op")
		require.ErrorIs(t, err, ErrFinanceInvalid)
		in = input(r, 101)
		_, err = DecideBillingReservation(db, r.ID, 5, in, "review-op")
		require.ErrorIs(t, err, ErrFinanceConflict)
		t.Setenv("CLEARAPI_BILLING_RECONCILIATION_SINGLE_INSTANCE", "false")
		_, err = DecideBillingReservation(db, r.ID, 5, input(r, 10), "review-op")
		require.ErrorIs(t, err, ErrFinanceUnavailable)
	})
}
