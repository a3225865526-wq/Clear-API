package model

import (
	"gorm.io/gorm"
)

// Frozen snapshots of controlled wallet rebuilds. A rebuild rewrites one
// wallet's ledger rows from a canonical fact set; the snapshot preserves the
// exact facts and the pre-rebuild projection so every rewrite stays
// reviewable. Rows are never updated or deleted.
type FinanceRebuildSnapshot struct {
	ID        int64  `json:"id"`
	UserID    int    `json:"user_id" gorm:"index"`
	FactCount int    `json:"fact_count"`
	Facts     string `json:"-" gorm:"type:text"`
	Before    string `json:"before" gorm:"type:text"`
	Hash      string `json:"hash" gorm:"type:varchar(64)"`
	Evidence  string `json:"evidence" gorm:"type:varchar(500)"`
	CreatedBy int    `json:"created_by"`
	CreatedAt int64  `json:"created_at"`
}

func FinanceRebuildsReady(db *gorm.DB) error {
	if db == nil {
		return ErrFinanceUnavailable
	}
	var schema FinanceSchema
	if err := db.First(&schema, 1).Error; err != nil || schema.Version < 8 || schema.Version > 9 {
		return ErrFinanceUnavailable
	}
	return nil
}

// Explicit additive migration only; never invoked by normal gateway startup.
func MigrateFinanceRebuilds(db *gorm.DB) error {
	if err := MigrateFinanceSync(db); err != nil {
		return err
	}
	if err := db.AutoMigrate(&FinanceRebuildSnapshot{}); err != nil {
		return err
	}
	return db.Model(&FinanceSchema{}).Where("id = ? AND version = ?", 1, 7).Update("version", 8).Error
}
