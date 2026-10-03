package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetBillingReconciliation(c *gin.Context) {
	if !model.BillingReconciliationEnabled() {
		financeError(c, model.ErrFinanceUnavailable)
		return
	}
	page, err := financePage(c)
	if err != nil {
		financeError(c, err)
		return
	}
	scope := c.DefaultQuery("scope", "pending")
	items, total, err := model.ListBillingReconciliation(model.DB, scope, page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"items": items, "total": total}})
}

func DecideBillingReconciliation(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		financeError(c, model.ErrFinanceInvalid)
		return
	}
	var input model.BillingDecisionInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		financeError(c, model.ErrFinanceInvalid)
		return
	}
	decision, err := model.DecideBillingReservation(model.DB, id, c.GetInt("id"), input, c.GetString(common.RequestIdKey))
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": decision})
}
