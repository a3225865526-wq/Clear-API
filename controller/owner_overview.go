package controller

import (
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetOwnerOverview(c *gin.Context) {
	var from, to int64
	for _, field := range []struct {
		name   string
		target *int64
	}{{"from", &from}, {"to", &to}} {
		if text, supplied := c.GetQuery(field.name); supplied {
			value, err := strconv.ParseInt(text, 10, 64)
			if err != nil {
				financeError(c, model.ErrFinanceInvalid)
				return
			}
			*field.target = value
		}
	}
	period := c.Query("period")
	if period != "" && (c.Request.URL.Query().Has("from") || c.Request.URL.Query().Has("to")) {
		financeError(c, model.ErrFinanceInvalid)
		return
	}
	if period == "" && !c.Request.URL.Query().Has("from") && !c.Request.URL.Query().Has("to") {
		period = "today"
	}
	now := time.Now()
	from, to, err := model.OwnerOverviewRange(period, from, to, now)
	if err != nil {
		financeError(c, err)
		return
	}
	result, err := model.GetOwnerOverview(c.Request.Context(), model.DB, model.LOG_DB, from, to, now)
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(200, gin.H{"success": true, "data": result})
}
