package accountpool

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// ExecutorClient is the credential-holding executor's management surface. The
// executor owns credentials, token refresh and account scheduling; this pool
// module only proxies management operations and never stores or refreshes
// credentials itself.
type ExecutorClient interface {
	Health(ctx context.Context) error
	// StartAuth opens an authorization session on the executor. For browser
	// flows it returns the authorization URL; for device flows the
	// verification URI (in AuthURL) plus user code. The returned StateRef is
	// the executor session key.
	StartAuth(ctx context.Context, req StartAuthRequest) (StartAuthResult, error)
	// AuthStatus maps the executor session state machine. The fixed v8.0.4
	// protocol only knows ok/wait/error on the wire; cancelled sessions are
	// indistinguishable from expired ones ("unknown or expired state").
	AuthStatus(ctx context.Context, stateRef string) (AuthState, error)
	// CompleteAuth drives one authorization round to a terminal state: for
	// browser flows it submits the pasted callback (which does NOT complete
	// the flow — the executor exchanges the code asynchronously), for device
	// flows it skips submission entirely, then polls AuthStatus until the
	// deadline. It returns when the session reached "completed"; correlation
	// to a concrete credential happens via ListCredentials diffing.
	CompleteAuth(ctx context.Context, req CompleteAuthRequest) error
	// ListCredentials returns the current credential entries (name, non-secret
	// display email, updated timestamp, disabled flag).
	ListCredentials(ctx context.Context) ([]CredentialEntry, error)
	// CancelAuth cancels a still-pending executor session. If the session
	// already completed this may fail; callers must check AuthStatus first.
	CancelAuth(ctx context.Context, stateRef string) error
	DeleteCredential(ctx context.Context, credentialRef string) error
	PauseCredential(ctx context.Context, credentialRef string) error
	ResumeCredential(ctx context.Context, credentialRef string) error
	CredentialStatus(ctx context.Context, credentialRef string) (ExecutorCredentialStatus, error)
}

type StartAuthRequest struct {
	Platform   string
	AccessType string
	Kind       string // initial | reauth
}

type StartAuthResult struct {
	Method    string // oauth_browser | oauth_device
	AuthURL   string // authorization URL (browser) or verification URI (device)
	UserCode  string // device flows only
	StateRef  string
	ExpiresAt time.Time
}

type AuthState string

const (
	AuthStateUnknown   AuthState = "unknown"
	AuthStateWaiting   AuthState = "waiting"
	AuthStateCompleted AuthState = "completed"
	AuthStateExpired   AuthState = "expired"
	AuthStateFailed    AuthState = "failed"
)

type CompleteAuthRequest struct {
	Platform    string
	AccessType  string
	Kind        string
	Method      string
	StateRef    string
	Code        string
	State       string
	CallbackURL string // full callback URL pasted back by the owner; parsed, never fetched
	Deadline    time.Time
}

// CredentialEntry is a credential projection containing no secret material.
type CredentialEntry struct {
	Name      string
	Email     string
	UpdatedAt int64
	Disabled  bool
}

// ExecutorCredentialStatus uses only fixed tokens so upstream response text
// never leaks into pool state.
type ExecutorCredentialStatus struct {
	State string // active | disabled | invalid | cooldown
}

const (
	CredentialStateActive   = "active"
	CredentialStateDisabled = "disabled"
	CredentialStateInvalid  = "invalid"
	CredentialStateCooldown = "cooldown"
)

// CliproxyEndpointConfigured reports whether the internal CLIProxyAPI
// management endpoint has been provisioned via environment. The values are
// process configuration and are never exposed through any API response.
func CliproxyEndpointConfigured() bool {
	return os.Getenv("CLEARAPI_POOL_CLIPROXY_URL") != ""
}

// ExecutorFactory is a package variable so tests can inject a mock executor.
// The default resolves the built-in CLIProxyAPI client; the grok2api executor
// has no management client yet and reports offline until it is adopted.
var ExecutorFactory = defaultExecutorFactory

func executorFor(id string) (ExecutorClient, error) {
	return ExecutorFactory(id)
}

func defaultExecutorFactory(id string) (ExecutorClient, error) {
	switch id {
	case ExecutorCliproxyAPI:
		return NewCliproxyClientFromEnv(), nil
	case ExecutorGrok2API, ExecutorNone:
		return NewOfflineExecutor(), nil
	default:
		return nil, ErrPoolInvalid
	}
}

// NewOfflineExecutor is the explicit fail-closed client used for executors
// that exist only as catalog entries; every operation reports offline.
func NewOfflineExecutor() ExecutorClient { return offlineExecutor{} }

type offlineExecutor struct{}

func (offlineExecutor) Health(context.Context) error { return ErrPoolExecutorOffline }
func (offlineExecutor) StartAuth(context.Context, StartAuthRequest) (StartAuthResult, error) {
	return StartAuthResult{}, ErrPoolExecutorOffline
}
func (offlineExecutor) AuthStatus(context.Context, string) (AuthState, error) {
	return AuthStateUnknown, ErrPoolExecutorOffline
}
func (offlineExecutor) CompleteAuth(context.Context, CompleteAuthRequest) error {
	return ErrPoolExecutorOffline
}
func (offlineExecutor) ListCredentials(context.Context) ([]CredentialEntry, error) {
	return nil, ErrPoolExecutorOffline
}
func (offlineExecutor) CancelAuth(context.Context, string) error { return ErrPoolExecutorOffline }
func (offlineExecutor) DeleteCredential(context.Context, string) error {
	return ErrPoolExecutorOffline
}
func (offlineExecutor) PauseCredential(context.Context, string) error { return ErrPoolExecutorOffline }
func (offlineExecutor) ResumeCredential(context.Context, string) error {
	return ErrPoolExecutorOffline
}
func (offlineExecutor) CredentialStatus(context.Context, string) (ExecutorCredentialStatus, error) {
	return ExecutorCredentialStatus{}, ErrPoolExecutorOffline
}

// ---------------------------------------------------------------------------
// CLIProxyAPI v8.0.4 management client (commit d33f63f8e3d98428440ebca5a5b6a981a61ff71e)
//
// NOT YET RUN-VERIFIED against a live executor; every endpoint path, request
// body and response shape below is taken from the pinned source:
//   - start:    internal/api/handlers/management/auth_files_provider_oauth.go
//               browser {"status":"ok","url","state"};
//               device  {"status":"ok","url","state","flow":"device","user_code","expires_in"}
//   - callback: internal/api/handlers/management/oauth_callback.go
//               body {provider, redirect_url, code, state, error} -> {"status":"ok"};
//               receiving it only drops a .oauth file, completion is async
//   - status:   GET /v0/management/get-auth-status?state=... always HTTP 200 with
//               {"status":"ok"|"wait"|"error","error":"..."}; unknown/expired/cancelled
//               all surface as error "unknown or expired state"; completed sessions
//               are purged after one minute
//   - listing:  GET /v0/management/auth-files, entries carry name/email/disabled/
//               updated_at as an RFC3339 time.Time string, omitted when zero
//               (no session correlation field exists in the protocol)
//   - disable:  PATCH /v0/management/auth-files/status body {name, disabled}
//   - delete:   DELETE /v0/management/auth-files?name=...
// Response parsing is defensive: any shape mismatch fails closed with a fixed
// error code; bodies are never surfaced verbatim in errors or logs.
// ---------------------------------------------------------------------------

type CliproxyClient struct {
	baseURL string
	key     string
	http    *http.Client
	// PollInterval is overridable for tests; production polls every 2s within
	// the deadline supplied by the caller.
	PollInterval time.Duration
}

func NewCliproxyClientFromEnv() *CliproxyClient {
	return &CliproxyClient{
		baseURL:      strings.TrimRight(os.Getenv("CLEARAPI_POOL_CLIPROXY_URL"), "/"),
		key:          os.Getenv("CLEARAPI_POOL_CLIPROXY_KEY"),
		http:         &http.Client{Timeout: 15 * time.Second},
		PollInterval: 2 * time.Second,
	}
}

var cliproxyProviderByPlatform = map[string]string{
	"claude":      "anthropic",
	"codex":       "codex",
	"kimi_coding": "kimi",
	"grok_build":  "xai",
}

func (c *CliproxyClient) do(ctx context.Context, method, path string, body string) (int, map[string]any, error) {
	if c.baseURL == "" {
		return 0, nil, ErrPoolExecutorOffline
	}
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return 0, nil, ErrPoolExecutorFailed
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if c.key != "" {
		request.Header.Set("X-Management-Key", c.key)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return 0, nil, ErrPoolExecutorOffline
	}
	defer response.Body.Close()
	// Bounded read; the body is parsed for a few known fields and never
	// surfaced verbatim in errors or logs.
	limited, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return response.StatusCode, nil, ErrPoolExecutorFailed
	}
	var decoded map[string]any
	if len(limited) > 0 {
		if err := common.Unmarshal(limited, &decoded); err != nil {
			decoded = nil
		}
	}
	return response.StatusCode, decoded, nil
}

func mapField[T any](decoded map[string]any, keys ...string) (T, bool) {
	var zero T
	for _, key := range keys {
		if value, ok := decoded[key]; ok {
			if typed, ok := value.(T); ok {
				return typed, true
			}
		}
	}
	return zero, false
}

func (c *CliproxyClient) Health(ctx context.Context) error {
	status, _, err := c.do(ctx, http.MethodGet, "/v0/management/auth-files", "")
	if err != nil {
		return err
	}
	if status >= 500 {
		return ErrPoolExecutorOffline
	}
	if status >= 400 {
		return ErrPoolExecutorFailed
	}
	return nil
}

func (c *CliproxyClient) StartAuth(ctx context.Context, req StartAuthRequest) (StartAuthResult, error) {
	provider, ok := cliproxyProviderByPlatform[req.Platform]
	if !ok {
		return StartAuthResult{}, ErrPoolUnsupported
	}
	status, decoded, err := c.do(ctx, http.MethodGet, "/v0/management/"+provider+"-auth-url", "")
	if err != nil {
		return StartAuthResult{}, err
	}
	if status != http.StatusOK || decoded == nil {
		return StartAuthResult{}, ErrPoolExecutorFailed
	}
	if text, _ := mapField[string](decoded, "error"); text != "" {
		return StartAuthResult{}, ErrPoolExecutorFailed
	}
	if value, _ := mapField[string](decoded, "status"); value != "ok" {
		return StartAuthResult{}, ErrPoolExecutorFailed
	}
	authURL, hasURL := mapField[string](decoded, "url")
	state, hasState := mapField[string](decoded, "state")
	if !hasURL || !hasState || authURL == "" || state == "" {
		return StartAuthResult{}, ErrPoolExecutorFailed
	}
	result := StartAuthResult{Method: AuthMethodOAuthBrowser, AuthURL: authURL, StateRef: state}
	if flow, _ := mapField[string](decoded, "flow"); flow == "device" {
		result.Method = AuthMethodOAuthDevice
	}
	if code, ok := mapField[string](decoded, "user_code"); ok {
		result.UserCode = code
		result.Method = AuthMethodOAuthDevice
	}
	if expires, ok := mapField[float64](decoded, "expires_in"); ok && expires > 0 {
		result.ExpiresAt = time.Now().Add(time.Duration(expires) * time.Second)
	}
	return result, nil
}

func (c *CliproxyClient) AuthStatus(ctx context.Context, stateRef string) (AuthState, error) {
	if stateRef == "" {
		return AuthStateUnknown, ErrPoolInvalid
	}
	status, decoded, err := c.do(ctx, http.MethodGet, "/v0/management/get-auth-status?state="+stateRef, "")
	if err != nil {
		return AuthStateUnknown, err
	}
	if status != http.StatusOK || decoded == nil {
		return AuthStateUnknown, ErrPoolExecutorFailed
	}
	switch value, _ := mapField[string](decoded, "status"); value {
	case "ok":
		return AuthStateCompleted, nil
	case "wait":
		return AuthStateWaiting, nil
	case "error":
		if text, _ := mapField[string](decoded, "error"); strings.Contains(text, "unknown or expired") {
			return AuthStateExpired, nil
		}
		return AuthStateFailed, nil
	default:
		return AuthStateUnknown, ErrPoolExecutorFailed
	}
}

func (c *CliproxyClient) CompleteAuth(ctx context.Context, req CompleteAuthRequest) error {
	provider, ok := cliproxyProviderByPlatform[req.Platform]
	if !ok {
		return ErrPoolUnsupported
	}
	// Browser flows: submit the pasted callback. Per the fixed protocol this
	// only enqueues the code for the executor's background exchange — it is
	// not completion, so the status poll below is mandatory.
	if req.Method != AuthMethodOAuthDevice {
		payload, err := common.Marshal(map[string]any{
			"provider":     provider,
			"redirect_url": req.CallbackURL,
			"code":         req.Code,
			"state":        req.State,
		})
		if err != nil {
			return ErrPoolExecutorFailed
		}
		status, decoded, err := c.do(ctx, http.MethodPost, "/v0/management/oauth-callback", string(payload))
		if err != nil {
			return err
		}
		if status >= 500 {
			return ErrPoolExecutorOffline
		}
		if status >= 400 || decoded == nil {
			return ErrPoolExecutorFailed
		}
		if value, _ := mapField[string](decoded, "status"); value != "ok" {
			return ErrPoolExecutorFailed
		}
	}
	// Device flows never submit a browser callback; both flows converge on
	// polling get-auth-status until a terminal state or the deadline.
	pollInterval := c.PollInterval
	if pollInterval <= 0 {
		pollInterval = 2 * time.Second
	}
	for {
		state, err := c.AuthStatus(ctx, req.StateRef)
		if err != nil {
			return err
		}
		switch state {
		case AuthStateCompleted:
			return nil
		case AuthStateExpired:
			return ErrPoolTaskExpired
		case AuthStateFailed:
			return ErrPoolExecutorFailed
		}
		if !req.Deadline.IsZero() && !time.Now().Before(req.Deadline) {
			return ErrPoolAuthTimeout
		}
		select {
		case <-ctx.Done():
			return ErrPoolExecutorOffline
		case <-time.After(pollInterval):
		}
	}
}

func (c *CliproxyClient) ListCredentials(ctx context.Context) ([]CredentialEntry, error) {
	status, decoded, err := c.do(ctx, http.MethodGet, "/v0/management/auth-files", "")
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK || decoded == nil {
		return nil, ErrPoolExecutorFailed
	}
	files, ok := mapField[[]any](decoded, "files", "auth_files", "data")
	if !ok {
		return nil, ErrPoolExecutorFailed
	}
	entries := make([]CredentialEntry, 0, len(files))
	for _, item := range files {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := mapField[string](entry, "name", "id")
		if name == "" {
			continue
		}
		email, _ := mapField[string](entry, "email", "account", "account_id")
		credential := CredentialEntry{Name: name, Email: email}
		if disabled, ok := mapField[bool](entry, "disabled"); ok {
			credential.Disabled = disabled
		}
		updated, ok := entryTime(entry)
		if !ok {
			// The fixed v8.0.4 build omits updated_at/modtime when the auth
			// zero-time is unset and serializes time.Time as RFC3339. A
			// missing or malformed timestamp is a contract violation: the
			// baseline diff depends on it, so fail closed instead of
			// silently defaulting to zero.
			return nil, ErrPoolExecutorFailed
		}
		credential.UpdatedAt = updated
		entries = append(entries, credential)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

// entryTime reads the auth-file entry timestamp exactly as the pinned
// serializer emits it: auth.UpdatedAt / os stat time.Time marshalled to an
// RFC3339 string (internal/api/handlers/management/auth_files.go:718-721,
// 733). Returns false when the key is missing (zero time is omitted) or not
// a valid RFC3339 string.
func entryTime(entry map[string]any) (int64, bool) {
	for _, key := range []string{"updated_at", "modtime"} {
		raw, ok := entry[key]
		if !ok {
			continue
		}
		text, ok := raw.(string)
		if !ok {
			return 0, false
		}
		parsed, err := time.Parse(time.RFC3339, text)
		if err != nil {
			return 0, false
		}
		return parsed.Unix(), true
	}
	return 0, false
}

func (c *CliproxyClient) CancelAuth(ctx context.Context, stateRef string) error {
	status, _, err := c.do(ctx, http.MethodDelete, "/v0/management/oauth-session?state="+stateRef, "")
	if err != nil {
		return err
	}
	if status >= 500 {
		return ErrPoolExecutorOffline
	}
	if status >= 400 {
		return ErrPoolExecutorFailed
	}
	return nil
}

func (c *CliproxyClient) DeleteCredential(ctx context.Context, credentialRef string) error {
	if credentialRef == "" {
		return nil
	}
	status, _, err := c.do(ctx, http.MethodDelete, "/v0/management/auth-files?name="+credentialRef, "")
	if err != nil {
		return err
	}
	if status >= 500 {
		return ErrPoolExecutorOffline
	}
	if status >= 400 {
		return fmt.Errorf("%w: delete rejected", ErrPoolExecutorFailed)
	}
	return nil
}

func (c *CliproxyClient) patchDisabled(ctx context.Context, credentialRef string, disabled bool) error {
	if credentialRef == "" {
		return ErrPoolInvalid
	}
	payload, err := common.Marshal(map[string]any{"name": credentialRef, "disabled": disabled})
	if err != nil {
		return ErrPoolExecutorFailed
	}
	status, decoded, err := c.do(ctx, http.MethodPatch, "/v0/management/auth-files/status", string(payload))
	if err != nil {
		return err
	}
	if status >= 500 {
		return ErrPoolExecutorOffline
	}
	if status >= 400 || decoded == nil {
		return ErrPoolExecutorFailed
	}
	if value, _ := mapField[string](decoded, "status"); value != "ok" {
		return ErrPoolExecutorFailed
	}
	return nil
}

func (c *CliproxyClient) PauseCredential(ctx context.Context, credentialRef string) error {
	return c.patchDisabled(ctx, credentialRef, true)
}

func (c *CliproxyClient) ResumeCredential(ctx context.Context, credentialRef string) error {
	return c.patchDisabled(ctx, credentialRef, false)
}

func (c *CliproxyClient) CredentialStatus(ctx context.Context, credentialRef string) (ExecutorCredentialStatus, error) {
	entries, err := c.ListCredentials(ctx)
	if err != nil {
		return ExecutorCredentialStatus{}, err
	}
	for _, entry := range entries {
		if entry.Name != credentialRef {
			continue
		}
		if entry.Disabled {
			return ExecutorCredentialStatus{State: CredentialStateDisabled}, nil
		}
		// Unavailable/status text is folded into fixed states; anything
		// unrecognized stays active rather than guessing a failure.
		return ExecutorCredentialStatus{State: CredentialStateActive}, nil
	}
	return ExecutorCredentialStatus{}, ErrPoolExecutorFailed
}

var _ ExecutorClient = (*CliproxyClient)(nil)
