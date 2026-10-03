package router

import (
	"bytes"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIRegisteredRequestBodyLimits(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	oldRate, oldAnonymous := common.GlobalApiRateLimitEnable, constant.AnonymousRequestBodyLimitKB
	common.GlobalApiRateLimitEnable, constant.AnonymousRequestBodyLimitKB = false, 512
	t.Cleanup(func() {
		gin.SetMode(oldMode)
		common.GlobalApiRateLimitEnable, constant.AnonymousRequestBodyLimitKB = oldRate, oldAnonymous
	})
	r := gin.New()
	SetApiRouter(r)
	// Reproduce production registration order: later engine middleware cannot
	// protect management routes that have already been registered.
	SetRelayRouter(r)
	for _, tc := range []struct {
		method, path string
		size, want   int
	}{
		{"POST", "/api/token/", (2 << 20) + 1, 413},
		{"PUT", "/api/option/", (2 << 20) + 1, 413},
		{"POST", "/api/owner/finance/supplier-bills", (2 << 20) + 1, 413},
		{"PUT", "/api/guide/article", (2 << 20) + 1, 413},
		{"POST", "/api/guide/images", (6 << 20) + 1, 413},
		{"POST", "/api/guide/images", 3 << 20, 401},
		{"POST", "/api/token/", 2 << 20, 401},
		{"POST", "/v1/chat/completions", 3 << 20, 401},
		{"POST", "/api/stripe/webhook", (512 << 10) + 1, 413},
		{"POST", "/api/creem/webhook", (512 << 10) + 1, 413},
		{"POST", "/api/waffo/webhook", (512 << 10) + 1, 413},
		{"POST", "/api/waffo-pancake/webhook/test", (512 << 10) + 1, 413},
	} {
		for _, unknownLength := range []bool{false, true} {
			t.Run(tc.method+tc.path+map[bool]string{false: "/known", true: "/chunked"}[unknownLength], func(t *testing.T) {
				req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(strings.Repeat("x", tc.size)))
				if unknownLength {
					req.ContentLength = -1
					req.TransferEncoding = []string{"chunked"}
				}
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				assert.Equal(t, tc.want, w.Code)
			})
		}
	}
}

type bodyLimitTestReader struct {
	reader io.Reader
	read   int
	closed bool
	err    error
}

func (r *bodyLimitTestReader) Read(p []byte) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	n, err := r.reader.Read(p)
	r.read += n
	return n, err
}

func (r *bodyLimitTestReader) Close() error {
	r.closed = true
	return nil
}

func TestAPIRequestBodyReading(t *testing.T) {
	for _, tc := range []struct {
		name   string
		size   int
		length int64
		err    error
		want   int
		read   int
	}{
		{"known overflow rejected before reading", (2 << 20) + 1, (2 << 20) + 1, nil, 413, 0},
		{"unknown overflow bounded", (2 << 20) + 100, -1, nil, 413, (2 << 20) + 1},
		{"misleading length still bounded", (2 << 20) + 100, 1, nil, 413, (2 << 20) + 1},
		{"read failure stops handler", 0, -1, errors.New("synthetic read failure"), 400, 0},
		{"exact boundary allowed", 2 << 20, -1, nil, 204, 2 << 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.Use(middleware.APIRequestBodyLimit())
			called := false
			r.POST("/api/probe", func(c *gin.Context) {
				called = true
				body, err := io.ReadAll(c.Request.Body)
				require.NoError(t, err)
				assert.Len(t, body, tc.size)
				c.Status(204)
			})
			body := &bodyLimitTestReader{reader: strings.NewReader(strings.Repeat("x", tc.size)), err: tc.err}
			req := httptest.NewRequest("POST", "/api/probe", nil)
			req.Body, req.ContentLength = body, tc.length
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, tc.want, w.Code)
			assert.Equal(t, tc.want == 204, called)
			assert.Equal(t, tc.read, body.read)
			assert.True(t, body.closed)
		})
	}
}

func TestAPIRequestBodyPreservesCallbackBytes(t *testing.T) {
	oldAnonymous := constant.AnonymousRequestBodyLimitKB
	constant.AnonymousRequestBodyLimitKB = 512
	t.Cleanup(func() { constant.AnonymousRequestBodyLimitKB = oldAnonymous })
	r := gin.New()
	r.Use(middleware.APIRequestBodyLimit(), middleware.AnonymousRequestBodyLimit())
	want := []byte(" {\n \"event\":\"order.completed\", \"amount\":\"10.00\" }\n")
	r.POST("/api/payment/webhook", func(c *gin.Context) {
		got, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		assert.Equal(t, want, got)
		assert.Equal(t, int64(len(want)), c.Request.ContentLength)
		assert.Equal(t, "synthetic-signature", c.GetHeader("X-Waffo-Signature"))
		c.Status(204)
	})
	req := httptest.NewRequest("POST", "/api/payment/webhook", bytes.NewReader(want))
	req.ContentLength = -1
	req.Header.Set("X-Waffo-Signature", "synthetic-signature")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, 204, w.Code)
}
