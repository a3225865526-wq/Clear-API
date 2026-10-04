package model

import (
	"errors"
	"os"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type BillingCompletionOutbox struct {
	ID          int64
	RequestID   string `gorm:"type:varchar(128);uniqueIndex:idx_billing_completion_outbox"`
	Payload     string `gorm:"type:text"`
	State       string `gorm:"type:varchar(16);index"`
	Attempts    int
	NextAttempt int64 `gorm:"index"`
}

type BillingCompletionReceipt struct {
	ID        int64
	RequestID string `gorm:"type:varchar(128);uniqueIndex:idx_billing_completion_receipt"`
}

func BillingOutboxEnabled() bool { return os.Getenv("CLEARAPI_BILLING_OUTBOX") == "true" }

func MigrateBillingCompletionOutbox(db *gorm.DB) error {
	return db.AutoMigrate(&BillingCompletionOutbox{})
}
func MigrateBillingCompletionReceipts(db *gorm.DB) error {
	return db.AutoMigrate(&BillingCompletionReceipt{})
}

func BillingCompletionOutboxReady(db, logDB *gorm.DB) error {
	if db == nil || logDB == nil || !db.Migrator().HasIndex(&BillingCompletionOutbox{}, "idx_billing_completion_outbox") || !logDB.Migrator().HasIndex(&BillingCompletionReceipt{}, "idx_billing_completion_receipt") {
		return ErrFinanceUnavailable
	}
	if logDB.Dialector.Name() != "sqlite" && logDB.Dialector.Name() != "mysql" && logDB.Dialector.Name() != "postgres" {
		return ErrFinanceUnavailable
	}
	var rows []BillingCompletionOutbox
	if !logDB.Migrator().HasTable(&Log{}) {
		return ErrFinanceUnavailable
	}
	return db.Select("id", "request_id", "payload", "state", "attempts", "next_attempt").Limit(0).Find(&rows).Error
}

// The primary outbox is the canonical bill. A log DB outage cannot erase it.
// Receipts make delivery idempotent even if acknowledgement fails after commit.
func RecoverBillingCompletionLogs(limit int) error {
	if !BillingOutboxEnabled() {
		return nil
	}
	if limit < 1 || limit > 100 {
		return ErrFinanceInvalid
	}
	var rows []BillingCompletionOutbox
	if err := DB.Where("state = ? AND next_attempt <= ?", "pending", time.Now().Unix()).Order("id").Limit(limit).Find(&rows).Error; err != nil {
		return err
	}
	var failures []error
	for _, row := range rows {
		err := LOG_DB.Transaction(func(tx *gorm.DB) error {
			var receipt BillingCompletionReceipt
			err := tx.Where("request_id = ?", row.RequestID).First(&receipt).Error
			if err == nil {
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err := tx.Create(&BillingCompletionReceipt{RequestID: row.RequestID}).Error; err != nil {
				return err
			}
			var log Log
			if err := common.Unmarshal([]byte(row.Payload), &log); err != nil {
				return err
			}
			return tx.Create(&log).Error
		})
		if err == nil {
			err = DB.Model(&BillingCompletionOutbox{}).Where("id = ? AND state = ?", row.ID, "pending").Update("state", "delivered").Error
		}
		if err != nil {
			attempts := min(row.Attempts+1, 6)
			_ = DB.Model(&BillingCompletionOutbox{}).Where("id = ? AND state = ? AND attempts = ?", row.ID, "pending", row.Attempts).Updates(map[string]any{"attempts": attempts, "next_attempt": time.Now().Unix() + int64(15*(1<<attempts))}).Error
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// Enrichment never creates a second bill. Keep the original amount, identities
// and timestamp; only details may be supplied after funds have committed.
func EnrichBillingCompletionLog(log *Log) (bool, error) {
	if !BillingOutboxEnabled() || log.RequestId == "" {
		return false, nil
	}
	var row BillingCompletionOutbox
	err := DB.Where("request_id = ?", log.RequestId).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	var canonical Log
	if err := common.Unmarshal([]byte(row.Payload), &canonical); err != nil {
		return true, err
	}
	if canonical.UserId != log.UserId || canonical.TokenId != log.TokenId || canonical.Quota != log.Quota || canonical.Type != LogTypeConsume {
		return true, ErrBillingReservationConflict
	}
	log.Id, log.CreatedAt = 0, canonical.CreatedAt
	payload, err := common.Marshal(log)
	if err != nil {
		return true, err
	}
	// Delivery may win this CAS. Its canonical record remains sufficient to
	// reconcile funds; do not overwrite the delivered immutable bill.
	return true, DB.Model(&BillingCompletionOutbox{}).Where("id = ? AND state = ?", row.ID, "pending").Update("payload", string(payload)).Error
}
