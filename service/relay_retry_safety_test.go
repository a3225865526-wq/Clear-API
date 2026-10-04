package service

import (
	"encoding/json"
	"errors"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestUsageRetryStopsAfterClientOutput(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Writer.WriteString("data: synthetic partial output\n\n")
	decision := DecideRelayRetry(c, types.NewError(errors.New("synthetic channel error"), types.ErrorCodeChannelNoAvailableKey), 2)
	if decision.Action != "stop" || decision.Reason != "response_started" {
		t.Fatal("stream restarted", decision)
	}
}

func TestUsageToolChainRetrySafetyPreservesRequests(t *testing.T) {
	for _, payload := range []string{
		`{"messages":[{"role":"tool","content":"fixed sample"}]}`,
		`{"messages":[{"role":"assistant","tool_calls":[{"id":"fixed"}]}]}`,
		`{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"fixed"}]}]}`,
		`{"input":[{"type":"function_call_output","call_id":"fixed","output":"fixed"}]}`,
		`{"contents":[{"parts":[{"functionResponse":{"name":"fixed"}}]}]}`,
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		request := json.RawMessage(payload)
		MarkRelayRetrySafety(c, request)
		decision := DecideRelayRetry(c, types.NewError(errors.New("synthetic network error"), types.ErrorCodeChannelNoAvailableKey), 2)
		if decision.Action != "stop" || decision.Reason != "tool_chain_not_idempotent" {
			t.Fatal(payload, decision)
		}
		if string(request) != payload {
			t.Fatal("request fields rewritten")
		}
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	MarkRelayRetrySafety(c, json.RawMessage(`{"tools":[{"type":"function"}],"messages":[{"role":"user","content":"fixed"}]}`))
	if c.GetBool("tool_chain_no_retry") {
		t.Fatal("tool declaration misidentified as executed chain")
	}
}
