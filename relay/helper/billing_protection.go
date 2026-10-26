package helper

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// PrepareBillingRequest keeps only features without a supported durable
// settlement path out of the wallet ledger. References, cache state and output
// limits are protocol data; their actual token usage is settled normally.
func PrepareBillingRequest(_ *gin.Context, info *relaycommon.RelayInfo, _ bool) error {
	if info == nil || (!service.BillingProtectionEnabled() && !service.DurableWalletBillingEnabled()) {
		return nil
	}
	if info.RelayFormat == types.RelayFormatOpenAIRealtime {
		return fmt.Errorf("Realtime has no durable per-turn settlement implementation")
	}
	if r, ok := info.Request.(*dto.ClaudeRequest); ok && len(r.McpServers) > 0 && string(r.McpServers) != "null" && string(r.McpServers) != "[]" {
		return fmt.Errorf("billing cannot meter Claude server-side MCP tool charges")
	}
	if r, ok := info.Request.(*dto.AudioRequest); ok && len(r.MaxNewTokens) > 0 {
		var limit uint
		if err := common.Unmarshal(r.MaxNewTokens, &limit); err != nil || limit > maxTokensLimit {
			return fmt.Errorf("audio max_new_tokens is out of range")
		}
	}
	return nil
}
