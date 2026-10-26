package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
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

type configuredConsoleAdapter struct{ db *gorm.DB }

func (a configuredConsoleAdapter) Authorize(ctx context.Context, m model.ConsoleMonitor) error {
	return AuthorizeConsoleProbeCost(ctx, a.db, m)
}
func (a configuredConsoleAdapter) Probe(ctx context.Context, m model.ConsoleMonitor, id int64) (ConsoleProbeOutcome, error) {
	return ProbeConsoleChannel(ctx, a.db, m, id)
}

// These are HTTP/DB contract tests against an explicitly synthetic provider.
// They are not evidence of a real vendor call or its pricing/limit contract.
func TestConsoleProbeConfiguredExecutionMatrix(t *testing.T) {
	for _, engine := range []struct {
		name    string
		dialect gorm.Dialector
		kind    common.DatabaseType
	}{
		{"sqlite", sqlite.Open(":memory:"), common.DatabaseTypeSQLite},
		{"mysql", mysql.Open(os.Getenv("MONITOR_RUNTIME_MYSQL_DSN")), common.DatabaseTypeMySQL},
		{"postgres", postgres.New(postgres.Config{DSN: os.Getenv("MONITOR_RUNTIME_POSTGRES_DSN"), PreferSimpleProtocol: true}), common.DatabaseTypePostgreSQL},
	} {
		t.Run(engine.name, func(t *testing.T) {
			if engine.name != "sqlite" && os.Getenv("MONITOR_RUNTIME_"+strings.ToUpper(engine.name)+"_DSN") == "" {
				t.Skip("dedicated database not configured")
			}
			db, err := gorm.Open(engine.dialect, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			defer sqlDB.Close()
			sqlDB.SetMaxOpenConns(1)
			old := common.MainDatabaseType()
			common.SetMainDatabaseType(engine.kind)
			defer common.SetMainDatabaseType(old)
			require.False(t, db.Migrator().HasTable(&model.ConsoleMonitor{}))
			require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.Option{}))
			require.NoError(t, model.MigrateConsoleMonitoringDB(db))
			require.NoError(t, model.MigrateConsoleMonitoringDB(db))
			var calls atomic.Int64
			var mode atomic.Value
			mode.Store("normal")
			started, release := make(chan struct{}, 1), make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				assert.Equal(t, "Bearer synthetic-only", r.Header.Get("Authorization"))
				assert.Contains(t, r.Header.Get("X-Client-Request-Id"), "clearapi-monitor-")
				body, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				assert.Equal(t, consoleProbePayload, string(body))
				switch mode.Load().(string) {
				case "hold":
					started <- struct{}{}
					<-release
				case "timeout":
					<-r.Context().Done()
					return
				case "missing-usage":
					_, _ = io.WriteString(w, `{"id":"resp-test","status":"completed","model":"gpt-6-luna"}`)
					return
				case "incomplete":
					_, _ = io.WriteString(w, `{"id":"resp-incomplete","status":"incomplete","model":"gpt-6-luna","usage":{"input_tokens":4,"output_tokens":2}}`)
					return
				}
				_, _ = fmt.Fprintf(w, `{"id":"resp-%d","status":"completed","model":"gpt-6-luna","usage":{"input_tokens":4,"output_tokens":2}}`, n)
			}))
			defer srv.Close()
			channel := model.Channel{Id: 1, Name: "synthetic only", Key: "synthetic-only", Type: 1, Models: "gpt-6-luna", Status: common.ChannelStatusEnabled, BaseURL: &srv.URL}
			require.NoError(t, db.Create(&channel).Error)
			m := consoleProbeCostTarget()
			m.DisplayName = "synthetic"
			m.IntervalSeconds = 60
			m.TimeoutSeconds = 1
			m.StaleSeconds = 900
			m.DailyRequests = 10
			m.RequestCostMicros = 100
			ctx := context.Background()
			require.NoError(t, model.SaveConsoleMonitor(ctx, db, &m))
			require.NoError(t, model.SetConsoleMonitorAllowlisted(ctx, db, m.ID, true))
			require.NoError(t, model.SetConsoleMonitorSwitch(ctx, db, m.ID, "published", true))
			require.NoError(t, model.SetConsoleMonitorSwitch(ctx, db, m.ID, "enabled", true))
			require.NoError(t, model.SaveConsoleMonitorPolicy(ctx, db, model.ConsoleMonitorPolicy{ID: 1, Enabled: true, MaxConcurrent: 1, DailyRequests: 10, DailyCostMicros: 200, RetentionDays: 90}))
			contract := ConsoleProbeCostContract{Source: "synthetic test only, not a vendor quote", ValidFrom: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour), InputTokenLimit: 64, InputMicros: 1, OutputMicros: 2, FixedFeeMicros: 4, HardCeilingMicros: 100}
			require.NoError(t, SaveConsoleProbeCostContract(ctx, db, m, contract))
			require.NoError(t, db.Model(&channel).Update("key", "rotated-only").Error)
			require.ErrorIs(t, AuthorizeConsoleProbeCost(ctx, db, m), ErrMonitorCostUnverified)
			require.Zero(t, calls.Load())
			require.NoError(t, db.Model(&channel).Update("key", "synthetic-only").Error)
			expired := contract
			expired.ExpiresAt = time.Now().Add(-time.Minute)
			require.ErrorIs(t, SaveConsoleProbeCostContract(ctx, db, m, expired), ErrMonitorCostUnverified)
			e := ConsoleProbeExecutor{DB: db, Adapters: map[string]ConsoleProbeAdapter{"channel": configuredConsoleAdapter{db}}}
			result, err := e.RunWithResult(ctx, m.ID)
			require.NoError(t, err)
			require.Equal(t, "normal", result.Status, "%+v", result)
			require.NotNil(t, result.MeteredCostMicros)
			assert.EqualValues(t, 12, *result.MeteredCostMicros)
			var attempt model.ConsoleMonitorAttempt
			require.NoError(t, db.First(&attempt, result.AttemptID).Error)
			assert.Equal(t, "resp-1", result.UpstreamResponseID)
			assert.EqualValues(t, 100, attempt.ReservedCostMicros)
			for i := range 2 {
				require.NoError(t, db.Model(&model.ConsoleMonitorAttempt{}).Where("monitor_id = ?", m.ID).Update("started_at", time.Now().Add(-time.Hour).Unix()).Error)
				_, err = e.RunWithResult(ctx, m.ID)
				if i == 0 {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, model.ErrMonitorBudget)
				}
			}
			assert.EqualValues(t, 2, calls.Load(), "budget prevents a third HTTP request")
			for _, scenario := range []string{"incomplete", "concurrent", "missing-usage", "timeout"} {
				t.Run(scenario, func(t *testing.T) {
					require.NoError(t, db.Where("id > 0").Delete(&model.ConsoleMonitorAttempt{}).Error)
					before := calls.Load()
					if scenario == "incomplete" {
						mode.Store(scenario)
						result, err := e.RunWithResult(ctx, m.ID)
						require.NoError(t, err)
						assert.Equal(t, "degraded", result.Status)
						assert.Equal(t, "probe_incomplete", result.ErrorCode)
					} else if scenario == "concurrent" {
						mode.Store("hold")
						done := make(chan error, 1)
						go func() { _, err := e.RunWithResult(ctx, m.ID); done <- err }()
						<-started
						_, err := e.RunWithResult(ctx, m.ID)
						require.ErrorIs(t, err, model.ErrMonitorBudget)
						close(release)
						require.NoError(t, <-done)
					} else {
						mode.Store(scenario)
						result, err := e.RunWithResult(ctx, m.ID)
						require.ErrorIs(t, err, ErrMonitorLeaseUnresolved)
						assert.Equal(t, "unknown", result.Status)
						_, err = e.RunWithResult(ctx, m.ID)
						require.ErrorIs(t, err, model.ErrMonitorBudget)
					}
					assert.Equal(t, before+1, calls.Load())
				})
			}
		})
	}
}

// This private transport fixture is not installed as a production contract.
const consoleProbePayload = `{"input":"monitor probe","max_output_tokens":16,"model":"gpt-6-luna","store":false,"stream":false}`

func syntheticConsoleProbeCostPolicy(now time.Time) consoleProbeCostPolicy {
	p := consoleProbeProductionCostPolicy()
	p.ChannelID, p.Model, p.Protocol = 1, "gpt-6-luna", "openai_responses"
	p.payload, p.contentType, p.keyHeader = consoleProbePayload, "application/json", "Authorization"
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

func TestConsoleProbeArbitraryModelsAndNativeProtocols(t *testing.T) {
	for _, tc := range []struct {
		protocol, path, contentType, response, responseType, successPath string
		poll                                                             bool
	}{
		{"openai_chat", "/v1/chat/completions", "", `{"id":"chat-receipt","model":"any-provider-model","usage":{"prompt_tokens":4,"completion_tokens":2}}`, "application/json", "", false},
		{"openai_responses", "/v1/responses", "", `{"id":"response-receipt","model":"any-provider-model","status":"completed","usage":{"input_tokens":4,"output_tokens":2}}`, "application/json", "", false},
		{"anthropic", "/v1/messages", "", `{"id":"claude-receipt","model":"any-provider-model","usage":{"input_tokens":4,"output_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":2}}`, "application/json", "", false},
		{"gemini", "/v1beta/models/any-provider-model:generateContent", "", `{"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":2,"thoughtsTokenCount":3}}`, "application/json", "", false},
		{"image-generation", "/v1/images/generations", "application/json", `{"data":[{"url":"https://example.invalid/synthetic.png"}]}`, "application/json", "data.0.url", false},
		{"audio-speech", "/v1/audio/speech", "application/json", "RIFF-synthetic-WAVE", "audio/wav", "", false},
		{"audio-transcription", "/v1/audio/transcriptions", "multipart/form-data; boundary=probe", `{"text":"synthetic transcript"}`, "application/json", "text", false},
		{"video-generation", "/v1/videos", "application/json", `{"id":"synthetic-job","status":"queued"}`, "application/json", "", true},
		{"custom-protocol", "/vendor/native", "application/json", `{"ok":true}`, "application/json", "ok", false},
		{"native-html", "/vendor/native", "application/json", "<html>not an API response</html>", "text/html", "", false},
		{"video-failed", "/v1/videos", "application/json", `{"id":"synthetic-job","status":"queued"}`, "application/json", "", true},
	} {
		t.Run(tc.protocol, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			connection, err := db.DB()
			require.NoError(t, err)
			connection.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, connection.Close()) })
			previous := common.MainDatabaseType()
			common.SetMainDatabaseType(common.DatabaseTypeSQLite)
			t.Cleanup(func() { common.SetMainDatabaseType(previous) })
			require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Option{}))
			require.NoError(t, model.MigrateConsoleMonitoringDB(db))
			var calls atomic.Int64
			sample := `{"model":"any-provider-model","input":"monitor probe"}`
			if tc.protocol == "image-generation" {
				sample = `{"model":"any-provider-model","prompt":"monitor probe","n":1,"size":"1024x1024"}`
			}
			if tc.protocol == "audio-speech" {
				sample = `{"model":"any-provider-model","input":"monitor probe","voice":"alloy","response_format":"wav"}`
			}
			if tc.poll {
				sample = `{"model":"any-provider-model","prompt":"monitor probe","seconds":"4","size":"720x1280"}`
			}
			// Valid one-second mono PCM fixture; no speech or supplier media processing
			// is claimed by these synthetic HTTP contract tests.
			wav := make([]byte, 44+16000)
			copy(wav, "RIFF")
			binary.LittleEndian.PutUint32(wav[4:8], uint32(len(wav)-8))
			copy(wav[8:], "WAVEfmt ")
			binary.LittleEndian.PutUint32(wav[16:20], 16)
			binary.LittleEndian.PutUint16(wav[20:22], 1)
			binary.LittleEndian.PutUint16(wav[22:24], 1)
			binary.LittleEndian.PutUint32(wav[24:28], 8000)
			binary.LittleEndian.PutUint32(wav[28:32], 16000)
			binary.LittleEndian.PutUint16(wav[32:34], 2)
			binary.LittleEndian.PutUint16(wav[34:36], 16)
			copy(wav[36:], "data")
			binary.LittleEndian.PutUint32(wav[40:44], 16000)
			if tc.protocol == "audio-transcription" {
				sample = "--probe\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\nany-provider-model\r\n--probe\r\nContent-Disposition: form-data; name=\"file\"; filename=\"sample.wav\"\r\nContent-Type: audio/wav\r\n\r\n" + string(wav) + "\r\n--probe--\r\n"
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method == http.MethodGet {
					assert.Equal(t, "/v1/videos/synthetic-job", r.URL.Path)
					w.Header().Set("Content-Type", "application/json")
					if tc.protocol == "video-failed" {
						_, _ = w.Write([]byte(`{"id":"synthetic-job","status":"failed"}`))
					} else {
						_, _ = w.Write([]byte(`{"id":"synthetic-job","status":"completed"}`))
					}
					return
				}
				assert.Equal(t, tc.path, r.URL.Path)
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				if tc.contentType != "" {
					assert.Equal(t, sample, string(body))
				} else if tc.protocol != "gemini" {
					assert.Contains(t, string(body), "any-provider-model")
				}
				if tc.protocol == "anthropic" {
					assert.Equal(t, "synthetic-only", r.Header.Get("X-Api-Key"))
				} else if tc.protocol == "gemini" {
					assert.Equal(t, "synthetic-only", r.Header.Get("X-Goog-Api-Key"))
				} else if tc.protocol == "custom-protocol" {
					assert.Equal(t, "synthetic-only", r.Header.Get("Api-Key"))
					assert.Empty(t, r.Header.Get("anthropic-version"))
				} else {
					assert.Equal(t, "Bearer synthetic-only", r.Header.Get("Authorization"))
				}
				w.Header().Set("Content-Type", tc.responseType)
				if tc.protocol == "audio-speech" {
					_, _ = w.Write(wav)
				} else {
					_, _ = w.Write([]byte(tc.response))
				}
			}))
			defer upstream.Close()
			// Neither the old channel ID nor a channel model list authorizes probes.
			require.NoError(t, db.Create(&model.Channel{Id: 7, Name: "arbitrary fixture", Key: "synthetic-only", Status: common.ChannelStatusEnabled, Models: "unrelated-listed-model", BaseURL: &upstream.URL}).Error)
			m := model.ConsoleMonitor{ChannelID: 7, TargetKind: "channel", TargetRef: "7", Model: "any-provider-model", Protocol: tc.protocol, DisplayName: tc.protocol, IntervalSeconds: 60, TimeoutSeconds: 1, StaleSeconds: 900, DailyRequests: 2, RequestCostMicros: 100}
			ctx := context.Background()
			require.NoError(t, model.SaveConsoleMonitor(ctx, db, &m))
			require.NoError(t, model.SetConsoleMonitorAllowlisted(ctx, db, m.ID, true))
			require.NoError(t, model.SetConsoleMonitorSwitch(ctx, db, m.ID, "published", true))
			contract := ConsoleProbeCostContract{Source: "synthetic fixture, no supplier evidence", ValidFrom: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour), InputTokenLimit: 64, InputMicros: 1, OutputMicros: 2, FixedFeeMicros: 4, HardCeilingMicros: 100}
			if tc.contentType != "" {
				contract.BillingUnit = "request"
				contract.InputMicros, contract.OutputMicros = 0, 0
				contract.NativeRequest = &ConsoleProbeNativeRequest{Path: tc.path, ContentType: tc.contentType, ResponseContentType: tc.responseType, SuccessPath: tc.successPath, BodyBase64: base64.StdEncoding.EncodeToString([]byte(sample))}
				if tc.protocol == "custom-protocol" {
					contract.NativeRequest.KeyHeader = "Api-Key"
				}
				if tc.poll {
					contract.NativeRequest.Poll = &ConsoleProbeNativePoll{Path: "/v1/videos/{id}", IDPath: "id", StatusPath: "status", CompletedStates: []string{"completed"}, FailedStates: []string{"failed"}}
				}
			}
			if tc.protocol == "image-generation" {
				bad := contract
				request := *contract.NativeRequest
				bad.NativeRequest = &request
				bad.NativeRequest.BodyBase64 = base64.StdEncoding.EncodeToString([]byte(`{"model":"any-provider-model","n":18446744073709551615}`))
				require.ErrorIs(t, SaveConsoleProbeCostContract(ctx, db, m, bad), model.ErrMonitorInvalid)
				bad.NativeRequest.BodyBase64 = base64.StdEncoding.EncodeToString([]byte(`{"model":"wrong-target","n":1}`))
				require.ErrorIs(t, SaveConsoleProbeCostContract(ctx, db, m, bad), model.ErrMonitorInvalid)
				assert.Zero(t, calls.Load())
			}
			require.NoError(t, SaveConsoleProbeCostContract(ctx, db, m, contract))
			require.NoError(t, model.SetConsoleMonitorSwitch(ctx, db, m.ID, "enabled", true))
			require.NoError(t, model.SaveConsoleMonitorPolicy(ctx, db, model.ConsoleMonitorPolicy{ID: 1, Enabled: true, MaxConcurrent: 1, DailyRequests: 2, DailyCostMicros: 200, RetentionDays: 90}))
			executor := ConsoleProbeExecutor{DB: db, Adapters: map[string]ConsoleProbeAdapter{"channel": configuredConsoleAdapter{db}}}
			result, err := executor.RunWithResult(ctx, m.ID)
			if tc.protocol == "native-html" {
				require.ErrorIs(t, err, ErrMonitorLeaseUnresolved)
				assert.Equal(t, "unknown", result.Status)
				var attempt model.ConsoleMonitorAttempt
				require.NoError(t, db.First(&attempt, result.AttemptID).Error)
				assert.Zero(t, attempt.FinishedAt)
				assert.EqualValues(t, 100, attempt.ReservedCostMicros)
				before := calls.Load()
				_, err = executor.RunWithResult(ctx, m.ID)
				require.ErrorIs(t, err, model.ErrMonitorBudget)
				assert.Equal(t, before, calls.Load(), "unresolved media cannot be retried or refunded automatically")
				return
			}
			if tc.protocol == "video-failed" {
				require.NoError(t, err)
				assert.Equal(t, "error", result.Status)
				assert.Nil(t, result.MeteredCostMicros)
				var attempt model.ConsoleMonitorAttempt
				require.NoError(t, db.First(&attempt, result.AttemptID).Error)
				assert.Positive(t, attempt.FinishedAt)
				assert.EqualValues(t, 100, attempt.ReservedCostMicros, "terminal failures never refund an unknown supplier charge")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "normal", result.Status)
			require.NotNil(t, result.MeteredCostMicros)
			if tc.contentType != "" {
				assert.EqualValues(t, 4, *result.MeteredCostMicros)
			} else {
				expected := int64(12)
				if tc.protocol == "anthropic" {
					expected = 17
				}
				if tc.protocol == "gemini" {
					expected = 18
				}
				assert.Equal(t, expected, *result.MeteredCostMicros)
			}
			if tc.poll {
				assert.EqualValues(t, 2, calls.Load())
				assert.Equal(t, "synthetic-job", result.UpstreamResponseID)
			} else {
				assert.EqualValues(t, 1, calls.Load())
			}
			// Any post-approval model change invalidates the supplier binding,
			// rather than silently probing the old model or reusing its price.
			m.Model = "changed-after-approval"
			require.ErrorIs(t, AuthorizeConsoleProbeCost(ctx, db, m), ErrMonitorCostUnverified)
			m.Model, m.Protocol = "any-provider-model", "changed-protocol"
			require.ErrorIs(t, AuthorizeConsoleProbeCost(ctx, db, m), ErrMonitorCostUnverified)
		})
	}
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
				assert.Equal(t, "blocked", result.Status)
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
