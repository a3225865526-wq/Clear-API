package zhipu_4v

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

// A Gemini generateContent request on a GLM-4V channel must leave as a chat
// body with the Zhipu top_p rule applied, replacing the previous
// "not implemented" failure.
func TestConvertGeminiRequestProducesChatBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/glm-4.6:generateContent", nil)

	topP := 1.0
	request := &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{
			{Role: "user", Parts: []dto.GeminiPart{{Text: "hello"}}},
		},
		GenerationConfig: dto.GeminiChatGenerationConfig{TopP: &topP},
	}
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatGemini,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "glm-4.6"},
	}

	converted, err := (&Adaptor{}).ConvertGeminiRequest(c, info, request)
	require.NoError(t, err)

	body, err := common.Marshal(converted)
	require.NoError(t, err)

	var payload struct {
		Model    string  `json:"model"`
		TopP     float64 `json:"top_p"`
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	require.NoError(t, common.Unmarshal(body, &payload))
	require.Equal(t, "glm-4.6", payload.Model)
	require.Equal(t, 0.99, payload.TopP, "Zhipu rejects top_p >= 1")
	require.Len(t, payload.Messages, 1)
	require.Equal(t, "hello", payload.Messages[0].Content)
}
