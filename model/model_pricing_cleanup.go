package model

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"gorm.io/gorm"
)

// Model pricing cleanup is a one-shot, administrator-approved operation over
// the existing pricing options. It removes the site's stored model price
// records so the list is empty, and never touches model definitions, channels,
// groups, API keys, users, usage logs or bills. It is not part of startup
// AutoMigrate and never runs automatically when the application starts.
//
// Clearing a record removes stored keys only. The engine's built-in defaults
// are never written back: they are applied at read time by
// effectiveModelPricing, so persisting them would make "cleared" mean
// "restored to the client sales price" instead of "empty".

// ErrModelPricingStale is returned when the reviewed cleanup plan no longer
// matches the stored pricing state, so a new plan must be prepared and reviewed.
var ErrModelPricingStale = errors.New("model pricing changed since this cleanup plan was prepared")

// modelPricingSalesKeys are the options that store this site's model sales
// prices. They are removed by cleanup. billing_setting.billing_mode is included
// because a stored mode is what keeps a model on an expression or a ratio once
// its prices are gone.
var modelPricingSalesKeys = []string{
	"ModelPrice", "ModelRatio", "CompletionRatio", "CacheRatio",
	"CreateCacheRatio", "ImageRatio", "AudioRatio", "AudioCompletionRatio",
	"billing_setting.billing_mode", "billing_setting.billing_expr",
	billing_setting.PluginBillingExprOption,
}

// storedPricingMaps reads the pricing options exactly as persisted, without
// merging engine defaults, so cleanup can distinguish "no stored record" from
// "stored value happens to equal the default".
func storedPricingMaps(db *gorm.DB) (map[string]map[string]any, error) {
	var rows []Option
	if err := db.Where(commonKeyCol+" IN ?", modelPricingOptionKeys).Find(&rows).Error; err != nil {
		return nil, err
	}
	values := make(map[string]map[string]any, len(modelPricingOptionKeys))
	for _, key := range modelPricingOptionKeys {
		values[key] = make(map[string]any)
	}
	for _, row := range rows {
		var entries map[string]any
		if err := common.UnmarshalJsonStr(row.Value, &entries); err != nil {
			return nil, fmt.Errorf("%s: %w", row.Key, err)
		}
		if entries == nil {
			return nil, fmt.Errorf("%s must be a JSON object", row.Key)
		}
		values[row.Key] = entries
	}
	return values, nil
}

// ModelPricingCleanupAction describes the only two outcomes cleanup produces:
// the stored record is emptied, or there was nothing stored to empty.
type ModelPricingCleanupAction string

const (
	ModelPricingCleanupKeep  ModelPricingCleanupAction = "keep"
	ModelPricingCleanupClear ModelPricingCleanupAction = "clear"
)

type ModelPricingRecord struct {
	ModelName string `json:"model_name"`
	// BillingMode is the current effective mode for display only; it is not a
	// value cleanup preserves.
	BillingMode string                    `json:"billing_mode"`
	Action      ModelPricingCleanupAction `json:"action"`
	// Version is the stored pricing version the administrator reviews; cleanup
	// is rejected if the record changed after this plan was prepared.
	Version string `json:"version"`
	// Configured lists the stored sales-price options that cleanup removes.
	Configured []string `json:"configured"`
	// BuiltInExpr reports that the engine has a built-in upstream expression
	// for this model. Cleanup never persists it; it only documents which price
	// the engine falls back to at read time.
	BuiltInExpr string `json:"built_in_expr,omitempty"`
	BuiltIn     bool   `json:"built_in"`
	// ClearCount is the number of stored entries cleanup removes for this model.
	ClearCount int `json:"clear_count"`
}

type ModelPricingCleanupPlan struct {
	ModelPricingRecord
	// Pricing is the replacement draft handed to the existing versioned pricing
	// transaction. Cleanup always submits an empty draft: every stored sales
	// price is removed and nothing is restored.
	Pricing PricingValues `json:"pricing"`
}

type ModelPricingCleanup struct {
	Snapshot string                    `json:"snapshot"`
	Records  []ModelPricingRecord      `json:"records"`
	Plans    []ModelPricingCleanupPlan `json:"plans"`
	Cleared  []string                  `json:"cleared,omitempty"`
}

type ModelPricingCleanupChange struct {
	ModelName       string `json:"model_name"`
	ExpectedVersion string `json:"expected_version"`
}

// ModelPricingCleanupReport describes every enabled model's stored pricing and
// what a cleanup removes. It has no write side effects.
func ModelPricingCleanupReport() (*ModelPricingCleanup, error) {
	stored, err := storedPricingMaps(DB)
	if err != nil {
		return nil, err
	}
	return buildModelPricingCleanup(DB, stored)
}

func buildModelPricingCleanup(db *gorm.DB, stored map[string]map[string]any) (*ModelPricingCleanup, error) {
	// The administrator sees the pricing list built from the stored option
	// maps, so cleanup must cover those names. Enabled abilities are included
	// too, so a routed model with no stored price is visible as already clear.
	var enabled []string
	if err := db.Model(&Ability{}).Where("enabled = ?", true).Distinct().Pluck("model", &enabled).Error; err != nil {
		return nil, err
	}
	nameSet := make(map[string]bool, len(enabled))
	for _, name := range enabled {
		nameSet[name] = true
	}
	for _, key := range modelPricingSalesKeys {
		if key == billing_setting.PluginBillingExprOption {
			continue
		}
		for name := range stored[key] {
			nameSet[name] = true
		}
	}
	for variant := range stored[billing_setting.PluginBillingExprOption] {
		if _, name, ok := billing_setting.SplitPluginBillingExprKey(variant); ok {
			nameSet[name] = true
		}
	}
	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	slices.Sort(names)
	report := &ModelPricingCleanup{
		Snapshot: pricingMapsVersion(stored),
		Records:  make([]ModelPricingRecord, 0, len(names)),
		Plans:    make([]ModelPricingCleanupPlan, 0, len(names)),
	}
	for _, name := range names {
		plan := planModelPricingCleanup(stored, name)
		report.Records = append(report.Records, plan.ModelPricingRecord)
		report.Plans = append(report.Plans, plan)
	}
	return report, nil
}

func planModelPricingCleanup(stored map[string]map[string]any, name string) ModelPricingCleanupPlan {
	// Only keys actually present in storage count: an absent key is already
	// cleared even though the engine would resolve a default for it.
	configured := modelPricingValues(stored, name)
	record := ModelPricingRecord{
		ModelName:   name,
		BillingMode: billing_setting.BillingModeRatio,
		Version:     ModelPricingVersion(configured),
		Configured:  make([]string, 0, len(configured)),
	}
	for _, key := range modelPricingSalesKeys {
		value, exists := configured[key]
		if !exists {
			continue
		}
		// A plugin map keeps only this model's variants; an empty map is no
		// stored record for this model.
		if key == billing_setting.PluginBillingExprOption {
			variants, _ := value.(map[string]any)
			if len(variants) == 0 {
				continue
			}
		}
		record.Configured = append(record.Configured, key)
	}
	if mode, ok := configured["billing_setting.billing_mode"].(string); ok && mode != "" {
		record.BillingMode = mode
	}
	record.BuiltInExpr, record.BuiltIn = billing_setting.GetBuiltinBillingExpr(name)
	record.ClearCount = len(record.Configured)
	record.Action = ModelPricingCleanupKeep
	if record.ClearCount > 0 {
		record.Action = ModelPricingCleanupClear
	}
	return ModelPricingCleanupPlan{ModelPricingRecord: record, Pricing: PricingValues{}}
}

func pricingMapsVersion(values map[string]map[string]any) string {
	encoded, err := common.Marshal(values)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}

// ApplyModelPricingCleanup removes the selected models' stored pricing through
// the same locked, versioned transaction used by the pricing editor. Every
// model must still match the version the administrator reviewed, and the plan
// must match the snapshot it was prepared from. The submitted draft is always
// empty: nothing is restored to a default sales price.
func ApplyModelPricingCleanup(snapshot string, changes []ModelPricingCleanupChange) (*ModelPricingCleanup, error) {
	if snapshot == "" {
		return nil, fmt.Errorf("%w: a prepared cleanup plan is required", ErrModelPricingStale)
	}
	if len(changes) == 0 {
		return nil, errors.New("select model pricing records before cleaning up")
	}
	seen := make(map[string]bool)
	for _, change := range changes {
		if seen[change.ModelName] {
			return nil, errors.New("duplicate model pricing cleanup")
		}
		seen[change.ModelName] = true
		if change.ExpectedVersion == "" {
			return nil, ErrModelPricingConflict
		}
	}
	var plan *ModelPricingCleanup
	err := mutateModelPricingOptions(func(tx *gorm.DB, values map[string]map[string]any) error {
		// The transaction hands back the stored maps merged with engine
		// defaults. Re-read the stored rows so plan review and clearing use the
		// same basis; otherwise a default would look like a stored record.
		stored, err := storedPricingMaps(tx)
		if err != nil {
			return err
		}
		current, err := buildModelPricingCleanup(tx, stored)
		if err != nil {
			return err
		}
		if current.Snapshot != snapshot {
			return ErrModelPricingStale
		}
		plans := make(map[string]ModelPricingCleanupPlan, len(current.Plans))
		for _, entry := range current.Plans {
			plans[entry.ModelName] = entry
		}
		cleared := make([]string, 0, len(changes))
		for _, change := range changes {
			if _, exists := plans[change.ModelName]; !exists {
				return fmt.Errorf("%w: %s is not an enabled model", ErrModelPricingStale, change.ModelName)
			}
			previous := modelPricingValues(stored, change.ModelName)
			if ModelPricingVersion(previous) != change.ExpectedVersion {
				return fmt.Errorf("%w: %s", ErrModelPricingConflict, change.ModelName)
			}
			entry := plans[change.ModelName]
			if entry.ClearCount == 0 {
				return fmt.Errorf("%w: %s has no stored model pricing to clean up", ErrModelPricingStale, change.ModelName)
			}
			if err := validateModelPricing(change.ModelName, entry.Pricing, previous); err != nil {
				return err
			}
			replaceModelPricing(stored, change.ModelName, entry.Pricing)
			cleared = append(cleared, change.ModelName)
		}
		// Persist exactly the surviving stored rows. Writing the merged maps
		// back would re-materialize every engine default as a stored record,
		// which would refill the pricing list instead of emptying the cleared
		// models.
		for key := range values {
			values[key] = stored[key]
		}
		plan = current
		plan.Cleared = cleared
		return nil
	})
	if err != nil {
		return nil, err
	}
	return plan, nil
}
