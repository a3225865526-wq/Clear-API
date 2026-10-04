package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Site-owner upstream account pool. Credentials themselves never live in this
// database: rows hold references into an executor's own credential store plus
// management status. Admin status (owner's pause switch) and run status
// (executor/admission state) are modeled as separate columns so background
// refresh or re-auth can never overwrite an owner pause.

const (
	PoolAdminStatusEnabled = "enabled"
	PoolAdminStatusPaused  = "paused"
)

const (
	PoolRunStatusAwaitingAuth    = "awaiting_auth"    // registered, no credential yet
	PoolRunStatusAuthorizing     = "authorizing"      // an auth task is pending
	PoolRunStatusAvailable       = "available"        // admitted and schedulable (phase 3)
	PoolRunStatusReauthRequired  = "reauth_required"  // executor reports the credential invalid
	PoolRunStatusCooldown        = "cooldown"         // executor reports a temporary upstream limit
	PoolRunStatusExecutorOffline = "executor_offline" // executor unreachable / not provisioned
	PoolRunStatusNotAdmitted     = "not_admitted"     // credential valid but capability admission not passed
)

const (
	PoolTaskStatusPending   = "pending"
	PoolTaskStatusCompleted = "completed"
	PoolTaskStatusExpired   = "expired"
	PoolTaskStatusCancelled = "cancelled"
	PoolTaskStatusFailed    = "failed"
)

const (
	PoolTaskFailExpired           = "expired"
	PoolTaskFailIdentityMismatch  = "identity_mismatch"
	PoolTaskFailCredentialUsed    = "credential_ref_taken"
	PoolTaskFailExecutorError     = "executor_error"
	PoolTaskFailPlatformMismatch  = "platform_mismatch"
	PoolTaskFailConcurrentUpdates = "concurrent_update"
	PoolTaskFailCorrelation       = "correlation_failed"
)

var (
	ErrPoolInvalid           = errors.New("pool invalid input")
	ErrPoolNotFound          = errors.New("pool record not found")
	ErrPoolConflict          = errors.New("pool state conflict")
	ErrPoolUnsupported       = errors.New("pool operation not supported by this adapter")
	ErrPoolExecutorOffline   = errors.New("pool executor unreachable or not provisioned")
	ErrPoolExecutorFailed    = errors.New("pool executor rejected the operation")
	ErrPoolUnavailable       = errors.New("pool storage not migrated")
	ErrPoolTaskExpired       = errors.New("pool auth task expired")
	ErrPoolTaskConsumed      = errors.New("pool auth task already settled")
	ErrPoolIdentityMismatch  = errors.New("pool upstream identity mismatch")
	ErrPoolNotAdmitted       = errors.New("pool account not admitted for scheduling")
	ErrPoolIsolationRequired = errors.New("pool executor isolation not attested")
	ErrPoolCorrelationFailed = errors.New("pool credential correlation failed")
	ErrPoolAuthTimeout       = errors.New("pool authorization still pending past deadline")
)

// UpstreamPoolAccount is one upstream subscription account registered by the
// site owner. IdentityMasked and CredentialRef are intentionally non-secret
// projections: a masked identity for display and the executor-side reference
// of the credential the executor manages.
type UpstreamPoolAccount struct {
	Id             int    `json:"id" gorm:"primaryKey"`
	Platform       string `json:"platform" gorm:"type:varchar(32);index:idx_pool_platform_type,priority:1"`
	AccessType     string `json:"access_type" gorm:"type:varchar(32);index:idx_pool_platform_type,priority:2"`
	IdentityMasked string `json:"identity_masked" gorm:"type:varchar(128)"`
	// IdentityFingerprint is a server-side SHA-256 of the normalized stable
	// upstream identity. It — never the display-only masked value — decides
	// whether a re-auth belongs to the same upstream account.
	IdentityFingerprint string `json:"-" gorm:"type:varchar(64);index"`
	Note                string `json:"note" gorm:"type:varchar(255)"`
	AdminStatus         string `json:"admin_status" gorm:"type:varchar(16);index"`
	RunStatus           string `json:"run_status" gorm:"type:varchar(24);index"`
	ExecutorId          string `json:"executor_id" gorm:"type:varchar(64)"`
	CredentialRef       string `json:"credential_ref" gorm:"type:varchar(191);index"`
	LastRefreshAt       int64  `json:"last_refresh_at"`
	LastError           string `json:"last_error" gorm:"type:varchar(255)"`
	Version             int    `json:"version" gorm:"default:1"`
	CreatedAt           int64  `json:"created_at" gorm:"index"`
	UpdatedAt           int64  `json:"updated_at"`
}

// UpstreamPoolAuthTask tracks one authorization round. It must never carry
// secret fields: no authorization code, callback URL, token, cookie or PKCE
// verifier is persisted here. ExecutorStateRef is an opaque correlation id
// that is useless without the executor's own management key.
type UpstreamPoolAuthTask struct {
	Id               int    `json:"id" gorm:"primaryKey"`
	AccountId        int    `json:"account_id" gorm:"index:idx_pool_task_account,priority:1"`
	OwnerUserId      int    `json:"owner_user_id" gorm:"index"`
	Platform         string `json:"platform" gorm:"type:varchar(32)"`
	AccessType       string `json:"access_type" gorm:"type:varchar(32)"`
	Kind             string `json:"kind" gorm:"type:varchar(16)"`
	Method           string `json:"method" gorm:"type:varchar(16)"`
	ExecutorId       string `json:"executor_id" gorm:"type:varchar(64)"`
	ExecutorStateRef string `json:"executor_state_ref" gorm:"type:varchar(191);index:idx_pool_task_account,priority:2"`
	// BaselineRefs is the JSON name→updated_at snapshot of the executor
	// credential listing taken at task start. The completion-time diff against
	// this snapshot is the only correlation evidence (the executor protocol
	// has no state→file field); it is metadata, not a secret.
	BaselineRefs  string `json:"baseline_refs" gorm:"type:text"`
	Status        string `json:"status" gorm:"type:varchar(16);index"`
	FailReason    string `json:"fail_reason" gorm:"type:varchar(64)"`
	PrevRunStatus string `json:"prev_run_status" gorm:"type:varchar(24)"`
	ExpiresAt     int64  `json:"expires_at" gorm:"index"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
}

func MigrateUpstreamPool(db *gorm.DB) error {
	return db.AutoMigrate(&UpstreamPoolAccount{}, &UpstreamPoolAuthTask{}, &UpstreamPoolAuthClaim{}, &UpstreamPoolQuotaSnapshot{})
}

func UpstreamPoolReady(db *gorm.DB) error {
	if db == nil {
		return ErrPoolUnavailable
	}
	if !db.Migrator().HasTable(&UpstreamPoolAccount{}) || !db.Migrator().HasTable(&UpstreamPoolAuthTask{}) || !db.Migrator().HasTable(&UpstreamPoolAuthClaim{}) || !db.Migrator().HasTable(&UpstreamPoolQuotaSnapshot{}) {
		return ErrPoolUnavailable
	}
	return nil
}

// UpstreamPoolQuotaSnapshot holds only bounded, non-secret quota observations.
// It is independent of account version updates and never changes scheduling.
type UpstreamPoolQuotaSnapshot struct {
	AccountId     int    `gorm:"primaryKey"`
	CredentialRef string `gorm:"type:varchar(191)"`
	Payload       string `gorm:"type:text"`
}

// UpstreamPoolAuthClaim is the atomic per-executor authorization claim. The
// primary key on executor_id makes acquisition an insert race across
// processes: only one pending authorization may exist per executor instance
// because completion correlates the new credential by diffing the executor
// listing against the task baseline. ExpiresAt describes the local task TTL,
// not proof of remote termination; unresolved claims remain held past it.
type UpstreamPoolAuthClaim struct {
	ExecutorId string `json:"executor_id" gorm:"primaryKey;type:varchar(64)"`
	AccountId  int    `json:"account_id" gorm:"index"`
	TaskId     int    `json:"task_id"`
	ExpiresAt  int64  `json:"expires_at" gorm:"index"`
	CreatedAt  int64  `json:"created_at"`
}

// AcquireUpstreamPoolClaim claims the executor for one authorization round.
// Returns ErrPoolConflict while any claim exists, including expired claims.
// released is retained for caller compatibility and is always false.
func AcquireUpstreamPoolClaim(db *gorm.DB, executorId string, accountID, taskID int, expiresAt int64) (released bool, err error) {
	if db == nil {
		return false, ErrPoolUnavailable
	}
	claim := &UpstreamPoolAuthClaim{ExecutorId: executorId, AccountId: accountID, TaskId: taskID, ExpiresAt: expiresAt, CreatedAt: time.Now().Unix()}
	// A local TTL cannot prove that a remote authorization worker stopped.
	// Do not reclaim on expiry or on a terminal local task status. DO NOTHING
	// also avoids aborting a PostgreSQL transaction on a uniqueness conflict.
	insert := db.Clauses(clause.OnConflict{DoNothing: true}).Create(claim)
	if insert.Error != nil {
		return false, insert.Error
	}
	if insert.RowsAffected == 0 {
		return false, ErrPoolConflict
	}
	// MySQL clientFoundRows can report one affected row for a no-op duplicate
	// update. Verify ownership instead of trusting that count as acquisition.
	var stored UpstreamPoolAuthClaim
	if err := db.Where("executor_id = ?", executorId).First(&stored).Error; err != nil {
		return false, err
	}
	if stored.TaskId != taskID || stored.AccountId != accountID {
		return false, ErrPoolConflict
	}
	return false, nil
}

// ReleaseUpstreamPoolClaim drops the claim when it belongs to the given task.
func ReleaseUpstreamPoolClaim(db *gorm.DB, executorId string, taskID int) error {
	if db == nil {
		return ErrPoolUnavailable
	}
	return db.Where("executor_id = ? AND task_id = ?", executorId, taskID).Delete(&UpstreamPoolAuthClaim{}).Error
}

// SweepUpstreamPoolClaims intentionally preserves unresolved reservations.
// Local task settlement or absence cannot establish remote termination.
func SweepUpstreamPoolClaims(db *gorm.DB) error {
	if db == nil {
		return ErrPoolUnavailable
	}
	// Unknown remote sessions require reconciliation, never automatic deletion.
	return nil
}

// UpdateUpstreamPoolAccount applies a column update guarded by the row
// version. RowsAffected == 0 means either a missing row or a concurrent
// writer won; callers translate that into ErrPoolConflict so a stale writer
// (for example a background sync racing the owner's pause) can never silently
// overwrite the winner.
func UpdateUpstreamPoolAccount(db *gorm.DB, id int, expectedVersion int, columns map[string]any) error {
	if db == nil {
		return ErrPoolUnavailable
	}
	columns["version"] = expectedVersion + 1
	columns["updated_at"] = time.Now().Unix()
	result := db.Model(&UpstreamPoolAccount{}).
		Where("id = ? AND version = ?", id, expectedVersion).
		Updates(columns)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var count int64
		if err := db.Model(&UpstreamPoolAccount{}).Where("id = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return ErrPoolNotFound
		}
		return ErrPoolConflict
	}
	return nil
}
