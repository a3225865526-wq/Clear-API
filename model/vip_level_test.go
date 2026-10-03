package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupVIPLevelTest(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&VIPLevel{}))
	t.Cleanup(func() { DB.Exec("DELETE FROM vip_levels") })
}

func TestSeedDefaultVIPLevelsIdempotent(t *testing.T) {
	setupVIPLevelTest(t)

	require.NoError(t, seedDefaultVIPLevels())
	require.NoError(t, seedDefaultVIPLevels())

	var levels []VIPLevel
	require.NoError(t, DB.Order("sort asc").Find(&levels).Error)
	require.Len(t, levels, 5)
	assert.Equal(t, "普通用户", levels[0].Name)
	assert.EqualValues(t, 0, levels[0].ThresholdCents)
	assert.EqualValues(t, 0, levels[0].BonusBasisPoints)
	assert.Equal(t, "VIP1", levels[1].Name)
	assert.EqualValues(t, 50000, levels[1].ThresholdCents)
	assert.EqualValues(t, 200, levels[1].BonusBasisPoints)
	assert.Equal(t, "VIP2", levels[2].Name)
	assert.EqualValues(t, 200000, levels[2].ThresholdCents)
	assert.Equal(t, "VIP3", levels[3].Name)
	assert.EqualValues(t, 500000, levels[3].ThresholdCents)
	assert.Equal(t, "VIP4", levels[4].Name)
	assert.EqualValues(t, 1000000, levels[4].ThresholdCents)
	assert.EqualValues(t, 800, levels[4].BonusBasisPoints)
}

func TestSeedDefaultVIPLevelsSkipsWhenTableHasRows(t *testing.T) {
	setupVIPLevelTest(t)
	now := common.GetTimestamp()
	require.NoError(t, DB.Create(&VIPLevel{Name: "自定义", ThresholdCents: 12345, BonusBasisPoints: 111, Enabled: true, CreatedTime: now, UpdatedTime: now}).Error)

	require.NoError(t, seedDefaultVIPLevels())

	var count int64
	require.NoError(t, DB.Model(&VIPLevel{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestGetVIPLevelForRechargeSelection(t *testing.T) {
	setupVIPLevelTest(t)
	now := common.GetTimestamp()
	levels := []VIPLevel{
		{Name: "普通用户", ThresholdCents: 0, BonusBasisPoints: 0, Enabled: true, Sort: 0, CreatedTime: now, UpdatedTime: now},
		{Name: "VIP1", ThresholdCents: 50000, BonusBasisPoints: 200, Enabled: true, Sort: 1, CreatedTime: now, UpdatedTime: now},
		{Name: "VIP2", ThresholdCents: 200000, BonusBasisPoints: 400, Enabled: true, Sort: 2, CreatedTime: now, UpdatedTime: now},
		{Name: "VIP_CLOSED", ThresholdCents: 500000, BonusBasisPoints: 900, Enabled: false, Sort: 3, CreatedTime: now, UpdatedTime: now},
	}
	require.NoError(t, DB.Create(&levels).Error)

	cases := []struct {
		totalCents int64
		found      bool
		name       string
		bp         int
	}{
		{0, true, "普通用户", 0},
		{49999, true, "普通用户", 0},
		{50000, true, "VIP1", 200},
		{190000, true, "VIP1", 200},
		{200000, true, "VIP2", 400},
		{999999999, true, "VIP2", 400}, // 禁用的高档不参与
	}
	for _, tc := range cases {
		level, ok, err := GetVIPLevelForRecharge(nil, tc.totalCents)
		require.NoError(t, err)
		require.Equal(t, tc.found, ok)
		assert.Equal(t, tc.name, level.Name)
		assert.Equal(t, tc.bp, level.BonusBasisPoints)
	}
}

func TestGetVIPLevelForRechargeNoLevelsFallsBack(t *testing.T) {
	setupVIPLevelTest(t)
	level, ok, err := GetVIPLevelForRecharge(nil, 100000)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, level)
}

func TestGetNextVIPLevel(t *testing.T) {
	setupVIPLevelTest(t)
	now := common.GetTimestamp()
	levels := []VIPLevel{
		{Name: "普通用户", ThresholdCents: 0, BonusBasisPoints: 0, Enabled: true, Sort: 0, CreatedTime: now, UpdatedTime: now},
		{Name: "VIP1", ThresholdCents: 50000, BonusBasisPoints: 200, Enabled: true, Sort: 1, CreatedTime: now, UpdatedTime: now},
		{Name: "VIP2", ThresholdCents: 200000, BonusBasisPoints: 400, Enabled: true, Sort: 2, CreatedTime: now, UpdatedTime: now},
	}
	require.NoError(t, DB.Create(&levels).Error)

	next, err := GetNextVIPLevel(190000)
	require.NoError(t, err)
	require.NotNil(t, next)
	assert.Equal(t, "VIP2", next.Name)
	assert.EqualValues(t, 200000, next.ThresholdCents)

	next, err = GetNextVIPLevel(200000)
	require.NoError(t, err)
	assert.Nil(t, next, "已达最高等级时 next 为 nil")
}

func TestComputeVipBonusQuotaFloors(t *testing.T) {
	cases := []struct {
		base int
		bp   int
		want int
	}{
		{6849315, 200, 136986},  // 136986.3 向下取整
		{1000000, 400, 40000},   // 整除
		{1000000, 0, 0},         // 无等级
		{0, 400, 0},             // 防御
		{33, 300, 0},            // 0.99 向下取整为 0
		{999999, 10000, 999999}, // 100% 加赠
	}
	for _, tc := range cases {
		got, err := computeVipBonusQuota(tc.base, tc.bp)
		require.NoError(t, err)
		assert.Equal(t, tc.want, got, "base=%d bp=%d", tc.base, tc.bp)
	}
}

func TestVIPLevelValidationAndUniqueEnabledThreshold(t *testing.T) {
	setupVIPLevelTest(t)
	now := common.GetTimestamp()
	base := &VIPLevel{Name: "普通用户", ThresholdCents: 0, BonusBasisPoints: 0, Enabled: true, Sort: 0, CreatedTime: now, UpdatedTime: now}
	require.NoError(t, base.Insert())

	duplicate := &VIPLevel{Name: "重复门槛", ThresholdCents: 0, BonusBasisPoints: 100, Enabled: true}
	require.Error(t, duplicate.Insert())

	disabled := &VIPLevel{Name: "禁用同门槛", ThresholdCents: 0, BonusBasisPoints: 100, Enabled: false}
	require.NoError(t, disabled.Insert())

	invalidBp := &VIPLevel{Name: "超高加赠", ThresholdCents: 10000, BonusBasisPoints: 10001, Enabled: true}
	require.Error(t, invalidBp.Insert())

	negativeBp := &VIPLevel{Name: "负加赠", ThresholdCents: 10000, BonusBasisPoints: -1, Enabled: true}
	require.Error(t, negativeBp.Insert())

	emptyName := &VIPLevel{Name: "", ThresholdCents: 10000, BonusBasisPoints: 0, Enabled: true}
	require.Error(t, emptyName.Insert())

	valid := &VIPLevel{Name: "VIP1", ThresholdCents: 50000, BonusBasisPoints: 200, Enabled: true}
	require.NoError(t, valid.Insert())

	valid.Name = "VIP1 改名"
	require.NoError(t, valid.Update())

	require.Error(t, DeleteVIPLevelById(0))
	require.NoError(t, DeleteVIPLevelById(disabled.Id))
	require.Error(t, DeleteVIPLevelById(disabled.Id), "重复删除报不存在")
}
