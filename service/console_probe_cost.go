package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrMonitorCostUnverified = errors.New("MONITOR_COST_UNVERIFIED")
	ErrMonitorCostExceeded   = errors.New("MONITOR_COST_EXCEEDED")
	ErrMonitorMetering       = errors.New("MONITOR_METERING_FAILED")
)

const consoleProbeCostVersion = "monitor-responses-v1"

// The execution snapshot uses an explicit upstream contract, never retail prices
// or RequestCostMicros as a substitute for the supplier's charging boundary.
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
	endpoint          string
	key               string
}

// ConsoleProbeCostContract is an operator-attested upstream charging contract.
// Rates are conservative whole micro-USD per token (rounded UP by the operator),
// including hidden tokens; the fixed fee includes all non-token charges. No
// default or inferred price is provided. Saving it never enables a monitor.
type ConsoleProbeCostContract struct {
	Source            string    `json:"source"`
	ValidFrom         time.Time `json:"valid_from"`
	ExpiresAt         time.Time `json:"expires_at"`
	InputTokenLimit   int64     `json:"input_token_limit"`
	InputMicros       int64     `json:"input_micros"`
	OutputMicros      int64     `json:"output_micros"`
	FixedFeeMicros    int64     `json:"fixed_fee_micros"`
	HardCeilingMicros int64     `json:"hard_ceiling_micros"`
	Endpoint          string    `json:"endpoint"`
	CredentialBinding string    `json:"credential_binding"`
}

func LoadConsoleProbeCostContract(ctx context.Context, db *gorm.DB, m model.ConsoleMonitor) (ConsoleProbeCostContract, error) {
	var option model.Option
	var contract ConsoleProbeCostContract
	if err := db.WithContext(ctx).Where(map[string]any{"key": fmt.Sprintf("console_monitor.cost.%d", m.ID)}).First(&option).Error; err != nil {
		return contract, ErrMonitorCostUnverified
	}
	if common.UnmarshalJsonStr(option.Value, &contract) != nil {
		return contract, ErrMonitorCostUnverified
	}
	return contract, nil
}

func consoleProbeContractPolicy(ctx context.Context, db *gorm.DB, m model.ConsoleMonitor, contract ConsoleProbeCostContract) (consoleProbeCostPolicy, error) {
	p := consoleProbeProductionCostPolicy()
	var channel model.Channel
	err := db.WithContext(ctx).First(&channel, m.ChannelID).Error
	if err != nil || channel.Status != common.ChannelStatusEnabled || channel.PoolChannelBound() || !slices.Contains(channel.GetModels(), m.Model) || channel.ModelMapping != nil && *channel.ModelMapping != "" {
		return p, model.ErrMonitorDenied
	}
	endpoint := strings.TrimRight(channel.GetBaseURL(), "/") + "/v1/responses"
	binding := fmt.Sprintf("%x", sha256.Sum256([]byte(endpoint+"\x00"+channel.Key)))
	u, parseErr := url.Parse(endpoint)
	if parseErr != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || contract.Endpoint != endpoint || contract.CredentialBinding != binding {
		return p, ErrMonitorCostUnverified
	}
	p.InputTokenLimit, p.HardCeilingMicros, p.PricingSource = contract.InputTokenLimit, contract.HardCeilingMicros, contract.Source
	p.endpoint, p.key = endpoint, channel.Key
	p.proof = &consoleProbeCostProof{Version: p.Version, Payload: consoleProbePayload, InputTokens: contract.InputTokenLimit, InputMicros: contract.InputMicros, OutputMicros: contract.OutputMicros, FixedFeeMicros: contract.FixedFeeMicros, ValidFrom: contract.ValidFrom, ExpiresAt: contract.ExpiresAt}
	cost, err := p.WorstCaseCost(m, consoleProbePayload, time.Now())
	if err != nil {
		return p, err
	}
	if cost > m.RequestCostMicros {
		return p, ErrMonitorCostExceeded
	}
	return p, nil
}

func AuthorizeConsoleProbeCost(ctx context.Context, db *gorm.DB, m model.ConsoleMonitor) error {
	contract, err := LoadConsoleProbeCostContract(ctx, db, m)
	if err != nil {
		return err
	}
	_, err = consoleProbeContractPolicy(ctx, db, m, contract)
	return err
}

func SaveConsoleProbeCostContract(ctx context.Context, db *gorm.DB, m model.ConsoleMonitor, contract ConsoleProbeCostContract) error {
	if len(contract.Source) == 0 || len(contract.Source) > 1024 || contract.InputTokenLimit < 1 || contract.InputTokenLimit > 4096 {
		return model.ErrMonitorInvalid
	}
	var channel model.Channel
	err := db.WithContext(ctx).First(&channel, m.ChannelID).Error
	if err != nil {
		return err
	}
	contract.Endpoint = strings.TrimRight(channel.GetBaseURL(), "/") + "/v1/responses"
	contract.CredentialBinding = fmt.Sprintf("%x", sha256.Sum256([]byte(contract.Endpoint+"\x00"+channel.Key)))
	if _, err := consoleProbeContractPolicy(ctx, db, m, contract); err != nil {
		return err
	}
	value, err := common.Marshal(contract)
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := model.LockConsoleMonitorPolicy(tx); err != nil {
			return err
		}
		var active int64
		if err := tx.Model(&model.ConsoleMonitorAttempt{}).Where("monitor_id = ? AND finished_at = 0", m.ID).Count(&active).Error; err != nil {
			return err
		}
		if active != 0 {
			return model.ErrMonitorLease
		}
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.AssignmentColumns([]string{"value"})}).Create(&model.Option{Key: fmt.Sprintf("console_monitor.cost.%d", m.ID), Value: string(value)}).Error
	})
}

// A proof must cover the exact fixed payload, including provider framing and
// hidden/reasoning tokens. Rates are conservative integer micro-USD per token;
// fee includes ALL non-token charges. Runtime requires an explicit valid contract.
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
