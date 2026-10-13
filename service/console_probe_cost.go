package service

import (
	"errors"
	"math"
	"time"

	"github.com/QuantumNous/new-api/model"
)

var (
	ErrMonitorCostUnverified = errors.New("MONITOR_COST_UNVERIFIED")
	ErrMonitorCostExceeded   = errors.New("MONITOR_COST_EXCEEDED")
)

const consoleProbeCostVersion = "monitor-responses-v1"

// Values are returned by value; there is no registry, setter, configuration or
// database pricing source. An absent proof is never replaced with retail prices
// or RequestCostMicros. This policy does not authorize runtime activation.
type consoleProbeCostPolicy struct {
	ChannelID         int
	Model             string
	Protocol          string
	InputTokenLimit   int64
	OutputTokenLimit  int64
	HardCeilingMicros int64
	PricingSource     string
	Version           string
	proof             *consoleProbeCostProof
}

// A proof must cover the exact fixed payload, including provider framing and
// hidden/reasoning tokens. Rates are conservative integer micro-USD per token;
// fee includes ALL non-token charges. No proof exists for the production target.
type consoleProbeCostProof struct {
	Version        string
	Payload        string
	InputTokens    int64
	InputMicros    int64
	OutputMicros   int64
	FixedFeeMicros int64
	ValidFrom      time.Time
	ExpiresAt      time.Time
}

const consoleProbePayload = `{"model":"gpt-6-luna","input":"monitor probe","stream":false,"max_output_tokens":16,"store":false}`

func consoleProbeProductionCostPolicy() consoleProbeCostPolicy {
	return consoleProbeCostPolicy{
		ChannelID: 1, Model: "gpt-6-luna", Protocol: "openai_responses",
		InputTokenLimit: 64, OutputTokenLimit: 16,
		Version: consoleProbeCostVersion,
		// Ceiling/source/proof intentionally absent: no verified provider evidence.
	}
}

// WorstCaseCost uses the full input/output limits, not expected usage or cache
// discounts. Invalid/incomplete evidence and arithmetic overflow fail closed.
func (p consoleProbeCostPolicy) WorstCaseCost(m model.ConsoleMonitor, payload string, now time.Time) (int64, error) {
	proof := p.proof
	if m.TargetKind != "channel" || m.TargetRef != "1" || m.ChannelID != 1 || m.Model != "gpt-6-luna" || m.Protocol != "openai_responses" ||
		p.ChannelID != m.ChannelID || p.Model != m.Model || p.Protocol != m.Protocol ||
		p.Version != consoleProbeCostVersion || p.PricingSource == "" || p.HardCeilingMicros <= 0 ||
		p.InputTokenLimit <= 0 || p.OutputTokenLimit != 16 || payload != consoleProbePayload || proof == nil {
		return 0, ErrMonitorCostUnverified
	}
	if proof.Version != p.Version || proof.Payload != payload || proof.InputTokens <= 0 || proof.InputTokens > p.InputTokenLimit ||
		proof.InputMicros <= 0 || proof.OutputMicros <= 0 || proof.FixedFeeMicros < 0 ||
		proof.ValidFrom.IsZero() || now.Before(proof.ValidFrom) || !now.Before(proof.ExpiresAt) {
		return 0, ErrMonitorCostUnverified
	}
	if p.InputTokenLimit > math.MaxInt64/proof.InputMicros || p.OutputTokenLimit > math.MaxInt64/proof.OutputMicros {
		return 0, ErrMonitorCostUnverified
	}
	input := p.InputTokenLimit * proof.InputMicros
	output := p.OutputTokenLimit * proof.OutputMicros
	if input > math.MaxInt64-output || input+output > math.MaxInt64-proof.FixedFeeMicros {
		return 0, ErrMonitorCostUnverified
	}
	cost := input + output + proof.FixedFeeMicros
	if cost > p.HardCeilingMicros {
		return cost, ErrMonitorCostExceeded
	}
	return cost, nil
}
