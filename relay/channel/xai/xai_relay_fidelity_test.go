package xai

import (
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
// xAI (Grok) OpenAI-compatible chat surface. They carry no credentials.

func loadXAIFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)
	return raw
}

func newXAIRelayInfo(model string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: model},
	}
}

func newXAIUpstreamResponse(t *testing.T, body string) *http.Response {
	t.Helper()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func newXAIContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	return c, recorder
}

// enableStreamingTimeout mirrors the harness used by the gemini channel tests:
// StreamScannerHandler panics on a zero idle timeout.
func enableStreamingTimeout(t *testing.T) {
	t.Helper()
	old := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() { constant.StreamingTimeout = old })
}

// splitSSEFrames splits a recorder body into `data: ...` payload strings,
// keeping order. Ping comment lines (none by default) are ignored.
func splitSSEFrames(t *testing.T, body string) []string {
	t.Helper()
	var frames []string
	for _, chunk := range strings.Split(body, "\n\n") {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" || !strings.HasPrefix(chunk, "data: ") {
			continue
		}
		frames = append(frames, strings.TrimPrefix(chunk, "data: "))
	}
	return frames
}

// A. Request fidelity: a plain grok chat request passes through the xai
// adaptor unchanged — model, tools, tool_choice and search_parameters intact.
func TestXAIConvertOpenAIRequestPassthrough(t *testing.T) {
	raw := loadXAIFixture(t, "xai_chat_request.json")
	request := &dto.GeneralOpenAIRequest{}
	require.NoError(t, common.Unmarshal(raw, request))

	adaptor := &Adaptor{}
	out, err := adaptor.ConvertOpenAIRequest(nil, newXAIRelayInfo("grok-4"), request)
	require.NoError(t, err)
	converted, ok := out.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.Same(t, request, converted, "non-special models must not be rebuilt")
	require.Equal(t, "grok-4", converted.Model)
	require.Len(t, converted.Tools, 1)
	require.Equal(t, "get_weather", converted.Tools[0].Function.Name)
	require.NotEmpty(t, converted.SearchParameters, "search_parameters must survive as raw JSON")
}

// B. The `-search` suffix is a channel-side alias for xAI live search: the
// suffix is stripped and search_parameters {mode:on} injected.
func TestXAIConvertOpenAIRequestSearchSuffix(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{Model: "grok-4-search"}
	info := newXAIRelayInfo("grok-4-search")
	out, err := (&Adaptor{}).ConvertOpenAIRequest(nil, info, request)
	require.NoError(t, err)
	converted, ok := out.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "grok-4", converted["model"])
	require.Equal(t, map[string]any{"mode": "on"}, converted["search_parameters"])
	require.Equal(t, "grok-4", info.UpstreamModelName)
}

// C. grok-3-mini reasoning suffixes map onto reasoning_effort, and the legacy
// max_tokens field migrates to max_completion_tokens.
func TestXAIConvertOpenAIRequestGrok3MiniSuffix(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{Model: "grok-3-mini-high"}
	maxTokens := uint(512)
	request.MaxTokens = &maxTokens
	info := newXAIRelayInfo("grok-3-mini-high")
	out, err := (&Adaptor{}).ConvertOpenAIRequest(nil, info, request)
	require.NoError(t, err)
	converted, ok := out.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.Equal(t, "grok-3-mini", converted.Model)
	require.Equal(t, "high", converted.ReasoningEffort)
	require.NotNil(t, converted.MaxCompletionTokens)
	require.Equal(t, uint(512), *converted.MaxCompletionTokens)
	require.Nil(t, converted.MaxTokens)
	require.Equal(t, "grok-3-mini", info.UpstreamModelName)
}

// D. Streaming fidelity: every upstream chunk is forwarded in order with its
// reasoning content, tool call and finish reason intact, the final usage frame
// is authoritative, and the stream ends with exactly one [DONE].
func TestXAIStreamHandlerForwardsSequenceAndUsage(t *testing.T) {
	enableStreamingTimeout(t)
	raw := loadXAIFixture(t, "xai_chat_stream_events.sse")
	info := newXAIRelayInfo("grok-4")
	c, recorder := newXAIContext(t)

	usage, apiErr := xAIStreamHandler(c, info, newXAIUpstreamResponse(t, string(raw)))
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, 88, usage.PromptTokens, "upstream prompt_tokens is authoritative")
	require.Equal(t, 21, usage.CompletionTokens, "completion derives from upstream total-prompt")
	require.Equal(t, 109, usage.TotalTokens)

	frames := splitSSEFrames(t, recorder.Body.String())
	require.Len(t, frames, 7, "6 upstream chunks + [DONE]")
	require.Equal(t, "[DONE]", frames[len(frames)-1], "the stream must end with exactly one [DONE]")

	// Reasoning content, tool call and finish reason survive verbatim. The
	// tool arguments are re-marshaled JSON-in-JSON, so assert the escaped form.
	joined := strings.Join(frames, "\n")
	require.Contains(t, joined, `"reasoning_content":"checking the weather tool"`)
	require.Contains(t, joined, `"get_weather"`)
	require.Contains(t, joined, `arguments":"{\"city\":\"Hangzhou\"}"`)
	require.Contains(t, joined, `"finish_reason":"tool_calls"`)

	// The usage-bearing frame carries the recomputed completion tokens.
	var usageFrame *dto.ChatCompletionsStreamResponse
	for _, frame := range frames {
		candidate := &dto.ChatCompletionsStreamResponse{}
		if common.Unmarshal([]byte(frame), candidate) == nil && candidate.Usage != nil {
			usageFrame = candidate
		}
	}
	require.NotNil(t, usageFrame, "a final usage frame must be forwarded")
	require.Equal(t, 21, usageFrame.Usage.CompletionTokens)

	require.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
	require.False(t, service.TokenObservationForLog(c, false).Interrupted)
}

// E. Upstream EOF before [DONE] must never become a synthetic successful
// [DONE]: the client gets an incomplete-stream error frame, the stream is
// marked interrupted, and the fallback usage is flagged as a local estimate.
func TestXAIStreamInterruptedDoesNotForgeDone(t *testing.T) {
	enableStreamingTimeout(t)
	raw := loadXAIFixture(t, "xai_chat_stream_interrupted.sse")
	info := newXAIRelayInfo("grok-4")
	info.SetEstimatePromptTokens(77)
	c, recorder := newXAIContext(t)

	usage, apiErr := xAIStreamHandler(c, info, newXAIUpstreamResponse(t, string(raw)))
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, 77, usage.PromptTokens, "estimate falls back to the pre-consume prompt estimate")
	require.Positive(t, usage.CompletionTokens, "received partial output is estimated, not zeroed")
	require.Equal(t, usage.PromptTokens+usage.CompletionTokens, usage.TotalTokens)

	frames := splitSSEFrames(t, recorder.Body.String())
	require.NotEmpty(t, frames)
	require.NotEqual(t, "[DONE]", frames[len(frames)-1], "an interrupted stream must not end with [DONE]")
	for _, frame := range frames {
		require.NotEqual(t, "[DONE]", frame, "no synthetic [DONE] may be written for an interrupted stream")
	}
	require.Contains(t, recorder.Body.String(), `"code":"incomplete_stream"`)
	require.Contains(t, recorder.Body.String(), "upstream stream ended before the terminal event")

	observation := service.TokenObservationForLog(c, false)
	require.True(t, observation.Interrupted, "the interruption must be recorded on the token observation")
	require.Equal(t, "local_estimate", observation.Source)

	require.NotEqual(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
}

// F. When the upstream already reported usage before the disconnect, that
// authoritative usage is kept as-is — it is not replaced by an estimate and
// the reported prompt/output tokens still settle.
func TestXAIStreamInterruptedKeepsReportedUsage(t *testing.T) {
	enableStreamingTimeout(t)
	raw := loadXAIFixture(t, "xai_chat_stream_interrupted_after_usage.sse")
	info := newXAIRelayInfo("grok-4")
	info.SetEstimatePromptTokens(77)
	c, recorder := newXAIContext(t)

	usage, apiErr := xAIStreamHandler(c, info, newXAIUpstreamResponse(t, string(raw)))
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, 88, usage.PromptTokens, "upstream-reported usage must not be overwritten by the estimate")
	require.Equal(t, 21, usage.CompletionTokens)
	require.Equal(t, 109, usage.TotalTokens)

	require.True(t, service.TokenObservationForLog(c, false).Interrupted)
	require.NotEqual(t, "[DONE]", splitSSEFrames(t, recorder.Body.String())[len(splitSSEFrames(t, recorder.Body.String()))-1])
	require.Contains(t, recorder.Body.String(), `"code":"incomplete_stream"`)
}

// G. Non-stream usage: xAI reports prompt/completion/total with reasoning
// detail; completion is recomputed from total-prompt and text tokens are the
// non-reasoning remainder. Nothing is dropped or double counted.
func TestXAIHandlerNonStreamUsage(t *testing.T) {
	raw := loadXAIFixture(t, "xai_chat_response.json")
	info := newXAIRelayInfo("grok-4")
	c, recorder := newXAIContext(t)

	usage, apiErr := xAIHandler(c, info, newXAIUpstreamResponse(t, string(raw)))
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, 50, usage.PromptTokens)
	require.Equal(t, 30, usage.CompletionTokens)
	require.Equal(t, 80, usage.TotalTokens)
	require.Equal(t, 12, usage.CompletionTokenDetails.ReasoningTokens)
	require.Equal(t, 18, usage.CompletionTokenDetails.TextTokens,
		"text tokens must exclude reasoning tokens, never the other way round")

	forwarded := &ChatCompletionResponse{}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), forwarded))
	require.Equal(t, "grok-4", forwarded.Model)
	require.Equal(t, "Mostly sunny, 24C.", forwarded.Choices[0].Message.StringContent())
}
