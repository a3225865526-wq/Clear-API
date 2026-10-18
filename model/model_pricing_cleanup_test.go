package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetModelPricingCleanupTestTables isolates pricing options and abilities so
// cleanup never observes state left behind by another test.
func resetModelPricingCleanupTestTables(t *testing.T) {
	t.Helper()
	resetPricingEndpointTestTables(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	require.NoError(t, DB.Exec("DELETE FROM options").Error)
	initCol()
	common.OptionMapRWMutex.Lock()
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	ratio_setting.InitRatioSettings()
	InvalidatePricingCache()
}

func seedModelPricingOverrides(t *testing.T, overrides map[string]string) {
	t.Helper()
	require.NoError(t, UpdateModelPricingOptions(overrides))
}

func cleanupRecordByName(t *testing.T, report *ModelPricingCleanup, name string) ModelPricingRecord {
	t.Helper()
	for _, record := range report.Records {
		if record.ModelName == name {
			return record
		}
	}
	require.Failf(t, "missing cleanup record", "model %s is not reported", name)
	return ModelPricingRecord{}
}

func cleanupPlanByName(t *testing.T, report *ModelPricingCleanup, name string) ModelPricingCleanupPlan {
	t.Helper()
	for _, plan := range report.Plans {
		if plan.ModelName == name {
			return plan
		}
	}
	require.Failf(t, "missing cleanup plan", "model %s is not planned", name)
	return ModelPricingCleanupPlan{}
}

// The cleanup list must describe where each price record lives, distinguish an
// inherited default from this site's own override, and plan a reset that keeps
// models priced when a built-in upstream price exists.
func TestModelPricingCleanupReportPlansResetWithoutTouchingOtherRecords(t *testing.T) {
	resetModelPricingCleanupTestTables(t)
	insertPricingEndpointChannel(t, 501, 1, pricingEndpointAdvancedCustomConfig())
	for _, name := range []string{"gpt-4", "legacy-request-model", "clean-model"} {
		insertPricingEndpointAbility(t, 501, name)
	}
	builtInName := "gpt-6-astra"
	insertPricingEndpointAbility(t, 501, builtInName)
	builtInExpr, hasBuiltIn := billing_setting.GetBuiltinBillingExpr(builtInName)
	require.True(t, hasBuiltIn, "the test needs a model with a built-in upstream price")

	seedModelPricingOverrides(t, map[string]string{
		"ModelRatio":                   `{"gpt-4":0.5,"gpt-6-astra":0.75}`,
		"ModelPrice":                   `{"legacy-request-model":0.02}`,
		"CompletionRatio":              `{"gpt-4":4}`,
		"billing_setting.billing_mode": `{"gpt-4":"ratio"}`,
	})

	report, err := ModelPricingCleanupReport()
	require.NoError(t, err)

	ratioRecord := cleanupRecordByName(t, report, "gpt-4")
	assert.Equal(t, ModelPricingCleanupReset, ratioRecord.Action)
	assert.Equal(t, billing_setting.BillingModeRatio, ratioRecord.BillingMode)
	assert.ElementsMatch(t, []string{"ModelRatio", "CompletionRatio", "billing_setting.billing_mode"}, ratioRecord.Configured)
	assert.False(t, ratioRecord.UnpricedAfterReset, "a built-in default ratio keeps the model priced")
	assert.Equal(t, 15.0, cleanupPlanByName(t, report, "gpt-4").Pricing["ModelRatio"],
		"reset must restore the built-in ratio instead of dropping the price")

	requestRecord := cleanupRecordByName(t, report, "legacy-request-model")
	assert.Equal(t, ModelPricingCleanupReset, requestRecord.Action)
	assert.Contains(t, requestRecord.Configured, "ModelPrice")

	// A model with a built-in upstream price is realigned to that cost price
	// instead of being left with a stale ratio override.
	builtInRecord := cleanupRecordByName(t, report, builtInName)
	assert.Equal(t, ModelPricingCleanupBuiltIn, builtInRecord.Action)
	assert.Equal(t, builtInExpr, builtInRecord.BuiltInExpr)
	assert.Equal(t, PricingValues{
		"billing_setting.billing_mode": billing_setting.BillingModeTieredExpr,
		"billing_setting.billing_expr": builtInExpr,
	}, cleanupPlanByName(t, report, builtInName).Pricing)

	assert.Equal(t, ModelPricingCleanupKeep, cleanupRecordByName(t, report, "clean-model").Action)
}

func TestModelPricingCleanupFlagsUnpricedModelWithoutBlocking(t *testing.T) {
	resetModelPricingCleanupTestTables(t)
	insertPricingEndpointChannel(t, 502, 1, pricingEndpointAdvancedCustomConfig())
	insertPricingEndpointAbility(t, 502, "no-default-model")
	seedModelPricingOverrides(t, map[string]string{
		"ModelRatio": `{"no-default-model":0.25}`,
	})

	report, err := ModelPricingCleanupReport()
	require.NoError(t, err)

	record := cleanupRecordByName(t, report, "no-default-model")
	assert.Equal(t, ModelPricingCleanupReset, record.Action)
	assert.True(t, record.UnpricedAfterReset, "resetting an unpriced model is a warning, not a blocker")
}

// Cleanup must remove only the selected pricing records and must preserve model
// definitions, channels, groups, keys, users and historical usage.
func TestApplyModelPricingCleanupClearsOnlySelectedPricing(t *testing.T) {
	resetModelPricingCleanupTestTables(t)
	insertPricingEndpointChannel(t, 503, 1, pricingEndpointAdvancedCustomConfig())
	for _, name := range []string{"cleanup-me", "keep-me"} {
		insertPricingEndpointAbility(t, 503, name)
	}
	seedModelPricingOverrides(t, map[string]string{
		"ModelRatio":      `{"cleanup-me":0.4,"keep-me":0.6}`,
		"CompletionRatio": `{"cleanup-me":3}`,
	})

	before, err := GetModelPricingSnapshot([]string{"cleanup-me", "keep-me"})
	require.NoError(t, err)
	beforeEntry := make(map[string]ModelPricingEntry, len(before.Entries))
	for _, entry := range before.Entries {
		beforeEntry[entry.ModelName] = entry
	}
	require.Contains(t, beforeEntry["cleanup-me"].Configured, "ModelRatio")

	report, err := ModelPricingCleanupReport()
	require.NoError(t, err)
	require.NotEmpty(t, report.Snapshot)

	applied, err := ApplyModelPricingCleanup(report.Snapshot, []ModelPricingCleanupChange{{
		ModelName:       "cleanup-me",
		ExpectedVersion: beforeEntry["cleanup-me"].Version,
	}})
	require.NoError(t, err)
	assert.Equal(t, []string{"cleanup-me"}, applied.Cleared)

	after, err := GetModelPricingSnapshot([]string{"cleanup-me", "keep-me"})
	require.NoError(t, err)
	afterEntry := make(map[string]ModelPricingEntry, len(after.Entries))
	for _, entry := range after.Entries {
		afterEntry[entry.ModelName] = entry
	}
	assert.Empty(t, afterEntry["cleanup-me"].Configured, "the selected model must lose its own pricing")
	assert.Equal(t, beforeEntry["keep-me"].Version, afterEntry["keep-me"].Version, "unselected pricing must be untouched")
	assert.Equal(t, beforeEntry["keep-me"].Configured, afterEntry["keep-me"].Configured)

	// Channels, abilities and model definitions are not pricing rows: they must
	// survive the cleanup untouched.
	var channels, abilities int64
	require.NoError(t, DB.Model(&Channel{}).Count(&channels).Error)
	require.NoError(t, DB.Model(&Ability{}).Count(&abilities).Error)
	assert.EqualValues(t, 1, channels)
	assert.EqualValues(t, 2, abilities)

	// Re-running the same plan is stale, and a keep record cannot be cleared.
	_, err = ApplyModelPricingCleanup(report.Snapshot, []ModelPricingCleanupChange{{
		ModelName:       "keep-me",
		ExpectedVersion: beforeEntry["keep-me"].Version,
	}})
	require.ErrorIs(t, err, ErrModelPricingStale, "a cleanup plan must not be replayed after the state changed")
}

func TestApplyModelPricingCleanupRejectsUnreviewedState(t *testing.T) {
	resetModelPricingCleanupTestTables(t)
	insertPricingEndpointChannel(t, 504, 1, pricingEndpointAdvancedCustomConfig())
	insertPricingEndpointAbility(t, 504, "race-model")
	seedModelPricingOverrides(t, map[string]string{"ModelRatio": `{"race-model":0.3}`})

	snapshot, err := GetModelPricingSnapshot([]string{"race-model"})
	require.NoError(t, err)
	staleVersion := snapshot.Entries[0].Version

	report, err := ModelPricingCleanupReport()
	require.NoError(t, err)

	// Another administrator edits the same model between review and apply.
	require.NoError(t, UpdateModelPricing([]ModelPricingChange{{
		ModelName:       "race-model",
		ExpectedVersion: staleVersion,
		Pricing:         PricingValues{"ModelRatio": 0.9},
	}}))

	_, err = ApplyModelPricingCleanup(report.Snapshot, []ModelPricingCleanupChange{{
		ModelName:       "race-model",
		ExpectedVersion: staleVersion,
	}})
	require.ErrorIs(t, err, ErrModelPricingStale)

	saved, err := GetModelPricingSnapshot([]string{"race-model"})
	require.NoError(t, err)
	assert.Equal(t, 0.9, saved.Entries[0].Configured["ModelRatio"], "a rejected cleanup must not overwrite a newer edit")
}
