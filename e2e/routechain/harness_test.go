// Package routechain hosts the Stage 3.5 network E2E acceptance for the
// Resilient Route Chain (docs/superpowers/specs/2026-10-06-resilient-route-chain-stage3-network-e2e.md).
//
// Every scenario drives real HTTP: client → production relay router
// (router.SetRelayRouter: TokenAuth → Distribute → controller.Relay) →
// route chain engine → HTTP transport → scripted mock upstream over real
// TCP → back to the client. No failover policy function is ever called
// directly; behavior is judged exclusively from mock call counters, client
// output and billing rows.
package routechain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/router"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	e2eModel   = "gpt-chain"
	e2eTokenKV = "e2echainkey"
)

// financeOnce guards the one-time process-global finance configuration.
var financeOnce sync.Once

// mockBehavior scripts one upstream response.
type mockBehavior struct {
	status        int           // HTTP status (default 200)
	contentType   string        // default application/json
	body          string        // non-stream body (default empty JSON ok)
	sse           []string      // when set: 200 SSE with these chunks then [DONE]
	delay         time.Duration // delay before responding
	disconnect    bool          // accept request, close connection without response
	cutAfterSSE   bool          // write SSE chunks, then cut mid-stream (12)
	hijackClose   bool          // raw hijack + immediate close (post-dispatch cut)
	poolExhausted bool          // executor envelope 503 auth_unavailable
}

type mockRecord struct {
	path     string
	bodyHash string
	at       time.Time
}

// mockUpstream is a scripted OpenAI-compatible upstream with call counters.
// It never records Authorization headers or channel keys.
type mockUpstream struct {
	srv       *httptest.Server
	mu        sync.Mutex
	records   []mockRecord
	behaviors []mockBehavior
	t         *testing.T
}

func (m *mockUpstream) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.records)
}

func (m *mockUpstream) recordsSnapshot() []mockRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]mockRecord, len(m.records))
	copy(out, m.records)
	return out
}

func (m *mockUpstream) handler(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	m.mu.Lock()
	index := len(m.records)
	m.records = append(m.records, mockRecord{path: r.URL.Path, at: time.Now()})
	var behavior mockBehavior
	if len(m.behaviors) == 0 {
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"m0","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
		return
	}
	if index < len(m.behaviors) {
		behavior = m.behaviors[index]
	} else {
		behavior = m.behaviors[len(m.behaviors)-1]
	}
	m.mu.Unlock()

	if behavior.delay > 0 {
		time.Sleep(behavior.delay)
	}
	if behavior.disconnect || behavior.hijackClose {
		if behavior.hijackClose {
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				m.t.Fatalf("mock upstream cannot hijack")
			}
			conn, _, err := hijacker.Hijack()
			if err != nil {
				m.t.Fatalf("hijack failed: %v", err)
			}
			_ = conn.Close()
			return
		}
		panic(http.ErrAbortHandler) // cut the connection after the request was fully received
	}
	if behavior.poolExhausted {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"auth_unavailable: no auth available","type":"server_error","code":"auth_unavailable","retryable":true}}`))
		return
	}
	if len(behavior.sse) > 0 {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		for _, chunk := range behavior.sse {
			_, _ = w.Write([]byte(chunk))
			flusher.Flush()
			time.Sleep(10 * time.Millisecond)
		}
		if behavior.cutAfterSSE {
			panic(http.ErrAbortHandler) // cut mid-stream after client-visible output
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
		return
	}
	status := behavior.status
	if status == 0 {
		status = http.StatusOK
	}
	contentType := behavior.contentType
	if contentType == "" {
		contentType = "application/json"
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_, _ = w.Write([]byte(behavior.body))
}

func newMockUpstream(t *testing.T, behaviors ...mockBehavior) *mockUpstream {
	t.Helper()
	m := &mockUpstream{behaviors: behaviors, t: t}
	m.srv = httptest.NewServer(http.HandlerFunc(m.handler))
	t.Cleanup(m.srv.Close)
	return m
}

// unusedUpstreamAddr returns a 127.0.0.1 port with no listener (connect-refused).
func unusedUpstreamAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	return "http://" + addr
}

type harness struct {
	t      *testing.T
	db     *gorm.DB
	engine *gin.Engine
	srv    *httptest.Server
	userID int
	closed *sync.Once
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	common.SetRouteChainEnabledForTest(true)
	t.Cleanup(func() { common.SetRouteChainEnabledForTest(false) })
	// Paid-request billing protection requires token counting and a bounded
	// output limit; mirror the production defaults (constant.CountToken=true).
	oldCountToken := constant.CountToken
	constant.CountToken = true
	t.Cleanup(func() { constant.CountToken = oldCountToken })
	// StreamingTimeout is option-configured in production; the empty test DB
	// leaves it at 0, which would panic stream handling (NewTicker).
	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() { constant.StreamingTimeout = oldStreamingTimeout })

	t.Setenv("CLEARAPI_FINANCE_ENABLED", "true")
	t.Setenv("CLEARAPI_FINANCE_OWNER_USER_ID", "1")
	t.Setenv("CLEARAPI_DURABLE_WALLET", "true")
	t.Setenv("CLEARAPI_BILLING_RECONCILIATION_SINGLE_INSTANCE", "true")
	// Finance config globals are identical for every harness; initialize them
	// once per process. Re-initializing while a previous harness's in-flight
	// settlement goroutine still reads them is a data race (found by -race).
	financeOnce.Do(func() { common.InitFinanceConfig() })

	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "e2e.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(2)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, database.AutoMigrate(
		&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{},
		&model.Log{}, &model.Option{}, &model.BillingReservation{},
		&model.BillingReviewMarker{}, &model.BillingReservationDecision{},
		&model.FinanceVersion{}, &model.RouteChain{}, &model.RouteChainCandidate{},
		&model.UserSubscription{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{},
		&model.SubscriptionPreConsumeRecord{},
	))
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldRedis, oldBatch := common.RedisEnabled, common.BatchUpdateEnabled
	model.DB, model.LOG_DB = database, database
	common.RedisEnabled, common.BatchUpdateEnabled = false, false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	model.InitColumnQuotingForTest()
	// No globals restore: each test binary is a separate process and every
	// harness installs its own DB/globals; a late restore write races async
	// gopool workers (perf-metrics recordRedis) that outlive the server
	// drain (found by -race).
	_ = oldDB
	_ = oldLogDB
	_ = oldRedis
	_ = oldBatch

	model.InitOptionMap()
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(fmt.Sprintf(`{%q:1,%q:1}`, e2eModel, "gpt-legacy")))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(fmt.Sprintf(`{%q:2,%q:2}`, e2eModel, "gpt-legacy")))
	service.InitTokenEncoders()

	user := &model.User{Username: "e2e-user", Password: "test-password", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", Quota: 100000000}
	require.NoError(t, database.Create(user).Error)
	token := &model.Token{UserId: user.Id, Key: e2eTokenKV, Status: common.TokenStatusEnabled, UnlimitedQuota: true, ExpiredTime: -1, RemainQuota: 100000000, Name: "e2e"}
	require.NoError(t, database.Create(token).Error)

	engine := gin.New()
	router.SetRelayRouter(engine)
	server := httptest.NewServer(engine)
	h := &harness{t: t, db: database, engine: engine, srv: server, userID: user.Id}
	var closeOnce sync.Once
	h.closed = &closeOnce
	t.Cleanup(func() { h.Close() })
	return h
}

// Close stops the harness server. httptest.Server.Close waits for all
// in-flight handler goroutines (including post-response settlement) to
// finish, so a successor harness never races the previous one's tail.
func (h *harness) Close() {
	if h.closed != nil {
		h.closed.Do(h.srv.Close)
	}
}

func (h *harness) addChannel(id int, baseURL string, otherSettings string) {
	h.t.Helper()
	channel := &model.Channel{
		Id:            id,
		Type:          1, // OpenAI
		Key:           "sk-mock",
		Status:        common.ChannelStatusEnabled,
		Name:          "mock-channel",
		Models:        e2eModel + ",gpt-legacy",
		Group:         "default",
		OtherSettings: otherSettings,
	}
	if baseURL != "" {
		channel.BaseURL = &baseURL
	}
	require.NoError(h.t, h.db.Create(channel).Error)
	require.NoError(h.t, h.db.Create(&model.Ability{Group: "default", Model: e2eModel, ChannelId: id, Enabled: true}).Error)
	require.NoError(h.t, h.db.Create(&model.Ability{Group: "default", Model: "gpt-legacy", ChannelId: id, Enabled: true}).Error)
}

// setChain writes a chain config through the CAS repository.
func (h *harness) setChain(t *testing.T, allowCrossSource bool, candidates []model.RouteChainCandidate) *model.RouteChainWithCandidates {
	t.Helper()
	for i := range candidates {
		candidates[i].Enabled = true
		candidates[i].Priority = len(candidates) - i
	}
	chain := &model.RouteChainWithCandidates{
		RouteChain: model.RouteChain{Model: e2eModel, Group: "default", Enabled: true, AllowCrossSource: allowCrossSource},
		Candidates: candidates,
	}
	require.NoError(t, model.UpsertRouteChain(chain, 0))
	return chain
}

func (h *harness) setPoolPublication(t *testing.T) {
	t.Helper()
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	common.OptionMap[model.PoolMasterOptionKey] = "true"
	common.OptionMap[model.PoolRoutingOptionPrefix+e2eModel] = fmt.Sprintf(`{"route":{"model":%q,"source":"pool","enabled":true,"version":1}}`, e2eModel)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		delete(common.OptionMap, model.PoolMasterOptionKey)
		delete(common.OptionMap, model.PoolRoutingOptionPrefix+e2eModel)
		common.OptionMapRWMutex.Unlock()
	})
}

// postChat sends one real HTTP chat-completions request through the production stack.
func (h *harness) postChat(t *testing.T, modelName string, bodyExtra string, stream bool) (int, string, http.Header) {
	t.Helper()
	payload := fmt.Sprintf(`{"model":%q,"max_tokens":128,"messages":[{"role":"user","content":"hello"}]%s}`, modelName, bodyExtra)
	if stream {
		payload = fmt.Sprintf(`{"model":%q,"stream":true,"max_tokens":128,"messages":[{"role":"user","content":"hello"}]%s}`, modelName, bodyExtra)
	}
	request, err := http.NewRequest(http.MethodPost, h.srv.URL+"/v1/chat/completions", strings.NewReader(payload))
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer sk-"+e2eTokenKV)
	request.Header.Set("Content-Type", "application/json")
	response, err := h.srv.Client().Do(request)
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	return response.StatusCode, string(body), response.Header
}

func chatOKBody(content string) string {
	return fmt.Sprintf(`{"id":"mock","object":"chat.completion","model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`, e2eModel, content)
}

func sseChunk(content string) string {
	return fmt.Sprintf("data: {\"id\":\"mock\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":%q}}]}\n\n", content)
}

func errorBody(status int, code string) string {
	return fmt.Sprintf(`{"error":{"message":"upstream reject %d","type":"server_error","code":%q}}`, status, code)
}

// countReservations returns the number of billing_reservations rows.
func (h *harness) countReservations() int64 {
	h.t.Helper()
	var count int64
	require.NoError(h.t, h.db.Model(&model.BillingReservation{}).Count(&count).Error)
	return count
}

// countConsumeLogs returns the number of consume log rows.
func (h *harness) countConsumeLogs() int64 {
	h.t.Helper()
	var count int64
	require.NoError(h.t, h.db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&count).Error)
	return count
}

// eventuallyConsumeLogs polls for the consume-log write: settlement logging
// happens after the response is already on the wire, so the assert must wait
// out the async write instead of racing it.
func (h *harness) eventuallyConsumeLogs(want int64) int64 {
	h.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	count := h.countConsumeLogs()
	for count < want && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		count = h.countConsumeLogs()
	}
	return count
}

func (h *harness) userQuota() int {
	h.t.Helper()
	var user model.User
	require.NoError(h.t, h.db.First(&user, h.userID).Error)
	return user.Quota
}

func bodyHash(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:8])
}
