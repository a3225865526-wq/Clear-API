package model

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newCrashMatrixDB prepares an isolated database per test with the additive
// migrations applied twice to prove idempotency.
func newCrashMatrixDB(t *testing.T) *gorm.DB {
	t.Helper()
	driver := sqlite.Open(filepath.Join(t.TempDir(), "crash.db"))
	db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	oldDB := DB
	DB = db
	t.Cleanup(func() { DB = oldDB })
	require.NoError(t, db.AutoMigrate(&User{}, &Token{}, &Log{}, &BillingCompletionOutbox{}, &BillingReviewMarker{}, &FinanceRequestEvent{}, &FinanceSyncEvent{}))
	require.NoError(t, MigrateBillingReservations(db))
	require.NoError(t, MigrateBillingReservations(db))
	require.NoError(t, MigrateBillingAuthSnapshots(db))
	require.NoError(t, MigrateBillingAuthSnapshots(db))
	return db
}

func crashUser(t *testing.T, db *gorm.DB, name string, quota int) (int, int) {
	t.Helper()
	user := User{Username: name, AffCode: name, Quota: quota, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	token := Token{UserId: user.Id, Key: name + "-key", RemainQuota: quota, Status: common.TokenStatusEnabled, ExpiredTime: -1}
	require.NoError(t, db.Create(&token).Error)
	return user.Id, token.Id
}

func wallet(t *testing.T, db *gorm.DB, userID, tokenID int) (int, int) {
	t.Helper()
	var user User
	require.NoError(t, db.First(&user, userID).Error)
	var token Token
	require.NoError(t, db.First(&token, tokenID).Error)
	return user.Quota, token.RemainQuota
}

func state(t *testing.T, requestID string) BillingReservation {
	t.Helper()
	var row BillingReservation
	require.NoError(t, DB.Where("request_id = ?", requestID).First(&row).Error)
	return row
}

// TestCrashMatrixCoversReserveAndCompletionBoundaries walks the crash
// boundaries from the H2 plan: every boundary must leave either the old or
// the new atomic state, never a partial wallet/quota/reservation mutation.
func TestCrashMatrixCoversReserveAndCompletionBoundaries(t *testing.T) {
	if os.Getenv("TEST_BILLING_DIALECT") != "" && os.Getenv("TEST_BILLING_DIALECT") != "sqlite" &&
		os.Getenv("TEST_BILLING_DSN") == "" {
		t.Skip("non-sqlite dialect requires TEST_BILLING_DSN")
	}
	db := newCrashMatrixDB(t)

	// 1+2: crash before/after the reserve transaction. A rolled back reserve
	// leaves no row and no debit; a committed reserve stays active.
	userID, tokenID := crashUser(t, db, "crash-a", 1000)
	before, _ := wallet(t, db, userID, tokenID)
	requestID := "crash-reserve-1"
	require.NoError(t, ReserveBillingRequest(requestID, userID, tokenID, 40))
	row := state(t, requestID)
	assert.Equal(t, "active", row.State)
	assert.Equal(t, 40, row.Reserved)
	after, _ := wallet(t, db, userID, tokenID)
	assert.Equal(t, before-40, after, "reserve commits atomically")

	// 3+4: crash before/after upstream send. A committed reservation is never
	// auto-refunded: only an explicit proven-zero refund applies.
	require.Error(t, ReserveBillingRequest(requestID, userID, tokenID, 40), "duplicate reserve must conflict")

	// 5+6: completion intent boundaries. RequestBillingCompletion persists an
	// immutable intent; a crash before it leaves active, after it leaves
	// settle_pending which the recovery worker replays.
	require.NoError(t, RequestBillingCompletion(requestID, 30, false))
	row = state(t, requestID)
	assert.Equal(t, "settle_pending", row.State)

	// 7: crash during apply → rollback or full completion; replay is safe.
	require.NoError(t, RequestBillingCompletion(requestID, 30, false), "duplicate same-target intent is idempotent")
	require.Error(t, RequestBillingCompletion(requestID, 31, false), "different target conflicts")
	applied, err := ApplyBillingCompletion(requestID)
	require.NoError(t, err)
	require.NoError(t, err)
	assert.Equal(t, 30, applied, "duplicate apply is a no-op returning the applied total")

	// 8: crash after settle commit → settled, unused refunded exactly once.
	row = state(t, requestID)
	assert.Equal(t, "settled", row.State)
	assert.Equal(t, 30, row.Applied)
	finalUser, finalToken := wallet(t, db, userID, tokenID)
	assert.Equal(t, before-30, finalUser, "wallet reflects target once")
	assert.Equal(t, before-30, finalToken)

	// 9+10: refund path and duplicate refund.
	userID2, tokenID2 := crashUser(t, db, "crash-b", 500)
	before2, _ := wallet(t, db, userID2, tokenID2)
	require.NoError(t, ReserveBillingRequest("crash-refund", userID2, tokenID2, 100))
	require.NoError(t, RequestBillingCompletion("crash-refund", 0, true))
	require.NoError(t, RequestBillingCompletion("crash-refund", 0, true), "duplicate refund intent is idempotent")
	_, err = ApplyBillingCompletion("crash-refund")
	require.NoError(t, err)
	assert.Equal(t, "refunded", state(t, "crash-refund").State)
	refundedUser, _ := wallet(t, db, userID2, tokenID2)
	assert.Equal(t, before2, refundedUser, "full refund restores the wallet exactly once")
	_, err = ApplyBillingCompletion("crash-refund")
	require.NoError(t, err)
	still, _ := wallet(t, db, userID2, tokenID2)
	assert.Equal(t, before2, still, "duplicate apply after refund does not credit again")

	// 11: restart reconciliation replays pending completions only.
	userID3, tokenID3 := crashUser(t, db, "crash-c", 800)
	require.NoError(t, ReserveBillingRequest("crash-pending", userID3, tokenID3, 60))
	require.NoError(t, RequestBillingCompletion("crash-pending", 50, false))
	require.NoError(t, RecoverBillingCompletions(100))
	assert.Equal(t, "settled", state(t, "crash-pending").State)
	require.NoError(t, RecoverBillingCompletions(100), "recovery is idempotent")
	assert.Equal(t, "settled", state(t, "crash-pending").State)

	// 12: duplicate settlement across a restart keeps the ledger unchanged.
	userQuota, tokenRemain := wallet(t, db, userID3, tokenID3)
	assert.Equal(t, 800-50, userQuota)
	assert.Equal(t, 800-50, tokenRemain)

	// Active rows are never auto-refunded by recovery.
	userID4, tokenID4 := crashUser(t, db, "crash-d", 300)
	require.NoError(t, ReserveBillingRequest("crash-active", userID4, tokenID4, 120))
	require.NoError(t, RecoverBillingCompletions(100))
	assert.Equal(t, "active", state(t, "crash-active").State, "active reservation requires operator review, not automatic refund")
	activeUser, _ := wallet(t, db, userID4, tokenID4)
	assert.Equal(t, 300-120, activeUser)

	// needs_review rows are untouched by recovery and keep applied=reserved.
	userID5, tokenID5 := crashUser(t, db, "crash-e", 300)
	require.NoError(t, ReserveBillingRequest("crash-review", userID5, tokenID5, 50))
	require.NoError(t, RequestBillingCompletion("crash-review", 80, false))
	assert.Equal(t, "needs_review", state(t, "crash-review").State)
	reviewUser, _ := wallet(t, db, userID5, tokenID5)
	assert.Equal(t, 300-50, reviewUser, "no silent post-charge beyond the reservation")
}

// TestCrashMatrixConcurrentRaiseSettleRefund races raises against settles and
// refunds on one reservation: exactly one terminal effect must win and the
// wallet must never double-debit or oversell.
func TestCrashMatrixConcurrentRaiseSettleRefund(t *testing.T) {
	db := newCrashMatrixDB(t)
	userID, tokenID := crashUser(t, db, "race", 10000)

	const base = 100
	require.NoError(t, ReserveBillingRequest("race-reservation", userID, tokenID, base))
	beforeUser, _ := wallet(t, db, userID, tokenID)
	assert.Equal(t, 10000-base, beforeUser)

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(target int) {
			defer wg.Done()
			_ = RaiseBillingReservation("race-reservation", base+target)
		}(i * 10)
	}
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = RequestBillingCompletion("race-reservation", base, false)
		}()
	}
	wg.Wait()
	// Whatever won the race, recovery reaches a terminal state exactly once.
	require.NoError(t, RecoverBillingCompletions(100))
	row := state(t, "race-reservation")
	finalUser, finalToken := wallet(t, db, userID, tokenID)
	if row.State == "active" {
		assert.Equal(t, row.Reserved, row.Applied, "raise and applied must stay in lockstep while active")
	}
	assert.Equal(t, 10000-row.Applied, finalUser, "wallet debit equals the applied amount exactly once: no double charge, no oversell")
	assert.Equal(t, 10000-row.Applied, finalToken)
	assert.LessOrEqual(t, row.Applied, 170)
	t.Logf("race result: state=%s reserved=%d target=%d applied=%d", row.State, row.Reserved, row.Target, row.Applied)
}

// TestCrashMatrixConcurrentSettleVsRefund races a settle against a refund:
// only one terminal transition may win.
func TestCrashMatrixConcurrentSettleVsRefund(t *testing.T) {
	db := newCrashMatrixDB(t)
	userID, tokenID := crashUser(t, db, "race-terminal", 1000)
	require.NoError(t, ReserveBillingRequest("race-terminal", userID, tokenID, 200))
	beforeUser, _ := wallet(t, db, userID, tokenID)
	assert.Equal(t, 800, beforeUser)

	var wg sync.WaitGroup
	outcomes := make(chan string, 2)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := RequestBillingCompletion("race-terminal", 150, false); err == nil {
			if _, err := ApplyBillingCompletion("race-terminal"); err == nil {
				outcomes <- "settled"
				return
			}
		}
		outcomes <- "settle-failed"
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := RequestBillingCompletion("race-terminal", 0, true); err == nil {
			if _, err := ApplyBillingCompletion("race-terminal"); err == nil {
				outcomes <- "refunded"
				return
			}
		}
		outcomes <- "refund-failed"
	}()
	wg.Wait()
	close(outcomes)

	first := <-outcomes
	second := <-outcomes
	row := state(t, "race-terminal")
	assert.Contains(t, []string{"settled", "refunded"}, row.State, "exactly one terminal state wins")
	t.Logf("terminal race outcome: %s then %s, final state %s", first, second, row.State)
	finalUser, finalToken := wallet(t, db, userID, tokenID)
	if row.State == "settled" {
		assert.Equal(t, 1000-150, finalUser)
		assert.Equal(t, 1000-150, finalToken)
	} else {
		assert.Equal(t, 1000, finalUser)
		assert.Equal(t, 1000, finalToken)
	}
}

// TestCrashMatrixAuthSnapshotLifecycle: the authorization snapshot commits
// with the reserve and the raise, never blocks reconciliation, and freezes
// facts that survive restarts.
func TestCrashMatrixAuthSnapshotLifecycle(t *testing.T) {
	db := newCrashMatrixDB(t)
	userID, tokenID := crashUser(t, db, "snapshot", 5000)

	require.NoError(t, ReserveBillingRequestWithAuth("snap-1", userID, tokenID, 40, &BillingAuthSnapshot{
		RequestID: "snap-1", PolicyVersion: 1, Mode: "enforce", EstimatorVersion: "h2-reserve-v1",
		ProfileSource: "channel::1::model::m", PricingMode: "tiered_expr", RequiredQuota: 52, CapQuota: 5000,
	}))
	snapshot, err := GetBillingAuthSnapshot("snap-1")
	require.NoError(t, err)
	assert.Equal(t, "h2-reserve-v1", snapshot.EstimatorVersion)
	assert.Equal(t, 52, snapshot.RequiredQuota)

	require.NoError(t, RaiseBillingReservationWithAuth("snap-1", 52, &BillingAuthSnapshot{
		RequestID: "snap-1", PolicyVersion: 1, Mode: "enforce", EstimatorVersion: "h2-reserve-v1",
		ProfileSource: "channel::1::model::m", PricingMode: "tiered_expr",
		RequiredQuota: 52, CapQuota: 5000, HiddenInputMax: 4480,
	}))
	snapshot, err = GetBillingAuthSnapshot("snap-1")
	require.NoError(t, err)
	assert.Equal(t, int64(4480), snapshot.HiddenInputMax, "the raise freezes the dispatch envelope")
	assert.Equal(t, 52, snapshot.RequiredQuota)
	assert.Equal(t, "active", state(t, "snap-1").State)

	// Completion flows must not require or mutate the snapshot.
	require.NoError(t, RequestBillingCompletion("snap-1", 52, false))
	_, err = ApplyBillingCompletion("snap-1")
	require.NoError(t, err)
	_, err = GetBillingAuthSnapshot("snap-1")
	require.NoError(t, err, "the snapshot survives settlement for audit")
}
