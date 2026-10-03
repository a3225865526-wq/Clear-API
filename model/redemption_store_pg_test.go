package model

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// PostgreSQL acceptance tests for the store redeem money path (STEP 5).
// Enabled only when TEST_STORE_PG_DSN is set; skipped otherwise so the
// default SQLite suite is unaffected.
func setupStorePGTest(t *testing.T) {
	t.Helper()
	dsn := common.GetEnvOrDefaultString("TEST_STORE_PG_DSN", "")
	if dsn == "" {
		t.Skip("TEST_STORE_PG_DSN is not configured")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	require.NoError(t, db.AutoMigrate(
		&User{}, &Redemption{}, &RedemptionBatch{}, &VIPLevel{}, &BalanceTransaction{}, &Log{},
	))
	for _, table := range []string{
		"balance_transactions", "redemptions", "redemption_batches", "vip_levels", "users",
	} {
		require.NoError(t, db.Exec("DELETE FROM "+table).Error)
	}

	previousDB, previousLogDB := DB, LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis := common.RedisEnabled
	previousFlag := operation_setting.StoreRechargeEnabled
	previousPrice := operation_setting.Price
	DB, LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypePostgreSQL, common.DatabaseTypePostgreSQL)
	common.RedisEnabled = false
	operation_setting.StoreRechargeEnabled = true
	operation_setting.Price = 7.3
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		common.RedisEnabled = previousRedis
		operation_setting.StoreRechargeEnabled = previousFlag
		operation_setting.Price = previousPrice
	})

	now := common.GetTimestamp()
	require.NoError(t, DB.Create(&[]VIPLevel{
		{Name: "Base", ThresholdCents: 0, BonusBasisPoints: 0, Enabled: true, Sort: 0, CreatedTime: now, UpdatedTime: now},
		{Name: "TestVIP1", ThresholdCents: 50000, BonusBasisPoints: 200, Enabled: true, Sort: 1, CreatedTime: now, UpdatedTime: now},
	}).Error)
}

func pgStoreUser(t *testing.T, username string, quota int, total int64) *User {
	t.Helper()
	user := &User{
		Username: username, Password: "pg-store-test", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AffCode: username,
		Quota: quota, TotalRechargeCents: total,
	}
	require.NoError(t, DB.Create(user).Error)
	return user
}

func pgStoreCode(t *testing.T, key string, quota int, face int64) *Redemption {
	t.Helper()
	code := &Redemption{
		Name: "pg-store", Key: key, Status: common.RedemptionCodeStatusEnabled,
		Quota: quota, CreatedTime: common.GetTimestamp(),
		FaceValueCents: face, Type: RedemptionTypeBalance, Source: RedemptionSourceStore,
	}
	require.NoError(t, DB.Create(code).Error)
	return code
}

func TestStoreRedeemPGConcurrentSameCodeSingleSuccess(t *testing.T) {
	setupStorePGTest(t)
	user := pgStoreUser(t, "pg-conc-user", 0, 0)
	code := pgStoreCode(t, "pgconcurrentcode0000000000000001", 1000000, 10000)

	const goroutines = 10
	var mu sync.Mutex
	successes := 0
	var wg sync.WaitGroup
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := RedeemStoreCode(code.Key, user.Id); err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, 1, successes)
	reloaded := &User{}
	require.NoError(t, DB.First(reloaded, "id = ?", user.Id).Error)
	assert.Equal(t, 1000000, reloaded.Quota)
	assert.EqualValues(t, 10000, reloaded.TotalRechargeCents)

	var used Redemption
	require.NoError(t, DB.First(&used, "id = ?", code.Id).Error)
	assert.Equal(t, common.RedemptionCodeStatusUsed, used.Status)
	assert.Equal(t, user.Id, used.UsedUserId)

	var entries []BalanceTransaction
	require.NoError(t, DB.Where("user_id = ?", user.Id).Find(&entries).Error)
	require.Len(t, entries, 1)
	assert.Equal(t, BalanceTxTypeRecharge, entries[0].Type)
	assert.EqualValues(t, 10000, entries[0].RechargeCents)
}

func TestStoreRedeemPGSameUserTwoCodesNoLostUpdate(t *testing.T) {
	setupStorePGTest(t)
	user := pgStoreUser(t, "pg-two-codes", 0, 0)
	codeA := pgStoreCode(t, "pgtwocodesa0000000000000000001", 1000000, 60000)
	codeB := pgStoreCode(t, "pgtwocodesb0000000000000000001", 1000000, 10000)

	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for _, key := range []string{codeA.Key, codeB.Key} {
		wg.Add(1)
		go func(k string) {
			defer wg.Done()
			if _, err := RedeemStoreCode(k, user.Id); err != nil {
				errCh <- err
			}
		}(key)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("both codes must succeed, got error: %v", err)
	}

	reloaded := &User{}
	require.NoError(t, DB.First(reloaded, "id = ?", user.Id).Error)
	// Under true concurrency the user row lock serializes the two
	// transactions; each determines its VIP bonus from the cumulative value
	// it reads when it acquires the lock (STEP 2 decision: pre-recharge
	// level). Which code lands second decides the bonus, so assert the
	// invariants instead of a specific ordering.
	assert.Contains(t, []int{2000000, 2020000}, reloaded.Quota)
	assert.EqualValues(t, 70000, reloaded.TotalRechargeCents)

	var entries []BalanceTransaction
	require.NoError(t, DB.Where("user_id = ?", user.Id).Order("id asc").Find(&entries).Error)
	// 2 RECHARGE + at most 1 BONUS, with a continuous before/after chain.
	balance := 0
	bonusCount := 0
	for _, entry := range entries {
		assert.Equal(t, balance, entry.BalanceBefore, "ledger before/after chain must stay continuous")
		balance = entry.BalanceAfter
		if entry.Type == BalanceTxTypeBonus {
			bonusCount++
			assert.Equal(t, 20000, entry.AmountQuota)
			assert.EqualValues(t, 0, entry.RechargeCents)
		}
	}
	assert.Contains(t, []int{2, 3}, len(entries))
	assert.LessOrEqual(t, bonusCount, 1)
	assert.Equal(t, reloaded.Quota, balance)
}
