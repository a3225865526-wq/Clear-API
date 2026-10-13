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
	Status           string
	ErrorCode        string
	SourceChannelID  int
	CleanupStartedAt int64
}

type ConsoleProbeExecutor struct {
	DB       *gorm.DB
	Adapters map[string]ConsoleProbeAdapter
}

// RunScheduled performs one explicitly invoked scheduling pass. It is not
// registered with host background tasks and creates no startup goroutine.
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
	if !model.ConsoleMonitoringReady(e.DB) {
		return model.ErrMonitorNotReady
	}
	var configured model.ConsoleMonitor
	if err := e.DB.WithContext(ctx).First(&configured, id).Error; err != nil {
		return err
	}
	adapter := e.Adapters[configured.TargetKind]
	if adapter == nil {
		return model.ErrMonitorIntegration
	}
	if err := runConsoleAuthorizeSafely(ctx, adapter, configured); err != nil {
		return err
	}
	m, attempt, err := model.ReserveConsoleMonitorAttempt(ctx, e.DB, id, time.Now())
	if err != nil {
		return err
	}
	// Re-authorize the actual admitted snapshot, not the earlier unlocked read.
	probeCtx, cancel := context.WithTimeout(ctx, time.Duration(m.TimeoutSeconds)*time.Second)
	defer cancel()
	start := time.Now()
	outcome := ConsoleProbeOutcome{Status: "unknown", ErrorCode: "authorization_denied"}
	err = runConsoleAuthorizeSafely(probeCtx, adapter, m)
	if err == nil {
		outcome, err = runConsoleProbeSafely(probeCtx, adapter, m, attempt.ID)
	}
	if err == nil && probeCtx.Err() != nil {
		err = probeCtx.Err()
	}
	if err != nil {
		outcome.Status = "error"
		outcome.ErrorCode = "probe_failed"
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
			outcome.ErrorCode = "probe_timeout"
		}
	}
	if outcome.Status != "normal" && outcome.Status != "degraded" && outcome.Status != "error" && outcome.Status != "unknown" {
		outcome.Status = "unknown"
		outcome.ErrorCode = "invalid_result"
	}
	// Never persist arbitrary provider or adapter error strings.
	switch outcome.ErrorCode {
	case "", "probe_failed", "probe_timeout", "authorization_denied", "invalid_result":
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
			result := tx.Model(&model.ConsoleMonitorAttempt{}).Where("id = ? AND finished_at = 0", attempt.ID).Updates(updates)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return model.ErrMonitorDenied
			}
			return tx.Create(&model.ConsoleProbe{MonitorID: id, CheckedAt: now, Status: "unknown", ErrorCode: "probe_failed", LatencyMS: &latency, SourceKind: m.TargetKind}).Error
		})
		return errors.Join(ErrMonitorLeaseUnresolved, panicErr, persistErr)
	}
	return e.DB.WithContext(finishCtx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.ConsoleMonitorAttempt{}).Where("id = ? AND finished_at = 0", attempt.ID).Updates(map[string]any{"finished_at": now, "failure": outcome.Status != "normal" && outcome.Status != "degraded", "cleanup_state": "released", "cleanup_started_at": outcome.CleanupStartedAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return model.ErrMonitorDenied
		}
		return tx.Create(&model.ConsoleProbe{MonitorID: id, CheckedAt: now, Status: outcome.Status, ErrorCode: outcome.ErrorCode, LatencyMS: &latency, SourceChannelID: outcome.SourceChannelID, SourceKind: m.TargetKind}).Error
	})
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
