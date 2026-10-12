package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

func GetAllRedemptionBatches(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	batches, total, err := model.GetRedemptionBatches(pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(batches)
	common.ApiSuccess(c, pageInfo)
}

// AddRedemptionBatch 创建商城批次并一次性生成全部码（单事务）。审计只记录
// 数量与面额，绝不记录兑换码原文。
func AddRedemptionBatch(c *gin.Context) {
	var req struct {
		Name            string `json:"name"`
		FaceValueCents  int64  `json:"face_value_cents"`
		Count           int    `json:"count"`
		ExpiredTime     int64  `json:"expired_time"`
		Note            string `json:"note"`
		ProductID       string `json:"product_id"`
		ProductRevision int64  `json:"product_revision"`
		EvidenceOnly    bool   `json:"evidence_only"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.EvidenceOnly && c.GetInt("role") != common.RoleRootUser {
		common.ApiErrorMsg(c, "Only root may create a delivery evidence sample")
		return
	}
	if req.EvidenceOnly {
		if _, ok := middleware.GetSessionAuthIdentity(c); !ok {
			common.ApiErrorMsg(c, "Evidence samples require a live root session")
			return
		}
	}
	batch, keys, err := model.CreateRedemptionBatch(model.CreateRedemptionBatchInput{
		Name:           req.Name,
		FaceValueCents: req.FaceValueCents,
		Count:          req.Count,
		ExpiredTime:    req.ExpiredTime,
		Note:           req.Note,
		AdminUserID:    c.GetInt("id"),
		ProductID:      req.ProductID, ProductRevision: req.ProductRevision, EvidenceOnly: req.EvidenceOnly,
	})
	if err != nil {
		common.SysError("failed to create redemption batch: " + err.Error())
		common.ApiErrorMsg(c, "创建批次失败")
		return
	}
	recordManageAudit(c, "store.batch_create", map[string]any{
		"batch_id":         batch.Id,
		"name":             batch.Name,
		"count":            batch.Count,
		"face_value_cents": batch.FaceValueCents,
		"quota":            logger.LogQuota(batch.Quota),
	})
	common.ApiSuccess(c, gin.H{"batch": batch, "keys": keys})
}

// GetRedemptionBatchCodes 导出批次内未使用码（默认仅 unused）。
func GetRedemptionBatchCodes(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "无效的批次")
		return
	}
	keys, err := model.GetUnusedRedemptionBatchKeys(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "store.batch_export", map[string]any{
		"batch_id": id,
		"count":    len(keys),
	})
	common.ApiSuccess(c, keys)
}

// DisableRedemptionBatch 幂等禁用批次及其全部未使用码；重复调用返回成功。
func DisableRedemptionBatch(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "无效的批次")
		return
	}
	disabled, err := model.DisableRedemptionBatchById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "store.batch_disable", map[string]any{
		"batch_id": id,
		"count":    disabled,
	})
	common.ApiSuccess(c, disabled)
}
