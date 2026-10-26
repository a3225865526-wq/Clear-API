package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

// A transport must implement authorization, real route attribution and a hard
// per-request spending ceiling. It may not call channel_test or ordinary user
// settlement. No real adapter is registered without those guarantees.
type ConsoleProbeAdapter interface {
	Authorize(context.Context, model.ConsoleMonitor) error
	Probe(context.Context, model.ConsoleMonitor, int64) (ConsoleProbeOutcome, error)
}

type ConsoleProbeLeaseReconciler interface {
	Reconcile(context.Context, model.ConsoleMonitor, int64) (bool, error)
}

var ErrMonitorProbePanic = errors.New("MONITOR_PROBE_PANIC")

type ConsoleProbeOutcome struct {
	Status             string
	ErrorCode          string
	SourceChannelID    int
	CleanupStartedAt   int64
	MeteredCostMicros  *int64
	UpstreamResponseID string
}

type ConsoleProbeResult struct {
	AttemptID          int64  `json:"attempt_id"`
	ProbeID            int64  `json:"probe_id"`
	Status             string `json:"status"`
	ErrorCode          string `json:"error_code"`
	MeteredCostMicros  *int64 `json:"metered_cost_micros,omitempty"`
	UpstreamResponseID string `json:"upstream_response_id,omitempty"`
}

type ConsoleProbeExecutor struct {
	DB       *gorm.DB
	TaskID   string
	Adapters map[string]ConsoleProbeAdapter
}

// RunScheduled performs an interval scheduling pass without a startup goroutine.
// All workers/processes use the same database admission gate as manual runs.
func (e *ConsoleProbeExecutor) RunScheduled(ctx context.Context) error {
	if !model.ConsoleMonitoringReady(e.DB) {
		return model.ErrMonitorNotReady
	}
	var policy model.ConsoleMonitorPolicy
	if err := e.DB.WithContext(ctx).First(&policy, 1).Error; err != nil {
		return err
	}
	if !policy.Enabled {
		return model.ErrMonitorPaused
	}
	var monitors []model.ConsoleMonitor
	if err := e.DB.WithContext(ctx).Where("enabled = ? AND archived = ?", true, false).Order("id").Find(&monitors).Error; err != nil {
		return err
	}
	for _, m := range monitors {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := e.Run(ctx, m.ID)
		if err != nil && !errors.Is(err, model.ErrMonitorPaused) && !errors.Is(err, model.ErrMonitorBudget) && !errors.Is(err, model.ErrMonitorDenied) && !errors.Is(err, model.ErrMonitorIntegration) && !errors.Is(err, ErrMonitorLeaseUnresolved) {
			return err
		}
	}
	return model.PruneConsoleMonitoringHistory(ctx, e.DB, time.Now())
}

func (e *ConsoleProbeExecutor) Run(ctx context.Context, id int) error {
	_, err := e.RunWithResult(ctx, id)
	return err
}

func (e *ConsoleProbeExecutor) RunWithResult(ctx context.Context, id int) (result ConsoleProbeResult, err error) {
	if !model.ConsoleMonitoringReady(e.DB) {
		return result, model.ErrMonitorNotReady
	}
	defer func() {
		// Admission denials are real observations of a task that did not dispatch.
		// Never report them as successful probes or consume a paid attempt.
		if err == nil || result.ProbeID != 0 {
			return
		}
		status, code := "blocked", "MONITOR_TARGET_DENIED"
		switch {
		case errors.Is(err, model.ErrMonitorPaused):
			status, code = "skipped", "MONITOR_PAUSED"
		case errors.Is(err, model.ErrMonitorBudget):
			code = "MONITOR_BUDGET_OR_CONCURRENCY"
		case errors.Is(err, model.ErrMonitorIntegration):
			code = "MONITOR_INTEGRATION_PENDING"
		case errors.Is(err, model.ErrMonitorLease), errors.Is(err, ErrMonitorLeaseUnresolved):
			code = "MONITOR_LEASE_UNRESOLVED"
		case errors.Is(err, model.ErrMonitorDenied):
		default:
			return
		}
		record := model.ConsoleProbe{MonitorID: id, CheckedAt: time.Now().Unix(), Status: status, ErrorCode: code, AttemptID: result.AttemptID, TaskID: e.TaskID}
		persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if persistErr := e.DB.WithContext(persistCtx).Create(&record).Error; persistErr != nil {
			err = errors.Join(err, persistErr)
			return
		}
		result.ProbeID, result.Status, result.ErrorCode = record.ID, status, code
	}()
	var configured model.ConsoleMonitor
	if err := e.DB.WithContext(ctx).First(&configured, id).Error; err != nil {
		return result, err
	}
	// Only the approved dedicated-channel Responses boundary is affected.
	// No real adapter is required or registered to record a cost denial.
	costGated := isConsoleProbeCostScope(configured)
	if costGated && (!configured.Allowlisted || !configured.Published || !configured.Enabled || configured.Archived) {
		return result, model.ErrMonitorPaused
	}
	adapter := e.Adapters[configured.TargetKind]
	if adapter == nil && !costGated {
		return result, model.ErrMonitorIntegration
	}
	if adapter != nil && !costGated {
		if err := runConsoleAuthorizeSafely(ctx, adapter, configured); err != nil {
			return result, err
		}
	}
	m, attempt, err := model.ReserveConsoleMonitorAttempt(ctx, e.DB, id, time.Now())
	if err != nil {
		return result, err
	}
	result.AttemptID = attempt.ID
	// Re-authorize the actual admitted snapshot, not the earlier unlocked read.
	probeCtx, cancel := context.WithTimeout(ctx, time.Duration(m.TimeoutSeconds)*time.Second)
	defer cancel()
	start := time.Now()
	outcome := ConsoleProbeOutcome{Status: "unknown", ErrorCode: "authorization_denied"}
	if costGated || isConsoleProbeCostScope(m) {
		err = AuthorizeConsoleProbeCost(probeCtx, e.DB, m)
	} else if !costGated {
		err = runConsoleAuthorizeSafely(probeCtx, adapter, m)
	}
	if err == nil && adapter != nil {
		outcome, err = runConsoleProbeSafely(probeCtx, adapter, m, attempt.ID)
	}
	if err == nil && probeCtx.Err() != nil {
		err = probeCtx.Err()
	}
	if err == nil && costGated && (outcome.MeteredCostMicros == nil || *outcome.MeteredCostMicros < 0 || *outcome.MeteredCostMicros > attempt.ReservedCostMicros || outcome.UpstreamResponseID == "" || len(outcome.UpstreamResponseID) > 128) {
		err = errors.Join(ErrMonitorMetering, ErrMonitorLeaseUnresolved)
	}
	if err != nil {
		outcome.Status = "error"
		outcome.ErrorCode = consoleProbeFailureCode(err, probeCtx.Err())
		if errors.Is(err, ErrMonitorCostUnverified) || errors.Is(err, ErrMonitorCostExceeded) {
			outcome.Status = "blocked"
		}
	}
	if outcome.Status != "normal" && outcome.Status != "degraded" && outcome.Status != "error" && outcome.Status != "unknown" && outcome.Status != "blocked" {
		outcome.Status = "unknown"
		outcome.ErrorCode = "invalid_result"
	}
	// Never persist arbitrary provider or adapter error strings.
	switch outcome.ErrorCode {
	case "probe_transport_unresolved", "", "probe_failed", "probe_timeout", "probe_incomplete", "authorization_denied", "invalid_result", "MONITOR_COST_UNVERIFIED", "MONITOR_COST_EXCEEDED", "MONITOR_METERING_FAILED":
	default:
		outcome.ErrorCode = "probe_failed"
	}
	if (outcome.Status == "normal" || outcome.Status == "degraded") && (m.TargetKind == "channel" && outcome.SourceChannelID != m.ChannelID || m.TargetKind == "group_route" && outcome.SourceChannelID <= 0) {
		outcome.Status = "unknown"
		outcome.ErrorCode = "invalid_result"
	}
	if (outcome.Status == "normal" || outcome.Status == "degraded") && m.TargetKind == "group_route" {
		// Attribution is one actual route, never an all-channels health claim.
		var channel model.Channel
		var count int64
		if err := e.DB.WithContext(probeCtx).First(&channel, outcome.SourceChannelID).Error; err != nil {
			outcome.Status, outcome.ErrorCode = "unknown", "invalid_result"
		} else if channel.PoolChannelBound() || channel.Status != common.ChannelStatusEnabled || !slices.Contains(channel.GetModels(), m.Model) {
			outcome.Status, outcome.ErrorCode = "unknown", "invalid_result"
		} else if err := e.DB.WithContext(probeCtx).Model(&model.Ability{}).Where(map[string]any{"channel_id": channel.Id, "group": m.TargetRef, "model": m.Model, "enabled": true}).Count(&count).Error; err != nil || count == 0 {
			outcome.Status, outcome.ErrorCode = "unknown", "invalid_result"
		}
	}
	latency := time.Since(start).Milliseconds()
	now := time.Now().Unix()
	// Transport cancellation must not discard accounting for an admitted call.
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer finishCancel()
	if errors.Is(err, ErrMonitorLeaseUnresolved) || errors.Is(err, ErrMonitorProbePanic) {
		// Persist a visible, unresolved attempt and an unknown result, while
		// retaining the active-concurrency reservation for reconciliation.
		updates := map[string]any{"failure": true, "cleanup_state": "cleanup_pending", "cleanup_started_at": outcome.CleanupStartedAt}
		var panicErr error
		if errors.Is(err, ErrMonitorProbePanic) {
			// A recovered panic is not proof that transport stopped. Preserve a
			// durable safe reason and never log the recovered value or credentials.
			updates["recovery_reason"] = "probe_panic"
			panicErr = ErrMonitorProbePanic
			logger.LogError(finishCtx, fmt.Sprintf("monitor probe panic: monitor_id=%d attempt_id=%d; cleanup unconfirmed", id, attempt.ID))
		}
		persistErr := e.DB.WithContext(finishCtx).Transaction(func(tx *gorm.DB) error {
			updated := tx.Model(&model.ConsoleMonitorAttempt{}).Where("id = ? AND finished_at = 0", attempt.ID).Updates(updates)
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return model.ErrMonitorDenied
			}
			code := outcome.ErrorCode
			if code == "" {
				code = "probe_failed"
			}
			probe := model.ConsoleProbe{TaskID: e.TaskID, AttemptID: attempt.ID, MonitorID: id, CheckedAt: now, Status: "unknown", ErrorCode: code, LatencyMS: &latency, SourceKind: m.TargetKind}
			if err := tx.Create(&probe).Error; err != nil {
				return err
			}
			result.ProbeID, result.Status, result.ErrorCode = probe.ID, probe.Status, probe.ErrorCode
			return nil
		})
		if persistErr != nil {
			result = ConsoleProbeResult{AttemptID: attempt.ID}
		}
		return result, errors.Join(ErrMonitorLeaseUnresolved, panicErr, persistErr)
	}
	err = e.DB.WithContext(finishCtx).Transaction(func(tx *gorm.DB) error {
		updated := tx.Model(&model.ConsoleMonitorAttempt{}).Where("id = ? AND finished_at = 0", attempt.ID).Updates(map[string]any{"finished_at": now, "failure": outcome.Status != "normal" && outcome.Status != "degraded", "cleanup_state": "released", "cleanup_started_at": outcome.CleanupStartedAt})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return model.ErrMonitorDenied
		}
		probe := model.ConsoleProbe{TaskID: e.TaskID, AttemptID: attempt.ID, MonitorID: id, CheckedAt: now, Status: outcome.Status, ErrorCode: outcome.ErrorCode, LatencyMS: &latency, SourceChannelID: outcome.SourceChannelID, SourceKind: m.TargetKind}
		if err := tx.Create(&probe).Error; err != nil {
			return err
		}
		result.ProbeID, result.Status, result.ErrorCode = probe.ID, probe.Status, probe.ErrorCode
		result.MeteredCostMicros = outcome.MeteredCostMicros
		result.UpstreamResponseID = outcome.UpstreamResponseID
		return nil
	})
	if err != nil {
		result = ConsoleProbeResult{AttemptID: attempt.ID}
	}
	return result, err
}

func consoleProbeFailureCode(err, contextErr error) string {
	switch {
	case errors.Is(err, ErrMonitorCostUnverified):
		return "MONITOR_COST_UNVERIFIED"
	case errors.Is(err, ErrMonitorCostExceeded):
		return "MONITOR_COST_EXCEEDED"
	case errors.Is(err, ErrMonitorMetering):
		return "MONITOR_METERING_FAILED"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(contextErr, context.DeadlineExceeded):
		return "probe_timeout"
	case errors.Is(err, ErrMonitorLeaseUnresolved):
		return "probe_transport_unresolved"
	default:
		return "probe_failed"
	}
}

func isConsoleProbeCostScope(m model.ConsoleMonitor) bool {
	return m.TargetKind == "channel" && m.ChannelID == 1 && m.TargetRef == "1" && m.Model == "gpt-6-luna" && m.Protocol == "openai_responses"
}

func runConsoleProbeSafely(ctx context.Context, adapter ConsoleProbeAdapter, m model.ConsoleMonitor, attemptID int64) (outcome ConsoleProbeOutcome, err error) {
	defer func() {
		if recover() != nil {
			outcome = ConsoleProbeOutcome{}
			err = ErrMonitorProbePanic
		}
	}()
	return adapter.Probe(ctx, m, attemptID)
}

func runConsoleAuthorizeSafely(ctx context.Context, adapter ConsoleProbeAdapter, m model.ConsoleMonitor) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrMonitorProbePanic
		}
	}()
	return adapter.Authorize(ctx, m)
}

// ReconcileConsoleMonitorAttempt releases a retained concurrency reservation
// only after the adapter's authoritative boundary confirms the attempt is
// inactive. Operators should call this through an authenticated, audited path.
func (e *ConsoleProbeExecutor) ReconcileConsoleMonitorAttempt(ctx context.Context, attemptID int64) error {
	if !model.ConsoleMonitoringReady(e.DB) {
		return model.ErrMonitorNotReady
	}
	var attempt model.ConsoleMonitorAttempt
	if err := e.DB.WithContext(ctx).First(&attempt, attemptID).Error; err != nil {
		return err
	}
	if attempt.FinishedAt != 0 || attempt.CleanupState != "cleanup_pending" {
		return model.ErrMonitorInvalid
	}
	var configured model.ConsoleMonitor
	if err := e.DB.WithContext(ctx).First(&configured, attempt.MonitorID).Error; err != nil {
		return err
	}
	adapter, ok := e.Adapters[configured.TargetKind].(ConsoleProbeLeaseReconciler)
	if !ok {
		return model.ErrMonitorIntegration
	}
	claimed := e.DB.WithContext(ctx).Model(&model.ConsoleMonitorAttempt{}).Where("id = ? AND finished_at = 0 AND cleanup_state = ?", attempt.ID, "cleanup_pending").Updates(map[string]any{"cleanup_state": "cleanup_reconciling"})
	if claimed.Error != nil {
		return claimed.Error
	}
	if claimed.RowsAffected != 1 {
		return ErrMonitorLeaseUnresolved
	}
	confirmed, err := adapter.Reconcile(ctx, configured, attempt.ID)
	if err != nil {
		return e.reopenConsoleMonitorReconciliation(ctx, attempt.ID, err)
	}
	if !confirmed {
		return e.reopenConsoleMonitorReconciliation(ctx, attempt.ID, ErrMonitorLeaseUnresolved)
	}
	result := e.DB.WithContext(ctx).Model(&model.ConsoleMonitorAttempt{}).Where("id = ? AND finished_at = 0 AND cleanup_state = ?", attempt.ID, "cleanup_reconciling").Updates(map[string]any{"finished_at": time.Now().Unix(), "cleanup_state": "reconciled", "failure": true})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return model.ErrMonitorDenied
	}
	return nil
}

func (e *ConsoleProbeExecutor) reopenConsoleMonitorReconciliation(ctx context.Context, attemptID int64, cause error) error {
	result := e.DB.WithContext(ctx).Model(&model.ConsoleMonitorAttempt{}).Where("id = ? AND finished_at = 0 AND cleanup_state = ?", attemptID, "cleanup_reconciling").Updates(map[string]any{"cleanup_state": "cleanup_pending"})
	if result.Error != nil {
		return errors.Join(ErrMonitorLeaseUnresolved, cause, result.Error)
	}
	return errors.Join(ErrMonitorLeaseUnresolved, cause)
}

type ConsoleMonitorDailyResult struct {
	Normal  int `json:"normal"`
	Failed  int `json:"failed"`
	Blocked int `json:"blocked"`
	Skipped int `json:"skipped"`
}

// The daily observation pass still records paused/budget-blocked published
// targets. Only RunWithResult can authorize and reserve a real upstream call.
func (e *ConsoleProbeExecutor) RunDailyScheduled(ctx context.Context) (ConsoleMonitorDailyResult, error) {
	summary := ConsoleMonitorDailyResult{}
	if !model.ConsoleMonitoringReady(e.DB) {
		return summary, model.ErrMonitorNotReady
	}
	var monitors []model.ConsoleMonitor
	if err := e.DB.WithContext(ctx).Where("published = ? AND archived = ?", true, false).Order("id").Find(&monitors).Error; err != nil {
		return summary, err
	}
	for _, m := range monitors {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		result, err := e.RunWithResult(ctx, m.ID)
		if result.ProbeID == 0 && err != nil {
			return summary, err
		}
		switch result.Status {
		case "normal", "degraded":
			summary.Normal++
		case "skipped":
			summary.Skipped++
		case "blocked":
			summary.Blocked++
		default:
			summary.Failed++
		}
	}
	if err := model.PruneConsoleMonitoringHistory(ctx, e.DB, time.Now()); err != nil {
		return summary, err
	}
	if summary.Failed > 0 || summary.Blocked > 0 {
		return summary, errors.New("MONITOR_DAILY_INCOMPLETE")
	}
	return summary, nil
}
