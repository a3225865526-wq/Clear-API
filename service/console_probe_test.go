package service_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/router"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type monitorMockAdapter struct {
	calls   int
	failure bool
	probe   func(context.Context, model.ConsoleMonitor) (service.ConsoleProbeOutcome, error)
}

func (a *monitorMockAdapter) Authorize(ctx context.Context, m model.ConsoleMonitor) error {
	if m.TargetKind == "account_pool_group" {
		return model.ErrMonitorIntegration
	}
	return nil
}
func (a *monitorMockAdapter) Probe(ctx context.Context, m model.ConsoleMonitor, _ int64) (service.ConsoleProbeOutcome, error) {
	a.calls++
	if a.probe != nil {
		return a.probe(ctx, m)
	}
	if a.failure {
		return service.ConsoleProbeOutcome{}, context.DeadlineExceeded
	}
	return service.ConsoleProbeOutcome{Status: "normal", SourceChannelID: m.ChannelID}, nil
}

// Legacy schema fixtures deliberately omit all new control-plane fields.
type legacyMonitor struct {
	ID          int `gorm:"primaryKey"`
	ChannelID   int `gorm:"index"`
	DisplayName string
	Platform    string
	Model       string
	Published   bool `gorm:"default:false"`
}
type legacyProbe struct {
	ID         int64 `gorm:"primaryKey"`
	MonitorID  int   `gorm:"index:idx_console_probe_time,priority:1"`
	CheckedAt  int64 `gorm:"index:idx_console_probe_time,priority:2"`
	Status     string
	LatencyMS  *int64
	EndpointMS *int64
	ErrorCode  string
}

func TestConsoleMonitorControlMatrix(t *testing.T) {
	cases := []struct {
		name   string
		driver gorm.Dialector
		kind   common.DatabaseType
	}{
		{"sqlite", sqlite.Open(":memory:"), common.DatabaseTypeSQLite},
		{"postgres", postgres.New(postgres.Config{DSN: os.Getenv("MONITOR_TEST_POSTGRES_DSN"), PreferSimpleProtocol: true}), common.DatabaseTypePostgreSQL},
		{"mysql", mysql.Open(os.Getenv("MONITOR_TEST_MYSQL_DSN")), common.DatabaseTypeMySQL},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "postgres" && os.Getenv("MONITOR_TEST_POSTGRES_DSN") == "" || tc.name == "mysql" && os.Getenv("MONITOR_TEST_MYSQL_DSN") == "" {
				t.Skip("isolated database not configured")
			}
			db, err := gorm.Open(tc.driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			if tc.name == "sqlite" {
				sqlDB.SetMaxOpenConns(1)
			}
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			oldKind := common.MainDatabaseType()
			common.SetMainDatabaseType(tc.kind)
			t.Cleanup(func() { common.SetMainDatabaseType(oldKind) })
			ctx := context.Background()
			// Only run against an empty dedicated fixture database, never an existing
			// installation. Exercise fresh migration before the legacy upgrade case.
			require.False(t, db.Migrator().HasTable(&model.Channel{}), "fixture database must be empty")
			require.False(t, db.Migrator().HasTable(&model.ConsoleMonitor{}), "fixture database must be empty")
			require.NoError(t, model.MigrateConsoleMonitoringDB(db))
			require.NoError(t, model.MigrateConsoleMonitoringDB(db))
			// Reconstruct the PR #29 schema before exercising its additive upgrade.
			for _, column := range []string{"allowlisted", "created_by", "created_at"} {
				require.NoError(t, db.Migrator().DropColumn(&model.ConsoleMonitor{}, column))
			}
			prior := model.ConsoleMonitor{ID: 49, ChannelID: 1, Model: "prior-model", TargetKind: "channel", TargetRef: "1", Protocol: "openai_chat", Published: true, IntervalSeconds: 600, TimeoutSeconds: 30, StaleSeconds: 900}
			require.NoError(t, db.Omit("Allowlisted", "CreatedBy", "CreatedAt").Create(&prior).Error)
			assert.False(t, model.ConsoleMonitorAllowlistReady(db))
			require.NoError(t, model.MigrateConsoleMonitoringDB(db))
			require.NoError(t, model.MigrateConsoleMonitoringDB(db))
			var upgraded model.ConsoleMonitor
			require.NoError(t, db.First(&upgraded, 49).Error)
			assert.Equal(t, prior, upgraded)
			assert.True(t, model.ConsoleMonitorAllowlistReady(db))
			require.NoError(t, db.Migrator().DropTable(&model.ConsoleMonitorAttempt{}, &model.ConsoleMonitorPolicy{}, &model.ConsoleProbe{}, &model.ConsoleMonitor{}))
			require.NoError(t, db.Table("console_monitors").AutoMigrate(&legacyMonitor{}))
			require.NoError(t, db.Table("console_probes").AutoMigrate(&legacyProbe{}))
			require.NoError(t, db.Table("console_monitors").Create(&legacyMonitor{ID: 50, ChannelID: 9, Model: "legacy", DisplayName: "Retained", Published: true}).Error)
			require.NoError(t, db.Table("console_probes").Create(&legacyProbe{MonitorID: 50, Status: "normal", CheckedAt: time.Now().Unix()}).Error)
			require.NoError(t, model.MigrateConsoleMonitoringDB(db))
			require.NoError(t, model.MigrateConsoleMonitoringDB(db))
			var legacy model.ConsoleMonitor
			require.NoError(t, db.First(&legacy, 50).Error)
			assert.True(t, legacy.Published)
			assert.False(t, legacy.Enabled)
			var history int64
			require.NoError(t, db.Model(&model.ConsoleProbe{}).Count(&history).Error)
			assert.EqualValues(t, 1, history)
			require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
			require.NoError(t, db.Create(&model.Channel{Id: 1, Name: "Allowed", Key: "fixture-only", Status: common.ChannelStatusEnabled, Models: "model-a,model-b"}).Error)
			require.NoError(t, db.Create(&model.Channel{Id: 2, Name: "Disabled", Key: "fixture-only", Status: common.ChannelStatusManuallyDisabled, Models: "model-a"}).Error)
			require.NoError(t, db.Create(&model.Channel{Id: 3, Name: "Same model", Key: "fixture-only", Status: common.ChannelStatusEnabled, Models: "model-a"}).Error)
			require.NoError(t, db.Create(&model.Channel{Id: 9, Name: "Legacy", Key: "fixture-only", Status: common.ChannelStatusEnabled, Models: "legacy"}).Error)
			m := model.ConsoleMonitor{ChannelID: 1, TargetKind: "channel", TargetRef: "1", Model: "model-a", Protocol: "openai_chat", DisplayName: "Public name", IntervalSeconds: 60, TimeoutSeconds: 1, StaleSeconds: 900, DailyRequests: 2, RequestCostMicros: 10}
			require.NoError(t, model.SaveConsoleMonitor(ctx, db, &m))
			require.NoError(t, db.Model(&m).Updates(map[string]any{"created_by": 7, "created_at": int64(1720000000)}).Error)
			require.NoError(t, model.SetConsoleMonitorAllowlisted(ctx, db, m.ID, true))
			require.NoError(t, model.SetConsoleMonitorAllowlisted(ctx, db, m.ID, true))
			var allowlisted model.ConsoleMonitor
			require.NoError(t, db.First(&allowlisted, m.ID).Error)
			assert.True(t, allowlisted.Allowlisted)
			edit := allowlisted
			edit.DisplayName, edit.DailyRequests, edit.RequestCostMicros = "Updated allowlist name", 999, 999
			require.NoError(t, model.UpdateConsoleMonitorAllowlist(ctx, db, &edit))
			require.NoError(t, db.First(&allowlisted, m.ID).Error)
			assert.Equal(t, "Updated allowlist name", allowlisted.DisplayName)
			assert.EqualValues(t, 2, allowlisted.DailyRequests)
			assert.EqualValues(t, 10, allowlisted.RequestCostMicros)
			assert.False(t, allowlisted.Enabled)
			require.NoError(t, model.MigrateConsoleMonitoringDB(db))
			require.NoError(t, db.First(&allowlisted, m.ID).Error)
			assert.Equal(t, 7, allowlisted.CreatedBy)
			assert.EqualValues(t, 1720000000, allowlisted.CreatedAt)
			assert.True(t, allowlisted.Allowlisted)
			adopt := m
			adopt.ID = 0
			adopt.ChannelID = 9
			adopt.TargetRef = "9"
			adopt.Model = "legacy"
			require.NoError(t, model.SaveConsoleMonitor(ctx, db, &adopt))
			assert.Equal(t, 50, adopt.ID)
			firstID := m.ID
			m.ID = 0
			require.NoError(t, model.SaveConsoleMonitor(ctx, db, &m))
			assert.Equal(t, firstID, m.ID)
			require.NoError(t, model.SetConsoleMonitorSwitch(ctx, db, m.ID, "published", true))
			var published model.ConsoleMonitor
			require.NoError(t, db.First(&published, m.ID).Error)
			assert.False(t, published.Enabled)
			second := m
			second.ID = 0
			second.Model = "model-b"
			require.NoError(t, model.SaveConsoleMonitor(ctx, db, &second))
			assert.NotEqual(t, m.ID, second.ID)
			third := m
			third.ID = 0
			third.ChannelID = 3
			third.TargetRef = "3"
			require.NoError(t, model.SaveConsoleMonitor(ctx, db, &third))
			assert.NotEqual(t, m.ID, third.ID)
			denied := m
			denied.ID = 0
			denied.ChannelID = 2
			denied.TargetRef = "2"
			require.ErrorIs(t, model.SaveConsoleMonitor(ctx, db, &denied), model.ErrMonitorDenied)
			denied = m
			denied.ID = 0
			denied.Model = "not-authorized"
			require.ErrorIs(t, model.SaveConsoleMonitor(ctx, db, &denied), model.ErrMonitorDenied)
			denied = m
			denied.TargetKind = "account_pool_group"
			denied.ChannelID = 0
			denied.ID = 0
			denied.TargetRef = "fixture-pool-group"
			require.NoError(t, model.SaveConsoleMonitor(ctx, db, &denied))
			require.ErrorIs(t, model.SetConsoleMonitorSwitch(ctx, db, denied.ID, "published", true), model.ErrMonitorIntegration)
			require.ErrorIs(t, model.SetConsoleMonitorAllowlisted(ctx, db, denied.ID, true), model.ErrMonitorIntegration)
			require.ErrorIs(t, model.SetConsoleMonitorSwitch(ctx, db, denied.ID, "enabled", true), model.ErrMonitorIntegration)
			require.NoError(t, db.Create(&model.Ability{Group: "stable-group", Model: "model-a", ChannelId: 1, Enabled: true}).Error)
			group := m
			group.ID = 0
			group.ChannelID = 0
			group.TargetKind = "group_route"
			group.TargetRef = "stable-group"
			require.NoError(t, model.SaveConsoleMonitor(ctx, db, &group))
			require.NoError(t, model.SetConsoleMonitorAllowlisted(ctx, db, group.ID, true))
			group.Model = "not-authorized"
			require.ErrorIs(t, model.ValidateMonitorTarget(db, &group), model.ErrMonitorDenied)
			mock := &monitorMockAdapter{}
			executor := service.ConsoleProbeExecutor{DB: db, Adapters: map[string]service.ConsoleProbeAdapter{"channel": mock}}
			require.ErrorIs(t, executor.Run(ctx, m.ID), model.ErrMonitorPaused)
			assert.Zero(t, mock.calls)
			require.NoError(t, model.SetConsoleMonitorSwitch(ctx, db, m.ID, "enabled", true))
			require.ErrorIs(t, executor.Run(ctx, m.ID), model.ErrMonitorPaused)
			assert.Zero(t, mock.calls)
			require.NoError(t, db.Model(&model.ConsoleMonitorPolicy{}).Where("id = 1").Updates(map[string]any{"enabled": true, "max_concurrent": 1, "daily_requests": 2, "daily_cost_micros": 20}).Error)
			for _, gate := range []string{"allowlisted", "published", "enabled"} {
				require.NoError(t, db.Model(&model.ConsoleMonitor{}).Where("id = ?", m.ID).Update(gate, false).Error)
				result, err := executor.RunWithResult(ctx, m.ID)
				require.ErrorIs(t, err, model.ErrMonitorPaused, gate)
				assert.Zero(t, result.AttemptID)
				assert.Zero(t, mock.calls)
				require.NoError(t, db.Model(&model.ConsoleMonitor{}).Where("id = ?", m.ID).Update(gate, true).Error)
			}
			require.NoError(t, executor.Run(ctx, m.ID))
			assert.Equal(t, 1, mock.calls)
			require.ErrorIs(t, executor.Run(ctx, m.ID), model.ErrMonitorBudget)
			assert.Equal(t, 1, mock.calls)
			var probe model.ConsoleProbe
			require.NoError(t, db.Where("monitor_id = ? AND status = ?", m.ID, "normal").First(&probe).Error)
			assert.Equal(t, "normal", probe.Status)
			assert.Equal(t, 1, probe.SourceChannelID)
			// A different model has its own records; failed probes preserve channels.
			require.NoError(t, model.SetConsoleMonitorAllowlisted(ctx, db, second.ID, true))
			require.NoError(t, model.SetConsoleMonitorSwitch(ctx, db, second.ID, "published", true))
			require.NoError(t, model.SetConsoleMonitorSwitch(ctx, db, second.ID, "enabled", true))
			mock.failure = true
			require.NoError(t, executor.Run(ctx, second.ID))
			assert.Equal(t, 2, mock.calls)
			probe = model.ConsoleProbe{}
			require.NoError(t, db.Where("monitor_id = ?", second.ID).First(&probe).Error)
			assert.Equal(t, "error", probe.Status)
			require.NoError(t, model.SetConsoleMonitorAllowlisted(ctx, db, third.ID, true))
			require.NoError(t, model.SetConsoleMonitorSwitch(ctx, db, third.ID, "published", true))
			require.NoError(t, model.SetConsoleMonitorSwitch(ctx, db, third.ID, "enabled", true))
			require.ErrorIs(t, executor.Run(ctx, third.ID), model.ErrMonitorBudget)
			assert.Equal(t, 2, mock.calls)
			for _, id := range []int{1, 2, 3} {
				var c model.Channel
				require.NoError(t, db.First(&c, id).Error)
				assert.Zero(t, c.UsedQuota)
				assert.Zero(t, c.ResponseTime)
				if id == 2 {
					assert.Equal(t, common.ChannelStatusManuallyDisabled, c.Status)
				} else {
					assert.Equal(t, common.ChannelStatusEnabled, c.Status)
				}
			}
			require.NoError(t, model.SetConsoleMonitorSwitch(ctx, db, m.ID, "archived", true))
			require.NoError(t, model.MigrateConsoleMonitoringDB(db))
			var restarted model.ConsoleMonitor
			require.NoError(t, db.First(&restarted, m.ID).Error)
			assert.True(t, restarted.Archived)
			assert.False(t, restarted.Enabled)
			assert.False(t, restarted.Published)
			require.NoError(t, db.Model(&model.ConsoleProbe{}).Count(&history).Error)
			assert.EqualValues(t, 10, history) // Three execution records plus seven recorded admission denials.
			require.NoError(t, model.SaveConsoleMonitorPolicy(ctx, db, model.ConsoleMonitorPolicy{ID: 1, Enabled: true, MaxConcurrent: 1, DailyRequests: 10, DailyCostMicros: 100, RetentionDays: 90}))
			// Two processes racing for the same target must reserve only once.
			start := make(chan struct{})
			results := make(chan error, 2)
			for range 2 {
				go func() {
					<-start
					_, _, err := model.ReserveConsoleMonitorAttempt(ctx, db, third.ID, time.Now())
					results <- err
				}()
			}
			close(start)
			successes := 0
			for range 2 {
				if err := <-results; err == nil {
					successes++
				}
			}
			assert.Equal(t, 1, successes)
			var active int64
			require.NoError(t, db.Model(&model.ConsoleMonitorAttempt{}).Where("finished_at = 0").Count(&active).Error)
			assert.EqualValues(t, 1, active)
		})
	}
}

type monitorPoolBoundaryMock struct {
	checkErr, acquireErr error
	lease                *monitorPoolLeaseMock
	acquires             int
}

func (b *monitorPoolBoundaryMock) Check(context.Context, model.ConsoleMonitor) error {
	return b.checkErr
}
func (b *monitorPoolBoundaryMock) Acquire(context.Context, model.ConsoleMonitor, int64) (service.ConsolePoolProbeLease, error) {
	b.acquires++
	return b.lease, b.acquireErr
}
func (b *monitorPoolBoundaryMock) Reconcile(context.Context, model.ConsoleMonitor, int64) (bool, error) {
	if b.lease == nil {
		return false, model.ErrMonitorLease
	}
	return b.lease.reconciled, b.lease.reconcileErr
}

type monitorPoolLeaseMock struct {
	probes, releases int
	probeErr         error
	releaseErr       error
	releasePanic     bool
	releaseWait      bool
	probeReturnedAt  time.Time
	releaseStartedAt time.Time
	reconciled       bool
	reconcileErr     error
	releaseCanceled  bool
	cancel           context.CancelFunc
}

func (l *monitorPoolLeaseMock) Probe(context.Context) (service.ConsoleProbeOutcome, error) {
	l.probes++
	l.probeReturnedAt = time.Now()
	if l.cancel != nil {
		l.cancel()
	}
	return service.ConsoleProbeOutcome{Status: "normal"}, l.probeErr
}
func (l *monitorPoolLeaseMock) Release(ctx context.Context) error {
	l.releases++
	l.releaseStartedAt = time.Now()
	if l.releasePanic {
		panic("fixture release panic")
	}
	if l.releaseWait {
		<-ctx.Done()
		l.releaseCanceled = ctx.Err() != nil
		return ctx.Err()
	}
	l.releaseCanceled = ctx.Err() != nil
	return l.releaseErr
}

func TestConsoleMonitorPoolContractDenialAndLeaseCleanup(t *testing.T) {
	m := model.ConsoleMonitor{TargetKind: "account_pool_group", TargetRef: "fixture-group", Model: "fixture-model", RequestCostMicros: 10}
	denied := &monitorPoolBoundaryMock{checkErr: model.ErrMonitorDenied}
	deniedAdapter := service.ConsolePoolProbeAdapter{Boundary: denied}
	_, err := deniedAdapter.Probe(context.Background(), m, 1)
	require.ErrorIs(t, err, model.ErrMonitorDenied)
	assert.Zero(t, denied.acquires)
	// Passing the initial check cannot bypass revocation at atomic acquisition.
	denied.checkErr, denied.acquireErr = nil, model.ErrMonitorDenied
	_, err = deniedAdapter.Probe(context.Background(), m, 1)
	require.ErrorIs(t, err, model.ErrMonitorDenied)
	assert.Equal(t, 1, denied.acquires)
	lease := &monitorPoolLeaseMock{probeErr: context.Canceled}
	boundary := &monitorPoolBoundaryMock{lease: lease}
	adapter := service.ConsolePoolProbeAdapter{Boundary: boundary}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lease.cancel = cancel
	_, err = adapter.Probe(ctx, m, 1)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, lease.probes)
	assert.Equal(t, 1, lease.releases)
	assert.False(t, lease.releaseCanceled, "cleanup uses an independent bounded context")
	adapter.Boundary = nil
	require.ErrorIs(t, adapter.Authorize(context.Background(), m), model.ErrMonitorIntegration)
}

type monitorReconcileAdapter struct {
	mu        sync.Mutex
	calls     int
	confirmed bool
	err       error
}

func (a *monitorReconcileAdapter) Authorize(context.Context, model.ConsoleMonitor) error { return nil }
func (a *monitorReconcileAdapter) Probe(context.Context, model.ConsoleMonitor, int64) (service.ConsoleProbeOutcome, error) {
	return service.ConsoleProbeOutcome{Status: "error", ErrorCode: "probe_failed"}, nil
}
func (a *monitorReconcileAdapter) Reconcile(context.Context, model.ConsoleMonitor, int64) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	return a.confirmed, a.err
}

func TestConsoleMonitorLeaseFailuresRetainCapacityAndCleanupStartsAfterProbe(t *testing.T) {
	m := model.ConsoleMonitor{TargetKind: "account_pool_group", TargetRef: "fixture-group", Model: "fixture-model", RequestCostMicros: 10}
	lease := &monitorPoolLeaseMock{releaseWait: true}
	adapter := service.ConsolePoolProbeAdapter{Boundary: &monitorPoolBoundaryMock{lease: lease}, CleanupTimeout: time.Millisecond}
	_, err := adapter.Probe(context.Background(), m, 7)
	require.ErrorIs(t, err, service.ErrMonitorLeaseUnresolved)
	assert.Equal(t, 1, lease.releases)
	assert.True(t, lease.releaseCanceled, "cleanup timeout is bounded independently of the probe context")
	assert.False(t, lease.releaseStartedAt.Before(lease.probeReturnedAt), "cleanup begins after probe returns")

	lease = &monitorPoolLeaseMock{releaseErr: errors.New("release uncertain")}
	adapter.Boundary = &monitorPoolBoundaryMock{lease: lease}
	_, err = adapter.Probe(context.Background(), m, 8)
	require.ErrorIs(t, err, service.ErrMonitorLeaseUnresolved)
	assert.Equal(t, 1, lease.releases)

	lease = &monitorPoolLeaseMock{releasePanic: true}
	adapter.Boundary = &monitorPoolBoundaryMock{lease: lease}
	_, err = adapter.Probe(context.Background(), m, 9)
	require.ErrorIs(t, err, service.ErrMonitorLeaseUnresolved)
}

func TestConsoleMonitorExecutorRetainsUnresolvedAttemptUntilReconciled(t *testing.T) {
	db, m := consoleMonitorFixture(t)
	ctx := context.Background()
	require.NoError(t, model.SetConsoleMonitorSwitch(ctx, db, m.ID, "enabled", true))
	require.NoError(t, model.SaveConsoleMonitorPolicy(ctx, db, model.ConsoleMonitorPolicy{ID: 1, Enabled: true, MaxConcurrent: 1, DailyRequests: 10, DailyCostMicros: 100, RetentionDays: 90}))
	adapter := &monitorMockAdapter{probe: func(context.Context, model.ConsoleMonitor) (service.ConsoleProbeOutcome, error) {
		return service.ConsoleProbeOutcome{}, errors.Join(service.ErrMonitorLeaseUnresolved, errors.New("fixture-cleanup-secret"))
	}}
	e := service.ConsoleProbeExecutor{DB: db, Adapters: map[string]service.ConsoleProbeAdapter{"channel": adapter}}
	result, err := e.RunWithResult(ctx, m.ID)
	require.ErrorIs(t, err, service.ErrMonitorLeaseUnresolved)
	require.Positive(t, result.AttemptID)
	require.Positive(t, result.ProbeID)
	assert.Equal(t, "unknown", result.Status)
	assert.Equal(t, "probe_transport_unresolved", result.ErrorCode)
	assert.NotContains(t, err.Error(), "fixture-cleanup-secret")
	var attempt model.ConsoleMonitorAttempt
	require.NoError(t, db.Where("monitor_id = ?", m.ID).First(&attempt).Error)
	assert.Equal(t, result.AttemptID, attempt.ID)
	assert.Zero(t, attempt.FinishedAt)
	assert.Equal(t, "cleanup_pending", attempt.CleanupState)
	var active int64
	require.NoError(t, db.Model(&model.ConsoleMonitorAttempt{}).Where("finished_at = 0").Count(&active).Error)
	assert.EqualValues(t, 1, active)
}

func TestConsoleMonitorProbePanicRetainsCapacityUntilReconciled(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"stop unconfirmed", nil},
		{"confirmation failed", errors.New("fixture reconciliation failure")},
		{"confirmation canceled", context.Canceled},
		{"confirmation timed out", context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, m := consoleMonitorFixture(t)
			ctx := context.Background()
			require.NoError(t, model.SetConsoleMonitorSwitch(ctx, db, m.ID, "enabled", true))
			require.NoError(t, model.SaveConsoleMonitorPolicy(ctx, db, model.ConsoleMonitorPolicy{ID: 1, Enabled: true, MaxConcurrent: 1, DailyRequests: 10, DailyCostMicros: 100, RetentionDays: 90}))
			adapter := &monitorMockAdapter{probe: func(context.Context, model.ConsoleMonitor) (service.ConsoleProbeOutcome, error) {
				panic("fixture-panic-secret")
			}}
			e := service.ConsoleProbeExecutor{DB: db, Adapters: map[string]service.ConsoleProbeAdapter{"channel": adapter}}
			err := e.Run(ctx, m.ID)
			require.ErrorIs(t, err, service.ErrMonitorLeaseUnresolved)
			require.ErrorIs(t, err, service.ErrMonitorProbePanic)
			var attempt model.ConsoleMonitorAttempt
			require.NoError(t, db.Where("monitor_id = ?", m.ID).First(&attempt).Error)
			assert.Zero(t, attempt.FinishedAt)
			assert.Equal(t, "cleanup_pending", attempt.CleanupState)
			assert.True(t, attempt.Failure)
			assert.Equal(t, "probe_panic", attempt.RecoveryReason)
			var probe model.ConsoleProbe
			require.NoError(t, db.Where("monitor_id = ?", m.ID).First(&probe).Error)
			assert.Equal(t, "unknown", probe.Status)
			assert.Equal(t, "probe_failed", probe.ErrorCode)
			encoded, marshalErr := common.Marshal([]any{attempt, probe})
			require.NoError(t, marshalErr)
			assert.NotContains(t, string(encoded), "fixture-panic-secret")

			second := m
			second.ID, second.Model = 0, "model-b"
			require.NoError(t, model.SaveConsoleMonitor(ctx, db, &second))
			require.NoError(t, model.SetConsoleMonitorAllowlisted(ctx, db, second.ID, true))
			require.NoError(t, model.SetConsoleMonitorSwitch(ctx, db, second.ID, "published", true))
			require.NoError(t, model.SetConsoleMonitorSwitch(ctx, db, second.ID, "enabled", true))
			require.ErrorIs(t, e.Run(ctx, second.ID), model.ErrMonitorBudget)
			assert.Equal(t, 1, adapter.calls, "another monitor cannot start a probe while the panic holds capacity")

			reconciler := &monitorReconcileAdapter{err: tc.err}
			e.Adapters["channel"] = reconciler
			require.ErrorIs(t, e.ReconcileConsoleMonitorAttempt(ctx, attempt.ID), service.ErrMonitorLeaseUnresolved)
			require.NoError(t, db.First(&attempt, attempt.ID).Error)
			assert.Zero(t, attempt.FinishedAt)
			assert.Equal(t, "cleanup_pending", attempt.CleanupState)
			require.ErrorIs(t, e.Run(ctx, second.ID), model.ErrMonitorBudget)

			reconciler.confirmed, reconciler.err = true, nil
			require.NoError(t, e.ReconcileConsoleMonitorAttempt(ctx, attempt.ID))
			require.NoError(t, db.First(&attempt, attempt.ID).Error)
			assert.NotZero(t, attempt.FinishedAt)
			assert.Equal(t, "reconciled", attempt.CleanupState)
			assert.Equal(t, "probe_panic", attempt.RecoveryReason)
			e.Adapters["channel"] = &monitorMockAdapter{}
			require.NoError(t, e.Run(ctx, second.ID))
		})
	}
}

func TestConsoleMonitorReconciliationIsSingleClaimAndRecoveryKeepsLease(t *testing.T) {
	db, m := consoleMonitorFixture(t)
	attempt := model.ConsoleMonitorAttempt{MonitorID: m.ID, StartedAt: time.Now().Unix(), LeaseUntil: time.Now().Unix() + 1, ReservedCostMicros: 10, CleanupState: "cleanup_pending"}
	require.NoError(t, db.Create(&attempt).Error)
	adapter := &monitorReconcileAdapter{confirmed: false}
	e := service.ConsoleProbeExecutor{DB: db, Adapters: map[string]service.ConsoleProbeAdapter{"channel": adapter}}
	require.ErrorIs(t, e.ReconcileConsoleMonitorAttempt(context.Background(), attempt.ID), service.ErrMonitorLeaseUnresolved)
	var saved model.ConsoleMonitorAttempt
	require.NoError(t, db.First(&saved, attempt.ID).Error)
	assert.Zero(t, saved.FinishedAt)
	assert.Equal(t, "cleanup_pending", saved.CleanupState)

	adapter.confirmed = true
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			results <- e.ReconcileConsoleMonitorAttempt(context.Background(), attempt.ID)
		}()
	}
	wait.Wait()
	close(results)
	successes := 0
	for result := range results {
		if result == nil {
			successes++
		}
	}
	assert.Equal(t, 1, successes)
	assert.Equal(t, 2, adapter.calls, "a failed confirmation may be retried, but a successful claim is single-use")
	require.NoError(t, db.First(&saved, attempt.ID).Error)
	assert.NotZero(t, saved.FinishedAt)
	assert.Equal(t, "reconciled", saved.CleanupState)

	stale := model.ConsoleMonitorAttempt{MonitorID: m.ID, StartedAt: time.Now().Unix(), LeaseUntil: time.Now().Unix() + 1, ReservedCostMicros: 10, CleanupState: "cleanup_reconciling"}
	require.NoError(t, db.Create(&stale).Error)
	require.NoError(t, model.RecoverConsoleMonitorAttempt(context.Background(), db, stale.ID, "worker restarted before boundary confirmation", time.Now()))
	saved = model.ConsoleMonitorAttempt{}
	require.NoError(t, db.First(&saved, stale.ID).Error)
	assert.Equal(t, "cleanup_pending", saved.CleanupState)
	assert.Equal(t, "worker restarted before boundary confirmation", saved.RecoveryReason)
	assert.Zero(t, saved.FinishedAt)
}

func TestConsoleMonitorSchedulerRespectsPauseAndPublication(t *testing.T) {
	db, m := consoleMonitorFixture(t)
	mock := &monitorMockAdapter{}
	e := service.ConsoleProbeExecutor{DB: db, Adapters: map[string]service.ConsoleProbeAdapter{"channel": mock}}
	require.ErrorIs(t, e.RunScheduled(context.Background()), model.ErrMonitorPaused)
	require.NoError(t, model.SetConsoleMonitorSwitch(context.Background(), db, m.ID, "enabled", true))
	require.NoError(t, model.SaveConsoleMonitorPolicy(context.Background(), db, model.ConsoleMonitorPolicy{ID: 1, Enabled: true, MaxConcurrent: 1, DailyRequests: 2, DailyCostMicros: 20, RetentionDays: 90}))
	require.NoError(t, model.SetConsoleMonitorSwitch(context.Background(), db, m.ID, "published", false))
	require.NoError(t, e.RunScheduled(context.Background()))
	assert.Zero(t, mock.calls, "unpublished targets cannot execute")
	require.NoError(t, model.SetConsoleMonitorSwitch(context.Background(), db, m.ID, "published", true))
	require.NoError(t, e.RunScheduled(context.Background()))
	require.NoError(t, e.RunScheduled(context.Background()))
	assert.Equal(t, 1, mock.calls, "repeated scheduler passes cannot bypass per-target intervals")
	var saved model.ConsoleMonitor
	require.NoError(t, db.First(&saved, m.ID).Error)
	assert.True(t, saved.Published, "execution never changes publication")
}

func TestConsoleMonitorRetentionPrunesFinishedHistoryButKeepsUnresolvedAttempts(t *testing.T) {
	db, m := consoleMonitorFixture(t)
	ctx := context.Background()
	require.NoError(t, model.SaveConsoleMonitorPolicy(ctx, db, model.ConsoleMonitorPolicy{ID: 1, Enabled: false, MaxConcurrent: 1, DailyRequests: 10, DailyCostMicros: 100, RetentionDays: 30}))
	now := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	old := now.Add(-31 * 24 * time.Hour).Unix()
	require.NoError(t, db.Create(&model.ConsoleProbe{MonitorID: m.ID, CheckedAt: old, Status: "normal"}).Error)
	require.NoError(t, db.Create(&model.ConsoleMonitorAttempt{MonitorID: m.ID, StartedAt: old, FinishedAt: old + 1, ReservedCostMicros: 10}).Error)
	require.NoError(t, db.Create(&model.ConsoleMonitorAttempt{MonitorID: m.ID, StartedAt: old, LeaseUntil: old + 60, ReservedCostMicros: 10}).Error)
	require.NoError(t, model.PruneConsoleMonitoringHistory(ctx, db, now))
	var probes, attempts int64
	require.NoError(t, db.Model(&model.ConsoleProbe{}).Count(&probes).Error)
	require.NoError(t, db.Model(&model.ConsoleMonitorAttempt{}).Count(&attempts).Error)
	assert.Zero(t, probes)
	assert.EqualValues(t, 1, attempts)
}

func consoleMonitorFixture(t *testing.T) (*gorm.DB, model.ConsoleMonitor) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "monitor.sqlite")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	oldKind := common.MainDatabaseType()
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() { common.SetMainDatabaseType(oldKind) })
	require.NoError(t, model.MigrateConsoleMonitoringDB(db))
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	require.NoError(t, db.Create(&model.Channel{Id: 1, Status: common.ChannelStatusEnabled, Key: "fixture-secret", Models: "gpt-6-luna,model-b", Name: "Allowed"}).Error)
	require.NoError(t, db.Create(&model.Channel{Id: 2, Status: common.ChannelStatusManuallyDisabled, Key: "fixture-secret", Models: "gpt-6.1-sol", Name: "Disabled"}).Error)
	m := model.ConsoleMonitor{ChannelID: 1, TargetKind: "channel", TargetRef: "1", Model: "gpt-6-luna", Protocol: "openai_chat", DisplayName: "Approved", IntervalSeconds: 60, TimeoutSeconds: 1, StaleSeconds: 900, DailyRequests: 2, RequestCostMicros: 10}
	require.NoError(t, model.SaveConsoleMonitor(context.Background(), db, &m))
	require.NoError(t, model.SetConsoleMonitorAllowlisted(context.Background(), db, m.ID, true))
	require.NoError(t, model.SetConsoleMonitorSwitch(context.Background(), db, m.ID, "published", true))
	return db, m
}

func TestConsoleMonitorAdmissionRejectsOutstandingExpiredAndBackoffAttempts(t *testing.T) {
	db, m := consoleMonitorFixture(t)
	ctx := context.Background()
	require.NoError(t, model.SetConsoleMonitorSwitch(ctx, db, m.ID, "enabled", true))
	require.NoError(t, model.SaveConsoleMonitorPolicy(ctx, db, model.ConsoleMonitorPolicy{ID: 1, Enabled: true, MaxConcurrent: 1, DailyRequests: 2, DailyCostMicros: 20, RetentionDays: 90}))
	now := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	_, attempt, err := model.ReserveConsoleMonitorAttempt(ctx, db, m.ID, now)
	require.NoError(t, err)
	_, _, err = model.ReserveConsoleMonitorAttempt(ctx, db, m.ID, now.Add(time.Hour))
	require.ErrorIs(t, err, model.ErrMonitorBudget, "lease expiry cannot release an unresolved request")
	require.NoError(t, db.Model(&attempt).Updates(map[string]any{"finished_at": now.Unix() + 1, "failure": true}).Error)
	_, _, err = model.ReserveConsoleMonitorAttempt(ctx, db, m.ID, now.Add(60*time.Second))
	require.ErrorIs(t, err, model.ErrMonitorBudget)
	_, _, err = model.ReserveConsoleMonitorAttempt(ctx, db, m.ID, now.Add(120*time.Second))
	require.NoError(t, err)
	var cost int64
	require.NoError(t, db.Model(&model.ConsoleMonitorAttempt{}).Select("sum(reserved_cost_micros)").Scan(&cost).Error)
	assert.EqualValues(t, 20, cost, "failures do not refund monitoring reservations")
}

func TestConsoleMonitorExecutorValidatesGroupSourceAndSanitizesErrors(t *testing.T) {
	for _, tc := range []struct {
		name         string
		outcome      service.ConsoleProbeOutcome
		err          error
		status, code string
	}{
		{"selected member", service.ConsoleProbeOutcome{Status: "normal", SourceChannelID: 1}, nil, "normal", ""},
		{"missing source", service.ConsoleProbeOutcome{Status: "normal"}, nil, "unknown", "invalid_result"},
		{"disabled source", service.ConsoleProbeOutcome{Status: "normal", SourceChannelID: 2}, nil, "unknown", "invalid_result"},
		{"timeout", service.ConsoleProbeOutcome{}, context.DeadlineExceeded, "error", "probe_timeout"},
		{"secret upstream error", service.ConsoleProbeOutcome{ErrorCode: "fixture-secret"}, errors.New("fixture-secret"), "error", "probe_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, m := consoleMonitorFixture(t)
			require.NoError(t, db.Create(&model.Ability{ChannelId: 1, Group: "fixture-group", Model: m.Model, Enabled: true}).Error)
			m.ID, m.ChannelID, m.TargetKind, m.TargetRef = 0, 0, "group_route", "fixture-group"
			require.NoError(t, model.SaveConsoleMonitor(context.Background(), db, &m))
			require.NoError(t, model.SetConsoleMonitorAllowlisted(context.Background(), db, m.ID, true))
			require.NoError(t, model.SetConsoleMonitorSwitch(context.Background(), db, m.ID, "published", true))
			require.NoError(t, model.SetConsoleMonitorSwitch(context.Background(), db, m.ID, "enabled", true))
			require.NoError(t, model.SaveConsoleMonitorPolicy(context.Background(), db, model.ConsoleMonitorPolicy{ID: 1, Enabled: true, MaxConcurrent: 1, DailyRequests: 2, DailyCostMicros: 20, RetentionDays: 90}))
			adapter := &monitorMockAdapter{probe: func(context.Context, model.ConsoleMonitor) (service.ConsoleProbeOutcome, error) {
				return tc.outcome, tc.err
			}}
			e := service.ConsoleProbeExecutor{DB: db, Adapters: map[string]service.ConsoleProbeAdapter{"group_route": adapter}}
			require.NoError(t, e.Run(context.Background(), m.ID))
			var result model.ConsoleProbe
			require.NoError(t, db.First(&result).Error)
			assert.Equal(t, tc.status, result.Status)
			assert.Equal(t, tc.code, result.ErrorCode)
		})
	}
}

func TestConsoleMonitorAdminAuthorizationPublicationAndFailClosedExecution(t *testing.T) {
	db, m := consoleMonitorFixture(t)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedis, previousMemory := common.RedisEnabled, common.MemoryCacheEnabled
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled, common.MemoryCacheEnabled = false, false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled, common.MemoryCacheEnabled = previousRedis, previousMemory
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.AuditLog{}))
	for _, user := range []struct {
		id, role, status int
		token            string
	}{
		{1, common.RoleRootUser, common.UserStatusEnabled, "fixture-root-pat"},
		{2, common.RoleAdminUser, common.UserStatusEnabled, "fixture-admin-pat"},
		{3, common.RoleCommonUser, common.UserStatusEnabled, "fixture-user-pat"},
		{4, common.RoleRootUser, common.UserStatusDisabled, "fixture-disabled-pat"},
	} {
		token := user.token
		require.NoError(t, db.Create(&model.User{Id: user.id, Username: fmt.Sprintf("monitor-%d", user.id), Password: "unused-fixture", Role: user.role, Status: user.status, AuthVersion: 1, AccessToken: &token, AffCode: fmt.Sprintf("fixture-aff-%d", user.id)}).Error)
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	router.SetApiRouter(engine)
	request := func(method, path, token, contentType, marker string, body any) *httptest.ResponseRecorder {
		encoded, err := common.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest(method, path, bytes.NewReader(encoded))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("X-ClearAPI-Monitor", marker)
		req.Header.Set("New-Api-User", "1") // A client role/id claim is not authorization.
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		return response
	}
	base := "/api/console/monitor-admin"
	for _, token := range []string{"", "fixture-admin-pat", "fixture-user-pat", "fixture-disabled-pat"} {
		response := request(http.MethodGet, base, token, "application/json", "1", nil)
		assert.Contains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, response.Code)
		response = request(http.MethodPut, fmt.Sprintf("%s/%d/switch/published", base, m.ID), token, "application/json", "1", map[string]any{"value": true, "confirm": true})
		assert.Contains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, response.Code)
	}
	for _, input := range []struct{ contentType, marker string }{{"text/plain", "1"}, {"application/json", ""}} {
		response := request(http.MethodPut, fmt.Sprintf("%s/%d/switch/published", base, m.ID), "fixture-root-pat", input.contentType, input.marker, map[string]any{"value": true, "confirm": true})
		assert.Equal(t, http.StatusForbidden, response.Code)
	}
	response := request(http.MethodGet, base+"/targets", "fixture-root-pat", "application/json", "1", nil)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.NotContains(t, response.Body.String(), "fixture-secret")
	response = request(http.MethodPut, fmt.Sprintf("%s/%d/switch/published", base, m.ID), "fixture-root-pat", "application/json", "1", map[string]any{"value": true, "confirm": false})
	assert.Equal(t, http.StatusBadRequest, response.Code)
	response = request(http.MethodPut, fmt.Sprintf("%s/%d/switch/published", base, m.ID), "fixture-root-pat", "application/json", "1", map[string]any{"value": true, "confirm": true})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var saved model.ConsoleMonitor
	require.NoError(t, db.First(&saved, m.ID).Error)
	assert.True(t, saved.Published)
	assert.False(t, saved.Enabled)
	response = request(http.MethodPut, fmt.Sprintf("%s/%d/switch/enabled", base, m.ID), "fixture-root-pat", "application/json", "1", map[string]any{"value": true, "confirm": true})
	assert.Equal(t, http.StatusForbidden, response.Code, "the fixture uses unsupported openai_chat")
	response = request(http.MethodPost, fmt.Sprintf("%s/%d/probe", base, m.ID), "fixture-root-pat", "application/json", "1", map[string]any{})
	assert.Equal(t, http.StatusBadRequest, response.Code)
	response = request(http.MethodPost, fmt.Sprintf("%s/%d/probe", base, m.ID), "fixture-root-pat", "application/json", "1", map[string]any{"confirm": true})
	assert.Equal(t, http.StatusForbidden, response.Code)
	assert.Contains(t, response.Body.String(), "MONITOR_TARGET_DENIED", "the fixture uses the unsupported openai_chat transport")
	var recoverable model.ConsoleMonitorAttempt
	require.NoError(t, db.Create(&model.ConsoleMonitorAttempt{MonitorID: m.ID, StartedAt: time.Now().Unix(), CleanupState: "cleanup_reconciling", ReservedCostMicros: 10}).Error)
	require.NoError(t, db.Last(&recoverable).Error)
	response = request(http.MethodPost, fmt.Sprintf("%s/attempts/%d/recover", base, recoverable.ID), "fixture-root-pat", "application/json", "1", map[string]any{"reason": "fixture worker restart", "confirm": true})
	assert.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var recovered model.ConsoleMonitorAttempt
	require.NoError(t, db.First(&recovered, recoverable.ID).Error)
	assert.Equal(t, "cleanup_pending", recovered.CleanupState)
	var attempts int64
	require.NoError(t, db.Model(&model.ConsoleMonitorAttempt{}).Count(&attempts).Error)
	assert.EqualValues(t, 1, attempts)
	var audit model.AuditLog
	require.NoError(t, db.Where("action = ?", "monitor.switch").First(&audit).Error)
	assert.Equal(t, 1, audit.UserId)
	audit = model.AuditLog{}
	require.NoError(t, db.Where("action = ?", "monitor.attempt_recover").First(&audit).Error)
	assert.Equal(t, "monitor.attempt_recover", audit.Action)
	assert.NotContains(t, audit.Content, "fixture worker restart")

	t.Run("allowlist management keeps execution and accounting isolated", func(t *testing.T) {
		base := "/api/console/monitor-targets"
		input := map[string]any{"target_type": "channel", "target_ref": "1", "channel_id": 1, "model": "model-b", "protocol": "openai_chat", "display_name": "Allowlist target", "interval_seconds": 600, "timeout_seconds": 30, "stale_seconds": 900, "enabled": true, "published": true, "created_by": 999, "created_at": 1, "daily_requests": 100, "request_cost_micros": 999, "max_concurrency": 32, "daily_budget_limit": 999}
		for _, token := range []string{"", "fixture-admin-pat", "fixture-user-pat", "fixture-disabled-pat"} {
			for _, route := range []struct{ method, path string }{{http.MethodGet, base}, {http.MethodPost, base}, {http.MethodPut, base}, {http.MethodPut, base + "/1/switch/enabled"}, {http.MethodPut, base + "/1/switch/published"}} {
				response := request(route.method, route.path, token, "application/json", "1", input)
				assert.Contains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, response.Code)
			}
		}
		for _, marker := range []string{"", "1"} {
			contentType := "text/plain"
			if marker == "" {
				contentType = "application/json"
			}
			assert.Equal(t, http.StatusForbidden, request(http.MethodPost, base, "fixture-root-pat", contentType, marker, input).Code)
		}
		var policyBefore model.ConsoleMonitorPolicy
		require.NoError(t, db.First(&policyBefore, 1).Error)
		response := request(http.MethodPost, base, "fixture-root-pat", "application/json", "1", input)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var target model.ConsoleMonitor
		require.NoError(t, db.Where("model = ?", "model-b").First(&target).Error)
		assert.False(t, target.Enabled)
		assert.False(t, target.Allowlisted)
		assert.False(t, target.Published)
		assert.Equal(t, 1, target.CreatedBy)
		assert.Greater(t, target.CreatedAt, int64(1))
		assert.Zero(t, target.DailyRequests)
		assert.Zero(t, target.RequestCostMicros)
		original := target
		response = request(http.MethodPost, base, "fixture-root-pat", "application/json", "1", input)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var count int64
		require.NoError(t, db.Model(&model.ConsoleMonitor{}).Where("model = ?", "model-b").Count(&count).Error)
		assert.EqualValues(t, 1, count)
		for _, change := range []struct {
			action string
			value  bool
		}{{"enabled", true}, {"enabled", true}, {"published", true}, {"published", true}, {"published", false}, {"published", false}, {"enabled", false}, {"enabled", false}} {
			response = request(http.MethodPut, fmt.Sprintf("%s/%d/switch/%s", base, target.ID, change.action), "fixture-root-pat", "application/json", "1", map[string]any{"value": change.value, "confirm": true})
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.NoError(t, db.First(&target, target.ID).Error)
			assert.False(t, target.Enabled)
			if change.action == "enabled" {
				assert.Equal(t, change.value, target.Allowlisted)
			} else {
				assert.Equal(t, change.value, target.Published)
			}
		}
		input["id"], input["display_name"], input["timeout_seconds"] = target.ID, "Updated target", 45
		require.NoError(t, db.Model(&target).Updates(map[string]any{"daily_requests": 2, "request_cost_micros": 10}).Error)
		response = request(http.MethodPut, base, "fixture-root-pat", "application/json", "1", input)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		require.NoError(t, db.First(&target, target.ID).Error)
		assert.Equal(t, "Updated target", target.DisplayName)
		assert.EqualValues(t, 45, target.TimeoutSeconds)
		assert.Equal(t, original.CreatedAt, target.CreatedAt)
		assert.Equal(t, original.CreatedBy, target.CreatedBy)
		assert.EqualValues(t, 10, target.RequestCostMicros)
		assert.EqualValues(t, 2, target.DailyRequests)
		input["model"] = "gpt-6-luna"
		assert.Equal(t, http.StatusForbidden, request(http.MethodPut, base, "fixture-root-pat", "application/json", "1", input).Code)
		response = request(http.MethodGet, base, "fixture-root-pat", "application/json", "1", nil)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		assert.Contains(t, response.Body.String(), `"created_by":1`)
		assert.Contains(t, response.Body.String(), `"max_concurrency":1`)
		assert.Contains(t, response.Body.String(), `"real_probe_ready":false`)
		var policyAfter model.ConsoleMonitorPolicy
		require.NoError(t, db.First(&policyAfter, 1).Error)
		assert.Equal(t, policyBefore, policyAfter)
		var attemptsAfter int64
		require.NoError(t, db.Model(&model.ConsoleMonitorAttempt{}).Count(&attemptsAfter).Error)
		assert.Equal(t, attempts, attemptsAfter)
		var channel model.Channel
		require.NoError(t, db.First(&channel, 1).Error)
		assert.Equal(t, common.ChannelStatusEnabled, channel.Status)
		var user model.User
		require.NoError(t, db.First(&user, 1).Error)
		assert.Zero(t, user.Quota)
		assert.Zero(t, user.UsedQuota)
	})
}

func TestConsoleMonitorCLIDryRunAndSeparateApprovals(t *testing.T) {
	db, m := consoleMonitorFixture(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	var database string
	require.NoError(t, db.Raw("SELECT file FROM pragma_database_list WHERE name = 'main'").Scan(&database).Error)
	require.NoError(t, db.Migrator().DropTable(&model.ConsoleMonitorAttempt{}, &model.ConsoleMonitorPolicy{}, &model.ConsoleProbe{}, &model.ConsoleMonitor{}))
	binary := filepath.Join(t.TempDir(), "monitor-init")
	build := exec.Command("go", "build", "-o", binary, "../cmd/monitor-init")
	output, err := build.CombinedOutput()
	require.NoError(t, err, string(output))
	catalog := filepath.Join(t.TempDir(), "catalog.json")
	m.ID, m.Published = 0, true
	encoded, err := common.Marshal([]model.ConsoleMonitor{m})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(catalog, encoded, 0600))
	base := []string{"--dialect", "sqlite", "--database", database, "--migrate", "--catalog", catalog}
	for _, tc := range []struct {
		name    string
		flags   []string
		allowed bool
	}{
		{"default dry run", nil, true},
		{"apply without confirmation", []string{"--apply"}, false},
		{"migration approval only", []string{"--apply", "--confirm-database", database, "--approve-monitor-migration"}, false},
		{"different database confirmation", []string{"--apply", "--confirm-database", "other", "--approve-monitor-migration", "--approve-monitor-publication"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(binary, append(append([]string{}, base...), tc.flags...)...)
			cmd.Env = append(os.Environ(), "CLEARAPI_MONITOR_DSN="+database)
			output, err := cmd.CombinedOutput()
			if tc.allowed {
				require.NoError(t, err, string(output))
				assert.Contains(t, string(output), "DRY RUN")
			} else {
				require.Error(t, err)
			}
			assert.False(t, db.Migrator().HasTable(&model.ConsoleMonitor{}))
		})
	}
	for range 2 {
		cmd := exec.Command(binary, append(append([]string{}, base...), "--apply", "--confirm-database", database, "--approve-monitor-migration", "--approve-monitor-publication")...)
		cmd.Env = append(os.Environ(), "CLEARAPI_MONITOR_DSN="+database)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, string(output))
	}
	var monitors []model.ConsoleMonitor
	require.NoError(t, db.Find(&monitors).Error)
	require.Len(t, monitors, 1)
	assert.True(t, monitors[0].Published)
	assert.False(t, monitors[0].Enabled)
	assert.Equal(t, 1, monitors[0].ChannelID)
	assert.Equal(t, "gpt-6-luna", monitors[0].Model)
	var count int64
	require.NoError(t, db.Model(&model.ConsoleProbe{}).Count(&count).Error)
	assert.Zero(t, count)
	// Keep the fixture connection alive until all subprocess inspection is done.
	require.NoError(t, sqlDB.Ping())
}
