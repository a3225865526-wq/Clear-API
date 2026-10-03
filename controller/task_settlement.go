package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetTaskSettlements(c *gin.Context) {
	page, err := financePage(c)
	if err != nil {
		financeError(c, err)
		return
	}
	rows, total, err := model.ListTaskSettlements(model.DB, c.DefaultQuery("scope", "pending"), page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"items": rows, "total": total}})
}

func RetryTaskSettlement(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		financeError(c, model.ErrFinanceInvalid)
		return
	}
	var input model.TaskSettlementRetryInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	if common.DecodeJson(c.Request.Body, &input) != nil {
		financeError(c, model.ErrFinanceInvalid)
		return
	}
	r, err := model.RetryTaskSettlement(model.DB, id, c.GetInt("id"), input)
	if err != nil {
		financeError(c, err)
		return
	}
	_ = model.DeliverTaskSettlementLogs(model.DB, model.LOG_DB)
	if err := model.DB.First(r, r.ID).Error; err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": r})
}
