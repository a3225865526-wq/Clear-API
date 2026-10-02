package controller

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
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
