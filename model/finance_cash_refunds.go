package model

import (
	"errors"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// Cash refunds are append-only facts against one confirmed receipt and never
// restate prior receipts, revenue or the business wallet. Only reliably
// succeeded refunds reduce the known net receipts; pending, queued and failed
// records stay visible without changing any total.
type FinanceCashRefund struct {
	ID         int64  `json:"id"`
	EventKey   string `json:"event_key" gorm:"type:varchar(100);uniqueIndex"`
	ReceiptID  int64  `json:"receipt_id" gorm:"index"`
	OrderID    int    `json:"order_id" gorm:"index"`
	State      string `json:"state" gorm:"type:varchar(16)"`
	AmountCNY  string `json:"amount_cny" gorm:"type:varchar(64)"`
	RefundedAt int64  `json:"refunded_at" gorm:"index"`
	Evidence   string `json:"evidence" gorm:"type:varchar(500)"`
	CreatedBy  int    `json:"created_by"`
	CreatedAt  int64  `json:"created_at"`
}

const FinanceRefundSucceeded = "succeeded"

func FinanceRefundsReady(db *gorm.DB) error {
	if db == nil {
		return ErrFinanceUnavailable
	}
	var schema FinanceSchema
	if err := db.First(&schema, 1).Error; err != nil || schema.Version < 6 || schema.Version > 9 {
		return ErrFinanceUnavailable
	}
	return nil
}

// Explicit additive migration only; never invoked by normal gateway startup.
func MigrateFinanceRefunds(db *gorm.DB) error {
	if err := MigrateFinanceSupplier(db); err != nil {
		return err
	}
	if err := db.AutoMigrate(&FinanceCashRefund{}); err != nil {
		return err
	}
	return db.Model(&FinanceSchema{}).Where("id = ? AND version = ?", 1, 5).Update("version", 6).Error
}

// The receipt row lock serializes concurrent refund appends. Applied, queued
// and succeeded refunds reserve against the confirmed amount; failed refunds
// are history only and never reserve. There is no update or delete path.
func AppendFinanceCashRefund(db *gorm.DB, entry *FinanceCashRefund, requestID string) error {
	if err := FinanceRefundsReady(db); err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var duplicate FinanceCashRefund
		if err := tx.Where("event_key = ?", entry.EventKey).First(&duplicate).Error; err == nil {
			return ErrFinanceConflict
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var receipt FinanceCashReceipt
		if err := lockForUpdate(tx).First(&receipt, entry.ReceiptID).Error; err != nil {
			return ErrFinanceInvalid
		}
		receiptAmount, err := decimal.NewFromString(receipt.AmountCNY)
		if err != nil {
			return ErrFinanceInvalid
		}
		amount, err := decimal.NewFromString(entry.AmountCNY)
		if err != nil {
			return ErrFinanceInvalid
		}
		reserved := decimal.Zero
		var lastID int64
		for {
			var refunds []FinanceCashRefund
			if err := tx.Where("receipt_id = ? AND id > ?", entry.ReceiptID, lastID).Order("id asc").Limit(100).Find(&refunds).Error; err != nil {
				return err
			}
			if len(refunds) == 0 {
				break
			}
			for _, refund := range refunds {
				lastID = refund.ID
				if refund.State == "failed" {
					continue
				}
				value, err := decimal.NewFromString(refund.AmountCNY)
				if err != nil {
					return ErrFinanceInvalid
				}
				reserved = reserved.Add(value)
			}
		}
		if entry.State != "failed" && reserved.Add(amount).Compare(receiptAmount) > 0 {
			return ErrFinanceInvalid
		}
		entry.ID, entry.OrderID, entry.CreatedAt = 0, receipt.OrderID, time.Now().Unix()
		if err := tx.Create(entry).Error; err != nil {
			return err
		}
		return tx.Create(&FinanceAudit{ActorID: entry.CreatedBy, Action: "cash.refund", EntityID: entry.ID, PreviousID: receipt.ID, RequestID: requestID, CreatedAt: entry.CreatedAt}).Error
	})
}
