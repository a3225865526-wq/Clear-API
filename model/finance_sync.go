package model

import (
	"errors"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// Finance sync event statuses. pending/processing are the working states,
// done is terminal, blocked waits on an external confirmation (no retry
// counting), needs_review is parked for the owner and never retried.
const (
	FinanceSyncPending     = "pending"
	FinanceSyncProcessing  = "processing"
	FinanceSyncDone        = "done"
	FinanceSyncBlocked     = "blocked"
	FinanceSyncNeedsReview = "needs_review"
)

// Append-only outbox of business facts. Rows are emitted inside the business
// transaction that made the fact true and applied to the ledger exactly once
// by a keyed, revision-checked processor. Payloads stay minimal typed
// snapshots without request bodies or credentials.
type FinanceSyncEvent struct {
	ID            int64  `json:"id"`
	EventKey      string `json:"event_key" gorm:"type:varchar(100);uniqueIndex"`
	Topic         string `json:"topic" gorm:"type:varchar(32);index"`
	UserID        int    `json:"user_id" gorm:"index"`
	RefID         int64  `json:"ref_id" gorm:"index"`
	At            int64  `json:"at"`
	Payload       string `json:"-" gorm:"type:text"`
	Status        string `json:"status" gorm:"type:varchar(16);index"`
	Attempts      int    `json:"attempts"`
	NextAttemptAt int64  `json:"next_attempt_at" gorm:"index"`
	ErrorClass    string `json:"error_class" gorm:"type:varchar(32)"`
	ClaimedAt     int64  `json:"claimed_at"`
	ProcessedAt   int64  `json:"processed_at"`
	CreatedAt     int64  `json:"created_at"`
}

type financeRedemptionPayload struct {
	Quota int64 `json:"quota"`
}

func FinanceSyncReady(db *gorm.DB) error {
	if db == nil {
		return ErrFinanceUnavailable
	}
	var schema FinanceSchema
	if err := db.First(&schema, 1).Error; err != nil || schema.Version < 7 || schema.Version > 9 {
		return ErrFinanceUnavailable
	}
	return nil
}

// Explicit additive migration only; never invoked by normal gateway startup.
func MigrateFinanceSync(db *gorm.DB) error {
	if err := MigrateFinanceRefunds(db); err != nil {
		return err
	}
	if err := db.AutoMigrate(&FinanceSyncEvent{}); err != nil {
		return err
	}
	return db.Model(&FinanceSchema{}).Where("id = ? AND version = ?", 1, 6).Update("version", 7).Error
}

// Emission is atomic with the offline redemption transaction. When finance is
// disabled or the sync schema is absent this is a no-op; a failed insert
// fails the redemption so no quota is ever credited without its source fact.
func EmitFinanceRedemptionEvent(tx *gorm.DB, redemption *Redemption, userID int, redeemedAt int64) error {
	if !common.FinanceEnabled() {
		return nil
	}
	if FinanceSyncReady(tx) != nil {
		return nil
	}
	payload, err := common.Marshal(financeRedemptionPayload{Quota: int64(redemption.Quota)})
	if err != nil {
		return err
	}
	entry := &FinanceSyncEvent{
		EventKey:      "auto-offline-redemption-" + strconv.Itoa(redemption.Id),
		Topic:         "offline_redemption",
		UserID:        userID,
		RefID:         int64(redemption.Id),
		At:            redeemedAt,
		Payload:       string(payload),
		Status:        FinanceSyncPending,
		NextAttemptAt: time.Now().Unix(),
		CreatedAt:     time.Now().Unix(),
	}
	var duplicate FinanceSyncEvent
	if err := tx.Where("event_key = ?", entry.EventKey).First(&duplicate).Error; err == nil {
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return tx.Create(entry).Error
}
