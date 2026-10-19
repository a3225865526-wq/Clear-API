package model

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// openModelPricingCleanupDialect opens an isolated database for the requested
// dialect. MySQL and PostgreSQL require an explicit DSN and are skipped when it
// is absent, matching the existing database-verification convention.
func openModelPricingCleanupDialect(t *testing.T, dialect string) *gorm.DB {
	t.Helper()
	// Quoted column names and the main database type are process globals. Capture
	// them before this helper changes anything, and restore on cleanup, so later
	// tests keep the dialect they expect.
	previousType := common.MainDatabaseType()
	previousKeyCol, previousGroupCol := commonKeyCol, commonGroupCol
	previousTrueVal, previousFalseVal := commonTrueVal, commonFalseVal
	t.Cleanup(func() {
		common.SetMainDatabaseType(previousType)
		commonKeyCol, commonGroupCol = previousKeyCol, previousGroupCol
		commonTrueVal, commonFalseVal = previousTrueVal, previousFalseVal
	})
	var driver gorm.Dialector
	switch dialect {
	case "sqlite":
		common.SetMainDatabaseType(common.DatabaseTypeSQLite)
		driver = sqlite.Open(filepath.Join(t.TempDir(), "pricing_cleanup.db"))
	case "mysql":
		dsn := os.Getenv("TEST_MYSQL_DSN")
		if dsn == "" {
			t.Skip("TEST_MYSQL_DSN is not configured")
		}
		common.SetMainDatabaseType(common.DatabaseTypeMySQL)
		driver = mysql.Open(dsn)
	case "postgres":
		dsn := os.Getenv("TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("TEST_POSTGRES_DSN is not configured")
		}
		common.SetMainDatabaseType(common.DatabaseTypePostgreSQL)
		driver = postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true})
	default:
		t.Fatalf("unsupported dialect %q", dialect)
	}
	// Quoted column names are dialect-specific; set them up before any query.
	initCol()
	db, err := gorm.Open(driver, &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Option{}, &Channel{}, &Ability{}))
	for _, table := range []string{"options", "abilities", "channels"} {
		require.NoError(t, db.Exec("DELETE FROM "+table).Error)
	}
	// The pricing write path publishes to common.OptionMap.
	common.OptionMapRWMutex.Lock()
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		for _, table := range []string{"options", "abilities", "channels"} {
			require.NoError(t, db.Exec("DELETE FROM "+table).Error)
		}
	})
	return db
}

// Cleanup must empty the stored pricing records on every supported database and
// must not write engine defaults back into storage.
func TestModelPricingCleanupAcrossDialects(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := openModelPricingCleanupDialect(t, dialect)
			// DB and the pricing caches are process globals. Restore them before
			// the connection closes so later tests never observe this dialect.
			previousDB, previousLogDB := DB, LOG_DB
			previousQuotaUnit := common.QuotaPerUnit
			DB = db
			t.Cleanup(func() {
				DB, LOG_DB = previousDB, previousLogDB
				common.QuotaPerUnit = previousQuotaUnit
				// Restore dialect-aware quoting for the restored database before
				// any later test queries it.
				initCol()
				ratio_setting.InitRatioSettings()
				InvalidatePricingCache()
			})
			ratio_setting.InitRatioSettings()

			require.NoError(t, db.Create(&Channel{
				Id: 801, Type: 1, Key: "key-801", Status: common.ChannelStatusEnabled, Name: "channel-801",
			}).Error)
			for _, name := range []string{"dialect-model", "bystander-model"} {
				require.NoError(t, db.Create(&Ability{Group: "default", Model: name, ChannelId: 801, Enabled: true}).Error)
			}

			// Seed stored prices, then clear one model through the real path.
			require.NoError(t, UpdateModelPricingOptions(map[string]string{
				"ModelRatio":      `{"dialect-model":0.4}`,
				"CompletionRatio": `{"dialect-model":3}`,
			}))

			before, err := GetModelPricingSnapshot([]string{"dialect-model"})
			require.NoError(t, err)
			require.NotEmpty(t, before.Entries[0].Version)
			storedBefore, err := storedPricingMaps(db)
			require.NoError(t, err)

			report, err := ModelPricingCleanupReport()
			require.NoError(t, err)
			applied, err := ApplyModelPricingCleanup(report.Snapshot, []ModelPricingCleanupChange{{
				ModelName:       "dialect-model",
				ExpectedVersion: before.Entries[0].Version,
			}})
			require.NoError(t, err)
			assert.Equal(t, []string{"dialect-model"}, applied.Cleared)

			storedAfter, err := storedPricingMaps(db)
			require.NoError(t, err)
			assert.Empty(t, modelPricingValues(storedAfter, "dialect-model"),
				"the cleared model must hold no stored price")
			assert.NotContains(t, storedAfter["ModelRatio"], "dialect-model")
			assert.NotContains(t, storedAfter["CompletionRatio"], "dialect-model")
			// Cleanup removes the cleared entries and adds nothing: the stored
			// option maps may only shrink.
			for key, beforeEntries := range storedBefore {
				assert.LessOrEqual(t, len(storedAfter[key]), len(beforeEntries),
					"cleanup must not materialize default records in %s", key)
			}

			// Model definitions and channels survive; nothing else was deleted.
			var channels, abilities int64
			require.NoError(t, db.Model(&Channel{}).Count(&channels).Error)
			require.NoError(t, db.Model(&Ability{}).Count(&abilities).Error)
			assert.EqualValues(t, 1, channels)
			assert.EqualValues(t, 2, abilities)
		})
	}
}
