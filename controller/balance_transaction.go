package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// balanceTransactionDTO 显式挑选对外字段；metadata 只透出兑换码末 4 位。
type balanceTransactionDTO struct {
	Id            int    `json:"id"`
	UserId        int    `json:"user_id"`
	CreatedAt     int64  `json:"created_at"`
	Type          string `json:"type"`
	AmountQuota   int    `json:"amount_quota"`
	BalanceBefore int    `json:"balance_before"`
	BalanceAfter  int    `json:"balance_after"`
	RechargeCents int64  `json:"recharge_cents"`
	ReferenceType string `json:"reference_type"`
	ReferenceId   int    `json:"reference_id"`
	Description   string `json:"description"`
	CodeTail      string `json:"code_tail,omitempty"`
}

func toBalanceTransactionDTOs(items []*model.BalanceTransaction) []balanceTransactionDTO {
	dtos := make([]balanceTransactionDTO, 0, len(items))
	for _, item := range items {
		dtos = append(dtos, balanceTransactionDTO{
			Id:            item.Id,
			UserId:        item.UserId,
			CreatedAt:     item.CreatedAt,
			Type:          item.Type,
			AmountQuota:   item.AmountQuota,
			BalanceBefore: item.BalanceBefore,
			BalanceAfter:  item.BalanceAfter,
			RechargeCents: item.RechargeCents,
			ReferenceType: item.ReferenceType,
			ReferenceId:   item.ReferenceId,
			Description:   item.Description,
			CodeTail:      model.BalanceTransactionCodeTail(item.Metadata),
		})
	}
	return dtos
}

// GetUserBalanceTransactions GET /api/user/balance_transactions/self
// 仅查询本人流水，user_id 取自会话，不接受请求参数。
func GetUserBalanceTransactions(c *gin.Context) {
	userId := c.GetInt("id")
	pageInfo := common.GetPageQuery(c)
	items, total, err := model.GetBalanceTransactions(userId, c.Query("type"), pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(toBalanceTransactionDTOs(items))
	common.ApiSuccess(c, pageInfo)
}

// GetAllBalanceTransactions GET /api/user/balance_transactions
// 管理员查询，可按 user_id/type 过滤；无写入接口。
func GetAllBalanceTransactions(c *gin.Context) {
	userId, _ := strconv.Atoi(c.Query("user_id"))
	pageInfo := common.GetPageQuery(c)
	items, total, err := model.GetAllBalanceTransactions(userId, c.Query("type"), pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(toBalanceTransactionDTOs(items))
	common.ApiSuccess(c, pageInfo)
}
