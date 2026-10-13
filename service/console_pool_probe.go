package service

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/model"
)

// INTEGRATION PENDING: this is a proposed monitoring-only boundary, not a frozen
// Account Pool API. Nothing implements or registers it in gateway startup.
// Check and Acquire MUST honor group/model/protocol identity, capability and
// authorization, Fence, lease, actual routing and Billing admission. Acquire
// must recheck them atomically. No account credentials cross this boundary and
// no ordinary-user settlement or consumption log is permitted.
type ConsolePoolProbeBoundary interface {
	Check(context.Context, model.ConsoleMonitor) error
	Acquire(context.Context, model.ConsoleMonitor, int64) (ConsolePoolProbeLease, error)
	Reconcile(context.Context, model.ConsoleMonitor, int64) (bool, error)
}

// The lease owns the selected route and enforces the monitoring spending ceiling
// at the upstream boundary, not merely an estimated/retail price. Release must
// stop transport before relinquishing the lease, including cancellation paths.
type ConsolePoolProbeLease interface {
	Probe(context.Context) (ConsoleProbeOutcome, error)
	Release(context.Context) error
}

type ConsolePoolProbeAdapter struct {
	Boundary       ConsolePoolProbeBoundary
	CleanupTimeout time.Duration
}

var ErrMonitorLeaseUnresolved = model.ErrMonitorLease

func (a *ConsolePoolProbeAdapter) Authorize(ctx context.Context, m model.ConsoleMonitor) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if a.Boundary == nil {
		return model.ErrMonitorIntegration
	}
	if m.TargetKind != "account_pool_group" || m.TargetRef == "" || m.Model == "" || m.RequestCostMicros <= 0 {
		return model.ErrMonitorDenied
	}
	if err := a.Boundary.Check(ctx, m); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return model.ErrMonitorDenied
	}
	return nil
}

func (a *ConsolePoolProbeAdapter) Probe(ctx context.Context, m model.ConsoleMonitor, attemptID int64) (outcome ConsoleProbeOutcome, probeErr error) {
	if err := a.Authorize(ctx, m); err != nil {
		return ConsoleProbeOutcome{}, err
	}
	lease, err := a.Boundary.Acquire(ctx, m, attemptID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ConsoleProbeOutcome{}, err
		}
		return ConsoleProbeOutcome{}, model.ErrMonitorDenied
	}
	if lease == nil {
		return ConsoleProbeOutcome{}, model.ErrMonitorDenied
	}
	defer func() {
		// Cleanup gets its own full deadline, starting only after Probe returns.
		cleanupStartedAt := time.Now().Unix()
		outcome.CleanupStartedAt = cleanupStartedAt
		if recovered := recover(); recovered != nil {
			outcome = ConsoleProbeOutcome{Status: "unknown", ErrorCode: "probe_failed", CleanupStartedAt: cleanupStartedAt}
			probeErr = ErrMonitorProbePanic
		}
		cleanupTimeout := a.CleanupTimeout
		if cleanupTimeout <= 0 {
			cleanupTimeout = 5 * time.Second
		}
		releaseCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		if err := releaseConsolePoolLease(releaseCtx, lease); err != nil {
			// Keep the durable attempt unresolved: expiry or a failed release is
			// not evidence that the upstream transport stopped.
			outcome = ConsoleProbeOutcome{Status: "unknown", ErrorCode: "probe_failed", CleanupStartedAt: cleanupStartedAt}
			probeErr = errors.Join(ErrMonitorLeaseUnresolved, err)
		}
	}()
	return lease.Probe(ctx)
}

func releaseConsolePoolLease(ctx context.Context, lease ConsolePoolProbeLease) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrMonitorLeaseUnresolved
		}
	}()
	return lease.Release(ctx)
}

// Reconcile reports whether the boundary can prove that this attempt's route
// and transport are inactive. It never releases or expires a lease itself.
func (a *ConsolePoolProbeAdapter) Reconcile(ctx context.Context, m model.ConsoleMonitor, attemptID int64) (bool, error) {
	if a.Boundary == nil || ctx.Err() != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, model.ErrMonitorIntegration
	}
	return a.Boundary.Reconcile(ctx, m, attemptID)
}
