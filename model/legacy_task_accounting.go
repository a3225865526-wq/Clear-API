package model

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// AdjustLegacyTaskAccounting commits the task marker and quota mutations
// together. A stale poller cannot apply its delta a second time.
// Subscription tasks without an original accounting epoch require review.
func AdjustLegacyTaskAccounting(task *Task, target int) (bool, error) {
	if task == nil || task.ID <= 0 || target < 0 || target > math.MaxInt32 {
		return false, ErrBillingReservationConflict
	}
	changed := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var stored Task
		if err := lockForUpdate(tx).First(&stored, task.ID).Error; err != nil {
			return err
		}
		if stored.UserId != task.UserId || stored.ChannelId != task.ChannelId || stored.PrivateData.TokenId != task.PrivateData.TokenId {
			return ErrBillingReservationConflict
		}
		if stored.Quota == target {
			return nil
		}
		if stored.Quota != task.Quota || stored.Quota < 0 || stored.Quota > math.MaxInt32 {
			return ErrBillingReservationConflict
		}
		if stored.PrivateData.SubscriptionId > 0 || stored.PrivateData.BillingSource == "subscription" {
			return errors.New("legacy subscription task requires verified settlement epoch")
		}
		delta := target - stored.Quota
		r := TaskSettlement{UserID: stored.UserId, TokenID: stored.PrivateData.TokenId, ChannelID: stored.ChannelId}
		if err := adjustTaskSettlementFunding(tx, &r, delta); err != nil {
			return err
		}
		if err := adjustTaskSettlementToken(tx, &r, delta); err != nil {
			return err
		}
		for _, update := range []*gorm.DB{
			tx.Unscoped().Model(&User{}).Where("id = ? AND used_quota >= ? AND used_quota <= ?", stored.UserId, max(-delta, 0), common.MaxWalletQuota-int64(max(delta, 0))).Update("used_quota", gorm.Expr("used_quota + ?", delta)),
			tx.Model(&Channel{}).Where("id = ? AND used_quota >= ? AND used_quota <= ?", stored.ChannelId, max(-delta, 0), common.MaxWalletQuota-int64(max(delta, 0))).Update("used_quota", gorm.Expr("used_quota + ?", delta)),
		} {
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected != 1 {
				return ErrBillingReservationConflict
			}
		}
		result := tx.Model(&Task{}).Where("id = ? AND quota = ?", stored.ID, stored.Quota).Update("quota", target)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrBillingReservationConflict
		}
		if BillingOutboxEnabled() {
			if err := saveLegacyBillingMovement(tx, fmt.Sprintf("legacy-task-%d-%d-%d", stored.ID, stored.Quota, target), stored.UserId, stored.PrivateData.TokenId, stored.ChannelId, stored.Quota, target); err != nil {
				return err
			}
		}
		changed = true
		return nil
	})
	if err == nil {
		task.Quota = target
	}
	return changed, err
}

// RefundLegacyMidjourneyAccounting also claims the persisted marker inside
// the transaction. Uncharged preparations (token_id=0) cannot mint refunds.
func RefundLegacyMidjourneyAccounting(task *Midjourney) (bool, error) {
	if task == nil || task.Id <= 0 {
		return false, ErrBillingReservationConflict
	}
	changed := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var stored Midjourney
		if err := lockForUpdate(tx).First(&stored, task.Id).Error; err != nil {
			return err
		}
		if stored.UserId != task.UserId {
			return ErrBillingReservationConflict
		}
		if stored.Quota == 0 {
			return nil
		}
		if stored.Quota < 0 || stored.Quota > math.MaxInt32 || stored.TokenId <= 0 || stored.Quota != task.Quota {
			return ErrBillingReservationConflict
		}
		r := TaskSettlement{UserID: stored.UserId, TokenID: stored.TokenId, ChannelID: stored.GetBillingChannelId()}
		delta := -stored.Quota
		if err := adjustTaskSettlementFunding(tx, &r, delta); err != nil {
			return err
		}
		if err := adjustTaskSettlementToken(tx, &r, delta); err != nil {
			return err
		}
		for _, result := range []*gorm.DB{
			tx.Unscoped().Model(&User{}).Where("id = ? AND used_quota >= ?", r.UserID, stored.Quota).Update("used_quota", gorm.Expr("used_quota + ?", delta)),
			tx.Model(&Channel{}).Where("id = ? AND used_quota >= ?", r.ChannelID, stored.Quota).Update("used_quota", gorm.Expr("used_quota + ?", delta)),
		} {
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrBillingReservationConflict
			}
		}
		result := tx.Model(&Midjourney{}).Where("id = ? AND quota = ?", stored.Id, stored.Quota).Update("quota", 0)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrBillingReservationConflict
		}
		if BillingOutboxEnabled() {
			if err := saveLegacyBillingMovement(tx, fmt.Sprintf("legacy-mj-refund-%d", stored.Id), r.UserID, r.TokenID, r.ChannelID, stored.Quota, 0); err != nil {
				return err
			}
		}
		changed = true
		return nil
	})
	if err == nil {
		task.Quota = 0
	}
	return changed, err
}

func saveLegacyBillingMovement(tx *gorm.DB, requestID string, userID, tokenID, channelID, original, target int) error {
	log := Log{UserId: userID, TokenId: tokenID, ChannelId: channelID, RequestId: requestID, CreatedAt: time.Now().Unix(), Type: LogTypeConsume, Quota: target - original, Content: "Atomic legacy task adjustment", Other: "{}"}
	if log.Quota < 0 {
		log.Type, log.Quota = LogTypeRefund, -log.Quota
	}
	payload, err := common.Marshal(&log)
	if err != nil {
		return err
	}
	return tx.Create(&BillingCompletionOutbox{RequestID: requestID, Payload: string(payload), State: "pending"}).Error
}

func ChargeLegacyMidjourneyAccounting(task *Midjourney, tokenID int, requestID string) (bool, error) {
	if task == nil || task.Id <= 0 || tokenID <= 0 {
		return false, ErrBillingReservationConflict
	}
	if requestID == "" {
		requestID = fmt.Sprintf("legacy-mj-charge-%d", task.Id)
	}
	charged := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var stored Midjourney
		if err := lockForUpdate(tx).First(&stored, task.Id).Error; err != nil {
			return err
		}
		if stored.UserId != task.UserId || stored.Quota != task.Quota || stored.Quota < 0 || stored.Quota > math.MaxInt32 {
			return ErrBillingReservationConflict
		}
		if stored.TokenId == tokenID {
			return nil
		}
		if stored.TokenId != 0 {
			return ErrBillingReservationConflict
		}
		var token Token
		if err := lockForUpdate(tx).Where("id = ? AND user_id = ? AND status = ? AND (expired_time = -1 OR expired_time > ?)", tokenID, stored.UserId, common.TokenStatusEnabled, time.Now().Unix()).First(&token).Error; err != nil {
			return err
		}
		r := TaskSettlement{UserID: stored.UserId, TokenID: tokenID, ChannelID: stored.GetBillingChannelId()}
		if err := adjustTaskSettlementFunding(tx, &r, stored.Quota); err != nil {
			return err
		}
		if err := adjustTaskSettlementToken(tx, &r, stored.Quota); err != nil {
			return err
		}
		for _, result := range []*gorm.DB{
			tx.Model(&User{}).Where("id = ? AND status = ? AND used_quota >= 0 AND used_quota <= ?", r.UserID, common.UserStatusEnabled, common.MaxWalletQuota-int64(stored.Quota)).Updates(map[string]any{"used_quota": gorm.Expr("used_quota + ?", stored.Quota), "request_count": gorm.Expr("request_count + 1")}),
			tx.Model(&Channel{}).Where("id = ? AND used_quota >= 0 AND used_quota <= ?", r.ChannelID, common.MaxWalletQuota-int64(stored.Quota)).Update("used_quota", gorm.Expr("used_quota + ?", stored.Quota)),
		} {
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrBillingReservationConflict
			}
		}
		result := tx.Model(&Midjourney{}).Where("id = ? AND token_id = ?", stored.Id, 0).Update("token_id", tokenID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrBillingReservationConflict
		}
		if BillingOutboxEnabled() {
			if err := saveLegacyBillingMovement(tx, requestID, r.UserID, r.TokenID, r.ChannelID, 0, stored.Quota); err != nil {
				return err
			}
		}
		charged = true
		return nil
	})
	if err == nil {
		task.TokenId = tokenID
	}
	return charged, err
}
