package channel

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newInboundContext(t *testing.T) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/qwen-plus:generateContent", nil)
	return c
}

// Chat-compatible adaptors rely on this helper to turn a Gemini request into
// the chat body their upstream expects, and it must record the outbound
// protocol so conversion bookkeeping matches a native chat request.
func TestConvertGeminiRequestToChatProducesChatRequest(t *testing.T) {
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatGemini}
	request := &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{{Role: "user", Parts: []dto.GeminiPart{{Text: "hello"}}}},
	}

	chatRequest, err := ConvertGeminiRequestToChat(newInboundContext(t), info, request)
	require.NoError(t, err)

	body, err := common.Marshal(chatRequest)
	require.NoError(t, err)
	var payload struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	require.NoError(t, common.Unmarshal(body, &payload))
	require.Len(t, payload.Messages, 1)
	assert.Equal(t, "user", payload.Messages[0].Role)
	assert.Equal(t, "hello", payload.Messages[0].Content)

	assert.Equal(t, types.RelayFormatOpenAI, info.GetFinalRequestRelayFormat())
}

func TestConvertGeminiRequestToChatRequiresContext(t *testing.T) {
	_, err := ConvertGeminiRequestToChat(nil, &relaycommon.RelayInfo{}, &dto.GeminiChatRequest{})
	require.Error(t, err)
}

// Channels that cannot translate a protocol return a client error that skips
// channel retries: a retryable 500 would burn attempts on unrelated channels and
// still fail for the client.
func TestNewUnsupportedEndpointErrorIsNonRetryableClientError(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiType: 4}}
	err := NewUnsupportedEndpointError(newInboundContext(t), info, "the legacy API accepts its own prompt format")

	assert.Equal(t, http.StatusBadRequest, err.StatusCode)
	assert.True(t, types.IsSkipRetryError(err))
	assert.Contains(t, err.Error(), "/v1beta/models/qwen-plus:generateContent")
	assert.Contains(t, err.Error(), "the legacy API accepts its own prompt format")
}
