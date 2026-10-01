package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

var ErrFinanceUnavailable = errors.New("finance schema has not been explicitly migrated")
var ErrFinanceConflict = errors.New("finance version conflict")
var ErrFinanceInvalid = errors.New("invalid finance record")

// Phase one stores cost/FX/pool revisions together. Typed payload validation is
// in service; revisions are immutable and referenced by ID, never overwritten.
type FinanceVersion struct {
	ID            int64  `json:"id"`
	Kind          string `json:"kind" gorm:"type:varchar(16);uniqueIndex:idx_fin_version,priority:1"`
	RecordKey     string `json:"record_key" gorm:"type:varchar(100);uniqueIndex:idx_fin_version,priority:2"`
	Revision      int64  `json:"revision" gorm:"uniqueIndex:idx_fin_version,priority:3"`
	ParentID      int64  `json:"parent_id"`
	EffectiveFrom int64  `json:"effective_from"`
	EffectiveTo   int64  `json:"effective_to"`
	Active        bool   `json:"active"`
	Payload       string `json:"-" gorm:"type:text"`
	Source        string `json:"source" gorm:"type:varchar(500)"`
	CreatedAt     int64  `json:"created_at"`
	CreatedBy     int    `json:"created_by"`
}

// Amount is an exact decimal string in the original currency. FXVersionID
// freezes its CNY conversion. A correction is another expense referencing the
// original; there are deliberately no update/delete operations.
type FinanceExpense struct {
	ID          int64  `json:"id"`
	EventKey    string `json:"event_key" gorm:"type:varchar(100);uniqueIndex"`
	Ledger      string `json:"ledger" gorm:"type:varchar(16)"`
	Category    string `json:"category" gorm:"type:varchar(100)"`
	PoolKey     string `json:"pool_key" gorm:"type:varchar(100)"`
	AccountRef  string `json:"account_ref" gorm:"type:varchar(100)"`
	Amount      string `json:"amount" gorm:"type:varchar(64)"`
	Currency    string `json:"currency" gorm:"type:varchar(3)"`
	FXVersionID int64  `json:"fx_version_id"`
	PeriodFrom  int64  `json:"period_from"`
	PeriodTo    int64  `json:"period_to"`
	Recurrence  string `json:"recurrence" gorm:"type:varchar(16)"`
	AdjustsID   int64  `json:"adjusts_id"`
	Source      string `json:"source" gorm:"type:varchar(500)"`
	Note        string `json:"note" gorm:"type:varchar(1000)"`
	CreatedAt   int64  `json:"created_at"`
	CreatedBy   int    `json:"created_by"`
}

type FinanceAudit struct {
	ID         int64  `json:"id"`
	ActorID    int    `json:"actor_id"`
	Action     string `json:"action" gorm:"type:varchar(32)"`
	EntityID   int64  `json:"entity_id"`
	PreviousID int64  `json:"previous_id"`
	RequestID  string `json:"request_id" gorm:"type:varchar(64)"`
	CreatedAt  int64  `json:"created_at"`
}

type FinanceSchema struct {
	ID      int `gorm:"primaryKey;autoIncrement:false"`
	Version int
}

// MigrateFinance must only be called by an explicitly approved migration tool
// or isolated tests. It is intentionally absent from application AutoMigrate.
func MigrateFinance(db *gorm.DB) error {
	if db.Migrator().HasTable(&FinanceSchema{}) {
		var existing FinanceSchema
		err := db.First(&existing, 1).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil && (existing.Version < 1 || existing.Version > 5) {
			return ErrFinanceUnavailable
		}
	}
	if err := db.AutoMigrate(&FinanceVersion{}, &FinanceExpense{}, &FinanceAudit{}, &FinanceSchema{}); err != nil {
		return err
	}
	return db.FirstOrCreate(&FinanceSchema{ID: 1, Version: 1}, FinanceSchema{ID: 1}).Error
}

func FinanceReady(db *gorm.DB) error {
	if db == nil {
		return ErrFinanceUnavailable
	}
	var schema FinanceSchema
	if err := db.First(&schema, 1).Error; err != nil || schema.Version < 1 || schema.Version > 5 {
		return ErrFinanceUnavailable
	}
	return nil
}

// AppendFinanceVersion uses an expected parent and a unique revision index.
// The index also rejects simultaneous first writes and stale concurrent writers
// on SQLite, where SELECT FOR UPDATE is unavailable.
func AppendFinanceVersion(db *gorm.DB, entry *FinanceVersion, expectedID int64, requestID string) error {
	if err := FinanceReady(db); err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var previous FinanceVersion
		err := lockForUpdate(tx).Where("kind = ? AND record_key = ?", entry.Kind, entry.RecordKey).Order("revision desc").First(&previous).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if previous.ID != expectedID {
			return ErrFinanceConflict
		}
		// Backdated correction is explicit via another key/backfill in a later
		// phase; new versions never silently supersede an earlier time interval.
		if previous.ID != 0 && entry.EffectiveFrom <= previous.EffectiveFrom {
			return ErrFinanceConflict
		}
		entry.ID = 0
		entry.ParentID = previous.ID
		entry.Revision = previous.Revision + 1
		entry.CreatedAt = time.Now().Unix()
		if err := tx.Create(entry).Error; err != nil {
			return err
		}
		return tx.Create(&FinanceAudit{ActorID: entry.CreatedBy, Action: entry.Kind + ".version", EntityID: entry.ID, PreviousID: previous.ID, RequestID: requestID, CreatedAt: entry.CreatedAt}).Error
	})
}

func AppendFinanceExpense(db *gorm.DB, entry *FinanceExpense, requestID string) error {
	if err := FinanceReady(db); err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var duplicate FinanceExpense
		err := tx.Where("event_key = ?", entry.EventKey).First(&duplicate).Error
		if err == nil {
			return ErrFinanceConflict
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if entry.AdjustsID != 0 {
			var original FinanceExpense
			if err := tx.First(&original, entry.AdjustsID).Error; err != nil {
				return ErrFinanceInvalid
			}
			if original.AdjustsID != 0 || entry.Currency != original.Currency || entry.Ledger != original.Ledger || entry.PoolKey != original.PoolKey || entry.AccountRef != original.AccountRef || entry.FXVersionID != original.FXVersionID || entry.Recurrence != "once" || entry.PeriodFrom < original.PeriodFrom || entry.PeriodTo > original.PeriodTo {
				return ErrFinanceInvalid
			}
		}
		entry.ID = 0
		entry.CreatedAt = time.Now().Unix()
		if err := tx.Create(entry).Error; err != nil {
			return err
		}
		return tx.Create(&FinanceAudit{ActorID: entry.CreatedBy, Action: "expense.append", EntityID: entry.ID, PreviousID: entry.AdjustsID, RequestID: requestID, CreatedAt: entry.CreatedAt}).Error
	})
}
