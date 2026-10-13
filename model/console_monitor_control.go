package model

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrMonitorInvalid     = errors.New("MONITOR_INVALID")
	ErrMonitorNotReady    = errors.New("MONITOR_NOT_INITIALIZED")
	ErrMonitorDenied      = errors.New("MONITOR_TARGET_DENIED")
	ErrMonitorPaused      = errors.New("MONITOR_PAUSED")
	ErrMonitorBudget      = errors.New("MONITOR_BUDGET_OR_CONCURRENCY")
	ErrMonitorIntegration = errors.New("MONITOR_INTEGRATION_PENDING")
	ErrMonitorLease       = errors.New("MONITOR_LEASE_UNRESOLVED")
)

// The policy row serializes admissions across processes. No site option enables
// execution implicitly. A fresh migration creates a paused policy.
type ConsoleMonitorPolicy struct {
	ID              int   `gorm:"primaryKey;autoIncrement:false" json:"id"`
	SchemaVersion   int   `json:"schema_version"`
	Enabled         bool  `json:"enabled"`
	MaxConcurrent   int64 `json:"max_concurrent"`
	DailyRequests   int64 `json:"daily_requests"`
	DailyCostMicros int64 `json:"daily_cost_micros"`
	RetentionDays   int   `json:"retention_days"`
}

// Every attempt is reserved before transport, including timeouts and crashes.
// Reserved cost is not refunded: unknown upstream outcomes remain bounded.
type ConsoleMonitorAttempt struct {
	ID                 int64 `gorm:"primaryKey"`
	MonitorID          int   `gorm:"index"`
	StartedAt          int64 `gorm:"index"`
	LeaseUntil         int64 `gorm:"index"`
	FinishedAt         int64
	ReservedCostMicros int64
	Failure            bool
	CleanupState       string `gorm:"size:32;index"`
	CleanupStartedAt   int64
	RecoveryReason     string `gorm:"size:512"`
	RecoveredAt        int64
}

// Opt-in only. Does not call InitDB or any host startup migration.
func MigrateConsoleMonitoringDB(db *gorm.DB) error {
	if db == nil {
		return ErrMonitorInvalid
	}
	if db.Migrator().HasTable(&ConsoleMonitorPolicy{}) && db.Migrator().HasColumn(&ConsoleMonitorPolicy{}, "schema_version") {
		var policy ConsoleMonitorPolicy
		if err := db.First(&policy, 1).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if policy.SchemaVersion > 1 {
			return ErrMonitorNotReady
		}
	}
	if err := db.AutoMigrate(&ConsoleMonitor{}, &ConsoleProbe{}, &ConsoleMonitorPolicy{}, &ConsoleMonitorAttempt{}); err != nil {
		return err
	}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&ConsoleMonitorPolicy{ID: 1, SchemaVersion: 1, MaxConcurrent: 1, RetentionDays: 90}).Error; err != nil {
		return err
	}
	if err := db.Model(&ConsoleMonitorAttempt{}).Where("finished_at = 0 AND cleanup_state = ?", "").Update("cleanup_state", "cleanup_pending").Error; err != nil {
		return err
	}
	if err := db.Model(&ConsoleMonitorAttempt{}).Where("finished_at <> 0 AND cleanup_state = ?", "").Update("cleanup_state", "released").Error; err != nil {
		return err
	}
	return db.Model(&ConsoleMonitorPolicy{}).Where("id = 1 AND schema_version = 0").Update("schema_version", 1).Error
}

func ConsoleMonitoringReady(db *gorm.DB) bool {
	if db == nil || !db.Migrator().HasTable(&ConsoleMonitor{}) || !db.Migrator().HasTable(&ConsoleProbe{}) || !db.Migrator().HasTable(&ConsoleMonitorPolicy{}) || !db.Migrator().HasTable(&ConsoleMonitorAttempt{}) || !db.Migrator().HasColumn(&ConsoleMonitorPolicy{}, "schema_version") {
		return false
	}
	var policy ConsoleMonitorPolicy
	return db.First(&policy, 1).Error == nil && policy.SchemaVersion == 1
}

// ValidateMonitorTarget reads only non-secret target configuration. Group route
// execution remains unavailable until a side-effect-free routing adapter exists.
func ValidateMonitorTarget(db *gorm.DB, m *ConsoleMonitor) error {
	if m == nil || strings.TrimSpace(m.Model) == "" || len(m.Model) > 255 || strings.ContainsRune(m.Model, '\x00') || strings.ContainsRune(m.TargetRef, '\x00') || strings.TrimSpace(m.DisplayName) == "" || len(m.DisplayName) > 255 || len(m.Platform) > 255 {
		return ErrMonitorInvalid
	}
	if m.Protocol != "openai_chat" && m.Protocol != "openai_responses" {
		return ErrMonitorInvalid
	}
	if m.IntervalSeconds < 60 || m.IntervalSeconds > 86400 || m.TimeoutSeconds < 1 || m.TimeoutSeconds > 120 || m.StaleSeconds < m.IntervalSeconds || m.StaleSeconds > 604800 || m.DailyRequests < 0 || m.DailyRequests > 1440 || m.RequestCostMicros < 0 || m.RequestCostMicros > 1000000000 {
		return ErrMonitorInvalid
	}
	switch m.TargetKind {
	case "channel":
		id, err := strconv.Atoi(m.TargetRef)
		if err != nil || id <= 0 || strconv.Itoa(id) != m.TargetRef || m.ChannelID != id {
			return ErrMonitorInvalid
		}
		var c Channel
		if err := db.First(&c, id).Error; err != nil {
			return ErrMonitorDenied
		}
		// Never publish or probe a disabled or pool-backed channel through this path.
		if c.Status != common.ChannelStatusEnabled || c.PoolChannelBound() || !slices.Contains(c.GetModels(), m.Model) {
			return ErrMonitorDenied
		}
	case "group_route":
		if m.ChannelID != 0 || m.TargetRef == "" || len(m.TargetRef) > 64 {
			return ErrMonitorInvalid
		}
		var count int64
		if err := db.Model(&Ability{}).Where(map[string]any{"group": m.TargetRef, "model": m.Model, "enabled": true}).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return ErrMonitorDenied
		}
	case "account_pool_group":
		if m.ChannelID != 0 || strings.TrimSpace(m.TargetRef) == "" || len(m.TargetRef) > 255 {
			return ErrMonitorInvalid
		}
		// An offline catalog entry grants no admission or publication. Only the
		// future frozen pool contract can authorize execution of this identity.
		if m.Enabled || m.Published {
			return ErrMonitorIntegration
		}
	default:
		return ErrMonitorInvalid
	}
	if m.Enabled && (m.DailyRequests == 0 || m.RequestCostMicros == 0) {
		return ErrMonitorBudget
	}
	digest := sha256.Sum256([]byte(m.TargetKind + "\x00" + m.TargetRef + "\x00" + m.Model + "\x00" + m.Protocol))
	identity := fmt.Sprintf("%x", digest)
	m.Identity = &identity
	return nil
}

// SaveConsoleMonitor changes configuration only. Publishing and enabling are
// deliberately separate operations; repeating a create preserves its settings.
func SaveConsoleMonitor(ctx context.Context, db *gorm.DB, m *ConsoleMonitor) error {
	if !ConsoleMonitoringReady(db) {
		return ErrMonitorNotReady
	}
	if m == nil || m.Published || m.Enabled || m.Archived {
		return ErrMonitorInvalid
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var policy ConsoleMonitorPolicy
		if err := lockForUpdate(tx).First(&policy, 1).Error; err != nil {
			return err
		}
		if err := ValidateMonitorTarget(tx, m); err != nil {
			return err
		}
		var existing ConsoleMonitor
		err := tx.Where("identity = ?", *m.Identity).First(&existing).Error
		if err == nil {
			if m.ID != 0 && m.ID != existing.ID {
				return ErrMonitorInvalid
			}
			m.ID = existing.ID
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if m.ID != 0 {
			return ErrMonitorInvalid
		}
		if m.TargetKind == "channel" {
			var legacy []ConsoleMonitor
			if err := tx.Where("identity IS NULL AND channel_id = ? AND model = ?", m.ChannelID, m.Model).Find(&legacy).Error; err != nil {
				return err
			}
			if len(legacy) > 1 {
				return ErrMonitorInvalid
			}
			if len(legacy) == 1 {
				m.ID = legacy[0].ID
				return tx.Model(&legacy[0]).Updates(map[string]any{"identity": *m.Identity, "target_kind": m.TargetKind, "target_ref": m.TargetRef, "protocol": m.Protocol, "interval_seconds": m.IntervalSeconds, "timeout_seconds": m.TimeoutSeconds, "stale_seconds": m.StaleSeconds, "daily_requests": m.DailyRequests, "request_cost_micros": m.RequestCostMicros}).Error
			}
		}
		return tx.Create(m).Error
	})
}

// Configuration edits cannot change identity, publication, or execution gates.
func UpdateConsoleMonitor(ctx context.Context, db *gorm.DB, m *ConsoleMonitor) error {
	if !ConsoleMonitoringReady(db) {
		return ErrMonitorNotReady
	}
	if m == nil || m.ID <= 0 {
		return ErrMonitorInvalid
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var policy ConsoleMonitorPolicy
		if err := lockForUpdate(tx).First(&policy, 1).Error; err != nil {
			return err
		}
		var current ConsoleMonitor
		if err := tx.First(&current, m.ID).Error; err != nil {
			return err
		}
		if current.Identity == nil {
			return ErrMonitorDenied
		}
		m.Enabled = current.Enabled
		if err := ValidateMonitorTarget(tx, m); err != nil {
			return err
		}
		if *m.Identity != *current.Identity {
			return ErrMonitorDenied
		}
		return tx.Model(&current).Updates(map[string]any{"display_name": m.DisplayName, "platform": m.Platform, "interval_seconds": m.IntervalSeconds, "timeout_seconds": m.TimeoutSeconds, "stale_seconds": m.StaleSeconds, "daily_requests": m.DailyRequests, "request_cost_micros": m.RequestCostMicros}).Error
	})
}

func SaveConsoleMonitorPolicy(ctx context.Context, db *gorm.DB, p ConsoleMonitorPolicy) error {
	if !ConsoleMonitoringReady(db) {
		return ErrMonitorNotReady
	}
	if p.ID != 1 || p.MaxConcurrent < 1 || p.MaxConcurrent > 32 || p.DailyRequests < 0 || p.DailyRequests > 100000 || p.DailyCostMicros < 0 || p.DailyCostMicros > 1000000000000 || p.RetentionDays < 30 || p.RetentionDays > 365 {
		return ErrMonitorInvalid
	}
	if p.Enabled && (p.DailyRequests == 0 || p.DailyCostMicros == 0) {
		return ErrMonitorBudget
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing ConsoleMonitorPolicy
		if err := lockForUpdate(tx).First(&existing, 1).Error; err != nil {
			return err
		}
		return tx.Model(&existing).Updates(map[string]any{"enabled": p.Enabled, "max_concurrent": p.MaxConcurrent, "daily_requests": p.DailyRequests, "daily_cost_micros": p.DailyCostMicros, "retention_days": p.RetentionDays}).Error
	})
}

func SetConsoleMonitorSwitch(ctx context.Context, db *gorm.DB, id int, action string, value bool) error {
	if !ConsoleMonitoringReady(db) {
		return ErrMonitorNotReady
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var policy ConsoleMonitorPolicy
		if err := lockForUpdate(tx).First(&policy, 1).Error; err != nil {
			return err
		}
		var m ConsoleMonitor
		if err := tx.First(&m, id).Error; err != nil {
			return err
		}
		if action != "published" && action != "enabled" && action != "archived" {
			return ErrMonitorInvalid
		}
		if value && action != "archived" {
			if m.Archived {
				return ErrMonitorDenied
			}
			if action == "enabled" {
				m.Enabled = true
			}
			if action == "published" {
				m.Published = true
			}
			if err := ValidateMonitorTarget(tx, &m); err != nil {
				return err
			}
		}
		updates := map[string]any{action: value}
		if action == "archived" && value {
			updates["published"] = false
			updates["enabled"] = false
		}
		return tx.Model(&m).Updates(updates).Error
	})
}

// Admission checks manual and scheduled runs identically. The callback is not
// invoked here; policy locks are never held during network operations.
func ReserveConsoleMonitorAttempt(ctx context.Context, db *gorm.DB, id int, now time.Time) (ConsoleMonitor, ConsoleMonitorAttempt, error) {
	var m ConsoleMonitor
	var attempt ConsoleMonitorAttempt
	if !ConsoleMonitoringReady(db) {
		return m, attempt, ErrMonitorNotReady
	}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var p ConsoleMonitorPolicy
		if err := lockForUpdate(tx).First(&p, 1).Error; err != nil {
			return err
		}
		if !p.Enabled {
			return ErrMonitorPaused
		}
		if err := tx.First(&m, id).Error; err != nil {
			return err
		}
		if !m.Enabled || m.Archived {
			return ErrMonitorPaused
		}
		if err := ValidateMonitorTarget(tx, &m); err != nil {
			return err
		}
		if p.MaxConcurrent < 1 || p.MaxConcurrent > 32 || p.DailyRequests < 1 || p.DailyRequests > 100000 || p.DailyCostMicros < 1 || p.DailyCostMicros > 1000000000000 {
			return ErrMonitorBudget
		}
		var active int64
		// Expiry alone does not prove an upstream request has stopped. Unknown
		// crash outcomes retain their concurrency slot until reconciliation.
		if err := tx.Model(&ConsoleMonitorAttempt{}).Where("finished_at = 0").Count(&active).Error; err != nil {
			return err
		}
		if active >= p.MaxConcurrent {
			return ErrMonitorBudget
		}
		var latest ConsoleMonitorAttempt
		err := tx.Where("monitor_id = ?", id).Order("id DESC").First(&latest).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		wait := m.IntervalSeconds
		if latest.Failure {
			wait = min(wait*2, int64(86400))
		}
		if latest.ID != 0 && (latest.FinishedAt == 0 || latest.StartedAt+wait > now.Unix()) {
			return ErrMonitorBudget
		}
		// Rolling 24-hour limits avoid timezone/reset-boundary bursts.
		var totals struct {
			Requests int64
			Cost     int64
		}
		if err := tx.Model(&ConsoleMonitorAttempt{}).Select("count(*) AS requests, coalesce(sum(reserved_cost_micros),0) AS cost").Where("started_at > ?", now.Add(-24*time.Hour).Unix()).Scan(&totals).Error; err != nil {
			return err
		}
		if totals.Requests >= p.DailyRequests || m.RequestCostMicros > p.DailyCostMicros-totals.Cost {
			return ErrMonitorBudget
		}
		var targetCount int64
		if err := tx.Model(&ConsoleMonitorAttempt{}).Where("monitor_id = ? AND started_at > ?", id, now.Add(-24*time.Hour).Unix()).Count(&targetCount).Error; err != nil {
			return err
		}
		if targetCount >= m.DailyRequests {
			return ErrMonitorBudget
		}
		attempt = ConsoleMonitorAttempt{MonitorID: id, StartedAt: now.Unix(), LeaseUntil: now.Unix() + m.TimeoutSeconds + 30, ReservedCostMicros: m.RequestCostMicros, CleanupState: "active"}
		return tx.Create(&attempt).Error
	})
	return m, attempt, err
}

// RecoverConsoleMonitorAttempt is an explicit operator recovery action.
// It never releases a lease or frees capacity; it only reopens a reconciliation
// claim after a process restart or a documented manual review.
func RecoverConsoleMonitorAttempt(ctx context.Context, db *gorm.DB, attemptID int64, reason string, now time.Time) error {
	if !ConsoleMonitoringReady(db) || attemptID <= 0 || strings.TrimSpace(reason) == "" || len(reason) > 512 {
		return ErrMonitorInvalid
	}
	reason = strings.TrimSpace(reason)
	result := db.WithContext(ctx).Model(&ConsoleMonitorAttempt{}).Where("id = ? AND finished_at = 0 AND cleanup_state IN ?", attemptID, []string{"active", "cleanup_reconciling"}).Updates(map[string]any{"cleanup_state": "cleanup_pending", "recovery_reason": reason, "recovered_at": now.Unix()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrMonitorLease
	}
	return nil
}

// PruneConsoleMonitoringHistory removes only data past the configured retention
// window. Unfinished attempts are never deleted: an expired lease is not proof
// that an upstream request stopped, so its reservation remains reconcilable.
func PruneConsoleMonitoringHistory(ctx context.Context, db *gorm.DB, now time.Time) error {
	if !ConsoleMonitoringReady(db) {
		return ErrMonitorNotReady
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var policy ConsoleMonitorPolicy
		if err := lockForUpdate(tx).First(&policy, 1).Error; err != nil {
			return err
		}
		if policy.RetentionDays < 30 || policy.RetentionDays > 365 {
			return ErrMonitorInvalid
		}
		cutoff := now.Add(-time.Duration(policy.RetentionDays) * 24 * time.Hour).Unix()
		if err := tx.Where("checked_at < ?", cutoff).Delete(&ConsoleProbe{}).Error; err != nil {
			return err
		}
		return tx.Where("finished_at > ? AND started_at < ?", 0, cutoff).Delete(&ConsoleMonitorAttempt{}).Error
	})
}
