package model

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// This switch is deliberately separate from the migration and never changes
// balances at startup. The first version shares the single-process boundary of
// reservation review; queued/cache-based quota writers are incompatible.
func TaskSettlementRequested() bool { return os.Getenv("CLEARAPI_TASK_SETTLEMENT") == "true" }

func TaskSettlementReady(db *gorm.DB) error {
	if !TaskSettlementRequested() || !BillingReconciliationEnabled() {
		return ErrFinanceUnavailable
	}
	return TaskSettlementSchemaReady(db)
}

func TaskSettlementSchemaReady(db *gorm.DB) error {
	if err := BillingReconciliationReady(db); err != nil {
		return err
	}
	if !db.Migrator().HasIndex(&TaskSettlement{}, "idx_task_settlement_task") {
		return ErrFinanceUnavailable
	}
	if !db.Migrator().HasIndex(&TaskSettlement{}, "idx_task_settlement_request") {
		return ErrFinanceUnavailable
	}
	if !db.Migrator().HasIndex(&TaskSubscriptionEpoch{}, "idx_task_subscription_epoch") {
		return ErrFinanceUnavailable
	}
	var epochs []TaskSubscriptionEpoch
	if err := db.Select("id", "request_id", "subscription_id", "user_id", "reset_time").Limit(0).Find(&epochs).Error; err != nil {
		return err
	}
	var rows []TaskSettlement
	return db.Select("id", "task_id", "request_id", "user_id", "token_id", "channel_id", "subscription_id", "subscription_reset", "original_quota", "target_quota", "state", "error_class", "attempts", "next_attempt", "reason", "facts", "token_outcome", "log_payload", "log_state", "log_attempts", "log_next_attempt", "created_at", "updated_at").Limit(0).Find(&rows).Error
}

type TaskSettlement struct {
	ID                int64   `json:"id"`
	TaskID            int64   `json:"task_id" gorm:"uniqueIndex:idx_task_settlement_task"`
	RequestID         string  `json:"request_id" gorm:"type:varchar(128);index"`
	RequestKey        *string `json:"-" gorm:"type:varchar(128);uniqueIndex:idx_task_settlement_request"`
	UserID            int     `json:"user_id"`
	TokenID           int     `json:"token_id"`
	ChannelID         int     `json:"channel_id"`
	SubscriptionID    int     `json:"subscription_id"`
	SubscriptionReset int64   `json:"-"`
	OriginalQuota     int     `json:"original_quota"`
	TargetQuota       *int    `json:"target_quota"`
	State             string  `json:"state" gorm:"type:varchar(24);index"`
	ErrorClass        string  `json:"error_class" gorm:"type:varchar(40)"`
	Attempts          int     `json:"attempts"`
	NextAttempt       int64   `json:"next_attempt"`
	Reason            string  `json:"reason" gorm:"type:varchar(500)"`
	Facts             string  `json:"-" gorm:"type:text"`
	TokenOutcome      string  `json:"token_outcome" gorm:"type:varchar(24)"`
	LogPayload        string  `json:"-" gorm:"type:text"`
	LogState          string  `json:"log_state" gorm:"type:varchar(24)"`
	LogAttempts       int     `json:"log_attempts"`
	LogNextAttempt    int64   `json:"log_next_attempt"`
	CreatedAt         int64   `json:"created_at"`
	UpdatedAt         int64   `json:"updated_at"`
}

// Captured in the pre-consume transaction, before task insertion can cross a
// reset boundary. It contains identities and an accounting epoch only.
type TaskSubscriptionEpoch struct {
	ID             int64
	RequestID      string `gorm:"type:varchar(64);uniqueIndex:idx_task_subscription_epoch"`
	SubscriptionID int
	UserID         int
	ResetTime      int64
}

// Explicit, additive migration. The released Task table is a prerequisite;
// there is no backfill that treats historical terminal tasks as owed refunds.
func MigrateTaskSettlements(db *gorm.DB) error {
	if err := BillingReconciliationReady(db); err != nil {
		return err
	}
	if !db.Migrator().HasTable(&Task{}) {
		return ErrFinanceUnavailable
	}
	return db.AutoMigrate(&TaskSettlement{}, &TaskSubscriptionEpoch{})
}

func createTaskSettlement(tx *gorm.DB, task *Task, historical bool) error {
	if task.Quota < 0 || task.Quota > math.MaxInt32 || (task.PrivateData.BillingSource != "" && task.PrivateData.BillingSource != "wallet" && task.PrivateData.BillingSource != "subscription") {
		return ErrFinanceInvalid
	}
	state := "running"
	if task.Status == TaskStatusSuccess || task.Status == TaskStatusFailure {
		state = "awaiting_facts"
	}
	r := TaskSettlement{TaskID: task.ID, UserID: task.UserId, TokenID: task.PrivateData.TokenId, ChannelID: task.ChannelId, OriginalQuota: task.Quota, State: state, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()}
	if task.PrivateData.Execution != nil {
		r.RequestID = task.PrivateData.Execution.RequestID
	}
	if r.RequestID != "" {
		r.RequestKey = &r.RequestID
	} else {
		r.State, r.ErrorClass = "needs_review", "request_identity_missing"
	}
	if task.PrivateData.BillingSource == "subscription" {
		r.SubscriptionID = task.PrivateData.SubscriptionId
		var sub UserSubscription
		if err := lockForUpdate(tx).Where("id = ? AND user_id = ?", r.SubscriptionID, r.UserID).First(&sub).Error; err != nil && !historical {
			return err
		}
		r.SubscriptionReset = sub.LastResetTime
		var epoch TaskSubscriptionEpoch
		if err := tx.Where("request_id = ? AND subscription_id = ? AND user_id = ?", r.RequestID, r.SubscriptionID, r.UserID).First(&epoch).Error; err == nil {
			r.SubscriptionReset = epoch.ResetTime
			if sub.LastResetTime != epoch.ResetTime {
				r.State, r.ErrorClass = "needs_review", "subscription_reset"
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		} else {
			r.State, r.ErrorClass = "needs_review", "subscription_epoch_unknown"
		}
	}
	if historical {
		r.State, r.ErrorClass = "needs_review", "historical_unverified"
	}
	return tx.Create(&r).Error
}

func ListTaskSettlements(db *gorm.DB, scope string, offset, limit int) ([]TaskSettlement, int64, error) {
	if err := TaskSettlementReady(db); err != nil {
		return nil, 0, err
	}
	if offset < 0 || limit < 1 || limit > 100 || (scope != "pending" && scope != "applied") {
		return nil, 0, ErrFinanceInvalid
	}
	query := db.Model(&TaskSettlement{})
	if scope == "applied" {
		query = query.Where("state = ?", "applied")
	} else {
		query = query.Where("state IN ? OR (state = ? AND log_state IN ?)", []string{"awaiting_facts", "pending", "needs_review"}, "applied", []string{"pending", "needs_review"})
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return nil, 0, err
	}
	var rows []TaskSettlement
	err := query.Order("id desc").Offset(offset).Limit(limit).Find(&rows).Error
	return rows, count, err
}

// FreezeTaskSettlement stores the first final amount and safe log projection
// before moving money. A replay may only use that same immutable target.
func FreezeTaskSettlement(db *gorm.DB, taskID int64, target int, reason, facts string, log Log) (*TaskSettlement, error) {
	if err := TaskSettlementReady(db); err != nil {
		return nil, err
	}
	if target < 0 || target > math.MaxInt32 || len(reason) > 500 || len(facts) > 16384 {
		return nil, ErrFinanceInvalid
	}
	if facts == "" {
		facts = "{}"
	}
	var r TaskSettlement
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("task_id = ?", taskID).First(&r).Error; err != nil {
			return err
		}
		if r.TargetQuota != nil {
			if *r.TargetQuota != target {
				return ErrFinanceConflict
			}
			return nil
		}
		if r.State != "awaiting_facts" {
			return ErrFinanceConflict
		}
		delta := target - r.OriginalQuota
		log.UserId, log.ChannelId, log.TokenId, log.CreatedAt = r.UserID, r.ChannelID, r.TokenID, time.Now().Unix()
		var user User
		if err := tx.Unscoped().Select("id", "username").First(&user, r.UserID).Error; err != nil {
			return err
		}
		log.Username = user.Username
		if r.TokenID > 0 {
			var token Token
			err := tx.Unscoped().Select("id", "name").First(&token, r.TokenID).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			log.TokenName = token.Name
		}
		log.Type, log.Quota = LogTypeConsume, delta
		if delta < 0 {
			log.Type, log.Quota = LogTypeRefund, -delta
		}
		payload, err := common.Marshal(log)
		if err != nil {
			return err
		}
		result := tx.Model(&TaskSettlement{}).Where("id = ? AND state = ? AND target_quota IS NULL", r.ID, r.State).Updates(map[string]any{"target_quota": target, "state": "pending", "reason": reason, "facts": facts, "log_payload": string(payload), "updated_at": time.Now().Unix()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrFinanceConflict
		}
		r.TargetQuota, r.State, r.Reason, r.Facts, r.LogPayload = &target, "pending", reason, facts, string(payload)
		return nil
	})
	return &r, err
}

type taskSettlementProblem string

func (p taskSettlementProblem) Error() string { return string(p) }

// ApplyTaskSettlement commits the original funding, token, usage counters,
// task quota and durable audit/observation together. A lost response is safe
// to retry; a previously applied intent is never applied twice.
func ApplyTaskSettlement(db *gorm.DB, id int64) (*TaskSettlement, error) {
	if err := TaskSettlementReady(db); err != nil {
		return nil, err
	}
	// Share the export writer's mutex so its hourly cache flush cannot create
	// or update the same bucket concurrently. This feature requires one process.
	export := common.DataExportEnabled && common.LogConsumeEnabled
	if export {
		CacheQuotaDataLock.Lock()
		defer CacheQuotaDataLock.Unlock()
	}
	var r TaskSettlement
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).First(&r, id).Error; err != nil {
			return err
		}
		if r.State == "applied" {
			return nil
		}
		if r.State != "pending" || r.TargetQuota == nil {
			return ErrFinanceConflict
		}
		claim := tx.Model(&TaskSettlement{}).Where("id = ? AND state = ?", id, "pending").Update("state", "applying")
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected != 1 {
			return ErrFinanceConflict
		}
		var task Task
		if err := lockForUpdate(tx).First(&task, r.TaskID).Error; err != nil {
			return taskSettlementProblem("task_missing")
		}
		if (task.Status != TaskStatusSuccess && task.Status != TaskStatusFailure) || task.Quota != r.OriginalQuota || task.UserId != r.UserID || task.ChannelId != r.ChannelID || task.PrivateData.TokenId != r.TokenID || (task.PrivateData.BillingSource == "subscription") != (r.SubscriptionID > 0) || task.PrivateData.SubscriptionId != r.SubscriptionID {
			return taskSettlementProblem("task_conflict")
		}
		if task.PrivateData.Execution == nil || task.PrivateData.Execution.RequestID != r.RequestID {
			return taskSettlementProblem("task_conflict")
		}
		delta := *r.TargetQuota - r.OriginalQuota
		var reservation BillingReservation
		if r.RequestID != "" && r.SubscriptionID == 0 {
			if err := lockForUpdate(tx).Where("request_id = ?", r.RequestID).First(&reservation).Error; err != nil {
				return taskSettlementProblem("reservation_missing")
			}
			if reservation.State != "settled" || reservation.Applied != r.OriginalQuota || reservation.UserID != r.UserID || reservation.TokenID != r.TokenID {
				return taskSettlementProblem("reservation_conflict")
			}
		}
		if delta != 0 {
			if err := adjustTaskSettlementFunding(tx, &r, delta); err != nil {
				return err
			}
			if err := adjustTaskSettlementToken(tx, &r, delta); err != nil {
				return err
			}
			user := tx.Unscoped().Model(&User{}).Where("id = ? AND used_quota >= ? AND used_quota <= ?", r.UserID, max(-delta, 0), common.MaxWalletQuota-int64(max(delta, 0))).Update("used_quota", gorm.Expr("used_quota + ?", delta))
			if user.Error != nil {
				return user.Error
			}
			if user.RowsAffected != 1 {
				return taskSettlementProblem("usage_conflict")
			}
			if r.ChannelID > 0 {
				channel := tx.Model(&Channel{}).Where("id = ? AND used_quota >= ? AND used_quota <= ?", r.ChannelID, max(-delta, 0), common.MaxWalletQuota-int64(max(delta, 0))).Update("used_quota", gorm.Expr("used_quota + ?", delta))
				if channel.Error != nil {
					return channel.Error
				}
				if channel.RowsAffected != 1 {
					return taskSettlementProblem("usage_conflict")
				}
			}
		}
		if reservation.ID != 0 {
			if err := tx.Model(&reservation).Updates(map[string]any{"state": "task_settled", "applied": *r.TargetQuota, "target": *r.TargetQuota, "updated_at": time.Now().Unix()}).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&task).Update("quota", *r.TargetQuota).Error; err != nil {
			return err
		}
		if delta > 0 && export {
			if err := saveTaskSettlementExport(tx, &r, &task, delta); err != nil {
				return err
			}
		}
		if err := tx.Create(&FinanceAudit{Action: "task.settle", EntityID: r.ID, PreviousID: r.TaskID, RequestID: r.RequestID, CreatedAt: time.Now().Unix()}).Error; err != nil {
			return err
		}
		payload, err := common.Marshal(map[string]any{"task_id": r.TaskID, "settlement_id": r.ID, "delta": delta, "actual_quota": *r.TargetQuota, "pre_consumed_quota": r.OriginalQuota, "reason": r.Reason, "usage_facts": common.RawMessage(r.Facts)})
		if err != nil {
			return err
		}
		if err := tx.Create(&FinanceRequestEvent{EventKey: fmt.Sprintf("task-settlement-%d", r.ID), RequestID: r.RequestID, UserID: r.UserID, Phase: "task_adjustment", At: time.Now().Unix(), Payload: string(payload)}).Error; err != nil {
			return err
		}
		if FinanceSyncReady(tx) == nil {
			if err := tx.Create(&FinanceSyncEvent{EventKey: fmt.Sprintf("task-settlement-sync-%d", r.ID), Topic: "task_settlement", UserID: r.UserID, RefID: r.ID, At: time.Now().Unix(), Payload: string(payload), Status: FinanceSyncNeedsReview, ErrorClass: "task_settlement_requires_review", CreatedAt: time.Now().Unix()}).Error; err != nil {
				return err
			}
		}
		r.State, r.ErrorClass, r.UpdatedAt = "applied", "", time.Now().Unix()
		r.LogState = "pending"
		if delta == 0 || (delta > 0 && !common.LogConsumeEnabled) {
			r.LogState = "delivered"
		}
		return tx.Model(&r).Updates(map[string]any{"state": r.State, "error_class": "", "log_state": r.LogState, "token_outcome": r.TokenOutcome, "updated_at": r.UpdatedAt}).Error
	})
	if err == nil || errors.Is(err, ErrFinanceConflict) {
		return &r, err
	}
	class, state := "database_error", "pending"
	var problem taskSettlementProblem
	if errors.As(err, &problem) {
		class, state = string(problem), "needs_review"
	}
	// This follow-up changes no money. If storage is still unavailable, the
	// frozen pending intent remains recoverable instead of claiming success.
	attempts := r.Attempts + 1
	if attempts >= 5 {
		state = "needs_review"
	}
	record := db.Model(&TaskSettlement{}).Where("id = ? AND state = ? AND attempts = ?", id, "pending", r.Attempts).Updates(map[string]any{"state": state, "error_class": class, "attempts": attempts, "next_attempt": time.Now().Unix() + int64(15*(1<<min(attempts, 5))), "updated_at": time.Now().Unix()})
	if record.Error != nil {
		return &r, record.Error
	}
	if state == "needs_review" {
		return &r, ErrFinanceConflict
	}
	return &r, err
}

// Write the positive adjustment to the existing hourly dashboard bucket in
// the settlement transaction. A retry therefore cannot duplicate exported usage.
func saveTaskSettlementExport(tx *gorm.DB, r *TaskSettlement, task *Task, delta int) error {
	var log Log
	if err := common.Unmarshal([]byte(r.LogPayload), &log); err != nil {
		return err
	}
	nodeName := task.PrivateData.NodeName
	if nodeName == "" {
		nodeName = common.NodeName
	}
	row := QuotaData{UserID: r.UserID, Username: log.Username, ModelName: log.ModelName, CreatedAt: log.CreatedAt - log.CreatedAt%3600, UseGroup: log.Group, TokenID: r.TokenID, ChannelID: r.ChannelID, NodeName: nodeName, Count: 1, Quota: delta}
	var existing QuotaData
	err := lockForUpdate(tx).Where("user_id = ? AND username = ? AND model_name = ? AND created_at = ? AND use_group = ? AND token_id = ? AND channel_id = ? AND node_name = ?", row.UserID, row.Username, row.ModelName, row.CreatedAt, row.UseGroup, row.TokenID, row.ChannelID, row.NodeName).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return tx.Create(&row).Error
	}
	if err != nil {
		return err
	}
	return tx.Model(&existing).Updates(map[string]any{"count": gorm.Expr("count + 1"), "quota": gorm.Expr("quota + ?", delta)}).Error
}

func adjustTaskSettlementFunding(tx *gorm.DB, r *TaskSettlement, delta int) error {
	if r.SubscriptionID > 0 {
		var sub UserSubscription
		if err := lockForUpdate(tx).Where("id = ? AND user_id = ?", r.SubscriptionID, r.UserID).First(&sub).Error; err != nil {
			return taskSettlementProblem("subscription_missing")
		}
		if sub.LastResetTime != r.SubscriptionReset {
			return taskSettlementProblem("subscription_reset")
		}
		if sub.AmountUsed < 0 || sub.AmountUsed < int64(max(-delta, 0)) || sub.AmountUsed > common.MaxWalletQuota-int64(max(delta, 0)) {
			return taskSettlementProblem("subscription_conflict")
		}
		used := sub.AmountUsed + int64(delta)
		if sub.AmountTotal > 0 && used > sub.AmountTotal {
			return taskSettlementProblem("subscription_insufficient")
		}
		return tx.Model(&sub).Update("amount_used", used).Error
	}
	query := tx.Unscoped().Model(&User{}).Where("id = ? AND quota >= ? AND quota <= ?", r.UserID, max(delta, 0), common.MaxWalletQuota-int64(max(-delta, 0)))
	result := query.Update("quota", gorm.Expr("quota - ?", delta))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return taskSettlementProblem("wallet_insufficient_or_limit")
	}
	return nil
}

func adjustTaskSettlementToken(tx *gorm.DB, r *TaskSettlement, delta int) error {
	if r.TokenID <= 0 {
		r.TokenOutcome = "not_required"
		return nil
	}
	var token Token
	err := lockForUpdate(tx.Unscoped()).Where("id = ? AND user_id = ?", r.TokenID, r.UserID).First(&token).Error
	if errors.Is(err, gorm.ErrRecordNotFound) && delta < 0 {
		r.TokenOutcome = "token_missing"
		return nil
	}
	if err != nil {
		return taskSettlementProblem("token_missing")
	}
	if (!token.UnlimitedQuota && delta > 0 && token.RemainQuota < delta) || token.UsedQuota < max(-delta, 0) || int64(token.UsedQuota) > common.MaxWalletQuota-int64(max(delta, 0)) || int64(token.RemainQuota) > common.MaxWalletQuota-int64(max(-delta, 0)) || int64(token.RemainQuota) < -common.MaxWalletQuota+int64(max(delta, 0)) {
		return taskSettlementProblem("token_quota_conflict")
	}
	r.TokenOutcome = "adjusted"
	return tx.Unscoped().Model(&token).Updates(map[string]any{"remain_quota": gorm.Expr("remain_quota - ?", delta), "used_quota": gorm.Expr("used_quota + ?", delta)}).Error
}

type TaskSettlementRetryInput struct {
	LogOnly             bool   `json:"log_only"`
	ExpectedLogAttempts int    `json:"expected_log_attempts"`
	ExpectedAttempts    int    `json:"expected_attempts"`
	ExpectedTarget      int    `json:"expected_target"`
	Reason              string `json:"reason"`
	Confirmed           bool   `json:"confirmed"`
}

// The operator can retry a frozen amount, never invent an unknown amount or
// clear a historical ambiguity through this endpoint.
func RetryTaskSettlement(db *gorm.DB, id int64, actor int, input TaskSettlementRetryInput) (*TaskSettlement, error) {
	if !common.IsFinanceOwner(actor) {
		return nil, ErrFinanceInvalid
	}
	if err := TaskSettlementReady(db); err != nil {
		return nil, err
	}
	if !input.Confirmed || strings.TrimSpace(input.Reason) == "" || len(input.Reason) > 500 || !utf8.ValidString(input.Reason) {
		return nil, ErrFinanceInvalid
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		var r TaskSettlement
		if err := lockForUpdate(tx).First(&r, id).Error; err != nil {
			return ErrFinanceInvalid
		}
		action := "task.retry"
		if input.LogOnly {
			if r.State != "applied" || r.LogState != "needs_review" || r.LogAttempts != input.ExpectedLogAttempts || r.TargetQuota == nil || *r.TargetQuota != input.ExpectedTarget {
				return ErrFinanceConflict
			}
			if err := tx.Model(&r).Updates(map[string]any{"log_state": "pending", "log_next_attempt": 0}).Error; err != nil {
				return err
			}
			action = "task.log_retry"
		} else {
			if r.State != "needs_review" || r.TargetQuota == nil || *r.TargetQuota != input.ExpectedTarget || r.Attempts != input.ExpectedAttempts || r.ErrorClass == "historical_unverified" {
				return ErrFinanceConflict
			}
			result := tx.Model(&r).Where("state = ? AND attempts = ?", "needs_review", r.Attempts).Updates(map[string]any{"state": "pending", "next_attempt": 0, "updated_at": time.Now().Unix()})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrFinanceConflict
			}
		}
		payload, err := common.Marshal(map[string]any{"reason": input.Reason, "target": input.ExpectedTarget, "attempts": input.ExpectedAttempts, "log_only": input.LogOnly, "log_attempts": input.ExpectedLogAttempts})
		if err != nil {
			return err
		}
		audit := FinanceAudit{ActorID: actor, Action: action, EntityID: id, PreviousID: r.TaskID, RequestID: r.RequestID, CreatedAt: time.Now().Unix()}
		if err := tx.Create(&audit).Error; err != nil {
			return err
		}
		return tx.Create(&FinanceRequestEvent{EventKey: fmt.Sprintf("task-retry-%d", audit.ID), RequestID: r.RequestID, UserID: r.UserID, Phase: "task_retry", At: audit.CreatedAt, Payload: string(payload)}).Error
	})
	if err != nil {
		return nil, err
	}
	return ApplyTaskSettlement(db, id)
}
