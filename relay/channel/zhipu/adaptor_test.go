package zhipu

import (
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The legacy Zhipu v3 surface is a proprietary prompt format. A Claude request
// used to hit an unconditional panic (a 500 with no usable message); it must be
// a client error that skips channel retries instead.
func TestConvertClaudeRequestRejectsWithoutPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiType: 4, UpstreamModelName: "chatglm_turbo"}}

	require.NotPanics(t, func() {
		_, err := (&Adaptor{}).ConvertClaudeRequest(c, info, &dto.ClaudeRequest{Model: "chatglm_turbo"})
		var apiErr *types.NewAPIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
		assert.True(t, types.IsSkipRetryError(apiErr))
		assert.Contains(t, apiErr.Error(), "/v1/messages")
	})
}

func TestConvertGeminiRequestRejectsLegacySurface(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/chatglm_turbo:generateContent", nil)

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiType: 4, UpstreamModelName: "chatglm_turbo"}}

	_, err := (&Adaptor{}).ConvertGeminiRequest(c, info, &dto.GeminiChatRequest{})
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.True(t, types.IsSkipRetryError(apiErr))
}
