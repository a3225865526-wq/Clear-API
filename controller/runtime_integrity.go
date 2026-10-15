package controller

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetRuntimeIntegrity(c *gin.Context) {
	getRuntimeIntegrity(c, nil)
}

// RuntimeIntegrityHandler supports explicit test dependency wiring. The actual
// API router remains bound to GetRuntimeIntegrity and its production provider.
func RuntimeIntegrityHandler(provider model.RuntimeIdentityProvider) gin.HandlerFunc {
	return func(c *gin.Context) { getRuntimeIntegrity(c, provider) }
}

func getRuntimeIntegrity(c *gin.Context, provider model.RuntimeIdentityProvider) {
	if c.Request.URL.RawQuery != "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "status": "BLOCKED", "code": "PROBE_UNAVAILABLE"})
		return
	}
	report, err := service.CollectRuntimeIntegrityReportWithProvider(c.Request.Context(), runtimePurchasableView, provider)
	if err != nil {
		code := "PROBE_UNAVAILABLE"
		switch {
		case errors.Is(err, model.ErrRuntimeConfigMismatch):
			code = "RUNTIME_CONFIG_MISMATCH"
		case errors.Is(err, model.ErrApprovalViewInvalid):
			code = "APPROVAL_VIEW_INVALID"
		case errors.Is(err, model.ErrProductViewInvalid):
			code = "PRODUCT_VIEW_INVALID"
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "status": "BLOCKED", "code": code})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": report})
}

// runtimePurchasableView reuses both live buying surfaces. Neither a raw table
// hash nor credit_catalog alone describes the wallet's legacy compatibility UI.
func runtimePurchasableView() ([]byte, error) {
	wallet, err := getStoreProductsForUser()
	if err != nil {
		return nil, err
	}
	shop, err := model.GetPublicCreditCatalog()
	if err != nil {
		return nil, err
	}
	return common.Marshal(struct {
		Wallet []map[string]any      `json:"wallet"`
		Shop   []model.CreditProduct `json:"shop"`
	}{wallet, shop.Products})
}
