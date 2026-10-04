package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

// RegisterUpstreamPoolRoutes mounts the site-owner upstream account pool.
// Every endpoint, including capability, runs behind PoolOwnerAuth: the module
// can be disabled (then 404) but never widened beyond authenticated user ID 1
// with the project root role.
func RegisterUpstreamPoolRoutes(api *gin.RouterGroup) {
	pool := api.Group("/owner/pool")
	pool.Use(middleware.DisableCache(), middleware.PoolOwnerAuth())
	pool.GET("/capability", controller.PoolCapability)
	pool.GET("/executors", controller.ListPoolExecutors)
	pool.GET("/catalog", controller.GetPoolCatalog)
	pool.GET("/model-routes", controller.ListPoolModelRoutes)
	pool.PUT("/model-routes", controller.SavePoolModelRoute)
	pool.GET("/accounts", controller.ListPoolAccounts)
	pool.POST("/accounts", controller.CreatePoolAccount)
	pool.GET("/accounts/:id", controller.GetPoolAccount)
	pool.GET("/accounts/:id/capabilities", controller.GetPoolAccountCapabilities)
	pool.GET("/accounts/:id/quota", controller.GetPoolAccountQuota)
	pool.POST("/accounts/:id/auth/start", controller.StartPoolAuth)
	pool.POST("/accounts/:id/auth/complete", controller.CompletePoolAuth)
	pool.POST("/accounts/:id/auth/cancel", controller.CancelPoolAuth)
	pool.POST("/accounts/:id/pause", controller.PausePoolAccount)
	pool.POST("/accounts/:id/resume", controller.ResumePoolAccount)
	pool.POST("/accounts/:id/reauth", controller.StartPoolReauth)
	pool.POST("/accounts/:id/delete", controller.DeletePoolAccount)
	pool.POST("/accounts/:id/sync", controller.SyncPoolAccount)
}
