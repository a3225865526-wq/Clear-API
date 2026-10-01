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
	result := &FinanceRevenueResult{Simulation: true, Status: "known", Lots: []FinanceRevenuePart{}, Allocations: []FinanceRevenueAllocation{}, Warnings: []string{"simulation_only", "cash_receipts_not_connected", "cash_refunds_and_subscription_revenue_not_supported"}}
	lots := []financeRevenueLot{}
	lotByKey := map[string]int{}
	consumptionByKey := map[string]int{}
	seen, reversed := map[string]bool{}, map[string]bool{}
	cash, revenue := decimal.Zero, decimal.Zero
	unknownCash := false
	var lastAt int64
	for _, event := range input.Events {
		if !financeKeyPattern.MatchString(event.Key) || seen[event.Key] || event.At <= 0 || event.At < lastAt || event.Quota <= 0 || event.Quota > common.MaxWalletQuota {
			return nil, model.ErrFinanceInvalid
		}
		seen[event.Key], lastAt = true, event.At
		switch event.Kind {
		case "credit":
			if !slices.Contains([]string{"paid", "gift", "unknown"}, event.Source) || event.Reverses != "" || event.Quota > common.MaxWalletQuota-result.RemainingQuota {
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
			if event.Source != "" || event.ValueCNY != nil || event.Reverses != "" || event.Quota > result.RemainingQuota {
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
			consumptionByKey[event.Key] = len(result.Allocations)
			result.Allocations = append(result.Allocations, allocation)
		case "reverse":
			index, exists := consumptionByKey[event.Reverses]
			if !exists || reversed[event.Reverses] || event.Source != "" || event.ValueCNY != nil || event.Quota > common.MaxWalletQuota-result.RemainingQuota {
				return nil, model.ErrFinanceInvalid
			}
			original := result.Allocations[index]
			var total int64
			for _, part := range original.Parts {
				total += part.Quota
			}
			// Only whole consumption reversals are supported here. Partial cash
			// refunds require separately approved attributable adjustment facts.
			if event.Quota != total {
				return nil, model.ErrFinanceInvalid
			}
			allocation := FinanceRevenueAllocation{EventKey: event.Key, Kind: event.Kind, Reverses: event.Reverses, Parts: []FinanceRevenuePart{}}
			for _, part := range original.Parts {
				lot := &lots[lotByKey[part.LotKey]]
				lot.used -= part.Quota
				part.Quota = -part.Quota
				if part.ValueCNY == nil {
					result.UnknownConsumedQuota += part.Quota
				} else {
					amount, _ := financeDecimal(*part.ValueCNY, false)
					revenue = revenue.Sub(amount)
					lot.recognized = lot.recognized.Sub(amount)
					value := amount.Neg().String()
					part.ValueCNY = &value
				}
				allocation.Parts = append(allocation.Parts, part)
			}
			result.RemainingQuota += total
			reversed[event.Reverses] = true
			result.Allocations = append(result.Allocations, allocation)
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
