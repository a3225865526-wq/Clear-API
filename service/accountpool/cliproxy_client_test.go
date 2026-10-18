package accountpool

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The httptest servers below reproduce the wire shapes of the pinned
// CLIProxyAPI v8.0.4 management API (see executor.go for the source map), so
// the REAL adapter is exercised rather than the business-layer stand-in.

func newTestCliproxyClient(t *testing.T, handler http.HandlerFunc) (*CliproxyClient, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := &CliproxyClient{
		baseURL:      server.URL,
		key:          "synthetic-management-key",
		http:         server.Client(),
		PollInterval: time.Millisecond,
	}
	return client, server
}

func TestCliproxyStartAuthBrowserShape(t *testing.T) {
	var seenKey, seenPath string
	client, _ := newTestCliproxyClient(t, func(w http.ResponseWriter, r *http.Request) {
		seenKey, seenPath = r.Header.Get("X-Management-Key"), r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","url":"https://claude.example/oauth/authorize?client_id=x","state":"st-1"}`))
	})
	result, err := client.StartAuth(context.Background(), StartAuthRequest{Platform: "claude"})
	require.NoError(t, err)
	assert.Equal(t, AuthMethodOAuthBrowser, result.Method)
	assert.Equal(t, "https://claude.example/oauth/authorize?client_id=x", result.AuthURL)
	assert.Equal(t, "st-1", result.StateRef)
	assert.Empty(t, result.UserCode)
	assert.Equal(t, "synthetic-management-key", seenKey)
	assert.Equal(t, "/v0/management/anthropic-auth-url", seenPath)
}

func TestCliproxyStartAuthDeviceShape(t *testing.T) {
	client, _ := newTestCliproxyClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v0/management/kimi-auth-url", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","url":"https://auth.kimi.com/device","state":"kmi-st","flow":"device","user_code":"ABCD-EFGH","expires_in":300}`))
	})
	result, err := client.StartAuth(context.Background(), StartAuthRequest{Platform: "kimi_coding"})
	require.NoError(t, err)
	assert.Equal(t, AuthMethodOAuthDevice, result.Method)
	assert.Equal(t, "https://auth.kimi.com/device", result.AuthURL)
	assert.Equal(t, "ABCD-EFGH", result.UserCode)
	assert.WithinDuration(t, time.Now().Add(300*time.Second), result.ExpiresAt, 5*time.Second)
}

func TestCliproxyStartAuthFailsClosedOnBadShapes(t *testing.T) {
	for name, body := range map[string]string{
		"error-field":      `{"error":"boom"}`,
		"not-ok":           `{"status":"error"}`,
		"missing-state":    `{"status":"ok","url":"https://x"}`,
		"missing-url":      `{"status":"ok","state":"s"}`,
		"unexpected-shape": `{"foo":1}`,
		"garbage":          `not-json`,
	} {
		t.Run(name, func(t *testing.T) {
			client, _ := newTestCliproxyClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(body))
			})
			_, err := client.StartAuth(context.Background(), StartAuthRequest{Platform: "claude"})
			assert.ErrorIs(t, err, ErrPoolExecutorFailed)
		})
	}
	// Unsupported platform never reaches the network.
	client, _ := newTestCliproxyClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("network call must not happen for unsupported platforms")
	})
	_, err := client.StartAuth(context.Background(), StartAuthRequest{Platform: "qwen_web"})
	assert.ErrorIs(t, err, ErrPoolUnsupported)
}

func TestCliproxyAuthStatusStates(t *testing.T) {
	client, _ := newTestCliproxyClient(t, func(w http.ResponseWriter, r *http.Request) {
		state := r.URL.Query().Get("state")
		var body string
		switch state {
		case "done":
			body = `{"status":"ok"}`
		case "waiting":
			body = `{"status":"wait"}`
		case "gone":
			body = `{"status":"error","error":"unknown or expired state"}`
		case "broken":
			body = `{"status":"error","error":"exchange failed upstream"}`
		default:
			body = `{}`
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	})
	// The fixed protocol only knows ok/wait/error on the wire; cancelled
	// sessions are indistinguishable from expired ones.
	for state, want := range map[string]AuthState{
		"done": AuthStateCompleted, "waiting": AuthStateWaiting,
		"gone": AuthStateExpired, "broken": AuthStateFailed,
	} {
		got, err := client.AuthStatus(context.Background(), state)
		require.NoError(t, err, state)
		assert.Equal(t, want, got, state)
	}
	_, err := client.AuthStatus(context.Background(), "missing")
	assert.ErrorIs(t, err, ErrPoolExecutorFailed)
}

func TestCliproxyCompleteBrowserFlowSubmitsCallbackThenPolls(t *testing.T) {
	var callbacks []map[string]any
	statusCalls := 0
	client, _ := newTestCliproxyClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/oauth-callback":
			raw, _ := io.ReadAll(r.Body)
			var decoded map[string]any
			require.NoError(t, json.Unmarshal(raw, &decoded))
			callbacks = append(callbacks, decoded)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case r.URL.Path == "/v0/management/get-auth-status":
			statusCalls++
			// The callback being received does NOT mean completion: the
			// session must be polled to the terminal state.
			body := `{"status":"wait"}`
			if statusCalls >= 3 {
				body = `{"status":"ok"}`
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(body))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	err := client.CompleteAuth(context.Background(), CompleteAuthRequest{
		Platform: "claude", Method: AuthMethodOAuthBrowser, StateRef: "st-9",
		CallbackURL: "https://platform.claude.com/oauth/code/callback?code=c1&state=st-9",
		Code:        "c1", State: "st-9",
	})
	require.NoError(t, err)
	require.Len(t, callbacks, 1)
	assert.Equal(t, "anthropic", callbacks[0]["provider"])
	assert.Equal(t, "c1", callbacks[0]["code"])
	assert.Equal(t, "st-9", callbacks[0]["state"])
	assert.NotEmpty(t, callbacks[0]["redirect_url"])
	assert.GreaterOrEqual(t, statusCalls, 3)
}

func TestCliproxyCompleteDeviceFlowNeverSubmitsCallback(t *testing.T) {
	statusCalls := 0
	client, _ := newTestCliproxyClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/oauth-callback" {
			t.Fatal("device flows must not submit a browser callback")
		}
		statusCalls++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	err := client.CompleteAuth(context.Background(), CompleteAuthRequest{
		Platform: "kimi_coding", Method: AuthMethodOAuthDevice, StateRef: "kmi-1",
	})
	require.NoError(t, err)
	assert.Positive(t, statusCalls)
}

func TestCliproxyCompleteTerminalStatesAndTimeout(t *testing.T) {
	client, _ := newTestCliproxyClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/oauth-callback" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		// Session expired before the user finished: "unknown or expired".
		if strings.Contains(r.URL.Query().Get("state"), "expired") {
			_, _ = w.Write([]byte(`{"status":"error","error":"unknown or expired state"}`))
			return
		}
		if strings.Contains(r.URL.Query().Get("state"), "broken") {
			_, _ = w.Write([]byte(`{"status":"error","error":"token exchange rejected"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"wait"}`))
	})
	expired := client.CompleteAuth(context.Background(), CompleteAuthRequest{
		Platform: "claude", Method: AuthMethodOAuthBrowser, StateRef: "expired-1",
		CallbackURL: "https://x/cb?code=a&state=expired-1", Code: "a", State: "expired-1",
	})
	assert.ErrorIs(t, expired, ErrPoolTaskExpired)
	broken := client.CompleteAuth(context.Background(), CompleteAuthRequest{
		Platform: "claude", Method: AuthMethodOAuthBrowser, StateRef: "broken-1",
		CallbackURL: "https://x/cb?code=a&state=broken-1", Code: "a", State: "broken-1",
	})
	assert.ErrorIs(t, broken, ErrPoolExecutorFailed)
	// A session that stays "wait" past the deadline is a timeout, never a
	// fake success.
	stuck := client.CompleteAuth(context.Background(), CompleteAuthRequest{
		Platform: "claude", Method: AuthMethodOAuthBrowser, StateRef: "stuck-1",
		CallbackURL: "https://x/cb?code=a&state=stuck-1", Code: "a", State: "stuck-1",
		Deadline: time.Now().Add(20 * time.Millisecond),
	})
	assert.ErrorIs(t, stuck, ErrPoolAuthTimeout)
}

func TestCliproxyImportCredentialMapsDuplicateToConflict(t *testing.T) {
	client, _ := newTestCliproxyClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v0/management/pool/import", r.URL.Path)
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"credential exists"}`))
	})
	_, err := client.ImportCredential(context.Background(), CredentialImportRequest{
		FileName: "codex-account.json",
		Raw:      []byte(`{"type":"codex","access_token":"x","refresh_token":"y","account_id":"a","email":"a@example.com"}`),
	})
	assert.ErrorIs(t, err, ErrPoolConflict)
}

func TestCliproxyListCredentialsParsesEntries(t *testing.T) {
	// The pinned build serializes auth.UpdatedAt (time.Time) as an RFC3339
	// string into both "modtime" and "updated_at", and omits both keys when
	// the time is zero (auth_files.go:718-721).
	client, _ := newTestCliproxyClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"files":[
			{"name":"zeta-file.json","email":"z@e.test","disabled":false,"updated_at":"2026-10-04T01:02:03Z"},
			{"name":"alpha-file.json","email":"a@e.test","disabled":true,"modtime":"2026-10-04T04:05:06Z"},
			{"no-name":true},
			{"name":"bare.json","updated_at":"2026-10-04T07:08:09Z"}
		]}`))
	})
	entries, err := client.ListCredentials(context.Background())
	require.NoError(t, err)
	require.Len(t, entries, 3)
	// Sorted by name for deterministic diffing.
	assert.Equal(t, "alpha-file.json", entries[0].Name)
	assert.Equal(t, "a@e.test", entries[0].Email)
	assert.True(t, entries[0].Disabled)
	assert.EqualValues(t, time.Date(2026, 10, 4, 4, 5, 6, 0, time.UTC).Unix(), entries[0].UpdatedAt)
	assert.Equal(t, "bare.json", entries[1].Name)
	assert.Equal(t, "zeta-file.json", entries[2].Name)
	assert.EqualValues(t, time.Date(2026, 10, 4, 1, 2, 3, 0, time.UTC).Unix(), entries[2].UpdatedAt)
}

func TestCliproxyListCredentialsFailsClosedOnBadTimestamps(t *testing.T) {
	// A missing or malformed timestamp is a wire-contract violation: the
	// baseline diff depends on it, so the listing must fail instead of
	// silently defaulting the time to zero.
	cases := map[string]string{
		"missing":    `{"files":[{"name":"a.json"}]}`,
		"numeric":    `{"files":[{"name":"a.json","updated_at":111}]}`,
		"malformed":  `{"files":[{"name":"a.json","updated_at":"04/10/2026"}]}`,
		"null":       `{"files":[{"name":"a.json","updated_at":null}]}`,
		"second-bad": `{"files":[{"name":"a.json","updated_at":"2026-10-04T01:02:03Z"},{"name":"b.json","updated_at":"nope"}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			client, _ := newTestCliproxyClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(body))
			})
			_, err := client.ListCredentials(context.Background())
			assert.ErrorIs(t, err, ErrPoolExecutorFailed, name)
		})
	}
}

func TestCliproxyPauseResumeAndDeleteWireShapes(t *testing.T) {
	var patchBodies []map[string]any
	var deletePaths []string
	client, _ := newTestCliproxyClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPatch && r.URL.Path == "/v0/management/auth-files/status":
			raw, _ := io.ReadAll(r.Body)
			var decoded map[string]any
			require.NoError(t, json.Unmarshal(raw, &decoded))
			patchBodies = append(patchBodies, decoded)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok","disabled":true}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v0/management/auth-files":
			deletePaths = append(deletePaths, r.URL.Query().Get("name"))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	require.NoError(t, client.PauseCredential(context.Background(), "auth.json"))
	require.NoError(t, client.ResumeCredential(context.Background(), "auth.json"))
	require.NoError(t, client.DeleteCredential(context.Background(), "auth.json"))
	require.Len(t, patchBodies, 2)
	assert.Equal(t, "auth.json", patchBodies[0]["name"])
	assert.Equal(t, true, patchBodies[0]["disabled"])
	assert.Equal(t, false, patchBodies[1]["disabled"])
	assert.Equal(t, []string{"auth.json"}, deletePaths)

	// Rejections fail closed.
	rejecting, _ := newTestCliproxyClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"no"}`))
	})
	assert.ErrorIs(t, rejecting.PauseCredential(context.Background(), "auth.json"), ErrPoolExecutorFailed)
	assert.ErrorIs(t, rejecting.ResumeCredential(context.Background(), "auth.json"), ErrPoolExecutorFailed)
	assert.ErrorIs(t, rejecting.DeleteCredential(context.Background(), "auth.json"), ErrPoolExecutorFailed)
}

func TestCliproxyCredentialStatusStates(t *testing.T) {
	client, _ := newTestCliproxyClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"files":[
			{"name":"parked.json","disabled":true,"updated_at":"2026-10-04T01:02:03Z"},
			{"name":"live.json","disabled":false,"status":"working","updated_at":"2026-10-04T01:02:03Z"}
		]}`))
	})
	parked, err := client.CredentialStatus(context.Background(), "parked.json")
	require.NoError(t, err)
	assert.Equal(t, CredentialStateDisabled, parked.State)
	live, err := client.CredentialStatus(context.Background(), "live.json")
	require.NoError(t, err)
	assert.Equal(t, CredentialStateActive, live.State)
	_, err = client.CredentialStatus(context.Background(), "absent.json")
	assert.ErrorIs(t, err, ErrPoolExecutorFailed)
}

func TestCliproxyClientWithoutEndpointIsOffline(t *testing.T) {
	client := &CliproxyClient{http: &http.Client{}, PollInterval: time.Millisecond}
	ctx := context.Background()
	assert.ErrorIs(t, client.Health(ctx), ErrPoolExecutorOffline)
	_, err := client.StartAuth(ctx, StartAuthRequest{Platform: "claude"})
	assert.ErrorIs(t, err, ErrPoolExecutorOffline)
	assert.ErrorIs(t, client.CompleteAuth(ctx, CompleteAuthRequest{Platform: "claude", Method: AuthMethodOAuthBrowser}), ErrPoolExecutorOffline)
	_, err = client.ListCredentials(ctx)
	assert.ErrorIs(t, err, ErrPoolExecutorOffline)
	assert.ErrorIs(t, client.PauseCredential(ctx, "a.json"), ErrPoolExecutorOffline)
	assert.ErrorIs(t, client.ResumeCredential(ctx, "a.json"), ErrPoolExecutorOffline)
	assert.ErrorIs(t, client.DeleteCredential(ctx, "a.json"), ErrPoolExecutorOffline)
}

func TestCliproxyHealthFailsClosed(t *testing.T) {
	healthy, _ := newTestCliproxyClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"files":[]}`))
	})
	assert.NoError(t, healthy.Health(context.Background()))
	broken, _ := newTestCliproxyClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	assert.ErrorIs(t, broken.Health(context.Background()), ErrPoolExecutorOffline)
	unauthorized, _ := newTestCliproxyClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	assert.ErrorIs(t, unauthorized.Health(context.Background()), ErrPoolExecutorFailed)
}
