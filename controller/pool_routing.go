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
	platform, _, found := accountpool.Lookup(input.Platform, input.AccessType)
	if !found || (platform.Platform != "codex" && platform.Platform != "claude" && platform.Platform != "gemini_code_assist" && platform.Platform != "grok_web_sso") {
		poolError(c, model.ErrPoolInvalid)
		return
	}
	// Publication is the operator's explicit release decision (STEP 4). It is
	// deliberately not derived from capability grants, and the former
	// always-failing static admission gate is gone: requests only reach the
	// pool when master switch + channel routing + publication + group/model
	// permissions all intersect, and the executor re-checks trust and
	// capability per account×model at dispatch.
	route, err := model.SavePoolModelRoute(input.PoolModelRoute, input.ExpectedVersion)
	if err != nil {
		poolError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": route})
}
