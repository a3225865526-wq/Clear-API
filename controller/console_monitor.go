package controller

import (
	"errors"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"strconv"
	"time"
)

func GetConsoleMonitoring(c *gin.Context) {
	data, err := model.GetConsoleMonitoring(c.Request.Context(), time.Now())
	if err != nil {
		c.JSON(500, gin.H{"success": false, "message": "Unable to load channel monitoring"})
		return
	}
	c.JSON(200, gin.H{"success": true, "data": data})
}

func GetConsoleMonitorHistory(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"success": false, "message": "Invalid monitor"})
		return
	}
	var req struct {
		Page int    `form:"page,default=1" binding:"min=1,max=1000000"`
		Size int    `form:"page_size,default=20" binding:"min=1,max=100"`
		From string `form:"from"`
		To   string `form:"to"`
	}
	if c.ShouldBindQuery(&req) != nil {
		c.JSON(400, gin.H{"success": false, "message": "Invalid pagination"})
		return
	}
	from, to := int64(0), time.Now().Unix()+1
	if req.From != "" {
		v, e := time.ParseInLocation("2006-01-02", req.From, model.ConsoleBusinessZone)
		if e != nil {
			c.JSON(400, gin.H{"success": false, "message": "Invalid date"})
			return
		}
		from = v.Unix()
	}
	if req.To != "" {
		v, e := time.ParseInLocation("2006-01-02", req.To, model.ConsoleBusinessZone)
		if e != nil {
			c.JSON(400, gin.H{"success": false, "message": "Invalid date"})
			return
		}
		to = v.AddDate(0, 0, 1).Unix()
	}
	if from >= to {
		c.JSON(400, gin.H{"success": false, "message": "Invalid date range"})
		return
	}
	data, err := model.GetConsoleMonitorHistory(c.Request.Context(), id, req.Page, req.Size, from, to)
	if err != nil {
		code := 500
		if errors.Is(err, gorm.ErrRecordNotFound) {
			code = 404
		}
		c.JSON(code, gin.H{"success": false, "message": "Unable to load monitoring history"})
		return
	}
	c.JSON(200, gin.H{"success": true, "data": data})
}
