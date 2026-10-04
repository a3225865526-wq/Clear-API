package openai

import (
	"github.com/QuantumNous/new-api/service"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestObserveStreamChoicesDedupesFunctionCallNames(t *testing.T) {
	info := &relaycommon.RelayInfo{StreamStatus: relaycommon.NewStreamStatus()}
	seen := make(map[string]struct{})
	var names []string

	chunks := []string{
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"get_weather","arguments":""}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"x\"}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"c2","type":"function","function":{"name":"get_time","arguments":""}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{}"}}]}}]}`,
	}
	for _, chunk := range chunks {
		observeStreamChoices(info, chunk, seen, &names)
	}

	require.Len(t, names, 2)
	assert.Equal(t, []string{"get_weather", "get_time"}, names)
	assert.Empty(t, info.StreamStatus.ResponseOutcome(), "tool call deltas carry no finish reason")

	observeStreamChoices(info, `{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`, seen, &names)
	assert.Equal(t, "completed", info.StreamStatus.ResponseOutcome())
	assert.False(t, info.PerformanceBusinessRejection)

	filtered := &relaycommon.RelayInfo{StreamStatus: relaycommon.NewStreamStatus()}
	observeStreamChoices(filtered, `{"choices":[{"index":0,"delta":{},"finish_reason":"content_filter"}]}`, map[string]struct{}{}, &names)
	assert.True(t, filtered.PerformanceBusinessRejection)
}

func TestOaiStreamHandlerMarksMissingDoneAsIncomplete(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := "data: {\"id\":\"chatcmpl-test\",\"created\":1,\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n"
	c, recorder, resp, info := newImageTestContext(t, body, "text/event-stream", true)
	info.ChannelMeta = &relaycommon.ChannelMeta{}
	info.RelayFormat = types.RelayFormatOpenAI
	info.UpstreamModelName = "test-model"
	info.DisablePing = true

	usage, apiErr := OaiStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Zero(t, usage.PromptTokens, "incomplete streams must not estimate upstream token usage")
	assert.Zero(t, usage.CompletionTokens, "incomplete streams must not estimate upstream token usage")
	assert.NotContains(t, recorder.Body.String(), "[DONE]")
	assert.Contains(t, recorder.Body.String(), "chatcmpl-test", "the last received chunk must be preserved")
	assert.Contains(t, recorder.Body.String(), "upstream_error")
	assert.Contains(t, recorder.Body.String(), "terminal event")
	assert.Equal(t, relaycommon.StreamEndReasonEOF, info.StreamStatus.EndReason)
	assert.Equal(t, string(relaycommon.ResponseOutcomeFailed), info.StreamStatus.ResponseOutcome())
	assert.True(t, info.StreamStatus.HasErrors())
	observation := service.TokenObservationForLog(c, false)
	assert.True(t, observation.Interrupted)
	assert.Nil(t, observation.Total)
}

func TestUsageOaiStreamToolFramesPreserveRawMetering(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	body := `data: {"id":"synthetic","model":"test-model","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"synthetic_tool","arguments":"{}"}}]}}]}` + "\n\n" +
		`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}}` + "\n\ndata: [DONE]\n\n"
	c, recorder, resp, info := newImageTestContext(t, body, "text/event-stream", true)
	info.ChannelMeta = &relaycommon.ChannelMeta{UpstreamModelName: "test-model"}
	info.RelayFormat = types.RelayFormatOpenAI
	info.DisablePing = true
	usage, err := OaiStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	assert.Equal(t, 15, usage.TotalTokens)
	observation := service.TokenObservationForLog(c, false)
	assert.Equal(t, "upstream_complete", observation.Quality)
	require.NotNil(t, observation.Total)
	assert.Equal(t, int64(15), *observation.Total)
	assert.Contains(t, recorder.Body.String(), "synthetic_tool")
	assert.Contains(t, recorder.Body.String(), "[DONE]")
}

func TestOaiStreamHandlerKeepsDoneForCompleteStream(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := "data: {\"id\":\"chatcmpl-test\",\"created\":1,\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl-test\",\"created\":1,\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	c, recorder, resp, info := newImageTestContext(t, body, "text/event-stream", true)
	info.ChannelMeta = &relaycommon.ChannelMeta{}
	info.RelayFormat = types.RelayFormatOpenAI
	info.UpstreamModelName = "test-model"
	info.DisablePing = true

	usage, apiErr := OaiStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Contains(t, recorder.Body.String(), "[DONE]")
	assert.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
	assert.Equal(t, string(relaycommon.ResponseOutcomeCompleted), info.StreamStatus.ResponseOutcome())
}

func TestOaiStreamHandlerBillsPartialOutputWithoutDone(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	for _, tc := range []struct {
		name, tail         string
		prompt, completion int
	}{
		{"estimated", "", 100, 0},
		{"upstream usage", "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":123,\"completion_tokens\":7,\"total_tokens\":130}}\n\n", 123, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := "data: {\"id\":\"partial\",\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello world\"}}]}\n\n" + tc.tail
			c, recorder, resp, info := newImageTestContext(t, body, "text/event-stream", true)
			info.ChannelMeta = &relaycommon.ChannelMeta{}
			info.RelayFormat = types.RelayFormatOpenAI
			info.RelayMode = relayconstant.RelayModeChatCompletions
			info.UpstreamModelName = "test-model"
			info.SetEstimatePromptTokens(100)
			info.DisablePing = true
			usage, apiErr := OaiStreamHandler(c, info, resp)
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Equal(t, tc.prompt, usage.PromptTokens)
			assert.Positive(t, usage.CompletionTokens)
			if tc.completion > 0 {
				assert.Equal(t, tc.completion, usage.CompletionTokens)
			}
			assert.NotContains(t, recorder.Body.String(), "[DONE]")
			assert.Equal(t, string(relaycommon.ResponseOutcomeFailed), info.StreamStatus.ResponseOutcome())
		})
	}
}
