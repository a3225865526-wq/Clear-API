package service_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodexMeteringGolden(t *testing.T) {
	data, err := os.ReadFile("testdata/codex-metering.json")
	require.NoError(t, err)
	var cases []struct {
		Name           string
		Usage          json.RawMessage
		Status, Reason string
		Derived        bool
	}
	require.NoError(t, common.Unmarshal(data, &cases))
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			m := dto.ParseCodexMetering(tc.Usage)
			assert.Equal(t, tc.Status, m.Status)
			assert.Equal(t, tc.Reason, m.Reason)
			assert.Equal(t, tc.Derived, m.TotalDerived)
			if m.Status == "exact" {
				assert.Equal(t, *m.Input+*m.Output, *m.Total)
			}
		})
	}
}

const exactCodexFinal = `{"type":"response.completed","response":{"id":"synthetic","model":"synthetic-model","status":"completed","usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120,"input_tokens_details":{"cached_tokens":30},"output_tokens_details":{"reasoning_tokens":7}}}}`

func TestCodexMeteringStreamFinalAndInterruption(t *testing.T) {
	for _, tc := range []struct {
		name   string
		events []string
		exact  bool
	}{
		{"final", []string{exactCodexFinal}, true},
		{"delta-and-duplicate-final", []string{`{"type":"response.output_text.delta","delta":"visible text"}`, exactCodexFinal, exactCodexFinal}, true},
		{"client-disconnect", []string{`{"type":"response.created","response":{"status":"in_progress"}}`}, false},
		{"upstream-eof", nil, false},
		{"context-cancel", []string{`{"type":"response.output_text.delta","delta":"visible text"}`}, false},
		{"security-revoke", []string{`{"type":"error","code":"account_revoked"}`}, false},
		{"final-before-disconnect", []string{exactCodexFinal, `{"type":"error","code":"account_revoked"}`}, true},
		{"malformed", []string{`{"type":"response.completed","response":{"usage":{"input_tokens":-1}}}`}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			info := &relaycommon.RelayInfo{IsStream: true, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeCodex, UpstreamModelName: "synthetic-model"}, StreamStatus: relaycommon.NewStreamStatus()}
			info.SetEstimatePromptTokens(9999)
			a := service.NewResponsesUsageAccumulator(info, c)
			for _, raw := range tc.events {
				var event dto.ResponsesStreamResponse
				require.NoError(t, common.Unmarshal([]byte(raw), &event))
				service.ObserveCodexMetering(c, info, []byte(raw), service.IsResponsesUsageTerminal(event.Type))
				a.Observe(&event)
			}
			u := a.Finish()
			require.NotNil(t, u.CodexMetering)
			if tc.exact {
				assert.Equal(t, "exact", u.CodexMetering.Status)
				assert.Equal(t, 100, u.PromptTokens)
				assert.Equal(t, 20, u.CompletionTokens)
				assert.Equal(t, 120, u.TotalTokens)
				assert.Equal(t, 30, u.PromptTokensDetails.CachedTokens)
				assert.Equal(t, 7, u.CompletionTokenDetails.ReasoningTokens)
			} else {
				assert.Equal(t, "unavailable", u.CodexMetering.Status)
				assert.Nil(t, u.CodexMetering.Total)
				assert.Zero(t, u.TotalTokens)
			}
			assert.Same(t, u, a.Finish())
		})
	}
}

func TestCodexMeteringHTTPHandlers(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		for _, raw := range []string{exactCodexFinal, `{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}}}`, `{"type":"response.completed","response":{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"estimate must not bill"}]}]}}`} {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			info := &relaycommon.RelayInfo{IsStream: stream, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeCodex, UpstreamModelName: "synthetic-model"}, StreamStatus: relaycommon.NewStreamStatus()}
			info.SetEstimatePromptTokens(9999)
			body := raw
			if stream {
				body = "data: " + raw + "\n\n"
			} else {
				var root map[string]json.RawMessage
				require.NoError(t, common.Unmarshal([]byte(raw), &root))
				body = string(root["response"])
			}
			resp := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
			var u *dto.Usage
			if stream {
				result, err := openai.OaiResponsesStreamHandler(c, info, resp)
				require.Nil(t, err)
				u = result
			} else {
				result, err := openai.OaiResponsesHandler(c, info, resp)
				require.Nil(t, err)
				u = result
			}
			require.NotNil(t, u.CodexMetering)
			if strings.Contains(raw, `"usage"`) {
				assert.Equal(t, "exact", u.CodexMetering.Status)
			} else {
				assert.Equal(t, "unavailable", u.CodexMetering.Status)
				assert.Zero(t, u.TotalTokens)
			}
		}
	}
}
