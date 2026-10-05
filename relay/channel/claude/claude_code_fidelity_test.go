package claude

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// The fixtures in testdata/ are desensitized, protocol-accurate samples of what
// the Claude Code client sends on /v1/messages. They carry no credentials.

// loadClaudeCodeFixture loads one fixture file.
func loadClaudeCodeFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)
	return raw
}

// requireNoClaudeError asserts a nil *types.NewAPIError. The handler returns a
// pointer, so the check must stay on the pointer type — boxing a nil pointer
// into the error interface would report a (printed-empty) non-nil error.
func requireNoClaudeError(t *testing.T, err *types.NewAPIError) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected relay error: statusCode=%d err=%+v", err.StatusCode, err)
	}
}

// A. Messages fidelity: a Claude Code request survives the native Claude
// adaptor unchanged — no field dropped, no value rewritten, block order,
// tool IDs and cache_control preserved.
func TestClaudeCodeMessagesRequestRoundTripFidelity(t *testing.T) {
	raw := loadClaudeCodeFixture(t, "claude_code_messages_request.json")
	request := &dto.ClaudeRequest{}
	require.NoError(t, json.Unmarshal(raw, request))

	info := &relaycommon.RelayInfo{
		OriginModelName: request.Model,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: request.Model,
		},
	}
	out, err := (&Adaptor{}).ConvertClaudeRequest(nil, info, request)
	require.NoError(t, err)
	converted, ok := out.(*dto.ClaudeRequest)
	require.True(t, ok)

	emitted, err := json.Marshal(converted)
	require.NoError(t, err)
	require.JSONEq(t, string(raw), string(emitted),
		"native Claude Code request must round-trip through the Claude adaptor unchanged")

	require.Empty(t, info.ConversionDiagnostics())
}

// B. Streaming fidelity: the Claude Code SSE event sequence passes through the
// RelayFormatClaude path with the same event names, in the same order, with
// semantically identical payloads, ending in exactly one message_stop.
func TestClaudeCodeStreamEventSequencePassthrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	raw := loadClaudeCodeFixture(t, "claude_code_stream_events.jsonl")
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	require.NotEmpty(t, lines)

	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatClaude,
		OriginModelName: "claude-sonnet-4-5-20250929",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "claude-sonnet-4-5-20250929",
		},
		StreamStatus: relaycommon.NewStreamStatus(),
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	claudeInfo := &ClaudeResponseInfo{
		ResponseId:   "msg_01SyntheticFixture",
		Model:        "claude-sonnet-4-5-20250929",
		ResponseText: strings.Builder{},
		Usage:        &dto.Usage{},
	}

	for _, line := range lines {
		requireNoClaudeError(t, HandleStreamResponseData(c, info, claudeInfo, strings.TrimSpace(line)))
	}

	var emittedNames []string
	var emittedData []string
	for _, chunk := range strings.Split(recorder.Body.String(), "\n\n") {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		var eventName, data string
		for _, row := range strings.Split(chunk, "\n") {
			if strings.HasPrefix(row, "event: ") {
				eventName = strings.TrimPrefix(row, "event: ")
			}
			if strings.HasPrefix(row, "data: ") {
				data = strings.TrimPrefix(row, "data: ")
			}
		}
		emittedNames = append(emittedNames, eventName)
		emittedData = append(emittedData, data)
	}

	require.Len(t, emittedNames, len(lines))
	require.Len(t, emittedData, len(lines))
	for i, line := range lines {
		expected := &dto.ClaudeResponse{}
		require.NoError(t, json.Unmarshal([]byte(line), expected))
		require.Equal(t, expected.Type, emittedNames[i], "event %d name", i)

		if expected.Type == "message_delta" {
			// new-api's Bedrock workaround may enrich message_delta usage
			// additively (e.g. synthesizing the structured cache_creation
			// object); every field the upstream sent must survive unchanged.
			actual := &dto.ClaudeResponse{}
			require.NoError(t, json.Unmarshal([]byte(emittedData[i]), actual))
			require.Equal(t, expected.Delta, actual.Delta, "event %d delta", i)
			require.Equal(t, expected.Usage.InputTokens, actual.Usage.InputTokens, "event %d usage.input_tokens", i)
			require.Equal(t, expected.Usage.OutputTokens, actual.Usage.OutputTokens, "event %d usage.output_tokens", i)
			require.Equal(t, expected.Usage.CacheCreationInputTokens, actual.Usage.CacheCreationInputTokens, "event %d usage.cache_creation_input_tokens", i)
			require.Equal(t, expected.Usage.CacheReadInputTokens, actual.Usage.CacheReadInputTokens, "event %d usage.cache_read_input_tokens", i)
			continue
		}
		actual := &dto.ClaudeResponse{}
		require.NoError(t, json.Unmarshal([]byte(emittedData[i]), actual), "event %d data must be valid JSON", i)
		require.JSONEq(t, line, emittedData[i], "event %d payload must survive passthrough", i)
	}
	require.Equal(t, "message_stop", emittedNames[len(emittedNames)-1], "the last event must be the terminal event")
	require.Equal(t, 1, countOccurrences(emittedNames, "message_start"), "message_start must not repeat")
	require.Equal(t, 1, countOccurrences(emittedNames, "message_stop"), "message_stop must not repeat")
}

func countOccurrences(values []string, target string) int {
	count := 0
	for _, value := range values {
		if value == target {
			count++
		}
	}
	return count
}

// The streaming passthrough accumulates authoritative usage: input/cache tokens
// from message_start, output tokens from message_delta, all under the
// anthropic usage semantic.
func TestClaudeCodeStreamUsageAccumulation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	raw := loadClaudeCodeFixture(t, "claude_code_stream_events.jsonl")

	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatClaude,
		OriginModelName: "claude-sonnet-4-5-20250929",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "claude-sonnet-4-5-20250929",
		},
		StreamStatus: relaycommon.NewStreamStatus(),
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	claudeInfo := &ClaudeResponseInfo{
		Model:        "claude-sonnet-4-5-20250929",
		ResponseText: strings.Builder{},
		Usage:        &dto.Usage{},
	}

	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		requireNoClaudeError(t, HandleStreamResponseData(c, info, claudeInfo, strings.TrimSpace(line)))
	}

	require.Equal(t, 1050, claudeInfo.Usage.PromptTokens)
	require.Equal(t, 42, claudeInfo.Usage.CompletionTokens)
	require.Equal(t, 2048, claudeInfo.Usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, 512, claudeInfo.Usage.PromptTokensDetails.CachedCreationTokens)
}

// C. Upstream EOF before the terminal event is detected as a non-completed
// stream, never as a silent success.
func TestClaudeCodeStreamInterruptedBeforeTerminalEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	raw := loadClaudeCodeFixture(t, "claude_code_stream_events.jsonl")
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")

	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "claude-sonnet-4-5-20250929",
		},
		StreamStatus: relaycommon.NewStreamStatus(),
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	claudeInfo := &ClaudeResponseInfo{
		Model:        "claude-sonnet-4-5-20250929",
		ResponseText: strings.Builder{},
		Usage:        &dto.Usage{},
	}

	// Feed everything except the final message_stop (and its message_delta),
	// simulating an upstream disconnect mid-stream.
	for _, line := range lines[:len(lines)-2] {
		requireNoClaudeError(t, HandleStreamResponseData(c, info, claudeInfo, strings.TrimSpace(line)))
	}

	info.StreamStatus.RequireTerminal()
	require.NotEqual(t, "completed", info.StreamStatus.ResponseOutcome(),
		"a stream without message_stop must not be reported as completed")
}

// D. A malformed upstream event fails loudly instead of being forwarded or
// silently swallowed.
func TestClaudeCodeStreamMalformedEventFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "claude-sonnet-4-5-20250929",
		},
		StreamStatus: relaycommon.NewStreamStatus(),
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	claudeInfo := &ClaudeResponseInfo{
		Model:        "claude-sonnet-4-5-20250929",
		ResponseText: strings.Builder{},
		Usage:        &dto.Usage{},
	}

	err := HandleStreamResponseData(c, info, claudeInfo, "{not-json")
	require.Error(t, err)
	require.Empty(t, recorder.Body.String(), "a malformed event must not reach the client")
}

// E. An in-stream upstream error event surfaces as a Claude-typed error; the
// client-visible rendering keeps the Anthropic error type and message.
func TestClaudeCodeStreamErrorEventSurfacesClaudeError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "claude-sonnet-4-5-20250929",
		},
		StreamStatus: relaycommon.NewStreamStatus(),
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	claudeInfo := &ClaudeResponseInfo{
		Model:        "claude-sonnet-4-5-20250929",
		ResponseText: strings.Builder{},
		Usage:        &dto.Usage{},
	}

	err := HandleStreamResponseData(c, info, claudeInfo,
		`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)
	require.NotNil(t, err)
	require.Equal(t, http.StatusInternalServerError, err.StatusCode)
	require.Equal(t, "Overloaded", err.Error())

	// The controller renders RelayFormatClaude errors with ToClaudeError; the
	// Anthropic error type must survive into that client-visible shape.
	rendered := gin.H{"type": "error", "error": err.ToClaudeError()}
	encoded, jsonErr := json.Marshal(rendered)
	require.NoError(t, jsonErr)
	require.JSONEq(t, `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`, string(encoded))
	require.Empty(t, recorder.Body.String())
}

// The message_delta usage patch is additive only: fields the upstream sent
// survive unchanged, and the patch may synthesize the structured cache_creation
// object from the flat cache_creation_input_tokens field. Nothing is removed or
// rewritten (Claude Code consumes these events directly).
func TestClaudeCodeStreamMessageDeltaPatchIsAdditiveOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	raw := loadClaudeCodeFixture(t, "claude_code_stream_events.jsonl")
	var messageDelta string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, `"type":"message_delta"`) {
			messageDelta = line
			break
		}
	}
	require.NotEmpty(t, messageDelta)

	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "claude-sonnet-4-5-20250929",
		},
		StreamStatus: relaycommon.NewStreamStatus(),
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	claudeInfo := &ClaudeResponseInfo{
		Model:        "claude-sonnet-4-5-20250929",
		ResponseText: strings.Builder{},
		Usage:        &dto.Usage{},
	}
	requireNoClaudeError(t, HandleStreamResponseData(c, info, claudeInfo, messageDelta))
	require.Contains(t, recorder.Body.String(), "event: message_delta\n")
	data := strings.SplitN(recorder.Body.String(), "data: ", 2)[1]
	data = strings.TrimSuffix(data, "\n")

	original := &dto.ClaudeResponse{}
	require.NoError(t, json.Unmarshal([]byte(messageDelta), original))
	emitted := &dto.ClaudeResponse{}
	require.NoError(t, json.Unmarshal([]byte(data), emitted))
	require.Equal(t, original.Type, emitted.Type)
	require.Equal(t, original.Delta, emitted.Delta)
	require.Equal(t, original.Usage.InputTokens, emitted.Usage.InputTokens)
	require.Equal(t, original.Usage.OutputTokens, emitted.Usage.OutputTokens)
	require.Equal(t, original.Usage.CacheCreationInputTokens, emitted.Usage.CacheCreationInputTokens)
	require.Equal(t, original.Usage.CacheReadInputTokens, emitted.Usage.CacheReadInputTokens)
	require.NotNil(t, emitted.Usage.CacheCreation, "structured cache_creation is synthesized for clients")
}

// helper.ClaudeChunkData stays silent once the client is gone: events after a
// client disconnect are not written, while the handler keeps consuming upstream
// data until the scanner closes it.
func TestClaudeCodeStreamClientCancelStopsWrites(t *testing.T) {
	gin.SetMode(gin.TestMode)
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "claude-sonnet-4-5-20250929",
		},
		StreamStatus: relaycommon.NewStreamStatus(),
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest("POST", "/v1/messages", nil)
	cancelled, cancel := context.WithCancel(request.Context())
	cancel()
	defer cancel()
	c.Request = request.WithContext(cancelled)

	claudeInfo := &ClaudeResponseInfo{
		Model:        "claude-sonnet-4-5-20250929",
		ResponseText: strings.Builder{},
		Usage:        &dto.Usage{},
	}
	requireNoClaudeError(t, HandleStreamResponseData(c, info, claudeInfo,
		`{"type":"message_start","message":{"id":"msg_x","type":"message","role":"assistant","model":"claude-sonnet-4-5-20250929","usage":{"input_tokens":1,"output_tokens":1}}}`))
	require.Empty(t, recorder.Body.String(), "no event may be written after the client cancelled")
	helper.FlushWriter(c)
}
