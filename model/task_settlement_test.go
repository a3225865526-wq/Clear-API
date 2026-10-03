package model

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTaskSettlementDatabase(t *testing.T) {
	t.Cleanup(common.InitFinanceConfig)
	t.Setenv("CLEARAPI_FINANCE_ENABLED", "true")
	t.Setenv("CLEARAPI_FINANCE_OWNER_USER_ID", "5")
	t.Setenv("CLEARAPI_DURABLE_WALLET", "true")
	t.Setenv("CLEARAPI_BILLING_RECONCILIATION_SINGLE_INSTANCE", "true")
	t.Setenv("CLEARAPI_TASK_SETTLEMENT", "true")
	common.InitFinanceConfig()
	oldDB, oldRedis, oldBatch, oldType := DB, common.RedisEnabled, common.BatchUpdateEnabled, common.MainDatabaseType()
	common.RedisEnabled, common.BatchUpdateEnabled = false, false
	t.Cleanup(func() {
		DB = oldDB
		common.RedisEnabled, common.BatchUpdateEnabled = oldRedis, oldBatch
		common.SetMainDatabaseType(oldType)
	})
	dialect, dsn, logDSN := os.Getenv("TEST_BILLING_DIALECT"), os.Getenv("TEST_BILLING_DSN"), os.Getenv("TEST_TASK_LOG_DSN")
	open := func(log bool) *gorm.DB {
		t.Helper()
		var driver gorm.Dialector
		targetDSN := dsn
		if log {
			targetDSN = logDSN
		}
		switch dialect {
		case "", "sqlite":
			driver = sqlite.Open(filepath.Join(t.TempDir(), "task-settlement.db"))
			common.SetMainDatabaseType(common.DatabaseTypeSQLite)
		case "mysql":
			require.NotEmpty(t, targetDSN)
			driver = mysql.Open(targetDSN)
			common.SetMainDatabaseType(common.DatabaseTypeMySQL)
		case "postgres":
			require.NotEmpty(t, targetDSN)
			driver = postgres.Open(targetDSN)
			common.SetMainDatabaseType(common.DatabaseTypePostgreSQL)
		default:
			t.Fatal("unsupported dialect")
		}
		db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		require.NoError(t, err)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		sqlDB.SetMaxOpenConns(4)
		if dialect == "" || dialect == "sqlite" {
			sqlDB.SetMaxOpenConns(1)
		}
		t.Cleanup(func() { _ = sqlDB.Close() })
		return db
	}
	db, logDB := open(false), open(true)
	DB = db
	var version string
	query := "select version()"
	if dialect == "" || dialect == "sqlite" {
		query = "select sqlite_version()"
	}
	require.NoError(t, db.Raw(query).Scan(&version).Error)
	t.Logf("database %s %s", dialect, version)
	require.NoError(t, db.AutoMigrate(&User{}, &Token{}, &Task{}, &Channel{}, &UserSubscription{}, &SubscriptionPlan{}, &SubscriptionPreConsumeRecord{}, &QuotaData{}))
	legacy := Task{TaskID: "legacy-settlement-preserved", Quota: 19, Status: TaskStatusSuccess}
	require.NoError(t, db.Create(&legacy).Error)
	require.Error(t, TaskSettlementSchemaReady(db))
	require.NoError(t, MigrateFinanceRequests(db))
	require.NoError(t, MigrateFinanceSync(db))
	require.NoError(t, MigrateBillingReconciliation(db))
	require.NoError(t, MigrateTaskSettlements(db))
	require.NoError(t, MigrateTaskSettlements(db))
	require.NoError(t, TaskSettlementReady(db))
	var old Task
	require.NoError(t, db.First(&old, legacy.ID).Error)
	require.Equal(t, 19, old.Quota)
	var count int64
	require.NoError(t, db.Model(&TaskSettlement{}).Where("task_id = ?", legacy.ID).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, logDB.AutoMigrate(&Log{}))
	require.Error(t, TaskSettlementLogsReady(logDB))
	require.NoError(t, MigrateTaskSettlementLogs(logDB))
	require.NoError(t, MigrateTaskSettlementLogs(logDB))
	var seq int
	fixture := func(t *testing.T, subscription bool) (*Task, *TaskSettlement) {
		t.Helper()
		seq++
		u := User{Username: fmt.Sprintf("task-settlement-user-%d", seq), AffCode: fmt.Sprintf("task-settlement-aff-%d", seq), Quota: 1000, UsedQuota: 100, RequestCount: 1, Status: common.UserStatusEnabled}
		require.NoError(t, db.Create(&u).Error)
		key := Token{UserId: u.Id, Key: fmt.Sprintf("synthetic-task-settlement-%d", seq), RemainQuota: 1000, UsedQuota: 100, Status: common.TokenStatusEnabled, ExpiredTime: -1}
		require.NoError(t, db.Create(&key).Error)
		channel := Channel{Name: fmt.Sprintf("task-settlement-channel-%d", seq), UsedQuota: 100}
		require.NoError(t, db.Create(&channel).Error)
		task := &Task{TaskID: fmt.Sprintf("task-settlement-%d", seq), UserId: u.Id, ChannelId: channel.Id, Quota: 100, Status: TaskStatusInProgress, SubmitTime: time.Now().Unix(), PrivateData: TaskPrivateData{TokenId: key.Id, BillingSource: "wallet", Execution: &TaskExecutionSnapshot{RequestID: fmt.Sprintf("task-settlement-request-%d", seq)}}}
		if subscription {
			sub := UserSubscription{UserId: u.Id, AmountTotal: 1000, AmountUsed: 100, LastResetTime: 100}
			require.NoError(t, db.Create(&sub).Error)
			task.PrivateData.BillingSource, task.PrivateData.SubscriptionId = "subscription", sub.Id
			require.NoError(t, db.Create(&TaskSubscriptionEpoch{RequestID: task.PrivateData.Execution.RequestID, UserID: u.Id, SubscriptionID: sub.Id, ResetTime: 100}).Error)
		} else {
			require.NoError(t, db.Create(&BillingReservation{RequestID: task.PrivateData.Execution.RequestID, UserID: u.Id, TokenID: key.Id, Reserved: 100, Applied: 100, Target: 100, State: "settled"}).Error)
		}
		require.NoError(t, task.Insert())
		var r TaskSettlement
		require.NoError(t, db.Where("task_id = ?", task.ID).First(&r).Error)
		require.Equal(t, "running", r.State)
		task.Status = TaskStatusSuccess
		won, err := task.UpdateWithStatus(TaskStatusInProgress)
		require.NoError(t, err)
		require.True(t, won)
		require.NoError(t, db.First(&r, r.ID).Error)
		require.Equal(t, "awaiting_facts", r.State)
		return task, &r
	}
	freeze := func(t *testing.T, task *Task, target int) *TaskSettlement {
		t.Helper()
		r, err := FreezeTaskSettlement(db, task.ID, target, "synthetic completion", `{"total_tokens":12}`, Log{ModelName: "synthetic"})
		require.NoError(t, err)
		return r
	}
	balance := func(t *testing.T, task *Task, quota, used int) {
		t.Helper()
		var user User
		require.NoError(t, db.First(&user, task.UserId).Error)
		require.Equal(t, quota, user.Quota)
		require.Equal(t, used, user.UsedQuota)
		require.Equal(t, 1, user.RequestCount)
		var token Token
		require.NoError(t, db.Unscoped().First(&token, task.PrivateData.TokenId).Error)
		require.Equal(t, used, token.UsedQuota)
		var saved Task
		require.NoError(t, db.First(&saved, task.ID).Error)
		require.Equal(t, used, saved.Quota)
	}
	t.Run("positive negative zero and replay", func(t *testing.T) {
		for _, target := range []int{140, 60, 100, 0} {
			task, _ := fixture(t, false)
			r := freeze(t, task, target)
			_, err := ApplyTaskSettlement(db, r.ID)
			require.NoError(t, err)
			_, err = ApplyTaskSettlement(db, r.ID)
			require.NoError(t, err)
			balance(t, task, 1100-target, target)
			var exported struct {
				Quota int
				Count int
			}
			require.NoError(t, db.Model(&QuotaData{}).Select("COALESCE(SUM(quota), 0) AS quota, COALESCE(SUM(count), 0) AS count").Where("user_id = ?", task.UserId).Scan(&exported).Error)
			require.Equal(t, max(target-100, 0), exported.Quota)
			require.Equal(t, min(max(target-100, 0), 1), exported.Count)
			_, err = FreezeTaskSettlement(db, task.ID, target+1, "different", "{}", Log{})
			require.ErrorIs(t, err, ErrFinanceConflict)
			var audits int64
			require.NoError(t, db.Model(&FinanceAudit{}).Where("action = ? AND entity_id = ?", "task.settle", r.ID).Count(&audits).Error)
			require.EqualValues(t, 1, audits)
		}
	})
	t.Run("insufficient wallet token and rollback", func(t *testing.T) {
		for _, lowWallet := range []bool{true, false} {
			task, _ := fixture(t, false)
			if lowWallet {
				require.NoError(t, db.Model(&User{}).Where("id = ?", task.UserId).Update("quota", 10).Error)
			} else {
				require.NoError(t, db.Model(&Token{}).Where("id = ?", task.PrivateData.TokenId).Update("remain_quota", 10).Error)
			}
			r := freeze(t, task, 140)
			_, err := ApplyTaskSettlement(db, r.ID)
			require.ErrorIs(t, err, ErrFinanceConflict)
			require.NoError(t, db.First(r, r.ID).Error)
			require.Equal(t, "needs_review", r.State)
			wallet := 1000
			if lowWallet {
				wallet = 10
			}
			balance(t, task, wallet, 100)
			_, err = RetryTaskSettlement(db, r.ID, 5, TaskSettlementRetryInput{Confirmed: true, Reason: "verified", ExpectedTarget: 140, ExpectedAttempts: r.Attempts + 1})
			require.ErrorIs(t, err, ErrFinanceConflict)
			require.NoError(t, db.Model(&User{}).Where("id = ?", task.UserId).Update("quota", 1000).Error)
			require.NoError(t, db.Model(&Token{}).Where("id = ?", task.PrivateData.TokenId).Update("remain_quota", 1000).Error)
			_, err = RetryTaskSettlement(db, r.ID, 5, TaskSettlementRetryInput{Confirmed: true, Reason: "verified restored quota", ExpectedTarget: 140, ExpectedAttempts: r.Attempts})
			require.NoError(t, err)
			balance(t, task, 960, 140)
		}
	})
	t.Run("subscription insufficient reset deletion and original source", func(t *testing.T) {
		for _, mode := range []string{"ok", "insufficient", "reset", "deleted"} {
			task, _ := fixture(t, true)
			switch mode {
			case "insufficient":
				require.NoError(t, db.Model(&UserSubscription{}).Where("id = ?", task.PrivateData.SubscriptionId).Update("amount_total", 110).Error)
			case "reset":
				require.NoError(t, db.Model(&UserSubscription{}).Where("id = ?", task.PrivateData.SubscriptionId).Update("last_reset_time", 101).Error)
			case "deleted":
				require.NoError(t, db.Delete(&UserSubscription{}, task.PrivateData.SubscriptionId).Error)
			}
			r := freeze(t, task, 140)
			_, err := ApplyTaskSettlement(db, r.ID)
			if mode == "ok" {
				require.NoError(t, err)
				balance(t, task, 1000, 140)
				var sub UserSubscription
				require.NoError(t, db.First(&sub, task.PrivateData.SubscriptionId).Error)
				require.EqualValues(t, 140, sub.AmountUsed)
			} else {
				require.ErrorIs(t, err, ErrFinanceConflict)
				balance(t, task, 1000, 100)
			}
		}
	})
	t.Run("subscription epoch is captured before task insertion and refund is transactional", func(t *testing.T) {
		task, _ := fixture(t, true)
		plan := SubscriptionPlan{Title: "synthetic epoch plan", QuotaResetPeriod: "never"}
		require.NoError(t, db.Create(&plan).Error)
		require.NoError(t, db.Model(&UserSubscription{}).Where("id = ?", task.PrivateData.SubscriptionId).Updates(map[string]any{"plan_id": plan.Id, "status": "active", "end_time": time.Now().Unix() + 3600}).Error)
		requestID := fmt.Sprintf("subscription-epoch-request-%d", task.ID)
		_, err := PreConsumeUserSubscription(requestID, task.UserId, "synthetic", 0, 10)
		require.NoError(t, err)
		var epoch TaskSubscriptionEpoch
		require.NoError(t, db.Where("request_id = ?", requestID).First(&epoch).Error)
		require.EqualValues(t, 100, epoch.ResetTime)
		require.NoError(t, db.Model(&UserSubscription{}).Where("id = ?", task.PrivateData.SubscriptionId).Updates(map[string]any{"last_reset_time": 101, "amount_used": 5}).Error)
		require.ErrorIs(t, PostConsumeUserSubscriptionDelta(task.PrivateData.SubscriptionId, -10, requestID), ErrFinanceConflict)
		require.ErrorIs(t, RefundSubscriptionPreConsume(requestID), ErrFinanceConflict)
		_, err = PreConsumeUserSubscription(requestID, task.UserId, "synthetic", 0, 10)
		require.ErrorIs(t, err, ErrFinanceConflict)
		newTask := Task{TaskID: "task-after-cycle-reset", UserId: task.UserId, Quota: 10, Status: TaskStatusInProgress, PrivateData: TaskPrivateData{BillingSource: "subscription", SubscriptionId: task.PrivateData.SubscriptionId, Execution: &TaskExecutionSnapshot{RequestID: requestID}}}
		require.NoError(t, newTask.Insert())
		var review TaskSettlement
		require.NoError(t, db.Where("task_id = ?", newTask.ID).First(&review).Error)
		require.Equal(t, "needs_review", review.State)
		require.Equal(t, "subscription_reset", review.ErrorClass)
		require.NoError(t, db.Model(&UserSubscription{}).Where("id = ?", task.PrivateData.SubscriptionId).Updates(map[string]any{"last_reset_time": 100, "amount_used": 110}).Error)
		require.NoError(t, RefundSubscriptionPreConsume(requestID))
		require.NoError(t, RefundSubscriptionPreConsume(requestID))
		var sub UserSubscription
		require.NoError(t, db.First(&sub, task.PrivateData.SubscriptionId).Error)
		require.EqualValues(t, 100, sub.AmountUsed)
	})
	t.Run("one request cannot fund two task rows", func(t *testing.T) {
		task, _ := fixture(t, false)
		duplicate := *task
		duplicate.ID = 0
		duplicate.TaskID = "duplicate-funded-task"
		require.Error(t, duplicate.Insert())
		var count int64
		require.NoError(t, db.Model(&Task{}).Where("task_id = ?", duplicate.TaskID).Count(&count).Error)
		require.Zero(t, count)
	})
	t.Run("deleted and unlimited tokens", func(t *testing.T) {
		for _, mode := range []string{"soft", "hard_refund", "hard_charge", "unlimited"} {
			task, _ := fixture(t, false)
			target := 60
			if mode == "soft" {
				require.NoError(t, db.Delete(&Token{}, task.PrivateData.TokenId).Error)
			}
			if mode == "hard_refund" || mode == "hard_charge" {
				require.NoError(t, db.Unscoped().Delete(&Token{}, task.PrivateData.TokenId).Error)
			}
			if mode == "hard_charge" {
				target = 140
			}
			if mode == "unlimited" {
				target = 140
				require.NoError(t, db.Model(&Token{}).Where("id = ?", task.PrivateData.TokenId).Updates(map[string]any{"unlimited_quota": true, "remain_quota": 0}).Error)
			}
			r := freeze(t, task, target)
			_, err := ApplyTaskSettlement(db, r.ID)
			if mode == "hard_charge" {
				require.ErrorIs(t, err, ErrFinanceConflict)
			} else {
				require.NoError(t, err)
			}
			var user User
			require.NoError(t, db.First(&user, task.UserId).Error)
			expected := 1100 - target
			if mode == "hard_charge" {
				expected = 1000
			}
			require.Equal(t, expected, user.Quota)
		}
	})
	t.Run("refund cap and bad counters roll back", func(t *testing.T) {
		for _, cap := range []bool{true, false} {
			task, _ := fixture(t, false)
			if cap {
				require.NoError(t, db.Model(&User{}).Where("id = ?", task.UserId).Update("quota", common.MaxWalletQuota).Error)
			} else {
				require.NoError(t, db.Model(&Token{}).Where("id = ?", task.PrivateData.TokenId).Update("used_quota", 0).Error)
			}
			r := freeze(t, task, 60)
			_, err := ApplyTaskSettlement(db, r.ID)
			require.ErrorIs(t, err, ErrFinanceConflict)
			var user User
			require.NoError(t, db.First(&user, task.UserId).Error)
			want := int64(1000)
			if cap {
				want = common.MaxWalletQuota
			}
			require.EqualValues(t, want, user.Quota)
		}
	})
	t.Run("audit failure pending and recovery", func(t *testing.T) {
		task, _ := fixture(t, false)
		r := freeze(t, task, 140)
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register("task_audit_failure", func(tx *gorm.DB) {
			if tx.Statement.Table == "finance_audits" {
				tx.AddError(errors.New("synthetic audit unavailable"))
			}
		}))
		_, err := ApplyTaskSettlement(db, r.ID)
		require.Error(t, err)
		require.NoError(t, db.Callback().Create().Remove("task_audit_failure"))
		balance(t, task, 1000, 100)
		var exported int64
		require.NoError(t, db.Model(&QuotaData{}).Where("user_id = ?", task.UserId).Count(&exported).Error)
		require.Zero(t, exported)
		require.NoError(t, db.First(r, r.ID).Error)
		require.Equal(t, "pending", r.State)
		require.Greater(t, r.NextAttempt, time.Now().Unix())
		require.NoError(t, db.Model(r).Update("next_attempt", 0).Error)
		require.True(t, HasUnfinishedSyncTasks())
		require.NoError(t, RecoverTaskSettlements(db, logDB))
		balance(t, task, 960, 140)
	})
	t.Run("automatic retries stop and log-only retry never moves funding", func(t *testing.T) {
		task, _ := fixture(t, false)
		r := freeze(t, task, 140)
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register("task_bounded_audit_failure", func(tx *gorm.DB) {
			if tx.Statement.Table == "finance_audits" {
				tx.AddError(errors.New("synthetic audit failure"))
			}
		}))
		t.Cleanup(func() { _ = db.Callback().Create().Remove("task_bounded_audit_failure") })
		for range 5 {
			_, err := ApplyTaskSettlement(db, r.ID)
			require.Error(t, err)
		}
		require.NoError(t, db.Callback().Create().Remove("task_bounded_audit_failure"))
		require.NoError(t, db.First(r, r.ID).Error)
		require.Equal(t, "needs_review", r.State)
		require.Equal(t, 5, r.Attempts)
		balance(t, task, 1000, 100)
		_, err := ApplyTaskSettlement(db, r.ID)
		require.ErrorIs(t, err, ErrFinanceConflict)
		_, err = RetryTaskSettlement(db, r.ID, 5, TaskSettlementRetryInput{Confirmed: true, Reason: "database restored", ExpectedAttempts: 5, ExpectedTarget: 140})
		require.NoError(t, err)
		balance(t, task, 960, 140)
		require.NoError(t, logDB.Callback().Create().Before("gorm:create").Register("task_log_failure", func(tx *gorm.DB) {
			if tx.Statement.Table == "logs" {
				tx.AddError(errors.New("synthetic log unavailable"))
			}
		}))
		t.Cleanup(func() { _ = logDB.Callback().Create().Remove("task_log_failure") })
		for range 5 {
			require.NoError(t, db.Model(r).Update("log_next_attempt", 0).Error)
			require.NoError(t, DeliverTaskSettlementLogs(db, logDB))
		}
		require.NoError(t, logDB.Callback().Create().Remove("task_log_failure"))
		require.NoError(t, db.First(r, r.ID).Error)
		require.Equal(t, "needs_review", r.LogState)
		require.Equal(t, 5, r.LogAttempts)
		_, err = RetryTaskSettlement(db, r.ID, 5, TaskSettlementRetryInput{Confirmed: true, Reason: "log database restored", LogOnly: true, ExpectedLogAttempts: 5, ExpectedTarget: 140})
		require.NoError(t, err)
		require.NoError(t, DeliverTaskSettlementLogs(db, logDB))
		balance(t, task, 960, 140)
		var logs int64
		require.NoError(t, logDB.Model(&Log{}).Where("user_id = ?", task.UserId).Count(&logs).Error)
		require.EqualValues(t, 1, logs)
	})
	t.Run("concurrent settlement moves money once", func(t *testing.T) {
		task, _ := fixture(t, false)
		r := freeze(t, task, 140)
		var wg sync.WaitGroup
		errs := make(chan error, 6)
		for range 6 {
			wg.Go(func() { _, err := ApplyTaskSettlement(db, r.ID); errs <- err })
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		balance(t, task, 960, 140)
	})
	t.Run("missing facts never guess a refund and historical tasks remain unverified", func(t *testing.T) {
		task, r := fixture(t, false)
		require.NoError(t, db.Model(r).Update("updated_at", time.Now().Unix()-61).Error)
		require.NoError(t, RecoverTaskSettlements(db, logDB))
		require.NoError(t, db.First(r, r.ID).Error)
		require.Equal(t, "needs_review", r.State)
		require.Nil(t, r.TargetQuota)
		balance(t, task, 1000, 100)
		_, err := RetryTaskSettlement(db, r.ID, 5, TaskSettlementRetryInput{Confirmed: true, Reason: "cannot invent a target"})
		require.ErrorIs(t, err, ErrFinanceConflict)
		historical := Task{TaskID: "old-running", Status: TaskStatusInProgress, Quota: 100}
		require.NoError(t, db.Create(&historical).Error)
		historical.Status = TaskStatusFailure
		won, err := historical.UpdateWithStatus(TaskStatusInProgress)
		require.NoError(t, err)
		require.True(t, won)
		var h TaskSettlement
		require.NoError(t, db.Where("task_id = ?", historical.ID).First(&h).Error)
		require.Equal(t, "historical_unverified", h.ErrorClass)
	})
	t.Run("reservation review cannot refund an owned task", func(t *testing.T) {
		task, _ := fixture(t, false)
		var r BillingReservation
		require.NoError(t, db.Where("request_id = ?", task.PrivateData.Execution.RequestID).First(&r).Error)
		require.NoError(t, db.Create(&BillingReviewMarker{ReservationID: r.ID, Reason: "upstream_usage_unknown"}).Error)
		_, err := DecideBillingReservation(db, r.ID, 5, BillingDecisionInput{ExpectedState: r.State, ExpectedApplied: 100, ExpectedTarget: 100, RefundQuota: 20, Reason: "synthetic review", ExecutionEnded: true}, "operator-request")
		require.ErrorIs(t, err, ErrFinanceConflict)
		freeze(t, task, 140)
	})
	t.Run("separate log inbox replay and acknowledgement failure", func(t *testing.T) {
		task, _ := fixture(t, false)
		r := freeze(t, task, 140)
		_, err := ApplyTaskSettlement(db, r.ID)
		require.NoError(t, err)
		require.NoError(t, db.Callback().Update().Before("gorm:update").Register("task_ack_failure", func(tx *gorm.DB) {
			if tx.Statement.Table == "task_settlements" {
				tx.AddError(errors.New("synthetic acknowledgement unavailable"))
			}
		}))
		require.Error(t, DeliverTaskSettlementLogs(db, logDB))
		require.NoError(t, db.Callback().Update().Remove("task_ack_failure"))
		require.NoError(t, DeliverTaskSettlementLogs(db, logDB))
		require.NoError(t, DeliverTaskSettlementLogs(db, logDB))
		var logs int64
		require.NoError(t, logDB.Model(&Log{}).Where("user_id = ?", task.UserId).Count(&logs).Error)
		require.EqualValues(t, 1, logs)
	})
	t.Run("invalid amounts owner confirmation and switch", func(t *testing.T) {
		task, r := fixture(t, false)
		_, err := FreezeTaskSettlement(db, task.ID, -1, "invalid", "{}", Log{})
		require.ErrorIs(t, err, ErrFinanceInvalid)
		_, err = RetryTaskSettlement(db, r.ID, 7, TaskSettlementRetryInput{})
		require.ErrorIs(t, err, ErrFinanceInvalid)
		_, err = RetryTaskSettlement(db, r.ID, 5, TaskSettlementRetryInput{})
		require.ErrorIs(t, err, ErrFinanceInvalid)
		t.Setenv("CLEARAPI_TASK_SETTLEMENT", "false")
		require.Error(t, TaskSettlementReady(db))
	})
}
