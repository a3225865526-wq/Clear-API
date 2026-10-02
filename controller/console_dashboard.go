package controller

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"strconv"
	"time"
)

func GetConsoleDashboard(c *gin.Context) {
	days, err := strconv.Atoi(c.DefaultQuery("days", "7"))
	if err != nil || (days != 7 && days != 15 && days != 30) {
		c.JSON(400, gin.H{"success": false, "message": "Invalid time range"})
		return
	}
	data, err := model.GetConsoleDashboard(c.Request.Context(), c.GetInt("id"), days, time.Now())
	if err != nil {
		c.JSON(500, gin.H{"success": false, "message": "Unable to load account statistics"})
		return
	}
	c.JSON(200, gin.H{"success": true, "data": data})
}
