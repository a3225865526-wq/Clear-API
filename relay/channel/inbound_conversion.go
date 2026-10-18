package channel

import (
	"errors"
	"fmt"
	"net/http"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// Chat-compatible channels expose a single chat-completions route for text, but
// the gateway also accepts Gemini generateContent requests on every text
// channel. Such a request has to be converted to a chat request before the
// channel-specific adaptor applies its provider rules; the shared OpenAI
// response handlers then render the upstream chat body back as Gemini.
//
// This wrapper keeps that step identical across chat-compatible adaptors
// instead of each one re-deriving the relaykit conversion path.
func ConvertGeminiRequestToChat(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (*dto.GeneralOpenAIRequest, error) {
	if c == nil {
		return nil, errors.New("request context is required for inbound conversion")
	}
	result, err := service.ConvertRequest(c, info, types.RelayFormatOpenAI, request)
	if err != nil {
		return nil, err
	}
	chatRequest, ok := result.Value.(*dto.GeneralOpenAIRequest)
	if !ok || chatRequest == nil {
		return nil, fmt.Errorf("inbound conversion produced %T, expected an OpenAI chat completions request", result.Value)
	}
	// Upstream now receives chat completions, so record that protocol and ask
	// for usage in the stream exactly like a native chat request does.
	// SupportStreamOptions is promoted from the embedded channel meta, which
	// this helper must not assume the caller has populated.
	relaycommon.AppendRequestConversionFromRequest(info, chatRequest)
	if info != nil && info.ChannelMeta != nil && info.ChannelMeta.SupportStreamOptions && info.IsStream {
		chatRequest.StreamOptions = &dto.StreamOptions{IncludeUsage: true}
	}
	return chatRequest, nil
}

// NewUnsupportedEndpointError reports an inbound protocol a channel type cannot
// serve. It is a client error that skips channel retries: every channel of that
// type refuses the same endpoint, so a retryable 500 would only burn attempts on
// unrelated channels and still fail for the client.
func NewUnsupportedEndpointError(c *gin.Context, info *relaycommon.RelayInfo, detail string) *types.NewAPIError {
	path := "this endpoint"
	if c != nil && c.Request != nil && c.Request.URL != nil && c.Request.URL.Path != "" {
		path = c.Request.URL.Path
	}
	apiType := -1
	if info != nil {
		apiType = info.ApiType
	}
	return types.NewErrorWithStatusCode(
		fmt.Errorf("channel api type %d cannot serve %s: %s", apiType, path, detail),
		types.ErrorCodeInvalidRequest,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	)
}
