package model

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func routeChainFixture() *RouteChainWithCandidates {
	return &RouteChainWithCandidates{
		RouteChain: RouteChain{
			Model:   "gpt-test",
			Group:   "default",
			Enabled: true,
		},
		Candidates: []RouteChainCandidate{
			{CandidateType: RouteChainCandidateTypePool, TargetId: 11, Priority: 10, Enabled: true},
			{CandidateType: RouteChainCandidateTypeAPIChannel, TargetId: 22, Priority: 5, Enabled: true},
		},
	}
}

func TestRouteChainDatabase(t *testing.T) {
	oldDB, oldRedis, oldBatch := DB, common.RedisEnabled, common.BatchUpdateEnabled
	oldType := common.MainDatabaseType()
	common.RedisEnabled, common.BatchUpdateEnabled = false, false
	t.Cleanup(func() {
		DB = oldDB
		common.RedisEnabled, common.BatchUpdateEnabled = oldRedis, oldBatch
		common.SetMainDatabaseType(oldType)
		initCol()
	})
	dialect, dsn := os.Getenv("TEST_ROUTE_CHAIN_DIALECT"), os.Getenv("TEST_ROUTE_CHAIN_DSN")
	var driver gorm.Dialector
	switch dialect {
	case "", "sqlite":
		driver = sqlite.Open(filepath.Join(t.TempDir(), "route_chain.db"))
		common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	case "mysql":
		require.NotEmpty(t, dsn)
		driver = mysql.Open(dsn)
		common.SetMainDatabaseType(common.DatabaseTypeMySQL)
	case "postgres":
		require.NotEmpty(t, dsn)
		driver = postgres.Open(dsn)
		common.SetMainDatabaseType(common.DatabaseTypePostgreSQL)
	default:
		t.Fatal("unsupported dialect")
	}
	db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	DB = db
	initCol()

	var version string
	versionQuery := "select version()"
	if dialect == "" || dialect == "sqlite" {
		versionQuery = "select sqlite_version()"
	}
	require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
	t.Logf("route chain dialect %s version %s", dialect, version)

	// Migration is idempotent: a fresh database and a repeated pass both succeed.
	require.NoError(t, MigrateRouteChain(db))
	require.NoError(t, MigrateRouteChain(db))
	// The stage 6 archive guard queries the attempt ledger inside chain saves,
	// so the ledger tables belong to this harness too.
	require.NoError(t, MigrateRouteChainLedger(db))

	chain := routeChainFixture()
	require.NoError(t, UpsertRouteChain(chain, 0))
	require.Equal(t, 1, chain.Version)
	require.NotZero(t, chain.Id)
	for i := range chain.Candidates {
		require.NotZero(t, chain.Candidates[i].Id)
	}

	// Duplicate (model, group) creation fails closed.
	duplicate := routeChainFixture()
	require.ErrorIs(t, UpsertRouteChain(duplicate, 0), ErrRouteChainExists)

	loaded, err := GetRouteChainForModel("gpt-test", "default")
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.True(t, loaded.Enabled)
	require.Len(t, loaded.Candidates, 2)
	require.Equal(t, RouteChainCandidateTypePool, loaded.Candidates[0].CandidateType)
	require.Equal(t, 10, loaded.Candidates[0].Priority)
	require.Equal(t, 5, loaded.Candidates[1].Priority)

	missing, err := GetRouteChainForModel("gpt-test", "other-group")
	require.NoError(t, err)
	require.Nil(t, missing)

	enabledChain, err := GetEnabledRouteChainForModel("gpt-test", "default")
	require.NoError(t, err)
	require.NotNil(t, enabledChain)
	require.NoError(t, DeleteRouteChain(loaded.Id, loaded.Version))
	enabledChain, err = GetEnabledRouteChainForModel("gpt-test", "default")
	require.NoError(t, err)
	require.Nil(t, enabledChain)

	// Recreate and exercise CAS updates.
	chain = routeChainFixture()
	require.NoError(t, UpsertRouteChain(chain, 0))
	require.NoError(t, UpsertRouteChain(chain, chain.Version))
	require.Equal(t, 2, chain.Version)
	require.ErrorIs(t, UpsertRouteChain(chain, 1), ErrRouteChainVersionConflict)

	stale := routeChainFixture()
	stale.Id = chain.Id
	stale.Version = 99
	require.ErrorIs(t, UpsertRouteChain(stale, 99), ErrRouteChainVersionConflict)

	// Candidate sync keeps existing ids, inserts new rows, deletes removed rows.
	keptId := chain.Candidates[0].Id
	chain.Candidates[0].Priority = 20
	chain.Candidates = append(chain.Candidates[:1], RouteChainCandidate{
		CandidateType: RouteChainCandidateTypeAPIChannel,
		TargetId:      33,
		Priority:      5,
		Enabled:       true,
	})
	require.NoError(t, UpsertRouteChain(chain, chain.Version))
	require.Equal(t, 3, chain.Version)
	loaded, err = GetRouteChainById(chain.Id)
	require.NoError(t, err)
	require.Len(t, loaded.Candidates, 2)
	require.Equal(t, keptId, loaded.Candidates[0].Id)
	require.Equal(t, 20, loaded.Candidates[0].Priority)
	require.Equal(t, RouteChainCandidateTypeAPIChannel, loaded.Candidates[1].CandidateType)
	require.Equal(t, 33, loaded.Candidates[1].TargetId)

	// Reorder assigns descending priorities following payload order.
	wrongSet := []int{loaded.Candidates[0].Id, loaded.Candidates[1].Id, 999999}
	require.ErrorIs(t, ReorderRouteChainCandidates(loaded.Id, wrongSet, loaded.Version), ErrRouteChainCandidateSet)
	ordered := []int{loaded.Candidates[1].Id, loaded.Candidates[0].Id}
	require.NoError(t, ReorderRouteChainCandidates(loaded.Id, ordered, loaded.Version))
	loaded, err = GetRouteChainById(loaded.Id)
	require.NoError(t, err)
	require.Equal(t, ordered[0], loaded.Candidates[0].Id)
	require.Equal(t, 2, loaded.Candidates[0].Priority)
	require.Equal(t, 1, loaded.Candidates[1].Priority)
	require.Equal(t, 4, loaded.Version)
	require.ErrorIs(t, ReorderRouteChainCandidates(loaded.Id, ordered, loaded.Version-1), ErrRouteChainVersionConflict)

	// A full priority swap between two candidates must succeed (transient
	// duplicate priorities are legal inside the CAS-serialized transaction).
	swapped := []int{ordered[1], ordered[0]}
	require.NoError(t, ReorderRouteChainCandidates(loaded.Id, swapped, loaded.Version))
	loaded, err = GetRouteChainById(loaded.Id)
	require.NoError(t, err)
	require.Equal(t, swapped[0], loaded.Candidates[0].Id)
	require.Equal(t, swapped[1], loaded.Candidates[1].Id)

	// Candidate enable/disable is CAS-guarded too.
	first := loaded.Candidates[0]
	require.NoError(t, SetRouteChainCandidateEnabled(loaded.Id, first.Id, false, loaded.Version))
	loaded, err = GetRouteChainById(loaded.Id)
	require.NoError(t, err)
	require.False(t, loaded.Candidates[0].Enabled)
	require.ErrorIs(t, SetRouteChainCandidateEnabled(loaded.Id, first.Id, true, 1), ErrRouteChainVersionConflict)

	require.NoError(t, DeleteRouteChain(loaded.Id, loaded.Version))
	gone, err := GetRouteChainForModel("gpt-test", "default")
	require.NoError(t, err)
	require.Nil(t, gone)
	require.ErrorIs(t, DeleteRouteChain(loaded.Id, loaded.Version), ErrRouteChainVersionConflict)
}

func TestRouteChainValidation(t *testing.T) {
	oldDB := DB
	oldType := common.MainDatabaseType()
	t.Cleanup(func() {
		DB = oldDB
		common.SetMainDatabaseType(oldType)
		initCol()
	})
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "route_chain_validate.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	initCol()
	require.NoError(t, MigrateRouteChain(db))
	require.NoError(t, MigrateRouteChainLedger(db))

	rc := func(model string, group string, candidates ...RouteChainCandidate) *RouteChainWithCandidates {
		return &RouteChainWithCandidates{RouteChain: RouteChain{Model: model, Group: group}, Candidates: candidates}
	}
	pool := func(target int, priority int) RouteChainCandidate {
		return RouteChainCandidate{CandidateType: RouteChainCandidateTypePool, TargetId: target, Priority: priority}
	}
	apiChannel := func(target int, priority int) RouteChainCandidate {
		return RouteChainCandidate{CandidateType: RouteChainCandidateTypeAPIChannel, TargetId: target, Priority: priority}
	}
	tooMany := make([]RouteChainCandidate, RouteChainMaxCandidateRows+1)
	for i := range tooMany {
		tooMany[i] = RouteChainCandidate{CandidateType: RouteChainCandidateTypePool, TargetId: i + 1, Priority: i + 1}
	}

	cases := map[string]*RouteChainWithCandidates{
		"empty model":         rc(" ", "default", pool(1, 1)),
		"empty group":         rc("m", "", pool(1, 1)),
		"no candidates":       rc("m", "g"),
		"too many candidates": rc("m", "g", tooMany...),
		"unknown type":        rc("m", "g", RouteChainCandidate{CandidateType: "magic", TargetId: 1, Priority: 1}),
		"bad target":          rc("m", "g", pool(0, 1)),
		"duplicate priority":  rc("m", "g", pool(1, 1), apiChannel(2, 1)),
		"duplicate target":    rc("m", "g", pool(1, 1), pool(1, 2)),
		"invalid policy json": rc("m", "g", RouteChainCandidate{CandidateType: RouteChainCandidateTypePool, TargetId: 1, Priority: 1, Policy: "{not-json"}),
		"unknown capability":  rc("m", "g", RouteChainCandidate{CandidateType: RouteChainCandidateTypePool, TargetId: 1, Priority: 1, Policy: `{"capabilities":["teleport"]}`}),
		"bad threshold":       rc("m", "g", RouteChainCandidate{CandidateType: RouteChainCandidateTypePool, TargetId: 1, Priority: 1, Policy: `{"health_policy":{"failure_threshold":99}}`}),
		"bad cooldown":        rc("m", "g", RouteChainCandidate{CandidateType: RouteChainCandidateTypePool, TargetId: 1, Priority: 1, Policy: `{"health_policy":{"cooldown_seconds":7200}}`}),
		"bad connect timeout": rc("m", "g", RouteChainCandidate{CandidateType: RouteChainCandidateTypePool, TargetId: 1, Priority: 1, Policy: `{"timeout_policy":{"connect_timeout_ms":500000}}`}),
	}
	for name, chain := range cases {
		require.ErrorIs(t, UpsertRouteChain(chain, 0), ErrRouteChainInvalid, name)
	}
}

func TestRouteCandidatePolicyDefaults(t *testing.T) {
	empty, err := ParseRouteCandidatePolicy("")
	require.NoError(t, err)
	require.Equal(t, 3, empty.FailureThreshold())
	require.Equal(t, 60, empty.CooldownSeconds())
	require.False(t, empty.FailoverOn429())
	require.True(t, empty.FailoverOn404())
	require.Equal(t, 0, empty.ConnectTimeoutMs())

	custom, err := ParseRouteCandidatePolicy(`{
		"health_policy": {"failure_threshold": 5, "cooldown_seconds": 120},
		"failure_policy": {"failover_on_429": true, "failover_on_404": false},
		"timeout_policy": {"connect_timeout_ms": 8000},
		"capabilities": ["stream", "tools"]
	}`)
	require.NoError(t, err)
	require.Equal(t, 5, custom.FailureThreshold())
	require.Equal(t, 120, custom.CooldownSeconds())
	require.True(t, custom.FailoverOn429())
	require.False(t, custom.FailoverOn404())
	require.Equal(t, 8000, custom.ConnectTimeoutMs())
}
