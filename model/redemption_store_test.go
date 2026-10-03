package model

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cleanupStoreRedeemTables(t *testing.T) {
	t.Cleanup(func() {
		DB.Exec("DELETE FROM balance_transactions")
		DB.Exec("DELETE FROM redemptions")
		DB.Exec("DELETE FROM redemption_batches")
		DB.Exec("DELETE FROM vip_levels")
		DB.Exec("DELETE FROM users")
	})
}

func setupStoreRedeemTest(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&User{}, &Redemption{}, &RedemptionBatch{}, &VIPLevel{}, &BalanceTransaction{}))
	cleanupStoreRedeemTables(t)
	previousFlag := operation_setting.StoreRechargeEnabled
	operation_setting.StoreRechargeEnabled = true
	t.Cleanup(func() { operation_setting.StoreRechargeEnabled = previousFlag })
}

func createStoreTestUser(t *testing.T, username string, quota int, totalRechargeCents int64) *User {
	t.Helper()
	user := &User{
		Username:           username,
		Password:           "store-test-password",
		Role:               common.RoleCommonUser,
		Status:             common.UserStatusEnabled,
		Group:              "default",
		AffCode:            username,
		Quota:              quota,
		TotalRechargeCents: totalRechargeCents,
	}
	require.NoError(t, DB.Create(user).Error)
	return user
}

func createStoreTestCode(t *testing.T, key string, quota int, faceValueCents int64, source string, codeType string, status int, expiredTime int64) *Redemption {
	t.Helper()
	code := &Redemption{
		Name:           "store-test",
		Key:            key,
		Status:         status,
		Quota:          quota,
		CreatedTime:    common.GetTimestamp(),
		ExpiredTime:    expiredTime,
		BatchId:        0,
		FaceValueCents: faceValueCents,
		Type:           codeType,
		Source:         source,
	}
	require.NoError(t, DB.Create(code).Error)
	return code
}

func seedStoreVIPLevels(t *testing.T) {
	t.Helper()
	now := common.GetTimestamp()
	require.NoError(t, DB.Create(&[]VIPLevel{
		{Name: "普通用户", ThresholdCents: 0, BonusBasisPoints: 0, Enabled: true, Sort: 0, CreatedTime: now, UpdatedTime: now},
		{Name: "VIP1", ThresholdCents: 50000, BonusBasisPoints: 200, Enabled: true, Sort: 1, CreatedTime: now, UpdatedTime: now},
		{Name: "VIP2", ThresholdCents: 200000, BonusBasisPoints: 400, Enabled: true, Sort: 2, CreatedTime: now, UpdatedTime: now},
	}).Error)
}

func countStoreBalanceTx(t *testing.T, userId int) int64 {
	t.Helper()
	var count int64
	require.NoError(t, DB.Model(&BalanceTransaction{}).Where("user_id = ?", userId).Count(&count).Error)
	return count
}

func reloadStoreUser(t *testing.T, userId int) *User {
	t.Helper()
	user := &User{}
	require.NoError(t, DB.First(user, "id = ?", userId).Error)
	return user
}

func TestRedeemStoreCodeSuccessWithoutVIP(t *testing.T) {
	setupStoreRedeemTest(t)
	seedStoreVIPLevels(t)
	user := createStoreTestUser(t, "store-user-1", 0, 0)
	code := createStoreTestCode(t, "store-code-0000000000000000000000000001", 1000000, 10000, RedemptionSourceStore, RedemptionTypeBalance, common.RedemptionCodeStatusEnabled, 0)

	result, err := RedeemStoreCode(code.Key, user.Id)
	require.NoError(t, err)
	assert.Equal(t, 1000000, result.BaseQuota)
	assert.Equal(t, 0, result.BonusQuota)
	assert.Equal(t, 1000000, result.TotalQuota)
	assert.Equal(t, 1000000, result.Balance)
	assert.Equal(t, int64(10000), result.FaceValueCents)
	assert.Equal(t, "普通用户", result.VIPName)
	assert.Equal(t, 0, result.VIPBonusBasisPoints)
	assert.Equal(t, "普通用户", result.NewVIPName)

	reloaded := reloadStoreUser(t, user.Id)
	assert.Equal(t, 1000000, reloaded.Quota)
	assert.Equal(t, int64(10000), reloaded.TotalRechargeCents)

	var used Redemption
	require.NoError(t, DB.First(&used, "id = ?", code.Id).Error)
	assert.Equal(t, common.RedemptionCodeStatusUsed, used.Status)
	assert.Equal(t, user.Id, used.UsedUserId)
	assert.NotZero(t, used.RedeemedTime)

	var entries []BalanceTransaction
	require.NoError(t, DB.Where("user_id = ?", user.Id).Order("id asc").Find(&entries).Error)
	require.Len(t, entries, 1)
	assert.Equal(t, BalanceTxTypeRecharge, entries[0].Type)
	assert.Equal(t, 1000000, entries[0].AmountQuota)
	assert.Equal(t, 0, entries[0].BalanceBefore)
	assert.Equal(t, 1000000, entries[0].BalanceAfter)
	assert.Equal(t, int64(10000), entries[0].RechargeCents)
	assert.Equal(t, BalanceTxReferenceRedemption, entries[0].ReferenceType)
	assert.Equal(t, code.Id, entries[0].ReferenceId)
	assert.Equal(t, code.Key[len(code.Key)-4:], BalanceTransactionCodeTail(entries[0].Metadata))
	assert.NotContains(t, entries[0].Metadata, code.Key)
}

func TestRedeemStoreCodeUsesRechargeLevelAndBonusNotCounted(t *testing.T) {
	setupStoreRedeemTest(t)
	seedStoreVIPLevels(t)
	// ¥600 已达 VIP1（2%），本次充值使用充值前等级。
	user := createStoreTestUser(t, "store-user-vip", 0, 60000)
	code := createStoreTestCode(t, "store-code-0000000000000000000000000002", 1000000, 10000, RedemptionSourceStore, RedemptionTypeBalance, common.RedemptionCodeStatusEnabled, 0)

	result, err := RedeemStoreCode(code.Key, user.Id)
	require.NoError(t, err)
	assert.Equal(t, "VIP1", result.VIPName)
	assert.Equal(t, 200, result.VIPBonusBasisPoints)
	assert.Equal(t, 20000, result.BonusQuota)
	assert.Equal(t, 1020000, result.Balance)
	// 累计充值只加面额 ¥100，不含赠送。
	assert.Equal(t, int64(70000), reloadStoreUser(t, user.Id).TotalRechargeCents)
	// 升级判断：70000 < 200000，仍是 VIP1。
	assert.Equal(t, "VIP1", result.NewVIPName)

	var entries []BalanceTransaction
	require.NoError(t, DB.Where("user_id = ?", user.Id).Order("id asc").Find(&entries).Error)
	require.Len(t, entries, 2)
	assert.Equal(t, BalanceTxTypeRecharge, entries[0].Type)
	assert.Equal(t, 0, entries[0].BalanceBefore)
	assert.Equal(t, 1000000, entries[0].BalanceAfter)
	assert.Equal(t, BalanceTxTypeBonus, entries[1].Type)
	assert.Equal(t, 20000, entries[1].AmountQuota)
	assert.Equal(t, 1000000, entries[1].BalanceBefore)
	assert.Equal(t, 1020000, entries[1].BalanceAfter)
	assert.Equal(t, int64(0), entries[1].RechargeCents)
}

func TestRedeemStoreCodeCrossThresholdUsesOldLevelNextTimeNewLevel(t *testing.T) {
	setupStoreRedeemTest(t)
	seedStoreVIPLevels(t)
	// ¥1900 → VIP1 2%；充值 ¥100 跨过 ¥2000 门槛，本笔仍 2%。
	user := createStoreTestUser(t, "store-user-cross", 0, 190000)
	code := createStoreTestCode(t, "store-code-0000000000000000000000000003", 1000000, 10000, RedemptionSourceStore, RedemptionTypeBalance, common.RedemptionCodeStatusEnabled, 0)

	result, err := RedeemStoreCode(code.Key, user.Id)
	require.NoError(t, err)
	assert.Equal(t, "VIP1", result.VIPName)
	assert.Equal(t, 20000, result.BonusQuota)
	assert.Equal(t, int64(200000), reloadStoreUser(t, user.Id).TotalRechargeCents)
	assert.Equal(t, "VIP2", result.NewVIPName)

	// 下一笔开始使用新等级 4%。
	code2 := createStoreTestCode(t, "store-code-0000000000000000000000000004", 1000000, 10000, RedemptionSourceStore, RedemptionTypeBalance, common.RedemptionCodeStatusEnabled, 0)
	result2, err := RedeemStoreCode(code2.Key, user.Id)
	require.NoError(t, err)
	assert.Equal(t, "VIP2", result2.VIPName)
	assert.Equal(t, 400, result2.VIPBonusBasisPoints)
	assert.Equal(t, 40000, result2.BonusQuota)
	assert.Equal(t, int64(210000), reloadStoreUser(t, user.Id).TotalRechargeCents)
}

func TestRedeemStoreCodeDuplicateFails(t *testing.T) {
	setupStoreRedeemTest(t)
	user := createStoreTestUser(t, "store-user-dup", 0, 0)
	code := createStoreTestCode(t, "store-code-0000000000000000000000000005", 1000000, 10000, RedemptionSourceStore, RedemptionTypeBalance, common.RedemptionCodeStatusEnabled, 0)

	_, err := RedeemStoreCode(code.Key, user.Id)
	require.NoError(t, err)
	_, err = RedeemStoreCode(code.Key, user.Id)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrRedeemFailed))
	assert.Equal(t, 1000000, reloadStoreUser(t, user.Id).Quota)
	assert.Equal(t, int64(1), countStoreBalanceTx(t, user.Id))
}

func TestRedeemStoreCodeConcurrentSameCodeSingleSuccess(t *testing.T) {
	setupStoreRedeemTest(t)
	user := createStoreTestUser(t, "store-user-conc", 0, 0)
	code := createStoreTestCode(t, "store-code-0000000000000000000000000006", 1000000, 10000, RedemptionSourceStore, RedemptionTypeBalance, common.RedemptionCodeStatusEnabled, 0)

	const goroutines = 10
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := RedeemStoreCode(code.Key, user.Id)
			if err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, 1, successes)
	assert.Equal(t, 1000000, reloadStoreUser(t, user.Id).Quota)
	assert.Equal(t, int64(10000), reloadStoreUser(t, user.Id).TotalRechargeCents)
	assert.Equal(t, int64(1), countStoreBalanceTx(t, user.Id))
}

func TestRedeemStoreCodeTwoUsersRaceSameCode(t *testing.T) {
	setupStoreRedeemTest(t)
	userA := createStoreTestUser(t, "store-user-race-a", 0, 0)
	userB := createStoreTestUser(t, "store-user-race-b", 0, 0)
	code := createStoreTestCode(t, "store-code-0000000000000000000000000007", 1000000, 10000, RedemptionSourceStore, RedemptionTypeBalance, common.RedemptionCodeStatusEnabled, 0)

	var wg sync.WaitGroup
	var mu sync.Mutex
	winners := make([]int, 0, 2)
	for _, candidate := range []*User{userA, userB} {
		wg.Add(1)
		go func(userId int) {
			defer wg.Done()
			if _, err := RedeemStoreCode(code.Key, userId); err == nil {
				mu.Lock()
				winners = append(winners, userId)
				mu.Unlock()
			}
		}(candidate.Id)
	}
	wg.Wait()
	require.Len(t, winners, 1)
	winner := reloadStoreUser(t, winners[0])
	assert.Equal(t, 1000000, winner.Quota)
	assert.Equal(t, int64(10000), winner.TotalRechargeCents)
	assert.Equal(t, int64(1), countStoreBalanceTx(t, winners[0]))
}

func TestRedeemStoreCodeSequentialDifferentCodesBothCredited(t *testing.T) {
	setupStoreRedeemTest(t)
	seedStoreVIPLevels(t)
	user := createStoreTestUser(t, "store-user-two", 0, 0)
	codeA := createStoreTestCode(t, "store-code-0000000000000000000000000008", 1000000, 60000, RedemptionSourceStore, RedemptionTypeBalance, common.RedemptionCodeStatusEnabled, 0)
	codeB := createStoreTestCode(t, "store-code-0000000000000000000000000009", 1000000, 10000, RedemptionSourceStore, RedemptionTypeBalance, common.RedemptionCodeStatusEnabled, 0)

	resultA, err := RedeemStoreCode(codeA.Key, user.Id)
	require.NoError(t, err)
	assert.Equal(t, 0, resultA.BonusQuota)

	resultB, err := RedeemStoreCode(codeB.Key, user.Id)
	require.NoError(t, err)
	assert.Equal(t, 20000, resultB.BonusQuota)

	reloaded := reloadStoreUser(t, user.Id)
	assert.Equal(t, 2020000, reloaded.Quota)
	assert.Equal(t, int64(70000), reloaded.TotalRechargeCents)
	assert.Equal(t, int64(3), countStoreBalanceTx(t, user.Id))
}

func TestRedeemStoreCodeRejectsInvalidStates(t *testing.T) {
	setupStoreRedeemTest(t)
	user := createStoreTestUser(t, "store-user-invalid", 0, 0)
	now := common.GetTimestamp()
	cases := []struct {
		name   string
		key    string
		quota  int
		face   int64
		source string
		ctype  string
		status int
		expire int64
	}{
		{"expired", "store-code-expired0000000000000000000001", 1000000, 10000, RedemptionSourceStore, RedemptionTypeBalance, common.RedemptionCodeStatusEnabled, now - 10},
		{"disabled", "store-code-disabled000000000000000000001", 1000000, 10000, RedemptionSourceStore, RedemptionTypeBalance, common.RedemptionCodeStatusDisabled, 0},
		{"used", "store-code-used000000000000000000000001", 1000000, 10000, RedemptionSourceStore, RedemptionTypeBalance, common.RedemptionCodeStatusUsed, 0},
		{"legacy-source", "store-code-legacy0000000000000000000001", 1000000, 10000, RedemptionSourceLegacy, RedemptionTypeBalance, common.RedemptionCodeStatusEnabled, 0},
		{"offline-source", "store-code-offline000000000000000000001", 1000000, 10000, RedemptionSourceOfflineOrder, RedemptionTypeBalance, common.RedemptionCodeStatusEnabled, 0},
		{"admin-source", "store-code-admin00000000000000000000001", 1000000, 10000, RedemptionSourceAdmin, RedemptionTypeBalance, common.RedemptionCodeStatusEnabled, 0},
		{"subscription-type", "store-code-sub000000000000000000000001", 1000000, 10000, RedemptionSourceStore, RedemptionTypeSubscription, common.RedemptionCodeStatusEnabled, 0},
		{"zero-face-value", "store-code-zeroface000000000000000000001", 1000000, 0, RedemptionSourceStore, RedemptionTypeBalance, common.RedemptionCodeStatusEnabled, 0},
		{"zero-quota", "store-code-zeroquota000000000000000000001", -1, 10000, RedemptionSourceStore, RedemptionTypeBalance, common.RedemptionCodeStatusEnabled, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code := createStoreTestCode(t, tc.key, tc.quota, tc.face, tc.source, tc.ctype, tc.status, tc.expire)
			_, err := RedeemStoreCode(code.Key, user.Id)
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrRedeemFailed), "expect generic redeem failure, got %v", err)
		})
	}
	// 全部失败后用户余额与账本不变。
	assert.Equal(t, 0, reloadStoreUser(t, user.Id).Quota)
	assert.Equal(t, int64(0), countStoreBalanceTx(t, user.Id))
	assert.Equal(t, int64(0), reloadStoreUser(t, user.Id).TotalRechargeCents)
}

func TestRedeemStoreCodeUnknownCodeFails(t *testing.T) {
	setupStoreRedeemTest(t)
	user := createStoreTestUser(t, "store-user-unknown", 0, 0)
	_, err := RedeemStoreCode("nonexistent-code-000000000000000000001", user.Id)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrRedeemFailed))
}

func TestRedeemStoreCodeWalletCeilingRollsBack(t *testing.T) {
	setupStoreRedeemTest(t)
	user := createStoreTestUser(t, "store-user-ceiling", common.MaxWalletQuota, 0)
	code := createStoreTestCode(t, "store-code-ceiling00000000000000000001", 1000000, 10000, RedemptionSourceStore, RedemptionTypeBalance, common.RedemptionCodeStatusEnabled, 0)

	_, err := RedeemStoreCode(code.Key, user.Id)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrRedeemFailed))

	// 整体回滚：码仍 Enabled、余额/累计充值/账本均不变。
	var stored Redemption
	require.NoError(t, DB.First(&stored, "id = ?", code.Id).Error)
	assert.Equal(t, common.RedemptionCodeStatusEnabled, stored.Status)
	assert.Equal(t, common.MaxWalletQuota, reloadStoreUser(t, user.Id).Quota)
	assert.Equal(t, int64(0), reloadStoreUser(t, user.Id).TotalRechargeCents)
	assert.Equal(t, int64(0), countStoreBalanceTx(t, user.Id))
}

func TestRedeemStoreCodeDisabledFlag(t *testing.T) {
	setupStoreRedeemTest(t)
	operation_setting.StoreRechargeEnabled = false
	t.Cleanup(func() { operation_setting.StoreRechargeEnabled = true })
	user := createStoreTestUser(t, "store-user-flagoff", 0, 0)
	code := createStoreTestCode(t, "store-code-flagoff000000000000000000001", 1000000, 10000, RedemptionSourceStore, RedemptionTypeBalance, common.RedemptionCodeStatusEnabled, 0)

	_, err := RedeemStoreCode(code.Key, user.Id)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrStoreRechargeDisabled))

	var stored Redemption
	require.NoError(t, DB.First(&stored, "id = ?", code.Id).Error)
	assert.Equal(t, common.RedemptionCodeStatusEnabled, stored.Status)
	assert.Equal(t, 0, reloadStoreUser(t, user.Id).Quota)
}

func TestRedeemStoreCodeEmptyKey(t *testing.T) {
	setupStoreRedeemTest(t)
	user := createStoreTestUser(t, "store-user-empty", 0, 0)
	_, err := RedeemStoreCode("", user.Id)
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrRedeemFailed))
	_, err = RedeemStoreCode(fmt.Sprintf("%032d", 1), 0)
	require.Error(t, err)
}
