package controller

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// RedeemStoreCode 商城卡密兑换入口（POST /api/clearapi/store/redeem）。
// 只接受 Source=STORE 且 Type=BALANCE 的兑换码；除功能开关外，所有失败
// 统一返回模糊提示，防止兑换码状态探测。
func RedeemStoreCode(c *gin.Context) {
	if !operation_setting.StoreRechargeEnabled {
		common.ApiErrorMsg(c, "商城充值当前未开启")
		return
	}
	var req topUpRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	key := strings.TrimSpace(req.Key)
	if key == "" || len(key) > 64 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	id := c.GetInt("id")
	lock := getTopUpLock(id)
	if !lock.TryLock() {
		common.ApiErrorI18n(c, i18n.MsgUserTopUpProcessing)
		return
	}
	defer lock.Unlock()
	result, err := model.RedeemStoreCode(key, id)
	if err != nil {
		if errors.Is(err, model.ErrStoreRechargeDisabled) {
			common.ApiErrorMsg(c, "商城充值当前未开启")
			return
		}
		common.ApiErrorI18n(c, i18n.MsgRedeemFailed)
		return
	}
	common.ApiSuccess(c, result)
}

// getStoreProductsForUser 下发启用中的商城档位；开关关闭时返回空列表。
func getStoreProductsForUser() []map[string]any {
	products := []map[string]any{}
	if !operation_setting.StoreRechargeEnabled {
		return products
	}
	list, err := model.GetEnabledStoreProducts()
	if err != nil {
		common.SysError("failed to load store products: " + err.Error())
		return products
	}
	for _, product := range list {
		products = append(products, map[string]any{
			"id":               product.Id,
			"name":             product.Name,
			"face_value_cents": product.FaceValueCents,
			"store_url":        product.StoreUrl,
			"description":      product.Description,
			"sort":             product.Sort,
		})
	}
	return products
}

// getUserVipInfo 返回用户当前等级与累计充值；next_level 为 nil 表示已最高。
func getUserVipInfo(userId int) map[string]any {
	if userId == 0 {
		return nil
	}
	user, err := model.GetUserById(userId, false)
	if err != nil {
		return nil
	}
	total := user.TotalRechargeCents
	info := map[string]any{
		"name":                 "",
		"bonus_basis_points":   0,
		"total_recharge_cents": total,
		"next_level":           nil,
	}
	if level, ok, err := model.GetVIPLevelForRecharge(nil, total); err == nil && ok {
		info["name"] = level.Name
		info["bonus_basis_points"] = level.BonusBasisPoints
	}
	if next, err := model.GetNextVIPLevel(total); err == nil && next != nil {
		info["next_level"] = map[string]any{
			"name":            next.Name,
			"threshold_cents": next.ThresholdCents,
			"remaining_cents": next.ThresholdCents - total,
		}
	}
	return info
}
