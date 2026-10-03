package model

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// The inbox lives in the separately configured log database. Receipt and log
// insert share its transaction; a crash before the main outbox acknowledgement
// replays the receipt instead of duplicating the log.
type TaskSettlementLogReceipt struct {
	ID           int64
	SettlementID int64 `gorm:"uniqueIndex:idx_task_settlement_log"`
}

func MigrateTaskSettlementLogs(db *gorm.DB) error {
	if db == nil || !db.Migrator().HasTable(&Log{}) {
		return ErrFinanceUnavailable
	}
	if db.Dialector.Name() != "sqlite" && db.Dialector.Name() != "mysql" && db.Dialector.Name() != "postgres" {
		return ErrFinanceUnavailable
	}
	return db.AutoMigrate(&TaskSettlementLogReceipt{})
}

func TaskSettlementLogsReady(db *gorm.DB) error {
	if db == nil || !db.Migrator().HasIndex(&TaskSettlementLogReceipt{}, "idx_task_settlement_log") {
		return ErrFinanceUnavailable
	}
	if db.Dialector.Name() != "sqlite" && db.Dialector.Name() != "mysql" && db.Dialector.Name() != "postgres" {
		return ErrFinanceUnavailable
	}
	return nil
}

func HasTaskSettlementWork(db *gorm.DB) bool {
	if TaskSettlementReady(db) != nil {
		return false
	}
	var id int64
	err := db.Model(&TaskSettlement{}).Where("(state = ? AND next_attempt <= ?) OR (state = ? AND updated_at <= ?) OR (log_state = ? AND log_next_attempt <= ?)", "pending", time.Now().Unix(), "awaiting_facts", time.Now().Unix()-60, "pending", time.Now().Unix()).Limit(1).Pluck("id", &id).Error
	return err == nil && id != 0
}

func MarkTaskSettlementReview(db *gorm.DB, taskID int64, class string) error {
	if err := TaskSettlementReady(db); err != nil {
		return err
	}
	return db.Model(&TaskSettlement{}).Where("task_id = ? AND state = ?", taskID, "awaiting_facts").Updates(map[string]any{"state": "needs_review", "error_class": class, "updated_at": time.Now().Unix()}).Error
}

func RecoverTaskSettlements(db, logDB *gorm.DB) error {
	if err := TaskSettlementReady(db); err != nil {
		return err
	}
	// A crash before final facts were frozen cannot be turned into a guessed
	// charge. Age only promotes visibility; it never authorizes money movement.
	if err := db.Model(&TaskSettlement{}).Where("state = ? AND updated_at <= ?", "awaiting_facts", time.Now().Unix()-60).Updates(map[string]any{"state": "needs_review", "error_class": "final_facts_missing", "updated_at": time.Now().Unix()}).Error; err != nil {
		return err
	}
	var rows []TaskSettlement
	if err := db.Where("state = ? AND next_attempt <= ?", "pending", time.Now().Unix()).Order("id").Limit(100).Find(&rows).Error; err != nil {
		return err
	}
	for _, r := range rows {
		_, _ = ApplyTaskSettlement(db, r.ID)
	}
	return DeliverTaskSettlementLogs(db, logDB)
}

func DeliverTaskSettlementLogs(db, logDB *gorm.DB) error {
	if err := TaskSettlementReady(db); err != nil {
		return err
	}
	var rows []TaskSettlement
	if err := db.Where("state = ? AND log_state = ? AND log_next_attempt <= ?", "applied", "pending", time.Now().Unix()).Order("id").Limit(100).Find(&rows).Error; err != nil {
		return err
	}
	for _, r := range rows {
		err := TaskSettlementLogsReady(logDB)
		if err == nil {
			err = logDB.Transaction(func(tx *gorm.DB) error {
				var receipt TaskSettlementLogReceipt
				if err := tx.Where("settlement_id = ?", r.ID).First(&receipt).Error; err == nil {
					return nil
				} else if !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				if err := tx.Create(&TaskSettlementLogReceipt{SettlementID: r.ID}).Error; err != nil {
					return err
				}
				var log Log
				if err := common.UnmarshalJsonStr(r.LogPayload, &log); err != nil {
					return err
				}
				log.Id = 0
				return tx.Create(&log).Error
			})
		}
		updates := map[string]any{"log_state": "delivered", "log_attempts": r.LogAttempts + 1}
		if err != nil {
			state := "pending"
			if r.LogAttempts+1 >= 5 {
				state = "needs_review"
			}
			updates = map[string]any{"log_state": state, "log_attempts": r.LogAttempts + 1, "log_next_attempt": time.Now().Unix() + int64(15*(1<<min(r.LogAttempts+1, 5)))}
		}
		if err := db.Model(&TaskSettlement{}).Where("id = ? AND log_state = ? AND log_attempts = ?", r.ID, "pending", r.LogAttempts).Updates(updates).Error; err != nil {
			return err
		}
	}
	return nil
}
