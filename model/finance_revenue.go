package model

import (
	"errors"

	"gorm.io/gorm"
)

// Mutable projections are rebuilt from immutable events/parts. They belong to
// the finance ledger, not to User.Quota or any billing funding source.
type FinanceWallet struct {
	UserID               int    `json:"user_id" gorm:"primaryKey;autoIncrement:false"`
	Revision             int64  `json:"revision"`
	LastAt               int64  `json:"last_at"`
	RemainingQuota       int64  `json:"remaining_quota"`
	KnownBackingCNY      string `json:"known_backing_cny" gorm:"type:varchar(64)"`
	KnownRevenueCNY      string `json:"known_revenue_cny" gorm:"type:varchar(64)"`
	UnknownCreditCount   int64  `json:"unknown_credit_count"`
	UnknownConsumedQuota int64  `json:"unknown_consumed_quota"`
}

type FinanceRevenueLot struct {
	ID            int64   `json:"id"`
	UserID        int     `json:"user_id" gorm:"index"`
	EventID       int64   `json:"event_id" gorm:"uniqueIndex"`
	LotKey        string  `json:"lot_key" gorm:"type:varchar(100)"`
	Source        string  `json:"source" gorm:"type:varchar(16)"`
	TotalQuota    int64   `json:"total_quota"`
	UsedQuota     int64   `json:"used_quota"`
	ValueCNY      *string `json:"value_cny" gorm:"type:varchar(64)"`
	RecognizedCNY string  `json:"recognized_cny" gorm:"type:varchar(64)"`
}

type FinanceRevenueRecord struct {
	ID        int64   `json:"id"`
	UserID    int     `json:"user_id" gorm:"uniqueIndex:idx_fin_revenue_key,priority:1;uniqueIndex:idx_fin_revenue_seq,priority:1"`
	EventKey  string  `json:"event_key" gorm:"type:varchar(100);uniqueIndex:idx_fin_revenue_key,priority:2"`
	Revision  int64   `json:"revision" gorm:"uniqueIndex:idx_fin_revenue_seq,priority:2"`
	At        int64   `json:"at"`
	Kind      string  `json:"kind" gorm:"type:varchar(16)"`
	Quota     int64   `json:"quota"`
	Source    string  `json:"source" gorm:"type:varchar(16)"`
	ValueCNY  *string `json:"value_cny" gorm:"type:varchar(64)"`
	Reverses  int64   `json:"reverses" gorm:"index"`
	Evidence  string  `json:"evidence" gorm:"type:varchar(500)"`
	CreatedAt int64   `json:"created_at"`
	CreatedBy int     `json:"created_by"`
}

// A reversal appends negative parts referencing the original event; it never
// rewrites that event's allocation or applies today's valuation.
type FinanceRevenueAllocation struct {
	ID       int64   `json:"id"`
	EventID  int64   `json:"event_id" gorm:"index"`
	LotID    int64   `json:"lot_id" gorm:"index"`
	Quota    int64   `json:"quota"`
	ValueCNY *string `json:"value_cny" gorm:"type:varchar(64)"`
}

func FinanceRevenueReady(db *gorm.DB) error {
	if db == nil {
		return ErrFinanceUnavailable
	}
	var schema FinanceSchema
	if err := db.First(&schema, 1).Error; err != nil || schema.Version < 2 || schema.Version > 9 {
		return ErrFinanceUnavailable
	}
	return nil
}

// This additive v2 migration has its own approval gate. A v1 approval must not
// implicitly authorize these tables. Normal application startup calls neither.
func MigrateFinanceRevenue(db *gorm.DB) error {
	if err := MigrateFinance(db); err != nil {
		return err
	}
	if err := db.AutoMigrate(&FinanceWallet{}, &FinanceRevenueLot{}, &FinanceRevenueRecord{}, &FinanceRevenueAllocation{}); err != nil {
		return err
	}
	return db.Model(&FinanceSchema{}).Where("id = ? AND version = ?", 1, 1).Update("version", 2).Error
}

// Lock the ledger head on databases that support row locks. Revision checks
// and a conditional update also protect SQLite and reject stale writers.
func LockFinanceWallet(tx *gorm.DB, userID int, expected int64) (*FinanceWallet, error) {
	var wallet FinanceWallet
	err := lockForUpdate(tx).First(&wallet, "user_id = ?", userID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if expected != 0 {
			return nil, ErrFinanceConflict
		}
		wallet = FinanceWallet{UserID: userID, KnownBackingCNY: "0", KnownRevenueCNY: "0"}
		if err := tx.Create(&wallet).Error; err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if wallet.Revision != expected {
		return nil, ErrFinanceConflict
	}
	return &wallet, nil
}
