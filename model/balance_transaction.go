package model

import "encoding/json"

// BalanceTransaction 是余额调整账本（主 DB，绝不写入 LOG_DB/ClickHouse）。
// 定位为“余额变动流水”：RECHARGE/BONUS/ADMIN/COMPENSATION/REFERRAL/ACTIVITY。
// API 消费继续走现有 usage log，不写入本表。
type BalanceTransaction struct {
	Id            int    `json:"id"`
	UserId        int    `json:"user_id" gorm:"index:idx_balance_tx_user_time,priority:1"`
	CreatedAt     int64  `json:"created_at" gorm:"bigint;index:idx_balance_tx_user_time,priority:2"`
	Type          string `json:"type" gorm:"type:varchar(16);index"`
	AmountQuota   int    `json:"amount_quota"`
	BalanceBefore int    `json:"balance_before"`
	BalanceAfter  int    `json:"balance_after"`
	RechargeCents int64  `json:"recharge_cents" gorm:"type:bigint;default:0"` // 仅 RECHARGE 记录人民币面额（分）
	ReferenceType string `json:"reference_type" gorm:"type:varchar(16);default:''"`
	ReferenceId   int    `json:"reference_id" gorm:"default:0"`
	Description   string `json:"description" gorm:"type:varchar(255);default:''"`
	Metadata      string `json:"metadata" gorm:"type:text"` // JSON，禁止存完整兑换码
}

const (
	BalanceTxTypeRecharge     = "RECHARGE"
	BalanceTxTypeBonus        = "BONUS"
	BalanceTxTypeAdmin        = "ADMIN"
	BalanceTxTypeCompensation = "COMPENSATION"
	BalanceTxTypeReferral     = "REFERRAL"
	BalanceTxTypeActivity     = "ACTIVITY"

	BalanceTxReferenceRedemption = "REDEMPTION"
)

// BalanceTransactionMeta 是 metadata 列的受控结构；对用户展示时只挑字段，
// 绝不回显完整兑换码。
type BalanceTransactionMeta struct {
	CodeTail string `json:"code_tail,omitempty"`
}

func balanceTransactionMetaForCode(key string) string {
	if len(key) < 4 {
		return ""
	}
	meta, err := json.Marshal(BalanceTransactionMeta{CodeTail: key[len(key)-4:]})
	if err != nil {
		return ""
	}
	return string(meta)
}

// BalanceTransactionCodeTail 从 metadata 中取出展示用的兑换码末 4 位；
// 仅此字段允许对用户可见。
func BalanceTransactionCodeTail(metadata string) string {
	if metadata == "" {
		return ""
	}
	var meta BalanceTransactionMeta
	if err := json.Unmarshal([]byte(metadata), &meta); err != nil {
		return ""
	}
	return meta.CodeTail
}

func GetBalanceTransactions(userId int, txType string, startIdx int, num int) ([]*BalanceTransaction, int64, error) {
	return queryBalanceTransactions(userId, txType, startIdx, num)
}

// GetAllBalanceTransactions 管理员查询；userId>0 时按用户过滤，0 表示全部。
func GetAllBalanceTransactions(userId int, txType string, startIdx int, num int) ([]*BalanceTransaction, int64, error) {
	return queryBalanceTransactions(userId, txType, startIdx, num)
}

func queryBalanceTransactions(userId int, txType string, startIdx int, num int) ([]*BalanceTransaction, int64, error) {
	query := DB.Model(&BalanceTransaction{})
	if userId > 0 {
		query = query.Where("user_id = ?", userId)
	}
	if txType != "" {
		query = query.Where("type = ?", txType)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := []*BalanceTransaction{}
	err := query.Order("created_at DESC, id DESC").Limit(num).Offset(startIdx).Find(&items).Error
	return items, total, err
}
