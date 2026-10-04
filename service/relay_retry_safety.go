package service

import (
	"encoding/json"
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// MarkRelayRetrySafety records only a boolean, never tool arguments or request
// contents. A tool continuation has no proven idempotency contract here.
// Tool declarations alone are not evidence that a tool has been executed.
func MarkRelayRetrySafety(c *gin.Context, request any) {
	payload, err := common.Marshal(request)
	if err != nil {
		c.Set("tool_chain_no_retry", true)
		return
	}
	var root map[string]json.RawMessage
	if common.Unmarshal(payload, &root) != nil {
		c.Set("tool_chain_no_retry", true)
		return
	}
	c.Set("tool_chain_no_retry", hasToolContinuation(root, 0))
}

func hasToolContinuation(node map[string]json.RawMessage, depth int) bool {
	if depth > 8 {
		return true
	}
	var role, kind string
	_ = common.Unmarshal(node["role"], &role)
	_ = common.Unmarshal(node["type"], &kind)
	if role == "tool" || role == "function" {
		return true
	}
	switch kind {
	case "tool_use", "tool_result", "function_call", "function_call_output", "custom_tool_call", "custom_tool_call_output":
		return true
	}
	for _, key := range []string{"tool_calls", "function_call", "functionCall", "functionResponse"} {
		raw := node[key]
		if len(raw) > 0 && string(raw) != "null" && string(raw) != "[]" && string(raw) != "{}" {
			return true
		}
	}
	for _, key := range []string{"messages", "input", "contents", "content", "parts"} {
		var items []json.RawMessage
		if common.Unmarshal(node[key], &items) != nil {
			continue
		}
		for _, item := range items {
			var child map[string]json.RawMessage
			if common.Unmarshal(item, &child) == nil && hasToolContinuation(child, depth+1) {
				return true
			}
		}
	}
	return false
}
