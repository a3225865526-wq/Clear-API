package service

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type FinanceSupplierInput struct {
	Scope       string `json:"scope"`
	EventKey    string `json:"event_key"`
	ChannelID   int    `json:"channel_id"`
	ExpectedID  int64  `json:"expected_id"`
	ReplacesID  int64  `json:"replaces_id"`
	PeriodFrom  int64  `json:"period_from"`
	PeriodTo    int64  `json:"period_to"`
	Currency    string `json:"currency"`
	Amount      string `json:"amount"`
	FXVersionID int64  `json:"fx_version_id"`
	Evidence    string `json:"evidence"`
}

func RegisterFinanceSupplierBill(db *gorm.DB, input FinanceSupplierInput, actor int, requestID string) (*model.FinanceSupplierBill, error) {
	if err := model.FinanceSupplierReady(db); err != nil {
		return nil, err
	}
	if input.Scope != "all_channel_request_costs" || actor <= 0 || input.ChannelID <= 0 || input.ExpectedID < 0 || input.ReplacesID < 0 || input.FXVersionID < 0 || !financeKeyPattern.MatchString(input.EventKey) || input.PeriodFrom <= 0 || input.PeriodTo <= input.PeriodFrom || input.PeriodTo-input.PeriodFrom > 366*86400 || !financeCurrencyPattern.MatchString(input.Currency) || strings.TrimSpace(input.Evidence) == "" || len(input.Evidence) > 500 {
		return nil, model.ErrFinanceInvalid
	}
	amount, err := financeDecimal(input.Amount, false)
	if err != nil {
		return nil, err
	}
	entry := &model.FinanceSupplierBill{Scope: input.Scope, EventKey: input.EventKey, ChannelID: input.ChannelID, ReplacesID: input.ReplacesID, PeriodFrom: input.PeriodFrom, PeriodTo: input.PeriodTo, Currency: input.Currency, Amount: amount.String(), FXVersionID: input.FXVersionID, Evidence: input.Evidence, CreatedBy: actor}
	if input.Currency == "CNY" {
		if input.FXVersionID != 0 {
			return nil, model.ErrFinanceInvalid
		}
		value := amount.String()
		entry.AmountCNY = &value
	} else if input.FXVersionID != 0 {
		rate, err := FinanceFrozenRate(db, input.FXVersionID, input.Currency, input.PeriodFrom)
		if err != nil {
			return nil, err
		}
		value := amount.Mul(rate).Round(12).String()
		entry.AmountCNY = &value
	}
	if err := model.AppendFinanceSupplierBill(db, entry, input.ExpectedID, requestID); err != nil {
		return nil, err
	}
	return entry, nil
}

type FinanceSupplierCoverage struct {
	Bill   model.FinanceSupplierBill `json:"bill"`
	Status string                    `json:"status"`
}

type FinanceSupplierSummary struct {
	CaptureFailuresSinceProcessStart int64                     `json:"capture_failures_since_process_start"`
	ChannelID                        int                       `json:"channel_id"`
	From                             int64                     `json:"from"`
	To                               int64                     `json:"to"`
	Status                           string                    `json:"status"`
	TotalCNY                         *string                   `json:"total_cny"`
	KnownSelectedCNY                 string                    `json:"known_selected_cny"`
	KnownBilledCNY                   string                    `json:"known_billed_cny"`
	KnownUncoveredEstimateCNY        string                    `json:"known_uncovered_estimate_cny"`
	KnownObservedEstimateCNY         string                    `json:"known_observed_estimate_cny"`
	ObservedAttempts                 int                       `json:"observed_attempts"`
	UnknownEstimateAttempts          int                       `json:"unknown_estimate_attempts"`
	UncoveredAttempts                int                       `json:"uncovered_attempts"`
	UnknownSelectedItems             int                       `json:"unknown_selected_items"`
	CoveredSeconds                   int64                     `json:"covered_seconds"`
	UncoveredSeconds                 int64                     `json:"uncovered_seconds"`
	Bills                            []FinanceSupplierCoverage `json:"bills"`
	Warnings                         []string                  `json:"warnings"`
}

// Attribution uses attempt_start.At in [from,to), including failed attempts.
// A fully included bill replaces ALL estimates in its scope. A clipped bill is
// unknown for this query: there is no unsupported time-proration of usage.
func ReconcileFinanceSupplier(db *gorm.DB, channelID int, from, to int64) (*FinanceSupplierSummary, error) {
	if channelID <= 0 || from <= 0 || to <= from || to-from > 366*86400 {
		return nil, model.ErrFinanceInvalid
	}
	if err := model.FinanceSupplierReady(db); err != nil {
		return nil, err
	}
	result := &FinanceSupplierSummary{ChannelID: channelID, From: from, To: to, Status: "partial", Bills: []FinanceSupplierCoverage{}, Warnings: []string{"owner_confirmed_manual_bill_not_supplier_api_verified", "bill_scope_all_channel_request_costs_excludes_pool_fixed_and_operating_fees", "attempt_start_time_may_differ_from_supplier_usage_time", "best_effort_capture_has_historical_and_transport_gaps", "capture_failure_counter_resets_on_restart", "registered_channel_only_not_site_total", "expenses_not_combined_to_avoid_double_counting"}}
	result.CaptureFailuresSinceProcessStart = financeCaptureFailures.Load()
	err := db.Transaction(func(tx *gorm.DB) error {
		var bills []model.FinanceSupplierBill
		if err := tx.Where("channel_id = ? AND period_from < ? AND period_to > ?", channelID, to, from).Order("revision desc").Limit(1001).Find(&bills).Error; err != nil {
			return err
		}
		if len(bills) > 1000 {
			return model.ErrFinanceInvalid
		}
		superseded := map[int64]bool{}
		billed, uncovered, observed := decimal.Zero, decimal.Zero, decimal.Zero
		for _, bill := range bills {
			if !superseded[bill.ID] {
				status := "owner_confirmed_manual"
				result.CoveredSeconds += min(to, bill.PeriodTo) - max(from, bill.PeriodFrom)
				if bill.PeriodFrom < from || bill.PeriodTo > to {
					status = "partial_period_amount_unknown"
					result.UnknownSelectedItems++
				} else if bill.AmountCNY == nil {
					status = "unknown_fx"
					result.UnknownSelectedItems++
				} else {
					value, err := decimal.NewFromString(*bill.AmountCNY)
					if err != nil {
						return model.ErrFinanceInvalid
					}
					billed = billed.Add(value)
				}
				result.Bills = append(result.Bills, FinanceSupplierCoverage{Bill: bill, Status: status})
			}
			superseded[bill.ReplacesID] = true
		}
		result.UncoveredSeconds = to - from - result.CoveredSeconds
		var starts []model.FinanceRequestEvent
		if err := tx.Where("channel_id = ? AND phase = ? AND at >= ? AND at < ?", channelID, "attempt_start", from, to).Limit(10001).Find(&starts).Error; err != nil {
			return err
		}
		if len(starts) > 10000 {
			return model.ErrFinanceInvalid
		}
		for _, start := range starts {
			result.ObservedAttempts++
			covered := false
			for _, scope := range result.Bills {
				if start.At >= scope.Bill.PeriodFrom && start.At < scope.Bill.PeriodTo {
					covered = true
					break
				}
			}
			if !covered {
				result.UncoveredAttempts++
			}
			var usages []model.FinanceRequestEvent
			if err := tx.Where("request_id = ? AND attempt_id = ? AND channel_id = ? AND phase = ?", start.RequestID, start.AttemptID, channelID, "usage").Limit(2).Find(&usages).Error; err != nil {
				return err
			}
			var snapshot FinanceUsageSnapshot
			if len(usages) != 1 || common.UnmarshalJsonStr(usages[0].Payload, &snapshot) != nil || snapshot.CostCNY == nil {
				result.UnknownEstimateAttempts++
				if !covered {
					result.UnknownSelectedItems++
				}
				continue
			}
			value, err := financeDecimal(*snapshot.CostCNY, false)
			if err != nil {
				return err
			}
			observed = observed.Add(value)
			if !covered {
				uncovered = uncovered.Add(value)
			}
		}
		result.KnownBilledCNY, result.KnownUncoveredEstimateCNY, result.KnownObservedEstimateCNY = billed.String(), uncovered.String(), observed.String()
		result.KnownSelectedCNY = billed.Add(uncovered).String()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
