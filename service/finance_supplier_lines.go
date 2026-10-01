package service

import (
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

var financeLineReferencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_:-]{0,99}$`)

var financeLineCategories = map[string]bool{"usage": true, "fixed": true, "minimum": true, "late_fee": true, "credit": true}

type FinanceSupplierLineInput struct {
	Scope            string  `json:"scope"`
	EventKey         string  `json:"event_key"`
	BillID           int64   `json:"bill_id"`
	ChannelID        int     `json:"channel_id"`
	Category         string  `json:"category"`
	UpstreamModel    string  `json:"upstream_model"`
	Reference        string  `json:"reference"`
	UsageFrom        int64   `json:"usage_from"`
	UsageTo          int64   `json:"usage_to"`
	Quantity         *string `json:"quantity"`
	Currency         string  `json:"currency"`
	Amount           string  `json:"amount"`
	FXVersionID      int64   `json:"fx_version_id"`
	ReplacesLineID   int64   `json:"replaces_line_id"`
	ReferencesLineID int64   `json:"references_line_id"`
	Evidence         string  `json:"evidence"`
}

func RegisterFinanceSupplierBillLine(db *gorm.DB, input FinanceSupplierLineInput, actor int, requestID string) (*model.FinanceSupplierBillLine, error) {
	if err := model.FinanceSupplierLinesReady(db); err != nil {
		return nil, err
	}
	if input.Scope != "supplier_bill_detail" || actor <= 0 || input.BillID <= 0 || input.ChannelID <= 0 || !financeLineCategories[input.Category] || !financeKeyPattern.MatchString(input.EventKey) || input.UsageFrom <= 0 || input.UsageTo <= input.UsageFrom || input.UsageTo-input.UsageFrom > 366*86400 || !financeCurrencyPattern.MatchString(input.Currency) || strings.TrimSpace(input.Evidence) == "" || len(input.Evidence) > 500 || len(input.UpstreamModel) > 200 || input.ReplacesLineID < 0 || input.ReferencesLineID < 0 || input.FXVersionID < 0 {
		return nil, model.ErrFinanceInvalid
	}
	if input.Reference != "" && !financeLineReferencePattern.MatchString(input.Reference) {
		return nil, model.ErrFinanceInvalid
	}
	if input.ReplacesLineID != 0 && input.ReferencesLineID != 0 {
		return nil, model.ErrFinanceInvalid
	}
	if input.Category == "credit" && input.ReferencesLineID == 0 {
		return nil, model.ErrFinanceInvalid
	}
	if input.Category != "credit" && input.ReferencesLineID != 0 {
		return nil, model.ErrFinanceInvalid
	}
	amount, err := financeDecimal(input.Amount, false)
	if err != nil {
		return nil, err
	}
	if input.Quantity != nil {
		quantity, err := financeDecimal(*input.Quantity, false)
		if err != nil {
			return nil, err
		}
		quantityValue := quantity.String()
		input.Quantity = &quantityValue
	}
	entry := &model.FinanceSupplierBillLine{EventKey: input.EventKey, BillID: input.BillID, ChannelID: input.ChannelID, Category: input.Category, UpstreamModel: input.UpstreamModel, Reference: input.Reference, UsageFrom: input.UsageFrom, UsageTo: input.UsageTo, Quantity: input.Quantity, Currency: input.Currency, Amount: amount.String(), FXVersionID: input.FXVersionID, ReplacesLineID: input.ReplacesLineID, ReferencesLineID: input.ReferencesLineID, Evidence: input.Evidence, CreatedBy: actor}
	if input.Currency == "CNY" {
		if input.FXVersionID != 0 {
			return nil, model.ErrFinanceInvalid
		}
		value := amount.String()
		entry.AmountCNY = &value
	} else if input.FXVersionID != 0 {
		rate, err := FinanceFrozenRate(db, input.FXVersionID, input.Currency, input.UsageFrom)
		if err != nil {
			return nil, err
		}
		value := amount.Mul(rate).Round(12).String()
		entry.AmountCNY = &value
	}
	if err := model.AppendFinanceSupplierBillLine(db, entry, requestID); err != nil {
		return nil, err
	}
	return entry, nil
}

// Line reference matching is resolved at read time against the append-only
// fact tables, so facts captured later (late arrivals) still resolve. A
// matched reference associates the line with its facts; it never upgrades the
// line's evidence by itself and never auto-confirms amounts.
type FinanceLineMatch struct {
	Status          string  `json:"status"`
	Reference       string  `json:"reference"`
	EstimateCNY     *string `json:"associated_estimate_cny"`
	AssociatedFacts int     `json:"associated_facts"`
}

func resolveFinanceLineMatch(tx *gorm.DB, line model.FinanceSupplierBillLine) FinanceLineMatch {
	match := FinanceLineMatch{Status: "no_reference", Reference: line.Reference}
	if line.Reference == "" {
		return match
	}
	if strings.HasPrefix(line.Reference, "task:") {
		var submissions int64
		if err := tx.Model(&model.FinanceRequestEvent{}).Where("request_id = ? AND phase = ?", line.Reference, "task_submission").Count(&submissions).Error; err != nil {
			match.Status = "unmatched_ref"
			return match
		}
		if submissions == 1 {
			var adjustments int64
			_ = tx.Model(&model.FinanceRequestEvent{}).Where("request_id = ? AND phase = ?", line.Reference, "task_adjustment").Count(&adjustments).Error
			match.Status, match.AssociatedFacts = "matched_task", int(submissions+adjustments)
			return match
		}
		match.Status = "unmatched_ref"
		return match
	}
	var starts []model.FinanceRequestEvent
	if err := tx.Where("request_id = ? AND phase = ?", line.Reference, "attempt_start").Limit(100).Find(&starts).Error; err != nil || len(starts) == 0 {
		match.Status = "unmatched_ref"
		return match
	}
	// Retried requests keep one fact set per gateway attempt; the reference
	// resolves to the logical request and sums every attempt's usage estimate.
	estimate := decimal.Zero
	known := false
	for _, start := range starts {
		var usage model.FinanceRequestEvent
		if err := tx.Where("request_id = ? AND attempt_id = ? AND channel_id = ? AND phase = ?", start.RequestID, start.AttemptID, start.ChannelID, "usage").Limit(2).Find(&usage).Error; err != nil {
			continue
		}
		if usage.ID == 0 {
			continue
		}
		var snapshot FinanceUsageSnapshot
		if common.UnmarshalJsonStr(usage.Payload, &snapshot) != nil || snapshot.CostCNY == nil {
			continue
		}
		value, err := decimal.NewFromString(*snapshot.CostCNY)
		if err != nil {
			continue
		}
		estimate = estimate.Add(value)
		known = true
	}
	match.Status, match.AssociatedFacts = "matched_request", len(starts)
	if known {
		value := estimate.String()
		match.EstimateCNY = &value
	}
	return match
}

type FinanceSupplierLineView struct {
	model.FinanceSupplierBillLine
	Superseded bool             `json:"superseded"`
	Match      FinanceLineMatch `json:"match"`
}

type FinanceLineCategorySummary struct {
	Category   string            `json:"category"`
	Lines      int               `json:"lines"`
	AmountCNY  *string           `json:"net_cny"`
	ByCurrency map[string]string `json:"by_currency"`
}

type FinanceSupplierLinesSummary struct {
	Status                           string                       `json:"status"`
	Bill                             model.FinanceSupplierBill    `json:"bill"`
	TotalLines                       int                          `json:"total_lines"`
	EffectiveLines                   int                          `json:"effective_lines"`
	CreditLines                      int                          `json:"credit_lines"`
	UnknownFXLines                   int                          `json:"unknown_fx_lines"`
	OutOfBillPeriodLines             int                          `json:"out_of_bill_period_lines"`
	NetCNY                           *string                      `json:"net_cny"`
	BillAmountCNY                    *string                      `json:"bill_amount_cny"`
	VarianceCNY                      *string                      `json:"variance_cny"`
	UsageNetCNY                      *string                      `json:"usage_net_cny"`
	NonUsageNetCNY                   *string                      `json:"non_usage_net_cny"`
	Categories                       []FinanceLineCategorySummary `json:"categories"`
	MatchStats                       map[string]int               `json:"match_stats"`
	CaptureFailuresSinceProcessStart int64                        `json:"capture_failures_since_process_start"`
	Warnings                         []string                     `json:"warnings"`
}

// The bill total stays the only posting value for its scope. Lines provide a
// reconciliation view: category nets, currency splits, match statistics and
// the variance against the bill. Nothing here is added to profit figures, and
// non-usage lines (fixed/minimum/late_fee) must be deduplicated against the
// expense ledger elsewhere — they are shown separately for exactly that check.
func SummarizeFinanceSupplierLines(db *gorm.DB, billID int64) (*FinanceSupplierLinesSummary, error) {
	if billID <= 0 {
		return nil, model.ErrFinanceInvalid
	}
	if err := model.FinanceSupplierLinesReady(db); err != nil {
		return nil, err
	}
	result := &FinanceSupplierLinesSummary{Status: "partial", Categories: []FinanceLineCategorySummary{}, MatchStats: map[string]int{}, Warnings: []string{
		"bill_total_remains_the_only_posting_value",
		"non_usage_lines_must_be_deduplicated_with_expense_ledger",
		"matched_reference_does_not_verify_amount",
		"unknown_fx_lines_excluded_from_cny_net",
		"out_of_bill_period_lines_pending_review_no_proration",
		"request_estimate_comparison_is_in_the_reconciliation_endpoint",
		"owner_confirmed_manual_bill_not_supplier_api_verified",
	}}
	result.CaptureFailuresSinceProcessStart = financeCaptureFailures.Load()
	err := db.Transaction(func(tx *gorm.DB) error {
		var bill model.FinanceSupplierBill
		if err := tx.First(&bill, billID).Error; err != nil {
			return model.ErrFinanceInvalid
		}
		var chain []model.FinanceSupplierBill
		if err := tx.Where("channel_id = ?", bill.ChannelID).Order("revision desc").Find(&chain).Error; err != nil {
			return err
		}
		supersededBills := map[int64]bool{}
		for _, row := range chain {
			supersededBills[row.ReplacesID] = true
		}
		if supersededBills[bill.ID] {
			return model.ErrFinanceInvalid
		}
		result.Bill = bill
		if bill.AmountCNY != nil {
			result.BillAmountCNY = bill.AmountCNY
		}
		var lines []model.FinanceSupplierBillLine
		if err := tx.Where("bill_id = ?", billID).Order("id asc").Limit(10001).Find(&lines).Error; err != nil {
			return err
		}
		if len(lines) > 10000 {
			return model.ErrFinanceInvalid
		}
		replacedLines := map[int64]bool{}
		for _, line := range lines {
			if line.ReplacesLineID != 0 {
				replacedLines[line.ReplacesLineID] = true
			}
		}
		categoryLines := map[string]int{}
		categoryCNY := map[string]decimal.Decimal{}
		categoryCurrency := map[string]map[string]decimal.Decimal{}
		netCNY, usageNet, nonUsageNet := decimal.Zero, decimal.Zero, decimal.Zero
		for _, line := range lines {
			result.TotalLines++
			if replacedLines[line.ID] {
				continue
			}
			result.EffectiveLines++
			if line.Category == "credit" {
				result.CreditLines++
			}
			if line.UsageFrom < bill.PeriodFrom || line.UsageTo > bill.PeriodTo {
				result.OutOfBillPeriodLines++
			}
			match := resolveFinanceLineMatch(tx, line)
			result.MatchStats[match.Status]++
			// Currency subtotals accumulate every effective line in its
			// original currency; the CNY net only includes frozen conversions
			// and treats credit lines purely as offsets (second pass below),
			// never as positive cost.
			byCurrency := categoryCurrency[line.Category]
			if byCurrency == nil {
				byCurrency = map[string]decimal.Decimal{}
			}
			native, err := decimal.NewFromString(line.Amount)
			if err != nil {
				return model.ErrFinanceInvalid
			}
			byCurrency[line.Currency] = byCurrency[line.Currency].Add(native)
			categoryCurrency[line.Category] = byCurrency
			if line.AmountCNY == nil {
				result.UnknownFXLines++
			} else if line.Category != "credit" {
				value, err := decimal.NewFromString(*line.AmountCNY)
				if err != nil {
					return model.ErrFinanceInvalid
				}
				categoryCNY[line.Category] = categoryCNY[line.Category].Add(value)
				netCNY = netCNY.Add(value)
				if line.Category == "usage" {
					usageNet = usageNet.Add(value)
				} else {
					nonUsageNet = nonUsageNet.Add(value)
				}
			}
			categoryLines[line.Category]++
		}
		// Credit lines offset their category: walk them again as negative
		// contributions so each category net is gross minus credits.
		for _, line := range lines {
			if replacedLines[line.ID] || line.Category != "credit" || line.AmountCNY == nil {
				continue
			}
			value, err := decimal.NewFromString(*line.AmountCNY)
			if err != nil {
				return model.ErrFinanceInvalid
			}
			var original model.FinanceSupplierBillLine
			if err := tx.First(&original, line.ReferencesLineID).Error; err != nil {
				return model.ErrFinanceInvalid
			}
			categoryCNY[original.Category] = categoryCNY[original.Category].Sub(value)
			netCNY = netCNY.Sub(value)
			if original.Category == "usage" {
				usageNet = usageNet.Sub(value)
			} else {
				nonUsageNet = nonUsageNet.Sub(value)
			}
		}
		for category, count := range categoryLines {
			entry := FinanceLineCategorySummary{Category: category, Lines: count, ByCurrency: map[string]string{}}
			if value, ok := categoryCNY[category]; ok {
				stringValue := value.String()
				entry.AmountCNY = &stringValue
			}
			for currency, value := range categoryCurrency[category] {
				entry.ByCurrency[currency] = value.String()
			}
			result.Categories = append(result.Categories, entry)
		}
		if result.EffectiveLines > 0 {
			value := netCNY.String()
			result.NetCNY = &value
			if result.UnknownFXLines == 0 && result.BillAmountCNY != nil {
				billValue, err := decimal.NewFromString(*result.BillAmountCNY)
				if err != nil {
					return model.ErrFinanceInvalid
				}
				variance := billValue.Sub(netCNY).String()
				result.VarianceCNY = &variance
			}
			if usageNet.Sign() != 0 || categoryLines["usage"] > 0 {
				value := usageNet.String()
				result.UsageNetCNY = &value
			}
			if nonUsageNet.Sign() != 0 || categoryLines["fixed"] > 0 || categoryLines["minimum"] > 0 || categoryLines["late_fee"] > 0 {
				value := nonUsageNet.String()
				result.NonUsageNetCNY = &value
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ListFinanceSupplierBillLines returns every line of one currently effective
// bill with its live reference match. Superseded originals stay visible.
func ListFinanceSupplierBillLines(db *gorm.DB, billID int64) ([]FinanceSupplierLineView, *model.FinanceSupplierBill, error) {
	if billID <= 0 {
		return nil, nil, model.ErrFinanceInvalid
	}
	if err := model.FinanceSupplierLinesReady(db); err != nil {
		return nil, nil, err
	}
	var lines []FinanceSupplierLineView
	var bill model.FinanceSupplierBill
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&bill, billID).Error; err != nil {
			return model.ErrFinanceInvalid
		}
		var chain []model.FinanceSupplierBill
		if err := tx.Where("channel_id = ?", bill.ChannelID).Order("revision desc").Find(&chain).Error; err != nil {
			return err
		}
		supersededBills := map[int64]bool{}
		for _, row := range chain {
			supersededBills[row.ReplacesID] = true
		}
		if supersededBills[bill.ID] {
			return model.ErrFinanceInvalid
		}
		var rows []model.FinanceSupplierBillLine
		if err := tx.Where("bill_id = ?", billID).Order("id asc").Limit(10001).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > 10000 {
			return model.ErrFinanceInvalid
		}
		replaced := map[int64]bool{}
		for _, line := range rows {
			if line.ReplacesLineID != 0 {
				replaced[line.ReplacesLineID] = true
			}
		}
		for _, line := range rows {
			lines = append(lines, FinanceSupplierLineView{FinanceSupplierBillLine: line, Superseded: replaced[line.ID], Match: resolveFinanceLineMatch(tx, line)})
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return lines, &bill, nil
}
