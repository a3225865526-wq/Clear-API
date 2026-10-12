package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"strconv"

	"github.com/gin-gonic/gin"
)

func GetAllStoreProducts(c *gin.Context) {
	products, err := model.GetAllStoreProducts()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, products)
}

func AddStoreProduct(c *gin.Context) {
	common.ApiErrorMsg(c, "Legacy products are frozen; use the credit catalog approval workflow")
	return

}

func UpdateStoreProduct(c *gin.Context) {
	common.ApiErrorMsg(c, "Legacy products are frozen; use the credit catalog approval workflow")
	return

}

func DeleteStoreProduct(c *gin.Context) {
	common.ApiErrorMsg(c, "Legacy products are frozen; use the credit catalog approval workflow")
	return

}

func GetAllVIPLevels(c *gin.Context) {
	levels, err := model.GetAllVIPLevels()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, levels)
}

func AddVIPLevel(c *gin.Context) {
	level := model.VIPLevel{}
	if err := c.ShouldBindJSON(&level); err != nil {
		common.ApiError(c, err)
		return
	}
	level.Id = 0
	if err := level.Insert(); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	recordManageAudit(c, "vip.level_create", map[string]any{
		"id": level.Id, "name": level.Name, "threshold_cents": level.ThresholdCents,
	})
	common.ApiSuccess(c, level)
}

func UpdateVIPLevel(c *gin.Context) {
	level := model.VIPLevel{}
	if err := c.ShouldBindJSON(&level); err != nil {
		common.ApiError(c, err)
		return
	}
	if level.Id == 0 {
		common.ApiErrorMsg(c, "无效的等级")
		return
	}
	existing, err := model.GetVIPLevelById(level.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	existing.Name = level.Name
	existing.ThresholdCents = level.ThresholdCents
	existing.BonusBasisPoints = level.BonusBasisPoints
	existing.Enabled = level.Enabled
	existing.Sort = level.Sort
	if err := existing.Update(); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	recordManageAudit(c, "vip.level_update", map[string]any{
		"id": existing.Id, "name": existing.Name, "threshold_cents": existing.ThresholdCents,
	})
	common.ApiSuccess(c, existing)
}

func DeleteVIPLevel(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "无效的等级")
		return
	}
	if err := model.DeleteVIPLevelById(id); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "vip.level_delete", map[string]any{"id": id})
	common.ApiSuccess(c, nil)
}
