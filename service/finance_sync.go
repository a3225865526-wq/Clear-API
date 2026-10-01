package service

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

const (
	financeSyncBatchSize     = 100
	financeSyncLeaseSeconds  = 60
	financeSyncMaxAttempts   = 5
	financeSyncWorkerSeconds = 10
)

// Outcome classes stay bounded and free of raw database or payload details.
const (
	financeSyncClassReceiptUnconfirmed = "receipt_unconfirmed"
	financeSyncClassOutOfOrder         = "out_of_order"
	financeSyncClassInvalidFact        = "invalid_fact"
	financeSyncClassSettlementOpen     = "settlement_unresolved"
	financeSyncClassRefundReview       = "refund_requires_review"
	financeSyncClassFactConflict       = "fact_conflict"
	financeSyncClassVersionConflict    = "version_conflict"
	financeSyncClassProcessingError    = "processing_error"
	financeSyncClassAttemptsExhausted  = "attempts_exhausted"
)

type financeSyncOutcome struct {
	status string
	class  string
}

func financeSyncBackoff(attempts int) int64 {
	seconds := int64(30) << min(attempts, 6)
	if seconds > 3600 {
		seconds = 3600
	}
	return seconds
}

// One processor pass is crash safe: claiming is a compare-and-swap, the
// ledger application is keyed and idempotent, and progress lives in the
// table, not in process memory. Another worker claiming an event makes our
// result write a no-op; a lost race re-imports through the existing key.
func RunFinanceSyncPass(db *gorm.DB, batchSize int) (int, error) {
	if err := model.FinanceSyncReady(db); err != nil {
		return 0, err
	}
	if batchSize <= 0 || batchSize > 1000 {
		batchSize = financeSyncBatchSize
	}
	now := time.Now().Unix()
	if err := db.Model(&model.FinanceSyncEvent{}).
		Where("status = ? AND claimed_at < ?", model.FinanceSyncProcessing, now-financeSyncLeaseSeconds).
		Updates(map[string]any{"status": model.FinanceSyncPending, "error_class": "reclaimed", "next_attempt_at": now}).Error; err != nil {
		return 0, err
	}
	processed := 0
	for {
		var batch []model.FinanceSyncEvent
		if err := db.Where("status = ? AND next_attempt_at <= ?", model.FinanceSyncPending, now).Order("id asc").Limit(batchSize).Find(&batch).Error; err != nil {
			return processed, err
		}
		if len(batch) == 0 {
			break
		}
		for i := range batch {
			event := batch[i]
			claimed := db.Model(&model.FinanceSyncEvent{}).
				Where("id = ? AND status = ?", event.ID, model.FinanceSyncPending).
				Updates(map[string]any{"status": model.FinanceSyncProcessing, "claimed_at": now})
			if claimed.Error != nil {
				return processed, claimed.Error
			}
			if claimed.RowsAffected == 0 {
				continue // another worker owns this event
			}
			outcome := processFinanceSyncEvent(db, &event)
			if err := applyFinanceSyncOutcome(db, &event, outcome, now); err != nil {
				return processed, err
			}
			processed++
		}
		if len(batch) < batchSize {
			break
		}
	}
	return processed, nil
}

func applyFinanceSyncOutcome(db *gorm.DB, event *model.FinanceSyncEvent, outcome financeSyncOutcome, claimedAt int64) error {
	attempts := event.Attempts + 1
	updates := map[string]any{"attempts": attempts, "error_class": outcome.class}
	switch outcome.status {
	case model.FinanceSyncDone:
		updates["status"] = model.FinanceSyncDone
		updates["processed_at"] = claimedAt
	case model.FinanceSyncBlocked:
		updates["status"] = model.FinanceSyncBlocked
		updates["next_attempt_at"] = 0
	case model.FinanceSyncNeedsReview:
		updates["status"] = model.FinanceSyncNeedsReview
		updates["processed_at"] = claimedAt
	default:
		if attempts >= financeSyncMaxAttempts {
			updates["status"] = model.FinanceSyncNeedsReview
			updates["error_class"] = financeSyncClassAttemptsExhausted
			updates["processed_at"] = claimedAt
		} else {
			updates["status"] = model.FinanceSyncPending
			updates["next_attempt_at"] = claimedAt + financeSyncBackoff(attempts)
		}
	}
	// Tied to our own claim: a stale write after lease reclaim cannot corrupt
	// another attempt's state; the keyed re-import stays exactly-once.
	result := db.Model(&model.FinanceSyncEvent{}).
		Where("id = ? AND status = ? AND claimed_at = ?", event.ID, model.FinanceSyncProcessing, claimedAt).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func financeSyncOutOfOrder(tx *gorm.DB, userID int, at int64) bool {
	var wallet model.FinanceWallet
	if err := tx.Where("user_id = ?", userID).First(&wallet).Error; err != nil {
		return false // no ledger head yet; nothing can be out of order
	}
	return at < wallet.LastAt
}

// resolveFinanceConflict distinguishes "another path already imported this
// keyed fact" (done) from a wallet revision race (retry).
func resolveFinanceConflict(tx *gorm.DB, userID int, eventKey string) (bool, error) {
	var count int64
	if err := tx.Model(&model.FinanceRevenueRecord{}).Where("user_id = ? AND event_key = ?", userID, eventKey).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func processFinanceSyncEvent(db *gorm.DB, event *model.FinanceSyncEvent) financeSyncOutcome {
	switch event.Topic {
	case "offline_redemption":
		return processFinanceSyncRedemption(db, event)
	case "consumption":
		return processFinanceSyncConsumption(db, event)
	default:
		return financeSyncOutcome{model.FinanceSyncNeedsReview, financeSyncClassInvalidFact}
	}
}

// Resolutions separate fact extraction (shared with the controlled rebuild)
// from ledger application. ready carries the resolved fact; done means the
// ledger already reflects it (imported or zero charge); blocked and
// needs_review keep the processor's parking semantics; pending is transient.
const financeSyncReady = "ready"

type financeSyncResolution struct {
	outcome  financeSyncOutcome
	event    *FinanceRevenueEvent
	evidence string
}

func resolveFinanceSyncRedemption(tx *gorm.DB, syncEvent *model.FinanceSyncEvent) financeSyncResolution {
	if syncEvent.UserID <= 0 || syncEvent.UserID > math.MaxInt32 || syncEvent.RefID <= 0 || syncEvent.RefID > math.MaxInt32 || syncEvent.At <= 0 {
		return financeSyncResolution{outcome: financeSyncOutcome{model.FinanceSyncNeedsReview, financeSyncClassInvalidFact}}
	}
	sources, err := PreviewFinanceOfflineSources(tx, syncEvent.UserID, []int{int(syncEvent.RefID)})
	if err != nil {
		switch {
		case errors.Is(err, model.ErrFinanceConflict):
			return financeSyncResolution{outcome: financeSyncOutcome{model.FinanceSyncNeedsReview, financeSyncClassFactConflict}}
		case errors.Is(err, model.ErrFinanceInvalid):
			return financeSyncResolution{outcome: financeSyncOutcome{model.FinanceSyncNeedsReview, financeSyncClassInvalidFact}}
		default:
			return financeSyncResolution{outcome: financeSyncOutcome{model.FinanceSyncPending, financeSyncClassProcessingError}}
		}
	}
	if len(sources) != 1 {
		return financeSyncResolution{outcome: financeSyncOutcome{model.FinanceSyncNeedsReview, financeSyncClassInvalidFact}}
	}
	source := sources[0]
	if source.AlreadyImported {
		return financeSyncResolution{outcome: financeSyncOutcome{model.FinanceSyncDone, ""}}
	}
	if source.Event.ValueCNY == nil {
		return financeSyncResolution{outcome: financeSyncOutcome{model.FinanceSyncBlocked, financeSyncClassReceiptUnconfirmed}}
	}
	evidence := fmt.Sprintf("auto_sync:receipt:%d; offline_order:%d; redemption:%d", source.ReceiptID, source.OrderID, syncEvent.RefID)
	return financeSyncResolution{outcome: financeSyncOutcome{financeSyncReady, ""}, event: &source.Event, evidence: evidence}
}

func resolveFinanceSyncConsumption(tx *gorm.DB, syncEvent *model.FinanceSyncEvent) financeSyncResolution {
	if syncEvent.UserID <= 0 || syncEvent.UserID > math.MaxInt32 || syncEvent.RefID <= 0 {
		return financeSyncResolution{outcome: financeSyncOutcome{model.FinanceSyncNeedsReview, financeSyncClassInvalidFact}}
	}
	sources, err := PreviewFinanceConsumptions(tx, syncEvent.UserID, []int64{syncEvent.RefID})
	if err != nil {
		if errors.Is(err, model.ErrFinanceInvalid) {
			return financeSyncResolution{outcome: financeSyncOutcome{model.FinanceSyncNeedsReview, financeSyncClassInvalidFact}}
		}
		return financeSyncResolution{outcome: financeSyncOutcome{model.FinanceSyncPending, financeSyncClassProcessingError}}
	}
	if len(sources) != 1 {
		return financeSyncResolution{outcome: financeSyncOutcome{model.FinanceSyncNeedsReview, financeSyncClassInvalidFact}}
	}
	source := sources[0]
	switch source.Status {
	case "already_imported":
		return financeSyncResolution{outcome: financeSyncOutcome{model.FinanceSyncDone, ""}}
	case "no_charge":
		return financeSyncResolution{outcome: financeSyncOutcome{model.FinanceSyncDone, "no_charge"}}
	case "refund_requires_review":
		return financeSyncResolution{outcome: financeSyncOutcome{model.FinanceSyncNeedsReview, financeSyncClassRefundReview}}
	case "unknown":
		return financeSyncResolution{outcome: financeSyncOutcome{model.FinanceSyncNeedsReview, financeSyncClassSettlementOpen}}
	}
	evidence := fmt.Sprintf("auto_sync:observation:%d; request_id:%s", source.ObservationID, source.RequestID)
	return financeSyncResolution{outcome: financeSyncOutcome{financeSyncReady, ""}, event: source.Event, evidence: evidence}
}

// Shared application path: out-of-order parking first, then the keyed,
// revision-checked append with conflict classification.
func applyFinanceSyncFact(tx *gorm.DB, syncEvent *model.FinanceSyncEvent, resolution financeSyncResolution) financeSyncOutcome {
	if financeSyncOutOfOrder(tx, syncEvent.UserID, resolution.event.At) {
		return financeSyncOutcome{model.FinanceSyncNeedsReview, financeSyncClassOutOfOrder}
	}
	var wallet model.FinanceWallet
	expected := int64(0)
	if err := tx.Where("user_id = ?", syncEvent.UserID).First(&wallet).Error; err == nil {
		expected = wallet.Revision
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return financeSyncOutcome{model.FinanceSyncPending, financeSyncClassProcessingError}
	}
	_, appendErr := appendFinanceRevenueAudit(tx, syncEvent.UserID, common.FinanceOwnerID(), FinanceRevenueWrite{ExpectedRevision: expected, Event: *resolution.event, Evidence: resolution.evidence}, "finance-sync", "revenue.auto_sync")
	if appendErr == nil {
		return financeSyncOutcome{model.FinanceSyncDone, ""}
	}
	switch {
	case errors.Is(appendErr, model.ErrFinanceConflict):
		imported, resolveErr := resolveFinanceConflict(tx, syncEvent.UserID, resolution.event.Key)
		if resolveErr != nil {
			return financeSyncOutcome{model.FinanceSyncPending, financeSyncClassProcessingError}
		}
		if imported {
			return financeSyncOutcome{model.FinanceSyncDone, ""}
		}
		return financeSyncOutcome{model.FinanceSyncPending, financeSyncClassVersionConflict}
	case errors.Is(appendErr, model.ErrFinanceInvalid):
		return financeSyncOutcome{model.FinanceSyncNeedsReview, financeSyncClassInvalidFact}
	default:
		return financeSyncOutcome{model.FinanceSyncPending, financeSyncClassProcessingError}
	}
}

func processFinanceSyncRedemption(db *gorm.DB, event *model.FinanceSyncEvent) financeSyncOutcome {
	var outcome financeSyncOutcome
	err := db.Transaction(func(tx *gorm.DB) error {
		resolution := resolveFinanceSyncRedemption(tx, event)
		if resolution.outcome.status == financeSyncReady {
			outcome = applyFinanceSyncFact(tx, event, resolution)
		} else {
			outcome = resolution.outcome
		}
		return nil
	})
	if err != nil {
		return financeSyncOutcome{model.FinanceSyncPending, financeSyncClassProcessingError}
	}
	return outcome
}

func processFinanceSyncConsumption(db *gorm.DB, event *model.FinanceSyncEvent) financeSyncOutcome {
	var outcome financeSyncOutcome
	err := db.Transaction(func(tx *gorm.DB) error {
		resolution := resolveFinanceSyncConsumption(tx, event)
		if resolution.outcome.status == financeSyncReady {
			outcome = applyFinanceSyncFact(tx, event, resolution)
		} else {
			outcome = resolution.outcome
		}
		return nil
	})
	if err != nil {
		return financeSyncOutcome{model.FinanceSyncPending, financeSyncClassProcessingError}
	}
	return outcome
}

type FinanceSyncHealthReport struct {
	Counts                     map[string]int64 `json:"counts"`
	OldestPendingAgeSeconds    *int64           `json:"oldest_pending_age_seconds"`
	WatermarkID                int64            `json:"watermark_id"`
	NeedsReviewClasses         map[string]int64 `json:"needs_review_classes"`
	SettlementEmission         string           `json:"settlement_emission"`
	SettlementSampleSize       int              `json:"settlement_sample_size"`
	UnimportedSettlementSample int              `json:"unimported_settlement_sample"`
	Status                     string           `json:"status"`
	Warnings                   []string         `json:"warnings"`
}

// Every number is read from the outbox table, so the health survives restarts
// and is shared by all instances; process counters remain diagnostic only.
func FinanceSyncHealth(db *gorm.DB) (*FinanceSyncHealthReport, error) {
	if err := model.FinanceSyncReady(db); err != nil {
		return nil, err
	}
	result := &FinanceSyncHealthReport{Counts: map[string]int64{}, NeedsReviewClasses: map[string]int64{}, Status: "partial", Warnings: []string{
		"auto_emission_covers_offline_redemption_only",
		"consumption_emission_not_connected_to_settlement_boundary",
		"parked_events_require_owner_resolution",
		"balance_layers_cache_batch_and_db_not_atomic",
	}}
	var rows []struct {
		Status string
		Count  int64
	}
	if err := db.Model(&model.FinanceSyncEvent{}).Select("status, count(*) as count").Group("status").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result.Counts[row.Status] = row.Count
	}
	var classes []struct {
		ErrorClass string
		Count      int64
	}
	if err := db.Model(&model.FinanceSyncEvent{}).Select("error_class, count(*) as count").Where("status = ?", model.FinanceSyncNeedsReview).Group("error_class").Scan(&classes).Error; err != nil {
		return nil, err
	}
	for _, row := range classes {
		result.NeedsReviewClasses[row.ErrorClass] = row.Count
	}
	var watermark *int64
	if err := db.Model(&model.FinanceSyncEvent{}).Select("max(id) as watermark").Where("status = ?", model.FinanceSyncDone).Scan(&watermark).Error; err != nil {
		return nil, err
	}
	if watermark != nil {
		result.WatermarkID = *watermark
	}
	var oldest *int64
	if err := db.Model(&model.FinanceSyncEvent{}).Select("min(created_at) as oldest").Where("status = ?", model.FinanceSyncPending).Scan(&oldest).Error; err != nil {
		return nil, err
	}
	if oldest != nil && *oldest > 0 {
		age := time.Now().Unix() - *oldest
		if age < 0 {
			age = 0
		}
		result.OldestPendingAgeSeconds = &age
	}
	// Bounded reconciliation sample: settled wallet settlements whose charged
	// quota never reached the ledger. A nonzero count is a gap signal, never
	// proof of completeness.
	if common.BatchUpdateEnabled {
		result.SettlementEmission = "inactive_batch_updates"
	} else {
		result.SettlementEmission = "active"
		var observations []model.FinanceRequestEvent
		if err := db.Where("phase = ?", "text_settlement").Order("id desc").Limit(100).Find(&observations).Error; err != nil {
			return nil, err
		}
		candidateIDs := make([]int64, 0, len(observations))
		for _, observation := range observations {
			var billing FinanceBillingSnapshot
			if common.UnmarshalJsonStr(observation.Payload, &billing) != nil {
				continue
			}
			if billing.FundingSource == BillingSourceWallet && billing.Status == "settled" && billing.ChargedQuota != nil && *billing.ChargedQuota > 0 && int64(*billing.ChargedQuota) <= common.MaxWalletQuota {
				candidateIDs = append(candidateIDs, observation.ID)
			}
		}
		result.SettlementSampleSize = len(candidateIDs)
		for start := 0; start < len(candidateIDs); start += 50 {
			batch := candidateIDs[start:min(start+50, len(candidateIDs))]
			keys := make([]string, 0, len(batch))
			for _, id := range batch {
				keys = append(keys, fmt.Sprintf("request-charge-%d", id))
			}
			var imported int64
			if err := db.Model(&model.FinanceRevenueRecord{}).Where("event_key IN ?", keys).Count(&imported).Error; err != nil {
				return nil, err
			}
			result.UnimportedSettlementSample += len(batch) - int(imported)
		}
	}
	return result, nil
}

// Confirming a receipt unblocks the redemption events that were only waiting
// for its frozen value. Requeueing is best effort and never fails the
// confirmation itself.
func RequeueFinanceSyncForReceipt(db *gorm.DB, receipt *model.FinanceCashReceipt) {
	if err := model.FinanceSyncReady(db); err != nil {
		return
	}
	var frozenIDs []int
	if common.UnmarshalJsonStr(receipt.RedemptionIDs, &frozenIDs) != nil || len(frozenIDs) == 0 {
		return
	}
	ids := make([]int64, 0, len(frozenIDs))
	for _, id := range frozenIDs {
		ids = append(ids, int64(id))
	}
	err := db.Model(&model.FinanceSyncEvent{}).
		Where("topic = ? AND ref_id IN ? AND status = ?", "offline_redemption", ids, model.FinanceSyncBlocked).
		Updates(map[string]any{"status": model.FinanceSyncPending, "error_class": "receipt_confirmed", "next_attempt_at": time.Now().Unix()})
	if err.Error != nil {
		common.SysLog("failed to requeue finance sync events after receipt confirmation")
	}
}

// StartFinanceSyncWorker runs the bounded retry loop on application startup.
// It does nothing unless the owner finance feature is separately enabled, so
// current runtime configuration is unchanged.
func StartFinanceSyncWorker() {
	if !common.FinanceEnabled() {
		return
	}
	go func() {
		for {
			func() {
				defer func() {
					if r := recover(); r != nil {
						common.SysError(fmt.Sprintf("finance sync worker panic recovered: %v", r))
					}
				}()
				if _, err := RunFinanceSyncPass(model.DB, financeSyncBatchSize); err != nil && !errors.Is(err, model.ErrFinanceUnavailable) {
					common.SysLog("finance sync pass failed: " + err.Error())
				}
			}()
			time.Sleep(financeSyncWorkerSeconds * time.Second)
		}
	}()
}
