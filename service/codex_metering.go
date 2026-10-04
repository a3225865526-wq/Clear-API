package service

import (
	"encoding/json"
	"reflect"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

const codexMeterKey = "private_codex_metering"

type codexMeterState struct {
	Final     *dto.CodexMetering
	AttemptID string
}

func EnableCodexMetering(c *gin.Context) {
	if _, ok := c.Get(codexMeterKey); !ok {
		c.Set(codexMeterKey, &codexMeterState{})
	}
}

// ObserveCodexMetering reads upstream bytes before DTO defaulting and before
// writing to the client. A later disconnect cannot erase an observed final.
func ObserveCodexMetering(c *gin.Context, info *relaycommon.RelayInfo, payload []byte, final bool) {
	var root map[string]json.RawMessage
	if common.Unmarshal(payload, &root) != nil {
		return
	}
	if raw := root["response"]; len(raw) > 0 {
		var response map[string]json.RawMessage
		if common.Unmarshal(raw, &response) == nil {
			root = response
		}
	}
	raw := root["usage"]
	var fields map[string]json.RawMessage
	_ = common.Unmarshal(raw, &fields)
	var m *dto.CodexMetering
	if extension, exists := fields["codex_metering"]; exists {
		EnableCodexMetering(c)
		m = &dto.CodexMetering{}
		if common.Unmarshal(extension, m) != nil || m.Source != "codex_upstream" {
			m = &dto.CodexMetering{Status: "unavailable", Source: "codex_upstream", Reason: "invalid_metering_envelope"}
		} else if m.Status == "exact" {
			m.Validate()
		} else {
			m.Status = "unavailable"
		}
	}
	if info != nil && info.GetChannelType() == constant.ChannelTypeCodex {
		EnableCodexMetering(c)
	}
	value, enabled := c.Get(codexMeterKey)
	if !enabled {
		return
	}
	state := value.(*codexMeterState)
	if m == nil && len(raw) == 0 {
		return
	}
	if m == nil {
		m = dto.ParseCodexMetering(raw)
	}
	if m.AttemptID != "" {
		if state.AttemptID != "" && state.AttemptID != m.AttemptID {
			state.Final = &dto.CodexMetering{Status: "unavailable", Source: "codex_upstream", Reason: "multiple_upstream_attempts"}
			c.Set("upstream_usage_uncertain", true)
			return
		}
		state.AttemptID = m.AttemptID
	}
	if extension := fields["codex_metering"]; len(extension) > 0 {
		final = m.Final
	}
	if !final {
		return
	}
	if state.Final == nil {
		state.Final = m
		if m.Status != "exact" {
			c.Set("upstream_usage_uncertain", true)
		}
		return
	}
	if !reflect.DeepEqual(state.Final, m) {
		state.Final = &dto.CodexMetering{Status: "unavailable", Source: "codex_upstream", Reason: "conflicting_final_usage"}
		c.Set("upstream_usage_uncertain", true)
	}
}

func CodexUsageForSettlement(c *gin.Context, info *relaycommon.RelayInfo) *dto.Usage {
	if c == nil {
		return nil
	}
	if info != nil && info.GetChannelType() == constant.ChannelTypeCodex {
		EnableCodexMetering(c)
	}
	value, ok := c.Get(codexMeterKey)
	if !ok {
		return nil
	}
	state := value.(*codexMeterState)
	m := state.Final
	if m == nil {
		m = &dto.CodexMetering{Status: "unavailable", Source: "codex_upstream", Reason: "final_usage_not_received", AttemptID: state.AttemptID}
	}
	u := &dto.Usage{CodexMetering: m, UsageSource: m.Source, UsageSemantic: dto.BillingUsageSemanticOpenAI}
	if m.Status == "exact" && m.Validate() {
		u.PromptTokens, u.InputTokens = int(*m.Input), int(*m.Input)
		u.CompletionTokens, u.OutputTokens = int(*m.Output), int(*m.Output)
		u.TotalTokens = int(*m.Total)
		u.PromptTokensDetails.CachedTokens = int(*m.Cached)
		if m.CacheWrite != nil {
			u.PromptTokensDetails.CacheWriteTokens = int(*m.CacheWrite)
		}
		u.CompletionTokenDetails.ReasoningTokens = int(*m.Reasoning)
	}
	return u
}

func IsResponsesUsageTerminal(kind string) bool {
	switch kind {
	case "response.completed", "response.done", "response.incomplete", "response.failed", "response.cancelled", "response.canceled":
		return true
	}
	return false
}

// Unknown provider consumption retains the reservation, never an exact zero
// bill. Existing durable review/recovery remains responsible for reconciliation.
func PostCodexUnavailableQuota(c *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage) {
	c.Set("upstream_usage_uncertain", true)
	status := "unavailable_no_reservation"
	charged := 0
	if session, ok := info.Billing.(*BillingSession); ok {
		status = "unavailable_reserved_for_review"
		if err := session.SettleUncertain(); err != nil {
			status = "unavailable_reconciliation_required"
			logger.LogError(c, "Codex usage unavailable; billing requires reconciliation")
		}
		charged = session.ChargedQuota()
		if !session.durable {
			status = "unavailable_retained_without_durable_review"
		}
	}
	other := model.NewLogOther()
	other.SetAdmin("codex_metering", usage.CodexMetering)
	other.SetAdmin("settlement_status", status)
	streamInterrupted := false
	if info.StreamStatus != nil {
		streamInterrupted = info.IsStream && info.StreamStatus.ResponseOutcome() != "completed"
	}
	other.SetAdmin("stream_interrupted", streamInterrupted)
	CaptureFinanceUsage(c, info, map[string]*string{}, "codex_authoritative_usage_unavailable")
	CaptureFinanceBilling(info, "text_settlement", &charged, info.BillingSource, status)
	model.RecordConsumeLog(c, info.UserId, model.RecordConsumeLogParams{ChannelId: info.ChannelId, ModelName: info.GetBillingModelName(), TokenId: info.TokenId, Quota: charged, IsStream: info.IsStream, Group: info.UsingGroup, Content: "Codex authoritative usage unavailable; retained reservation is not a token bill", Other: other})
}
