package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// Each bill covers ALL request procurement costs on one actual channel in a
// half-open period. Corrections replace the amount, never add it a second time.
// Pool fixed fees and operating fees are outside this scope.
type FinanceSupplierBill struct {
	ID          int64   `json:"id"`
	EventKey    string  `json:"event_key" gorm:"type:varchar(100);uniqueIndex"`
	ChannelID   int     `json:"channel_id" gorm:"uniqueIndex:idx_fin_bill_revision,priority:1"`
	Scope       string  `json:"scope" gorm:"type:varchar(32)"`
	Revision    int64   `json:"revision" gorm:"uniqueIndex:idx_fin_bill_revision,priority:2"`
	ReplacesID  int64   `json:"replaces_id"`
	PeriodFrom  int64   `json:"period_from"`
	PeriodTo    int64   `json:"period_to"`
	Currency    string  `json:"currency" gorm:"type:varchar(3)"`
	Amount      string  `json:"amount" gorm:"type:varchar(64)"`
	FXVersionID int64   `json:"fx_version_id"`
	AmountCNY   *string `json:"amount_cny" gorm:"type:varchar(64)"`
	Evidence    string  `json:"evidence" gorm:"type:varchar(500)"`
	CreatedBy   int     `json:"created_by"`
	CreatedAt   int64   `json:"created_at"`
}

func FinanceSupplierReady(db *gorm.DB) error {
	if db == nil {
		return ErrFinanceUnavailable
	}
	var schema FinanceSchema
	if err := db.First(&schema, 1).Error; err != nil || schema.Version < 5 || schema.Version > 9 {
		return ErrFinanceUnavailable
	}
	return nil
}

func MigrateFinanceSupplier(db *gorm.DB) error {
	if err := MigrateFinanceReceipts(db); err != nil {
		return err
	}
	if err := db.AutoMigrate(&FinanceSupplierBill{}); err != nil {
		return err
	}
	return db.Model(&FinanceSchema{}).Where("id = ? AND version = ?", 1, 4).Update("version", 5).Error
}

// A channel-wide unique revision serializes even disjoint first writes on
// SQLite. Concurrent overlapping inserts cannot both commit after reading the
// same previous revision. No update/delete of earlier bills is exposed.
func AppendFinanceSupplierBill(db *gorm.DB, entry *FinanceSupplierBill, expectedID int64, requestID string) error {
	if err := FinanceSupplierReady(db); err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var duplicate FinanceSupplierBill
		if err := tx.Where("event_key = ?", entry.EventKey).First(&duplicate).Error; err == nil {
			return ErrFinanceConflict
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var previous FinanceSupplierBill
		err := lockForUpdate(tx).Where("channel_id = ?", entry.ChannelID).Order("revision desc").First(&previous).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if previous.ID != expectedID {
			return ErrFinanceConflict
		}
		var rows []FinanceSupplierBill
		if err := tx.Where("channel_id = ?", entry.ChannelID).Order("revision desc").Find(&rows).Error; err != nil {
			return err
		}
		superseded := map[int64]bool{}
		found := entry.ReplacesID == 0
		for _, row := range rows {
			if !superseded[row.ID] {
				if row.ID == entry.ReplacesID {
					found = true
					if entry.PeriodFrom != row.PeriodFrom || entry.PeriodTo != row.PeriodTo || entry.Currency != row.Currency || entry.FXVersionID != row.FXVersionID {
						return ErrFinanceInvalid
					}
				} else if entry.PeriodFrom < row.PeriodTo && entry.PeriodTo > row.PeriodFrom {
					return ErrFinanceConflict
				}
			}
			superseded[row.ReplacesID] = true
		}
		if !found {
			return ErrFinanceConflict
		}
		entry.ID, entry.Revision, entry.CreatedAt = 0, previous.Revision+1, time.Now().Unix()
		if err := tx.Create(entry).Error; err != nil {
			return err
		}
		return tx.Create(&FinanceAudit{ActorID: entry.CreatedBy, Action: "supplier.append", EntityID: entry.ID, PreviousID: entry.ReplacesID, RequestID: requestID, CreatedAt: entry.CreatedAt}).Error
	})
}
