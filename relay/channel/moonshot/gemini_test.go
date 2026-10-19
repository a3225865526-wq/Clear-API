package moonshot

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

// A Gemini generateContent request on a Kimi channel must leave as a chat body
// with the per-model temperature rule still applied, replacing the previous
// "not implemented" failure.
func TestConvertGeminiRequestProducesChatBodyWithTemperatureRule(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/kimi-k2.6:generateContent", nil)

	temperature := 0.7
	request := &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{
			{Role: "user", Parts: []dto.GeminiPart{{Text: "hello"}}},
		},
		GenerationConfig: dto.GeminiChatGenerationConfig{Temperature: &temperature},
	}
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatGemini,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "kimi-k2.6"},
	}

	converted, err := (&Adaptor{}).ConvertGeminiRequest(c, info, request)
	require.NoError(t, err)

	body, err := common.Marshal(converted)
	require.NoError(t, err)

	var payload struct {
		Model       string  `json:"model"`
		Temperature float64 `json:"temperature"`
		Messages    []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	require.NoError(t, common.Unmarshal(body, &payload))
	require.Equal(t, "kimi-k2.6", payload.Model)
	require.Equal(t, 1.0, payload.Temperature, "kimi-k2.6 only accepts temperature 1")
	require.Len(t, payload.Messages, 1)
	require.Equal(t, "hello", payload.Messages[0].Content)
}
