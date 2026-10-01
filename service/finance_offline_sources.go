package service

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type FinanceReceiptConfirmation struct {
	OrderID            int    `json:"order_id"`
	ConfirmedAmountCNY string `json:"confirmed_amount_cny"`
	Evidence           string `json:"evidence"`
}

// Confirmation is a deliberate owner operation, not inference from an order's
// paid flag. Receipts are immutable and never created again on redemption.
func ConfirmFinanceOfflineReceipt(db *gorm.DB, actorID int, input FinanceReceiptConfirmation, requestID string) (*model.FinanceCashReceipt, error) {
	if err := model.FinanceReceiptsReady(db); err != nil {
		return nil, err
	}
	amount, err := financeDecimal(input.ConfirmedAmountCNY, true)
	if err != nil || !amount.IsPositive() || input.OrderID <= 0 || !common.IsFinanceOwner(actorID) || input.Evidence == "" || len(input.Evidence) > 500 {
		return nil, model.ErrFinanceInvalid
	}
	var receipt model.FinanceCashReceipt
	err = db.Transaction(func(tx *gorm.DB) error {
		order, ids, err := model.FinanceOfflineOrderFacts(tx, input.OrderID)
		if err != nil {
			return err
		}
		if order.Currency != "CNY" || order.Status != model.OfflineOrderPaid || order.MoneyCents <= 0 || order.MoneyCents > common.MaxWalletQuota || order.CodeCount < 1 || order.CodeCount > 100 || len(ids) != order.CodeCount || order.QuotaPerCode <= 0 || int64(order.QuotaPerCode) > common.MaxWalletQuota || order.PaidAt <= 0 || order.PaidAt > time.Now().Unix() || !amount.Equal(decimal.NewFromInt(order.MoneyCents).Div(decimal.NewFromInt(100))) {
			return model.ErrFinanceInvalid
		}
		var count int64
		if err := tx.Model(&model.FinanceCashReceipt{}).Where("order_id = ?", order.Id).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return model.ErrFinanceConflict
		}
		encoded, err := common.Marshal(ids)
		if err != nil {
			return err
		}
		receipt = model.FinanceCashReceipt{OrderID: order.Id, AmountCNY: amount.String(), PaidAt: order.PaidAt, QuotaPerCode: int64(order.QuotaPerCode), CodeCount: order.CodeCount, RedemptionIDs: string(encoded), Evidence: input.Evidence, CreatedBy: actorID, CreatedAt: time.Now().Unix()}
		if err := tx.Create(&receipt).Error; err != nil {
			return err
		}
		return tx.Create(&model.FinanceAudit{ActorID: actorID, Action: "cash.offline_confirm", EntityID: receipt.ID, RequestID: requestID, CreatedAt: receipt.CreatedAt}).Error
	})
	if err != nil {
		return nil, err
	}
	RequeueFinanceSyncForReceipt(db, &receipt)
	return &receipt, nil
}

type FinanceOfflineSource struct {
	RedemptionID    int                 `json:"redemption_id"`
	OrderID         int                 `json:"order_id"`
	ReceiptID       int64               `json:"receipt_id"`
	Event           FinanceRevenueEvent `json:"event"`
	AlreadyImported bool                `json:"already_imported"`
}

type FinanceOfflineImport struct {
	RedemptionIDs    []int  `json:"redemption_ids"`
	ExpectedRevision int64  `json:"expected_revision"`
	Evidence         string `json:"evidence"`
}

// Resolve only selected redeemed facts. Unknown receipts stay unknown in the
// preview; import requires explicit cash confirmation and unchanged sources.
func PreviewFinanceOfflineSources(db *gorm.DB, userID int, ids []int) ([]FinanceOfflineSource, error) {
	if err := model.FinanceReceiptsReady(db); err != nil {
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
	result := make([]FinanceOfflineSource, 0, len(selected))
	for _, id := range selected {
		var redeemed model.Redemption
		if err := db.Unscoped().Select("id", "status", "quota", "used_user_id", "redeemed_time").First(&redeemed, id).Error; err != nil {
			return nil, err
		}
		if redeemed.Status != common.RedemptionCodeStatusUsed || redeemed.UsedUserId != userID || redeemed.Quota <= 0 || int64(redeemed.Quota) > common.MaxWalletQuota || redeemed.RedeemedTime <= 0 || redeemed.RedeemedTime > time.Now().Unix() {
			return nil, model.ErrFinanceInvalid
		}
		var link model.OfflineOrderCode
		if err := db.Select("order_id", "redemption_id").Where("redemption_id = ?", id).First(&link).Error; err != nil {
			return nil, model.ErrFinanceInvalid
		}
		order, currentIDs, err := model.FinanceOfflineOrderFacts(db, link.OrderID)
		if err != nil {
			return nil, err
		}
		if order.Status != model.OfflineOrderPaid || order.CodeCount < 1 || order.CodeCount > 100 || len(currentIDs) != order.CodeCount || !slices.Contains(currentIDs, id) || order.QuotaPerCode != redeemed.Quota {
			return nil, model.ErrFinanceConflict
		}
		key := fmt.Sprintf("offline-redemption-%d", id)
		source := FinanceOfflineSource{RedemptionID: id, OrderID: link.OrderID, Event: FinanceRevenueEvent{Key: key, At: redeemed.RedeemedTime, Kind: "credit", Quota: int64(redeemed.Quota), Source: "unknown"}}
		var receipt model.FinanceCashReceipt
		err = db.Where("order_id = ?", link.OrderID).First(&receipt).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if err == nil {
			var frozenIDs []int
			if common.UnmarshalJsonStr(receipt.RedemptionIDs, &frozenIDs) != nil {
				return nil, model.ErrFinanceInvalid
			}
			amount, err := financeDecimal(receipt.AmountCNY, true)
			position := slices.Index(frozenIDs, id)
			if err != nil || receipt.CodeCount < 1 || receipt.CodeCount > 100 || len(frozenIDs) != receipt.CodeCount || position < 0 || !slices.Equal(frozenIDs, currentIDs) || receipt.QuotaPerCode != int64(redeemed.Quota) || order.Currency != "CNY" || order.Status != model.OfflineOrderPaid || receipt.PaidAt != order.PaidAt || redeemed.RedeemedTime < receipt.PaidAt || receipt.CodeCount != order.CodeCount || receipt.QuotaPerCode != int64(order.QuotaPerCode) || !amount.Equal(decimal.NewFromInt(order.MoneyCents).Div(decimal.NewFromInt(100))) {
				return nil, model.ErrFinanceConflict
			}
			// Cumulative rounding across all codes preserves the exact order value;
			// gifts included in each code share this value over its total quota.
			value := amount.Mul(decimal.NewFromInt(int64(position+1))).DivRound(decimal.NewFromInt(int64(receipt.CodeCount)), 12).Sub(amount.Mul(decimal.NewFromInt(int64(position))).DivRound(decimal.NewFromInt(int64(receipt.CodeCount)), 12)).String()
			source.ReceiptID, source.Event.Source, source.Event.ValueCNY = receipt.ID, "paid", &value
		}
		var existing model.FinanceRevenueRecord
		if err := db.Where("event_key = ?", key).Find(&existing).Error; err != nil {
			return nil, err
		}
		if existing.ID != 0 {
			if existing.UserID != userID || existing.Quota != source.Event.Quota || existing.At != source.Event.At || existing.Source != source.Event.Source || (existing.ValueCNY == nil) != (source.Event.ValueCNY == nil) || (existing.ValueCNY != nil && *existing.ValueCNY != *source.Event.ValueCNY) {
				return nil, model.ErrFinanceConflict
			}
			source.AlreadyImported = true
		}
		result = append(result, source)
	}
	slices.SortFunc(result, func(a, b FinanceOfflineSource) int {
		if a.Event.At < b.Event.At {
			return -1
		}
		if a.Event.At > b.Event.At {
			return 1
		}
		return a.RedemptionID - b.RedemptionID
	})
	return result, nil
}

func ImportFinanceOfflineSources(db *gorm.DB, userID, actorID int, input FinanceOfflineImport, requestID string) ([]model.FinanceRevenueRecord, error) {
	if !common.IsFinanceOwner(actorID) || input.ExpectedRevision < 0 || input.ExpectedRevision > math.MaxInt64-100 || input.Evidence == "" || len(input.Evidence) > 400 {
		return nil, model.ErrFinanceInvalid
	}
	result := []model.FinanceRevenueRecord{}
	err := db.Transaction(func(tx *gorm.DB) error {
		sources, err := PreviewFinanceOfflineSources(tx, userID, input.RedemptionIDs)
		if err != nil {
			return err
		}
		// Lock/check the head even when all selected sources were imported before.
		if _, err := model.LockFinanceWallet(tx, userID, input.ExpectedRevision); err != nil {
			return err
		}
		for _, source := range sources {
			if source.Event.ValueCNY == nil {
				return model.ErrFinanceInvalid
			}
			if source.AlreadyImported {
				continue
			}
			evidence := fmt.Sprintf("cash_receipt:%d; offline_order:%d; %s", source.ReceiptID, source.OrderID, input.Evidence)
			record, err := appendFinanceRevenue(tx, userID, actorID, FinanceRevenueWrite{ExpectedRevision: input.ExpectedRevision, Event: source.Event, Evidence: evidence}, requestID)
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

type FinanceCashSummary struct {
	KnownReceiptsCNY    string   `json:"known_receipts_cny"`
	KnownRefundsCNY     string   `json:"known_refunds_succeeded_cny"`
	NetKnownReceiptsCNY string   `json:"net_known_receipts_cny"`
	TotalReceiptsCNY    *string  `json:"total_receipts_cny"`
	Status              string   `json:"status"`
	Warnings            []string `json:"warnings"`
}

func SummarizeFinanceCash(db *gorm.DB, from, to int64) (*FinanceCashSummary, error) {
	if err := model.FinanceReceiptsReady(db); err != nil {
		return nil, err
	}
	if from < 0 || to <= from {
		return nil, model.ErrFinanceInvalid
	}
	result := &FinanceCashSummary{Status: "partial", Warnings: []string{"owner_confirmed_manual_offline_only", "other_payment_sources_not_connected", "refunds_owner_confirmed_manual_not_bank_reconciled", "refunds_attributed_to_refund_period_no_restatement", "not_bank_reconciled"}}
	total := decimal.Zero
	var lastID int64
	for {
		var receipts []model.FinanceCashReceipt
		if err := db.Where("paid_at >= ? AND paid_at < ? AND id > ?", from, to, lastID).Order("id asc").Limit(100).Find(&receipts).Error; err != nil {
			return nil, err
		}
		if len(receipts) == 0 {
			break
		}
		for _, receipt := range receipts {
			amount, err := financeDecimal(receipt.AmountCNY, true)
			if err != nil {
				return nil, err
			}
			total = total.Add(amount)
			lastID = receipt.ID
		}
	}
	if model.FinanceRefundsReady(db) == nil {
		refunds, err := financeRefundTotals(db, from, to)
		if err != nil {
			return nil, err
		}
		result.KnownRefundsCNY = refunds.String()
		result.NetKnownReceiptsCNY = total.Sub(refunds).String()
	} else {
		result.KnownRefundsCNY = "0"
		result.NetKnownReceiptsCNY = total.String()
		result.Warnings = append(result.Warnings, "cash_refund_table_not_migrated")
	}
	result.KnownReceiptsCNY = total.String()
	return result, nil
}
