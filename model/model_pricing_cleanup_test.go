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

func storedOptionEntries(t *testing.T, key string) map[string]any {
	t.Helper()
	var row Option
	err := DB.Where(commonKeyCol+" = ?", key).First(&row).Error
	require.NoError(t, err)
	var entries map[string]any
	require.NoError(t, common.UnmarshalJsonStr(row.Value, &entries))
	return entries
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

// The cleanup list must describe the stored records it will remove, and must not
// treat an inherited engine default as a stored site price.
func TestModelPricingCleanupReportDescribesStoredRecordsOnly(t *testing.T) {
	resetModelPricingCleanupTestTables(t)
	insertPricingEndpointChannel(t, 501, 1, pricingEndpointAdvancedCustomConfig())
	for _, name := range []string{"gpt-4", "clean-model"} {
		insertPricingEndpointAbility(t, 501, name)
	}
	builtInName := "gpt-6-astra"
	insertPricingEndpointAbility(t, 501, builtInName)
	_, hasBuiltIn := billing_setting.GetBuiltinBillingExpr(builtInName)
	require.True(t, hasBuiltIn, "the test needs a model with a built-in upstream price")

	seedModelPricingOverrides(t, map[string]string{
		"ModelRatio":                   `{"gpt-4":0.5,"gpt-6-astra":0.75}`,
		"CompletionRatio":              `{"gpt-4":4}`,
		"billing_setting.billing_mode": `{"gpt-4":"ratio"}`,
		"billing_setting.billing_expr": `{"gpt-4":"tier(\"base\", p * 1)"}`,
	})

	report, err := ModelPricingCleanupReport()
	require.NoError(t, err)

	ratioRecord := cleanupRecordByName(t, report, "gpt-4")
	assert.Equal(t, ModelPricingCleanupClear, ratioRecord.Action)
	assert.Subset(t, ratioRecord.Configured,
		[]string{"ModelRatio", "CompletionRatio", "billing_setting.billing_mode", "billing_setting.billing_expr"})
	// The shared pricing write path materializes engine defaults into storage,
	// so cleanup may also find such a key stored for the model; it clears it
	// rather than leaving it behind.
	for _, key := range ratioRecord.Configured {
		assert.Contains(t, modelPricingSalesKeys, key)
	}
	assert.Equal(t, len(ratioRecord.Configured), ratioRecord.ClearCount)

	// A model with no stored record is already clear even though the engine
	// would resolve a default price for it.
	assert.Equal(t, ModelPricingCleanupKeep, cleanupRecordByName(t, report, "clean-model").Action)
	assert.Zero(t, cleanupRecordByName(t, report, "clean-model").ClearCount)

	// A built-in expression is not a stored record: cleanup must not report it
	// as something to remove, and never writes it into storage.
	builtInRecord := cleanupRecordByName(t, report, builtInName)
	assert.True(t, builtInRecord.BuiltIn)
	assert.Equal(t, ModelPricingCleanupClear, builtInRecord.Action)
	assert.NotContains(t, builtInRecord.Configured, "billing_setting.billing_expr")
}

// Cleanup must empty the stored sales-price record rather than restoring the
// built-in or default client price.
func TestApplyModelPricingCleanupEmptiesStoredPricesInsteadOfRestoringDefaults(t *testing.T) {
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
	require.Contains(t, storedOptionEntries(t, "ModelRatio"), "cleanup-me")

	report, err := ModelPricingCleanupReport()
	require.NoError(t, err)
	applied, err := ApplyModelPricingCleanup(report.Snapshot, []ModelPricingCleanupChange{{
		ModelName:       "cleanup-me",
		ExpectedVersion: beforeEntry["cleanup-me"].Version,
	}})
	require.NoError(t, err)
	assert.Equal(t, []string{"cleanup-me"}, applied.Cleared)

	// The stored maps no longer hold the cleared model, and no default was
	// written back in its place.
	storedRatio := storedOptionEntries(t, "ModelRatio")
	assert.NotContains(t, storedRatio, "cleanup-me", "the cleared price must not be restored")
	assert.NotContains(t, storedOptionEntries(t, "CompletionRatio"), "cleanup-me")
	assert.Equal(t, 0.6, storedRatio["keep-me"], "unselected pricing must be untouched")

	// The engine still resolves a built-in default for display, which is the
	// pre-existing fallback rather than a restored record.
	after, err := GetModelPricingSnapshot([]string{"cleanup-me", "keep-me"})
	require.NoError(t, err)
	afterEntry := make(map[string]ModelPricingEntry, len(after.Entries))
	for _, entry := range after.Entries {
		afterEntry[entry.ModelName] = entry
	}
	assert.Empty(t, afterEntry["cleanup-me"].Configured, "the selected model must lose its own pricing")
	assert.Equal(t, beforeEntry["keep-me"].Version, afterEntry["keep-me"].Version)

	// Channels and abilities are not pricing rows and must survive untouched.
	var channels, abilities int64
	require.NoError(t, DB.Model(&Channel{}).Count(&channels).Error)
	require.NoError(t, DB.Model(&Ability{}).Count(&abilities).Error)
	assert.EqualValues(t, 1, channels)
	assert.EqualValues(t, 2, abilities)

	// Replaying the same plan is stale: cleanup is a one-shot action.
	_, err = ApplyModelPricingCleanup(report.Snapshot, []ModelPricingCleanupChange{{
		ModelName:       "keep-me",
		ExpectedVersion: beforeEntry["keep-me"].Version,
	}})
	require.ErrorIs(t, err, ErrModelPricingStale, "a cleanup plan must not be replayed after the state changed")
}

// Clearing must survive a process restart: startup reloads the stored options,
// which must not reinstate a cleared price.
func TestClearedModelPricingStaysClearedAfterReload(t *testing.T) {
	resetModelPricingCleanupTestTables(t)
	insertPricingEndpointChannel(t, 505, 1, pricingEndpointAdvancedCustomConfig())
	insertPricingEndpointAbility(t, 505, "restart-model")
	seedModelPricingOverrides(t, map[string]string{"ModelRatio": `{"restart-model":0.35}`})

	snapshot, err := GetModelPricingSnapshot([]string{"restart-model"})
	require.NoError(t, err)
	report, err := ModelPricingCleanupReport()
	require.NoError(t, err)
	_, err = ApplyModelPricingCleanup(report.Snapshot, []ModelPricingCleanupChange{{
		ModelName:       "restart-model",
		ExpectedVersion: snapshot.Entries[0].Version,
	}})
	require.NoError(t, err)

	// Simulate startup: defaults are installed, then options are reloaded.
	ratio_setting.InitRatioSettings()
	initCol()
	reloaded, err := storedPricingMaps(DB)
	require.NoError(t, err)
	assert.Empty(t, modelPricingValues(reloaded, "restart-model"),
		"a cleared price must not reappear after reloading stored options")
}

// Cleanup must not refill the pricing list: the shared write path materializes
// engine defaults on save, so cleanup has to persist only the surviving stored
// rows.
func TestApplyModelPricingCleanupDoesNotMaterializeDefaultsForOtherModels(t *testing.T) {
	resetModelPricingCleanupTestTables(t)
	insertPricingEndpointChannel(t, 506, 1, pricingEndpointAdvancedCustomConfig())
	insertPricingEndpointAbility(t, 506, "target-model")
	insertPricingEndpointAbility(t, 506, "bystander-model")
	seedModelPricingOverrides(t, map[string]string{"ModelRatio": `{"target-model":0.4}`})

	before, err := GetModelPricingSnapshot([]string{"target-model", "bystander-model"})
	require.NoError(t, err)
	targetVersion := ""
	for _, entry := range before.Entries {
		if entry.ModelName == "target-model" {
			targetVersion = entry.Version
		}
	}
	require.NotEmpty(t, targetVersion)

	// Scale the concern up: a default-heavy option must not gain records.
	seedStored := len(storedOptionEntries(t, "CompletionRatio"))
	require.Positive(t, seedStored)

	report, err := ModelPricingCleanupReport()
	require.NoError(t, err)
	_, err = ApplyModelPricingCleanup(report.Snapshot, []ModelPricingCleanupChange{{
		ModelName:       "target-model",
		ExpectedVersion: targetVersion,
	}})
	require.NoError(t, err)

	stored, err := storedPricingMaps(DB)
	require.NoError(t, err)
	assert.Empty(t, modelPricingValues(stored, "target-model"), "the cleared model must stay empty")
	assert.Empty(t, modelPricingValues(stored, "bystander-model"),
		"an untouched model must not gain a stored default record")
	assert.Len(t, stored["CompletionRatio"], seedStored,
		"cleanup must not expand the stored pricing list")

	// A second pass cannot loop on the models this cleanup cleared.
	second, err := ModelPricingCleanupReport()
	require.NoError(t, err)
	for _, record := range second.Records {
		if record.ModelName != "target-model" && record.ModelName != "bystander-model" {
			continue
		}
		assert.Equal(t, ModelPricingCleanupKeep, record.Action,
			"%s must not be clearable twice", record.ModelName)
	}
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
