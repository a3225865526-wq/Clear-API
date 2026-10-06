package model

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// L30: the attempt-ledger tables must migrate on SQLite, MySQL and
// PostgreSQL — fresh, idempotent re-run, upgrade with pre-existing rows, and
// a restart (close + reopen + migrate again) that preserves data. MySQL and
// PostgreSQL run only when TEST_MYSQL_DSN / TEST_POSTGRES_DSN are provided.
func TestRouteChainLedgerMigrationDialects(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(filepath.Join(t.TempDir(), "ledger-migrate.db"))
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN is not configured")
				}
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN is not configured")
				}
				driver = postgres.Open(dsn)
			}
			db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(2)
			t.Cleanup(func() { _ = sqlDB.Close() })
			if dialect != "sqlite" {
				// Isolate runs: drop any rows from a previous test execution.
				require.NoError(t, MigrateRouteChainLedger(db))
				require.NoError(t, db.Where("1 = 1").Delete(&RouteChainAttemptRecord{}).Error)
				require.NoError(t, db.Where("1 = 1").Delete(&RouteChainRequestRecord{}).Error)
				require.NoError(t, db.Where("1 = 1").Delete(&RouteChainDecisionRecord{}).Error)
			}

			// 1. Fresh migration on an empty database.
			require.NoError(t, MigrateRouteChainLedger(db))
			require.NoError(t, RouteChainLedgerReady(db))

			// 2. Idempotent re-run.
			require.NoError(t, MigrateRouteChainLedger(db))
			require.NoError(t, RouteChainLedgerReady(db))

			// 3. Upgrade: pre-existing rows survive a re-migration.
			requestId := "ledger-migrate-" + dialect
			require.NoError(t, db.Create(&RouteChainRequestRecord{
				RequestId: requestId, RouteChainId: 7, RouteChainVersion: 2,
				RequestedModel: "m", CanonicalModel: "m", Group: "default",
				StartedAt: time.Now().Unix(), UserBillingState: RouteChainBillingPending,
			}).Error)
			require.NoError(t, db.Create(&RouteChainAttemptRecord{
				AttemptId: requestId + "-a1", RequestId: requestId, Sequence: 1,
				RouteChainId: 7, CandidateId: 3, CandidateType: RouteChainCandidateTypePool,
				StartedAt: time.Now().Unix(), DispatchState: RouteChainPhaseDispatched,
				UsageState: RouteChainUsagePending, ProviderCostState: RouteChainCostZeroConfirmed,
			}).Error)
			require.NoError(t, db.Create(&RouteChainDecisionRecord{
				RequestId: requestId, ChainId: 7, CandidateId: 3,
				Phase: "selection_skip", Decision: "skipped", Reason: "unavailable:channel_disabled", At: time.Now().Unix(),
			}).Error)
			require.NoError(t, MigrateRouteChainLedger(db))
			require.NoError(t, RouteChainLedgerReady(db))
			var preserved RouteChainAttemptRecord
			require.NoError(t, db.Where("attempt_id = ?", requestId+"-a1").First(&preserved).Error)
			assert.Equal(t, RouteChainCostZeroConfirmed, preserved.ProviderCostState)
			assert.Equal(t, 1, preserved.Sequence)

			// 4. Restart: close, reopen, migrate again — data preserved.
			require.NoError(t, sqlDB.Close())
			reopened, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			reopenedSQL, err := reopened.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = reopenedSQL.Close() })
			require.NoError(t, MigrateRouteChainLedger(reopened))
			require.NoError(t, RouteChainLedgerReady(reopened))
			var stillThere RouteChainAttemptRecord
			require.NoError(t, reopened.Where("attempt_id = ?", requestId+"-a1").First(&stillThere).Error)
			assert.Equal(t, preserved.AttemptId, stillThere.AttemptId)
		})
	}
}
