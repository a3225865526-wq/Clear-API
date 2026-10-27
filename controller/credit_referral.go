package controller

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

func GetCreditCatalog(c *gin.Context) {
	catalog, err := model.GetPublicCreditCatalog()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	for i := range catalog.Products {
		if !catalog.Products[i].Published {
			catalog.Products[i].PurchaseURL = ""
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": catalog})
}

func GetAdminCreditCatalog(c *gin.Context) {
	catalog, err := model.GetCreditCatalog()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": catalog})
}

func SaveCreditCatalog(c *gin.Context) {
	var catalog model.CreditCatalog
	if err := common.DecodeJson(c.Request.Body, &catalog); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.SaveCreditCatalog(catalog, c.GetInt("id")); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func GetCreditProductReview(c *gin.Context) {
	review, err := model.GetCreditProductReview(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, review)
}

// ResolveCreditPurchase rechecks admission at click time; a cached card cannot
// redirect after its approval was revoked or its financial revision changed.
func ResolveCreditPurchase(c *gin.Context) {
	if c.Param("kind") == "legacy" {
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil || id <= 0 {
			c.Status(http.StatusNotFound)
			return
		}
		products, err := getStoreProductsForUser()
		if err != nil {
			common.SysError("invalid purchase view: " + err.Error())
			c.Status(http.StatusServiceUnavailable)
			return
		}
		for _, p := range products {
			if p["id"] == id {
				c.Redirect(http.StatusFound, p["store_url"].(string))
				return
			}
		}
		c.Status(http.StatusNotFound)
		return
	}
	if c.Param("kind") != "product" {
		c.Status(http.StatusNotFound)
		return
	}
	revision, err := strconv.ParseInt(c.Query("revision"), 10, 64)
	if err != nil || revision < 0 {
		c.Status(http.StatusNotFound)
		return
	}
	catalog, err := model.GetPublicCreditCatalog()
	if err != nil {
		common.SysError("invalid purchase view: " + err.Error())
		c.Status(http.StatusServiceUnavailable)
		return
	}
	for _, p := range catalog.Products {
		if p.ID != c.Param("id") {
			continue
		}
		if p.Revision != revision {
			c.Status(http.StatusConflict)
			return
		}
		c.Redirect(http.StatusFound, p.PurchaseURL)
		return
	}
	c.Status(http.StatusNotFound)
}

func ChangeCreditProductState(c *gin.Context) {
	permission := authz.StoreReview
	if c.Param("action") == "activate" {
		for _, required := range []authz.Permission{authz.StoreEdit, authz.StoreReview, authz.StorePublish} {
			if !authz.Can(c.GetInt("id"), c.GetInt("role"), required) {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
		}
	}
	switch c.Param("action") {
	case "map", "archive", "restore", "delete":
		permission = authz.StoreEdit
	case "publish", "unpublish":
		permission = authz.StorePublish
	case "revoke":
		permission = authz.StoreRevoke
	}
	if !authz.Can(c.GetInt("id"), c.GetInt("role"), permission) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	if _, ok := middleware.GetSessionAuthIdentity(c); !ok {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "Product review requires a live dashboard session"})
		return
	}
	var req struct {
		Version       int64  `json:"version"`
		Revision      int64  `json:"revision"`
		Confirmation  string `json:"confirmation"`
		LegacyStoreID int    `json:"legacy_store_id"`
		Digest        string `json:"digest"`
	}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	if c.Param("action") != "submit" {
		review, err := model.GetCreditProductReview(c.Param("id"))
		if err != nil || review.Digest != req.Digest || review.Product.Revision != req.Revision {
			common.ApiErrorMsg(c, "Product review changed; reload before confirming")
			return
		}
		if c.Param("action") == "map" && req.Confirmation != "MAP "+review.Confirmation+fmt.Sprintf(":%d", req.LegacyStoreID) {
			common.ApiErrorMsg(c, "Explicit mapping confirmation required")
			return
		}
		context, err := common.Marshal(service.StoreProductOperationContext{ProductID: c.Param("id"), Revision: req.Revision, Version: req.Version, Action: c.Param("action"), Digest: req.Digest, LegacyStoreID: req.LegacyStoreID})
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeStoreProductOperation, Context: context}) == nil {
			return
		}
	}
	var err error
	if c.Param("action") == "map" {
		err = model.MapLegacyCreditProduct(c.Param("id"), req.Version, req.Revision, req.LegacyStoreID, c.GetInt("id"))
	} else {
		err = model.ChangeCreditProductState(c.Param("id"), req.Version, req.Revision, c.Param("action"), req.Confirmation, c.GetInt("id"))
	}
	if err != nil {
		if c.Param("action") == "activate" {
			common.SysError("product automatic validation failed: " + err.Error())
			code := "STORE_AUTOMATIC_VALIDATION_FAILED"
			message := "Automatic validation failed. Check the product configuration and delivery evidence, then reload before retrying."
			if err.Error() == "catalog changed; reload before saving" || err.Error() == "product revision changed" {
				code = "STORE_CATALOG_CHANGED"
				message = "The product catalog changed. Reload the page and try again."
			}
			c.JSON(http.StatusOK, gin.H{"success": false, "code": code, "message": message})
			return
		}
		common.ApiError(c, err)
		return
	}
	if action := c.Param("action"); action == "archive" || action == "restore" || action == "delete" {
		recordManageAudit(c, "store.product_"+action, map[string]any{"id": c.Param("id"), "revision": req.Revision})
	}
	common.ApiSuccess(c, nil)
}

func GetReferralSettings(c *gin.Context) {
	cfg, err := model.GetReferralConfig()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": cfg})
}

func SaveReferralSettings(c *gin.Context) {
	var cfg model.ReferralConfig
	if err := common.DecodeJson(c.Request.Body, &cfg); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.SaveReferralConfig(cfg, c.GetInt("id")); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func GetReferralSelf(c *gin.Context) {
	id := c.GetInt("id")
	code, err := model.EnsureReferralCode(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	cfg, err := model.GetReferralConfig()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var count int64
	if err = model.DB.Unscoped().Model(&model.User{}).Where("inviter_id = ? AND id <> ?", id, id).Count(&count).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var user model.User
	if err = model.DB.Select("referral_paid_quota").First(&user, id).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	page = max(0, page)
	var rewards []model.ReferralReward
	var total int64
	query := model.DB.Model(&model.ReferralReward{}).Where("inviter_id = ?", id)
	if err = query.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if err = query.Order("id DESC").Limit(20).Offset(page * 20).Find(&rewards).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"code": code, "link": "https://www.clear999.com/sign-up?aff=" + code,
		"count": count, "paid_quota": user.ReferralPaidQuota, "rule": cfg, "rewards": referralPublicRecords(rewards), "total": total}})
}

func referralPublicRecords(rewards []model.ReferralReward) []gin.H {
	items := make([]gin.H, 0, len(rewards))
	for _, r := range rewards {
		items = append(items, gin.H{"id": r.ID, "reward_quota": r.RewardQuota, "status": r.Status, "created_time": r.CreatedTime, "paid_time": r.PaidTime})
	}
	return items
}

func ListReferralRewards(c *gin.Context) {
	page, _ := strconv.Atoi(c.Query("page"))
	page = max(0, page)
	var rewards []model.ReferralReward
	var total int64
	if err := model.DB.Model(&model.ReferralReward{}).Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.DB.Order("id DESC").Limit(50).Offset(page * 50).Find(&rewards).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	type summary struct {
		Status string `json:"status"`
		Count  int64  `json:"count"`
		Quota  int64  `json:"quota"`
	}
	var sums []summary
	if err := model.DB.Model(&model.ReferralReward{}).Select("status, COUNT(*) AS count, SUM(reward_quota) AS quota").Group("status").Scan(&sums).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	// Only root gets internal identities, through an explicit response DTO.
	items := make([]gin.H, 0, len(rewards))
	for _, r := range rewards {
		items = append(items, gin.H{"reward": r, "inviter_id": r.InviterID, "invitee_id": r.InviteeID})
	}
	var audits []model.ReferralAudit
	if err := model.DB.Order("id DESC").Limit(50).Find(&audits).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"items": items, "total": total, "summary": sums, "audits": audits}})
}

func ResumeReferralReward(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid reward ID")
		return
	}
	var input struct {
		Reason string `json:"reason"`
	}
	if err = common.DecodeJson(c.Request.Body, &input); err != nil {
		common.ApiError(c, err)
		return
	}
	if err = model.ResumeReferralReward(id, c.GetInt("id"), input.Reason); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func SetRedemptionReferralKind(c *gin.Context) {
	var input struct {
		IDs    []int  `json:"ids"`
		Kind   string `json:"kind"`
		Reason string `json:"reason"`
	}
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.SetRedemptionReferralKind(input.IDs, input.Kind, input.Reason, c.GetInt("id")); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
