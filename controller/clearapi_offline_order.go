package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type createOfflineOrderRequest struct {
	OrderNo      string `json:"order_no"`
	MoneyCents   int64  `json:"money_cents"`
	QuotaPerCode int    `json:"quota_per_code"`
	Count        int    `json:"count"`
	PaidAt       int64  `json:"paid_at"`
	ExpiredTime  int64  `json:"expired_time"`
}

type offlineOrderCodeView struct {
	ID     int    `json:"id"`
	Key    string `json:"key"`
	Status int    `json:"status"`
}

type offlineOrderView struct {
	model.OfflineOrder
	Codes []offlineOrderCodeView `json:"codes"`
}

func CreateClearAPIOfflineOrder(c *gin.Context) {
	var req createOfflineOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	input := model.CreateOfflineOrderInput{
		OrderNo: strings.TrimSpace(req.OrderNo), MoneyCents: req.MoneyCents,
		QuotaPerCode: req.QuotaPerCode, CodeCount: req.Count,
		PaidAt: req.PaidAt, ExpiredTime: req.ExpiredTime, AdminUserID: c.GetInt("id"),
	}
	order, keys, err := model.CreateOfflineOrder(input)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "clearapi.offline_order_create", map[string]any{
		"order_id": order.Id, "order_no": order.OrderNo, "money_cents": order.MoneyCents,
		"quota_per_code": order.QuotaPerCode, "count": order.CodeCount,
	})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"order": order, "codes": keys}})
}

func ListClearAPIOfflineOrders(c *gin.Context) {
	page := common.GetPageQuery(c)
	orders, total, err := model.GetOfflineOrders(page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	views := make([]offlineOrderView, 0, len(orders))
	for _, order := range orders {
		view := offlineOrderView{OfflineOrder: order, Codes: make([]offlineOrderCodeView, 0, len(order.Codes))}
		for _, link := range order.Codes {
			view.Codes = append(view.Codes, offlineOrderCodeView{
				ID: link.RedemptionID, Key: model.MaskTokenKey(link.Redemption.Key), Status: link.Redemption.Status,
			})
		}
		views = append(views, view)
	}
	page.SetTotal(int(total))
	page.SetItems(views)
	common.ApiSuccess(c, page)
}

func VoidClearAPIOfflineOrder(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := model.VoidOfflineOrder(id); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "clearapi.offline_order_void", map[string]any{"order_id": id})
	common.ApiSuccess(c, nil)
}

func RedeemClearAPIOfflineCode(c *gin.Context) {
	var req topUpRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Key) == "" {
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
	quota, err := model.RedeemOffline(strings.TrimSpace(req.Key), id)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgRedeemFailed)
		return
	}
	common.ApiSuccess(c, quota)
}
