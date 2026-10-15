package model

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const CreditApprovalPolicy = "solo-operator-v1"

// CreditApprovalEvent is the authoritative, transactional approval history.
// It contains evidence references and batch metadata, never redemption keys.
type CreditApprovalEvent struct {
	ID                  int    `json:"id"`
	ProductID           string `json:"product_id" gorm:"type:varchar(64);index"`
	Revision            int64  `json:"revision"`
	Digest              string `json:"digest" gorm:"type:varchar(64)"`
	Policy              string `json:"policy" gorm:"type:varchar(64)"`
	EditedBy            int    `json:"edited_by"`
	ActorID             int    `json:"actor_id"`
	Decision            string `json:"decision" gorm:"type:varchar(24)"`
	CreatedTime         int64  `json:"created_time"`
	EvidenceBatchID     int    `json:"evidence_batch_id"`
	DeliveryEvidence    string `json:"delivery_evidence" gorm:"type:varchar(256)"`
	SoloOperator        bool   `json:"solo_operator"`
	ReferenceApprovalID int    `json:"reference_approval_id" gorm:"index"`
}

type CreditProductReview struct {
	Product      CreditProduct         `json:"product"`
	Digest       string                `json:"digest"`
	Confirmation string                `json:"confirmation"`
	Events       []CreditApprovalEvent `json:"events"`
}

func validCreditProductState(state string) bool {
	switch state {
	case "draft", "pending", "approved", "published", "rejected", "revoked":
		return true
	}
	return false
}

// CreditProductDigest binds all user-facing promises and delivery/mapping data.
// Sorting, operation state and timestamps deliberately do not change approval.
func CreditProductDigest(p CreditProduct) (string, error) {
	// Explicit v1 terms keep future display/transport fields out of approvals.
	terms := struct {
		ID               string `json:"id"`
		Name             string `json:"name"`
		Description      string `json:"description"`
		Currency         string `json:"currency"`
		PriceCents       int64  `json:"price_cents"`
		CreditCents      int64  `json:"credit_cents"`
		BaseQuota        int    `json:"base_quota"`
		PurchaseURL      string `json:"purchase_url"`
		EvidenceBatchID  int    `json:"evidence_batch_id"`
		DeliveryEvidence string `json:"delivery_evidence"`
		DeliveryBinding  string `json:"delivery_binding"`
		LegacyStoreID    int    `json:"legacy_store_id"`
	}{p.ID, p.Name, p.Description, p.Currency, p.PriceCents, p.CreditCents, p.BaseQuota, p.PurchaseURL, p.EvidenceBatchID, p.DeliveryEvidence, p.DeliveryBinding, p.LegacyStoreID}
	raw, err := common.Marshal(terms)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}

func CreditProductQuota(p CreditProduct) (int, error) {
	if p.BaseQuota <= 0 {
		return 0, errors.New("exact batch quota must be specified")
	}
	if err := common.ValidateWalletQuota(p.BaseQuota); err != nil {
		return 0, err
	}
	return p.BaseQuota, nil
}

func lockedCreditCatalog(tx *gorm.DB) (Option, CreditCatalog, error) {
	var row Option
	if err := lockForUpdate(tx).Where(&Option{Key: CreditCatalogOptionKey}).First(&row).Error; err != nil {
		return row, CreditCatalog{}, err
	}
	var c CreditCatalog
	err := common.UnmarshalJsonStr(row.Value, &c)
	if err == nil && c.ApprovalSchema == 1 {
		c.Products = c.ApprovalProducts
	}
	return row, c, err
}

func persistCreditCatalog(tx *gorm.DB, row Option, c CreditCatalog) error {
	if err := c.Validate(); err != nil {
		return err
	}
	c.Version++
	c.ApprovalProducts = c.Products
	if c.Unified {
		products, err := effectiveCreditProducts(tx, c)
		if err != nil {
			return err
		}
		c.Products = products
	} else {
		c.Products = c.LegacyCatalog
	}
	raw, err := common.Marshal(c)
	if err != nil {
		return err
	}
	result := tx.Model(&Option{}).Where(&Option{Key: CreditCatalogOptionKey}).Where("value = ?", row.Value).Update("value", string(raw))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("catalog changed; reload before saving")
	}
	return nil
}

// PrepareCreditCatalog freezes existing compatibility entries exactly once.
// This is a schema preparation, not a mapping, approval or live-view activation.
func PrepareCreditCatalog() error {
	return DB.Transaction(func(tx *gorm.DB) error {
		row, c, err := lockedCreditCatalog(tx)
		if err != nil {
			return err
		}
		if c.ApprovalSchema == 1 {
			return nil
		}
		if c.ApprovalSchema != 0 {
			return errors.New("unsupported credit approval schema")
		}
		if err := c.Validate(); err != nil {
			return err
		}
		var prepared int64
		if err := tx.Model(&CreditApprovalEvent{}).Where("decision = ?", "prepare").Count(&prepared).Error; err != nil {
			return err
		}
		if prepared != 0 {
			return errors.New("approval schema was removed; refusing to recapture legacy entries")
		}
		if err := tx.Order("id").Find(&c.LegacyProducts).Error; err != nil {
			return err
		}
		for _, p := range c.LegacyProducts {
			if p.Enabled && (p.validate() != nil || !TrustedCreditPurchaseURL(p.StoreUrl)) {
				return errors.New("enabled legacy product is not a legal Catfk compatibility entry")
			}
		}
		c.LegacyCatalog = append([]CreditProduct{}, c.Products...)
		for i := range c.Products {
			p := &c.Products[i]
			p.Currency, p.Revision, p.State = "CNY", 1, "draft"
			p.Published, p.DeliveryVerified = false, false
			p.ApprovalID, p.EditedBy, p.LegacyStoreID, p.LegacyWithdrawn = 0, 0, 0, false
		}
		c.ApprovalSchema = 1
		if err := tx.Model(&RedemptionBatch{}).Order("id").Pluck("id", &c.LegacyEvidenceBatchIDs).Error; err != nil {
			return err
		}
		if err := tx.Create(&CreditApprovalEvent{Decision: "prepare", Policy: CreditApprovalPolicy, CreatedTime: common.GetTimestamp(), SoloOperator: true}).Error; err != nil {
			return err
		}
		return persistCreditCatalog(tx, row, c)
	})
}

func appendCreditEvent(tx *gorm.DB, p CreditProduct, actor int, decision string) (*CreditApprovalEvent, error) {
	digest, err := CreditProductDigest(p)
	if err != nil {
		return nil, err
	}
	event := &CreditApprovalEvent{ProductID: p.ID, Revision: p.Revision, Digest: digest,
		Policy: CreditApprovalPolicy, EditedBy: p.EditedBy, ActorID: actor, Decision: decision,
		CreatedTime: common.GetTimestamp(), EvidenceBatchID: p.EvidenceBatchID,
		DeliveryEvidence: p.DeliveryEvidence, SoloOperator: true, ReferenceApprovalID: p.ApprovalID}
	return event, tx.Create(event).Error
}

func SaveCreditProductDrafts(in CreditCatalog, actor int) error {
	if actor <= 0 {
		return errors.New("invalid operator")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		row, c, err := lockedCreditCatalog(tx)
		if err != nil {
			return err
		}
		if c.ApprovalSchema != 1 || in.Version != c.Version {
			return errors.New("catalog changed; reload before saving")
		}
		previous := make(map[string]CreditProduct, len(c.Products))
		for _, p := range c.Products {
			previous[p.ID] = p
		}
		products := make([]CreditProduct, 0, len(in.Products))
		for _, p := range in.Products {
			p.BaseCredit, p.PurchasePath = "", ""
			if p.BaseQuota > 0 {
				if err := common.ValidateWalletQuota(p.BaseQuota); err != nil {
					return err
				}
				p.CreditCents = decimal.NewFromInt(int64(p.BaseQuota)).Mul(decimal.NewFromInt(100)).Div(decimal.NewFromFloat(common.QuotaPerUnit)).Floor().IntPart()
			}
			if p.Currency != "" && p.Currency != "CNY" {
				return errors.New("product currency must be CNY")
			}
			old, exists := previous[p.ID]
			if !exists {
				if p.Published || p.DeliveryVerified || p.ApprovalID != 0 || p.LegacyStoreID != 0 || p.LegacyWithdrawn || (p.State != "" && p.State != "draft") {
					return errors.New("use dedicated approval operations")
				}
				p.Currency, p.Revision, p.State, p.EditedBy = "CNY", 1, "draft", actor
			} else {
				if p.Revision != old.Revision {
					return errors.New("product revision changed")
				}
				if p.Published != old.Published || p.DeliveryVerified != old.DeliveryVerified || p.State != old.State || p.ApprovalID != old.ApprovalID || p.LegacyStoreID != old.LegacyStoreID || p.LegacyWithdrawn != old.LegacyWithdrawn {
					return errors.New("use dedicated approval operations")
				}
				p.EditedBy = old.EditedBy
				a, err := CreditProductDigest(old)
				if err != nil {
					return err
				}
				b, err := CreditProductDigest(p)
				if err != nil {
					return err
				}
				if a != b {
					p.Revision, p.State, p.EditedBy = old.Revision+1, "draft", actor
					p.ApprovalID, p.Published, p.DeliveryVerified = 0, false, false
				}
				delete(previous, p.ID)
			}
			if len(p.DeliveryEvidence) > 256 || len(p.DeliveryBinding) > 256 {
				return errors.New("invalid evidence reference")
			}
			if _, err := appendCreditEvent(tx, p, actor, "edit"); err != nil {
				return err
			}
			products = append(products, p)
		}
		// Keep stable identities and history; operators unpublish instead of deleting.
		if len(previous) != 0 {
			return errors.New("unpublish products instead of deleting their history")
		}
		c.Products = products
		return persistCreditCatalog(tx, row, c)
	})
}

func validateCreditDelivery(tx *gorm.DB, p CreditProduct) error {
	if p.BaseQuota <= 0 {
		return errors.New("exact batch quota must be specified")
	}
	if p.Currency != "CNY" || p.EvidenceBatchID <= 0 || strings.TrimSpace(p.DeliveryEvidence) == "" || strings.TrimSpace(p.DeliveryBinding) == "" || !TrustedCreditPurchaseURL(p.PurchaseURL) {
		return errors.New("verified batch and delivery references are required")
	}
	var batch RedemptionBatch
	if err := tx.First(&batch, p.EvidenceBatchID).Error; err != nil {
		return errors.New("evidence batch unavailable")
	}
	var withdrawn int64
	if err := tx.Model(&CreditApprovalEvent{}).Where("decision = ? AND product_id = ? AND revision = ? AND evidence_batch_id = ?", "revoke", p.ID, p.Revision, batch.Id).Count(&withdrawn).Error; err != nil || withdrawn != 0 {
		return errors.New("evidence approval withdrawn for this revision")
	}
	quota, err := CreditProductQuota(p)
	if batch.ProductID == "" {
		_, catalog, err := lockedCreditCatalog(tx)
		if err != nil || !slices.Contains(catalog.LegacyEvidenceBatchIDs, batch.Id) || batch.ProductRevision != 0 || batch.DeliveryBinding != "" || batch.ApprovalID != 0 || batch.EvidenceOnly {
			return errors.New("unbound batch is not frozen legacy evidence")
		}
		var conflicting int64
		if err := tx.Model(&CreditApprovalEvent{}).Where("decision = ? AND evidence_batch_id = ? AND (product_id <> ? OR revision <> ?)", "legacy_delivery", batch.Id, p.ID, p.Revision).Count(&conflicting).Error; err != nil || conflicting != 0 {
			return errors.New("legacy evidence belongs to another product revision")
		}
	} else {
		var missing int64
		if err := tx.Model(&RedemptionBatch{}).Where("id = ?", batch.Id).Where("product_id IS NULL OR product_revision IS NULL OR delivery_binding IS NULL OR approval_id IS NULL OR evidence_only IS NULL").Count(&missing).Error; err != nil || missing != 0 {
			return errors.New("incomplete evidence batch binding")
		}
		if batch.ProductID != p.ID || batch.ProductRevision != p.Revision || batch.DeliveryBinding != p.DeliveryBinding {
			return errors.New("evidence batch belongs to another product revision or delivery binding")
		}
		if batch.EvidenceOnly {
			var sample CreditApprovalEvent
			if batch.Count != 1 || batch.ApprovalID != 0 || tx.Where("decision = ? AND product_id = ? AND revision = ? AND evidence_batch_id = ?", "sample", p.ID, p.Revision, batch.Id).First(&sample).Error != nil || sample.Policy != CreditApprovalPolicy {
				return errors.New("evidence sample lacks its issuance record")
			}
		} else {
			var approval CreditApprovalEvent
			if err := tx.First(&approval, batch.ApprovalID).Error; err != nil || approval.Decision != "approve" || approval.ProductID != p.ID || approval.Revision != p.Revision || approval.Policy != CreditApprovalPolicy {
				return errors.New("evidence inventory lacks a matching approval")
			}
			var revoked int64
			if err := tx.Model(&CreditApprovalEvent{}).Where("decision = ? AND reference_approval_id = ?", "revoke", batch.ApprovalID).Count(&revoked).Error; err != nil || revoked != 0 {
				return errors.New("evidence inventory approval revoked")
			}
		}
	}
	if err != nil || quota <= 0 || batch.Quota != quota || batch.FaceValueCents != p.PriceCents || batch.Source != RedemptionSourceStore || batch.Type != RedemptionTypeBalance || batch.Status != RedemptionBatchStatusEnabled || (batch.ExpiredTime != 0 && batch.ExpiredTime < common.GetTimestamp()) {
		return errors.New("evidence batch does not match product promise")
	}
	var total, mismatches int64
	codes := tx.Model(&Redemption{}).Where("batch_id = ?", batch.Id)
	if err := codes.Count(&total).Error; err != nil {
		return err
	}
	if err := tx.Model(&Redemption{}).Where("batch_id = ?", batch.Id).
		Where("quota IS NULL OR face_value_cents IS NULL OR source IS NULL OR type IS NULL OR quota <> ? OR face_value_cents <> ? OR source <> ? OR type <> ?", p.BaseQuota, p.PriceCents, RedemptionSourceStore, RedemptionTypeBalance).Count(&mismatches).Error; err != nil {
		return err
	}
	if total == 0 || total != int64(batch.Count) || mismatches != 0 {
		return errors.New("evidence batch code quantities do not match its frozen quota")
	}
	return nil
}

func validateCreditApprovalRecord(tx *gorm.DB, p CreditProduct) error {
	if p.ApprovalID <= 0 || (p.State != "approved" && p.State != "published") || p.Published != (p.State == "published") || !p.DeliveryVerified {
		return errors.New("invalid product approval state")
	}
	var event CreditApprovalEvent
	if err := tx.First(&event, p.ApprovalID).Error; err != nil {
		return errors.New("approval unavailable")
	}
	digest, err := CreditProductDigest(p)
	if err != nil {
		return err
	}
	if event.Decision != "approve" || event.ProductID != p.ID || event.Revision != p.Revision || event.Digest != digest || event.Policy != CreditApprovalPolicy || !event.SoloOperator || event.ActorID <= 0 || event.EditedBy != p.EditedBy {
		return errors.New("approval does not match current revision")
	}
	var count int64
	if err := tx.Model(&CreditApprovalEvent{}).Where("reference_approval_id = ? AND decision = ?", event.ID, "revoke").Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return errors.New("approval revoked")
	}
	return nil
}

func validateCreditApproval(tx *gorm.DB, p CreditProduct) error {
	if err := validateCreditApprovalRecord(tx, p); err != nil {
		return err
	}
	return validateCreditDelivery(tx, p)
}

func GetCreditProductReview(id string) (*CreditProductReview, error) {
	c, err := GetCreditCatalog()
	if err != nil {
		return nil, err
	}
	for _, p := range c.Products {
		if p.ID != id {
			continue
		}
		digest, err := CreditProductDigest(p)
		if err != nil {
			return nil, err
		}
		review := &CreditProductReview{Product: p, Digest: digest, Confirmation: fmt.Sprintf("%s@%d:%s", p.ID, p.Revision, digest), Events: []CreditApprovalEvent{}}
		err = DB.Where("product_id = ?", id).Order("id").Find(&review.Events).Error
		return review, err
	}
	return nil, errors.New("product not found")
}

// ChangeCreditProductState locks the catalog and verifies both client versions.
func ChangeCreditProductState(id string, version, revision int64, action, confirmation string, actor int) error {
	if actor <= 0 {
		return errors.New("invalid operator")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		row, c, err := lockedCreditCatalog(tx)
		if err != nil {
			return err
		}
		if c.ApprovalSchema != 1 || c.Version != version {
			return errors.New("catalog changed; reload before saving")
		}
		for i := range c.Products {
			p := &c.Products[i]
			if p.ID != id {
				continue
			}
			if p.Revision != revision {
				return errors.New("product revision changed")
			}
			if action != "submit" {
				digest, err := CreditProductDigest(*p)
				if err != nil {
					return err
				}
				if confirmation != fmt.Sprintf("%s %s@%d:%s", strings.ToUpper(action), p.ID, p.Revision, digest) {
					return errors.New("explicit revision confirmation required")
				}
			}
			switch action {
			case "submit":
				if p.State != "draft" && p.State != "rejected" && p.State != "revoked" {
					return errors.New("cannot submit this state")
				}
				if err := validateCreditDelivery(tx, *p); err != nil {
					return err
				}
				var batch RedemptionBatch
				if err := tx.First(&batch, p.EvidenceBatchID).Error; err != nil {
					return err
				}
				if batch.ProductID == "" {
					if _, err := appendCreditEvent(tx, *p, actor, "legacy_delivery"); err != nil {
						return err
					}
				}
				p.State = "pending"
			case "approve", "reject":
				if p.State != "pending" {
					return errors.New("product is not pending review")
				}
				if err := validateCreditDelivery(tx, *p); err != nil {
					return err
				}
				if action == "reject" {
					p.State = "rejected"
				} else {
					p.State = "approved"
					p.DeliveryVerified = true
				}
			case "publish":
				if p.State != "approved" {
					return errors.New("product is not approved")
				}
				if err := validateCreditApproval(tx, *p); err != nil {
					return err
				}
				p.State, p.Published = "published", true
				p.LegacyWithdrawn = true
			case "unpublish":
				if p.State != "published" {
					return errors.New("product is not published")
				}
				p.State, p.Published = "approved", false
			case "revoke":
				if err := validateCreditApprovalRecord(tx, *p); err != nil {
					return err
				}
				p.State, p.Published, p.DeliveryVerified = "revoked", false, false
				p.LegacyWithdrawn = true
			default:
				return errors.New("unknown product operation")
			}
			event, err := appendCreditEvent(tx, *p, actor, action)
			if err != nil {
				return err
			}
			if action == "approve" {
				p.ApprovalID = event.ID
			}
			if action == "revoke" {
				p.ApprovalID = 0
			}
			return persistCreditCatalog(tx, row, c)
		}
		return errors.New("product not found")
	})
}

// MapLegacyCreditProduct is explicit and never infers identity or quota.
func MapLegacyCreditProduct(id string, version, revision int64, legacyID int, actor int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		row, c, err := lockedCreditCatalog(tx)
		if err != nil {
			return err
		}
		if actor <= 0 || c.Unified || c.ApprovalSchema != 1 || c.Version != version || legacyID <= 0 {
			return errors.New("invalid legacy mapping operation")
		}
		var legacy *StoreProduct
		for _, p := range c.LegacyProducts {
			if p.Id == legacyID {
				legacy = p
			}
		}
		if legacy == nil {
			return errors.New("not a frozen legacy product")
		}
		var current StoreProduct
		if err := tx.First(&current, legacyID).Error; err != nil || !reflect.DeepEqual(*legacy, current) {
			return errors.New("legacy product changed since preparation")
		}
		for _, p := range c.Products {
			if p.ID != id && p.LegacyStoreID == legacyID {
				return errors.New("legacy product already mapped")
			}
		}
		for i := range c.Products {
			p := &c.Products[i]
			if p.ID != id {
				continue
			}
			if p.Revision != revision || p.PriceCents != legacy.FaceValueCents || p.PurchaseURL != legacy.StoreUrl {
				return errors.New("explicit mapping fields or revision mismatch")
			}
			if err := validateCreditDelivery(tx, *p); err != nil {
				return err
			}
			p.LegacyStoreID, p.Revision, p.State, p.EditedBy = legacyID, p.Revision+1, "draft", actor
			p.ApprovalID, p.Published, p.DeliveryVerified = 0, false, false
			if _, err := appendCreditEvent(tx, *p, actor, "map"); err != nil {
				return err
			}
			return persistCreditCatalog(tx, row, c)
		}
		return errors.New("product not found")
	})
}

// EffectiveCreditProducts validates every publication claim before projection.
// Errors are not swallowed as an empty, apparently safe catalog.
func effectiveCreditProducts(tx *gorm.DB, c CreditCatalog) ([]CreditProduct, error) {
	result := []CreditProduct{}
	if c.ApprovalSchema != 1 {
		return nil, errors.New("credit approvals not prepared")
	}
	for _, p := range c.Products {
		if p.State == "approved" {
			if err := validateCreditApprovalRecord(tx, p); err != nil {
				return nil, err
			}
		}
		if p.Published || p.State == "published" {
			if err := validateCreditApproval(tx, p); err != nil {
				return nil, fmt.Errorf("invalid publication %s: %w", p.ID, err)
			}
			if operation_setting.StoreRechargeEnabled {
				p.BaseCredit = decimal.NewFromInt(int64(p.BaseQuota)).Div(decimal.NewFromFloat(common.QuotaPerUnit)).String()
				result = append(result, p)
			}
		}
	}
	return result, nil
}

func GetEffectiveCreditProducts() (CreditCatalog, error) {
	var result CreditCatalog
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		result, err = getEffectiveCreditProducts(tx)
		return err
	})
	return result, err
}

// getEffectiveCreditProducts shares the existing approval validation with
// diagnostics that already own a transaction. It performs no persistent writes.
func getEffectiveCreditProducts(tx *gorm.DB) (CreditCatalog, error) {
	_, c, err := lockedCreditCatalog(tx)
	if err != nil {
		return CreditCatalog{}, err
	}
	if err := c.Validate(); err != nil {
		return CreditCatalog{}, err
	}
	if err := validateLegacyCreditSnapshot(tx, c); err != nil {
		return CreditCatalog{}, err
	}
	products, err := effectiveCreditProducts(tx, c)
	if err != nil {
		return CreditCatalog{}, err
	}
	c.Products = products
	return c, nil
}

func validateLegacyCreditSnapshot(tx *gorm.DB, c CreditCatalog) error {
	var current []*StoreProduct
	if err := tx.Find(&current).Error; err != nil {
		return err
	}
	for _, row := range current {
		found := false
		for _, frozen := range c.LegacyProducts {
			if row.Id != frozen.Id {
				continue
			}
			if !reflect.DeepEqual(row, frozen) {
				return errors.New("legacy product changed outside approval workflow")
			}
			found = true
		}
		if !found {
			return errors.New("unknown legacy product outside frozen compatibility set")
		}
	}
	for _, frozen := range c.LegacyProducts {
		found := false
		for _, row := range current {
			if row.Id == frozen.Id {
				found = true
			}
		}
		if !found {
			return errors.New("frozen legacy product missing")
		}
	}
	return nil
}

// ActivateUnifiedCreditView is deliberately not registered as an HTTP route in
// Stage 2A. Stage 2C must authorize the controlled cutover separately.
func ActivateUnifiedCreditView(version int64, actor int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		row, c, err := lockedCreditCatalog(tx)
		if err != nil {
			return err
		}
		if actor <= 0 || c.Version != version || c.Unified || !operation_setting.StoreRechargeEnabled {
			return errors.New("invalid unified cutover")
		}
		if err := validateLegacyCreditSnapshot(tx, c); err != nil {
			return err
		}
		if _, err := effectiveCreditProducts(tx, c); err != nil {
			return err
		}
		for _, legacy := range c.LegacyProducts {
			var current StoreProduct
			if err := tx.First(&current, legacy.Id).Error; err != nil || !reflect.DeepEqual(*legacy, current) {
				return errors.New("legacy product changed since preparation")
			}
			if !legacy.Enabled {
				continue
			}
			found := false
			for _, p := range c.Products {
				if p.LegacyStoreID == legacy.Id && p.State == "published" && validateCreditApproval(tx, p) == nil {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("legacy product %d lacks verified published mapping", legacy.Id)
			}
		}
		for _, legacy := range c.LegacyCatalog {
			if !legacy.Published {
				continue
			}
			found := false
			for _, p := range c.Products {
				if p.ID == legacy.ID && p.State == "published" && validateCreditApproval(tx, p) == nil {
					found = true
				}
			}
			if !found {
				return errors.New("legacy catalog publication has not been reviewed")
			}
		}
		c.Unified = true
		if _, err := appendCreditEvent(tx, CreditProduct{ID: "catalog", State: "draft"}, actor, "activate"); err != nil {
			return err
		}
		return persistCreditCatalog(tx, row, c)
	})
}

func GetPublicCreditCatalog() (CreditCatalog, error) {
	effective, err := GetEffectiveCreditProducts()
	if err != nil {
		return effective, err
	}
	if !effective.Unified && operation_setting.StoreRechargeEnabled {
		for _, legacy := range effective.LegacyCatalog {
			if !legacy.Published {
				continue
			}
			replaced := false
			for _, p := range effective.ApprovalProducts {
				if p.ID == legacy.ID && p.LegacyWithdrawn {
					replaced = true
				}
			}
			for _, p := range effective.Products {
				if p.ID == legacy.ID {
					replaced = true
				}
			}
			if !replaced {
				effective.Products = append(effective.Products, legacy)
			}
		}
	}
	effective.LegacyProducts, effective.LegacyCatalog, effective.ApprovalProducts = nil, nil, nil
	effective.LegacyEvidenceBatchIDs = nil
	// Public projections cannot disclose internal approval actors or evidence.
	for i := range effective.Products {
		p := &effective.Products[i]
		p.EditedBy, p.EvidenceBatchID, p.ApprovalID, p.LegacyStoreID = 0, 0, 0, 0
		p.DeliveryEvidence, p.DeliveryBinding = "", ""
		p.PurchasePath = fmt.Sprintf("/api/clearapi/purchase/product/%s?revision=%d", p.ID, p.Revision)
	}
	return effective, nil
}
