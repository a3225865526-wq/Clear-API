package service

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// Pool fixed and variable fees have exactly one posting location: the expense
// ledger row registered against the pool key. Per-attempt request cost
// estimates stay reconciliation-only figures; they are never added to the
// profit total, so a pool's fees cannot be counted twice. This summary only
// arranges the existing registrations and observations side by side.

type FinancePoolCostEntry struct {
	PoolKey        string  `json:"pool_key"`
	Name           string  `json:"name"`
	Platform       string  `json:"platform"`
	ExpenseCNY     *string `json:"registered_expense_cny"`
	ExpenseCount   int     `json:"expense_count"`
	ExpenseUnknown int     `json:"expense_unknown_count"`
	ExpenseMissing bool    `json:"expense_missing"`
	RequestCostCNY *string `json:"request_cost_estimate_cny"`
	RequestKnown   int     `json:"request_cost_known"`
	RequestUnknown int     `json:"request_cost_unknown"`
}

type FinancePoolCostSummary struct {
	From                             int64                  `json:"from"`
	To                               int64                  `json:"to"`
	Status                           string                 `json:"status"`
	Pools                            []FinancePoolCostEntry `json:"pools"`
	Unpooled                         FinancePoolCostEntry   `json:"unpooled"`
	CaptureFailuresSinceProcessStart int64                  `json:"capture_failures_since_process_start"`
	Warnings                         []string               `json:"warnings"`
}

// SummarizeFinancePoolCosts arranges pool registrations and per-attempt cost
// estimates for one period. Attribution follows the immutable send time
// (attempt_start), including failed attempts and attempts without any usage.
func SummarizeFinancePoolCosts(db *gorm.DB, from, to int64) (*FinancePoolCostSummary, error) {
	if from <= 0 || to <= from || to-from > 366*86400 {
		return nil, model.ErrFinanceInvalid
	}
	if err := model.FinanceReady(db); err != nil {
		return nil, err
	}
	result := &FinancePoolCostSummary{From: from, To: to, Status: "partial", Pools: []FinancePoolCostEntry{}, Warnings: []string{
		"pool_fees_post_only_in_expense_ledger",
		"request_estimates_are_reconciliation_only_not_profit",
		"attempt_start_time_may_differ_from_supplier_usage_time",
		"best_effort_capture_has_historical_and_transport_gaps",
		"account_level_attribution_requires_reliable_internal_reference",
	}}
	result.CaptureFailuresSinceProcessStart = financeCaptureFailures.Load()
	err := db.Transaction(func(tx *gorm.DB) error {
		// Configured pools overlapping the period, latest revision per key.
		var poolRows []model.FinanceVersion
		if err := tx.Where("kind = ? AND effective_from < ? AND effective_to > ?", "pool", to, from).Order("revision desc").Limit(1001).Find(&poolRows).Error; err != nil {
			return err
		}
		if len(poolRows) > 1000 {
			return model.ErrFinanceInvalid
		}
		poolSeen := map[string]bool{}
		entries := map[string]*FinancePoolCostEntry{}
		for _, row := range poolRows {
			if poolSeen[row.RecordKey] {
				continue
			}
			poolSeen[row.RecordKey] = true
			var pool FinancePool
			if common.UnmarshalJsonStr(row.Payload, &pool) != nil {
				return model.ErrFinanceInvalid
			}
			entries[row.RecordKey] = &FinancePoolCostEntry{PoolKey: row.RecordKey, Name: pool.Name, Platform: pool.Platform}
		}

		// Registered pool expenses, accrued into the queried period exactly
		// like the operating expense summary.
		var expenses []model.FinanceExpense
		if err := tx.Where("pool_key <> '' AND period_from < ? AND period_to > ?", to, from).Limit(10001).Find(&expenses).Error; err != nil {
			return err
		}
		if len(expenses) > 10000 {
			return model.ErrFinanceInvalid
		}
		expenseTotals := map[string]decimal.Decimal{}
		expenseCounts := map[string]int{}
		unknownExpenses := map[string]int{}
		for _, expense := range expenses {
			amount, err := AccrueFinanceExpense(expense, from, to)
			if err != nil {
				return err
			}
			if expense.Currency != "CNY" {
				if expense.FXVersionID == 0 {
					unknownExpenses[expense.PoolKey]++
					continue
				}
				at := expense.PeriodFrom
				if expense.AdjustsID > 0 {
					var original model.FinanceExpense
					if err := tx.First(&original, expense.AdjustsID).Error; err != nil {
						return model.ErrFinanceInvalid
					}
					at = original.PeriodFrom
				}
				rate, err := FinanceFrozenRate(tx, expense.FXVersionID, expense.Currency, at)
				if err != nil {
					return errors.New("invalid recorded FX reference")
				}
				amount = amount.Mul(rate)
			}
			total := expenseTotals[expense.PoolKey]
			expenseTotals[expense.PoolKey] = total.Add(amount)
			expenseCounts[expense.PoolKey]++
		}
		for key, count := range expenseCounts {
			if entry := entries[key]; entry != nil {
				entry.ExpenseCount = count
				entry.ExpenseUnknown = unknownExpenses[key]
				if total, ok := expenseTotals[key]; ok {
					value := total.String()
					entry.ExpenseCNY = &value
				}
			}
		}
		for _, entry := range entries {
			if entry.ExpenseCount == 0 && entry.ExpenseUnknown == 0 {
				entry.ExpenseMissing = true
			}
		}

		// Per-attempt request cost estimates, attributed by the pool key
		// frozen at attempt start. Usage without a cost rule, without usage,
		// or without any observation stays unknown.
		var starts []model.FinanceRequestEvent
		if err := tx.Where("phase = ? AND at >= ? AND at < ?", "attempt_start", from, to).Limit(10001).Find(&starts).Error; err != nil {
			return err
		}
		if len(starts) > 10000 {
			return model.ErrFinanceInvalid
		}
		requestTotals := map[string]decimal.Decimal{}
		requestKnown := map[string]int{}
		requestUnknown := map[string]int{}
		for _, start := range starts {
			var snapshot FinanceAttemptSnapshot
			if common.UnmarshalJsonStr(start.Payload, &snapshot) != nil {
				return model.ErrFinanceInvalid
			}
			poolKey := snapshot.PoolKey
			var usage model.FinanceRequestEvent
			if err := tx.Where("attempt_id = ? AND phase = ?", start.AttemptID, "usage").Limit(2).Find(&usage).Error; err != nil {
				return err
			}
			known := false
			if usage.ID != 0 {
				var usageSnapshot FinanceUsageSnapshot
				if common.UnmarshalJsonStr(usage.Payload, &usageSnapshot) != nil {
					return model.ErrFinanceInvalid
				}
				if usageSnapshot.CostCNY != nil {
					value, err := decimal.NewFromString(*usageSnapshot.CostCNY)
					if err != nil {
						return model.ErrFinanceInvalid
					}
					total := requestTotals[poolKey]
					requestTotals[poolKey] = total.Add(value)
					known = true
				}
			}
			if known {
				requestKnown[poolKey]++
			} else {
				requestUnknown[poolKey]++
			}
		}
		for _, entry := range entries {
			entry.RequestKnown = requestKnown[entry.PoolKey]
			entry.RequestUnknown = requestUnknown[entry.PoolKey]
			if total, ok := requestTotals[entry.PoolKey]; ok {
				value := total.String()
				entry.RequestCostCNY = &value
			}
		}
		unpooled := FinancePoolCostEntry{PoolKey: ""}
		unpooled.RequestKnown = requestKnown[""]
		unpooled.RequestUnknown = requestUnknown[""]
		if total, ok := requestTotals[""]; ok {
			value := total.String()
			unpooled.RequestCostCNY = &value
		}
		if unpooled.RequestKnown > 0 || unpooled.RequestUnknown > 0 {
			result.Unpooled = unpooled
		}
		for _, entry := range entries {
			result.Pools = append(result.Pools, *entry)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
