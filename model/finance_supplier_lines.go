package model

import (
	"errors"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// Bill lines are the reconciliation layer under one supplier bill. The bill
// total stays the only posting value for its scope; lines never replace or
// adjust it. A credit line offsets one original line by a positive amount and
// never exceeds it; a replacement line supersedes the original once. Earlier
// lines are never updated or deleted.
type FinanceSupplierBillLine struct {
	ID               int64   `json:"id"`
	EventKey         string  `json:"event_key" gorm:"type:varchar(100);uniqueIndex"`
	BillID           int64   `json:"bill_id" gorm:"index"`
	ChannelID        int     `json:"channel_id" gorm:"index"`
	Category         string  `json:"category" gorm:"type:varchar(32)"`
	UpstreamModel    string  `json:"upstream_model" gorm:"type:varchar(200)"`
	Reference        string  `json:"reference" gorm:"type:varchar(100)"`
	UsageFrom        int64   `json:"usage_from"`
	UsageTo          int64   `json:"usage_to"`
	Quantity         *string `json:"quantity" gorm:"type:varchar(64)"`
	Currency         string  `json:"currency" gorm:"type:varchar(3)"`
	Amount           string  `json:"amount" gorm:"type:varchar(64)"`
	FXVersionID      int64   `json:"fx_version_id"`
	AmountCNY        *string `json:"amount_cny" gorm:"type:varchar(64)"`
	ReplacesLineID   int64   `json:"replaces_line_id"`
	ReferencesLineID int64   `json:"references_line_id"`
	Evidence         string  `json:"evidence" gorm:"type:varchar(500)"`
	CreatedBy        int     `json:"created_by"`
	CreatedAt        int64   `json:"created_at"`
}

func FinanceSupplierLinesReady(db *gorm.DB) error {
	if db == nil {
		return ErrFinanceUnavailable
	}
	var schema FinanceSchema
	if err := db.First(&schema, 1).Error; err != nil || schema.Version < 9 || schema.Version > 9 {
		return ErrFinanceUnavailable
	}
	return nil
}

func MigrateFinanceSupplierLines(db *gorm.DB) error {
	if err := MigrateFinanceRebuilds(db); err != nil {
		return err
	}
	if err := db.AutoMigrate(&FinanceSupplierBillLine{}); err != nil {
		return err
	}
	return db.Model(&FinanceSchema{}).Where("id = ? AND version = ?", 1, 8).Update("version", 9).Error
}

// AppendFinanceSupplierBillLine enforces: unique event key, the target bill is
// the currently effective revision on its channel, credit lines reference an
// original line of the same bill without exceeding its amount in accumulated
// outstanding credits, and replacement lines supersede one original line once
// while keeping category, currency, FX and usage period unchanged.
func AppendFinanceSupplierBillLine(db *gorm.DB, entry *FinanceSupplierBillLine, requestID string) error {
	if err := FinanceSupplierLinesReady(db); err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var duplicate FinanceSupplierBillLine
		if err := tx.Where("event_key = ?", entry.EventKey).First(&duplicate).Error; err == nil {
			return ErrFinanceConflict
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var bills []FinanceSupplierBill
		if err := tx.Where("channel_id = ?", entry.ChannelID).Order("revision desc").Limit(1001).Find(&bills).Error; err != nil {
			return err
		}
		superseded := map[int64]bool{}
		var bill *FinanceSupplierBill
		for i := range bills {
			if !superseded[bills[i].ID] && bills[i].ID == entry.BillID {
				bill = &bills[i]
			}
			superseded[bills[i].ReplacesID] = true
		}
		if bill == nil {
			return ErrFinanceInvalid
		}
		var count int64
		if err := tx.Model(&FinanceSupplierBillLine{}).Where("bill_id = ?", entry.BillID).Count(&count).Error; err != nil {
			return err
		}
		if count > 10000 {
			return ErrFinanceInvalid
		}
		if entry.ReferencesLineID != 0 {
			// A credit offsets one original, unreplaced, non-credit line of the
			// same bill; accumulated outstanding credits never exceed its amount.
			var original FinanceSupplierBillLine
			if err := tx.First(&original, entry.ReferencesLineID).Error; err != nil {
				return ErrFinanceInvalid
			}
			if original.BillID != entry.BillID || original.Category == "credit" || entry.ReplacesLineID != 0 {
				return ErrFinanceInvalid
			}
			amount, err := decimal.NewFromString(entry.Amount)
			if err != nil || amount.IsNegative() {
				return ErrFinanceInvalid
			}
			originalAmount, err := decimal.NewFromString(original.Amount)
			if err != nil || originalAmount.IsNegative() {
				return ErrFinanceInvalid
			}
			var credits []FinanceSupplierBillLine
			if err := tx.Where("references_line_id = ? AND category = ?", original.ID, "credit").Order("id asc").Find(&credits).Error; err != nil {
				return err
			}
			outstanding := decimal.Zero
			for _, credit := range credits {
				value, err := decimal.NewFromString(credit.Amount)
				if err != nil || value.IsNegative() {
					return ErrFinanceInvalid
				}
				outstanding = outstanding.Add(value)
			}
			if outstanding.Add(amount).Cmp(originalAmount) > 0 {
				return ErrFinanceInvalid
			}
			if entry.Currency != original.Currency || entry.FXVersionID != original.FXVersionID {
				return ErrFinanceInvalid
			}
		}
		if entry.ReplacesLineID != 0 {
			var original FinanceSupplierBillLine
			if err := tx.First(&original, entry.ReplacesLineID).Error; err != nil {
				return ErrFinanceInvalid
			}
			if original.BillID != entry.BillID || original.Category == "credit" || original.ReferencesLineID != 0 {
				return ErrFinanceInvalid
			}
			var superseding FinanceSupplierBillLine
			if err := tx.Where("replaces_line_id = ?", original.ID).Limit(1).First(&superseding).Error; err == nil {
				return ErrFinanceConflict
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if entry.Category != original.Category || entry.Currency != original.Currency || entry.FXVersionID != original.FXVersionID || entry.UsageFrom != original.UsageFrom || entry.UsageTo != original.UsageTo {
				return ErrFinanceInvalid
			}
		}
		entry.ID, entry.CreatedAt = 0, time.Now().Unix()
		if err := tx.Create(entry).Error; err != nil {
			return err
		}
		previous := entry.ReferencesLineID
		if entry.ReplacesLineID != 0 {
			previous = entry.ReplacesLineID
		}
		return tx.Create(&FinanceAudit{ActorID: entry.CreatedBy, Action: "supplier.line", EntityID: entry.ID, PreviousID: previous, RequestID: requestID, CreatedAt: entry.CreatedAt}).Error
	})
}
