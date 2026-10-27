package model

import (
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// Deletion checks are advisory in a review and repeated under the catalog lock
// for the actual delete. Optional finance tables may legitimately be absent.
type CreditProductDeletion struct {
	Batches        int64 `json:"batches"`
	Codes          int64 `json:"codes"`
	UsedCodes      int64 `json:"used_codes"`
	Orders         int64 `json:"orders"`
	BalanceEntries int64 `json:"balance_entries"`
	FinanceEvents  int64 `json:"finance_events"`
	CashReceipts   int64 `json:"cash_receipts"`
	Rewards        int64 `json:"rewards"`
	Events         int64 `json:"events"`
	CanDelete      bool  `json:"can_delete"`
}

func inspectCreditProductDeletion(tx *gorm.DB, catalog CreditCatalog, p CreditProduct) (*CreditProductDeletion, error) {
	r := &CreditProductDeletion{}
	evidence := tx.Model(&CreditApprovalEvent{}).Select("evidence_batch_id").Where("product_id = ?", p.ID)
	batchIDs := tx.Model(&RedemptionBatch{}).Select("id").Where("product_id = ? OR id = ? OR id IN (?)", p.ID, p.EvidenceBatchID, evidence)
	codeIDs := tx.Unscoped().Model(&Redemption{}).Select("id").Where("batch_id IN (?)", batchIDs)
	if err := tx.Model(&RedemptionBatch{}).Where("id IN (?)", batchIDs).Count(&r.Batches).Error; err != nil {
		return nil, err
	}
	if err := tx.Unscoped().Model(&Redemption{}).Where("id IN (?)", codeIDs).Count(&r.Codes).Error; err != nil {
		return nil, err
	}
	if err := tx.Unscoped().Model(&Redemption{}).Where("id IN (?)", codeIDs).Where("used_user_id > 0 OR redeemed_time > 0 OR status = ?", common.RedemptionCodeStatusUsed).Count(&r.UsedCodes).Error; err != nil {
		return nil, err
	}
	if err := tx.Model(&CreditApprovalEvent{}).Where("product_id = ?", p.ID).Count(&r.Events).Error; err != nil {
		return nil, err
	}
	if tx.Migrator().HasTable(&OfflineOrderCode{}) {
		links := tx.Model(&OfflineOrderCode{}).Select("order_id").Where("redemption_id IN (?)", codeIDs)
		if err := tx.Model(&OfflineOrderCode{}).Where("redemption_id IN (?)", codeIDs).Distinct("order_id").Count(&r.Orders).Error; err != nil {
			return nil, err
		}
		if tx.Migrator().HasTable(&FinanceCashReceipt{}) {
			if err := tx.Model(&FinanceCashReceipt{}).Where("order_id IN (?)", links).Count(&r.CashReceipts).Error; err != nil {
				return nil, err
			}
		}
	}
	if tx.Migrator().HasTable(&BalanceTransaction{}) {
		if err := tx.Model(&BalanceTransaction{}).Where("reference_type = ? AND reference_id IN (?)", BalanceTxReferenceRedemption, codeIDs).Count(&r.BalanceEntries).Error; err != nil {
			return nil, err
		}
	}
	if tx.Migrator().HasTable(&FinanceSyncEvent{}) {
		if err := tx.Model(&FinanceSyncEvent{}).Where("topic = ? AND ref_id IN (?)", "offline_redemption", codeIDs).Count(&r.FinanceEvents).Error; err != nil {
			return nil, err
		}
	}
	if tx.Migrator().HasTable(&ReferralReward{}) {
		if err := tx.Model(&ReferralReward{}).Where("redemption_id IN (?)", codeIDs).Count(&r.Rewards).Error; err != nil {
			return nil, err
		}
	}
	r.CanDelete = p.State == "draft" && p.Revision == 1 && !p.Published && !p.DeliveryVerified &&
		p.ApprovalID == 0 && p.EvidenceBatchID == 0 && p.LegacyStoreID == 0 && !p.LegacyWithdrawn &&
		r.Batches == 0 && r.Codes == 0 && r.Events == 0 && r.Orders == 0 && r.BalanceEntries == 0 &&
		r.FinanceEvents == 0 && r.CashReceipts == 0 && r.Rewards == 0
	for _, legacy := range catalog.LegacyCatalog {
		if legacy.ID == p.ID && legacy.Published {
			r.CanDelete = false
		}
	}
	return r, nil
}

// Stock choices are validated with the same approval and delivery checks used
// during issuance. The creation transaction still validates again.
type RedemptionBatchProduct struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Revision        int64  `json:"revision"`
	PriceCents      int64  `json:"price_cents"`
	BaseQuota       int    `json:"base_quota"`
	EvidenceBatchID int    `json:"evidence_batch_id"`
	DeliveryBinding string `json:"delivery_binding"`
	State           string `json:"state"`
	StockReady      bool   `json:"stock_ready"`
	SampleReady     bool   `json:"sample_ready"`
}

func GetRedemptionBatchProducts() (result []RedemptionBatchProduct, err error) {
	result = []RedemptionBatchProduct{}
	err = DB.Transaction(func(tx *gorm.DB) error {
		_, c, err := lockedCreditCatalog(tx)
		if err != nil {
			return err
		}
		if err := c.Validate(); err != nil {
			return err
		}
		for _, p := range c.Products {
			if p.Archived {
				continue
			}
			choice := RedemptionBatchProduct{ID: p.ID, Name: p.Name, Revision: p.Revision, PriceCents: p.PriceCents, BaseQuota: p.BaseQuota, EvidenceBatchID: p.EvidenceBatchID, DeliveryBinding: p.DeliveryBinding, State: p.State}
			choice.StockReady = validateCreditApproval(tx, p) == nil
			choice.SampleReady = !c.Unified && p.State == "draft" && p.BaseQuota > 0 && p.DeliveryBinding != ""
			if choice.SampleReady {
				var issued int64
				if err := tx.Model(&CreditApprovalEvent{}).Where("product_id = ? AND revision = ? AND decision = ?", p.ID, p.Revision, "sample").Count(&issued).Error; err != nil {
					return err
				}
				choice.SampleReady = issued == 0
			}
			result = append(result, choice)
		}
		return nil
	})
	return
}
