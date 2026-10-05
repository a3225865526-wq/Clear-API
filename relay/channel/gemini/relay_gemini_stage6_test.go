package gemini

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// The fixtures in testdata/ are desensitized, protocol-accurate samples of the
// native Gemini generateContent / streamGenerateContent surface. They carry no
// credentials.

func loadGeminiFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)
	return raw
}

func loadGeminiSSEPayloads(t *testing.T, name string) []string {
	t.Helper()
	var payloads []string
	for _, line := range strings.Split(string(loadGeminiFixture(t, name)), "\n") {
		line = strings.TrimSpace(line)
		if payload, ok := strings.CutPrefix(line, "data: "); ok && payload != "" {
			payloads = append(payloads, payload)
		}
	}
	return payloads
}

func enableGeminiStreamingTimeout(t *testing.T) {
	t.Helper()
	old := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() { constant.StreamingTimeout = old })
}

func newGeminiContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1beta/models/gemini-2.5-flash:streamGenerateContent", nil)
	return c, recorder
}

func newGeminiUpstreamResponse(t *testing.T, body string) *http.Response {
	t.Helper()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func splitGeminiSSEPayloads(t *testing.T, body string) []string {
	t.Helper()
	var payloads []string
	for _, chunk := range strings.Split(body, "\n\n") {
		chunk = strings.TrimSpace(chunk)
		if payload, ok := strings.CutPrefix(chunk, "data: "); ok && payload != "" {
			payloads = append(payloads, payload)
		}
	}
	return payloads
}

// A. Request fidelity: a native Gemini request survives the adaptor with all
// protocol fields intact — multi-part content (text/inlineData/fileData),
// function call/response parts, tools, toolConfig, safetySettings,
// generationConfig (including thinkingConfig and structured output) and
// cachedContent. Nothing is dropped or rewritten.
func TestGeminiNativeRequestFieldFidelity(t *testing.T) {
	raw := loadGeminiFixture(t, "gemini_native_request.json")
	request := &dto.GeminiChatRequest{}
	require.NoError(t, common.Unmarshal(raw, request))

	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-2.5-flash",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "gemini-2.5-flash"},
	}
	out, err := (&Adaptor{}).ConvertGeminiRequest(nil, info, request)
	require.NoError(t, err)
	converted, ok := out.(*dto.GeminiChatRequest)
	require.True(t, ok)
	require.Same(t, request, converted, "native requests must not be rebuilt")

	// systemInstruction preserved.
	require.NotNil(t, converted.SystemInstructions)
	require.Equal(t, "You are a helpful weather assistant.", converted.SystemInstructions.Parts[0].Text)

	// Multi-part content preserved, including media and tool parts.
	require.Len(t, converted.Contents, 3)
	first := converted.Contents[0].Parts
	require.Equal(t, "What is in this picture and what tool would you use to check the weather?", first[0].Text)
	require.NotNil(t, first[1].InlineData)
	require.Equal(t, "image/png", first[1].InlineData.MimeType)
	require.Equal(t, "aW1hZ2UtYnl0ZXMtc3ludGhldGlj", first[1].InlineData.Data)
	require.NotNil(t, first[2].FileData)
	require.Equal(t, "video/webm", first[2].FileData.MimeType, "YouTube fileData keeps its mime type")
	require.Equal(t, "model", converted.Contents[1].Role)
	require.NotNil(t, converted.Contents[1].Parts[1].FunctionCall)
	require.Equal(t, "get_weather", converted.Contents[1].Parts[1].FunctionCall.FunctionName)
	require.NotNil(t, converted.Contents[2].Parts[0].FunctionResponse)
	require.Equal(t, "get_weather", converted.Contents[2].Parts[0].FunctionResponse.Name)

	// tools/toolConfig preserved (raw passthrough).
	require.NotNil(t, converted.Tools)
	require.Contains(t, string(converted.Tools), `"get_weather"`)
	require.NotNil(t, converted.ToolConfig.FunctionCallingConfig)
	require.Equal(t, "AUTO", string(converted.ToolConfig.FunctionCallingConfig.Mode))

	// safetySettings preserved.
	require.Len(t, converted.SafetySettings, 2)
	require.Equal(t, "OFF", converted.SafetySettings[1].Threshold)

	// generationConfig preserved, including thinking and structured output.
	config := converted.GenerationConfig
	require.Equal(t, 0.7, *config.Temperature)
	require.Equal(t, uint(1024), *config.MaxOutputTokens)
	require.Equal(t, []string{"END"}, config.StopSequences)
	require.Equal(t, "application/json", config.ResponseMimeType)
	require.NotNil(t, config.ResponseSchema)
	require.NotNil(t, config.ThinkingConfig)
	require.NotNil(t, config.ThinkingConfig.IncludeThoughts)
	require.True(t, *config.ThinkingConfig.IncludeThoughts)
	require.NotNil(t, config.ThinkingConfig.ThinkingBudget)
	require.Equal(t, 2048, *config.ThinkingConfig.ThinkingBudget)

	// cachedContent passthrough.
	require.Equal(t, "cachedContents/synthetic-stage6", converted.CachedContent)
}

// B. Native streaming passthrough: every upstream SSE data line reaches the
// client byte-for-byte, in order, and the returned usage is the accumulated
// authoritative usageMetadata.
func TestGeminiNativeStreamPassthroughEventSequence(t *testing.T) {
	enableGeminiStreamingTimeout(t)
	payloads := loadGeminiSSEPayloads(t, "gemini_stream_events.sse")
	require.Len(t, payloads, 3)

	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatGemini,
		OriginModelName: "gemini-2.5-flash",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "gemini-2.5-flash"},
	}
	c, recorder := newGeminiContext(t)

	usage, apiErr := GeminiTextGenerationStreamHandler(c, info, newGeminiUpstreamResponse(t, string(loadGeminiFixture(t, "gemini_stream_events.sse"))))
	require.Nil(t, apiErr)
	require.NotNil(t, usage)

	emitted := splitGeminiSSEPayloads(t, recorder.Body.String())
	require.Len(t, emitted, len(payloads), "each upstream SSE line must be forwarded exactly once")
	for i := range payloads {
		require.Equal(t, payloads[i], emitted[i], "stream line %d must pass through unchanged", i)
	}

	// Authoritative usage: prompt from usageMetadata, completion includes
	// thoughts, cache tokens ride as a detail rather than extra output.
	require.Equal(t, 120, usage.PromptTokens)
	require.Equal(t, 34, usage.CompletionTokens, "candidates 28 + thoughts 6")
	require.Equal(t, 154, usage.TotalTokens)
	require.Equal(t, 64, usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, 6, usage.CompletionTokenDetails.ReasoningTokens)

	require.Equal(t, "completed", info.StreamStatus.ResponseOutcome(),
		"finishReason STOP must mark the stream completed")
}

// C. Interrupted stream (EOF before any finishReason): partial usageMetadata
// still settles the prompt authoritatively, the missing completion is filled
// from received text, and the result is flagged estimated — never an exact 0.
func TestGeminiStreamInterruptedUsesPartialMetadataThenEstimates(t *testing.T) {
	enableGeminiStreamingTimeout(t)
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatGemini,
		OriginModelName: "gemini-2.5-flash",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "gemini-2.5-flash"},
	}
	info.SetEstimatePromptTokens(77)
	c, _ := newGeminiContext(t)

	usage, apiErr := GeminiTextGenerationStreamHandler(c, info, newGeminiUpstreamResponse(t, string(loadGeminiFixture(t, "gemini_stream_interrupted.sse"))))
	require.Nil(t, apiErr)
	require.NotNil(t, usage)

	require.Equal(t, 120, usage.PromptTokens, "observed promptTokenCount stays authoritative")
	require.Positive(t, usage.CompletionTokens, "received text fills the missing completion, not a zero")
	require.NotNil(t, usage.BillingUsage)
	require.True(t, usage.BillingUsage.Estimated, "the estimated completion must be flagged")

	require.NotEqual(t, "completed", info.StreamStatus.ResponseOutcome(),
		"a stream without finishReason must not be reported as completed")
}

// D. A malformed upstream stream line fails closed: the handler errors and
// nothing from that point reaches the client.
func TestGeminiStreamMalformedFailsClosed(t *testing.T) {
	enableGeminiStreamingTimeout(t)
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatGemini,
		OriginModelName: "gemini-2.5-flash",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "gemini-2.5-flash"},
	}
	c, recorder := newGeminiContext(t)

	usage, apiErr := GeminiTextGenerationStreamHandler(c, info, newGeminiUpstreamResponse(t, "data: {not-json\n\n"))
	require.NotNil(t, apiErr, "malformed stream data must surface an error")
	require.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
	require.Zero(t, usage.PromptTokens)
	require.Zero(t, usage.CompletionTokens, "malformed stream data must not produce billable usage")
	require.Empty(t, recorder.Body.String(), "no frame from a malformed line may reach the client")
}

// E. Upstream HTTP errors keep their status code and message through
// RelayErrorHandler. (Known limitation, recorded not fixed: Retry-After is
// not parsed on this path.)
func TestGeminiUpstreamErrorStatusPreserved(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"error":{"code":429,"message":"Resource exhausted","status":"RESOURCE_EXHAUSTED"}}`
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	err := service.RelayErrorHandler(context.Background(), resp, false)
	require.NotNil(t, err)
	require.Equal(t, http.StatusTooManyRequests, err.StatusCode, "the upstream status must survive error conversion")
	require.Contains(t, err.Error(), "Resource exhausted")
}
