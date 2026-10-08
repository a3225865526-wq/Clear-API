package stagingdiag

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relayhelper "github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStage74DiagnosticAccess(t *testing.T) {
	now := time.Now()
	key := strings.Repeat("a", 64)
	reads := 0
	read := func() (any, error) { reads++; return map[string]any{"model": "gpt-5.6-luna"}, nil }
	handler := Handler(key, now.Add(time.Minute), read, func() time.Time { return now })
	for _, tc := range []struct {
		name, method, path, peer, auth, origin string
		code                                   int
	}{
		{"authorized", "GET", Path, "127.0.0.1:1000", "Bearer " + key, "", 200},
		{"missing key", "GET", Path, "127.0.0.1:1000", "", "", 401},
		{"wrong key", "GET", Path, "127.0.0.1:1000", "Bearer " + strings.Repeat("b", 64), "", 401},
		{"external even with key", "GET", Path, "172.18.0.2:1000", "Bearer " + key, "", 403},
		{"browser origin", "GET", Path, "127.0.0.1:1000", "Bearer " + key, "https://example.invalid", 403},
		{"mutation", "POST", Path, "127.0.0.1:1000", "Bearer " + key, "", 404},
		{"query target", "GET", Path + "?model=other", "127.0.0.1:1000", "Bearer " + key, "", 404},
		{"other path", "GET", "/api/option/", "127.0.0.1:1000", "Bearer " + key, "", 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.RemoteAddr = tc.peer
			req.Header.Set("Authorization", tc.auth)
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("X-Forwarded-For", "127.0.0.1")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			assert.Equal(t, tc.code, response.Code)
			assert.NotContains(t, response.Body.String(), key)
			assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		})
	}
	assert.Equal(t, 1, reads)
	now = now.Add(2 * time.Minute)
	req := httptest.NewRequest("GET", Path, nil)
	req.RemoteAddr = "127.0.0.1:1000"
	req.Header.Set("Authorization", "Bearer "+key)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	assert.Equal(t, http.StatusGone, response.Code)
	assert.Equal(t, 1, reads)
}

func TestStage74AuthorizationFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operator.json")
	// A missing grant never starts a listener in either build variant.
	require.NoError(t, Start(func() (any, error) { t.Fatal("startup must not read without authorization"); return nil, nil }))
	require.NoError(t, os.WriteFile(path, []byte(`{"purpose":"production","key":"short","expires":1}`), 0600))
	_, err := loadGrant(path)
	require.Error(t, err)
	require.NoError(t, os.Chmod(path, 0644))
	_, err = loadGrant(path)
	require.Error(t, err)
}

func TestStage74StartupBuildGateAndGrant(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("staging authorization owner test requires isolated Linux root")
	}
	path := filepath.Join(t.TempDir(), "operator.json")
	original := grantPath
	if err := os.Chmod(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	grantPath = path
	t.Cleanup(func() { grantPath = original })
	key := strings.Repeat("c", 64)
	for _, tc := range []struct {
		purpose, key string
		expires      int64
		valid        bool
	}{
		{"local-staging", key, time.Now().Add(time.Minute).Unix(), true},
		{"production", key, time.Now().Add(time.Minute).Unix(), false},
		{"local-staging", "short", time.Now().Add(time.Minute).Unix(), false},
		{"local-staging", key, time.Now().Add(-time.Minute).Unix(), false},
		{"local-staging", key, time.Now().Add(time.Hour).Unix(), false},
	} {
		body := fmt.Sprintf(`{"purpose":%q,"key":%q,"expires":%d}`, tc.purpose, tc.key, tc.expires)
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := loadGrant(path)
		if (err == nil) != tc.valid {
			t.Fatal("authorization lifetime/purpose/key gate mismatch")
		}
	}
	body := fmt.Sprintf(`{"purpose":"local-staging","key":%q,"expires":%d}`, key, time.Now().Add(time.Minute).Unix())
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	if Enabled {
		if err := os.Chmod(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := Start(func() (any, error) { t.Fatal("unsafe directory invoked getter"); return nil, nil }); err == nil {
			t.Fatal("unsafe authorization directory accepted")
		}
		if err := os.Chmod(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(path, 65534, 65534); err != nil {
			t.Fatal(err)
		}
		if err := Start(func() (any, error) { t.Fatal("wrong owner invoked getter"); return nil, nil }); err == nil {
			t.Fatal("unauthorized file owner accepted")
		}
		if err := os.Chown(path, 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := Start(func() (any, error) { return map[string]any{"diagnostic": "safe"}, nil }); err != nil {
		t.Fatal(err)
	}
	if !Enabled {
		conn, err := net.DialTimeout("tcp4", "127.0.0.1:19740", time.Second)
		if err == nil {
			_ = conn.Close()
			t.Fatal("ordinary build started diagnostic listener")
		}
		return
	}
	client := &http.Client{Timeout: time.Second}
	req, err := http.NewRequest("GET", "http://127.0.0.1:19740"+Path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatal("authorized tagged startup read failed")
	}
}

func TestStage74PricingReadsActualGetters(t *testing.T) {
	for _, pair := range []struct {
		get func() string
		set func(string) error
	}{
		{ratio_setting.ModelRatio2JSONString, ratio_setting.UpdateModelRatioByJSONString},
		{ratio_setting.ModelPrice2JSONString, ratio_setting.UpdateModelPriceByJSONString},
		{ratio_setting.CompletionRatio2JSONString, ratio_setting.UpdateCompletionRatioByJSONString},
		{ratio_setting.CacheRatio2JSONString, ratio_setting.UpdateCacheRatioByJSONString},
		{ratio_setting.GroupRatio2JSONString, ratio_setting.UpdateGroupRatioByJSONString},
		{ratio_setting.GroupGroupRatio2JSONString, ratio_setting.UpdateGroupGroupRatioByJSONString},
	} {
		previous := pair.get()
		t.Cleanup(func() { require.NoError(t, pair.set(previous)) })
	}
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-5.6-luna":0.1}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"gpt-5.6-luna":6}`))
	require.NoError(t, ratio_setting.UpdateCacheRatioByJSONString(`{"gpt-5.6-luna":0.1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{}`))
	value, err := relayhelper.RuntimePricingDiagnostic()
	require.NoError(t, err)
	data, err := common.Marshal(value)
	require.NoError(t, err)
	var projected map[string]any
	require.NoError(t, common.Unmarshal(data, &projected))
	assert.Equal(t, 0.1, projected["model_ratio"])
	assert.Equal(t, float64(6), projected["completion_ratio"])
	assert.Equal(t, 0.1, projected["cache_ratio"])
	assert.Equal(t, false, projected["model_price_present"])
	assert.Nil(t, projected["model_price"])
	assert.Equal(t, float64(1), projected["group_ratio"])
	assert.Equal(t, "gpt-5.6-luna", projected["canonical_pricing_key"])
	// Changing the actual map must be visible immediately without a pricing cache reload.
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-5.6-luna":0.2}`))
	value, err = relayhelper.RuntimePricingDiagnostic()
	require.NoError(t, err)
	data, err = common.Marshal(value)
	require.NoError(t, err)
	require.NoError(t, common.Unmarshal(data, &projected))
	assert.Equal(t, 0.2, projected["model_ratio"])
	assert.NotContains(t, string(data), "access_token")
	assert.NotContains(t, string(data), "billing_expr\":")
}
