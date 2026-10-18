package model

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"gorm.io/gorm"
)

// Pricing cleanup is a one-shot, administrator-approved operation over the
// existing pricing options. It never touches model definitions, channels,
// groups, API keys, users, usage logs or bills. It is not part of startup
// AutoMigrate and never runs automatically when the application starts.

// ErrModelPricingStale is returned when the reviewed cleanup plan no longer
// matches the stored pricing state, so a new plan must be prepared and reviewed.
var ErrModelPricingStale = errors.New("model pricing changed since this cleanup plan was prepared")

type ModelPricingCleanupAction string

const (
	// ModelPricingCleanupKeep leaves an already clean record untouched.
	ModelPricingCleanupKeep ModelPricingCleanupAction = "keep"
	// ModelPricingCleanupReset removes this site's own pricing so the built-in
	// or engine default pricing applies again.
	ModelPricingCleanupReset ModelPricingCleanupAction = "reset_to_defaults"
	// ModelPricingCleanupBuiltIn replaces this site's own pricing with the
	// built-in upstream cost expression, keeping the model priced while its
	// sales price is reviewed.
	ModelPricingCleanupBuiltIn ModelPricingCleanupAction = "use_builtin"
)

type ModelPricingRecord struct {
	ModelName string `json:"model_name"`
	// BillingMode is the effective mode, so the administrator can see whether
	// the record currently charges a ratio, a per-request price or an expression.
	BillingMode string `json:"billing_mode"`
	// Version is the stored pricing version the administrator reviews; cleanup
	// is rejected if the record changed after this plan was prepared.
	Version        string                    `json:"version"`
	Configured     []string                  `json:"configured"`
	ConfiguredExpr string                    `json:"configured_expr,omitempty"`
	BuiltInExpr    string                    `json:"built_in_expr,omitempty"`
	BuiltIn        bool                      `json:"built_in"`
	Action         ModelPricingCleanupAction `json:"action"`
	// UnpricedAfterReset warns that the reset model has no built-in and no
	// default price, so it becomes unpriced until the administrator sets a
	// sales price. It is a warning, never a blocker.
	UnpricedAfterReset bool `json:"unpriced_after_reset,omitempty"`
}

type ModelPricingCleanupPlan struct {
	ModelPricingRecord
	// Pricing is the complete replacement draft for the existing versioned
	// pricing transaction. An empty draft means "no override".
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

// ModelPricingCleanupReport describes every enabled model's current pricing and
// what a cleanup would do to it. It has no write side effects.
func ModelPricingCleanupReport() (*ModelPricingCleanup, error) {
	values, _, _, err := readModelPricingMaps(DB)
	if err != nil {
		return nil, err
	}
	return buildModelPricingCleanup(DB, values)
}

func buildModelPricingCleanup(db *gorm.DB, values map[string]map[string]any) (*ModelPricingCleanup, error) {
	var names []string
	if err := db.Model(&Ability{}).Where("enabled = ?", true).Distinct().Pluck("model", &names).Error; err != nil {
		return nil, err
	}
	slices.Sort(names)
	report := &ModelPricingCleanup{
		Snapshot: pricingMapsVersion(values),
		Records:  make([]ModelPricingRecord, 0, len(names)),
		Plans:    make([]ModelPricingCleanupPlan, 0, len(names)),
	}
	for _, name := range names {
		plan := planModelPricingCleanup(values, name)
		report.Records = append(report.Records, plan.ModelPricingRecord)
		report.Plans = append(report.Plans, plan)
	}
	return report, nil
}

func planModelPricingCleanup(values map[string]map[string]any, name string) ModelPricingCleanupPlan {
	configured := modelPricingValues(values, name)
	defaults := modelPricingValues(defaultPricingMaps(), name)
	record := ModelPricingRecord{
		ModelName:   name,
		BillingMode: billing_setting.BillingModeRatio,
		Version:     ModelPricingVersion(configured),
		Configured:  modelPricingOverrides(configured, defaults),
	}
	if mode, ok := configured["billing_setting.billing_mode"].(string); ok && mode != "" {
		record.BillingMode = mode
	}
	record.ConfiguredExpr, _ = configured["billing_setting.billing_expr"].(string)
	record.BuiltInExpr, record.BuiltIn = billing_setting.GetBuiltinBillingExpr(name)
	record.Action, record.UnpricedAfterReset = modelPricingCleanupAction(values, record)
	plan := ModelPricingCleanupPlan{ModelPricingRecord: record, Pricing: defaults}
	if record.Action == ModelPricingCleanupBuiltIn {
		plan.Pricing = PricingValues{
			"billing_setting.billing_mode": billing_setting.BillingModeTieredExpr,
			"billing_setting.billing_expr": record.BuiltInExpr,
		}
	}
	return plan
}

// modelPricingOverrides lists the option keys whose stored value differs from
// the engine default. The pricing maps are read as defaults merged with stored
// rows, so an equal value means no administrator override is stored.
func modelPricingOverrides(configured, defaults PricingValues) []string {
	keys := make([]string, 0, len(configured))
	for _, key := range modelPricingOptionKeys {
		stored, exists := configured[key]
		fallback, hasFallback := defaults[key]
		if exists && (!hasFallback || stored != fallback) {
			keys = append(keys, key)
		}
	}
	return keys
}

func modelPricingCleanupAction(values map[string]map[string]any, record ModelPricingRecord) (ModelPricingCleanupAction, bool) {
	if !needsModelPricingCleanup(record) {
		return ModelPricingCleanupKeep, false
	}
	if record.BuiltIn {
		return ModelPricingCleanupBuiltIn, false
	}
	// Simulate the reset on a copy so the warning describes what the
	// administrator will actually get, not the current override.
	simulated := make(map[string]map[string]any, len(values))
	for key, entries := range values {
		simulated[key] = maps.Clone(entries)
	}
	replaceModelPricing(simulated, record.ModelName, modelPricingValues(defaultPricingMaps(), record.ModelName))
	return ModelPricingCleanupReset, effectiveModelPricingIsUnpriced(simulated, record.ModelName)
}

// needsModelPricingCleanup reports whether a record still carries this site's
// own pricing instead of inheriting built-in or engine default pricing.
func needsModelPricingCleanup(record ModelPricingRecord) bool {
	if len(record.Configured) > 0 || strings.TrimSpace(record.ConfiguredExpr) != "" {
		return true
	}
	// A model with a built-in upstream price but a legacy ratio/per-request
	// mode has not applied the upstream cost price yet.
	return record.BuiltIn && record.BillingMode != billing_setting.BillingModeTieredExpr
}

// effectiveModelPricingIsUnpriced reports whether a model would settle with no
// price at all: no fixed request price, no ratio and no expression.
func effectiveModelPricingIsUnpriced(values map[string]map[string]any, name string) bool {
	effective := effectiveModelPricing(values, name)
	if effective["billing_setting.billing_mode"] == billing_setting.BillingModeTieredExpr {
		expression, _ := effective["billing_setting.billing_expr"].(string)
		return strings.TrimSpace(expression) == ""
	}
	if _, priced := effective["ModelPrice"]; priced {
		return false
	}
	_, priced := effective["ModelRatio"]
	return !priced
}

func pricingMapsVersion(values map[string]map[string]any) string {
	encoded, err := common.Marshal(values)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}

// ApplyModelPricingCleanup removes the selected models' pricing records through
// the same locked, versioned transaction used by the pricing editor. Every
// model must still match the version the administrator reviewed, and the plan
// must match the snapshot it was prepared from.
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
		current, err := buildModelPricingCleanup(tx, values)
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
			previous := modelPricingValues(values, change.ModelName)
			if ModelPricingVersion(previous) != change.ExpectedVersion {
				return fmt.Errorf("%w: %s", ErrModelPricingConflict, change.ModelName)
			}
			entry, exists := plans[change.ModelName]
			if !exists {
				return fmt.Errorf("%w: %s is not an enabled model", ErrModelPricingStale, change.ModelName)
			}
			if entry.Action == ModelPricingCleanupKeep {
				return fmt.Errorf("%w: %s has no model pricing to clean up", ErrModelPricingStale, change.ModelName)
			}
			if err := validateModelPricing(change.ModelName, entry.Pricing, previous); err != nil {
				return err
			}
			replaceModelPricing(values, change.ModelName, entry.Pricing)
			cleared = append(cleared, change.ModelName)
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
