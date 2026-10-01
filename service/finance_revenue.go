package service

import (
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
)

// Preview facts describe one wallet, in authoritative event order. ValueCNY is
// the frozen cash-backed value of the entire credited quota, including package
// gifts. Crediting a redeemed lot is NOT another cash receipt.
// Standalone gifts have known zero value; historical unknowns have nil value.
// This calculator neither reads nor mutates the application's wallet.
type FinanceRevenueEvent struct {
	Key      string  `json:"key"`
	At       int64   `json:"at"`
	Kind     string  `json:"kind"`
	Quota    int64   `json:"quota"`
	Source   string  `json:"source,omitempty"`
	ValueCNY *string `json:"value_cny,omitempty"`
	Reverses string  `json:"reverses,omitempty"`
}

type FinanceRevenuePreview struct {
	Events             []FinanceRevenueEvent `json:"events"`
	NominalCNYPerQuota *string               `json:"nominal_cny_per_quota,omitempty"`
}

type FinanceRevenuePart struct {
	LotKey   string  `json:"lot_key"`
	Source   string  `json:"source"`
	Quota    int64   `json:"quota"`
	ValueCNY *string `json:"value_cny"`
}

type FinanceRevenueAllocation struct {
	EventKey string               `json:"event_key"`
	Kind     string               `json:"kind"`
	Reverses string               `json:"reverses,omitempty"`
	Parts    []FinanceRevenuePart `json:"parts"`
}

type FinanceRevenueResult struct {
	Simulation           bool                       `json:"simulation"`
	Status               string                     `json:"status"`
	KnownBackingValueCNY string                     `json:"known_credit_backing_cny"`
	BackingValueCNY      *string                    `json:"credit_backing_cny"`
	KnownConsumptionCNY  string                     `json:"known_consumption_cny"`
	ConsumptionCNY       *string                    `json:"consumption_cny"`
	UnknownConsumedQuota int64                      `json:"unknown_consumed_quota"`
	NominalUnknownCNY    *string                    `json:"nominal_unknown_consumption_cny"`
	RemainingQuota       int64                      `json:"remaining_quota"`
	Lots                 []FinanceRevenuePart       `json:"remaining_lots"`
	Allocations          []FinanceRevenueAllocation `json:"allocations"`
	Warnings             []string                   `json:"warnings"`
}

type financeRevenueLot struct {
	event      FinanceRevenueEvent
	used       int64
	value      decimal.Decimal
	recognized decimal.Decimal
}

// Cumulative allocation preserves the complete lot value when a quota lot is
// exhausted, even when its unit value is a repeating decimal. Reversals use
// the original allocation amount, never today's prices or a new FIFO pass.
func (lot financeRevenueLot) consumedValue(quota int64) decimal.Decimal {
	return lot.value.Mul(decimal.NewFromInt(quota)).DivRound(decimal.NewFromInt(lot.event.Quota), 12)
}

// Tracks how much of one consumption has already been reversed so several
// partial reversals stay within the original quota and sum to its value.
type financeConsumptionState struct {
	index       int
	reversed    int64
	reversedLot map[string]int64
}

func PreviewFinanceRevenue(input FinanceRevenuePreview) (*FinanceRevenueResult, error) {
	if len(input.Events) == 0 || len(input.Events) > 512 {
		return nil, model.ErrFinanceInvalid
	}
	var nominal *decimal.Decimal
	if input.NominalCNYPerQuota != nil {
		rate, err := financeDecimal(*input.NominalCNYPerQuota, false)
		if err != nil || !rate.IsPositive() {
			return nil, model.ErrFinanceInvalid
		}
		nominal = &rate
	}
	result := &FinanceRevenueResult{Simulation: true, Status: "known", Lots: []FinanceRevenuePart{}, Allocations: []FinanceRevenueAllocation{}, Warnings: []string{"simulation_only", "cash_receipts_not_connected", "cash_refunds_separate_cash_account", "subscription_revenue_not_supported"}}
	lots := []financeRevenueLot{}
	lotByKey := map[string]int{}
	consumptionByKey := map[string]*financeConsumptionState{}
	seen := map[string]bool{}
	cash, revenue := decimal.Zero, decimal.Zero
	unknownCash := false
	var lastAt int64
	for _, event := range input.Events {
		if !financeKeyPattern.MatchString(event.Key) || seen[event.Key] || event.At <= 0 || event.At < lastAt {
			return nil, model.ErrFinanceInvalid
		}
		seen[event.Key], lastAt = true, event.At
		switch event.Kind {
		case "credit":
			if event.Quota <= 0 || event.Quota > common.MaxWalletQuota || !slices.Contains([]string{"paid", "gift", "unknown"}, event.Source) || event.Reverses != "" || event.Quota > common.MaxWalletQuota-result.RemainingQuota {
				return nil, model.ErrFinanceInvalid
			}
			lot := financeRevenueLot{event: event}
			if event.Source == "unknown" {
				if event.ValueCNY != nil {
					return nil, model.ErrFinanceInvalid
				}
				unknownCash = true
			} else {
				if event.ValueCNY == nil {
					return nil, model.ErrFinanceInvalid
				}
				value, err := financeDecimal(*event.ValueCNY, false)
				if err != nil || (event.Source == "gift" && !value.IsZero()) || (event.Source == "paid" && !value.IsPositive()) {
					return nil, model.ErrFinanceInvalid
				}
				lot.value = value
				cash = cash.Add(value)
			}
			lotByKey[event.Key] = len(lots)
			lots = append(lots, lot)
			result.RemainingQuota += event.Quota
		case "consume":
			if event.Source != "" || event.ValueCNY != nil || event.Reverses != "" || event.Quota <= 0 || event.Quota > common.MaxWalletQuota || event.Quota > result.RemainingQuota {
				return nil, model.ErrFinanceInvalid
			}
			allocation := FinanceRevenueAllocation{EventKey: event.Key, Kind: event.Kind, Parts: []FinanceRevenuePart{}}
			left := event.Quota
			for i := range lots {
				lot := &lots[i]
				take := min(left, lot.event.Quota-lot.used)
				if take == 0 {
					continue
				}
				part := FinanceRevenuePart{LotKey: lot.event.Key, Source: lot.event.Source, Quota: take}
				if lot.event.Source == "unknown" {
					result.UnknownConsumedQuota += take
				} else {
					amount := decimal.Max(decimal.Zero, lot.consumedValue(lot.used+take).Sub(lot.recognized))
					lot.recognized = lot.recognized.Add(amount)
					value := amount.String()
					part.ValueCNY = &value
					revenue = revenue.Add(amount)
				}
				lot.used += take
				left -= take
				allocation.Parts = append(allocation.Parts, part)
				if left == 0 {
					break
				}
			}
			result.RemainingQuota -= event.Quota
			consumptionByKey[event.Key] = &financeConsumptionState{index: len(result.Allocations), reversedLot: map[string]int64{}}
			result.Allocations = append(result.Allocations, allocation)
		case "reverse":
			state, exists := consumptionByKey[event.Reverses]
			if !exists || event.Source != "" || event.ValueCNY != nil || event.Quota <= 0 || event.Quota > common.MaxWalletQuota {
				return nil, model.ErrFinanceInvalid
			}
			original := result.Allocations[state.index]
			allocation := FinanceRevenueAllocation{EventKey: event.Key, Kind: event.Kind, Reverses: event.Reverses, Parts: []FinanceRevenuePart{}}
			left := event.Quota
			for _, originalPart := range original.Parts {
				if left == 0 {
					break
				}
				reversedQuota := state.reversedLot[originalPart.LotKey]
				take := min(left, originalPart.Quota-reversedQuota)
				if take <= 0 {
					continue
				}
				lot := &lots[lotByKey[originalPart.LotKey]]
				part := FinanceRevenuePart{LotKey: originalPart.LotKey, Source: originalPart.Source, Quota: -take}
				if originalPart.ValueCNY == nil {
					result.UnknownConsumedQuota -= take
				} else {
					originalValue, err := financeDecimal(*originalPart.ValueCNY, false)
					if err != nil {
						return nil, model.ErrFinanceInvalid
					}
					partQuota := decimal.NewFromInt(originalPart.Quota)
					priorCumulative := originalValue.Mul(decimal.NewFromInt(reversedQuota)).DivRound(partQuota, 12)
					amount := originalValue.Mul(decimal.NewFromInt(reversedQuota+take)).DivRound(partQuota, 12).Sub(priorCumulative)
					revenue = revenue.Sub(amount)
					lot.recognized = lot.recognized.Sub(amount)
					value := amount.Neg().String()
					part.ValueCNY = &value
				}
				lot.used -= take
				state.reversed += take
				state.reversedLot[originalPart.LotKey] = reversedQuota + take
				allocation.Parts = append(allocation.Parts, part)
				left -= take
			}
			if left != 0 {
				return nil, model.ErrFinanceInvalid
			}
			result.RemainingQuota += event.Quota
			result.Allocations = append(result.Allocations, allocation)
		case "adjust":
			if event.Reverses != "" {
				return nil, model.ErrFinanceInvalid
			}
			if event.Quota > 0 {
				// A manual grant never carries cash value; paid sources must
				// enter through a cash-backed fact instead.
				if !slices.Contains([]string{"gift", "unknown"}, event.Source) || event.Quota > common.MaxWalletQuota-result.RemainingQuota {
					return nil, model.ErrFinanceInvalid
				}
				lot := financeRevenueLot{event: event}
				if event.Source == "unknown" {
					if event.ValueCNY != nil {
						return nil, model.ErrFinanceInvalid
					}
					unknownCash = true
				} else {
					if event.ValueCNY == nil {
						return nil, model.ErrFinanceInvalid
					}
					value, err := financeDecimal(*event.ValueCNY, false)
					if err != nil || !value.IsZero() {
						return nil, model.ErrFinanceInvalid
					}
					lot.value = value
				}
				lotByKey[event.Key] = len(lots)
				lots = append(lots, lot)
				result.RemainingQuota += event.Quota
			} else if event.Quota < 0 {
				if event.Source != "" || event.ValueCNY != nil {
					return nil, model.ErrFinanceInvalid
				}
				forfeit := -event.Quota
				if forfeit > result.RemainingQuota {
					return nil, model.ErrFinanceInvalid
				}
				allocation := FinanceRevenueAllocation{EventKey: event.Key, Kind: event.Kind, Parts: []FinanceRevenuePart{}}
				left := forfeit
				for i := range lots {
					lot := &lots[i]
					take := min(left, lot.event.Quota-lot.used)
					if take == 0 {
						continue
					}
					// Forfeited quota leaves the wallet without becoming
					// revenue or unknown consumption; its lot simply exhausts
					// sooner and later consumption recognizes what remains.
					allocation.Parts = append(allocation.Parts, FinanceRevenuePart{LotKey: lot.event.Key, Source: lot.event.Source, Quota: take})
					lot.used += take
					left -= take
					if left == 0 {
						break
					}
				}
				result.RemainingQuota -= forfeit
				result.Allocations = append(result.Allocations, allocation)
			} else {
				return nil, model.ErrFinanceInvalid
			}
		default:
			return nil, model.ErrFinanceInvalid
		}
	}
	for _, lot := range lots {
		part := FinanceRevenuePart{LotKey: lot.event.Key, Source: lot.event.Source, Quota: lot.event.Quota - lot.used}
		if lot.event.Source != "unknown" {
			value := lot.value.Sub(lot.recognized).String()
			part.ValueCNY = &value
		}
		result.Lots = append(result.Lots, part)
	}
	result.KnownBackingValueCNY, result.KnownConsumptionCNY = cash.String(), revenue.String()
	if unknownCash {
		result.Status = "partial"
		result.Warnings = append(result.Warnings, "unknown_credit_source")
	} else {
		value := cash.String()
		result.BackingValueCNY = &value
	}
	if result.UnknownConsumedQuota == 0 {
		value := revenue.String()
		result.ConsumptionCNY = &value
	} else {
		result.Status = "partial"
		result.Warnings = append(result.Warnings, "unknown_consumption_source")
		if nominal != nil {
			value := nominal.Mul(decimal.NewFromInt(result.UnknownConsumedQuota)).String()
			result.NominalUnknownCNY = &value
		}
	}
	return result, nil
}
