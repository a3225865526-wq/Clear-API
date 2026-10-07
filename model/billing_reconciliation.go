package model

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Management is opt-in for one gateway process. This registry is not a
// distributed lease: never enable the management switch on shared replicas.
var billingExecutionMu sync.Mutex
var billingExecutions = make(map[string]bool)

const BillingExecutionContextKey = "billing_execution_request_ids"

func EndBillingRequestExecution(requestID string) {
	billingExecutionMu.Lock()
	delete(billingExecutions, requestID)
	billingExecutionMu.Unlock()
}

func BillingReconciliationEnabled() bool {
	return os.Getenv("CLEARAPI_BILLING_RECONCILIATION_SINGLE_INSTANCE") == "true" &&
		os.Getenv("CLEARAPI_DURABLE_WALLET") == "true" && !common.RedisEnabled && !common.BatchUpdateEnabled
}

// Markers are append-only and survive full-reservation settlement.
type BillingReviewMarker struct {
	ID            int64
	ReservationID int64  `gorm:"uniqueIndex:idx_billing_review_reservation"`
	Reason        string `gorm:"type:varchar(32)"`
	CreatedAt     int64
}

// One final decision per reservation. Original billing facts are snapshotted;
// the only mutable part of the reservation is its reviewed state/applied total.
type BillingReservationDecision struct {
	ID            int64  `json:"id"`
	ReservationID int64  `json:"reservation_id" gorm:"uniqueIndex:idx_billing_decision_reservation"`
	UserID        int    `json:"user_id"`
	OriginalState string `json:"original_state" gorm:"type:varchar(24)"`
	Reserved      int    `json:"reserved"`
	Target        int    `json:"target"`
	Applied       int    `json:"applied"`
	RefundQuota   int    `json:"refund_quota"`
	FinalQuota    int    `json:"final_quota"`
	Reason        string `json:"reason" gorm:"type:varchar(500)"`
	TokenRefund   string `json:"token_refund" gorm:"type:varchar(24)"`
	ActorID       int    `json:"actor_id"`
	CreatedAt     int64  `json:"created_at"`
}

// billingReadyMu serializes schema readiness probes: gorm v1.25.12
// Schema.ParseIndexes (reached through Migrator.HasIndex) mutates the shared
// schema index cache without synchronization, so concurrent probes on one
// *gorm.DB are a data race. The probes are idempotent reads; serializing
// them costs nothing meaningful and keeps concurrent finance-owner actions
// off the racy gorm path.
var billingReadyMu sync.Mutex

func BillingReconciliationReady(db *gorm.DB) error {
	billingReadyMu.Lock()
	defer billingReadyMu.Unlock()
	if err := BillingReservationsReady(db); err != nil {
		return err
	}
	if err := FinanceRequestsReady(db); err != nil {
		return err
	}
	if !db.Migrator().HasIndex(&BillingReviewMarker{}, "idx_billing_review_reservation") ||
		!db.Migrator().HasIndex(&BillingReservationDecision{}, "idx_billing_decision_reservation") {
		return ErrFinanceUnavailable
	}
	var markers []BillingReviewMarker
	if err := db.Select("id", "reservation_id", "reason", "created_at").Limit(0).Find(&markers).Error; err != nil {
		return err
	}
	var decisions []BillingReservationDecision
	return db.Select("id", "reservation_id", "user_id", "original_state", "reserved", "target", "applied", "refund_quota", "final_quota", "reason", "token_refund", "actor_id", "created_at").Limit(0).Find(&decisions).Error
}

// Separately approved additive migration; never called at gateway startup.
func MigrateBillingReconciliation(db *gorm.DB) error {
	if err := FinanceRequestsReady(db); err != nil {
		return err
	}
	if err := MigrateBillingReservations(db); err != nil {
		return err
	}
	return db.AutoMigrate(&BillingReviewMarker{}, &BillingReservationDecision{})
}

func MarkBillingUsageUncertain(requestID string) error {
	return MarkBillingReviewMarker(requestID, "upstream_usage_unknown")
}

// MarkBillingReviewMarker appends the first review marker for a reservation.
// Markers are unique per reservation: a later different reason never
// overwrites the original evidence.
func MarkBillingReviewMarker(requestID string, reason string) error {
	// Without the additive schema, retain an active reservation for review
	// rather than silently settling an untraceable unknown-usage charge.
	var r BillingReservation
	if err := DB.Where("request_id = ?", requestID).First(&r).Error; err != nil {
		return err
	}
	if len(reason) > 32 {
		reason = reason[:32]
	}
	if reason == "" {
		reason = "usage_domain_anomaly"
	}
	marker := BillingReviewMarker{ReservationID: r.ID, Reason: reason, CreatedAt: time.Now().Unix()}
	return DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&marker).Error
}

type BillingReservationView struct {
	ID        int64                       `json:"id"`
	RequestID string                      `json:"request_id"`
	UserID    int                         `json:"user_id"`
	TokenID   int                         `json:"token_id"`
	Reserved  int                         `json:"reserved"`
	Target    int                         `json:"target"`
	Applied   int                         `json:"applied"`
	State     string                      `json:"state"`
	UpdatedAt int64                       `json:"updated_at"`
	Reason    string                      `json:"reason"`
	Executing bool                        `json:"executing"`
	Decision  *BillingReservationDecision `json:"decision,omitempty"`
}

func ListBillingReconciliation(db *gorm.DB, scope string, offset, limit int) ([]BillingReservationView, int64, error) {
	if offset < 0 || limit < 1 || limit > 100 || (scope != "pending" && scope != "reviewed") {
		return nil, 0, ErrFinanceInvalid
	}
	if err := BillingReconciliationReady(db); err != nil {
		return nil, 0, err
	}
	query := db.Model(&BillingReservation{})
	if scope == "reviewed" {
		query = query.Where("state = ?", "reviewed")
	} else {
		markers := db.Model(&BillingReviewMarker{}).Select("reservation_id")
		query = query.Where("state IN ? OR (state = ? AND id IN (?))", []string{"active", "needs_review", "settle_pending", "refund_pending"}, "settled", markers)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []BillingReservation
	if err := query.Order("id desc").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	result := make([]BillingReservationView, 0, len(rows))
	for _, r := range rows {
		view := BillingReservationView{ID: r.ID, RequestID: r.RequestID, UserID: r.UserID, TokenID: r.TokenID, Reserved: r.Reserved, Target: r.Target, Applied: r.Applied, State: r.State, UpdatedAt: r.UpdatedAt, Reason: r.State}
		var marker BillingReviewMarker
		if err := db.Where("reservation_id = ?", r.ID).First(&marker).Error; err == nil {
			view.Reason = marker.Reason
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, 0, err
		}
		if scope == "reviewed" {
			var decision BillingReservationDecision
			if err := db.Where("reservation_id = ?", r.ID).First(&decision).Error; err != nil {
				return nil, 0, err
			}
			view.Decision = &decision
		}
		billingExecutionMu.Lock()
		view.Executing = billingExecutions[r.RequestID]
		billingExecutionMu.Unlock()
		result = append(result, view)
	}
	return result, total, nil
}

type BillingDecisionInput struct {
	ExpectedState   string `json:"expected_state"`
	ExpectedApplied int    `json:"expected_applied"`
	ExpectedTarget  int    `json:"expected_target"`
	RefundQuota     int    `json:"refund_quota"`
	Reason          string `json:"reason"`
	ExecutionEnded  bool   `json:"execution_ended"`
}

func DecideBillingReservation(db *gorm.DB, id int64, actorID int, input BillingDecisionInput, requestID string) (*BillingReservationDecision, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if !common.IsFinanceOwner(actorID) || id <= 0 || !input.ExecutionEnded || input.RefundQuota < 0 || input.RefundQuota > math.MaxInt32 || input.Reason == "" || len(input.Reason) > 500 || len(requestID) > 64 {
		return nil, ErrFinanceInvalid
	}
	if !BillingReconciliationEnabled() {
		return nil, ErrFinanceUnavailable
	}
	if err := BillingReconciliationReady(db); err != nil {
		return nil, err
	}
	// Registration precedes the reservation commit. Request completion shares
	// this lock, so a current request cannot be decided during its lifecycle.
	billingExecutionMu.Lock()
	defer billingExecutionMu.Unlock()
	decision := &BillingReservationDecision{}
	err := db.Transaction(func(tx *gorm.DB) error {
		var r BillingReservation
		if err := lockForUpdate(tx).First(&r, id).Error; err != nil {
			return ErrFinanceInvalid
		}
		if billingExecutions[r.RequestID] {
			return ErrFinanceConflict
		}
		// An asynchronous task owns this charge after submission. Even when
		// its HTTP request has ended, reservation review must not refund it.
		if tx.Migrator().HasTable(&TaskSettlement{}) {
			var count int64
			if err := tx.Model(&TaskSettlement{}).Where("request_id = ?", r.RequestID).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return ErrFinanceConflict
			}
		}
		if err := tx.Where("reservation_id = ?", id).First(decision).Error; err == nil {
			if decision.RefundQuota != input.RefundQuota || decision.Reason != input.Reason || decision.OriginalState != input.ExpectedState || decision.Applied != input.ExpectedApplied || decision.Target != input.ExpectedTarget {
				return ErrFinanceConflict
			}
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if r.State != input.ExpectedState || r.Applied != input.ExpectedApplied || r.Target != input.ExpectedTarget || r.Applied < 0 || r.Applied > r.Reserved || input.RefundQuota > r.Applied {
			return ErrFinanceConflict
		}
		switch r.State {
		case "active", "needs_review", "settle_pending", "refund_pending":
		case "settled":
			var count int64
			if err := tx.Model(&BillingReviewMarker{}).Where("reservation_id = ?", id).Count(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				return ErrFinanceConflict
			}
		default:
			return ErrFinanceConflict
		}
		final := r.Applied - input.RefundQuota
		claim := tx.Model(&BillingReservation{}).Where("id = ? AND state = ? AND applied = ? AND target = ?", id, r.State, r.Applied, r.Target).Updates(map[string]any{"state": "reviewed", "applied": final, "updated_at": time.Now().Unix()})
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected != 1 {
			return ErrFinanceConflict
		}
		tokenRefund := "not_required"
		if input.RefundQuota > 0 {
			credit := input.RefundQuota
			user := tx.Unscoped().Model(&User{}).Where("id = ? AND quota >= 0 AND quota <= ?", r.UserID, common.MaxWalletQuota-int64(credit)).Update("quota", gorm.Expr("quota + ?", credit))
			if user.Error != nil {
				return user.Error
			}
			if user.RowsAffected != 1 {
				return ErrFinanceConflict
			}
			var token Token
			err := lockForUpdate(tx.Unscoped()).Where("id = ? AND user_id = ?", r.TokenID, r.UserID).First(&token).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				tokenRefund = "token_missing"
			} else if err != nil {
				return err
			} else {
				result := tx.Unscoped().Model(&Token{}).Where("id = ? AND user_id = ? AND used_quota >= ? AND remain_quota <= ?", r.TokenID, r.UserID, credit, common.MaxWalletQuota-int64(credit)).Updates(map[string]any{"remain_quota": gorm.Expr("remain_quota + ?", credit), "used_quota": gorm.Expr("used_quota - ?", credit)})
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected != 1 {
					return ErrFinanceConflict
				}
				tokenRefund = "credited"
			}
		}
		*decision = BillingReservationDecision{ReservationID: id, UserID: r.UserID, OriginalState: r.State, Reserved: r.Reserved, Target: r.Target, Applied: r.Applied, RefundQuota: input.RefundQuota, FinalQuota: final, Reason: input.Reason, TokenRefund: tokenRefund, ActorID: actorID, CreatedAt: time.Now().Unix()}
		if err := tx.Create(decision).Error; err != nil {
			return err
		}
		if err := tx.Create(&FinanceAudit{ActorID: actorID, Action: "billing.decide", EntityID: decision.ID, PreviousID: id, RequestID: requestID, CreatedAt: decision.CreatedAt}).Error; err != nil {
			return err
		}
		if input.RefundQuota > 0 {
			// Mark the separate observation ledger too. Existing import code
			// rejects charges with later refund observations instead of
			// treating the historical gross charge as final revenue.
			payload, err := common.Marshal(map[string]any{"reservation_id": id, "decision_id": decision.ID, "refund_quota": input.RefundQuota, "charged_quota": final, "status": "refunded", "funding_source": "wallet", "scope": "billing_reconciliation"})
			if err != nil {
				return err
			}
			if err := tx.Create(&FinanceRequestEvent{EventKey: fmt.Sprintf("billing-review-refund-%d", decision.ID), RequestID: r.RequestID, UserID: r.UserID, Phase: "funding_refund", At: decision.CreatedAt, Payload: string(payload)}).Error; err != nil {
				return err
			}
			if FinanceSyncReady(tx) == nil {
				return tx.Create(&FinanceSyncEvent{EventKey: fmt.Sprintf("billing-review-sync-%d", decision.ID), Topic: "billing_reconciliation", UserID: r.UserID, RefID: decision.ID, At: decision.CreatedAt, Payload: string(payload), Status: FinanceSyncNeedsReview, ErrorClass: "refund_requires_review", CreatedAt: decision.CreatedAt}).Error
			}
			return nil
		}
		return nil
	})
	return decision, err
}
