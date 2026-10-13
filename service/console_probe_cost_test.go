package service

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Synthetic evidence is constructed only in _test.go. No exported setter,
// registry, env var or database field can install this in runtime execution.
func syntheticConsoleProbeCostPolicy(now time.Time) consoleProbeCostPolicy {
	p := consoleProbeProductionCostPolicy()
	p.HardCeilingMicros = 200
	p.PricingSource = "synthetic-test-only"
	p.proof = &consoleProbeCostProof{Version: p.Version, Payload: consoleProbePayload,
		InputTokens: 4, InputMicros: 1, OutputMicros: 2, FixedFeeMicros: 4,
		ValidFrom: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}
	return p
}

func consoleProbeCostTarget() model.ConsoleMonitor {
	return model.ConsoleMonitor{ChannelID: 1, TargetKind: "channel", TargetRef: "1", Model: "gpt-6-luna", Protocol: "openai_responses"}
}

func TestConsoleProbeWorstCaseCost(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		change func(*consoleProbeCostPolicy, *model.ConsoleMonitor, *string)
		cost   int64
		err    error
	}{
		{"below", nil, 100, nil},
		{"equal", func(p *consoleProbeCostPolicy, _ *model.ConsoleMonitor, _ *string) { p.HardCeilingMicros = 100 }, 100, nil},
		{"exceed", func(p *consoleProbeCostPolicy, _ *model.ConsoleMonitor, _ *string) { p.HardCeilingMicros = 99 }, 100, ErrMonitorCostExceeded},
		{"missing", func(p *consoleProbeCostPolicy, _ *model.ConsoleMonitor, _ *string) { *p = consoleProbeCostPolicy{} }, 0, ErrMonitorCostUnverified},
		{"production", func(p *consoleProbeCostPolicy, _ *model.ConsoleMonitor, _ *string) {
			*p = consoleProbeProductionCostPolicy()
		}, 0, ErrMonitorCostUnverified},
		{"expired", func(p *consoleProbeCostPolicy, _ *model.ConsoleMonitor, _ *string) { p.proof.ExpiresAt = now }, 0, ErrMonitorCostUnverified},
		{"future", func(p *consoleProbeCostPolicy, _ *model.ConsoleMonitor, _ *string) {
			p.proof.ValidFrom = now.Add(time.Second)
		}, 0, ErrMonitorCostUnverified},
		{"version", func(p *consoleProbeCostPolicy, _ *model.ConsoleMonitor, _ *string) { p.proof.Version = "unknown" }, 0, ErrMonitorCostUnverified},
		{"policy-version", func(p *consoleProbeCostPolicy, _ *model.ConsoleMonitor, _ *string) { p.Version = "unknown" }, 0, ErrMonitorCostUnverified},
		{"input-limit", func(p *consoleProbeCostPolicy, _ *model.ConsoleMonitor, _ *string) {
			p.proof.InputTokens = p.InputTokenLimit + 1
		}, 0, ErrMonitorCostUnverified},
		{"output-limit", func(p *consoleProbeCostPolicy, _ *model.ConsoleMonitor, _ *string) { p.OutputTokenLimit = 17 }, 0, ErrMonitorCostUnverified},
		{"unknown-model", func(_ *consoleProbeCostPolicy, m *model.ConsoleMonitor, _ *string) { m.Model = "unknown" }, 0, ErrMonitorCostUnverified},
		{"channel", func(_ *consoleProbeCostPolicy, m *model.ConsoleMonitor, _ *string) { m.ChannelID = 2 }, 0, ErrMonitorCostUnverified},
		{"protocol", func(_ *consoleProbeCostPolicy, m *model.ConsoleMonitor, _ *string) { m.Protocol = "openai_chat" }, 0, ErrMonitorCostUnverified},
		{"payload", func(_ *consoleProbeCostPolicy, _ *model.ConsoleMonitor, body *string) { *body = `{}` }, 0, ErrMonitorCostUnverified},
		{"overflow-multiply", func(p *consoleProbeCostPolicy, _ *model.ConsoleMonitor, _ *string) {
			p.proof.InputMicros = math.MaxInt64
		}, 0, ErrMonitorCostUnverified},
		{"overflow-add", func(p *consoleProbeCostPolicy, _ *model.ConsoleMonitor, _ *string) {
			p.proof.FixedFeeMicros = math.MaxInt64
		}, 0, ErrMonitorCostUnverified},
		{"negative", func(p *consoleProbeCostPolicy, _ *model.ConsoleMonitor, _ *string) { p.proof.OutputMicros = -1 }, 0, ErrMonitorCostUnverified},
		{"source-missing", func(p *consoleProbeCostPolicy, _ *model.ConsoleMonitor, _ *string) { p.PricingSource = "" }, 0, ErrMonitorCostUnverified},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, m, body := syntheticConsoleProbeCostPolicy(now), consoleProbeCostTarget(), consoleProbePayload
			if tc.change != nil {
				tc.change(&p, &m, &body)
			}
			cost, err := p.WorstCaseCost(m, body, now)
			assert.Equal(t, tc.cost, cost)
			if tc.err == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.err)
			}
		})
	}
}

func TestConsoleProbeLocalTransport(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		expected error
	}{
		{"success", 200, `{"status":"completed"}`, nil},
		{"http-error-no-retry", 503, "unavailable", errConsoleProbeTransport},
		{"redirect", 307, "", errConsoleProbeTransport},
		{"response-limit", 200, strings.Repeat("x", (1<<20)+1), ErrMonitorLeaseUnresolved},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls, redirects atomic.Int64
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/redirected" {
					redirects.Add(1)
					w.WriteHeader(200)
					return
				}
				calls.Add(1)
				assert.Equal(t, "HTTP/1.1", r.Proto)
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Empty(t, r.Header.Get("Authorization"))
				body, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				assert.Equal(t, consoleProbePayload, string(body))
				w.Header().Set("Location", "/redirected")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			body, err := sendConsoleProbe(context.Background(), consoleProbeCostTarget(), syntheticConsoleProbeCostPolicy(time.Now()), srv.URL+"/v1/responses", time.Second)
			if tc.expected == nil {
				require.NoError(t, err)
				assert.Equal(t, tc.body, string(body))
			} else {
				require.ErrorIs(t, err, tc.expected)
			}
			assert.EqualValues(t, 1, calls.Load())
			assert.Zero(t, redirects.Load())
		})
	}

	t.Run("unverified-no-request", func(t *testing.T) {
		var calls atomic.Int64
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
		defer srv.Close()
		_, err := sendConsoleProbe(context.Background(), consoleProbeCostTarget(), consoleProbeProductionCostPolicy(), srv.URL+"/v1/responses", time.Second)
		require.ErrorIs(t, err, ErrMonitorCostUnverified)
		assert.Zero(t, calls.Load())
	})
	t.Run("non-local-rejected", func(t *testing.T) {
		_, err := sendConsoleProbe(context.Background(), consoleProbeCostTarget(), syntheticConsoleProbeCostPolicy(time.Now()), "http://192.0.2.1/v1/responses", time.Second)
		require.ErrorIs(t, err, ErrMonitorCostUnverified)
	})
	t.Run("connection-failure-no-retry", func(t *testing.T) {
		var calls atomic.Int64
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			_, _ = io.Copy(io.Discard, r.Body)
			conn, _, err := w.(http.Hijacker).Hijack()
			if assert.NoError(t, err) {
				_ = conn.Close()
			}
		}))
		defer srv.Close()
		_, err := sendConsoleProbe(context.Background(), consoleProbeCostTarget(), syntheticConsoleProbeCostPolicy(time.Now()), srv.URL+"/v1/responses", time.Second)
		require.ErrorIs(t, err, ErrMonitorLeaseUnresolved)
		assert.EqualValues(t, 1, calls.Load())
	})
	t.Run("canceled-before-dispatch", func(t *testing.T) {
		var calls atomic.Int64
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
		defer srv.Close()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := sendConsoleProbe(ctx, consoleProbeCostTarget(), syntheticConsoleProbeCostPolicy(time.Now()), srv.URL+"/v1/responses", time.Second)
		require.ErrorIs(t, err, context.Canceled)
		assert.Zero(t, calls.Load())
	})
	for _, name := range []string{"timeout", "cancellation"} {
		t.Run(name, func(t *testing.T) {
			started, stopped := make(chan struct{}), make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				close(started)
				<-r.Context().Done()
				close(stopped)
			}))
			defer srv.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			timeout := 5 * time.Second
			if name == "timeout" {
				timeout = 100 * time.Millisecond
			}
			done := make(chan error, 1)
			go func() {
				_, err := sendConsoleProbe(ctx, consoleProbeCostTarget(), syntheticConsoleProbeCostPolicy(time.Now()), srv.URL+"/v1/responses", timeout)
				done <- err
			}()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("request did not start")
			}
			if name == "cancellation" {
				cancel()
			}
			select {
			case err := <-done:
				require.ErrorIs(t, err, ErrMonitorLeaseUnresolved)
				if name == "timeout" {
					assert.ErrorIs(t, err, context.DeadlineExceeded)
				} else {
					assert.ErrorIs(t, err, context.Canceled)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("transport did not stop")
			}
			select {
			case <-stopped:
			case <-time.After(5 * time.Second):
				t.Fatal("server did not observe cancellation")
			}
		})
	}
}

type consoleCostTestAdapter struct{ calls int }

func (*consoleCostTestAdapter) Authorize(context.Context, model.ConsoleMonitor) error {
	panic("cost-gated target must never reach adapter authorization")
}
func (a *consoleCostTestAdapter) Probe(_ context.Context, m model.ConsoleMonitor, _ int64) (ConsoleProbeOutcome, error) {
	a.calls++
	return ConsoleProbeOutcome{Status: "normal", SourceChannelID: m.ChannelID}, nil
}

func TestConsoleProbeCostDenialPrecedesDeadline(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code string
	}{
		{"unverified", ErrMonitorCostUnverified, "MONITOR_COST_UNVERIFIED"},
		{"exceeded", ErrMonitorCostExceeded, "MONITOR_COST_EXCEEDED"},
		{"wrapped-unverified", errors.Join(ErrMonitorCostUnverified, context.DeadlineExceeded), "MONITOR_COST_UNVERIFIED"},
		{"wrapped-exceeded", errors.Join(ErrMonitorCostExceeded, context.DeadlineExceeded), "MONITOR_COST_EXCEEDED"},
		{"timeout", context.DeadlineExceeded, "probe_timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.code, consoleProbeFailureCode(tc.err, context.DeadlineExceeded))
		})
	}
}

// Expire the caller only after the admission transaction has committed.
// This models the deadline race without sleeps or changing the cost policy.
type consoleCostDeadlineContext struct {
	context.Context
	done chan struct{}
	once sync.Once
}

func (c *consoleCostDeadlineContext) Done() <-chan struct{} { return c.done }
func (c *consoleCostDeadlineContext) Err() error {
	select {
	case <-c.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}
func (c *consoleCostDeadlineContext) expire() { c.once.Do(func() { close(c.done) }) }

type consoleCostDeadlinePool struct {
	*sql.DB
	expire func()
}

func (p consoleCostDeadlinePool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := p.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &consoleCostDeadlineTx{Tx: tx, expire: p.expire}, nil
}

type consoleCostDeadlineTx struct {
	*sql.Tx
	expire func()
}

func (tx consoleCostDeadlineTx) Commit() error {
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	tx.expire()
	return nil
}

func TestConsoleProbeCostExecutorMatrix(t *testing.T) {
	for _, tc := range []struct {
		name    string
		dialect gorm.Dialector
		kind    common.DatabaseType
	}{
		{"sqlite", sqlite.Open(":memory:"), common.DatabaseTypeSQLite},
		{"postgres", postgres.New(postgres.Config{DSN: os.Getenv("PROBE_COST_POSTGRES_DSN"), PreferSimpleProtocol: true}), common.DatabaseTypePostgreSQL},
		{"mysql", mysql.Open(os.Getenv("PROBE_COST_MYSQL_DSN")), common.DatabaseTypeMySQL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "postgres" && os.Getenv("PROBE_COST_POSTGRES_DSN") == "" || tc.name == "mysql" && os.Getenv("PROBE_COST_MYSQL_DSN") == "" {
				t.Skip("isolated database not configured")
			}
			db, err := gorm.Open(tc.dialect, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			versionQuery := "SELECT version()"
			if tc.name == "sqlite" {
				versionQuery = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
			t.Logf("database version: %s", version)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			defer sqlDB.Close()
			old := common.MainDatabaseType()
			common.SetMainDatabaseType(tc.kind)
			defer common.SetMainDatabaseType(old)
			require.False(t, db.Migrator().HasTable(&model.Channel{}), "dedicated empty database required")
			require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
			require.NoError(t, model.MigrateConsoleMonitoringDB(db))
			require.NoError(t, model.MigrateConsoleMonitoringDB(db))
			channel := model.Channel{Id: 1, Name: "cost-test-only", Key: "fake-only", Models: "gpt-6-luna", Status: common.ChannelStatusEnabled}
			require.NoError(t, db.Create(&channel).Error)
			var before model.Channel
			require.NoError(t, db.First(&before, 1).Error)
			tablesBefore, err := db.Migrator().GetTables()
			require.NoError(t, err)
			m := consoleProbeCostTarget()
			m.DisplayName, m.IntervalSeconds, m.TimeoutSeconds, m.StaleSeconds = "cost probe", 60, 1, 900
			m.DailyRequests, m.RequestCostMicros = 10, 100
			ctx := context.Background()
			require.NoError(t, model.SaveConsoleMonitor(ctx, db, &m))
			require.NoError(t, model.SetConsoleMonitorAllowlisted(ctx, db, m.ID, true))
			require.NoError(t, model.SetConsoleMonitorSwitch(ctx, db, m.ID, "published", true))
			require.NoError(t, model.SetConsoleMonitorSwitch(ctx, db, m.ID, "enabled", true))
			require.NoError(t, model.SaveConsoleMonitorPolicy(ctx, db, model.ConsoleMonitorPolicy{ID: 1, Enabled: true, MaxConcurrent: 1, DailyRequests: 10, DailyCostMicros: 1000, RetentionDays: 90}))
			adapter := &consoleCostTestAdapter{}
			for _, run := range []struct {
				registered bool
				deadline   bool
			}{{false, false}, {true, false}, {true, true}} {
				e := ConsoleProbeExecutor{DB: db}
				if run.registered {
					e.Adapters = map[string]ConsoleProbeAdapter{"channel": adapter}
				}
				runCtx := ctx
				if run.deadline {
					deadline := &consoleCostDeadlineContext{Context: ctx, done: make(chan struct{})}
					runCtx = deadline
					pool := consoleCostDeadlinePool{DB: sqlDB, expire: deadline.expire}
					e.DB = db.Session(&gorm.Session{NewDB: true})
					e.DB.ConnPool = pool
					e.DB.Statement = &gorm.Statement{DB: e.DB, ConnPool: pool, Context: ctx}
				}
				result, err := e.RunWithResult(runCtx, m.ID)
				require.NoError(t, err)
				if run.deadline {
					require.ErrorIs(t, runCtx.Err(), context.DeadlineExceeded)
				}
				require.Positive(t, result.AttemptID)
				require.Positive(t, result.ProbeID)
				assert.Equal(t, "error", result.Status)
				assert.Equal(t, "MONITOR_COST_UNVERIFIED", result.ErrorCode)
				var attempt model.ConsoleMonitorAttempt
				var probe model.ConsoleProbe
				require.NoError(t, db.First(&attempt, result.AttemptID).Error)
				require.NoError(t, db.First(&probe, result.ProbeID).Error)
				assert.Equal(t, m.ID, attempt.MonitorID)
				assert.True(t, attempt.Failure)
				assert.Positive(t, attempt.FinishedAt)
				assert.Equal(t, "released", attempt.CleanupState)
				probe.SafeResult()
				assert.Equal(t, result.ErrorCode, probe.ErrorCode)
				assert.Equal(t, m.ID, probe.MonitorID)
				require.NoError(t, db.Model(&attempt).Update("started_at", time.Now().Add(-time.Hour).Unix()).Error)
			}
			assert.Zero(t, adapter.calls, "no provider/relay/pool execution")
			var after model.Channel
			require.NoError(t, db.First(&after, 1).Error)
			assert.Equal(t, before, after, "channel must not mutate")
			tablesAfter, err := db.Migrator().GetTables()
			require.NoError(t, err)
			assert.ElementsMatch(t, tablesBefore, tablesAfter, "no billing/finance tables created")
			var attempts, probes int64
			require.NoError(t, db.Model(&model.ConsoleMonitorAttempt{}).Count(&attempts).Error)
			require.NoError(t, db.Model(&model.ConsoleProbe{}).Count(&probes).Error)
			assert.EqualValues(t, 3, attempts)
			assert.EqualValues(t, 3, probes)
		})
	}
}
