package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// ErrStoreRechargeDisabled 商城充值功能开关关闭。该错误会原样返回给 controller，
// 用于给出明确提示（不泄露兑换码状态）。
var ErrStoreRechargeDisabled = errors.New("store.recharge.disabled")

// StoreRedeemResult 是商城兑换成功后的对外结果；禁止包含兑换码原文、
// Redemption/Batch 主键或任何数据库内部错误。
type StoreRedeemResult struct {
	BaseQuota           int    `json:"base_quota"`
	BonusQuota          int    `json:"bonus_quota"`
	TotalQuota          int    `json:"total_quota"`
	Balance             int    `json:"balance"`
	FaceValueCents      int64  `json:"face_value_cents"`
	VIPName             string `json:"vip_name"`
	VIPBonusBasisPoints int    `json:"vip_bonus_basis_points"`
	NewVIPName          string `json:"new_vip_name"`
}

// storeQuotaForFaceValueCents 按创建当时配置的 Price（1 USD 的 CNY 价格，
// 见 getPayMoney）把人民币面额换算成 wallet quota，向下取整（不多给），
// 结果随批次/兑换码冻结，之后 Price 变化不影响已生成卡密。
func storeQuotaForFaceValueCents(faceValueCents int64) (int, error) {
	if faceValueCents <= 0 {
		return 0, errors.New("面额必须大于 0")
	}
	if faceValueCents > maxStoreProductFaceValueCents*100 {
		return 0, errors.New("面额超出允许范围")
	}
	price := operation_setting.Price
	if price <= 0 {
		return 0, errors.New("充值价格未配置，无法换算额度")
	}
	dCNY := decimal.NewFromInt(faceValueCents).Div(decimal.NewFromInt(100))
	dQuota := dCNY.Div(decimal.NewFromFloat(price)).
		Mul(decimal.NewFromFloat(common.QuotaPerUnit)).
		Floor()
	quota, err := common.WalletQuotaFromDecimalStrict(dQuota)
	if err != nil {
		return 0, err
	}
	if quota <= 0 {
		return 0, errors.New("面额过小，无法换算出有效额度")
	}
	return quota, nil
}

// computeVipBonusQuota baseQuota × bp / 10000，向下取整；decimal 运算避免
// int64 溢出（MaxWalletQuota × 10000 已超出 int64 安全范围）。
func computeVipBonusQuota(baseQuota int, bonusBasisPoints int) (int, error) {
	if baseQuota <= 0 || bonusBasisPoints <= 0 {
		return 0, nil
	}
	bonus := decimal.NewFromInt(int64(baseQuota)).
		Mul(decimal.NewFromInt(int64(bonusBasisPoints))).
		Div(decimal.NewFromInt(10000)).
		Floor()
	bonusQuota, err := common.WalletQuotaFromDecimalStrict(bonus)
	if err != nil {
		return 0, err
	}
	return bonusQuota, nil
}

// RedeemStoreCode 商城卡密兑换：锁码 → 校验 → 锁用户 → 充值前 VIP →
// 一次 creditTopUpQuota(base+bonus) → CAS 翻码 → 累计充值(仅面额) →
// 写账本 → 提交。锁顺序固定为 Redemption → User，与其它路径不得反转。
func RedeemStoreCode(key string, userId int) (result *StoreRedeemResult, err error) {
	if key == "" {
		return nil, errors.New("未提供兑换码")
	}
	if userId == 0 {
		return nil, errors.New("无效的 user id")
	}
	if !operation_setting.StoreRechargeEnabled {
		return nil, ErrStoreRechargeDisabled
	}
	redemption := &Redemption{}

	keyCol := "`key`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		keyCol = `"key"`
	}
	common.RandomSleep()
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where(keyCol+" = ?", key).First(redemption).Error; err != nil {
			return errors.New("兑换码无效")
		}
		if redemption.Status != common.RedemptionCodeStatusEnabled {
			return errors.New("兑换码无效")
		}
		if redemption.ExpiredTime != 0 && redemption.ExpiredTime < common.GetTimestamp() {
			return errors.New("兑换码无效")
		}
		if redemption.Type != RedemptionTypeBalance {
			return errors.New("兑换码类型错误")
		}
		if redemption.Source != RedemptionSourceStore {
			return errors.New("兑换码来源错误")
		}
		if redemption.FaceValueCents <= 0 || redemption.Quota <= 0 {
			return errors.New("兑换码无效")
		}

		user := &User{}
		if err := lockForUpdate(tx).First(user, userId).Error; err != nil {
			return err
		}
		balanceBefore := user.Quota
		oldTotalRecharge := user.TotalRechargeCents

		// 充值前等级：禁止用累加后的累计充值反查。
		vipLevel, ok, err := GetVIPLevelForRecharge(tx, oldTotalRecharge)
		if err != nil {
			return err
		}
		bonusBasisPoints := 0
		vipName := ""
		if ok {
			bonusBasisPoints = vipLevel.BonusBasisPoints
			vipName = vipLevel.Name
		}

		baseQuota := redemption.Quota
		bonusQuota, err := computeVipBonusQuota(baseQuota, bonusBasisPoints)
		if err != nil {
			return err
		}
		totalQuota := baseQuota + bonusQuota
		if err := common.ValidateWalletQuota(totalQuota); err != nil {
			return err
		}

		if err := creditTopUpQuota(tx, userId, totalQuota, nil); err != nil {
			return err
		}

		// CAS：只有把 enabled 翻成 used 的事务才能入账，并发兑换在此分出胜负。
		cas := tx.Model(&Redemption{}).
			Where("id = ? AND status = ?", redemption.Id, common.RedemptionCodeStatusEnabled).
			Updates(map[string]any{
				"redeemed_time": common.GetTimestamp(),
				"status":        common.RedemptionCodeStatusUsed,
				"used_user_id":  userId,
			})
		if cas.Error != nil {
			return cas.Error
		}
		if cas.RowsAffected == 0 {
			return errors.New("兑换码无效")
		}

		newTotalRecharge := oldTotalRecharge + redemption.FaceValueCents
		if newTotalRecharge < oldTotalRecharge {
			return errors.New("累计充值溢出")
		}
		if err := tx.Model(&User{}).Where("id = ?", userId).
			Update("total_recharge_cents", gorm.Expr("total_recharge_cents + ?", redemption.FaceValueCents)).Error; err != nil {
			return err
		}

		// 账本按逻辑先后拆分：DB 一次 +total，流水首尾相接保持可对账。
		now := common.GetTimestamp()
		rechargeEntry := &BalanceTransaction{
			UserId:        userId,
			CreatedAt:     now,
			Type:          BalanceTxTypeRecharge,
			AmountQuota:   baseQuota,
			BalanceBefore: balanceBefore,
			BalanceAfter:  balanceBefore + baseQuota,
			RechargeCents: redemption.FaceValueCents,
			ReferenceType: BalanceTxReferenceRedemption,
			ReferenceId:   redemption.Id,
			Description:   "兑换码充值",
			Metadata:      balanceTransactionMetaForCode(key),
		}
		if err := tx.Create(rechargeEntry).Error; err != nil {
			return err
		}
		if bonusQuota > 0 {
			bonusEntry := &BalanceTransaction{
				UserId:        userId,
				CreatedAt:     now,
				Type:          BalanceTxTypeBonus,
				AmountQuota:   bonusQuota,
				BalanceBefore: balanceBefore + baseQuota,
				BalanceAfter:  balanceBefore + totalQuota,
				RechargeCents: 0,
				ReferenceType: BalanceTxReferenceRedemption,
				ReferenceId:   redemption.Id,
				Description:   fmt.Sprintf("%s 充值加赠", vipName),
				Metadata:      balanceTransactionMetaForCode(key),
			}
			if err := tx.Create(bonusEntry).Error; err != nil {
				return err
			}
		}

		newLevel, _, err := GetVIPLevelForRecharge(tx, newTotalRecharge)
		if err != nil {
			return err
		}
		newVIPName := ""
		if newLevel != nil {
			newVIPName = newLevel.Name
		}
		result = &StoreRedeemResult{
			BaseQuota:           baseQuota,
			BonusQuota:          bonusQuota,
			TotalQuota:          totalQuota,
			Balance:             balanceBefore + totalQuota,
			FaceValueCents:      redemption.FaceValueCents,
			VIPName:             vipName,
			VIPBonusBasisPoints: bonusBasisPoints,
			NewVIPName:          newVIPName,
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrStoreRechargeDisabled) {
			return nil, err
		}
		// 内部日志只带 ID 与原因，绝不记录完整兑换码。
		common.SysError(fmt.Sprintf("store redemption failed: user %d, redemption id %d, reason: %s", userId, redemption.Id, err.Error()))
		return nil, ErrRedeemFailed
	}
	syncCreditUserQuotaCache(userId, result.TotalQuota, "store redemption")
	RecordLog(userId, LogTypeTopup, fmt.Sprintf("通过商城兑换码充值 %s（会员加赠 %s），兑换码ID %d，批次ID %d",
		logger.LogQuota(result.BaseQuota), logger.LogQuota(result.BonusQuota), redemption.Id, redemption.BatchId))
	return result, nil
}
