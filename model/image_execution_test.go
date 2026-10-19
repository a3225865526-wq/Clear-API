package model

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestImageExecutionDatabase proves the authoritative image state machine on a
// real database: idempotency binding, the two separate dispatch boundaries,
// duplicate-terminal suppression, conflicting-body refusal and the UNKNOWN hold.
func TestImageExecutionDatabase(t *testing.T) {
	oldDB := DB
	oldType := common.MainDatabaseType()
	t.Cleanup(func() {
		DB = oldDB
		common.SetMainDatabaseType(oldType)
	})
	dialect, dsn := os.Getenv("TEST_IMAGE_DIALECT"), os.Getenv("TEST_IMAGE_DSN")
	var driver gorm.Dialector
	switch dialect {
	case "", "sqlite":
		driver = sqlite.Open(filepath.Join(t.TempDir(), "image.db"))
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
		t.Fatalf("unsupported dialect %q", dialect)
	}
	db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	DB = db
	// A shared server database may retain rows from an earlier run; the test
	// owns these two tables, so start from a clean schema on every run.
	require.NoError(t, db.Migrator().DropTable(&ImageExecutionAttempt{}, &ImageExecution{}))
	// Migrating twice must stay idempotent on every dialect.
	require.NoError(t, MigrateImageExecutions(db))
	require.NoError(t, MigrateImageExecutions(db))
	require.NoError(t, ImageExecutionsReady(db))

	body := ImageRequestBodyDigest([]byte(`{"model":"gpt-image-2","prompt":"cat","n":1}`))
	scope, keyDigest := "u11:t12", ImageRequestIdempotencyDigest("client-key")
	entry := &ImageExecution{
		RequestID: "req-1", UserID: 11, TokenID: 12,
		IdempotencyScope: &scope, IdempotencyKeyDigest: &keyDigest,
		BodyDigest: body, Operation: ImageRequestOperation, Model: "gpt-image-2",
		RequestedCount: 1, SourceStrategy: ImageSourcePoolFirst, BillingState: "pending",
	}
	require.NoError(t, CreateImageExecutionRequest(entry))
	require.Equal(t, ImageRequestCreated, entry.State)

	// A repeated key with the same body resolves to the stored request.
	same := &ImageExecution{
		RequestID: "req-2", UserID: 11, TokenID: 12,
		IdempotencyScope: &scope, IdempotencyKeyDigest: &keyDigest,
		BodyDigest: body, Operation: ImageRequestOperation, Model: "gpt-image-2",
		RequestedCount: 1, SourceStrategy: ImageSourcePoolFirst, BillingState: "pending",
	}
	require.ErrorIs(t, CreateImageExecutionRequest(same), ErrImageExecutionConflict)
	require.Equal(t, "req-1", same.RequestID)

	// The same key with a different body is refused.
	different := &ImageExecution{
		RequestID: "req-3", UserID: 11, TokenID: 12,
		IdempotencyScope: &scope, IdempotencyKeyDigest: &keyDigest,
		BodyDigest: ImageRequestBodyDigest([]byte(`{"model":"gpt-image-2","prompt":"dog","n":1}`)),
		Operation:  ImageRequestOperation, Model: "gpt-image-2", RequestedCount: 1, SourceStrategy: ImageSourcePoolFirst,
	}
	require.ErrorIs(t, CreateImageExecutionRequest(different), ErrImageExecutionIdempotent)
	state, err := LoadImageExecutionByIdempotency("u11:t12", ImageRequestIdempotencyDigest("client-key"))
	require.NoError(t, err)
	require.Equal(t, "req-1", state.RequestID)

	// Requests WITHOUT an idempotency key carry no dedup identity at all: two
	// independent keyless generations must both be creatable. A shared empty
	// string in a unique index used to reject every keyless request after the
	// first one on SQLite, MySQL and PostgreSQL alike.
	for _, id := range []string{"req-keyless-1", "req-keyless-2", "req-keyless-3"} {
		keyless := &ImageExecution{
			RequestID: id, UserID: 11, TokenID: 12,
			BodyDigest: body, Operation: ImageRequestOperation, Model: "gpt-image-2",
			RequestedCount: 1, SourceStrategy: ImageSourcePoolFirst, BillingState: "pending",
		}
		require.NoError(t, CreateImageExecutionRequest(keyless), "keyless request %s must be creatable", id)
		require.Equal(t, id, keyless.RequestID)
	}
	keylessCount, err := countImageExecutions(t)
	require.NoError(t, err)
	require.GreaterOrEqual(t, keylessCount, int64(4))

	require.NoError(t, AdvanceImageExecution("req-1", ImageRequestCreated, ImageRequestValidated, nil))
	require.NoError(t, AdvanceImageExecution("req-1", ImageRequestValidated, ImageRequestReserved, map[string]any{"billing_state": "reserved", "reserved_quota": 500}))
	// A stale transition from VALIDATED must fail: the row already moved on.
	require.ErrorIs(t, AdvanceImageExecution("req-1", ImageRequestValidated, ImageRequestReserved, nil), ErrImageExecutionState)

	attempt := &ImageExecutionAttempt{RequestID: "req-1", AttemptID: "req-1-a1", Sequence: 1, CandidateType: RouteChainCandidateTypePool, ChannelID: 7}
	require.NoError(t, CreateImageExecutionAttempt(attempt))
	// The unique (request, attempt) index is the duplicate-dispatch authority.
	duplicate := &ImageExecutionAttempt{RequestID: "req-1", AttemptID: "req-1-a1", Sequence: 1, CandidateType: RouteChainCandidateTypePool, ChannelID: 7}
	require.Error(t, CreateImageExecutionAttempt(duplicate))

	require.NoError(t, BeginImageAttemptDispatch("req-1", "req-1-a1"))
	// Repeating the send intent is idempotent, never a second dispatch.
	require.NoError(t, BeginImageAttemptDispatch("req-1", "req-1-a1"))
	require.NoError(t, MarkImageAttemptGatewaySent("req-1", "req-1-a1"))
	require.NoError(t, MarkImageAttemptGatewaySent("req-1", "req-1-a1"))
	attempts, err := ListImageExecutionAttempts("req-1")
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	require.Greater(t, attempts[0].GatewaySentAt, int64(0))
	// The executor boundary is a different fact and stays unset.
	require.Zero(t, attempts[0].ExecutorDispatchedAt)

	require.NoError(t, MarkImageAttemptDispatched("req-1", "req-1-a1"))
	changed, err := FinalizeImageAttempt("req-1", "req-1-a1", ImageAttemptSucceeded, map[string]any{"receipt_id": "rcpt-1", "billable_image_count": 1, "result_count": 1})
	require.NoError(t, err)
	require.True(t, changed)
	require.NoError(t, AdvanceImageExecution("req-1", "", ImageRequestSucceeded, map[string]any{"billing_state": "settled", "result_state": "COMPLETED", "billable_image_count": 1}))

	// A duplicate identical terminal changes nothing.
	changed, err = FinalizeImageAttempt("req-1", "req-1-a1", ImageAttemptSucceeded, map[string]any{"receipt_id": "rcpt-1"})
	require.NoError(t, err)
	require.False(t, changed)
	// A conflicting terminal is refused.
	_, err = FinalizeImageAttempt("req-1", "req-1-a1", ImageAttemptFailed, map[string]any{"end_reason": "late_failure"})
	require.ErrorIs(t, err, ErrImageExecutionState)

	// UNKNOWN keeps the request open for reconciliation.
	unknown := &ImageExecution{RequestID: "req-4", UserID: 11, TokenID: 12, BodyDigest: body, Operation: ImageRequestOperation, Model: "gpt-image-2", RequestedCount: 1, SourceStrategy: ImageSourcePoolFirst}
	require.NoError(t, CreateImageExecutionRequest(unknown))
	require.NoError(t, AdvanceImageExecution("req-4", ImageRequestCreated, ImageRequestReserved, nil))
	require.NoError(t, BillingHoldImageExecution("req-4"))
	held, err := LoadImageExecution("req-4")
	require.NoError(t, err)
	require.Equal(t, ImageRequestUnknown, held.State)
	require.Equal(t, "held_unknown", held.BillingState)
	// An UNKNOWN request is part of the pending set: it must never disappear
	// silently from reconciliation.
	pending, err := ImageExecutionsPending(50)
	require.NoError(t, err)
	found := false
	for _, row := range pending {
		if row.RequestID == "req-4" {
			found = true
		}
	}
	require.True(t, found)

	view := ProjectImageExecution(held, nil)
	require.Equal(t, "req-4", view.RequestID)
	require.Equal(t, ImageRequestUnknown, view.State)
}

// countImageExecutions reports how many request rows exist, so a keyless-create
// regression cannot pass by silently collapsing two requests onto one row.
func countImageExecutions(t *testing.T) (int64, error) {
	t.Helper()
	var count int64
	err := DB.Model(&ImageExecution{}).Count(&count).Error
	return count, err
}

// legacyImageExecution is the previously shipped shape, where both idempotency
// columns were non-nullable strings.
type legacyImageExecution struct {
	ID                   int64  `gorm:"primaryKey"`
	RequestID            string `gorm:"type:varchar(128);uniqueIndex:idx_image_execution_request_id"`
	UserID               int
	TokenID              int
	IdempotencyScope     string `gorm:"type:varchar(64);uniqueIndex:idx_image_execution_idempotency,priority:1"`
	IdempotencyKeyDigest string `gorm:"type:varchar(64);uniqueIndex:idx_image_execution_idempotency,priority:2"`
	BodyDigest           string `gorm:"type:varchar(64)"`
	Operation            string `gorm:"type:varchar(32)"`
	Model                string `gorm:"type:varchar(128)"`
	RequestedCount       int
	State                string
	BillingState         string
	CreatedTime          int64
	UpdatedTime          int64
}

func (legacyImageExecution) TableName() string { return "image_executions" }

// TestImageExecutionUpgradeFromLegacySchema proves the nullable-column change is
// a safe upgrade, not only a fresh-install shape: an existing table that already
// holds a keyed row must migrate in place (twice, idempotently), keep that row
// readable, and start accepting keyless rows.
func TestImageExecutionUpgradeFromLegacySchema(t *testing.T) {
	oldDB := DB
	t.Cleanup(func() { DB = oldDB })
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "upgrade.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	DB = db
	require.NoError(t, db.Migrator().DropTable(&legacyImageExecution{}))
	require.NoError(t, db.AutoMigrate(&legacyImageExecution{}))

	now := time.Now().Unix()
	require.NoError(t, db.Exec(
		`INSERT INTO image_executions (request_id, user_id, token_id, idempotency_scope, idempotency_key_digest, body_digest, operation, model, requested_count, state, billing_state, created_time, updated_time)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"legacy-req", 11, 12, "u11:t12", "digest-abc", "body-abc", ImageRequestOperation, "gpt-image-2", 1, ImageRequestSucceeded, "settled", now, now,
	).Error)

	require.NoError(t, MigrateImageExecutions(db))
	require.NoError(t, MigrateImageExecutions(db))
	require.NoError(t, ImageExecutionsReady(db))

	loaded, err := LoadImageExecution("legacy-req")
	require.NoError(t, err)
	require.NotNil(t, loaded.IdempotencyScope)
	require.Equal(t, "u11:t12", *loaded.IdempotencyScope)

	for _, id := range []string{"post-upgrade-1", "post-upgrade-2"} {
		entry := &ImageExecution{
			RequestID: id, UserID: 11, TokenID: 12,
			BodyDigest: "body-keyless", Operation: ImageRequestOperation, Model: "gpt-image-2",
			RequestedCount: 1, SourceStrategy: ImageSourcePoolFirst, BillingState: "pending",
		}
		require.NoError(t, CreateImageExecutionRequest(entry), "keyless row %s after upgrade", id)
	}
}

// TestImageSourcePolicyDatabase proves the four presets, the CAS versioning and
// the refusal of unknown strategies.
func TestImageSourcePolicyDatabase(t *testing.T) {
	oldDB := DB
	t.Cleanup(func() { DB = oldDB })
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "policy.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	DB = db
	require.NoError(t, db.AutoMigrate(&Option{}))
	oldOptions := common.OptionMap
	common.OptionMapRWMutex.Lock()
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = oldOptions
		common.OptionMapRWMutex.Unlock()
	})

	for _, strategy := range []string{ImageSourcePoolFirst, ImageSourceAPIFirst, ImageSourcePoolOnly, ImageSourceAPIOnly} {
		require.NoError(t, func() error {
			_, err := NormalizeImageSourceStrategy(strategy)
			return err
		}())
	}
	_, err = NormalizeImageSourceStrategy("legacy_fallback")
	require.ErrorIs(t, err, ErrImageSourcePolicyInvalid)

	saved, err := SaveImageSourcePolicy(ImageSourcePolicy{Model: "gpt-image-2", Strategy: ImageSourcePoolFirst, Enabled: true}, 0)
	require.NoError(t, err)
	require.Equal(t, 1, saved.Version)
	require.True(t, saved.AllowsPool())
	require.True(t, saved.AllowsAPI())
	require.True(t, saved.PoolFirst())

	// A stale expected version loses the CAS.
	_, err = SaveImageSourcePolicy(ImageSourcePolicy{Model: "gpt-image-2", Strategy: ImageSourceAPIOnly, Enabled: true}, 0)
	require.ErrorIs(t, err, ErrPoolConflict)

	updated, err := SaveImageSourcePolicy(ImageSourcePolicy{Model: "gpt-image-2", Strategy: ImageSourceAPIOnly, Enabled: true}, 1)
	require.NoError(t, err)
	require.Equal(t, 2, updated.Version)
	require.False(t, updated.AllowsPool())
	require.True(t, updated.AllowsAPI())
	require.False(t, updated.PoolFirst())

	// A paused policy allows neither source.
	paused, err := SaveImageSourcePolicy(ImageSourcePolicy{Model: "gpt-image-2", Strategy: ImageSourcePoolFirst, Enabled: false}, 2)
	require.NoError(t, err)
	require.False(t, paused.AllowsPool())
	require.False(t, paused.AllowsAPI())

	stored, err := GetImageSourcePolicy("gpt-image-2")
	require.NoError(t, err)
	require.NotNil(t, stored)
	require.False(t, stored.Enabled)

	// A missing model has no policy, which keeps legacy routing.
	missing, err := GetImageSourcePolicy("gpt-unconfigured")
	require.NoError(t, err)
	require.Nil(t, missing)

	// Corrupt stored state fails closed instead of silently allowing.
	common.OptionMapRWMutex.Lock()
	common.OptionMap[ImageSourcePolicyOptionKey("gpt-image-2")] = "{not json"
	common.OptionMapRWMutex.Unlock()
	_, err = GetImageSourcePolicy("gpt-image-2")
	require.ErrorIs(t, err, ErrImageSourcePolicyInvalid)

	list, err := ListImageSourcePolicies()
	if err == nil {
		for _, policy := range list {
			require.NotEqual(t, "gpt-image-2", policy.Model)
		}
	}
}
