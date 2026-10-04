package router_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/router"
	"github.com/QuantumNous/new-api/service/accountpool"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const poolRawIdentity = "secret-identity@example-upstream.com"

// mockPoolExecutor implements accountpool.ExecutorClient with configurable
// outcomes; counters let tests assert the executor really was consulted.
// The credential listing is mutable so tests can simulate the executor-side
// state changing between the baseline snapshot (task start) and completion.
type mockPoolExecutor struct {
	healthErr     error
	listErr       error
	startAuthErr  error
	startAuth     func(req accountpool.StartAuthRequest) (accountpool.StartAuthResult, error)
	completeErr   error
	complete      func(req accountpool.CompleteAuthRequest) error
	authState     accountpool.AuthState
	authStatusErr error
	cancelErr     error
	deleteErr     error
	pauseErr      error
	resumeErr     error
	credStatusErr error
	credState     string
	files         []accountpool.CredentialEntry

	startCalls    int
	completeCalls int
	cancelCalls   int
	deleteCalls   []string
	pauseCalls    []string
	resumeCalls   []string
}

func (m *mockPoolExecutor) Health(context.Context) error { return m.healthErr }

func (m *mockPoolExecutor) StartAuth(_ context.Context, req accountpool.StartAuthRequest) (accountpool.StartAuthResult, error) {
	m.startCalls++
	if m.startAuthErr != nil {
		return accountpool.StartAuthResult{}, m.startAuthErr
	}
	if m.startAuth != nil {
		return m.startAuth(req)
	}
	return accountpool.StartAuthResult{
		Method:   accountpool.AuthMethodOAuthBrowser,
		AuthURL:  "https://upstream.example/authorize?state=synthetic-state",
		StateRef: "synthetic-state-" + fmt.Sprint(m.startCalls),
	}, nil
}

func (m *mockPoolExecutor) AuthStatus(context.Context, string) (accountpool.AuthState, error) {
	if m.authStatusErr != nil {
		return accountpool.AuthStateUnknown, m.authStatusErr
	}
	if m.authState == "" {
		return accountpool.AuthStateWaiting, nil
	}
	return m.authState, nil
}

func (m *mockPoolExecutor) CompleteAuth(_ context.Context, req accountpool.CompleteAuthRequest) error {
	m.completeCalls++
	if m.completeErr != nil {
		return m.completeErr
	}
	if m.complete != nil {
		return m.complete(req)
	}
	return nil
}

func (m *mockPoolExecutor) ListCredentials(context.Context) ([]accountpool.CredentialEntry, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.files, nil
}

func (m *mockPoolExecutor) CancelAuth(context.Context, string) error {
	m.cancelCalls++
	return m.cancelErr
}

func (m *mockPoolExecutor) DeleteCredential(_ context.Context, ref string) error {
	m.deleteCalls = append(m.deleteCalls, ref)
	return m.deleteErr
}

func (m *mockPoolExecutor) PauseCredential(_ context.Context, ref string) error {
	m.pauseCalls = append(m.pauseCalls, ref)
	return m.pauseErr
}

func (m *mockPoolExecutor) ResumeCredential(_ context.Context, ref string) error {
	m.resumeCalls = append(m.resumeCalls, ref)
	return m.resumeErr
}

func (m *mockPoolExecutor) CredentialStatus(context.Context, string) (accountpool.ExecutorCredentialStatus, error) {
	if m.credStatusErr != nil {
		return accountpool.ExecutorCredentialStatus{}, m.credStatusErr
	}
	if m.credState == "" {
		m.credState = accountpool.CredentialStateActive
	}
	return accountpool.ExecutorCredentialStatus{State: m.credState}, nil
}

type poolTestSetup struct {
	api      *gin.Engine
	db       *gorm.DB
	executor *mockPoolExecutor
	request  func(method, path, token string, body any) *httptest.ResponseRecorder
}

func newPoolTestSetup(t *testing.T, enabled bool) *poolTestSetup {
	t.Helper()
	db := financeTestDB(t)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedis, previousMemory := common.RedisEnabled, common.MemoryCacheEnabled
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled, common.MemoryCacheEnabled = false, false
	previousType := common.MainDatabaseType()
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Setenv("CLEARAPI_POOL_ENABLED", "true")
	if !enabled {
		t.Setenv("CLEARAPI_POOL_ENABLED", "false")
	}
	// Fixture attests executor isolation so authorization flows run; the
	// unattested-default behavior has its own test below.
	t.Setenv("CLEARAPI_POOL_CLIPROXY_DEDICATED", "true")
	common.InitPoolConfig()
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.AuditLog{}))
	for _, user := range []struct {
		id, role, status int
		token            string
	}{
		{1, common.RoleRootUser, common.UserStatusEnabled, "synthetic-owner-pat"},
		{6, common.RoleRootUser, common.UserStatusEnabled, "synthetic-other-root-pat"},
		{7, common.RoleAdminUser, common.UserStatusEnabled, "synthetic-admin-pat"},
		{8, common.RoleCommonUser, common.UserStatusEnabled, "synthetic-user-pat"},
		{9, common.RoleRootUser, common.UserStatusDisabled, "synthetic-disabled-pat"},
	} {
		token := user.token
		require.NoError(t, db.Create(&model.User{Id: user.id, Username: fmt.Sprintf("synthetic-%d", user.id), Password: "synthetic-unused", Role: user.role, Status: user.status, Quota: 1, AuthVersion: 1, AccessToken: &token, AffCode: fmt.Sprintf("synthetic-aff-%d", user.id)}).Error)
	}
	gin.SetMode(gin.TestMode)
	api := gin.New()
	router.RegisterUpstreamPoolRoutes(api.Group("/api"))
	executor := &mockPoolExecutor{}
	previousFactory := accountpool.ExecutorFactory
	accountpool.ExecutorFactory = func(id string) (accountpool.ExecutorClient, error) {
		if id == accountpool.ExecutorGrok2API {
			return accountpool.NewOfflineExecutor(), nil
		}
		return executor, nil
	}
	t.Cleanup(func() {
		accountpool.ExecutorFactory = previousFactory
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled, common.MemoryCacheEnabled = previousRedis, previousMemory
		common.SetMainDatabaseType(previousType)
	})
	setup := &poolTestSetup{api: api, db: db, executor: executor}
	setup.request = func(method, path, token string, body any) *httptest.ResponseRecorder {
		t.Helper()
		encoded, err := common.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest(method, path, bytes.NewReader(encoded))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		api.ServeHTTP(response, req)
		return response
	}
	return setup
}

func ownerAccount(t *testing.T, setup *poolTestSetup, platform, accessType string) map[string]any {
	t.Helper()
	created := setup.request("POST", "/api/owner/pool/accounts", "synthetic-owner-pat", map[string]any{
		"platform": platform, "access_type": accessType, "note": "synthetic",
	})
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	var decoded struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, common.Unmarshal(created.Body.Bytes(), &decoded))
	return decoded.Data
}

func accountField(account map[string]any, key string) float64 {
	value, _ := account[key].(float64)
	return value
}

// startAuthAndComplete runs a full authorization round against the mock
// executor. listAfterStart replaces the credential listing observed during
// completion (the baseline was captured at start time).
func startAuthAndComplete(t *testing.T, setup *poolTestSetup, id int, listAfterStart []accountpool.CredentialEntry) *httptest.ResponseRecorder {
	t.Helper()
	start := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, start.Code, start.Body.String())
	setup.executor.files = listAfterStart
	return setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{
		"callback_url": "https://platform.claude.com/oauth/code/callback?code=synthetic-code-value&state=synthetic-state-1",
	})
}

func TestPoolModuleDisabledReturns404ForOwner(t *testing.T) {
	setup := newPoolTestSetup(t, false)
	for _, endpoint := range []struct{ method, path string }{
		{"GET", "/api/owner/pool/capability"},
		{"GET", "/api/owner/pool/accounts"},
		{"POST", "/api/owner/pool/accounts"},
	} {
		result := setup.request(endpoint.method, endpoint.path, "synthetic-owner-pat", map[string]any{})
		assert.Equal(t, http.StatusNotFound, result.Code, endpoint.path)
	}
}

func TestPoolRequiresOwnerIdentityOnEveryEndpoint(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	endpoints := []struct{ method, path string }{
		{"GET", "/capability"}, {"GET", "/executors"}, {"GET", "/catalog"},
		{"GET", "/accounts"}, {"POST", "/accounts"},
		{"GET", "/accounts/1"}, {"GET", "/accounts/1/capabilities"},
		{"POST", "/accounts/1/auth/start"}, {"POST", "/accounts/1/auth/complete"},
		{"POST", "/accounts/1/auth/cancel"}, {"POST", "/accounts/1/pause"},
		{"POST", "/accounts/1/resume"}, {"POST", "/accounts/1/reauth"},
		{"POST", "/accounts/1/delete"}, {"POST", "/accounts/1/sync"},
	}
	actors := []struct {
		token  string
		status int
	}{{"", 401}, {"synthetic-other-root-pat", 403}, {"synthetic-admin-pat", 403}, {"synthetic-user-pat", 403}, {"synthetic-disabled-pat", 401}}
	for _, endpoint := range endpoints {
		for _, actor := range actors {
			result := setup.request(endpoint.method, "/api/owner/pool"+endpoint.path, actor.token, map[string]any{})
			assert.Equal(t, actor.status, result.Code, "%s %s as %s", endpoint.method, endpoint.path, actor.token)
		}
	}
	// Owner identity is server-side: a forged id in the body changes nothing.
	forged := setup.request("GET", "/api/owner/pool/accounts", "synthetic-admin-pat", map[string]any{"user_id": 1})
	assert.Equal(t, http.StatusForbidden, forged.Code)
	owner := setup.request("GET", "/api/owner/pool/capability", "synthetic-owner-pat", nil)
	assert.Equal(t, http.StatusOK, owner.Code)
}

func TestPoolUnavailableBeforeMigration(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	result := setup.request("GET", "/api/owner/pool/accounts", "synthetic-owner-pat", nil)
	assert.Equal(t, http.StatusServiceUnavailable, result.Code)
	assert.Contains(t, result.Body.String(), "POOL_UNAVAILABLE")
	capability := setup.request("GET", "/api/owner/pool/capability", "synthetic-owner-pat", nil)
	assert.Equal(t, http.StatusOK, capability.Code)
	assert.Contains(t, capability.Body.String(), "false")
}

func TestPoolCatalogAndCreateValidation(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	catalog := setup.request("GET", "/api/owner/pool/catalog", "synthetic-owner-pat", nil)
	require.Equal(t, http.StatusOK, catalog.Code)
	// Disabled platforms keep their reasons visible but cannot be created.
	for _, platform := range []string{"gemini_code_assist", "qwen_web", "deepseek_web"} {
		created := setup.request("POST", "/api/owner/pool/accounts", "synthetic-owner-pat", map[string]any{
			"platform": platform, "access_type": map[string]string{
				"gemini_code_assist": "oauth_browser", "qwen_web": "browser_bridge", "deepseek_web": "cookie_import",
			}[platform],
		})
		assert.Equal(t, http.StatusBadRequest, created.Code, platform)
	}
	assert.Equal(t, http.StatusBadRequest, setup.request("POST", "/api/owner/pool/accounts", "synthetic-owner-pat", map[string]any{"platform": "unknown", "access_type": "oauth_browser"}).Code)
	assert.Equal(t, http.StatusBadRequest, setup.request("POST", "/api/owner/pool/accounts", "synthetic-owner-pat", map[string]any{"platform": "claude", "access_type": "oauth_device"}).Code)
	assert.Equal(t, http.StatusBadRequest, setup.request("POST", "/api/owner/pool/accounts", "synthetic-owner-pat", map[string]any{"platform": "claude", "access_type": "oauth_browser", "executor_id": "grok2api"}).Code)
	account := ownerAccount(t, setup, "claude", "oauth_browser")
	assert.Equal(t, "enabled", account["admin_status"])
	assert.Equal(t, "awaiting_auth", account["run_status"])
	// Grok sources are separate access types, never merged.
	for _, platform := range []string{"grok_build", "grok_web_sso", "grok_console"} {
		accessType := "oauth_device"
		if platform != "grok_build" {
			accessType = "cookie_import"
		}
		created := setup.request("POST", "/api/owner/pool/accounts", "synthetic-owner-pat", map[string]any{"platform": platform, "access_type": accessType})
		assert.Equal(t, http.StatusCreated, created.Code, platform)
	}
	// The executor listing carries the isolation attestation and actions.
	executors := setup.request("GET", "/api/owner/pool/executors", "synthetic-owner-pat", nil)
	assert.Contains(t, executors.Body.String(), "isolation_attested")
}

func TestPoolAuthorizationBlockedWithoutIsolationAttestation(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	t.Setenv("CLEARAPI_POOL_CLIPROXY_DEDICATED", "false")
	account := ownerAccount(t, setup, "claude", "oauth_browser")
	id := int(accountField(account, "id"))
	start := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{})
	assert.Equal(t, http.StatusConflict, start.Code)
	assert.Contains(t, start.Body.String(), "POOL_ISOLATION_REQUIRED")
	assert.Zero(t, setup.executor.startCalls, "no executor call may happen without the isolation attestation")
}

func TestPoolAuthorizationLifecycle(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account := ownerAccount(t, setup, "claude", "oauth_browser")
	id := int(accountField(account, "id"))

	start := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, start.Code, start.Body.String())
	assert.Contains(t, start.Body.String(), "authorize")
	var started struct {
		Data struct {
			TaskId int `json:"task_id"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(start.Body.Bytes(), &started))
	// The authorization URL is one-time output and never persisted.
	var task model.UpstreamPoolAuthTask
	require.NoError(t, setup.db.First(&task, started.Data.TaskId).Error)
	assert.Equal(t, model.PoolTaskStatusPending, task.Status)
	assert.NotContains(t, fmt.Sprintf("%v", task), "authorize")
	assert.Equal(t, 1, setup.executor.startCalls)

	// Duplicate start while a task is pending conflicts.
	assert.Equal(t, http.StatusConflict, setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{}).Code)
	// A second account cannot open a concurrent authorization on the same
	// executor: completion correlates credentials by listing diff, which is
	// only unambiguous with one active task per executor.
	other := ownerAccount(t, setup, "codex", "oauth_device")
	otherID := int(accountField(other, "id"))
	assert.Equal(t, http.StatusConflict, setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", otherID), "synthetic-owner-pat", map[string]any{}).Code)

	// Complete the still-pending task with a pasted full callback URL: the
	// callback is submitted, the session must reach the terminal completed
	// state, and the new credential is correlated by the baseline diff.
	setup.executor.complete = func(req accountpool.CompleteAuthRequest) error {
		assert.NotEmpty(t, req.StateRef, "executor session ref is submitted")
		return nil
	}
	// Malformed callback input is rejected without touching the executor.
	calls := setup.executor.completeCalls
	assert.Equal(t, http.StatusBadRequest, setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"callback_url": "not-a-url"}).Code)
	assert.Equal(t, calls, setup.executor.completeCalls)

	setup.executor.files = []accountpool.CredentialEntry{
		{Name: "synthetic-auth-file.json", Email: poolRawIdentity, UpdatedAt: 100},
	}
	completed := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{
		"callback_url": "https://platform.claude.com/oauth/code/callback?code=synthetic-code-value&state=synthetic-state-1",
	})
	require.Equal(t, http.StatusOK, completed.Code, completed.Body.String())
	var done struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, common.Unmarshal(completed.Body.Bytes(), &done))
	assert.Equal(t, "not_admitted", done.Data["run_status"])
	assert.Equal(t, "enabled", done.Data["admin_status"])
	assert.Equal(t, "s***@e***", done.Data["identity_masked"])
	assert.Equal(t, "synthetic-auth-file.json", done.Data["credential_ref"])
	assert.NotContains(t, completed.Body.String(), poolRawIdentity)
	assert.NotContains(t, completed.Body.String(), "synthetic-code-value")
	// Defense in depth: the fresh credential is parked at the executor
	// (disabled) until admission.
	assert.Equal(t, []string{"synthetic-auth-file.json"}, setup.executor.pauseCalls)

	// Duplicate callback submission conflicts.
	assert.Equal(t, http.StatusConflict, setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"callback_url": "https://x.example/cb?code=a&state=b"}).Code)

	// After completion there is no pending task anymore: further submissions
	// conflict instead of re-running the executor.
	assert.Equal(t, http.StatusConflict, setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"callback_url": "https://x.example/cb?code=a&state=b"}).Code)
}

func TestPoolCorrelationFailsClosed(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account := ownerAccount(t, setup, "claude", "oauth_browser")
	id := int(accountField(account, "id"))

	// Two new files after the baseline: ambiguous, nothing deleted, no
	// first-entry guess.
	start := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, start.Code)
	setup.executor.files = []accountpool.CredentialEntry{
		{Name: "pre-existing.json", Email: "pre@existing.test", UpdatedAt: 1},
		{Name: "new-one.json", Email: "new@one.test", UpdatedAt: 2},
		{Name: "new-two.json", Email: "new@two.test", UpdatedAt: 3},
	}
	ambiguous := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"callback_url": "https://x.example/cb?code=a&state=b"})
	assert.Equal(t, http.StatusBadGateway, ambiguous.Code)
	assert.Contains(t, ambiguous.Body.String(), "POOL_CORRELATION_FAILED")
	assert.Empty(t, setup.executor.deleteCalls, "ambiguous correlation must not delete anything")
	var after model.UpstreamPoolAccount
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Empty(t, after.CredentialRef)
	assert.Equal(t, "correlation_failed", after.LastError)

	// Zero new files: same failure mode.
	setup.executor.files = []accountpool.CredentialEntry{{Name: "pre-existing.json", Email: "pre@existing.test", UpdatedAt: 1}}
	start = setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, start.Code)
	none := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"callback_url": "https://x.example/cb?code=a&state=b"})
	assert.Equal(t, http.StatusBadGateway, none.Code)
	assert.Empty(t, setup.executor.deleteCalls)
}

func TestPoolAuthExpiryCancelAndPlatformMismatch(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account := ownerAccount(t, setup, "claude", "oauth_browser")
	id := int(accountField(account, "id"))

	// Expiry: a pending task past its deadline is marked expired, not completed.
	start := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, start.Code)
	require.NoError(t, setup.db.Model(&model.UpstreamPoolAuthTask{}).Where("account_id = ?", id).Update("expires_at", time.Now().Add(-time.Minute).Unix()).Error)
	expired := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"callback_url": "https://x.example/cb?code=a&state=b"})
	assert.Equal(t, http.StatusGone, expired.Code)
	var expiredTask model.UpstreamPoolAuthTask
	require.NoError(t, setup.db.Where("account_id = ?", id).First(&expiredTask).Error)
	assert.Equal(t, model.PoolTaskStatusExpired, expiredTask.Status)
	assert.Zero(t, setup.executor.completeCalls)

	// Cancel while waiting: the executor session is cancelled.
	start = setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, start.Code)
	var started struct {
		Data struct {
			TaskId int `json:"task_id"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(start.Body.Bytes(), &started))
	cancelled := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/cancel", id), "synthetic-owner-pat", map[string]any{"task_id": started.Data.TaskId})
	assert.Equal(t, http.StatusOK, cancelled.Code)
	assert.Equal(t, 1, setup.executor.cancelCalls)
	assert.Equal(t, http.StatusConflict, setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"callback_url": "https://x.example/cb?code=a&state=b"}).Code)
	var cancelledTask model.UpstreamPoolAuthTask
	require.NoError(t, setup.db.Where("account_id = ?", id).Order("id desc").First(&cancelledTask).Error)
	assert.Equal(t, model.PoolTaskStatusCancelled, cancelledTask.Status)

	// Cancel after the session silently completed: the correlated orphan
	// credential is deleted, the pre-existing one is untouched.
	setup.executor.files = []accountpool.CredentialEntry{
		{Name: "kept-existing.json", Email: "kept@existing.test", UpdatedAt: 1},
	}
	start = setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, start.Code)
	require.NoError(t, common.Unmarshal(start.Body.Bytes(), &started))
	setup.executor.authState = accountpool.AuthStateCompleted
	setup.executor.files = []accountpool.CredentialEntry{
		{Name: "kept-existing.json", Email: "kept@existing.test", UpdatedAt: 1},
		{Name: "orphan-completed.json", Email: "orphan@completed.test", UpdatedAt: 2},
	}
	cancelled = setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/cancel", id), "synthetic-owner-pat", map[string]any{"task_id": started.Data.TaskId})
	assert.Equal(t, http.StatusOK, cancelled.Code)
	assert.Equal(t, []string{"orphan-completed.json"}, setup.executor.deleteCalls)

	// Platform mismatch between the pending task and the account fails closed.
	setup.executor.authState = accountpool.AuthStateWaiting
	start = setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, start.Code)
	require.NoError(t, setup.db.Model(&model.UpstreamPoolAuthTask{}).Where("account_id = ?", id).Update("platform", "codex").Error)
	mismatch := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"callback_url": "https://x.example/cb?code=a&state=b"})
	assert.Equal(t, http.StatusConflict, mismatch.Code)
	var mismatchTask model.UpstreamPoolAuthTask
	require.NoError(t, setup.db.Where("account_id = ?", id).Order("id desc").First(&mismatchTask).Error)
	assert.Equal(t, model.PoolTaskFailPlatformMismatch, mismatchTask.FailReason)
}

func TestPoolReauthIdentityMismatchKeepsOriginal(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account := ownerAccount(t, setup, "claude", "oauth_browser")
	id := int(accountField(account, "id"))

	// Initial authorization.
	completed := startAuthAndComplete(t, setup, id, []accountpool.CredentialEntry{
		{Name: "synthetic-auth-file.json", Email: poolRawIdentity, UpdatedAt: 100},
	})
	require.Equal(t, http.StatusOK, completed.Code, completed.Body.String())

	// Re-auth must reach the executor and preserve id, note and admin status.
	// The submitted identity has the SAME masked display value but a
	// different fingerprint — the fingerprint, not the display value, must
	// detect the mismatch.
	sameMaskedDifferentAccount := "secret-identity@evil-domain.test"
	require.Equal(t, accountpool.MaskIdentity(poolRawIdentity), accountpool.MaskIdentity(sameMaskedDifferentAccount))
	reauth := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/reauth", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, reauth.Code)
	setup.executor.files = []accountpool.CredentialEntry{
		{Name: "synthetic-auth-file.json", Email: poolRawIdentity, UpdatedAt: 100},
		{Name: "other-auth-file.json", Email: sameMaskedDifferentAccount, UpdatedAt: 200},
	}
	mismatch := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"code": "abc", "state": "b"})
	assert.Equal(t, http.StatusConflict, mismatch.Code)
	assert.NotContains(t, mismatch.Body.String(), sameMaskedDifferentAccount)
	var after model.UpstreamPoolAccount
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, "synthetic-auth-file.json", after.CredentialRef, "original credential must survive")
	assert.Equal(t, "s***@e***", after.IdentityMasked)
	// The task-created orphan is deleted; the original credential is not.
	assert.Equal(t, []string{"other-auth-file.json"}, setup.executor.deleteCalls)
	var failedTask model.UpstreamPoolAuthTask
	require.NoError(t, setup.db.Where("account_id = ?", id).Order("id desc").First(&failedTask).Error)
	assert.Equal(t, model.PoolTaskFailIdentityMismatch, failedTask.FailReason)
	assert.Equal(t, model.PoolAdminStatusEnabled, after.AdminStatus)

	// An in-place refresh of an existing file (same name, new timestamp)
	// cannot prove task ownership — background refresh mutates timestamps
	// too — so completion fails closed: no correlation, no deletion, the
	// original credential and its fingerprint stay intact.
	reauth = setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/reauth", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, reauth.Code)
	inPlace := len(setup.executor.deleteCalls)
	setup.executor.files = []accountpool.CredentialEntry{
		{Name: "synthetic-auth-file.json", Email: poolRawIdentity, UpdatedAt: 300},
	}
	refreshed := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"code": "abc2", "state": "b"})
	assert.Equal(t, http.StatusBadGateway, refreshed.Code)
	assert.Contains(t, refreshed.Body.String(), "POOL_CORRELATION_FAILED")
	assert.Len(t, setup.executor.deleteCalls, inPlace, "an existing file must never be deleted as an orphan")
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, id, after.Id)
	assert.Equal(t, "synthetic-auth-file.json", after.CredentialRef)
	assert.NotEmpty(t, after.IdentityFingerprint)
	assert.Equal(t, "correlation_failed", after.LastError)
	var staleTask model.UpstreamPoolAuthTask
	require.NoError(t, setup.db.Where("account_id = ?", id).Order("id desc").First(&staleTask).Error)
	assert.Equal(t, model.PoolTaskFailCorrelation, staleTask.FailReason)
	// A provably new file still correlates and re-auths the account.
	reauth = setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/reauth", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, reauth.Code)
	setup.executor.files = []accountpool.CredentialEntry{
		{Name: "synthetic-auth-file.json", Email: poolRawIdentity, UpdatedAt: 300},
		{Name: "renamed-auth-file.json", Email: poolRawIdentity, UpdatedAt: 400},
	}
	refreshed = setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"code": "abc3", "state": "b"})
	require.Equal(t, http.StatusOK, refreshed.Code, refreshed.Body.String())
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, "renamed-auth-file.json", after.CredentialRef)
	assert.Equal(t, model.PoolRunStatusNotAdmitted, after.RunStatus)
	assert.NotEmpty(t, after.IdentityFingerprint)
}

func TestPoolCredentialTakenConflictsWithoutDeleting(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	first := ownerAccount(t, setup, "claude", "oauth_browser")
	firstID := int(accountField(first, "id"))
	completed := startAuthAndComplete(t, setup, firstID, []accountpool.CredentialEntry{
		{Name: "shared-auth-file.json", Email: poolRawIdentity, UpdatedAt: 100},
	})
	require.Equal(t, http.StatusOK, completed.Code)

	// A second account completes and its correlated credential equals the
	// first account's reference. Nothing may be deleted: the file could be
	// the other account's credential.
	second := ownerAccount(t, setup, "claude", "oauth_browser")
	secondID := int(accountField(second, "id"))
	setup.executor.files = nil
	start := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", secondID), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, start.Code)
	setup.executor.files = []accountpool.CredentialEntry{
		{Name: "shared-auth-file.json", Email: poolRawIdentity, UpdatedAt: 999},
	}
	taken := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", secondID), "synthetic-owner-pat", map[string]any{"callback_url": "https://x.example/cb?code=a&state=b"})
	assert.Equal(t, http.StatusConflict, taken.Code)
	assert.Empty(t, setup.executor.deleteCalls)
	var secondRow model.UpstreamPoolAccount
	require.NoError(t, setup.db.First(&secondRow, secondID).Error)
	assert.Empty(t, secondRow.CredentialRef)
	assert.Equal(t, "credential_ref_taken", secondRow.LastError)
}

func TestPoolLocalCommitFailureCleansCorrelatedOrphan(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account := ownerAccount(t, setup, "claude", "oauth_browser")
	id := int(accountField(account, "id"))
	setup.executor.files = []accountpool.CredentialEntry{
		{Name: "baseline-kept.json", Email: "baseline@kept.test", UpdatedAt: 5},
	}
	start := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, start.Code)
	setup.executor.files = []accountpool.CredentialEntry{
		{Name: "orphan-commit.json", Email: poolRawIdentity, UpdatedAt: 10},
		{Name: "baseline-kept.json", Email: "baseline@kept.test", UpdatedAt: 5},
	}
	// Fail the local account write after the executor already created the
	// credential: the response must not be a success and the correlated
	// orphan is cleaned up while no other credential is touched.
	callback := "synthetic_local_commit_failure"
	require.NoError(t, setup.db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "upstream_pool_accounts" {
			tx.AddError(fmt.Errorf("synthetic local commit failure"))
		}
	}))
	failed := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"callback_url": "https://x.example/cb?code=a&state=b"})
	require.NoError(t, setup.db.Callback().Update().Remove(callback))
	assert.NotEqual(t, http.StatusOK, failed.Code)
	assert.Equal(t, []string{"orphan-commit.json"}, setup.executor.deleteCalls, "only the task-created orphan is cleaned; the baseline file must never be touched")
	var after model.UpstreamPoolAccount
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Empty(t, after.CredentialRef)
	var task model.UpstreamPoolAuthTask
	require.NoError(t, setup.db.Where("account_id = ?", id).Order("id desc").First(&task).Error)
	assert.NotEqual(t, model.PoolTaskStatusCompleted, task.Status)
}

func TestPoolPauseResumeAndConcurrentWriters(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account := ownerAccount(t, setup, "codex", "oauth_device")
	id := int(accountField(account, "id"))
	version := int(accountField(account, "version"))

	// No credential yet: pause is a local-only flip.
	paused := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/pause", id), "synthetic-owner-pat", map[string]any{"expected_version": version})
	require.Equal(t, http.StatusOK, paused.Code, paused.Body.String())
	var after model.UpstreamPoolAccount
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, model.PoolAdminStatusPaused, after.AdminStatus)
	assert.Equal(t, model.PoolRunStatusAwaitingAuth, after.RunStatus)
	assert.Empty(t, setup.executor.pauseCalls)

	// Resume is gated on admission: phase two has no admission, so resume
	// fails explicitly, never touches the executor and never lifts the pause.
	resumed := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/resume", id), "synthetic-owner-pat", map[string]any{"expected_version": after.Version})
	assert.Equal(t, http.StatusConflict, resumed.Code)
	assert.Contains(t, resumed.Body.String(), "POOL_NOT_ADMITTED")
	assert.Empty(t, setup.executor.resumeCalls)
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, model.PoolAdminStatusPaused, after.AdminStatus)

	// With a credential, a later pause reaches the executor (remote first).
	require.NoError(t, setup.db.Model(&after).Update("credential_ref", "synthetic-auth-file.json").Error)
	require.NoError(t, setup.db.First(&after, id).Error)
	setup.executor.pauseCalls = nil
	paused = setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/pause", id), "synthetic-owner-pat", map[string]any{"expected_version": after.Version})
	require.Equal(t, http.StatusOK, paused.Code, paused.Body.String())
	assert.Equal(t, []string{"synthetic-auth-file.json"}, setup.executor.pauseCalls)

	// Remote pause failure leaves the local row untouched: no fake success.
	setup.executor.pauseErr = accountpool.ErrPoolExecutorOffline
	require.NoError(t, setup.db.First(&after, id).Error)
	require.NoError(t, setup.db.Model(&after).Update("admin_status", model.PoolAdminStatusEnabled).Error)
	require.NoError(t, setup.db.First(&after, id).Error)
	failedPause := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/pause", id), "synthetic-owner-pat", map[string]any{"expected_version": after.Version})
	assert.Equal(t, http.StatusBadGateway, failedPause.Code)
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, model.PoolAdminStatusEnabled, after.AdminStatus, "failed remote pause must not flip the local row")
	setup.executor.pauseErr = nil

	// A stale version after a successful remote flip is resolved by one
	// fresh re-apply (single authorized writer of the admin column).
	require.NoError(t, setup.db.First(&after, id).Error)
	staleVersion := after.Version - 1
	require.Greater(t, staleVersion, 0)
	stale := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/pause", id), "synthetic-owner-pat", map[string]any{"expected_version": staleVersion})
	assert.Equal(t, http.StatusOK, stale.Code, stale.Body.String())
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, model.PoolAdminStatusPaused, after.AdminStatus)

	// Two concurrent writers holding the same version: the version guard
	// serializes them; the loser re-reads and re-applies the same target
	// value once (single authorized writer of the admin column), so both
	// responses are honest and the final state is paused.
	concurrentVersion := after.Version
	pauseResults := make(chan int, 2)
	for range 2 {
		go func() {
			pauseResults <- setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/pause", id), "synthetic-owner-pat", map[string]any{"expected_version": concurrentVersion}).Code
		}()
	}
	wins := 0
	for range 2 {
		if code := <-pauseResults; code < 400 {
			wins++
		}
	}
	assert.GreaterOrEqual(t, wins, 1)
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, model.PoolAdminStatusPaused, after.AdminStatus)
}

func TestPoolAuthorizationAfterPauseKeepsPause(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account := ownerAccount(t, setup, "codex", "oauth_device")
	id := int(accountField(account, "id"))

	// Pause, then complete authorization: the pause column is untouched by
	// the authorization path and the fresh credential is parked remotely.
	paused := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/pause", id), "synthetic-owner-pat", map[string]any{"expected_version": int(accountField(account, "version"))})
	require.Equal(t, http.StatusOK, paused.Code)
	completed := startAuthAndComplete(t, setup, id, []accountpool.CredentialEntry{
		{Name: "parked-auth-file.json", Email: poolRawIdentity, UpdatedAt: 100},
	})
	require.Equal(t, http.StatusOK, completed.Code, completed.Body.String())
	var after model.UpstreamPoolAccount
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, model.PoolAdminStatusPaused, after.AdminStatus, "authorization never writes the admin column")
	assert.Equal(t, model.PoolRunStatusNotAdmitted, after.RunStatus)
	// Pausing again on the authorized account disables the credential remotely.
	setup.executor.pauseCalls = nil
	paused = setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/pause", id), "synthetic-owner-pat", map[string]any{"expected_version": after.Version})
	require.Equal(t, http.StatusOK, paused.Code)
	assert.Equal(t, []string{"parked-auth-file.json"}, setup.executor.pauseCalls)
}

func TestPoolExecutorOfflineAndFailureRetention(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account := ownerAccount(t, setup, "claude", "oauth_browser")
	id := int(accountField(account, "id"))

	// Executor unreachable at baseline listing: start fails offline and only
	// records the fixed run status.
	setup.executor.listErr = accountpool.ErrPoolExecutorOffline
	offline := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{})
	assert.Equal(t, http.StatusBadGateway, offline.Code)
	assert.Contains(t, offline.Body.String(), "POOL_EXECUTOR_OFFLINE")
	var after model.UpstreamPoolAccount
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, model.PoolRunStatusExecutorOffline, after.RunStatus)
	assert.Equal(t, model.PoolAdminStatusEnabled, after.AdminStatus, "executor failure must not change admin status")
	assert.Equal(t, "executor_offline", after.LastError)
	setup.executor.listErr = nil

	// Completion failure state is retained, not reported as success.
	require.NoError(t, setup.db.Model(&after).Update("run_status", model.PoolRunStatusAuthorizing).Error)
	require.NoError(t, setup.db.Create(&model.UpstreamPoolAuthTask{AccountId: id, OwnerUserId: 1, Platform: "claude", AccessType: "oauth_browser", Kind: "initial", Method: "oauth_browser", ExecutorId: "cliproxyapi", ExecutorStateRef: "s", BaselineRefs: "{}", Status: model.PoolTaskStatusPending, PrevRunStatus: model.PoolRunStatusAwaitingAuth, ExpiresAt: time.Now().Add(time.Hour).Unix()}).Error)
	setup.executor.completeErr = accountpool.ErrPoolExecutorFailed
	failed := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"code": "c", "state": "s"})
	assert.Equal(t, http.StatusBadGateway, failed.Code)
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.NotEqual(t, model.PoolRunStatusNotAdmitted, after.RunStatus)
	assert.Equal(t, "executor_error", after.LastError)
	assert.NotContains(t, after.LastError, "synthetic")

	// Grok2API executor is catalog-only: every operation reports offline.
	grok := setup.request("POST", "/api/owner/pool/accounts", "synthetic-owner-pat", map[string]any{"platform": "grok_build", "access_type": "oauth_device"})
	require.Equal(t, http.StatusCreated, grok.Code)
	var decoded struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, common.Unmarshal(grok.Body.Bytes(), &decoded))
	grokID := int(accountField(decoded.Data, "id"))
	grokStart := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", grokID), "synthetic-owner-pat", map[string]any{})
	assert.Equal(t, http.StatusBadGateway, grokStart.Code)
	executors := setup.request("GET", "/api/owner/pool/executors", "synthetic-owner-pat", nil)
	assert.Contains(t, executors.Body.String(), "\"connected\":false")
}

func TestPoolDeleteRequiresExecutorConfirmation(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account := ownerAccount(t, setup, "claude", "oauth_browser")
	id := int(accountField(account, "id"))

	// With a credential, deletion only happens after executor confirmation.
	require.NoError(t, setup.db.Model(&model.UpstreamPoolAccount{}).Where("id = ?", id).Update("credential_ref", "synthetic-auth-file.json").Error)
	setup.executor.deleteErr = accountpool.ErrPoolExecutorOffline
	blocked := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/delete", id), "synthetic-owner-pat", map[string]any{})
	assert.Equal(t, http.StatusBadGateway, blocked.Code)
	require.Len(t, setup.executor.deleteCalls, 1)
	var stillThere model.UpstreamPoolAccount
	require.NoError(t, setup.db.First(&stillThere, id).Error)

	setup.executor.deleteErr = nil
	setup.executor.deleteCalls = nil
	deleted := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/delete", id), "synthetic-owner-pat", map[string]any{})
	assert.Equal(t, http.StatusOK, deleted.Code)
	assert.Equal(t, []string{"synthetic-auth-file.json"}, setup.executor.deleteCalls)
	var count int64
	require.NoError(t, setup.db.Model(&model.UpstreamPoolAccount{}).Where("id = ?", id).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, setup.db.Model(&model.UpstreamPoolAuthTask{}).Where("account_id = ?", id).Count(&count).Error)
	assert.Zero(t, count)

	// Without a credential the row is removed without touching the executor.
	account = ownerAccount(t, setup, "claude", "oauth_browser")
	id = int(accountField(account, "id"))
	calls := len(setup.executor.deleteCalls)
	require.Equal(t, http.StatusOK, setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/delete", id), "synthetic-owner-pat", map[string]any{}).Code)
	assert.Equal(t, calls, len(setup.executor.deleteCalls))
}

func TestPoolSyncRunStatusPreservesAdminStatus(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account := ownerAccount(t, setup, "kimi_coding", "oauth_device")
	id := int(accountField(account, "id"))
	require.NoError(t, setup.db.Model(&model.UpstreamPoolAccount{}).Where("id = ?", id).Updates(map[string]any{"credential_ref": "kimi.json", "run_status": model.PoolRunStatusNotAdmitted, "admin_status": model.PoolAdminStatusPaused}).Error)

	// A credential parked by the admission gate maps to not_admitted, not to
	// a re-auth requirement.
	setup.executor.credState = accountpool.CredentialStateDisabled
	synced := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/sync", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, synced.Code)
	var after model.UpstreamPoolAccount
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, model.PoolRunStatusNotAdmitted, after.RunStatus)

	setup.executor.credState = accountpool.CredentialStateCooldown
	synced = setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/sync", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, synced.Code)
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, model.PoolRunStatusCooldown, after.RunStatus)
	assert.Equal(t, model.PoolAdminStatusPaused, after.AdminStatus, "sync must never lift an owner pause")

	setup.executor.credState = accountpool.CredentialStateInvalid
	synced = setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/sync", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, synced.Code)
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, model.PoolRunStatusReauthRequired, after.RunStatus)

	setup.executor.healthErr = accountpool.ErrPoolExecutorOffline
	synced = setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/sync", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, synced.Code)
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, model.PoolRunStatusExecutorOffline, after.RunStatus)
	assert.Equal(t, model.PoolAdminStatusPaused, after.AdminStatus)

	// Masked output check across a list response.
	list := setup.request("GET", "/api/owner/pool/accounts", "synthetic-owner-pat", nil)
	require.Equal(t, http.StatusOK, list.Code)
	assert.NotContains(t, list.Body.String(), poolRawIdentity)
	assert.NotContains(t, strings.ToLower(list.Body.String()), "token", "no credential fields leak")
}

func TestPoolConcurrentStartAtomicClaim(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	first := ownerAccount(t, setup, "claude", "oauth_browser")
	second := ownerAccount(t, setup, "codex", "oauth_browser")
	firstID, secondID := int(accountField(first, "id")), int(accountField(second, "id"))

	// Two accounts start authorization at the same time on the same
	// executor: the atomic claim admits exactly one, across processes too.
	results := make(chan int, 2)
	for _, id := range []int{firstID, secondID} {
		go func(accountID int) {
			results <- setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", accountID), "synthetic-owner-pat", map[string]any{}).Code
		}(id)
	}
	wins, conflicts := 0, 0
	for range 2 {
		switch code := <-results; code {
		case http.StatusOK:
			wins++
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("unexpected status %d", code)
		}
	}
	assert.Equal(t, 1, wins)
	assert.Equal(t, 1, conflicts)
	var pendingTasks, claims int64
	require.NoError(t, setup.db.Model(&model.UpstreamPoolAuthTask{}).Where("status = ?", model.PoolTaskStatusPending).Count(&pendingTasks).Error)
	require.NoError(t, setup.db.Model(&model.UpstreamPoolAuthClaim{}).Count(&claims).Error)
	assert.EqualValues(t, 1, pendingTasks)
	assert.EqualValues(t, 1, claims)

	// The losing account can start once the winner's claim is released.
	require.NoError(t, setup.db.Model(&model.UpstreamPoolAuthTask{}).Where("status = ?", model.PoolTaskStatusPending).Update("status", model.PoolTaskStatusCancelled).Error)
	require.NoError(t, setup.db.Where("1 = 1").Delete(&model.UpstreamPoolAuthClaim{}).Error)
	started := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", secondID), "synthetic-owner-pat", map[string]any{})
	assert.Equal(t, http.StatusOK, started.Code, started.Body.String())
}

func TestPoolClaimTakeoverOnExpiryAndSweep(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account := ownerAccount(t, setup, "claude", "oauth_browser")
	other := ownerAccount(t, setup, "claude", "oauth_browser")
	id, otherID := int(accountField(account, "id")), int(accountField(other, "id"))

	// A leftover from a dead process: expired claim on a stale pending task.
	staleTask := &model.UpstreamPoolAuthTask{AccountId: id, OwnerUserId: 1, Platform: "claude", AccessType: "oauth_browser", Kind: "initial", Method: "oauth_browser", ExecutorId: "cliproxyapi", ExecutorStateRef: "stale", Status: model.PoolTaskStatusPending, ExpiresAt: time.Now().Add(-time.Hour).Unix()}
	require.NoError(t, setup.db.Create(staleTask).Error)
	require.NoError(t, setup.db.Create(&model.UpstreamPoolAuthClaim{ExecutorId: "cliproxyapi", AccountId: id, TaskId: staleTask.Id, ExpiresAt: time.Now().Add(-time.Hour).Unix()}).Error)

	// A claim orphaned by a crash after its task settled.
	require.NoError(t, setup.db.Create(&model.UpstreamPoolAuthClaim{ExecutorId: "other-executor", AccountId: id, TaskId: 999999, ExpiresAt: time.Now().Add(time.Hour).Unix()}).Error)

	started := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", otherID), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, started.Code, started.Body.String())
	var stale model.UpstreamPoolAuthTask
	require.NoError(t, setup.db.First(&stale, staleTask.Id).Error)
	assert.Equal(t, model.PoolTaskStatusExpired, stale.Status, "takeover expires the abandoned task")
	var claims []model.UpstreamPoolAuthClaim
	require.NoError(t, setup.db.Find(&claims).Error)
	require.Len(t, claims, 1)
	assert.Equal(t, otherID, claims[0].AccountId)
	assert.Equal(t, startedTaskID(t, setup, otherID), claims[0].TaskId, "orphaned claim is swept, expired claim is taken over")
}

func startedTaskID(t *testing.T, setup *poolTestSetup, accountID int) int {
	t.Helper()
	var task model.UpstreamPoolAuthTask
	require.NoError(t, setup.db.Where("account_id = ? AND status = ?", accountID, model.PoolTaskStatusPending).First(&task).Error)
	return task.Id
}

func TestPoolCancelCompleteRace(t *testing.T) {
	for round := 0; round < 6; round++ {
		setup := newPoolTestSetup(t, true)
		require.NoError(t, model.MigrateUpstreamPool(setup.db))
		account := ownerAccount(t, setup, "claude", "oauth_browser")
		id := int(accountField(account, "id"))
		setup.executor.files = []accountpool.CredentialEntry{
			{Name: "baseline-kept.json", Email: "baseline@kept.test", UpdatedAt: 5},
		}
		start := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{})
		require.Equal(t, http.StatusOK, start.Code)
		var started struct {
			Data struct {
				TaskId int `json:"task_id"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(start.Body.Bytes(), &started))
		setup.executor.files = []accountpool.CredentialEntry{
			{Name: "baseline-kept.json", Email: "baseline@kept.test", UpdatedAt: 5},
			{Name: "race-new-file.json", Email: poolRawIdentity, UpdatedAt: 6},
		}
		cancelDone := make(chan int, 1)
		completeDone := make(chan int, 1)
		go func() {
			cancelDone <- setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/cancel", id), "synthetic-owner-pat", map[string]any{"task_id": started.Data.TaskId}).Code
		}()
		go func() {
			completeDone <- setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"callback_url": "https://x.example/cb?code=a&state=b"}).Code
		}()
		cancelCode, completeCode := <-cancelDone, <-completeDone
		var after model.UpstreamPoolAccount
		require.NoError(t, setup.db.First(&after, id).Error)
		deletedBaseline := false
		for _, ref := range setup.executor.deleteCalls {
			if ref == "baseline-kept.json" {
				deletedBaseline = true
			}
		}
		assert.False(t, deletedBaseline, "the baseline credential is never deleted, in any race outcome")
		if completeCode < 400 {
			// Completion won: the credential is claimed and parked; the
			// cancel must report failure, not success, and must not delete
			// the credential the completion just claimed.
			assert.Equal(t, http.StatusConflict, cancelCode)
			assert.Equal(t, "race-new-file.json", after.CredentialRef)
			assert.Equal(t, model.PoolRunStatusNotAdmitted, after.RunStatus)
		} else {
			// Cancel won: the task-created orphan is cleaned, baseline kept.
			assert.Equal(t, []string{"race-new-file.json"}, setup.executor.deleteCalls)
			assert.Empty(t, after.CredentialRef)
		}
	}
}

func TestPoolRefreshInterferenceDoesNotBreakCorrelation(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	account := ownerAccount(t, setup, "claude", "oauth_browser")
	id := int(accountField(account, "id"))
	setup.executor.files = []accountpool.CredentialEntry{
		{Name: "old-credential.json", Email: "old@keeper.test", UpdatedAt: 100},
	}
	start := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/start", id), "synthetic-owner-pat", map[string]any{})
	require.Equal(t, http.StatusOK, start.Code)
	// Background token refresh touches the old file during the task; the new
	// authorization file still correlates unambiguously.
	setup.executor.files = []accountpool.CredentialEntry{
		{Name: "old-credential.json", Email: "old@keeper.test", UpdatedAt: 999},
		{Name: "fresh-credential.json", Email: poolRawIdentity, UpdatedAt: 1000},
	}
	completed := setup.request("POST", fmt.Sprintf("/api/owner/pool/accounts/%d/auth/complete", id), "synthetic-owner-pat", map[string]any{"callback_url": "https://x.example/cb?code=a&state=b"})
	require.Equal(t, http.StatusOK, completed.Code, completed.Body.String())
	var after model.UpstreamPoolAccount
	require.NoError(t, setup.db.First(&after, id).Error)
	assert.Equal(t, "fresh-credential.json", after.CredentialRef)
	assert.Equal(t, "s***@e***", after.IdentityMasked)
	// The refreshed-but-pre-existing file is never treated as the new
	// credential nor cleaned up.
	assert.NotContains(t, setup.executor.deleteCalls, "old-credential.json")
}
