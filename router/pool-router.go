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
	pool.GET("/master", controller.GetPoolMaster)
	pool.PUT("/master", controller.SetPoolMaster)
	pool.GET("/channels", controller.ListPoolChannels)
	pool.PUT("/channels/:id/pool-routing", controller.SetChannelPoolRouting)
	pool.GET("/model-availability", controller.GetPoolModelAvailability)
	// Operator image source policy. This is the only place a user-visible image
	// request may learn which source is allowed; ordinary users have no
	// equivalent route and cannot influence it from the relay body.
	pool.GET("/image-policies", controller.ListImageSourcePolicies)
	pool.GET("/image-policy", controller.GetImageSourcePolicy)
	pool.PUT("/image-policy", controller.SaveImageSourcePolicy)
	// Reconciliation view for controlled image requests that still own an open
	// reservation (UNKNOWN, or terminated after a possibly-real send). Read-only:
	// it lists state and never releases, retries or settles anything by itself.
	pool.GET("/image-executions", controller.ListPendingImageExecutions)
	pool.GET("/fallback-policy", controller.GetPoolFallbackPolicy)
	pool.PUT("/fallback-policy", controller.SavePoolFallbackPolicy)
	pool.GET("/accounts", controller.ListPoolAccounts)
	pool.POST("/accounts", controller.CreatePoolAccount)
	pool.POST("/accounts/import", controller.ImportPoolCredentials)
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
