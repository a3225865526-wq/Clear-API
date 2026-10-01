package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

var financeKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,99}$`)
var financeDecimalPattern = regexp.MustCompile(`^-?[0-9]{1,16}(\.[0-9]{1,12})?$`)
var financeCurrencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

type FinanceUnitPrice struct {
	Meter string `json:"meter"`
	Unit  string `json:"unit"`
	Per   string `json:"per"`
	Price string `json:"price"`
}

type FinanceCostRule struct {
	ChannelID     int                `json:"channel_id"`
	UpstreamModel string             `json:"upstream_model"`
	PoolKey       string             `json:"pool_key"`
	Currency      string             `json:"currency"`
	Prices        []FinanceUnitPrice `json:"prices"`
}

type FinanceFXRate struct {
	Currency   string `json:"currency"`
	CNYPerUnit string `json:"cny_per_unit"`
}

type FinancePool struct {
	Name       string `json:"name"`
	Platform   string `json:"platform"`
	ChannelIDs []int  `json:"channel_ids"`
}

// All rates and money are exact decimal strings. No float-to-money rounding or
// currency-display setting participates in finance calculations.
func financeDecimal(value string, signed bool) (decimal.Decimal, error) {
	if !financeDecimalPattern.MatchString(value) {
		return decimal.Zero, model.ErrFinanceInvalid
	}
	number, err := decimal.NewFromString(value)
	if err != nil || (!signed && number.IsNegative()) {
		return decimal.Zero, model.ErrFinanceInvalid
	}
	return number, nil
}

func ValidateFinanceVersion(entry *model.FinanceVersion, raw json.RawMessage) error {
	if !financeKeyPattern.MatchString(entry.RecordKey) || entry.CreatedBy <= 0 || entry.EffectiveFrom <= 0 || entry.EffectiveTo <= entry.EffectiveFrom || len(entry.Source) == 0 || len(entry.Source) > 500 || len(raw) > 16384 {
		return model.ErrFinanceInvalid
	}
	var payload any
	switch entry.Kind {
	case "cost":
		var cost FinanceCostRule
		if err := common.Unmarshal(raw, &cost); err != nil {
			return model.ErrFinanceInvalid
		}
		if cost.ChannelID <= 0 || cost.UpstreamModel == "" || len(cost.UpstreamModel) > 200 || !financeCurrencyPattern.MatchString(cost.Currency) || len(cost.Prices) == 0 || len(cost.Prices) > 16 || (cost.PoolKey != "" && !financeKeyPattern.MatchString(cost.PoolKey)) {
			return model.ErrFinanceInvalid
		}
		meters := map[string]bool{}
		units := map[string]string{"input": "token", "output": "token", "cache_read": "token", "cache_write": "token", "cache_write_5m": "token", "cache_write_1h": "token", "image": "count", "audio": "second", "video": "second", "request": "count", "vendor": "unit"}
		for _, price := range cost.Prices {
			per, err := financeDecimal(price.Per, false)
			if err != nil || !per.IsPositive() || units[price.Meter] == "" || units[price.Meter] != price.Unit || meters[price.Meter] {
				return model.ErrFinanceInvalid
			}
			if _, err := financeDecimal(price.Price, false); err != nil {
				return err
			}
			meters[price.Meter] = true
		}
		if meters["cache_write"] && (meters["cache_write_5m"] || meters["cache_write_1h"]) {
			return model.ErrFinanceInvalid
		}
		payload = cost
	case "fx":
		var fx FinanceFXRate
		if err := common.Unmarshal(raw, &fx); err != nil {
			return model.ErrFinanceInvalid
		}
		rate, err := financeDecimal(fx.CNYPerUnit, false)
		if err != nil || !rate.IsPositive() || !financeCurrencyPattern.MatchString(fx.Currency) || fx.Currency == "CNY" {
			return model.ErrFinanceInvalid
		}
		payload = fx
	case "pool":
		var pool FinancePool
		if err := common.Unmarshal(raw, &pool); err != nil {
			return model.ErrFinanceInvalid
		}
		if pool.Name == "" || len(pool.Name) > 100 || pool.Platform == "" || len(pool.Platform) > 100 || len(pool.ChannelIDs) > 100 {
			return model.ErrFinanceInvalid
		}
		seen := map[int]bool{}
		for _, id := range pool.ChannelIDs {
			if id <= 0 || seen[id] {
				return model.ErrFinanceInvalid
			}
			seen[id] = true
		}
		payload = pool
	default:
		return model.ErrFinanceInvalid
	}
	// Canonical typed payload discards unrecognized fields rather than persisting
	// an arbitrary blob (including accidentally supplied credentials).
	encoded, err := common.Marshal(payload)
	if err != nil {
		return err
	}
	entry.Payload = string(encoded)
	return nil
}

func ValidateFinanceExpense(db *gorm.DB, entry *model.FinanceExpense) error {
	if !financeKeyPattern.MatchString(entry.EventKey) || entry.CreatedBy <= 0 || !slices.Contains([]string{"direct", "operating"}, entry.Ledger) || entry.Category == "" || len(entry.Category) > 100 || entry.PeriodFrom <= 0 || entry.PeriodTo <= entry.PeriodFrom || entry.PeriodTo-entry.PeriodFrom > 20*366*86400 || !slices.Contains([]string{"once", "month", "year"}, entry.Recurrence) || !financeCurrencyPattern.MatchString(entry.Currency) || entry.Source == "" || len(entry.Source) > 500 || len(entry.Note) > 1000 || (entry.PoolKey != "" && !financeKeyPattern.MatchString(entry.PoolKey)) || (entry.AccountRef != "" && !financeKeyPattern.MatchString(entry.AccountRef)) || entry.FXVersionID < 0 || entry.AdjustsID < 0 {
		return model.ErrFinanceInvalid
	}
	amount, err := financeDecimal(entry.Amount, entry.AdjustsID > 0)
	if err != nil || amount.IsZero() {
		return model.ErrFinanceInvalid
	}
	if entry.AdjustsID > 0 && (entry.Note == "" || entry.Recurrence != "once") {
		return model.ErrFinanceInvalid
	}
	if entry.AccountRef != "" && entry.PoolKey == "" {
		return model.ErrFinanceInvalid
	}
	if entry.PoolKey != "" && entry.AdjustsID == 0 {
		var pool model.FinanceVersion
		if err := db.Where("kind = ? AND record_key = ? AND effective_from <= ?", "pool", entry.PoolKey, entry.PeriodFrom).Order("revision desc").First(&pool).Error; err != nil || !pool.Active || entry.PeriodFrom >= pool.EffectiveTo {
			return model.ErrFinanceInvalid
		}
	}
	if entry.Currency == "CNY" {
		if entry.FXVersionID != 0 {
			return model.ErrFinanceInvalid
		}
		return nil
	}
	// Missing FX is preserved as unknown. A supplied rate must be a frozen,
	// matching version applicable at the start of the fee's attributed period.
	if entry.FXVersionID == 0 {
		return nil
	}
	at := entry.PeriodFrom
	if entry.AdjustsID > 0 {
		var original model.FinanceExpense
		if err := db.First(&original, entry.AdjustsID).Error; err != nil {
			return model.ErrFinanceInvalid
		}
		at = original.PeriodFrom
	}
	_, err = FinanceFrozenRate(db, entry.FXVersionID, entry.Currency, at)
	return err
}

func FinanceFrozenRate(db *gorm.DB, id int64, currency string, at int64) (decimal.Decimal, error) {
	var version model.FinanceVersion
	if err := db.First(&version, id).Error; err != nil {
		return decimal.Zero, model.ErrFinanceInvalid
	}
	var fx FinanceFXRate
	if version.Kind != "fx" || !version.Active || at < version.EffectiveFrom || at >= version.EffectiveTo || common.UnmarshalJsonStr(version.Payload, &fx) != nil || fx.Currency != currency {
		return decimal.Zero, model.ErrFinanceInvalid
	}
	rate, err := financeDecimal(fx.CNYPerUnit, false)
	if err != nil || !rate.IsPositive() {
		return decimal.Zero, model.ErrFinanceInvalid
	}
	return rate, nil
}

type FinanceCostEstimate struct {
	Amount   *string  `json:"amount"`
	Currency string   `json:"currency"`
	Status   string   `json:"status"`
	Missing  []string `json:"missing"`
}

// Quantities must be mutually exclusive procurement-normalized facts. Missing
// meters are unknown, even for a failed/non-user-charged attempt. This helper is
// deliberately not connected to existing user quota calculation.
func EstimateFinanceCost(rule FinanceCostRule, quantities map[string]*string) (FinanceCostEstimate, error) {
	result := FinanceCostEstimate{Currency: rule.Currency, Status: "estimated", Missing: []string{}}
	amount := decimal.Zero
	for _, price := range rule.Prices {
		value := quantities[price.Meter]
		if value == nil {
			result.Missing = append(result.Missing, price.Meter)
			continue
		}
		quantity, err := financeDecimal(*value, false)
		if err != nil {
			return result, err
		}
		per, err := financeDecimal(price.Per, false)
		if err != nil || !per.IsPositive() {
			return result, model.ErrFinanceInvalid
		}
		unitPrice, err := financeDecimal(price.Price, false)
		if err != nil {
			return result, err
		}
		amount = amount.Add(quantity.Mul(unitPrice).DivRound(per, 12))
	}
	if len(rule.Prices) == 0 {
		result.Missing = append(result.Missing, "cost_rule")
	}
	if len(result.Missing) > 0 {
		result.Status = "unknown"
		return result, nil
	}
	value := amount.String()
	result.Amount = &value
	return result, nil
}

// Calendar periods use Beijing time and the original day-of-month anchor.
// Jan 31 -> Feb 28 -> Mar 31, rather than AddDate's overflow into March.
func financeCalendarBoundary(anchor time.Time, months int) time.Time {
	first := time.Date(anchor.Year(), anchor.Month()+time.Month(months), 1, anchor.Hour(), anchor.Minute(), anchor.Second(), 0, anchor.Location())
	lastDay := time.Date(first.Year(), first.Month()+1, 0, 0, 0, 0, 0, first.Location()).Day()
	return time.Date(first.Year(), first.Month(), min(anchor.Day(), lastDay), anchor.Hour(), anchor.Minute(), anchor.Second(), 0, first.Location())
}

func AccrueFinanceExpense(entry model.FinanceExpense, from, to int64) (decimal.Decimal, error) {
	if to <= from || entry.PeriodFrom <= 0 || entry.PeriodTo <= entry.PeriodFrom || entry.PeriodTo-entry.PeriodFrom > 20*366*86400 {
		return decimal.Zero, model.ErrFinanceInvalid
	}
	amount, err := financeDecimal(entry.Amount, entry.AdjustsID > 0)
	if err != nil {
		return decimal.Zero, err
	}
	if !slices.Contains([]string{"once", "month", "year"}, entry.Recurrence) {
		return decimal.Zero, model.ErrFinanceInvalid
	}
	beijing := time.FixedZone("Asia/Shanghai", 8*3600)
	anchor := time.Unix(entry.PeriodFrom, 0).In(beijing)
	accrued := decimal.Zero
	for cycle := 0; ; cycle++ {
		start, end := entry.PeriodFrom, entry.PeriodTo
		if entry.Recurrence != "once" {
			months := 1
			if entry.Recurrence == "year" {
				months = 12
			}
			start = financeCalendarBoundary(anchor, cycle*months).Unix()
			end = financeCalendarBoundary(anchor, (cycle+1)*months).Unix()
		}
		if start >= entry.PeriodTo || start >= to {
			break
		}
		left, right := max(start, from), min(end, entry.PeriodTo, to)
		if right > left {
			accrued = accrued.Add(amount.Mul(decimal.NewFromInt(right-left)).DivRound(decimal.NewFromInt(end-start), 12))
		}
		if entry.Recurrence == "once" {
			break
		}
	}
	return accrued, nil
}

type FinanceExpenseSummary struct {
	DirectCNY      string   `json:"known_direct_cny"`
	OperatingCNY   string   `json:"known_operating_cny"`
	UnknownFXCount int      `json:"unknown_fx_count"`
	Status         string   `json:"status"`
	Warnings       []string `json:"warnings"`
}

func SummarizeFinanceExpenses(db *gorm.DB, from, to int64) (*FinanceExpenseSummary, error) {
	if from <= 0 || to <= from || to-from > 20*366*86400 {
		return nil, model.ErrFinanceInvalid
	}
	if err := model.FinanceReady(db); err != nil {
		return nil, err
	}
	var expenses []model.FinanceExpense
	if err := db.Where("period_from < ? AND period_to > ?", to, from).Find(&expenses).Error; err != nil {
		return nil, err
	}
	result := &FinanceExpenseSummary{Status: "partial", Warnings: []string{"request_costs_not_connected", "revenue_sources_not_connected", "expense_registration_not_confirmed"}}
	direct, operating := decimal.Zero, decimal.Zero
	for _, expense := range expenses {
		amount, err := AccrueFinanceExpense(expense, from, to)
		if err != nil {
			return nil, err
		}
		if expense.Currency != "CNY" {
			if expense.FXVersionID == 0 {
				result.UnknownFXCount++
				continue
			}
			at := expense.PeriodFrom
			if expense.AdjustsID > 0 {
				var original model.FinanceExpense
				if err := db.First(&original, expense.AdjustsID).Error; err != nil {
					return nil, model.ErrFinanceInvalid
				}
				at = original.PeriodFrom
			}
			rate, err := FinanceFrozenRate(db, expense.FXVersionID, expense.Currency, at)
			if err != nil {
				return nil, errors.New("invalid recorded FX reference")
			}
			amount = amount.Mul(rate)
		}
		if expense.Ledger == "direct" {
			direct = direct.Add(amount)
		} else {
			operating = operating.Add(amount)
		}
	}
	result.DirectCNY, result.OperatingCNY = direct.String(), operating.String()
	return result, nil
}

func ValidateFinanceKind(kind string) error {
	if !slices.Contains([]string{"cost", "fx", "pool"}, kind) {
		return fmt.Errorf("%w: kind", model.ErrFinanceInvalid)
	}
	return nil
}
