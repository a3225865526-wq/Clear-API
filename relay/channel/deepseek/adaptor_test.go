package deepseek

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newGeminiRelayInfo(model string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatGemini,
		RelayMode:   0,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: model},
	}
}

func newGeminiContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/deepseek-chat:generateContent", nil)
	return c
}

// A Gemini generateContent request arriving on a DeepSeek channel has to leave
// as a chat-completions body: the gateway converts it and DeepSeek rewriting
// (thinking suffixes included) still applies, instead of the old
// "not implemented" failure.
func TestConvertGeminiRequestProducesChatBody(t *testing.T) {
	request := &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{
			{Role: "user", Parts: []dto.GeminiPart{{Text: "hello"}}},
		},
	}
	info := newGeminiRelayInfo("deepseek-v4-flash-max")

	converted, err := (&Adaptor{}).ConvertGeminiRequest(newGeminiContext(t), info, request)
	require.NoError(t, err)

	body, err := common.Marshal(converted)
	require.NoError(t, err)

	var payload struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	require.NoError(t, common.Unmarshal(body, &payload))
	require.Equal(t, "deepseek-v4-flash", payload.Model, "the thinking-effort suffix is rewritten for the provider")
	require.Len(t, payload.Messages, 1)
	require.Equal(t, "user", payload.Messages[0].Role)
	require.Equal(t, "hello", payload.Messages[0].Content)
	require.Equal(t, "deepseek-v4-flash", info.UpstreamModelName, "billing and logging see the rewritten upstream model")
}
