package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetCreditCatalog(c *gin.Context) {
	catalog, err := model.GetCreditCatalog()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	for i := range catalog.Products {
		if !catalog.Products[i].Published {
			catalog.Products[i].PurchaseURL = ""
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": catalog, "legacy_purchase_url": model.LegacyCreditPurchaseURL})
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
