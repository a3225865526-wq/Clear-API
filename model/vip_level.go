package model

import (
	"errors"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// TableName 显式指定表名：GORM 默认命名策略会把 VIPLevel 拆成 v_ip_levels
// （IP 是其内置缩写词），与预期不符。
func (VIPLevel) TableName() string {
	return "vip_levels"
}

// VIPLevel 以“累计真实充值金额”（CNY 分）决定等级；bonus 以 basis points
// 保存（400 = 4%，10000 = 100%），禁止 float。
type VIPLevel struct {
	Id               int    `json:"id"`
	Name             string `json:"name" gorm:"type:varchar(32)"`
	ThresholdCents   int64  `json:"threshold_cents" gorm:"type:bigint;default:0;index"`
	BonusBasisPoints int    `json:"bonus_basis_points" gorm:"default:0"`
	// Enabled 不设 gorm default：default:true 会让 GORM 在插入零值 false 时
	// 忽略该字段、落到数据库默认值 true，导致“创建即禁用”失效。
	Enabled          bool   `json:"enabled"`
	Sort             int    `json:"sort" gorm:"default:0"`
	CreatedTime      int64  `json:"created_time" gorm:"bigint"`
	UpdatedTime      int64  `json:"updated_time" gorm:"bigint"`
}

// MaxVIPBonusBasisPoints 上限 100%。
const MaxVIPBonusBasisPoints = 10000

// GetVIPLevelForRecharge 返回累计充值对应的最高启用等级。无匹配（或 tx 为
// nil 之外的查询失败）时 ok=false，调用方按 0% 加赠、普通用户兜底处理。
// 必须在确定“充值前”累计金额之后调用，禁止用累加后的金额反查等级。
func GetVIPLevelForRecharge(tx *gorm.DB, totalRechargeCents int64) (*VIPLevel, bool, error) {
	q := tx
	if q == nil {
		q = DB
	}
	var level VIPLevel
	err := q.Where("enabled = ? AND threshold_cents <= ?", true, totalRechargeCents).
		Order("threshold_cents DESC, bonus_basis_points DESC").
		First(&level).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &level, true, nil
}

// GetNextVIPLevel 返回距当前累计充值最近的更高启用等级；已最高时返回 (nil, nil)。
func GetNextVIPLevel(totalRechargeCents int64) (*VIPLevel, error) {
	var level VIPLevel
	err := DB.Where("enabled = ? AND threshold_cents > ?", true, totalRechargeCents).
		Order("threshold_cents ASC").
		First(&level).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &level, nil
}

func GetAllVIPLevels() ([]*VIPLevel, error) {
	levels := []*VIPLevel{}
	err := DB.Order("sort ASC, threshold_cents ASC, id ASC").Find(&levels).Error
	return levels, err
}

func GetVIPLevelById(id int) (*VIPLevel, error) {
	if id == 0 {
		return nil, errors.New("id 为空")
	}
	var level VIPLevel
	err := DB.First(&level, "id = ?", id).Error
	return &level, err
}

func (level *VIPLevel) Insert() error {
	if err := level.validate(); err != nil {
		return err
	}
	if level.Enabled {
		taken, err := vipLevelThresholdTaken(level)
		if err != nil {
			return err
		}
		if taken {
			return errors.New("已存在相同门槛的启用等级")
		}
	}
	level.CreatedTime = common.GetTimestamp()
	level.UpdatedTime = level.CreatedTime
	return DB.Create(level).Error
}

func (level *VIPLevel) Update() error {
	if err := level.validate(); err != nil {
		return err
	}
	if level.Enabled {
		taken, err := vipLevelThresholdTaken(level)
		if err != nil {
			return err
		}
		if taken {
			return errors.New("已存在相同门槛的启用等级")
		}
	}
	level.UpdatedTime = common.GetTimestamp()
	return DB.Model(level).Select("name", "threshold_cents", "bonus_basis_points", "enabled", "sort", "updated_time").
		Updates(level).Error
}

func DeleteVIPLevelById(id int) (err error) {
	if id == 0 {
		return errors.New("id 为空")
	}
	result := DB.Where("id = ?", id).Delete(&VIPLevel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("等级不存在")
	}
	return nil
}

func (level *VIPLevel) validate() error {
	if utf8.RuneCountInString(level.Name) == 0 || utf8.RuneCountInString(level.Name) > 32 {
		return errors.New("等级名称长度须为 1-32")
	}
	if level.ThresholdCents < 0 {
		return errors.New("升级门槛不能为负数")
	}
	if level.BonusBasisPoints < 0 || level.BonusBasisPoints > MaxVIPBonusBasisPoints {
		return errors.New("充值加赠比例须在 0 到 100% 之间")
	}
	return nil
}

// vipLevelThresholdTaken 检查启用等级中是否已有相同门槛（不含自身），
// 避免等级归属出现歧义。
func vipLevelThresholdTaken(level *VIPLevel) (bool, error) {
	query := DB.Model(&VIPLevel{}).Where("enabled = ? AND threshold_cents = ?", true, level.ThresholdCents)
	if level.Id != 0 {
		query = query.Where("id <> ?", level.Id)
	}
	var count int64
	err := query.Count(&count).Error
	return count > 0, err
}

// seedDefaultVIPLevels 仅在表为空时插入默认等级，幂等；失败由调用方记录
// 日志而不阻断启动（等级缺失时兑换按 0% 加赠兜底）。
func seedDefaultVIPLevels() error {
	var count int64
	if err := DB.Model(&VIPLevel{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	now := common.GetTimestamp()
	defaults := []VIPLevel{
		{Name: "普通用户", ThresholdCents: 0, BonusBasisPoints: 0, Enabled: true, Sort: 0, CreatedTime: now, UpdatedTime: now},
		{Name: "VIP1", ThresholdCents: 50000, BonusBasisPoints: 200, Enabled: true, Sort: 1, CreatedTime: now, UpdatedTime: now},
		{Name: "VIP2", ThresholdCents: 200000, BonusBasisPoints: 400, Enabled: true, Sort: 2, CreatedTime: now, UpdatedTime: now},
		{Name: "VIP3", ThresholdCents: 500000, BonusBasisPoints: 600, Enabled: true, Sort: 3, CreatedTime: now, UpdatedTime: now},
		{Name: "VIP4", ThresholdCents: 1000000, BonusBasisPoints: 800, Enabled: true, Sort: 4, CreatedTime: now, UpdatedTime: now},
	}
	return DB.Create(&defaults).Error
}
