package model

import (
	"errors"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	RedemptionBatchStatusEnabled  = 1
	RedemptionBatchStatusDisabled = 2
)

// maxRedemptionBatchCount 与 AddRedemption 单次生成上限保持一致。
const maxRedemptionBatchCount = 100

// maxRedemptionBatchFaceValueCents 单码面额上限 ¥10,000,000.00（1e9 分）。
const maxRedemptionBatchFaceValueCents int64 = 1_000_000_000

type RedemptionBatch struct {
	Id              int    `json:"id"`
	Name            string `json:"name" gorm:"type:varchar(128);index"`
	Source          string `json:"source" gorm:"type:varchar(16);default:'STORE'"`
	Type            string `json:"type" gorm:"type:varchar(16);default:'BALANCE'"`
	FaceValueCents  int64  `json:"face_value_cents" gorm:"type:bigint"`
	Quota           int    `json:"quota" gorm:"default:0"`
	Count           int    `json:"count" gorm:"default:0"`
	ExpiredTime     int64  `json:"expired_time" gorm:"bigint"`
	Status          int    `json:"status" gorm:"default:1;index"`
	Note            string `json:"note" gorm:"type:varchar(255);default:''"`
	CreatedBy       int    `json:"created_by"`
	CreatedTime     int64  `json:"created_time" gorm:"bigint;index"`
	UpdatedTime     int64  `json:"updated_time" gorm:"bigint"`
	ProductID       string `json:"product_id" gorm:"type:varchar(64)"`
	ProductRevision int64  `json:"product_revision"`
	ApprovalID      int    `json:"approval_id"`
	DeliveryBinding string `json:"delivery_binding" gorm:"type:varchar(256)"`
	EvidenceOnly    bool   `json:"evidence_only"`
}

// RedemptionBatchStats 实时聚合（GROUP BY batch_id, status），不存计数器，
// 保证与 redemptions 表严格一致。gorm:"-"：统计字段不是真实列，
// 避免 Find 以包装结构体构建 SELECT 时带入不存在的列。
type RedemptionBatchStats struct {
	UsedCount     int64 `json:"used_count" gorm:"-"`
	UnusedCount   int64 `json:"unused_count" gorm:"-"`
	DisabledCount int64 `json:"disabled_count" gorm:"-"`
}

type RedemptionBatchWithStats struct {
	RedemptionBatch
	RedemptionBatchStats
}

type CreateRedemptionBatchInput struct {
	Name            string
	FaceValueCents  int64
	Count           int
	ExpiredTime     int64
	Note            string
	AdminUserID     int
	ProductID       string
	ProductRevision int64
	EvidenceOnly    bool
}

// CreateRedemptionBatch 创建批次并一次性生成全部商城码（单事务，任一码插入
// 失败整体回滚）。quota 按创建当时 Price 冻结。
func CreateRedemptionBatch(input CreateRedemptionBatchInput) (*RedemptionBatch, []string, error) {
	if input.EvidenceOnly && (input.ProductID == "" || input.Count != 1) {
		return nil, nil, errors.New("evidence sample requires one code bound to a draft revision")
	}
	if input.AdminUserID <= 0 {
		return nil, nil, errors.New("无效的管理员")
	}
	if utf8.RuneCountInString(input.Name) == 0 || utf8.RuneCountInString(input.Name) > 64 {
		return nil, nil, errors.New("批次名称长度须为 1-64")
	}
	if input.FaceValueCents <= 0 || input.FaceValueCents > maxRedemptionBatchFaceValueCents {
		return nil, nil, errors.New("面额超出允许范围")
	}
	if input.Count < 1 || input.Count > maxRedemptionBatchCount {
		return nil, nil, errors.New("生成数量须为 1-100")
	}
	if input.ExpiredTime != 0 && input.ExpiredTime < common.GetTimestamp() {
		return nil, nil, errors.New("过期时间无效")
	}
	quota := 0
	var err error
	if input.ProductID == "" && !input.EvidenceOnly {
		quota, err = storeQuotaForFaceValueCents(input.FaceValueCents)
		if err != nil {
			return nil, nil, err
		}
	}
	now := common.GetTimestamp()
	batch := &RedemptionBatch{
		Name:           input.Name,
		Source:         RedemptionSourceStore,
		Type:           RedemptionTypeBalance,
		FaceValueCents: input.FaceValueCents,
		Quota:          quota,
		Count:          input.Count,
		ExpiredTime:    input.ExpiredTime,
		Status:         RedemptionBatchStatusEnabled,
		Note:           input.Note,
		CreatedBy:      input.AdminUserID,
		CreatedTime:    now,
		UpdatedTime:    now,
	}
	keys := make([]string, 0, input.Count)
	err = DB.Transaction(func(tx *gorm.DB) error {
		var sampleCatalog CreditCatalog
		var sampleRow Option
		var optionCount int64
		if tx.Migrator().HasTable(&Option{}) {
			if err := tx.Model(&Option{}).Where(&Option{Key: CreditCatalogOptionKey}).Count(&optionCount).Error; err != nil {
				return err
			}
		}
		if optionCount > 0 {
			row, catalog, err := lockedCreditCatalog(tx)
			if err != nil {
				return err
			}
			if input.ProductID == "" && catalog.ApprovalSchema == 1 {
				return errors.New("new inventory requires a product revision")
			}
			if input.ProductID != "" {
				found := false
				for _, product := range catalog.Products {
					if product.ID != input.ProductID {
						continue
					}
					if product.Revision != input.ProductRevision || product.PriceCents != input.FaceValueCents {
						return errors.New("product revision or price mismatch")
					}
					if input.EvidenceOnly {
						if catalog.Unified || input.Count != 1 || product.State != "draft" || product.BaseQuota <= 0 || product.DeliveryBinding == "" {
							return errors.New("delivery evidence sample requires a draft in preparation mode")
						}
						batch.EvidenceOnly = true
						// A locking read is required here: MySQL REPEATABLE READ
						// may have an older snapshot from the initial option query.
						var issued []CreditApprovalEvent
						if err := lockForUpdate(tx).Where("product_id = ? AND revision = ? AND decision = ?", product.ID, product.Revision, "sample").Find(&issued).Error; err != nil {
							return err
						}
						if len(issued) != 0 {
							return errors.New("sample already issued for this product revision")
						}
						var existing []RedemptionBatch
						if err := lockForUpdate(tx).Where("product_id = ? AND product_revision = ? AND evidence_only = ?", product.ID, product.Revision, true).Find(&existing).Error; err != nil || len(existing) != 0 {
							return errors.New("sample batch already exists for this product revision")
						}
						sampleCatalog, sampleRow = catalog, row
					} else {
						if err := validateCreditApproval(tx, product); err != nil {
							return err
						}
					}
					quota, err := CreditProductQuota(product)
					if err != nil {
						return err
					}
					batch.Quota, batch.ProductID, batch.ProductRevision = quota, product.ID, product.Revision
					batch.ApprovalID, batch.DeliveryBinding = product.ApprovalID, product.DeliveryBinding
					found = true
				}
				if !found {
					return errors.New("approved product not found")
				}
			}
		} else {
			if input.ProductID != "" {
				return errors.New("catalog unavailable")
			}
			if tx.Migrator().HasTable(&CreditApprovalEvent{}) {
				var prepared int64
				if err := tx.Model(&CreditApprovalEvent{}).Where("decision = ?", "prepare").Count(&prepared).Error; err != nil {
					return err
				}
				if prepared != 0 {
					return errors.New("prepared catalog missing; inventory issuance refused")
				}
			}
		}
		if err := tx.Create(batch).Error; err != nil {
			return err
		}
		if input.EvidenceOnly {
			for i := range sampleCatalog.Products {
				p := &sampleCatalog.Products[i]
				if p.ID != input.ProductID {
					continue
				}
				p.EvidenceBatchID = batch.Id
				if _, err := appendCreditEvent(tx, *p, input.AdminUserID, "sample"); err != nil {
					return err
				}
			}
			// CAS claims the revision in the same transaction as batch and code
			// creation. SQLite competing readers fail rather than issue twice;
			// MySQL/PostgreSQL serialize on the existing catalog row lock.
			if err := persistCreditCatalog(tx, sampleRow, sampleCatalog); err != nil {
				return err
			}
		}
		for range input.Count {
			code := &Redemption{
				UserId:         input.AdminUserID,
				Name:           input.Name,
				Key:            common.GetUUID(),
				Status:         common.RedemptionCodeStatusEnabled,
				Quota:          batch.Quota,
				CreatedTime:    common.GetTimestamp(),
				ExpiredTime:    input.ExpiredTime,
				BatchId:        batch.Id,
				FaceValueCents: input.FaceValueCents,
				Type:           RedemptionTypeBalance,
				Source:         RedemptionSourceStore,
			}
			// SQL traces and driver errors can contain the newly issued key.
			if err := tx.Session(&gorm.Session{Logger: tx.Logger.LogMode(logger.Silent)}).Create(code).Error; err != nil {
				return errors.New("redemption code insertion failed")
			}
			keys = append(keys, code.Key)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return batch, keys, nil
}

// GetRedemptionBatches 分页列出批次并实时聚合已用/未用/已禁用数量。
func GetRedemptionBatches(startIdx int, num int) ([]*RedemptionBatchWithStats, int64, error) {
	var total int64
	if err := DB.Model(&RedemptionBatch{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	batches := []*RedemptionBatchWithStats{}
	// 显式指定 Model：Find 的目标类型是带统计字段的包装结构体，
	// 不指定时 GORM 会用目标类型推导出错误的表名。
	if err := DB.Model(&RedemptionBatch{}).Order("id desc").Limit(num).Offset(startIdx).Find(&batches).Error; err != nil {
		return nil, 0, err
	}
	if len(batches) > 0 {
		ids := make([]int, 0, len(batches))
		indexById := make(map[int]*RedemptionBatchWithStats, len(batches))
		for _, batch := range batches {
			ids = append(ids, batch.Id)
			indexById[batch.Id] = batch
		}
		var aggs []struct {
			BatchId int
			Status  int
			Cnt     int64
		}
		if err := DB.Model(&Redemption{}).
			Select("batch_id, status, COUNT(*) as cnt").
			Where("batch_id IN ?", ids).
			Group("batch_id, status").
			Scan(&aggs).Error; err != nil {
			return nil, 0, err
		}
		for _, agg := range aggs {
			batch := indexById[agg.BatchId]
			if batch == nil {
				continue
			}
			switch agg.Status {
			case common.RedemptionCodeStatusUsed:
				batch.UsedCount = agg.Cnt
			case common.RedemptionCodeStatusEnabled:
				batch.UnusedCount = agg.Cnt
			case common.RedemptionCodeStatusDisabled:
				batch.DisabledCount = agg.Cnt
			}
		}
	}
	return batches, total, nil
}

// GetUnusedRedemptionBatchKeys 返回批次内未使用码（导出用）。默认仅
// unused，不提供全量导出，避免把已用/禁用码一并带走。
func GetUnusedRedemptionBatchKeys(batchId int) ([]string, error) {
	if batchId <= 0 {
		return nil, errors.New("无效的批次")
	}
	var batch RedemptionBatch
	if err := DB.First(&batch, "id = ?", batchId).Error; err != nil {
		return nil, err
	}
	keyCol := "`key`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		keyCol = `"key"`
	}
	keys := make([]string, 0, batch.Count)
	err := DB.Model(&Redemption{}).
		Where("batch_id = ? AND status = ?", batchId, common.RedemptionCodeStatusEnabled).
		Order("id asc").
		Pluck(keyCol, &keys).Error
	return keys, err
}

// DisableRedemptionBatchById 幂等禁用批次：锁定批次行，置 Disabled，并把该
// 批次所有仍为 Enabled 的码原子翻为 Disabled（已 Used/已单独 Disabled 不动）。
// 不提供重新启用与物理删除。
func DisableRedemptionBatchById(id int) (disabledCodes int64, err error) {
	if id <= 0 {
		return 0, errors.New("无效的批次")
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		batch := &RedemptionBatch{}
		if err := lockForUpdate(tx).First(batch, "id = ?", id).Error; err != nil {
			return err
		}
		if batch.Status == RedemptionBatchStatusDisabled {
			// 幂等：重复禁用直接成功，返回 0。
			return nil
		}
		if err := tx.Model(&RedemptionBatch{}).Where("id = ?", id).
			Updates(map[string]any{
				"status":       RedemptionBatchStatusDisabled,
				"updated_time": common.GetTimestamp(),
			}).Error; err != nil {
			return err
		}
		result := tx.Model(&Redemption{}).
			Where("batch_id = ? AND status = ?", id, common.RedemptionCodeStatusEnabled).
			Update("status", common.RedemptionCodeStatusDisabled)
		if result.Error != nil {
			return result.Error
		}
		disabledCodes = result.RowsAffected
		return nil
	})
	return disabledCodes, err
}
