package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"net/url"
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

const consoleProbeCostVersion = "monitor-native-v2"

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
	payload           string
	contentType       string
	keyHeader         string
	native            *ConsoleProbeNativeRequest
	fixedRequest      bool
}

// ConsoleProbeCostContract is an operator-attested upstream charging contract.
// Rates are conservative whole micro-USD per token (rounded UP by the operator),
// including hidden tokens; the fixed fee includes all non-token charges. No
// default or inferred price is provided. Saving it never enables a monitor.
type ConsoleProbeCostContract struct {
	NativeRequest     *ConsoleProbeNativeRequest `json:"native_request,omitempty"`
	BillingUnit       string                     `json:"billing_unit,omitempty"` // "request" attests an exact all-inclusive per-request charge
	Source            string                     `json:"source"`
	ValidFrom         time.Time                  `json:"valid_from"`
	ExpiresAt         time.Time                  `json:"expires_at"`
	InputTokenLimit   int64                      `json:"input_token_limit"`
	InputMicros       int64                      `json:"input_micros"`
	OutputMicros      int64                      `json:"output_micros"`
	FixedFeeMicros    int64                      `json:"fixed_fee_micros"`
	HardCeilingMicros int64                      `json:"hard_ceiling_micros"`
	Endpoint          string                     `json:"endpoint"`
	CredentialBinding string                     `json:"credential_binding"`
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
	if err != nil || channel.Status != common.ChannelStatusEnabled || channel.PoolChannelBound() {
		return p, model.ErrMonitorDenied
	}
	path, payload, contentType, keyHeader, err := consoleProbeRequest(m, contract.NativeRequest)
	if err != nil {
		return p, ErrMonitorCostUnverified
	}
	endpoint := strings.TrimRight(channel.GetBaseURL(), "/") + path
	requestProfile, err := common.Marshal(contract.NativeRequest)
	if err != nil {
		return p, err
	}
	binding := fmt.Sprintf("%x", sha256.Sum256([]byte(endpoint+"\x00"+channel.Key+"\x00"+m.Model+"\x00"+m.Protocol+"\x00"+payload+"\x00"+string(requestProfile))))
	u, parseErr := url.Parse(endpoint)
	if parseErr != nil || u.User != nil || u.Fragment != "" || contract.Endpoint != endpoint || contract.CredentialBinding != binding {
		return p, ErrMonitorCostUnverified
	}
	p.ChannelID, p.Model, p.Protocol = m.ChannelID, m.Model, m.Protocol
	p.payload, p.contentType, p.keyHeader, p.native = payload, contentType, keyHeader, contract.NativeRequest
	p.fixedRequest = contract.BillingUnit == "request"
	if contract.NativeRequest != nil && !p.fixedRequest {
		return p, ErrMonitorCostUnverified
	}
	if p.fixedRequest && (contract.InputMicros != 0 || contract.OutputMicros != 0 || contract.FixedFeeMicros <= 0) {
		return p, ErrMonitorCostUnverified
	}
	p.InputTokenLimit, p.HardCeilingMicros, p.PricingSource = contract.InputTokenLimit, contract.HardCeilingMicros, contract.Source
	p.endpoint, p.key = endpoint, channel.Key
	p.proof = &consoleProbeCostProof{Version: p.Version, Payload: payload, InputTokens: contract.InputTokenLimit, InputMicros: contract.InputMicros, OutputMicros: contract.OutputMicros, FixedFeeMicros: contract.FixedFeeMicros, ValidFrom: contract.ValidFrom, ExpiresAt: contract.ExpiresAt}
	cost, err := p.WorstCaseCost(m, payload, time.Now())
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
	if len(contract.Source) == 0 || len(contract.Source) > 1024 || contract.InputTokenLimit < 0 || (contract.BillingUnit != "request" && contract.InputTokenLimit == 0) || contract.InputTokenLimit > 4096 || (contract.BillingUnit != "" && contract.BillingUnit != "request") {
		return model.ErrMonitorInvalid
	}
	var channel model.Channel
	err := db.WithContext(ctx).First(&channel, m.ChannelID).Error
	if err != nil {
		return err
	}
	path, payload, _, _, err := consoleProbeRequest(m, contract.NativeRequest)
	if err != nil {
		return err
	}
	contract.Endpoint = strings.TrimRight(channel.GetBaseURL(), "/") + path
	requestProfile, err := common.Marshal(contract.NativeRequest)
	if err != nil {
		return err
	}
	contract.CredentialBinding = fmt.Sprintf("%x", sha256.Sum256([]byte(contract.Endpoint+"\x00"+channel.Key+"\x00"+m.Model+"\x00"+m.Protocol+"\x00"+payload+"\x00"+string(requestProfile))))
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

func consoleProbeProductionCostPolicy() consoleProbeCostPolicy {
	return consoleProbeCostPolicy{
		InputTokenLimit: 64, OutputTokenLimit: 16,
		Version: consoleProbeCostVersion,
		// Ceiling/source/proof intentionally absent: no verified provider evidence.
	}
}

// WorstCaseCost uses the full input/output limits, not expected usage or cache
// discounts. Invalid/incomplete evidence and arithmetic overflow fail closed.
func (p consoleProbeCostPolicy) WorstCaseCost(m model.ConsoleMonitor, payload string, now time.Time) (int64, error) {
	proof := p.proof
	if m.TargetKind != "channel" || m.ChannelID <= 0 || m.Model == "" || m.Protocol == "" ||
		p.ChannelID != m.ChannelID || p.Model != m.Model || p.Protocol != m.Protocol ||
		p.Version != consoleProbeCostVersion || p.PricingSource == "" || p.HardCeilingMicros <= 0 ||
		(p.InputTokenLimit < 0 || !p.fixedRequest && p.InputTokenLimit == 0) || p.OutputTokenLimit != 16 || payload != p.payload || proof == nil {
		return 0, ErrMonitorCostUnverified
	}
	if proof.Version != p.Version || proof.Payload != payload || (proof.InputTokens < 0 || !p.fixedRequest && proof.InputTokens == 0) || proof.InputTokens > p.InputTokenLimit ||
		proof.InputMicros < 0 || proof.OutputMicros < 0 || proof.FixedFeeMicros < 0 ||
		(!p.fixedRequest && (proof.InputMicros == 0 || proof.OutputMicros == 0)) ||
		proof.ValidFrom.IsZero() || now.Before(proof.ValidFrom) || !now.Before(proof.ExpiresAt) {
		return 0, ErrMonitorCostUnverified
	}
	if (proof.InputMicros > 0 && p.InputTokenLimit > math.MaxInt64/proof.InputMicros) || (proof.OutputMicros > 0 && p.OutputTokenLimit > math.MaxInt64/proof.OutputMicros) {
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
