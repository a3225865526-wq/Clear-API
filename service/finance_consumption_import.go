package service

import (
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

type FinanceConsumptionImport struct {
	ObservationIDs   []int64 `json:"observation_ids"`
	ExpectedRevision int64   `json:"expected_revision"`
	Evidence         string  `json:"evidence"`
}

type FinanceConsumptionSource struct {
	ObservationID   int64                `json:"observation_id"`
	At              int64                `json:"observed_at"`
	RequestID       string               `json:"request_id"`
	ChannelID       int                  `json:"channel_id"`
	UpstreamModel   string               `json:"upstream_model"`
	Status          string               `json:"status"`
	ExistingEventID int64                `json:"existing_event_id"`
	Event           *FinanceRevenueEvent `json:"event"`
}

// Capture is best effort, not a financial transaction with the wallet. Only
// explicit, settled primary wallet facts may be allocated. Any refund observed
// later requires review; never infer a reversal from absent or queued refunds.
func PreviewFinanceConsumptions(db *gorm.DB, userID int, ids []int64) ([]FinanceConsumptionSource, error) {
	if err := model.FinanceRequestsReady(db); err != nil {
		return nil, err
	}
	if userID <= 0 || userID > math.MaxInt32 || len(ids) < 1 || len(ids) > 100 {
		return nil, model.ErrFinanceInvalid
	}
	selected := slices.Clone(ids)
	slices.Sort(selected)
	for i, id := range selected {
		if id <= 0 || (i > 0 && id == selected[i-1]) {
			return nil, model.ErrFinanceInvalid
		}
	}
	result := make([]FinanceConsumptionSource, 0, len(selected))
	for _, id := range selected {
		var fact model.FinanceRequestEvent
		if err := db.First(&fact, id).Error; err != nil {
			return nil, model.ErrFinanceInvalid
		}
		if fact.UserID != userID || fact.Phase != "text_settlement" || fact.AttemptID != "" || fact.RequestID == "" || fact.At <= 0 {
			return nil, model.ErrFinanceInvalid
		}
		source := FinanceConsumptionSource{ObservationID: fact.ID, At: fact.At, RequestID: fact.RequestID, ChannelID: fact.ChannelID, UpstreamModel: fact.UpstreamModel, Status: "unknown"}
		var billing FinanceBillingSnapshot
		if common.UnmarshalJsonStr(fact.Payload, &billing) != nil {
			return nil, model.ErrFinanceInvalid
		}
		var settlements, refunds int64
		if err := db.Model(&model.FinanceRequestEvent{}).Where("request_id = ? AND phase = ?", fact.RequestID, "text_settlement").Count(&settlements).Error; err != nil {
			return nil, err
		}
		if err := db.Model(&model.FinanceRequestEvent{}).Where("request_id = ? AND phase = ?", fact.RequestID, "funding_refund").Count(&refunds).Error; err != nil {
			return nil, err
		}
		key := fmt.Sprintf("request-charge-%d", fact.ID)
		var previous []model.FinanceRevenueRecord
		if err := db.Where("event_key = ?", key).Limit(2).Find(&previous).Error; err != nil {
			return nil, err
		}
		if len(previous) > 1 {
			return nil, model.ErrFinanceConflict
		}
		if billing.ChargedQuota != nil && *billing.ChargedQuota >= 0 && int64(*billing.ChargedQuota) <= common.MaxWalletQuota && billing.Scope == "billing_session_primary_funding" && billing.FundingSource == BillingSourceWallet && billing.Status == "settled" && settlements == 1 {
			source.Event = &FinanceRevenueEvent{Key: key, At: fact.At, Kind: "consume", Quota: int64(*billing.ChargedQuota)}
			source.Status = "ready"
			if *billing.ChargedQuota == 0 {
				source.Status = "no_charge"
			}
		}
		if len(previous) == 1 {
			old := previous[0]
			if source.Event == nil || old.UserID != userID || old.Kind != "consume" || old.At != fact.At || old.Quota != source.Event.Quota {
				return nil, model.ErrFinanceConflict
			}
			source.Status, source.ExistingEventID = "already_imported", old.ID
		}
		if refunds > 0 {
			source.Status = "refund_requires_review"
			source.Event = nil
		}
		result = append(result, source)
	}
	slices.SortFunc(result, func(a, b FinanceConsumptionSource) int {
		if a.At < b.At {
			return -1
		}
		if a.At > b.At {
			return 1
		}
		if a.ObservationID < b.ObservationID {
			return -1
		}
		if a.ObservationID > b.ObservationID {
			return 1
		}
		return 0
	})
	return result, nil
}

func ImportFinanceConsumptions(db *gorm.DB, userID, actorID int, input FinanceConsumptionImport, requestID string) ([]model.FinanceRevenueRecord, error) {
	if !common.IsFinanceOwner(actorID) || input.ExpectedRevision < 0 || input.ExpectedRevision > math.MaxInt64-100 || input.Evidence == "" || len(input.Evidence) > 350 {
		return nil, model.ErrFinanceInvalid
	}
	result := []model.FinanceRevenueRecord{}
	err := db.Transaction(func(tx *gorm.DB) error {
		sources, err := PreviewFinanceConsumptions(tx, userID, input.ObservationIDs)
		if err != nil {
			return err
		}
		for _, source := range sources {
			if source.Status != "ready" && source.Status != "already_imported" && source.Status != "no_charge" {
				return model.ErrFinanceInvalid
			}
		}
		var wallet model.FinanceWallet
		if err := tx.First(&wallet, "user_id = ?", userID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return model.ErrFinanceInvalid
		} else if err != nil {
			return err
		}
		if _, err := model.LockFinanceWallet(tx, userID, input.ExpectedRevision); err != nil {
			return err
		}
		for _, source := range sources {
			if source.Status != "ready" {
				continue
			}
			evidence := fmt.Sprintf("request_observation:%d; request_id:%s; %s", source.ObservationID, source.RequestID, input.Evidence)
			record, err := appendFinanceRevenue(tx, userID, actorID, FinanceRevenueWrite{ExpectedRevision: input.ExpectedRevision, Event: *source.Event, Evidence: evidence}, requestID)
			if err != nil {
				return err
			}
			result = append(result, *record)
			input.ExpectedRevision++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

type FinanceBalanceCheck struct {
	BusinessQuota int64    `json:"business_quota"`
	LedgerQuota   *int64   `json:"ledger_quota"`
	Difference    *int64   `json:"business_minus_ledger"`
	Status        string   `json:"status"`
	Warnings      []string `json:"warnings"`
}

func CheckFinanceBalance(db *gorm.DB, userID int) (*FinanceBalanceCheck, error) {
	if err := model.FinanceRevenueReady(db); err != nil {
		return nil, err
	}
	if userID <= 0 || userID > math.MaxInt32 {
		return nil, model.ErrFinanceInvalid
	}
	var user model.User
	if err := db.Select("id", "quota").First(&user, userID).Error; err != nil {
		return nil, model.ErrFinanceInvalid
	}
	if user.Quota < 0 || int64(user.Quota) > common.MaxWalletQuota {
		return nil, model.ErrFinanceInvalid
	}
	result := &FinanceBalanceCheck{BusinessQuota: int64(user.Quota), Status: "unknown", Warnings: []string{"point_in_time_non_atomic_reads", "matching_does_not_prove_source_or_capture_completeness", "in_flight_reservations_and_batch_updates_not_reconciled"}}
	var wallet model.FinanceWallet
	if err := db.First(&wallet, "user_id = ?", userID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return result, nil
	} else if err != nil {
		return nil, err
	}
	if wallet.RemainingQuota < 0 || wallet.RemainingQuota > common.MaxWalletQuota {
		return nil, model.ErrFinanceInvalid
	}
	difference := result.BusinessQuota - wallet.RemainingQuota
	result.LedgerQuota, result.Difference, result.Status = &wallet.RemainingQuota, &difference, "mismatch"
	if difference == 0 {
		result.Status = "matching_unverified"
	}
	return result, nil
}
