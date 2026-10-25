package helper

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// This is an enforced gateway output allowance, not a claim about a model's
// maximum capacity. Approved model profiles take precedence.
const defaultBillingOutputTokens = 4096

// PrepareBillingRequest uses each protocol's own DTO. It checks only facts
// that affect accounting; serialization and protocol validation stay with the
// adapters. TokenCountMeta, Usage/BillingUsage and BillingSession are the shared
// request, response and accounting contracts.
func PrepareBillingRequest(c *gin.Context, info *relaycommon.RelayInfo, defaultOutput bool) error {
	if info == nil || (!service.BillingProtectionEnabled() && !service.DurableWalletBillingEnabled()) {
		return nil
	}
	admission := *info
	if admission.ChannelMeta == nil {
		admission.InitChannelMeta(c)
	}
	model := admission.GetUpstreamModelName()
	if model == "" {
		model = info.GetOriginModelName()
	}
	policy := operation_setting.GetBillingReservePolicy()
	if info.RelayFormat == types.RelayFormatOpenAIRealtime {
		return fmt.Errorf("Realtime has no bounded per-turn reservation and is unavailable with durable billing")
	}
	profile, approved := operation_setting.ResolveReserveProfile(policy, admission.GetChannelID(), model)
	hidden := ""
	var cap **uint
	switch r := info.Request.(type) {
	case *dto.GeneralOpenAIRequest:
		if (info.RelayMode == relayconstant.RelayModeChatCompletions || info.RelayMode == relayconstant.RelayModeCompletions) && r.MaxTokens == nil && r.MaxCompletionTokens == nil {
			cap = &r.MaxTokens
			if info.RelayMode == relayconstant.RelayModeChatCompletions && dto.GetOpenAIChatCapabilities(r.Model, info.ReasoningEffort).UseMaxCompletionTokens {
				cap = &r.MaxCompletionTokens
			}
		}
	case *dto.OpenAIResponsesRequest:
		cap = &r.MaxOutputTokens
		if r.PreviousResponseID != "" {
			hidden = "previous_response_id"
		}
		if len(r.Conversation) > 0 && string(r.Conversation) != "null" {
			hidden = "conversation"
		}
		if len(r.Prompt) > 0 && string(r.Prompt) != "null" {
			hidden = "prompt reference"
		}
		var input any
		if err := common.Unmarshal(r.Input, &input); err != nil {
			return fmt.Errorf("invalid Responses input: %w", err)
		}
		if reference := responsesHiddenInput(input); reference != "" {
			hidden = reference
		}
	case *dto.OpenAIResponsesCompactionRequest:
		if r.PreviousResponseID != "" {
			hidden = "compact previous_response_id"
		}
		if !approved || profile.ModelOutputMax <= 0 {
			return fmt.Errorf("compact billing requires an approved model_output_max because the protocol has no output-limit field")
		}
		if profile.ModelOutputMax > maxTokensLimit {
			return fmt.Errorf("compact model_output_max exceeds the billing token limit")
		}
		info.BillingOutputLimit = int(profile.ModelOutputMax)
		var input any
		if err := common.Unmarshal(r.Input, &input); err != nil {
			return fmt.Errorf("invalid compact input: %w", err)
		}
		if reference := responsesHiddenInput(input); reference != "" {
			hidden = reference
		}
	case *dto.ClaudeRequest:
		cap = &r.MaxTokens
		if len(r.McpServers) > 0 && string(r.McpServers) != "null" && string(r.McpServers) != "[]" {
			return fmt.Errorf("billing cannot bound Claude server-side MCP tool charges")
		}
		if len(r.Container) > 0 && string(r.Container) != "null" {
			hidden = "Claude container state"
		}
	case *dto.AudioRequest:
		if info.RelayMode == relayconstant.RelayModeAudioSpeech {
			var limit uint
			if len(r.MaxNewTokens) > 0 {
				if err := common.Unmarshal(r.MaxNewTokens, &limit); err != nil || limit == 0 || limit > maxTokensLimit {
					return fmt.Errorf("audio max_new_tokens is invalid")
				}
			} else if approved && profile.ModelOutputMax > 0 && profile.ModelOutputMax <= maxTokensLimit {
				limit = uint(profile.ModelOutputMax)
			} else {
				return fmt.Errorf("audio speech billing requires a bounded max_new_tokens or an approved model_output_max")
			}
			info.BillingOutputLimit = int(limit)
			if len(r.RefAudio) > 0 && string(r.RefAudio) != "null" {
				hidden = "audio ref_audio"
			}
		}
	case *dto.GeminiChatRequest:
		cap = &r.GenerationConfig.MaxOutputTokens
		if r.CachedContent != "" {
			hidden = "Gemini cachedContent"
		}
		for _, content := range r.Contents {
			for _, part := range content.Parts {
				if part.FileData != nil {
					hidden = "Gemini fileData"
				}
			}
		}
		for i := range r.Requests {
			child := *info
			child.Request = &r.Requests[i]
			if len(r.Requests[i].Requests) > 0 {
				return fmt.Errorf("nested Gemini batches are not supported")
			}
			if err := PrepareBillingRequest(c, &child, defaultOutput); err != nil {
				return err
			}
		}
	}
	if hidden != "" && (policy.Mode != operation_setting.ReserveModeEnforce || !approved || profile.HiddenTotalMax <= 0) {
		return fmt.Errorf("billing cannot estimate %s without an enforced approved hidden-input reserve profile", hidden)
	}
	if defaultOutput && cap != nil && *cap == nil {
		limit := defaultBillingOutputTokens
		if _, claude := info.Request.(*dto.ClaudeRequest); claude {
			limit = model_setting.GetClaudeSettings().GetDefaultMaxTokens(model)
		}
		if approved && profile.ModelOutputMax > 0 {
			if profile.ModelOutputMax > maxTokensLimit {
				return fmt.Errorf("model_output_max exceeds the billing token limit")
			}
			limit = int(profile.ModelOutputMax)
		}
		if limit <= 0 || limit > maxTokensLimit {
			return fmt.Errorf("invalid server output allowance")
		}
		value := uint(limit)
		*cap = &value
	}
	return nil
}

// Inspect structural references, never words in user text or tool arguments.
func responsesHiddenInput(input any) string {
	switch value := input.(type) {
	case []any:
		for _, item := range value {
			if reason := responsesHiddenInput(item); reason != "" {
				return reason
			}
		}
	case map[string]any:
		kind, _ := value["type"].(string)
		if kind == "item_reference" {
			return "Responses item_reference"
		}
		for _, key := range []string{"encrypted_content", "file_id"} {
			if raw, present := value[key]; present && raw != nil && raw != "" {
				return "Responses " + key
			}
		}
		if reason := responsesHiddenInput(value["content"]); reason != "" {
			return reason
		}
	}
	return ""
}
