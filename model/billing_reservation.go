package model

import (
	"errors"
	"math"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

var ErrBillingReservationConflict = errors.New("billing reservation conflict")
var ErrBillingReservationQuota = errors.New("billing reservation quota insufficient")

// No credentials or request bodies are stored. The reservation and both quota
// mutations commit together. Active reservations never expire into refunds:
// an interrupted upstream request may already have incurred a charge.
type BillingReservation struct {
	ID        int64
	RequestID string `gorm:"type:varchar(128);uniqueIndex:idx_billing_request_id"`
	UserID    int
	TokenID   int
	Reserved  int
	Target    int
	Applied   int
	State     string `gorm:"type:varchar(24);index"`
	UpdatedAt int64
}

// MigrateBillingReservations is explicit; it is not part of startup AutoMigrate.
func MigrateBillingReservations(db *gorm.DB) error {
	return db.AutoMigrate(&BillingReservation{})
}

func BillingReservationsReady(db *gorm.DB) error {
	if db == nil {
		return errors.New("billing database unavailable")
	}
	if !db.Migrator().HasIndex(&BillingReservation{}, "idx_billing_request_id") {
		return errors.New("billing reservation uniqueness index is missing")
	}
	var rows []BillingReservation
	// A zero-row select checks required columns too, rather than only table existence.
	return db.Select("id", "request_id", "user_id", "token_id", "reserved", "target", "applied", "state", "updated_at").Limit(0).Find(&rows).Error
}

func ReserveBillingRequest(requestID string, userID, tokenID, quota int) error {
	if requestID == "" || len(requestID) > 128 || userID <= 0 || tokenID <= 0 || quota < 0 || quota > math.MaxInt32 {
		return errors.New("invalid billing reservation")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		r := BillingReservation{RequestID: requestID, UserID: userID, TokenID: tokenID, Reserved: quota, Applied: quota, State: "active", UpdatedAt: time.Now().Unix()}
		if err := tx.Create(&r).Error; err != nil {
			return err // The unique request ID prevents duplicate submission/reservation.
		}
		if quota == 0 {
			var user User
			if err := lockForUpdate(tx).Select("id").Where("id = ? AND status = ? AND quota >= 0 AND quota <= ?", userID, common.UserStatusEnabled, common.MaxWalletQuota).First(&user).Error; err != nil {
				return err
			}
		} else {
			user := tx.Model(&User{}).Where("id = ? AND status = ? AND quota >= ? AND quota <= ?", userID, common.UserStatusEnabled, quota, common.MaxWalletQuota).
				Update("quota", gorm.Expr("quota - ?", quota))
			if user.Error != nil {
				return user.Error
			}
			if user.RowsAffected != 1 {
				return ErrBillingReservationQuota
			}
		}
		var token Token
		if err := lockForUpdate(tx).Where("id = ? AND user_id = ? AND status = ?", tokenID, userID, common.TokenStatusEnabled).First(&token).Error; err != nil {
			return err
		}
		if token.ExpiredTime != -1 && token.ExpiredTime <= time.Now().Unix() {
			return ErrBillingReservationConflict
		}
		if token.UsedQuota < 0 || int64(token.UsedQuota) > common.MaxWalletQuota-int64(quota) || int64(token.RemainQuota) < -common.MaxWalletQuota+int64(quota) {
			return ErrBillingReservationConflict
		}
		if !token.UnlimitedQuota && token.RemainQuota < quota {
			return ErrBillingReservationQuota
		}
		if quota == 0 {
			return nil
		}
		result := tx.Model(&Token{}).Where("id = ? AND user_id = ?", tokenID, userID).Updates(map[string]any{
			"remain_quota": gorm.Expr("remain_quota - ?", quota), "used_quota": gorm.Expr("used_quota + ?", quota), "accessed_time": time.Now().Unix(),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrBillingReservationConflict
		}
		return nil
	})
}

// RaiseBillingReservation atomically grows an active wallet/token reservation.
func RaiseBillingReservation(requestID string, quota int) error {
	if quota < 0 || quota > math.MaxInt32 {
		return ErrBillingReservationConflict
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var r BillingReservation
		if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&r).Error; err != nil {
			return err
		}
		if r.State != "active" || r.Applied != r.Reserved {
			return ErrBillingReservationConflict
		}
		if quota <= r.Reserved {
			return nil
		}
		delta := quota - r.Reserved
		user := tx.Model(&User{}).Where("id = ? AND status = ? AND quota >= ? AND quota <= ?", r.UserID, common.UserStatusEnabled, delta, common.MaxWalletQuota).Update("quota", gorm.Expr("quota - ?", delta))
		if user.Error != nil {
			return user.Error
		}
		if user.RowsAffected != 1 {
			return ErrBillingReservationQuota
		}
		token := tx.Model(&Token{}).Where("id = ? AND user_id = ? AND status = ? AND (expired_time = -1 OR expired_time > ?) AND (unlimited_quota = ? OR remain_quota >= ?) AND used_quota >= 0 AND used_quota <= ? AND remain_quota >= ?", r.TokenID, r.UserID, common.TokenStatusEnabled, time.Now().Unix(), true, delta, common.MaxWalletQuota-int64(delta), -common.MaxWalletQuota+int64(delta)).Updates(map[string]any{"remain_quota": gorm.Expr("remain_quota - ?", delta), "used_quota": gorm.Expr("used_quota + ?", delta)})
		if token.Error != nil {
			return token.Error
		}
		if token.RowsAffected != 1 {
			return ErrBillingReservationQuota
		}
		claim := tx.Model(&BillingReservation{}).Where("id = ? AND state = ? AND reserved = ?", r.ID, "active", r.Reserved).Updates(map[string]any{"reserved": quota, "applied": quota, "updated_at": time.Now().Unix()})
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected != 1 {
			return ErrBillingReservationConflict
		}
		return nil
	})
}

// RequestBillingCompletion persists an immutable intent for crash recovery.
func RequestBillingCompletion(requestID string, target int, refund bool) error {
	if target < 0 || target > math.MaxInt32 || refund && target != 0 {
		return ErrBillingReservationConflict
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var r BillingReservation
		if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&r).Error; err != nil {
			return err
		}
		pending, done := "settle_pending", "settled"
		if refund {
			pending, done = "refund_pending", "refunded"
		}
		if r.State == pending || r.State == done {
			if r.Target != target {
				return ErrBillingReservationConflict
			}
			return nil
		}
		if r.State != "active" {
			return ErrBillingReservationConflict
		}
		// Do not silently forgive charges beyond the approved reservation. Keep
		// the amount and reservation for operator reconciliation, with no refund.
		if target > r.Reserved {
			pending = "needs_review"
		}
		result := tx.Model(&BillingReservation{}).Where("id = ? AND state = ?", r.ID, "active").Updates(map[string]any{"target": target, "state": pending, "updated_at": time.Now().Unix()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrBillingReservationConflict
		}
		return nil
	})
}

func ApplyBillingCompletion(requestID string) (int, error) {
	var applied int
	err := DB.Transaction(func(tx *gorm.DB) error {
		var r BillingReservation
		if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&r).Error; err != nil {
			return err
		}
		applied = r.Applied
		if r.State == "settled" || r.State == "refunded" {
			return nil
		}
		if r.State != "settle_pending" && r.State != "refund_pending" {
			return ErrBillingReservationConflict
		}
		if r.Target < 0 || r.Target > r.Reserved || r.Applied != r.Reserved {
			return ErrBillingReservationConflict
		}
		state := "settled"
		if r.State == "refund_pending" {
			state = "refunded"
		}
		claim := tx.Model(&BillingReservation{}).Where("id = ? AND state = ?", r.ID, r.State).Updates(map[string]any{"state": state, "applied": r.Target, "updated_at": time.Now().Unix()})
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected != 1 {
			return ErrBillingReservationConflict
		}
		credit := r.Reserved - r.Target
		if credit > 0 {
			user := tx.Unscoped().Model(&User{}).Where("id = ? AND quota <= ?", r.UserID, common.MaxWalletQuota-int64(credit)).Update("quota", gorm.Expr("quota + ?", credit))
			if user.Error != nil {
				return user.Error
			}
			if user.RowsAffected != 1 {
				return ErrBillingReservationConflict
			}
			token := tx.Unscoped().Model(&Token{}).Where("id = ? AND user_id = ? AND used_quota >= ? AND remain_quota <= ?", r.TokenID, r.UserID, credit, common.MaxWalletQuota-int64(credit)).Updates(map[string]any{
				"remain_quota": gorm.Expr("remain_quota + ?", credit), "used_quota": gorm.Expr("used_quota - ?", credit),
			})
			if token.Error != nil {
				return token.Error
			}
			if token.RowsAffected != 1 {
				return ErrBillingReservationConflict
			}
		}
		applied = r.Target
		return nil
	})
	return applied, err
}

func RecoverBillingCompletions(limit int) error {
	if limit < 1 || limit > 100 {
		return errors.New("invalid billing recovery batch")
	}
	var rows []BillingReservation
	if err := DB.Where("state IN ?", []string{"refund_pending", "settle_pending"}).Order("id").Limit(limit).Find(&rows).Error; err != nil {
		return err
	}
	var errs []error
	for _, r := range rows {
		if _, err := ApplyBillingCompletion(r.RequestID); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
