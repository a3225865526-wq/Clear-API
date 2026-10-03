package middleware

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// Runs after relay cleanup, including canceled requests, panics and WebSocket
// sessions. Only reservations actually created by this process are registered.
func BillingRequestExecution() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			for _, id := range c.GetStringSlice(model.BillingExecutionContextKey) {
				model.EndBillingRequestExecution(id)
			}
		}()
		c.Next()
	}
}
