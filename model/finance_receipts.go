package model

import "gorm.io/gorm"

// Cash receipts are distinct from quota redemption and recognized revenue.
// An owner-confirmed manual receipt is not a bank or supplier reconciliation.
type FinanceCashReceipt struct {
	ID            int64  `json:"id"`
	OrderID       int    `json:"order_id" gorm:"uniqueIndex"`
	AmountCNY     string `json:"amount_cny" gorm:"type:varchar(64)"`
	PaidAt        int64  `json:"paid_at" gorm:"index"`
	QuotaPerCode  int64  `json:"quota_per_code"`
	CodeCount     int    `json:"code_count"`
	RedemptionIDs string `json:"-" gorm:"type:text"`
	Evidence      string `json:"evidence" gorm:"type:varchar(500)"`
	CreatedBy     int    `json:"created_by"`
	CreatedAt     int64  `json:"created_at"`
}

func FinanceReceiptsReady(db *gorm.DB) error {
	if db == nil {
		return ErrFinanceUnavailable
	}
	var schema FinanceSchema
	if err := db.First(&schema, 1).Error; err != nil || schema.Version < 4 || schema.Version > 5 {
		return ErrFinanceUnavailable
	}
	return nil
}

// Explicit additive migration only; never invoked by normal gateway startup.
func MigrateFinanceReceipts(db *gorm.DB) error {
	if err := MigrateFinanceRequests(db); err != nil {
		return err
	}
	if err := db.AutoMigrate(&FinanceCashReceipt{}); err != nil {
		return err
	}
	return db.Model(&FinanceSchema{}).Where("id = ? AND version = ?", 1, 3).Update("version", 4).Error
}

// Read only non-secret business fields. Lock order/link facts during a financial
// import; never retrieve redemption keys, names, usernames or payment secrets.
func FinanceOfflineOrderFacts(tx *gorm.DB, orderID int) (*OfflineOrder, []int, error) {
	var order OfflineOrder
	if err := lockForUpdate(tx).Select("id", "currency", "money_cents", "quota_per_code", "code_count", "paid_at", "status").First(&order, orderID).Error; err != nil {
		return nil, nil, err
	}
	var links []OfflineOrderCode
	if err := lockForUpdate(tx).Select("id", "order_id", "redemption_id").Where("order_id = ?", orderID).Order("id asc").Find(&links).Error; err != nil {
		return nil, nil, err
	}
	ids := make([]int, 0, len(links))
	for _, link := range links {
		ids = append(ids, link.RedemptionID)
	}
	return &order, ids, nil
}
