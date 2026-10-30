package sub2api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetRequestURLAlphaSearch(t *testing.T) {
	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeSub2API,
			ChannelBaseUrl: "https://sub2api.example",
		},
		RequestURLPath: "/v1/alpha/search",
		RelayMode:      relayconstant.RelayModeAlphaSearch,
	}

	url, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://sub2api.example/v1/alpha/search", url)
}

// These exercise the real type=59 transport and response handlers against an
// HTTP mock. They deliberately do not stand in for a running Sub2API or wallet
// reconciliation across the two services.
func TestSub2APIProtocolContract(t *testing.T) {
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 10
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	chat := `{"id":"chat-fixture","model":"pool-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"cached_tokens":30}}}`
	responses := `{"id":"resp-fixture","model":"pool-model","status":"completed","output":[],"usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120,"input_tokens_details":{"cached_tokens":30}}}`
	messages := `{"id":"msg-fixture","type":"message","model":"pool-model","role":"assistant","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":70,"output_tokens":20,"cache_read_input_tokens":30}}`
	for _, tc := range []struct {
		name, path         string
		format             types.RelayFormat
		response, terminal string
		mode, prompt       int
		stream             bool
	}{
		{"chat JSON", "/v1/chat/completions", types.RelayFormatOpenAI, chat, "chat-fixture", relayconstant.RelayModeChatCompletions, 100, false},
		{"responses JSON", "/v1/responses", types.RelayFormatOpenAIResponses, responses, "resp-fixture", relayconstant.RelayModeResponses, 100, false},
		{"messages JSON", "/v1/messages?beta=true", types.RelayFormatClaude, messages, "msg-fixture", relayconstant.RelayModeChatCompletions, 70, false},
		{"chat SSE", "/v1/chat/completions", types.RelayFormatOpenAI,
			`data: {"id":"chat-fixture","model":"pool-model","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"cached_tokens":30}}}` + "\n\ndata: [DONE]\n\n", "[DONE]", relayconstant.RelayModeChatCompletions, 100, true},
		{"responses SSE", "/v1/responses", types.RelayFormatOpenAIResponses,
			"event: response.completed\ndata: " + `{"type":"response.completed","response":` + responses + "}\n\n", "response.completed", relayconstant.RelayModeResponses, 100, true},
		{"messages SSE", "/v1/messages?beta=true", types.RelayFormatClaude,
			"event: message_start\ndata: " + `{"type":"message_start","message":{"id":"msg-fixture","type":"message","role":"assistant","model":"pool-model","content":[],"usage":{"input_tokens":70,"output_tokens":0,"cache_read_input_tokens":30}}}` +
				"\n\nevent: message_delta\ndata: " + `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":20}}` +
				"\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n", "message_stop", relayconstant.RelayModeChatCompletions, 70, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tc.path, r.URL.RequestURI())
				assert.Equal(t, "Bearer internal-fixture-key", r.Header.Get("Authorization"))
				assert.Equal(t, "fixture-session", r.Header.Get("Session_id"))
				assert.Empty(t, r.Header.Get("Cookie"))
				assert.Empty(t, r.Header.Get("X-Goog-Api-Key"))
				if tc.format == types.RelayFormatClaude {
					assert.Equal(t, "internal-fixture-key", r.Header.Get("X-Api-Key"))
					assert.Equal(t, "2023-06-01", r.Header.Get("Anthropic-Version"))
				} else {
					assert.Empty(t, r.Header.Get("X-Api-Key"))
				}
				body, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				var payload map[string]any
				assert.NoError(t, common.Unmarshal(body, &payload))
				assert.Equal(t, "pool-model", payload["model"])
				if tc.stream {
					assert.Equal(t, "text/event-stream", r.Header.Get("Accept"))
					w.Header().Set("Content-Type", "text/event-stream")
				} else {
					w.Header().Set("Content-Type", "application/json")
				}
				_, _ = io.WriteString(w, tc.response)
				calls <- struct{}{}
			}))
			t.Cleanup(server.Close)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader("{}"))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Request.Header.Set("Authorization", "Bearer clearapi-user-fixture")
			c.Request.Header.Set("X-Api-Key", "clearapi-user-fixture")
			c.Request.Header.Set("X-Goog-Api-Key", "clearapi-user-fixture")
			c.Request.Header.Set("Cookie", "session=client-fixture")
			c.Request.Header.Set("Session_id", "fixture-session")
			c.Set("model_mapping", `{"public-model":"pool-model"}`)
			info := &relaycommon.RelayInfo{
				OriginModelName: "public-model", RequestURLPath: tc.path,
				RelayMode: tc.mode, RelayFormat: tc.format, IsStream: tc.stream,
				DisablePing: true, ShouldIncludeUsage: true,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType: constant.ChannelTypeSub2API, ChannelBaseUrl: server.URL,
					ApiKey: "internal-fixture-key", UpstreamModelName: "public-model",
					HeadersOverride: map[string]any{"Session_id": "{client_header:Session_id}"},
				},
			}
			maxTokens := uint(32)
			var request dto.Request
			switch tc.format {
			case types.RelayFormatClaude:
				request = &dto.ClaudeRequest{Model: "public-model", MaxTokens: &maxTokens, Stream: &tc.stream,
					Messages: []dto.ClaudeMessage{{Role: "user", Content: "hello"}}}
			case types.RelayFormatOpenAIResponses:
				request = &dto.OpenAIResponsesRequest{Model: "public-model", Input: json.RawMessage(`"hello"`),
					MaxOutputTokens: &maxTokens, Stream: &tc.stream}
			default:
				request = &dto.GeneralOpenAIRequest{Model: "public-model", MaxTokens: &maxTokens, Stream: &tc.stream,
					Messages: []dto.Message{{Role: "user", Content: "hello"}}}
			}
			require.NoError(t, helper.ModelMappedHelper(c, info, request))
			adaptor := &Adaptor{}
			adaptor.Init(info)
			var converted any
			var err error
			switch request := request.(type) {
			case *dto.ClaudeRequest:
				converted, err = adaptor.ConvertClaudeRequest(c, info, request)
			case *dto.OpenAIResponsesRequest:
				converted, err = adaptor.ConvertOpenAIResponsesRequest(c, info, *request)
			case *dto.GeneralOpenAIRequest:
				converted, err = adaptor.ConvertOpenAIRequest(c, info, request)
			}
			require.NoError(t, err)
			body, err := common.Marshal(converted)
			require.NoError(t, err)
			result, err := adaptor.DoRequest(c, info, strings.NewReader(string(body)))
			require.NoError(t, err)
			resp, ok := result.(*http.Response)
			require.True(t, ok)
			t.Cleanup(func() { _ = resp.Body.Close() })
			usageValue, apiErr := adaptor.DoResponse(c, resp, info)
			require.Nil(t, apiErr)
			usage, ok := usageValue.(*dto.Usage)
			require.True(t, ok)
			require.NotNil(t, usage)
			assert.Equal(t, tc.prompt, usage.PromptTokens)
			assert.Equal(t, 20, usage.CompletionTokens)
			assert.Equal(t, 30, usage.PromptTokensDetails.CachedTokens)
			assert.Contains(t, recorder.Body.String(), tc.terminal)
			assert.Equal(t, "public-model", info.OriginModelName)
			assert.Equal(t, "pool-model", info.UpstreamModelName)
			assert.Len(t, calls, 1)
		})
	}
}

func TestAdaptorInheritsNewAPIResponsesCompactSupport(t *testing.T) {
	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeSub2API,
			ChannelBaseUrl: "https://sub2api.example",
		},
		RequestURLPath: "/v1/responses/compact",
		RelayMode:      relayconstant.RelayModeResponsesCompact,
	}

	url, err := adaptor.GetRequestURL(info)

	require.NoError(t, err)
	assert.Equal(t, "https://sub2api.example/v1/responses/compact", url)
	assert.Equal(t, "sub2api", adaptor.GetChannelName())
	assert.Empty(t, adaptor.GetModelList())
}

func TestConvertClaudeRequestPreservesAdaptiveThinkingForCompatibleModel(t *testing.T) {
	adaptor := &Adaptor{}
	maxTokens := uint(8192)
	temperature := 0.2
	topP := 0.99
	request := &dto.ClaudeRequest{
		Model:        "gpt-5.6-sol",
		MaxTokens:    &maxTokens,
		Temperature:  &temperature,
		TopP:         &topP,
		Thinking:     &dto.Thinking{Type: "adaptive", Display: "summarized"},
		OutputConfig: json.RawMessage(`{"effort":"xhigh","provider_option":true}`),
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "hello"},
		},
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeSub2API,
		},
	}

	converted, err := adaptor.ConvertClaudeRequest(nil, info, request)

	require.NoError(t, err)
	assert.Same(t, request, converted)
	require.NotNil(t, request.Thinking)
	assert.Equal(t, "adaptive", request.Thinking.Type)
	assert.Equal(t, "summarized", request.Thinking.Display)
	assert.JSONEq(t, `{"effort":"xhigh","provider_option":true}`, string(request.OutputConfig))
	assert.Same(t, &temperature, request.Temperature)
	assert.Same(t, &topP, request.TopP)
	assert.Equal(t, "xhigh", info.ReasoningEffort)
	assert.Equal(t, "gpt-5.6-sol", info.UpstreamModelName)
}
