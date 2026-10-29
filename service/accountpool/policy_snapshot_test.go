package accountpool

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutomaticPoolGatewayProof(t *testing.T) {
	key := strings.Repeat("synthetic-proof-key-", 2)
	t.Setenv("CLEARAPI_PG03_POLICY_KEY", strings.Repeat("p", 32))
	t.Setenv("CLEARAPI_PG02_GATEWAY_PROOF_KEY", key)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(pg03SnapshotContext, &pg03RequestSnapshot{active: true, policy: pg03PolicySnapshot{PoolID: "synthetic-pool", ModelName: "new-model", RequestID: "gateway-request-id", IssuedAt: 123}})
	body := []byte(`{"model":"new-model","stream":true}`)
	req, err := http.NewRequest(http.MethodPost, "http://executor.invalid/v1/responses", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set(PG03PolicyHeader, "client-supplied")
	req.Header.Set("X-ClearAPI-PG02-Gateway-Proof", "client-supplied")
	require.NoError(t, ApplyPG03Policy(c, req))
	assert.Empty(t, req.Header.Get(PG03PolicyHeader))
	assert.Equal(t, "gateway-request-id", req.Header.Get("X-ClearAPI-PG02-Request"))
	assert.Equal(t, "gateway-request-id-attempt", req.Header.Get("X-ClearAPI-PG02-Attempt"))
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte("gateway-request-id\ngateway-request-id-attempt\nPOST\n/v1/responses\n"))
	mac.Write(body)
	assert.Equal(t, hex.EncodeToString(mac.Sum(nil)), req.Header.Get("X-ClearAPI-PG02-Gateway-Proof"))
	// The frozen binding cannot be reused with changed outbound bytes.
	changed, err := http.NewRequest(http.MethodPost, "http://executor.invalid/v1/responses", bytes.NewReader([]byte(`{"model":"other-model"}`)))
	require.NoError(t, err)
	require.Error(t, ApplyPG03Policy(c, changed))
}
