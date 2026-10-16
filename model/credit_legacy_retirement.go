package model

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const LegacyCreditRetirementPolicy = "legacy-retirement-v1"

// LegacyCreditRetirementPlan binds an explicit withdrawal of the complete frozen
// compatibility set. MissingStoreIDs acknowledges absence, never a deletion or a
// reconstruction of a sale. EvidenceRef identifies separately reviewed evidence.
type LegacyCreditRetirementPlan struct {
	CatalogVersion  int64  `json:"catalog_version"`
	CatalogDigest   string `json:"catalog_digest"`
	SnapshotDigest  string `json:"snapshot_digest"`
	SourceDigest    string `json:"source_digest"`
	MissingStoreIDs []int  `json:"missing_store_ids"`
	PrepareEventID  int    `json:"prepare_event_id"`
	PrepareDigest   string `json:"prepare_digest"`
	ActorID         int    `json:"actor_id"`
	EvidenceRef     string `json:"evidence_ref"`
}

type LegacyCreditRetirement struct {
	Plan    LegacyCreditRetirementPlan `json:"plan"`
	EventID int                        `json:"event_id"`
}

func (p LegacyCreditRetirementPlan) Digest() (string, error) {
	raw, err := common.Marshal(p)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}

func (p LegacyCreditRetirementPlan) Confirmation() (string, error) {
	digest, err := p.Digest()
	return fmt.Sprintf("RETIRE_LEGACY v%d:%s", p.CatalogVersion, digest), err
}

func legacyCreditSnapshotDigest(c CreditCatalog) (string, error) {
	raw, err := common.Marshal(struct {
		Products []*StoreProduct `json:"legacy_products"`
		Catalog  []CreditProduct `json:"legacy_catalog"`
		Batches  []int           `json:"legacy_evidence_batch_ids"`
	}{c.LegacyProducts, c.LegacyCatalog, c.LegacyEvidenceBatchIDs})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}

// legacyCreditRetirementPlan is shared by read-only review and the locked apply
// transaction; neither path infers a business mapping from price or product ID.
func legacyCreditRetirementPlan(tx *gorm.DB, row Option, c CreditCatalog, actor int, evidence string) (LegacyCreditRetirementPlan, error) {
	var plan LegacyCreditRetirementPlan
	if actor <= 0 || strings.TrimSpace(evidence) != evidence || len(evidence) < 1 || len(evidence) > 256 || strings.ContainsAny(evidence, "\r\n\x00") {
		return plan, errors.New("operator and a non-secret review evidence reference are required")
	}
	if c.ApprovalSchema != 1 || c.Unified || c.LegacyRetirement != nil || (len(c.LegacyProducts) == 0 && len(c.LegacyCatalog) == 0) {
		return plan, errors.New("catalog is not an unretired frozen compatibility set")
	}
	if err := c.Validate(); err != nil {
		return plan, err
	}
	var operator User
	if err := tx.Select("id", "role", "status").First(&operator, actor).Error; err != nil || operator.Role != common.RoleRootUser || operator.Status != common.UserStatusEnabled {
		return plan, errors.New("an active root operator is required")
	}
	for key, expected := range map[string]string{"StoreRechargeEnabled": "false", "DisableOnlinePayments": "true"} {
		var option Option
		if err := tx.Where(&Option{Key: key}).First(&option).Error; err != nil || option.Value != expected {
			return plan, errors.New("store and online payments must be explicitly disabled")
		}
	}
	var events []CreditApprovalEvent
	if err := tx.Where("decision IN ?", []string{"prepare", "retire_legacy"}).Order("id").Find(&events).Error; err != nil {
		return plan, err
	}
	if len(events) != 1 || events[0].Decision != "prepare" || events[0].Policy != CreditApprovalPolicy || events[0].CreatedTime <= 0 {
		return plan, errors.New("unique original preparation history is required")
	}
	var current []*StoreProduct
	if err := tx.Order("id").Find(&current).Error; err != nil {
		return plan, err
	}
	missing, err := legacyCreditMissingSources(c, current)
	if err != nil {
		return plan, err
	}
	// Publication validation remains mandatory even while the store is disabled.
	if _, err := effectiveCreditProducts(tx, c); err != nil {
		return plan, err
	}
	snapshot, err := legacyCreditSnapshotDigest(c)
	if err != nil {
		return plan, err
	}
	sources, err := common.Marshal(current)
	if err != nil {
		return plan, err
	}
	preparation, err := common.Marshal(events[0])
	if err != nil {
		return plan, err
	}
	return LegacyCreditRetirementPlan{CatalogVersion: c.Version, CatalogDigest: fmt.Sprintf("%x", sha256.Sum256([]byte(row.Value))), SnapshotDigest: snapshot,
		SourceDigest: fmt.Sprintf("%x", sha256.Sum256(sources)), MissingStoreIDs: missing, PrepareEventID: events[0].ID,
		PrepareDigest: fmt.Sprintf("%x", sha256.Sum256(preparation)), ActorID: actor, EvidenceRef: evidence}, nil
}

// ReviewLegacyCreditRetirement performs no migration, repair or audit write.
func ReviewLegacyCreditRetirement(ctx context.Context, db *gorm.DB, actor int, evidence string) (LegacyCreditRetirementPlan, error) {
	var plan LegacyCreditRetirementPlan
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row Option
		if err := tx.Where(&Option{Key: CreditCatalogOptionKey}).First(&row).Error; err != nil {
			return err
		}
		var c CreditCatalog
		if err := common.UnmarshalJsonStr(row.Value, &c); err != nil {
			return err
		}
		c.Products = c.ApprovalProducts
		var err error
		plan, err = legacyCreditRetirementPlan(tx, row, c, actor, evidence)
		return err
	})
	return plan, err
}

// RetireLegacyCreditCatalog is an offline operator entry point, never called by
// startup or an HTTP route. The caller must obtain separate production approval.
// Only the catalog option and one compensating event are written atomically.
func RetireLegacyCreditCatalog(ctx context.Context, db *gorm.DB, plan LegacyCreditRetirementPlan, confirmation string) error {
	expected, err := plan.Confirmation()
	if err != nil || confirmation != expected {
		return errors.New("explicit retirement plan confirmation required")
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, c, err := lockedCreditCatalog(tx)
		if err != nil {
			return err
		}
		if c.LegacyRetirement != nil {
			if !reflect.DeepEqual(c.LegacyRetirement.Plan, plan) {
				return errors.New("a different legacy retirement already exists")
			}
			if err := c.Validate(); err != nil {
				return err
			}
			if err := validateLegacyCreditSnapshot(tx, c); err != nil {
				return err
			}
			_, err = effectiveCreditProducts(tx, c)
			return err
		}
		// Hold the operating switches and operator identity stable until commit.
		for _, key := range []string{"DisableOnlinePayments", "StoreRechargeEnabled"} {
			var option Option
			if err := lockForUpdate(tx).Where(&Option{Key: key}).First(&option).Error; err != nil {
				return err
			}
		}
		var operator User
		if err := lockForUpdate(tx).Select("id", "role", "status").First(&operator, plan.ActorID).Error; err != nil {
			return err
		}
		fresh, err := legacyCreditRetirementPlan(tx, row, c, plan.ActorID, plan.EvidenceRef)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(fresh, plan) {
			return errors.New("retirement plan changed; review again")
		}
		digest, err := plan.Digest()
		if err != nil {
			return err
		}
		event := CreditApprovalEvent{ProductID: "catalog", Revision: plan.CatalogVersion, Digest: digest, Policy: LegacyCreditRetirementPolicy,
			ActorID: plan.ActorID, Decision: "retire_legacy", CreatedTime: common.GetTimestamp(), DeliveryEvidence: plan.EvidenceRef,
			SoloOperator: true, ReferenceApprovalID: plan.PrepareEventID}
		if err := tx.Create(&event).Error; err != nil {
			return err
		}
		c.LegacyRetirement = &LegacyCreditRetirement{Plan: plan, EventID: event.ID}
		if err := persistCreditCatalog(tx, row, c); err != nil {
			return err
		}
		c.Version++
		return validateLegacyCreditSnapshot(tx, c)
	})
}

// legacyCreditMissingSources rejects changed/unknown rows. Only exact absence
// can be acknowledged by retirement; present historical rows stay byte-for-byte
// equivalent to their frozen model values.
func legacyCreditMissingSources(c CreditCatalog, current []*StoreProduct) ([]int, error) {
	frozen := make(map[int]*StoreProduct, len(c.LegacyProducts))
	for _, p := range c.LegacyProducts {
		if p == nil || p.Id <= 0 || frozen[p.Id] != nil {
			return nil, errors.New("invalid frozen legacy identities")
		}
		frozen[p.Id] = p
	}
	for _, p := range current {
		if original := frozen[p.Id]; original == nil || !reflect.DeepEqual(original, p) {
			return nil, errors.New("unknown or changed legacy product outside frozen compatibility set")
		}
		delete(frozen, p.Id)
	}
	missing := []int{}
	for id := range frozen {
		missing = append(missing, id)
	}
	slices.Sort(missing)
	return missing, nil
}

func validateLegacyCreditRetirement(tx *gorm.DB, c CreditCatalog, current []*StoreProduct, missing []int) error {
	var events []CreditApprovalEvent
	if err := tx.Where("decision = ?", "retire_legacy").Find(&events).Error; err != nil {
		return err
	}
	if c.LegacyRetirement == nil {
		if len(events) != 0 || len(missing) != 0 {
			return errors.New("frozen legacy product missing or retirement record removed")
		}
		return nil
	}
	r := c.LegacyRetirement
	snapshot, err := legacyCreditSnapshotDigest(c)
	if err != nil {
		return err
	}
	sources, err := common.Marshal(current)
	if err != nil {
		return err
	}
	digest, err := r.Plan.Digest()
	if err != nil {
		return err
	}
	if c.ApprovalSchema != 1 || c.Unified || len(events) != 1 || events[0].ID != r.EventID || r.Plan.CatalogVersion <= 0 || c.Version <= r.Plan.CatalogVersion ||
		r.Plan.SnapshotDigest != snapshot || r.Plan.SourceDigest != fmt.Sprintf("%x", sha256.Sum256(sources)) || !slices.Equal(r.Plan.MissingStoreIDs, missing) {
		return errors.New("legacy retirement does not match frozen history and current sources")
	}
	e := events[0]
	if e.Policy != LegacyCreditRetirementPolicy || e.ProductID != "catalog" || e.Revision != r.Plan.CatalogVersion || e.Digest != digest ||
		e.ActorID <= 0 || e.ActorID != r.Plan.ActorID || e.DeliveryEvidence != r.Plan.EvidenceRef || e.CreatedTime <= 0 || !e.SoloOperator || e.ReferenceApprovalID != r.Plan.PrepareEventID {
		return errors.New("legacy retirement audit record is invalid")
	}
	var preparations []CreditApprovalEvent
	if err := tx.Where("decision = ?", "prepare").Find(&preparations).Error; err != nil {
		return err
	}
	if len(preparations) != 1 || preparations[0].ID != r.Plan.PrepareEventID || preparations[0].Policy != CreditApprovalPolicy || preparations[0].CreatedTime <= 0 || preparations[0].CreatedTime > e.CreatedTime {
		return errors.New("legacy retirement preparation history is invalid")
	}
	preparation, err := common.Marshal(preparations[0])
	if err != nil {
		return err
	}
	if r.Plan.PrepareDigest != fmt.Sprintf("%x", sha256.Sum256(preparation)) {
		return errors.New("legacy retirement preparation history changed")
	}
	return nil
}
