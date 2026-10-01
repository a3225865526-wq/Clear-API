package service

import (
	"slices"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

var financeRefundStates = []string{"pending", "queued", "succeeded", "failed"}

type FinanceCashRefundInput struct {
	EventKey   string `json:"event_key"`
	ReceiptID  int64  `json:"receipt_id"`
	State      string `json:"state"`
	AmountCNY  string `json:"amount_cny"`
	RefundedAt int64  `json:"refunded_at"`
	Evidence   string `json:"evidence"`
}

// The owner records each refund lifecycle fact manually against a confirmed
// receipt. Reserved keys stay available for future automatic payment
// correlation; nothing here touches prior revenue, lots or business wallets.
func RegisterFinanceCashRefund(db *gorm.DB, actorID int, input FinanceCashRefundInput, requestID string) (*model.FinanceCashRefund, error) {
	if err := model.FinanceRefundsReady(db); err != nil {
		return nil, err
	}
	if !common.IsFinanceOwner(actorID) || strings.HasPrefix(input.EventKey, "offline-refund-") || strings.HasPrefix(input.EventKey, "request-refund-") || !financeKeyPattern.MatchString(input.EventKey) || !slices.Contains(financeRefundStates, input.State) || input.ReceiptID <= 0 || input.RefundedAt <= 0 || input.RefundedAt > time.Now().Unix() || input.Evidence == "" || len(input.Evidence) > 500 {
		return nil, model.ErrFinanceInvalid
	}
	amount, err := financeDecimal(input.AmountCNY, false)
	if err != nil || !amount.IsPositive() {
		return nil, model.ErrFinanceInvalid
	}
	entry := &model.FinanceCashRefund{EventKey: input.EventKey, ReceiptID: input.ReceiptID, State: input.State, AmountCNY: amount.String(), RefundedAt: input.RefundedAt, Evidence: input.Evidence, CreatedBy: actorID}
	if err := model.AppendFinanceCashRefund(db, entry, requestID); err != nil {
		return nil, err
	}
	return entry, nil
}

// Succeeded refunds in the period; pending, queued and failed facts never
// reduce any total.
func financeRefundTotals(db *gorm.DB, from, to int64) (refunds decimal.Decimal, err error) {
	var lastID int64
	for {
		var batch []model.FinanceCashRefund
		if err := db.Where("refunded_at >= ? AND refunded_at < ? AND id > ?", from, to, lastID).Order("id asc").Limit(100).Find(&batch).Error; err != nil {
			return decimal.Zero, err
		}
		if len(batch) == 0 {
			break
		}
		for _, refund := range batch {
			lastID = refund.ID
			if refund.State != model.FinanceRefundSucceeded {
				continue
			}
			amount, parseErr := financeDecimal(refund.AmountCNY, true)
			if parseErr != nil {
				return decimal.Zero, parseErr
			}
			refunds = refunds.Add(amount)
		}
	}
	return refunds, nil
}
