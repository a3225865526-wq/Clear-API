package controller

import (
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
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

func GetRedemptionBatchProducts(c *gin.Context) {
	products, err := model.GetRedemptionBatchProducts()
	if err != nil {
		common.SysError("failed to load batch products: " + err.Error())
		common.ApiErrorMsg(c, "Failed to load batch products")
		return
	}
	common.ApiSuccess(c, products)
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
		common.ApiErrorMsg(c, "Invalid batch fields. Check the product ID, revision, amount, quantity and expiry.")
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
		message := "Failed to create batch. Please contact an administrator with the request time."
		// Only known business errors are exposed. Driver errors and SQL stay
		// in server logs and can never become a client-visible response.
		switch err.Error() {
		case "无效的管理员", "批次名称长度须为 1-64", "面额超出允许范围", "生成数量须为 1-100", "过期时间无效", "batch note must be at most 255 characters":
			message = "Invalid batch fields. Check the product ID, revision, amount, quantity and expiry."
		case "new inventory requires a product revision", "approved product not found":
			message = "Product not found. Use the approved catalog product ID, not the legacy product number."
		case "product revision or price mismatch":
			message = "The product revision or face value does not match the current catalog."
		case "invalid product approval state", "approval unavailable", "approval does not match current revision", "approval revoked", "evidence approval withdrawn for this revision":
			message = "The product requires a valid approval for its current revision."
		case "evidence batch unavailable", "evidence batch does not match product promise", "evidence batch code quantities do not match its frozen quota", "incomplete evidence batch binding", "evidence batch belongs to another product revision or delivery binding", "evidence inventory lacks a matching approval", "evidence inventory approval revoked", "evidence sample lacks its issuance record", "verified batch and delivery references are required", "exact batch quota must be specified":
			message = "Product delivery evidence is unavailable, disabled, expired or inconsistent. Review the delivery evidence before creating stock."
		case "batch name already exists for this product":
			message = "A batch with this name already exists for this product. Refresh the list before retrying."
		case "Restore the archived product before editing it.":
			message = err.Error()
		case "evidence sample requires one code bound to a draft revision", "delivery evidence sample requires a draft in preparation mode", "sample already issued for this product revision", "sample batch already exists for this product revision":
			message = "A delivery sample requires one code for a draft revision in preparation mode, and may only be issued once per revision."
		}
		common.ApiErrorMsg(c, message)
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

// Inspect and delete share the administrative middleware of /redemption.
// Deletion is an archive; the model repeats every check inside its transaction.
func InspectRedemptionBatchDeletion(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "无效的批次")
		return
	}
	result, err := model.GetRedemptionBatchDeletion(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiErrorMsg(c, "Batch not found")
		} else {
			common.SysError("failed to inspect redemption batch deletion: " + err.Error())
			common.ApiErrorMsg(c, "Failed to inspect batch deletion")
		}
		return
	}
	common.ApiSuccess(c, result)
}

func ArchiveRedemptionBatch(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "无效的批次")
		return
	}
	result, err := model.ArchiveRedemptionBatch(id)
	if err != nil {
		switch {
		case errors.Is(err, model.ErrRedemptionBatchEvidenceInUse):
			common.ApiErrorMsg(c, err.Error())
		case errors.Is(err, gorm.ErrRecordNotFound):
			common.ApiErrorMsg(c, "Batch not found")
		default:
			common.SysError("failed to archive redemption batch: " + err.Error())
			common.ApiErrorMsg(c, "Failed to delete batch")
		}
		return
	}
	recordManageAudit(c, "store.batch_archive", map[string]any{
		"batch_id": id, "name": result.Batch.Name, "unused": result.Unused,
		"used": result.Used, "orders": result.Orders, "balance_entries": result.BalanceEntries,
	})
	common.ApiSuccess(c, result)
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
