package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupStoreBatchTest(t *testing.T) *User {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&User{}, &Redemption{}, &RedemptionBatch{}, &VIPLevel{}, &BalanceTransaction{}, &OfflineOrder{}, &OfflineOrderCode{}))
	cleanupStoreRedeemTables(t)
	t.Cleanup(func() {
		DB.Exec("DELETE FROM offline_order_codes")
		DB.Exec("DELETE FROM offline_orders")
	})
	previousFlag := operation_setting.StoreRechargeEnabled
	previousPrice := operation_setting.Price
	operation_setting.StoreRechargeEnabled = true
	operation_setting.Price = 7.3
	t.Cleanup(func() {
		operation_setting.StoreRechargeEnabled = previousFlag
		operation_setting.Price = previousPrice
	})
	return createStoreTestUser(t, "store-batch-admin", 0, 0)
}

func TestCreateRedemptionBatchFreezesQuotaAtCreationPrice(t *testing.T) {
	admin := setupStoreBatchTest(t)

	batch, keys, err := CreateRedemptionBatch(CreateRedemptionBatchInput{
		Name:           "20261003-100YUAN",
		FaceValueCents: 10000,
		Count:          3,
		Note:           "第三方商城 ¥100 库存",
		AdminUserID:    admin.Id,
	})
	require.NoError(t, err)
	// ¥100 ÷ 7.3 × 500000 = 6849315.06... 向下取整。
	assert.Equal(t, 6849315, batch.Quota)
	assert.Equal(t, int64(10000), batch.FaceValueCents)
	assert.Equal(t, 3, batch.Count)
	assert.Equal(t, RedemptionBatchStatusEnabled, batch.Status)
	assert.Equal(t, RedemptionSourceStore, batch.Source)
	require.Len(t, keys, 3)

	var codes []Redemption
	require.NoError(t, DB.Where("batch_id = ?", batch.Id).Order("id asc").Find(&codes).Error)
	require.Len(t, codes, 3)
	for index, code := range codes {
		assert.Equal(t, keys[index], code.Key)
		assert.Equal(t, 6849315, code.Quota)
		assert.Equal(t, int64(10000), code.FaceValueCents)
		assert.Equal(t, RedemptionTypeBalance, code.Type)
		assert.Equal(t, RedemptionSourceStore, code.Source)
		assert.Equal(t, common.RedemptionCodeStatusEnabled, code.Status)
	}
}

func TestCreateRedemptionBatchValidation(t *testing.T) {
	admin := setupStoreBatchTest(t)

	cases := []struct {
		name   string
		mutate func(*CreateRedemptionBatchInput)
	}{
		{"zero face value", func(i *CreateRedemptionBatchInput) { i.FaceValueCents = 0 }},
		{"negative face value", func(i *CreateRedemptionBatchInput) { i.FaceValueCents = -1 }},
		{"face value too large", func(i *CreateRedemptionBatchInput) { i.FaceValueCents = maxRedemptionBatchFaceValueCents + 1 }},
		{"zero count", func(i *CreateRedemptionBatchInput) { i.Count = 0 }},
		{"count over limit", func(i *CreateRedemptionBatchInput) { i.Count = maxRedemptionBatchCount + 1 }},
		{"empty name", func(i *CreateRedemptionBatchInput) { i.Name = "" }},
		{"past expiry", func(i *CreateRedemptionBatchInput) { i.ExpiredTime = common.GetTimestamp() - 10 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := CreateRedemptionBatchInput{Name: "batch-validate", FaceValueCents: 10000, Count: 1, AdminUserID: admin.Id}
			tc.mutate(&input)
			_, _, err := CreateRedemptionBatch(input)
			require.Error(t, err)
		})
	}

	// Price 未配置或非正时拒绝创建。
	operation_setting.Price = 0
	_, _, err := CreateRedemptionBatch(CreateRedemptionBatchInput{Name: "batch-no-price", FaceValueCents: 10000, Count: 1, AdminUserID: admin.Id})
	require.Error(t, err)
}

func TestGetRedemptionBatchesStatsAreRealtime(t *testing.T) {
	admin := setupStoreBatchTest(t)
	batch, keys, err := CreateRedemptionBatch(CreateRedemptionBatchInput{Name: "stats-batch", FaceValueCents: 10000, Count: 3, AdminUserID: admin.Id})
	require.NoError(t, err)

	user := createStoreTestUser(t, "store-stats-user", 0, 0)
	_, err = RedeemStoreCode(keys[0], user.Id)
	require.NoError(t, err)

	// 单独禁用一个码，再禁用剩余：验证计数独立于批次禁用。
	var first Redemption
	require.NoError(t, DB.First(&first, "key = ?", keys[1]).Error)
	require.NoError(t, DB.Model(&Redemption{}).Where("id = ?", first.Id).Update("status", common.RedemptionCodeStatusDisabled).Error)

	batches, total, err := GetRedemptionBatches(0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, batches, 1)
	assert.Equal(t, batch.Id, batches[0].Id)
	assert.EqualValues(t, 1, batches[0].UsedCount)
	assert.EqualValues(t, 1, batches[0].UnusedCount)
	assert.EqualValues(t, 1, batches[0].DisabledCount)

	disabled, err := DisableRedemptionBatchById(batch.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 1, disabled)

	batches, _, err = GetRedemptionBatches(0, 10)
	require.NoError(t, err)
	require.Len(t, batches, 1)
	assert.Equal(t, RedemptionBatchStatusDisabled, batches[0].Status)
	assert.EqualValues(t, 1, batches[0].UsedCount)
	assert.EqualValues(t, 0, batches[0].UnusedCount)
	assert.EqualValues(t, 2, batches[0].DisabledCount)
}

func TestDisableRedemptionBatchKeepsUsedCodeAndIsIdempotent(t *testing.T) {
	admin := setupStoreBatchTest(t)
	batch, keys, err := CreateRedemptionBatch(CreateRedemptionBatchInput{Name: "disable-batch", FaceValueCents: 10000, Count: 3, AdminUserID: admin.Id})
	require.NoError(t, err)

	user := createStoreTestUser(t, "store-disable-user", 0, 0)
	_, err = RedeemStoreCode(keys[0], user.Id)
	require.NoError(t, err)

	disabled, err := DisableRedemptionBatchById(batch.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 2, disabled)

	var used Redemption
	require.NoError(t, DB.First(&used, "key = ?", keys[0]).Error)
	assert.Equal(t, common.RedemptionCodeStatusUsed, used.Status)
	for _, key := range keys[1:] {
		var code Redemption
		require.NoError(t, DB.First(&code, "key = ?", key).Error)
		assert.Equal(t, common.RedemptionCodeStatusDisabled, code.Status)
	}

	// 重复禁用幂等成功，返回 0。
	disabled, err = DisableRedemptionBatchById(batch.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 0, disabled)

	// 已禁用码不可再兑换。
	_, err = RedeemStoreCode(keys[1], user.Id)
	require.Error(t, err)
}

func TestGetUnusedRedemptionBatchKeysDefaultsToUnused(t *testing.T) {
	admin := setupStoreBatchTest(t)
	batch, keys, err := CreateRedemptionBatch(CreateRedemptionBatchInput{Name: "export-batch", FaceValueCents: 10000, Count: 3, AdminUserID: admin.Id})
	require.NoError(t, err)

	exported, err := GetUnusedRedemptionBatchKeys(batch.Id)
	require.NoError(t, err)
	assert.Equal(t, keys, exported)

	user := createStoreTestUser(t, "store-export-user", 0, 0)
	_, err = RedeemStoreCode(keys[0], user.Id)
	require.NoError(t, err)

	exported, err = GetUnusedRedemptionBatchKeys(batch.Id)
	require.NoError(t, err)
	require.Len(t, exported, 2)
	assert.NotContains(t, exported, keys[0])

	_, err = GetUnusedRedemptionBatchKeys(999999)
	require.Error(t, err)
}

func TestBackfillOfflineOrderRedemptionSourcesIdempotent(t *testing.T) {
	admin := setupStoreBatchTest(t)
	now := common.GetTimestamp()
	order, keys, err := CreateOfflineOrder(CreateOfflineOrderInput{
		OrderNo: "backfill-test", MoneyCents: 10000, QuotaPerCode: 500000,
		CodeCount: 2, PaidAt: now, AdminUserID: admin.Id,
	})
	require.NoError(t, err)
	require.Len(t, keys, 2)

	// 新建离线码已显式标记。
	var linked []Redemption
	require.NoError(t, DB.Where("id IN (SELECT redemption_id FROM offline_order_codes WHERE order_id = ?)", order.Id).Find(&linked).Error)
	require.Len(t, linked, 2)
	for _, code := range linked {
		assert.Equal(t, RedemptionSourceOfflineOrder, code.Source)
	}

	// 模拟历史数据：回填为 LEGACY 后执行两次 backfill，结果幂等。
	require.NoError(t, DB.Model(&Redemption{}).Where("id IN ?", []int{linked[0].Id, linked[1].Id}).Update("source", RedemptionSourceLegacy).Error)
	legacyCode := createStoreTestCode(t, "legacy-code-000000000000000000000001", 1000000, 0, RedemptionSourceLegacy, RedemptionTypeBalance, common.RedemptionCodeStatusEnabled, 0)

	require.NoError(t, backfillOfflineOrderRedemptionSources())
	require.NoError(t, backfillOfflineOrderRedemptionSources())

	var firstLinked Redemption
	require.NoError(t, DB.First(&firstLinked, "id = ?", linked[0].Id).Error)
	assert.Equal(t, RedemptionSourceOfflineOrder, firstLinked.Source)
	var secondLinked Redemption
	require.NoError(t, DB.First(&secondLinked, "id = ?", linked[1].Id).Error)
	assert.Equal(t, RedemptionSourceOfflineOrder, secondLinked.Source)
	var legacyReloaded Redemption
	require.NoError(t, DB.First(&legacyReloaded, "id = ?", legacyCode.Id).Error)
	assert.Equal(t, RedemptionSourceLegacy, legacyReloaded.Source)
}
