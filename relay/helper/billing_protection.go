package helper

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"os"
	"reflect"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
)

// Only the bounded, single-output OpenAI text path is currently admitted.
// Tool surcharges, multimodal quantities, provider-specific passthrough fields
// and arbitrary expressions cannot be bounded by evaluating one sample.
func protectedTextLimit(info *relaycommon.RelayInfo) (int, error) {
	if info != nil && info.RelayFormat == types.RelayFormatOpenAIResponses {
		return protectedResponsesTextLimit(info)
	}
	if !constant.CountToken || info == nil || info.RelayFormat != types.RelayFormatOpenAI {
		return 0, errors.New("protected billing requires token counting and an OpenAI text request")
	}
	if info.TokenGroup == "auto" {
		return 0, errors.New("protected billing requires a fixed group")
	}
	r, ok := info.Request.(*dto.GeneralOpenAIRequest)
	if !ok || len(r.Messages) == 0 {
		return 0, errors.New("protected billing requires chat messages")
	}
	if strings.Contains(r.Model, "search-preview") || strings.Contains(info.GetBillingModelName(), "search-preview") {
		return 0, errors.New("protected billing does not admit models with implicit search fees")
	}
	if r.N != nil && *r.N != 1 {
		return 0, errors.New("protected billing requires n=1")
	}
	limit := uint(0)
	if r.MaxTokens != nil {
		limit = *r.MaxTokens
	}
	if r.MaxCompletionTokens != nil {
		if limit != 0 && limit != *r.MaxCompletionTokens {
			return 0, errors.New("conflicting output token limits")
		}
		limit = *r.MaxCompletionTokens
	}
	if limit == 0 || limit > maxTokensLimit {
		return 0, errors.New("protected billing requires an explicit bounded output token limit")
	}
	body, err := json.Marshal(r)
	if err != nil {
		return 0, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return 0, err
	}
	allowed := map[string]bool{"model": true, "messages": true, "stream": true, "stream_options": true, "max_tokens": true, "max_completion_tokens": true, "n": true, "temperature": true, "top_p": true, "stop": true, "frequency_penalty": true, "presence_penalty": true, "seed": true, "user": true}
	for key := range fields {
		if !allowed[key] {
			return 0, errors.New("protected billing does not admit optional provider, tool or multimodal fields")
		}
	}
	var messages []map[string]json.RawMessage
	if err := json.Unmarshal(fields["messages"], &messages); err != nil {
		return 0, err
	}
	for _, message := range messages {
		for key := range message {
			if key != "role" && key != "content" && key != "name" {
				return 0, errors.New("protected billing requires plain text messages")
			}
		}
		var content string
		if err := json.Unmarshal(message["content"], &content); err != nil {
			return 0, errors.New("protected billing requires plain text message content")
		}
	}
	if info.ChannelMeta != nil && (len(info.ChannelMeta.ParamOverride) != 0 || len(info.ChannelMeta.HeadersOverride) != 0) {
		return 0, errors.New("protected billing does not admit channel parameter or header overrides")
	}
	return int(limit), nil
}

// Native Responses admission is limited to bounded, self-contained text SSE.
// Keep the Chat contract above and the reservation arithmetic below unchanged.
func protectedResponsesTextLimit(info *relaycommon.RelayInfo) (int, error) {
	if !constant.CountToken {
		return 0, errors.New("protected billing requires token counting")
	}
	if info.TokenGroup == "auto" {
		return 0, errors.New("protected billing requires a fixed group")
	}
	r, ok := info.Request.(*dto.OpenAIResponsesRequest)
	if !ok || r == nil {
		return 0, errors.New("protected billing requires a native Responses request")
	}
	if strings.Contains(r.Model, "search-preview") || strings.Contains(info.GetBillingModelName(), "search-preview") {
		return 0, errors.New("protected billing does not admit models with implicit search fees")
	}
	// Bypass MarshalJSON field filtering so an unsupported supplied field cannot
	// disappear before the protection allowlist checks it.
	type responsesFields dto.OpenAIResponsesRequest
	body, err := common.Marshal((*responsesFields)(r))
	if err != nil {
		return 0, err
	}
	if _, err := validateProtectedResponsesText(body); err != nil {
		return 0, err
	}
	if info.ChannelMeta != nil && (len(info.ChannelMeta.ParamOverride) != 0 || len(info.ChannelMeta.HeadersOverride) != 0) {
		return 0, errors.New("protected billing does not admit channel parameter or header overrides")
	}
	return int(*r.MaxOutputTokens), nil
}

func validateProtectedResponsesText(body []byte) (*dto.OpenAIResponsesRequest, error) {
	var fields map[string]common.RawMessage
	if err := common.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	for key := range fields {
		switch key {
		case "model", "input", "instructions", "max_output_tokens", "stream":
		default:
			return nil, errors.New("protected billing does not admit optional provider, tool or reference fields")
		}
	}
	var r dto.OpenAIResponsesRequest
	if err := common.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	if r.Stream == nil || !*r.Stream {
		return nil, errors.New("protected billing requires Responses SSE")
	}
	if r.MaxOutputTokens == nil || *r.MaxOutputTokens == 0 || *r.MaxOutputTokens > maxTokensLimit {
		return nil, errors.New("protected billing requires an explicit bounded output token limit")
	}
	if instructions, present := fields["instructions"]; present && common.GetJsonType(instructions) != "string" {
		return nil, errors.New("protected billing requires plain text instructions")
	}
	if common.GetJsonType(r.Input) == "string" {
		var text string
		if err := common.Unmarshal(r.Input, &text); err != nil || strings.TrimSpace(text) == "" {
			return nil, errors.New("protected billing requires nonempty text input")
		}
		return &r, nil
	}
	var messages []map[string]common.RawMessage
	if err := common.Unmarshal(r.Input, &messages); err != nil || len(messages) == 0 {
		return nil, errors.New("protected billing requires self-contained text input")
	}
	for _, message := range messages {
		for key := range message {
			if key != "type" && key != "role" && key != "content" {
				return nil, errors.New("protected billing does not admit input references or tool calls")
			}
		}
		var role, kind string
		if err := common.Unmarshal(message["role"], &role); err != nil || (role != "user" && role != "assistant" && role != "system" && role != "developer") {
			return nil, errors.New("protected billing requires plain text messages")
		}
		if raw, present := message["type"]; present {
			if err := common.Unmarshal(raw, &kind); err != nil || kind != "message" {
				return nil, errors.New("protected billing does not admit input references or tool calls")
			}
		}
		content := message["content"]
		if common.GetJsonType(content) == "string" {
			continue
		}
		var parts []map[string]common.RawMessage
		if err := common.Unmarshal(content, &parts); err != nil || len(parts) == 0 {
			return nil, errors.New("protected billing requires plain text message content")
		}
		for _, part := range parts {
			var partType, text string
			if len(part) != 2 || common.Unmarshal(part["type"], &partType) != nil || partType != "input_text" || common.GetJsonType(part["text"]) != "string" || common.Unmarshal(part["text"], &text) != nil {
				return nil, errors.New("protected billing does not admit multimodal input or references")
			}
		}
	}
	return &r, nil
}

func protectTextReservation(info *relaycommon.RelayInfo, price *hosttypes.PriceData, promptTokens, outputTokens int) error {
	if promptTokens <= 0 || outputTokens <= 0 || len(price.OtherRatios()) != 0 {
		return errors.New("protected billing cannot establish a text budget")
	}
	if !price.UsePrice && !price.FreeModel {
		for _, ratio := range []float64{price.ModelRatio, price.CompletionRatio, price.GroupRatioInfo.GroupRatio, price.CacheRatio, price.CacheCreationRatio, price.CacheCreation5mRatio, price.CacheCreation1hRatio} {
			if math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 {
				return errors.New("invalid protected billing ratio")
			}
		}
		// Cover the most expensive supported input category rather than assuming
		// every input token is uncached. The existing input multiplier remains.
		inputRatio := max(1.0, price.CacheRatio, price.CacheCreationRatio, price.CacheCreation5mRatio, price.CacheCreation1hRatio)
		budget := (float64(promptTokens)*inputRatio + float64(outputTokens)*price.CompletionRatio) * price.ModelRatio * price.GroupRatioInfo.GroupRatio
		if math.IsNaN(budget) || math.IsInf(budget, 0) || budget < 0 {
			return errors.New("invalid protected text budget")
		}
		quota, err := common.QuotaFromFloatStrict(math.Ceil(budget))
		if err != nil {
			return err
		}
		price.QuotaToPreConsume = max(price.QuotaToPreConsume, quota)
	}
	info.ProtectedTextBudget = true
	return nil
}

func billingProtectionEnabled() bool { return os.Getenv("CLEARAPI_BILLING_PROTECTION") == "true" }

// ValidateProtectedTextChannel runs for every selected attempt, after channel
// metadata is initialized. Raw passthrough and conversion cannot bypass the
// quantities validated before reservation.
func ValidateProtectedTextChannel(info *relaycommon.RelayInfo) error {
	if !billingProtectionEnabled() {
		return nil
	}
	if info == nil || !info.ProtectedTextBudget || info.ChannelMeta == nil {
		return errors.New("protected text budget is missing")
	}
	if info.ChannelType != constant.ChannelTypeOpenAI || len(info.ParamOverride) != 0 || len(info.HeadersOverride) != 0 || info.UseRuntimeHeadersOverride || info.ChannelSetting.PassThroughBodyEnabled || info.ChannelSetting.SystemPrompt != "" || model_setting.GetGlobalSettings().PassThroughRequestEnabled {
		return errors.New("protected billing requires a direct OpenAI channel without overrides, passthrough or injected prompts")
	}
	if info.TieredBillingSnapshot != nil {
		if !info.ProtectedTieredBudget {
			return errors.New("protected expression budget is missing")
		}
		return service.ValidateProtectedTieredSnapshot(info)
	}
	return nil
}

func ValidateProtectedTextBody(info *relaycommon.RelayInfo, body []byte) error {
	if !billingProtectionEnabled() {
		return nil
	}
	if info != nil && info.RelayFormat == types.RelayFormatOpenAIResponses {
		originalLimit, err := protectedTextLimit(info)
		if err != nil {
			return err
		}
		outbound, err := validateProtectedResponsesText(body)
		if err != nil {
			return err
		}
		original := info.Request.(*dto.OpenAIResponsesRequest)
		effective := *info
		effective.Request = outbound
		limit, err := protectedTextLimit(&effective)
		if err != nil {
			return err
		}
		var originalInput, outboundInput, originalInstructions, outboundInstructions any
		if err := common.Unmarshal(original.Input, &originalInput); err != nil {
			return err
		}
		if err := common.Unmarshal(outbound.Input, &outboundInput); err != nil {
			return err
		}
		if len(original.Instructions) > 0 {
			if err := common.Unmarshal(original.Instructions, &originalInstructions); err != nil {
				return err
			}
		}
		if len(outbound.Instructions) > 0 {
			if err := common.Unmarshal(outbound.Instructions, &outboundInstructions); err != nil {
				return err
			}
		}
		if limit != originalLimit || !reflect.DeepEqual(originalInput, outboundInput) || !reflect.DeepEqual(originalInstructions, outboundInstructions) {
			return errors.New("outbound text or output budget changed after reservation")
		}
		return nil
	}
	var outbound dto.GeneralOpenAIRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&outbound); err != nil {
		return errors.New("protected billing cannot verify the outbound request")
	}
	originalLimit, err := protectedTextLimit(info)
	if err != nil {
		return err
	}
	effective := *info
	effective.Request = &outbound
	limit, err := protectedTextLimit(&effective)
	if err != nil {
		return err
	}
	if limit != originalLimit || !reflect.DeepEqual(outbound.Messages, info.Request.(*dto.GeneralOpenAIRequest).Messages) {
		return errors.New("outbound text or output budget changed after reservation")
	}
	return nil
}
