package model

import (
	"context"

	"gorm.io/gorm"
)

// Allowlist management requires the explicit monitoring migration, including
// its additive administration columns. A read request never initializes them.
func ConsoleMonitorAllowlistReady(db *gorm.DB) bool {
	return ConsoleMonitoringReady(db) &&
		db.Migrator().HasColumn(&ConsoleMonitor{}, "allowlisted") &&
		db.Migrator().HasColumn(&ConsoleMonitor{}, "created_by") &&
		db.Migrator().HasColumn(&ConsoleMonitor{}, "created_at")
}

// Preserve execution controls under the same policy lock used by configuration
// writers. The allowlist API cannot overwrite concurrently updated cost fields.
func UpdateConsoleMonitorAllowlist(ctx context.Context, db *gorm.DB, m *ConsoleMonitor) error {
	if !ConsoleMonitorAllowlistReady(db) {
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
		m.DailyRequests, m.RequestCostMicros = current.DailyRequests, current.RequestCostMicros
		m.Published, m.Archived = current.Published, current.Archived
		return UpdateConsoleMonitor(ctx, tx, m)
	})
}

// Membership does not change Enabled, the policy, attempts, or provider state.
func SetConsoleMonitorAllowlisted(ctx context.Context, db *gorm.DB, id int, value bool) error {
	if !ConsoleMonitorAllowlistReady(db) {
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
		if value {
			if m.Archived {
				return ErrMonitorDenied
			}
			if m.TargetKind == "account_pool_group" {
				return ErrMonitorIntegration
			}
			if err := ValidateMonitorTarget(tx, &m); err != nil {
				return err
			}
		}
		return tx.Model(&m).Update("allowlisted", value).Error
	})
}
