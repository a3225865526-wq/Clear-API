package model

import (
	"errors"
	"regexp"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	OfflineOrderPaid = "paid"
	OfflineOrderVoid = "void"
)

var offlineOrderNoPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

type OfflineOrder struct {
	Id           int                `json:"id"`
	OrderNo      string             `json:"order_no" gorm:"type:varchar(100);uniqueIndex"`
	Currency     string             `json:"currency" gorm:"type:varchar(3)"`
	MoneyCents   int64              `json:"money_cents"`
	QuotaPerCode int                `json:"quota_per_code"`
	CodeCount    int                `json:"code_count"`
	PaidAt       int64              `json:"paid_at"`
	CreatedAt    int64              `json:"created_at"`
	AdminUserID  int                `json:"admin_user_id"`
	Status       string             `json:"status" gorm:"type:varchar(16)"`
	Codes        []OfflineOrderCode `json:"-" gorm:"foreignKey:OrderID"`
}

type OfflineOrderCode struct {
	Id           int        `json:"id"`
	OrderID      int        `json:"order_id" gorm:"index"`
	RedemptionID int        `json:"redemption_id" gorm:"uniqueIndex"`
	Redemption   Redemption `json:"-" gorm:"foreignKey:RedemptionID"`
}

type CreateOfflineOrderInput struct {
	OrderNo      string
	MoneyCents   int64
	QuotaPerCode int
	CodeCount    int
	PaidAt       int64
	ExpiredTime  int64
	AdminUserID  int
}

func CreateOfflineOrder(input CreateOfflineOrderInput) (*OfflineOrder, []string, error) {
	if !offlineOrderNoPattern.MatchString(input.OrderNo) || input.MoneyCents <= 0 || input.MoneyCents > common.MaxWalletQuota ||
		input.CodeCount < 1 || input.CodeCount > 100 || input.AdminUserID <= 0 ||
		input.PaidAt <= 0 || input.PaidAt > common.GetTimestamp() ||
		input.QuotaPerCode <= 0 || (input.ExpiredTime != 0 && input.ExpiredTime < common.GetTimestamp()) {
		return nil, nil, errors.New("invalid offline order")
	}
	if err := common.ValidateWalletQuota(input.QuotaPerCode); err != nil {
		return nil, nil, err
	}
	order := &OfflineOrder{
		OrderNo: input.OrderNo, Currency: "CNY", MoneyCents: input.MoneyCents,
		QuotaPerCode: input.QuotaPerCode, CodeCount: input.CodeCount,
		PaidAt: input.PaidAt, CreatedAt: common.GetTimestamp(),
		AdminUserID: input.AdminUserID, Status: OfflineOrderPaid,
	}
	keys := make([]string, 0, input.CodeCount)
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(order).Error; err != nil {
			return err
		}
		for range input.CodeCount {
			key := common.GetUUID()
			code := &Redemption{
				UserId: input.AdminUserID, Name: "ClearAPI offline",
				Key: key, Status: common.RedemptionCodeStatusEnabled,
				Quota: input.QuotaPerCode, CreatedTime: common.GetTimestamp(), ExpiredTime: input.ExpiredTime,
			}
			if err := tx.Create(code).Error; err != nil {
				return err
			}
			if err := tx.Create(&OfflineOrderCode{OrderID: order.Id, RedemptionID: code.Id}).Error; err != nil {
				return err
			}
			keys = append(keys, key)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return order, keys, nil
}

func GetOfflineOrders(start, size int) ([]OfflineOrder, int64, error) {
	var orders []OfflineOrder
	var total int64
	if err := DB.Model(&OfflineOrder{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := DB.Preload("Codes.Redemption").Order("id desc").Limit(size).Offset(start).Find(&orders).Error
	return orders, total, err
}

func VoidOfflineOrder(id int) error {
	if id <= 0 {
		return errors.New("invalid offline order id")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var order OfflineOrder
		if err := lockForUpdate(tx).Where("id = ?", id).First(&order).Error; err != nil {
			return err
		}
		if order.Status != OfflineOrderPaid {
			return errors.New("offline order is not active")
		}
		var links []OfflineOrderCode
		if err := tx.Where("order_id = ?", id).Find(&links).Error; err != nil {
			return err
		}
		if len(links) != order.CodeCount {
			return errors.New("offline order code count mismatch")
		}
		ids := make([]int, 0, len(links))
		for _, link := range links {
			ids = append(ids, link.RedemptionID)
		}
		result := tx.Model(&Redemption{}).Where("id IN ? AND status = ?", ids, common.RedemptionCodeStatusEnabled).
			Update("status", common.RedemptionCodeStatusDisabled)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != int64(order.CodeCount) {
			return errors.New("redeemed or disabled code prevents order void")
		}
		return tx.Model(&order).Where("status = ?", OfflineOrderPaid).Update("status", OfflineOrderVoid).Error
	})
}
