package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/accountpool"
	"github.com/gin-gonic/gin"
)

func ListPoolModelRoutes(c *gin.Context) {
	routes, err := model.ListPoolModelRoutes()
	if err != nil {
		poolError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": routes})
}

func SavePoolModelRoute(c *gin.Context) {
	var input struct {
		model.PoolModelRoute
		ExpectedVersion int `json:"expected_version"`
	}
	if c.ShouldBindJSON(&input) != nil {
		poolError(c, model.ErrPoolInvalid)
		return
	}
	platform, access, found := accountpool.Lookup(input.Platform, input.AccessType)
	if !found || (platform.Platform != "codex" && platform.Platform != "claude" && platform.Platform != "gemini_code_assist" && platform.Platform != "grok_web_sso") {
		poolError(c, model.ErrPoolInvalid)
		return
	}
	if input.Source == "pool" && input.Enabled {
		admission := accountpool.ModelAdmissionFor(platform.Platform, access.Type, input.Model)
		if err := admission.Check([]string{"text_chat", "exact_usage"}, accountpool.AdapterVersion); err != nil {
			poolError(c, accountpool.ErrPoolNotAdmitted)
			return
		}
	}
	route, err := model.SavePoolModelRoute(input.PoolModelRoute, input.ExpectedVersion)
	if err != nil {
		poolError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": route})
}
