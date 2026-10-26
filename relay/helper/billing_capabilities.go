package helper

import (
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
)

// ValidateBillingCapabilities restricts only independently charged capabilities
// for which the host has no usage/price settlement. Ordinary tool declarations,
// references and native metered search/file-search are protocol data.
func ValidateBillingCapabilities(info *relaycommon.RelayInfo) error {
	if info == nil {
		return nil
	}
	if info.RelayFormat == types.RelayFormatOpenAIRealtime && service.DurableWalletBillingEnabled() {
		return fmt.Errorf("Realtime has no durable per-turn settlement implementation")
	}
	var raw json.RawMessage
	switch request := info.Request.(type) {
	case *dto.GeneralOpenAIRequest:
		var err error
		raw, err = common.Marshal(request.Tools)
		if err != nil {
			return err
		}
	case *dto.OpenAIResponsesRequest:
		raw = request.Tools
	case *dto.ClaudeRequest:
		if len(request.McpServers) > 0 && string(request.McpServers) != "null" && string(request.McpServers) != "[]" {
			return fmt.Errorf("unsupported billing capability: Claude server-side MCP charges")
		}
		var err error
		raw, err = common.Marshal(request.Tools)
		if err != nil {
			return err
		}
	case *dto.GeminiChatRequest:
		for i := range request.Requests {
			child := *info
			child.Request = &request.Requests[i]
			if err := ValidateBillingCapabilities(&child); err != nil {
				return err
			}
		}
		raw = request.Tools
	}
	var tools []struct {
		Type               string          `json:"type"`
		CodeExecution      json.RawMessage `json:"codeExecution"`
		CodeExecutionSnake json.RawMessage `json:"code_execution"`
		URLContext         json.RawMessage `json:"url_context"`
		URLContextCamel    json.RawMessage `json:"urlContext"`
	}
	if len(raw) > 0 {
		if err := common.Unmarshal(raw, &tools); err != nil {
			return err
		}
	}
	for _, tool := range tools {
		switch tool.Type {
		case "code_interpreter":
			return fmt.Errorf("unsupported billing capability: %s", tool.Type)
		}
		if len(tool.CodeExecution) > 0 || len(tool.CodeExecutionSnake) > 0 {
			return fmt.Errorf("unsupported billing capability: Gemini code execution")
		}
		if len(tool.URLContext) > 0 || len(tool.URLContextCamel) > 0 {
			return fmt.Errorf("unsupported billing capability: Gemini URL context")
		}
	}
	return nil
}

// validateTextQuantities applies the same arithmetic bounds to final wire DTOs
// that ingress validation applies to client DTOs. No output allowance is inferred.
func validateTextQuantities(request dto.Request) error {
	count := 1
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		if ExceedsMaxTokensLimit(r.MaxTokens, r.MaxCompletionTokens) {
			return fmt.Errorf("output token limit is out of range")
		}
		if r.N != nil {
			count = *r.N
		}
	case *dto.OpenAIResponsesRequest:
		if ExceedsMaxTokensLimit(r.MaxOutputTokens) {
			return fmt.Errorf("output token limit is out of range")
		}
	case *dto.ClaudeRequest:
		if ExceedsMaxTokensLimit(r.MaxTokens, r.MaxTokensToSample) {
			return fmt.Errorf("output token limit is out of range")
		}
	case *dto.GeminiChatRequest:
		if ExceedsMaxTokensLimit(r.GenerationConfig.MaxOutputTokens) {
			return fmt.Errorf("output token limit is out of range")
		}
		if r.GenerationConfig.CandidateCount != nil {
			count = *r.GenerationConfig.CandidateCount
		}
		for i := range r.Requests {
			if err := validateTextQuantities(&r.Requests[i]); err != nil {
				return err
			}
		}
	case *dto.AudioRequest:
		if len(r.MaxNewTokens) > 0 {
			var limit uint
			if err := common.Unmarshal(r.MaxNewTokens, &limit); err != nil || limit > maxTokensLimit {
				return fmt.Errorf("audio max_new_tokens is out of range")
			}
		}
	}
	if count < 1 || count > dto.MaxImageN {
		return fmt.Errorf("output candidate count is out of range")
	}
	return nil
}
