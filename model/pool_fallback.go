package model

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const MaxPoolFallbackCount = 3

// PoolFallbackPolicy stores the bounded account order used by a pool model.
// It is intentionally independent from trust, capability, and release state.
type PoolFallbackPolicy struct {
	Id               int                  `json:"id" gorm:"primaryKey"`
	PoolId           string               `json:"pool_id" gorm:"type:varchar(64);index:idx_pool_fallback_scope,unique,priority:1"`
	ModelName        string               `json:"model_name" gorm:"type:varchar(128);index:idx_pool_fallback_scope,unique,priority:2"`
	MaxFallbackCount int                  `json:"max_fallback_count"`
	Enabled          bool                 `json:"enabled"`
	CreatedAt        time.Time            `json:"created_at"`
	UpdatedAt        time.Time            `json:"updated_at"`
	Members          []PoolFallbackMember `json:"members" gorm:"foreignKey:PolicyId;constraint:OnDelete:CASCADE"`
}

func (PoolFallbackPolicy) TableName() string { return "pool_fallback_policy" }

type PoolFallbackMember struct {
	Id        int  `json:"id" gorm:"primaryKey"`
	PolicyId  int  `json:"policy_id" gorm:"index:idx_pool_fallback_member_order,priority:1;uniqueIndex:idx_pool_fallback_member_account,priority:1"`
	AccountId int  `json:"account_id" gorm:"uniqueIndex:idx_pool_fallback_member_account,priority:2"`
	Priority  int  `json:"priority" gorm:"index:idx_pool_fallback_member_order,priority:2"`
	Enabled   bool `json:"enabled"`
}

func (PoolFallbackMember) TableName() string { return "pool_fallback_member" }

func MigratePoolFallback(db *gorm.DB) error {
	if db == nil {
		return ErrPoolUnavailable
	}
	return db.AutoMigrate(&PoolFallbackPolicy{}, &PoolFallbackMember{})
}

// PoolFallbackRepository reads the database directly; no policy cache or
// publication revision can delay a saved order for subsequent requests.
type PoolFallbackRepository struct {
	DB *gorm.DB
}

func (r PoolFallbackRepository) Get(ctx context.Context, poolID, modelName string) (*PoolFallbackPolicy, error) {
	if r.DB == nil || !r.DB.Migrator().HasTable(&PoolFallbackPolicy{}) || !r.DB.Migrator().HasTable(&PoolFallbackMember{}) {
		return nil, ErrPoolUnavailable
	}
	var policy PoolFallbackPolicy
	found := true
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Serialize the policy and member reads with whole-policy replacement.
		err := lockForUpdate(tx).Where("pool_id = ? AND model_name = ?", poolID, modelName).First(&policy).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			found = false
			return nil
		}
		if err != nil {
			return err
		}
		policy.Members = make([]PoolFallbackMember, 0)
		return tx.Where("policy_id = ?", policy.Id).Order("priority desc, id asc").Find(&policy.Members).Error
	})
	if err != nil || !found {
		return nil, err
	}
	return &policy, nil
}

func (r PoolFallbackRepository) Replace(ctx context.Context, policy PoolFallbackPolicy) (*PoolFallbackPolicy, error) {
	if r.DB == nil || !r.DB.Migrator().HasTable(&PoolFallbackPolicy{}) || !r.DB.Migrator().HasTable(&PoolFallbackMember{}) {
		return nil, ErrPoolUnavailable
	}
	if policy.MaxFallbackCount < 0 || policy.MaxFallbackCount > MaxPoolFallbackCount {
		return nil, ErrPoolInvalid
	}
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		seed := PoolFallbackPolicy{PoolId: policy.PoolId, ModelName: policy.ModelName}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; err != nil {
			return err
		}
		var stored PoolFallbackPolicy
		if err := lockForUpdate(tx).Where("pool_id = ? AND model_name = ?", policy.PoolId, policy.ModelName).First(&stored).Error; err != nil {
			return err
		}
		policy.Id, policy.CreatedAt, policy.UpdatedAt = stored.Id, stored.CreatedAt, time.Now().UTC()
		if err := tx.Model(&stored).Updates(map[string]any{
			"max_fallback_count": policy.MaxFallbackCount, "enabled": policy.Enabled, "updated_at": policy.UpdatedAt,
		}).Error; err != nil {
			return err
		}
		if err := tx.Where("policy_id = ?", policy.Id).Delete(&PoolFallbackMember{}).Error; err != nil {
			return err
		}
		for i := range policy.Members {
			policy.Members[i].Id = 0
			policy.Members[i].PolicyId = policy.Id
		}
		if len(policy.Members) == 0 {
			return nil
		}
		return tx.Create(&policy.Members).Error
	})
	if err != nil {
		return nil, err
	}
	return &policy, nil
}
