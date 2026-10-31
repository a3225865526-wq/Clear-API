package service

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

// A read-only configuration diagnosis. Admission still checks budgets, rate
// limits and leases transactionally immediately before every probe.
type ConsoleMonitorReadiness struct {
	ChannelID          int      `json:"channel_id"`
	MonitorID          int      `json:"monitor_id"`
	Allowlisted        bool     `json:"allowlisted"`
	ConfigurationReady bool     `json:"configuration_ready"`
	Blockers           []string `json:"blockers"`
}

func DiagnoseConsoleMonitor(ctx context.Context, db *gorm.DB, m model.ConsoleMonitor, policy model.ConsoleMonitorPolicy) ConsoleMonitorReadiness {
	r := ConsoleMonitorReadiness{ChannelID: m.ChannelID, MonitorID: m.ID, Allowlisted: m.Allowlisted, Blockers: []string{}}
	if !m.Allowlisted {
		r.Blockers = append(r.Blockers, "MONITOR_NOT_ALLOWLISTED")
	}
	if !m.Published {
		r.Blockers = append(r.Blockers, "MONITOR_NOT_PUBLISHED")
	}
	if !m.Enabled {
		r.Blockers = append(r.Blockers, "MONITOR_DISABLED")
	}
	if m.Archived {
		r.Blockers = append(r.Blockers, "MONITOR_ARCHIVED")
	}
	if !policy.Enabled {
		r.Blockers = append(r.Blockers, "MONITOR_POLICY_DISABLED")
	}
	if policy.MaxConcurrent < 1 || policy.DailyRequests < 1 || policy.DailyCostMicros < 1 || m.DailyRequests < 1 || m.RequestCostMicros < 1 {
		r.Blockers = append(r.Blockers, "MONITOR_BUDGET_UNCONFIGURED")
	}
	if m.TargetKind != "channel" {
		r.Blockers = append(r.Blockers, "MONITOR_INTEGRATION_PENDING")
	} else {
		var channel model.Channel
		if err := db.WithContext(ctx).Select("id", "status").First(&channel, m.ChannelID).Error; err != nil || channel.Status != common.ChannelStatusEnabled {
			r.Blockers = append(r.Blockers, "MONITOR_TARGET_DENIED")
		}
		// Uses the same current-channel endpoint, credential binding, protocol,
		// expiry and worst-case cost checks as real dispatch, without networking.
		if err := AuthorizeConsoleProbeCost(ctx, db, m); err != nil {
			code := "MONITOR_COST_UNVERIFIED"
			if errors.Is(err, ErrMonitorCostExceeded) {
				code = "MONITOR_COST_EXCEEDED"
			}
			if errors.Is(err, model.ErrMonitorInvalid) {
				code = "MONITOR_INVALID"
			}
			if errors.Is(err, model.ErrMonitorDenied) {
				code = "MONITOR_TARGET_DENIED"
			}
			r.Blockers = append(r.Blockers, code)
		}
	}
	r.ConfigurationReady = len(r.Blockers) == 0
	return r
}
