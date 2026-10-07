package model

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// Exact Stage 7.2 table shape: upgrade tests must not create the old database
// using today's schema or today's create hook.
type stage72RouteChain struct {
	Id               int
	Model            string `gorm:"type:varchar(255);uniqueIndex:idx_route_chain_model_group,priority:1"`
	Group            string `gorm:"type:varchar(64);uniqueIndex:idx_route_chain_model_group,priority:2"`
	Enabled          bool   `gorm:"default:false"`
	AllowCrossSource bool   `gorm:"default:false"`
	Archived         bool
	Version          int `gorm:"default:1"`
	CreatedTime      int64
	UpdatedTime      int64
}

func (stage72RouteChain) TableName() string { return "route_chains" }

func TestStage73ActiveSlot(t *testing.T) {
	oldDB, oldType := DB, common.MainDatabaseType()
	t.Cleanup(func() { DB = oldDB; common.SetMainDatabaseType(oldType); initCol() })
	dialect := os.Getenv("TEST_STAGE73_DIALECT")
	if dialect == "" {
		dialect = "sqlite"
	}
	for _, mode := range []string{"fresh", "upgrade", "interrupted", "duplicates", "invalid_index"} {
		t.Run(mode, func(t *testing.T) {
			var driver gorm.Dialector
			dsn := strings.ReplaceAll(os.Getenv("TEST_STAGE73_DSN"), "MODE", mode)
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(filepath.Join(t.TempDir(), "stage73.db") + "?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)")
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
			sqlDB.SetMaxOpenConns(8)
			t.Cleanup(func() { _ = sqlDB.Close() })
			DB = db
			initCol()
			versionQuery := "select version()"
			if dialect == "sqlite" {
				versionQuery = "select sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
			t.Logf("dialect=%s version=%s mode=%s", dialect, version, mode)
			require.NoError(t, MigrateRouteChainLedger(db))
			var legacy []stage72RouteChain
			var legacyCandidate RouteChainCandidate
			var legacyRequest RouteChainRequestRecord
			var legacyAttempt RouteChainAttemptRecord
			var legacyDecision RouteChainDecisionRecord
			if mode != "fresh" {
				require.NoError(t, db.AutoMigrate(&stage72RouteChain{}, &RouteChainCandidate{}))
				legacy = []stage72RouteChain{
					{Model: "gpt-5.6-luna", Group: "default", Archived: true, Version: 7, CreatedTime: 100, UpdatedTime: 200},
					{Model: "existing-active", Group: "default", Enabled: true, Version: 4, CreatedTime: 101, UpdatedTime: 201},
				}
				require.NoError(t, db.Create(&legacy).Error)
				legacyCandidate = RouteChainCandidate{ChainId: legacy[0].Id, CandidateType: RouteChainCandidateTypePool, TargetId: 77, Priority: 1, Enabled: true}
				require.NoError(t, db.Create(&legacyCandidate).Error)
				legacyRequest = RouteChainRequestRecord{RequestId: "pre-upgrade", RouteChainId: legacy[0].Id, RouteChainVersion: legacy[0].Version}
				legacyAttempt = RouteChainAttemptRecord{AttemptId: "pre-upgrade-attempt", RequestId: legacyRequest.RequestId, RouteChainId: legacy[0].Id, CandidateId: legacyCandidate.Id, Sequence: 1}
				legacyDecision = RouteChainDecisionRecord{RequestId: legacyRequest.RequestId, ChainId: legacy[0].Id, CandidateId: legacyCandidate.Id}
				require.NoError(t, db.Create(&legacyRequest).Error)
				require.NoError(t, db.Create(&legacyAttempt).Error)
				require.NoError(t, db.Create(&legacyDecision).Error)
				if mode == "duplicates" {
					// A deliberately corrupt old schema must fail before schema/data edits.
					require.NoError(t, db.Migrator().DropIndex(&stage72RouteChain{}, "idx_route_chain_model_group"))
					require.NoError(t, db.Create(&stage72RouteChain{Model: "existing-active", Group: "default", Version: 1}).Error)
					require.ErrorIs(t, MigrateRouteChain(db), ErrRouteChainExists)
					require.False(t, db.Migrator().HasColumn(&RouteChain{}, "active_scope_key"))
					var count int64
					require.NoError(t, db.Model(&stage72RouteChain{}).Count(&count).Error)
					require.EqualValues(t, 3, count)
					return
				}
				if mode == "interrupted" || mode == "invalid_index" {
					require.NoError(t, db.Migrator().AddColumn(&RouteChain{}, "ActiveScopeKey"))
					if mode == "invalid_index" {
						require.NoError(t, db.Exec("CREATE INDEX idx_route_chain_active_scope ON route_chains(active_scope_key)").Error)
						require.ErrorContains(t, MigrateRouteChain(db), "unique index missing or invalid")
						require.True(t, db.Migrator().HasIndex(&RouteChain{}, "idx_route_chain_model_group"))
						return
					}
					require.NoError(t, db.Migrator().CreateIndex(&RouteChain{}, "idx_route_chain_active_scope"))
				}
			}
			require.NoError(t, MigrateRouteChain(db))
			require.NoError(t, MigrateRouteChain(db))
			require.False(t, db.Migrator().HasIndex(&RouteChain{}, "idx_route_chain_model_group"))
			indexes, err := db.Migrator().GetIndexes(&RouteChain{})
			require.NoError(t, err)
			for _, index := range indexes {
				t.Logf("index=%s columns=%v", index.Name(), index.Columns())
			}
			for _, previous := range legacy {
				var unchanged stage72RouteChain
				require.NoError(t, db.First(&unchanged, previous.Id).Error)
				require.Equal(t, previous, unchanged, "all original columns preserved")
				var current RouteChain
				require.NoError(t, db.First(&current, previous.Id).Error)
				if previous.Archived {
					require.Nil(t, current.ActiveScopeKey)
				} else {
					require.NotNil(t, current.ActiveScopeKey)
					require.Equal(t, routeChainActiveScopeKey(previous.Model, previous.Group), *current.ActiveScopeKey)
				}
			}
			if len(legacy) > 0 {
				var candidate RouteChainCandidate
				var request RouteChainRequestRecord
				var attempt RouteChainAttemptRecord
				var decision RouteChainDecisionRecord
				require.NoError(t, db.First(&candidate, legacyCandidate.Id).Error)
				require.NoError(t, db.First(&request, legacyRequest.Id).Error)
				require.NoError(t, db.First(&attempt, legacyAttempt.Id).Error)
				require.NoError(t, db.First(&decision, legacyDecision.Id).Error)
				require.Equal(t, legacyCandidate, candidate)
				require.Equal(t, legacyRequest, request)
				require.Equal(t, legacyAttempt, attempt)
				require.Equal(t, legacyDecision, decision)
			}
			// A1-A7: preserve full ledger and candidates through successive reuse.
			newChain := func(name string) *RouteChainWithCandidates {
				chain := routeChainFixture()
				chain.Model = name
				return chain
			}
			first := newChain("gpt-5.6-luna")
			require.NoError(t, UpsertRouteChain(first, 0))
			require.ErrorIs(t, UpsertRouteChain(newChain(first.Model), 0), ErrRouteChainExists)
			request := RouteChainRequestRecord{RequestId: "stage73-history", RouteChainId: first.Id, RouteChainVersion: first.Version}
			attempt := RouteChainAttemptRecord{AttemptId: "stage73-attempt", RequestId: request.RequestId, Sequence: 1, RouteChainId: first.Id, CandidateId: first.Candidates[0].Id}
			decision := RouteChainDecisionRecord{RequestId: request.RequestId, ChainId: first.Id, CandidateId: first.Candidates[0].Id}
			require.NoError(t, db.Create(&request).Error)
			require.NoError(t, db.Create(&attempt).Error)
			require.NoError(t, db.Create(&decision).Error)
			require.NoError(t, ArchiveRouteChain(first.Id, first.Version))
			history, err := GetRouteChainById(first.Id)
			require.NoError(t, err)
			require.True(t, history.Archived)
			require.False(t, history.Enabled)
			require.Nil(t, history.ActiveScopeKey)
			require.Equal(t, first.Model, history.Model)
			require.Equal(t, first.Group, history.Group)
			require.Equal(t, first.Candidates, history.Candidates)
			require.ErrorIs(t, ArchiveRouteChain(first.Id, first.Version), ErrRouteChainVersionConflict)
			require.ErrorIs(t, SetRouteChainEnabled(first.Id, true, history.Version), ErrRouteChainVersionConflict)
			require.ErrorIs(t, SetRouteChainCandidateEnabled(first.Id, first.Candidates[0].Id, false, history.Version), ErrRouteChainVersionConflict)
			require.ErrorIs(t, ReorderRouteChainCandidates(first.Id, []int{first.Candidates[0].Id, first.Candidates[1].Id}, history.Version), ErrRouteChainVersionConflict)
			require.ErrorIs(t, UpsertRouteChain(history, history.Version), ErrRouteChainInvalid)
			resolved, err := GetEnabledRouteChainForModel(first.Model, first.Group)
			require.NoError(t, err)
			require.Nil(t, resolved)
			second := newChain(first.Model)
			require.NoError(t, UpsertRouteChain(second, 0))
			require.NoError(t, ArchiveRouteChain(second.Id, second.Version))
			third := newChain(first.Model)
			require.NoError(t, UpsertRouteChain(third, 0))
			resolved, err = GetEnabledRouteChainForModel(first.Model, first.Group)
			require.NoError(t, err)
			require.Equal(t, third.Id, resolved.Id)
			var keptRequest RouteChainRequestRecord
			var keptAttempt RouteChainAttemptRecord
			var keptDecision RouteChainDecisionRecord
			require.NoError(t, db.First(&keptRequest, request.Id).Error)
			require.NoError(t, db.First(&keptAttempt, attempt.Id).Error)
			require.NoError(t, db.First(&keptDecision, decision.Id).Error)
			require.Equal(t, request, keptRequest)
			require.Equal(t, attempt, keptAttempt)
			require.Equal(t, decision, keptDecision)
			// Disable reserves the slot; CAS update cannot drop or forge its key.
			require.NoError(t, SetRouteChainEnabled(third.Id, false, third.Version))
			require.ErrorIs(t, UpsertRouteChain(newChain(first.Model), 0), ErrRouteChainExists)
			third.Version++
			third.Enabled = false
			third.ActiveScopeKey = nil
			require.NoError(t, UpsertRouteChain(third, third.Version))
			require.NotNil(t, third.ActiveScopeKey)
			// A8: both writers start together. SQLite uses two real connections.
			start := make(chan struct{})
			results := make(chan error, 2)
			var writers sync.WaitGroup
			for range 2 {
				writers.Go(func() { <-start; results <- UpsertRouteChain(newChain("concurrent"), 0) })
			}
			close(start)
			writers.Wait()
			close(results)
			successes, conflicts := 0, 0
			for err := range results {
				if err == nil {
					successes++
				} else {
					require.ErrorIs(t, err, ErrRouteChainExists)
					conflicts++
				}
			}
			require.Equal(t, 1, successes)
			require.Equal(t, 1, conflicts)
			// A9: archive and create race; create can conflict before release.
			for round := range 8 {
				name := "archive-race-" + fmt.Sprint(round)
				active := newChain(name)
				require.NoError(t, UpsertRouteChain(active, 0))
				start := make(chan struct{})
				archiveResult, createResult := make(chan error, 1), make(chan error, 1)
				writers.Go(func() { <-start; archiveResult <- ArchiveRouteChain(active.Id, active.Version) })
				writers.Go(func() { <-start; createResult <- UpsertRouteChain(newChain(name), 0) })
				close(start)
				writers.Wait()
				require.NoError(t, <-archiveResult)
				if err := <-createResult; err != nil {
					require.ErrorIs(t, err, ErrRouteChainExists)
					require.NoError(t, UpsertRouteChain(newChain(name), 0))
				}
				var count int64
				require.NoError(t, db.Model(&RouteChain{}).Where("active_scope_key = ? AND archived = ?", routeChainActiveScopeKey(name, "default"), false).Count(&count).Error)
				require.EqualValues(t, 1, count)
			}
			require.NoError(t, MigrateRouteChain(db)) // multiple historical scopes survive rerun
		})
	}
	// Delimiter ambiguity, byte lengths, Unicode and exact identity.
	require.NotEqual(t, routeChainActiveScopeKey("a:b", "c"), routeChainActiveScopeKey("a", "b:c"))
	require.NotEqual(t, routeChainActiveScopeKey("模型", "default"), routeChainActiveScopeKey("模型d", "efault"))
	require.Equal(t, routeChainActiveScopeKey("模型", "默认"), routeChainActiveScopeKey("模型", "默认"))
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
