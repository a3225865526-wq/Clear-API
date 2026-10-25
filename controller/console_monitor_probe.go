package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const (
	consoleProbeModel    = "gpt-6-luna"
	consoleProbeProtocol = "openai_responses"
	consoleProbeMaxBody  = 1 << 20
)

// channelConsoleProbeAdapter is deliberately separate from channel_test. It
// sends one fixed, non-streaming Responses request through the selected
// channel, without user context, routing fallback, retry, or billing.
type channelConsoleProbeAdapter struct{}

func (channelConsoleProbeAdapter) Authorize(ctx context.Context, m model.ConsoleMonitor) error {
	if m.TargetKind != "channel" || m.Protocol != consoleProbeProtocol || m.ChannelID != 1 || m.Model != consoleProbeModel {
		return model.ErrMonitorDenied
	}
	if !m.Allowlisted || !m.Published || !m.Enabled || m.Archived {
		return model.ErrMonitorPaused
	}
	channel, err := model.GetChannelById(m.ChannelID, true)
	if err != nil {
		return model.ErrMonitorDenied
	}
	if channel.Status != common.ChannelStatusEnabled || channel.PoolChannelBound() || !slices.Contains(channel.GetModels(), m.Model) {
		return model.ErrMonitorDenied
	}
	// The executor contract requires a separately enforceable spending ceiling.
	// A token limit and an operator's reserved cost are not proof that this
	// compatible upstream enforces that ceiling. Keep transport unavailable
	// until that provider contract can be validated without weakening admission.
	return model.ErrMonitorIntegration
}

func (a channelConsoleProbeAdapter) Probe(ctx context.Context, m model.ConsoleMonitor, _ int64) (service.ConsoleProbeOutcome, error) {
	if err := a.Authorize(ctx, m); err != nil {
		return service.ConsoleProbeOutcome{}, err
	}
	channel, err := model.GetChannelById(m.ChannelID, true)
	if err != nil {
		return service.ConsoleProbeOutcome{}, model.ErrMonitorDenied
	}
	request := &dto.OpenAIResponsesRequest{
		Model: m.Model,
		Input: json.RawMessage(`"monitor probe"`),
	}
	stream := false
	maxOutputTokens := uint(16)
	request.Stream = &stream
	request.MaxOutputTokens = &maxOutputTokens
	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("channel", channel.Type)
	c.Set("base_url", channel.GetBaseURL())
	if apiErr := middleware.SetupContextForSelectedChannel(c, channel, m.Model); apiErr != nil {
		return service.ConsoleProbeOutcome{}, model.ErrMonitorDenied
	}

	info := relaycommon.GenRelayInfoResponses(c, request)
	info.IsStream = false
	info.InitChannelMeta(c)
	apiType, ok := common.ChannelType2APIType(channel.Type)
	if !ok {
		return service.ConsoleProbeOutcome{}, model.ErrMonitorIntegration
	}
	if apiType != constant.APITypeOpenAI {
		return service.ConsoleProbeOutcome{}, model.ErrMonitorIntegration
	}
	adaptor := relay.GetAdaptor(apiType)
	if adaptor == nil {
		return service.ConsoleProbeOutcome{}, model.ErrMonitorIntegration
	}
	adaptor.Init(info)
	converted, err := adaptor.ConvertOpenAIResponsesRequest(c, info, *request)
	if err != nil {
		return service.ConsoleProbeOutcome{}, model.ErrMonitorIntegration
	}
	body, err := common.Marshal(converted)
	if err != nil || len(body) > consoleProbeMaxBody {
		return service.ConsoleProbeOutcome{}, model.ErrMonitorIntegration
	}
	response, err := adaptor.DoRequest(c, info, bytes.NewReader(body))
	if err != nil {
		return service.ConsoleProbeOutcome{}, err
	}
	httpResponse, ok := response.(*http.Response)
	if !ok || httpResponse == nil || httpResponse.Body == nil {
		return service.ConsoleProbeOutcome{}, fmt.Errorf("invalid probe response")
	}
	defer httpResponse.Body.Close()
	if httpResponse.StatusCode < http.StatusOK || httpResponse.StatusCode >= http.StatusMultipleChoices {
		return service.ConsoleProbeOutcome{}, fmt.Errorf("probe upstream status")
	}
	responseBody, err := io.ReadAll(io.LimitReader(httpResponse.Body, consoleProbeMaxBody+1))
	if err != nil || len(responseBody) > consoleProbeMaxBody {
		return service.ConsoleProbeOutcome{}, fmt.Errorf("probe response body")
	}
	var responseDTO dto.OpenAIResponsesResponse
	if err := common.Unmarshal(responseBody, &responseDTO); err != nil {
		return service.ConsoleProbeOutcome{}, fmt.Errorf("probe response json")
	}
	if responseDTO.GetOpenAIError() != nil {
		return service.ConsoleProbeOutcome{}, fmt.Errorf("probe upstream error")
	}
	var status string
	if common.Unmarshal(responseDTO.Status, &status) != nil || status != "completed" {
		return service.ConsoleProbeOutcome{}, fmt.Errorf("probe response not completed")
	}
	return service.ConsoleProbeOutcome{Status: "normal", SourceChannelID: m.ChannelID}, nil
}
