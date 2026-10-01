package service

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// Total is the site total. RecordedTotal is exact only for the listed facts;
// neither an empty ledger nor a balanced wallet proves site coverage.
type FinanceProfitMetric struct {
	Total         *string  `json:"total"`
	RecordedTotal *string  `json:"recorded_total"`
	KnownSubtotal string   `json:"known_subtotal"`
	UnknownItems  int      `json:"unknown_items"`
	Status        string   `json:"status"`
	Coverage      []string `json:"coverage"`
	EvidenceLevel string   `json:"evidence_level"`
}

// Facts are posting contributions, not copied evidence or business records.
// A (metric, table, id, related_id) tuple identifies each contribution.
type FinanceProfitFact struct {
	Metric        string  `json:"metric"`
	Table         string  `json:"table"`
	ID            int64   `json:"id"`
	RelatedID     int64   `json:"related_id"`
	From          int64   `json:"from"`
	To            int64   `json:"to"`
	AmountCNY     *string `json:"amount_cny"`
	Status        string  `json:"status"`
	ChannelID     int     `json:"channel_id"`
	UpstreamModel string  `json:"upstream_model"`
	PoolKey       string  `json:"pool_key"`
	PostingKind   string  `json:"posting_kind"`
}

type FinanceProfitDimension struct {
	Dimension string                         `json:"dimension"`
	Key       string                         `json:"key"`
	Metrics   map[string]FinanceProfitMetric `json:"metrics"`
}

type FinanceProfitPeriod struct {
	From    int64                          `json:"from"`
	To      int64                          `json:"to"`
	Metrics map[string]FinanceProfitMetric `json:"metrics"`
}

type FinanceProfitSummary struct {
	FinanceProfitPeriod
	Currency       string                         `json:"currency"`
	Timezone       string                         `json:"timezone"`
	Status         string                         `json:"status"`
	Complete       bool                           `json:"complete_site_profit"`
	ProfitMargin   *string                        `json:"profit_margin"`
	RecordedMargin *string                        `json:"recorded_profit_margin"`
	Snapshot       string                         `json:"snapshot_sha256"`
	ReadAt         int64                          `json:"read_at"`
	SchemaVersion  int                            `json:"schema_version"`
	AuditWatermark int64                          `json:"audit_watermark_id"`
	WalletVersions map[int]int64                  `json:"wallet_versions"`
	SyncHealth     *FinanceSyncHealthReport       `json:"sync_health"`
	CaptureHealth  map[string]any                 `json:"capture_health"`
	SupplierChecks []*FinanceSupplierLinesSummary `json:"supplier_checks"`
	Dimensions     []FinanceProfitDimension       `json:"dimensions"`
	Trend          []FinanceProfitPeriod          `json:"trend"`
	Facts          []FinanceProfitFact            `json:"facts"`
	Warnings       []string                       `json:"warnings"`
}

// Named calendar selections are closed at next midnight/month boundary in
// Beijing. Future portions carry the same coverage gaps, never inferred zero.
func FinanceProfitRange(period string, from, to int64, now time.Time) (int64, int64, error) {
	if period != "" && (from != 0 || to != 0) {
		return 0, 0, model.ErrFinanceInvalid
	}
	local := now.In(time.FixedZone("Asia/Shanghai", 8*3600))
	switch period {
	case "today":
		start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
		from, to = start.Unix(), start.AddDate(0, 0, 1).Unix()
	case "month":
		start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, local.Location())
		from, to = start.Unix(), start.AddDate(0, 1, 0).Unix()
	case "":
	default:
		return 0, 0, model.ErrFinanceInvalid
	}
	if from <= 0 || to <= from || to-from > 366*86400 {
		return 0, 0, model.ErrFinanceInvalid
	}
	return from, to, nil
}

// All persisted reads use one serializable, read-only transaction. The hash
// includes mutable wallet/outbox heads and allocations, so a rebuild invalidates
// it even when auto-generated IDs happen to be reused. It is not an as-of-time
// archive: late facts restate prior periods at the next read.
func SummarizeFinanceProfit(db *gorm.DB, from, to int64, trend string) (*FinanceProfitSummary, error) {
	if _, _, err := FinanceProfitRange("", from, to, time.Now()); err != nil || !slices.Contains([]string{"", "day", "month"}, trend) {
		return nil, model.ErrFinanceInvalid
	}
	if err := model.FinanceSupplierLinesReady(db); err != nil {
		return nil, err
	}
	result := &FinanceProfitSummary{Currency: "CNY", Timezone: "Asia/Shanghai", Status: "partial", ReadAt: time.Now().Unix(), SchemaVersion: 9,
		WalletVersions: map[int]int64{}, Facts: []FinanceProfitFact{}, Dimensions: []FinanceProfitDimension{}, Trend: []FinanceProfitPeriod{}, SupplierChecks: []*FinanceSupplierLinesSummary{},
		Warnings: []string{"registered_facts_only_not_complete_site_profit", "late_facts_restate_business_period_and_opening_closing_stock", "stock_is_not_cash_or_period_revenue", "cash_refunds_do_not_reverse_consumption", "supplier_cost_attributed_by_send_period_not_vendor_usage_time", "estimates_and_bill_lines_are_reconciliation_only", "expense_absence_is_not_confirmed_zero", "account_attribution_not_sufficient_no_account_profit", "known_differences_are_not_complete_profit", "serializable_database_snapshot_excludes_runtime_cache_and_process_counters"}}
	result.CaptureHealth = FinanceCaptureHealth()
	err := db.Transaction(func(tx *gorm.DB) error {
		tx = tx.Session(&gorm.Session{DisableNestedTransaction: true})
		var records []model.FinanceRevenueRecord
		var lots []model.FinanceRevenueLot
		var allocations []model.FinanceRevenueAllocation
		var wallets []model.FinanceWallet
		var receipts []model.FinanceCashReceipt
		var refunds []model.FinanceCashRefund
		var expenses []model.FinanceExpense
		var bills []model.FinanceSupplierBill
		var requests []model.FinanceRequestEvent
		var versions []model.FinanceVersion
		var syncRows []model.FinanceSyncEvent
		// Caps reject the query; no truncation may masquerade as a total.
		queries := []struct {
			query *gorm.DB
			dest  any
		}{
			{tx.Where("at < ?", to), &records},
			{tx.Where("event_id IN (?)", tx.Model(&model.FinanceRevenueRecord{}).Select("id").Where("at < ?", to)), &lots},
			{tx.Where("event_id IN (?)", tx.Model(&model.FinanceRevenueRecord{}).Select("id").Where("at < ?", to)), &allocations},
			{tx, &wallets},
			{tx.Where("paid_at >= ? AND paid_at < ?", from, to), &receipts},
			{tx.Where("refunded_at >= ? AND refunded_at < ?", from, to), &refunds},
			{tx.Where("period_from < ? AND period_to > ?", to, from), &expenses},
			{tx.Where("period_from < ? AND period_to > ?", to, from), &bills},
			{tx.Where("(at >= ? AND at < ?) OR (phase = ? AND request_id IN (?)) OR (phase = ? AND at < ?)", from, to, "usage",
				tx.Model(&model.FinanceRequestEvent{}).Select("request_id").Where("phase = ? AND at >= ? AND at < ?", "attempt_start", from, to),
				"text_settlement", to), &requests},
			{tx.Where("kind = ? OR (kind = ? AND effective_from < ? AND effective_to > ?)", "fx", "pool", to, from), &versions},
			{tx, &syncRows},
		}
		for _, query := range queries {
			read := query.query.Order("id asc")
			if query.dest == &wallets {
				read = query.query.Order("user_id asc")
			}
			found := read.Limit(10001).Find(query.dest)
			if found.Error != nil {
				return found.Error
			}
			if found.RowsAffected > 10000 {
				return model.ErrFinanceInvalid
			}
		}
		for _, wallet := range wallets {
			result.WalletVersions[wallet.UserID] = wallet.Revision
		}
		var auditID *int64
		if err := tx.Model(&model.FinanceAudit{}).Select("max(id)").Scan(&auditID).Error; err != nil {
			return err
		}
		if auditID != nil {
			result.AuditWatermark = *auditID
		}
		var err error
		result.SyncHealth, err = FinanceSyncHealth(tx)
		if err != nil {
			return err
		}
		// Process-only capture counters are explicitly outside the DB snapshot.
		result.Warnings = append(result.Warnings, result.SyncHealth.Warnings...)
		result.Warnings = append(result.Warnings, result.CaptureHealth["warnings"].([]string)...)

		recordByID := map[int64]model.FinanceRevenueRecord{}
		for _, record := range records {
			recordByID[record.ID] = record
		}
		observationByID := map[int64]model.FinanceRequestEvent{}
		for _, observation := range requests {
			observationByID[observation.ID] = observation
		}
		for _, part := range allocations {
			record := recordByID[part.EventID]
			if record.Kind != "consume" && record.Kind != "reverse" {
				continue
			}
			fact := FinanceProfitFact{Metric: "consumption_revenue", Table: "finance_revenue_allocations", ID: part.ID, RelatedID: record.ID, From: record.At, To: record.At + 1, AmountCNY: part.ValueCNY, Status: "recorded", PostingKind: record.Kind}
			if fact.AmountCNY == nil {
				fact.Status = "unknown_source_value"
			}
			if text, ok := strings.CutPrefix(record.EventKey, "request-charge-"); ok {
				id, err := strconv.ParseInt(text, 10, 64)
				if err != nil {
					return model.ErrFinanceInvalid
				}
				if observation, ok := observationByID[id]; ok && observation.UserID == record.UserID {
					fact.ChannelID, fact.UpstreamModel = observation.ChannelID, observation.UpstreamModel
				}
			}
			if record.At >= from {
				result.Facts = append(result.Facts, fact)
			}
		}
		for _, boundary := range []struct {
			name string
			at   int64
		}{{"opening_source_value", from}, {"closing_source_value", to}} {
			for _, lot := range lots {
				if recordByID[lot.EventID].At >= boundary.at {
					continue
				}
				remainingQuota := lot.TotalQuota
				recognized := decimal.Zero
				forfeited := false
				for _, part := range allocations {
					if part.LotID != lot.ID || recordByID[part.EventID].At >= boundary.at {
						continue
					}
					remainingQuota -= part.Quota
					if recordByID[part.EventID].Kind == "adjust" {
						forfeited = true
					}
					if part.ValueCNY != nil {
						value, err := financeDecimal(*part.ValueCNY, true)
						if err != nil {
							return err
						}
						recognized = recognized.Add(value)
					}
				}
				if remainingQuota < 0 || remainingQuota > lot.TotalQuota {
					return model.ErrFinanceInvalid
				}
				if remainingQuota == 0 {
					continue
				}
				fact := FinanceProfitFact{Metric: boundary.name, Table: "finance_revenue_lots", ID: lot.ID, RelatedID: lot.EventID, From: boundary.at, To: boundary.at, Status: "unknown_source_value"}
				if lot.ValueCNY != nil && !forfeited {
					value, err := financeDecimal(*lot.ValueCNY, false)
					if err != nil {
						return err
					}
					value = value.Sub(recognized)
					if value.IsNegative() {
						return model.ErrFinanceInvalid
					}
					amount := value.String()
					fact.AmountCNY, fact.Status = &amount, "recorded"
				} else if forfeited {
					fact.Status = "forfeiture_value_requires_review"
				}
				result.Facts = append(result.Facts, fact)
			}
		}
		for _, receipt := range receipts {
			amount := receipt.AmountCNY
			result.Facts = append(result.Facts, FinanceProfitFact{Metric: "cash_receipts", Table: "finance_cash_receipts", ID: receipt.ID, From: receipt.PaidAt, To: receipt.PaidAt + 1, AmountCNY: &amount, Status: "owner_confirmed_manual"})
		}
		for _, refund := range refunds {
			if refund.State != model.FinanceRefundSucceeded {
				continue
			}
			amount := refund.AmountCNY
			result.Facts = append(result.Facts, FinanceProfitFact{Metric: "cash_refunds", Table: "finance_cash_refunds", ID: refund.ID, RelatedID: refund.ReceiptID, From: refund.RefundedAt, To: refund.RefundedAt + 1, AmountCNY: &amount, Status: "owner_confirmed_manual"})
		}
		superseded := map[int64]bool{}
		for _, bill := range bills {
			superseded[bill.ReplacesID] = true
		}
		nonUsageBills := map[int64]bool{}
		for _, bill := range bills {
			if superseded[bill.ID] {
				continue
			}
			check, err := SummarizeFinanceSupplierLines(tx, bill.ID)
			if err != nil {
				return err
			}
			slices.SortFunc(check.Categories, func(a, b FinanceLineCategorySummary) int { return strings.Compare(a.Category, b.Category) })
			result.SupplierChecks = append(result.SupplierChecks, check)
			for _, category := range check.Categories {
				if slices.Contains([]string{"fixed", "minimum", "late_fee"}, category.Category) {
					nonUsageBills[bill.ID] = true
				}
			}
			fact := FinanceProfitFact{Metric: "direct_cost", Table: "finance_supplier_bills", ID: bill.ID, RelatedID: bill.ReplacesID, From: bill.PeriodFrom, To: bill.PeriodTo, AmountCNY: bill.AmountCNY, Status: "owner_confirmed_manual", ChannelID: bill.ChannelID}
			if bill.PeriodFrom < from || bill.PeriodTo > to {
				fact.AmountCNY, fact.Status = nil, "clipped_bill_no_proration"
			} else if bill.AmountCNY == nil {
				fact.Status = "unknown_fx"
			}
			result.Facts = append(result.Facts, fact)
		}
		for _, expense := range expenses {
			metric := "operating_expenses"
			if expense.Ledger == "direct" {
				metric = "direct_cost"
			}
			fact := FinanceProfitFact{Metric: metric, Table: "finance_expenses", ID: expense.ID, RelatedID: expense.AdjustsID, From: expense.PeriodFrom, To: expense.PeriodTo, PoolKey: expense.PoolKey, Status: "recorded"}
			// No channel/reference column exists on an expense. Do not guess
			// that an unpooled direct fee is distinct from a supplier bill.
			duplicateRisk := false
			for _, bill := range bills {
				if superseded[bill.ID] || bill.PeriodFrom >= expense.PeriodTo || bill.PeriodTo <= expense.PeriodFrom {
					continue
				}
				if (expense.Ledger == "direct" && expense.PoolKey == "") || nonUsageBills[bill.ID] {
					duplicateRisk = true
				}
			}
			if duplicateRisk {
				fact.Status = "expense_bill_overlap_requires_review"
			} else {
				amount, err := financeProfitExpenseAmount(tx, expense, from, to)
				if err != nil {
					return err
				}
				fact.AmountCNY = amount
				if amount == nil {
					fact.Status = "unknown_fx"
				}
			}
			result.Facts = append(result.Facts, fact)
		}
		// Estimates remain separate even without a bill. Unknown attempts,
		// including failed/idle attempts, never become zero cost.
		for _, start := range requests {
			if start.Phase != "attempt_start" || start.At < from || start.At >= to {
				continue
			}
			var attempt FinanceAttemptSnapshot
			if common.UnmarshalJsonStr(start.Payload, &attempt) != nil {
				return model.ErrFinanceInvalid
			}
			fact := FinanceProfitFact{Metric: "request_cost_estimate", Table: "finance_request_events", ID: start.ID, From: start.At, To: start.At + 1, ChannelID: start.ChannelID, UpstreamModel: start.UpstreamModel, PoolKey: attempt.PoolKey, Status: "unknown_cost"}
			count := 0
			for _, usage := range requests {
				if usage.Phase != "usage" || usage.AttemptID != start.AttemptID || usage.RequestID != start.RequestID || usage.ChannelID != start.ChannelID {
					continue
				}
				count++
				var snapshot FinanceUsageSnapshot
				if common.UnmarshalJsonStr(usage.Payload, &snapshot) != nil {
					return model.ErrFinanceInvalid
				}
				fact.RelatedID, fact.AmountCNY = usage.ID, snapshot.CostCNY
			}
			if count != 1 {
				fact.AmountCNY = nil
			}
			if fact.AmountCNY != nil {
				fact.Status = "estimate_not_posted"
			}
			result.Facts = append(result.Facts, fact)
			covered := false
			for _, bill := range bills {
				if !superseded[bill.ID] && bill.ChannelID == start.ChannelID && bill.PeriodFrom <= start.At && bill.PeriodTo > start.At {
					covered = true
				}
			}
			if !covered {
				result.Facts = append(result.Facts, FinanceProfitFact{Metric: "direct_cost", Table: "finance_request_events", ID: start.ID, From: start.At, To: start.At + 1, ChannelID: start.ChannelID, UpstreamModel: start.UpstreamModel, PoolKey: attempt.PoolKey, Status: "attempt_without_supplier_bill"})
			}
		}
		poolSeen := map[string]bool{}
		for i := len(versions) - 1; i >= 0; i-- {
			version := versions[i]
			if version.Kind != "pool" || poolSeen[version.RecordKey] {
				continue
			}
			poolSeen[version.RecordKey] = true
			if !version.Active {
				continue
			}
			registered := false
			for _, expense := range expenses {
				if expense.PoolKey == version.RecordKey {
					registered = true
				}
			}
			if !registered {
				result.Facts = append(result.Facts, FinanceProfitFact{Metric: "direct_cost", Table: "finance_versions", ID: version.ID, From: max(from, version.EffectiveFrom), To: min(to, version.EffectiveTo), PoolKey: version.RecordKey, Status: "pool_expense_registration_missing"})
			}
		}
		result.FinanceProfitPeriod = financeProfitMetrics(result.Facts, from, to)
		// All account-free dimension rows sum to their corresponding posting
		// subtotal, with an explicit unattributed bucket. No bill allocation
		// to a model or pool is invented from its comparison estimates.
		for _, dimension := range []string{"channel", "upstream_model", "pool"} {
			groups := map[string][]FinanceProfitFact{}
			for _, fact := range result.Facts {
				key := "unattributed"
				switch dimension {
				case "channel":
					if fact.ChannelID > 0 {
						key = strconv.Itoa(fact.ChannelID)
					}
				case "upstream_model":
					if fact.UpstreamModel != "" {
						key = fact.UpstreamModel
					}
				case "pool":
					if fact.PoolKey != "" {
						key = fact.PoolKey
					}
				}
				groups[key] = append(groups[key], fact)
			}
			keys := make([]string, 0, len(groups))
			for key := range groups {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			for _, key := range keys {
				result.Dimensions = append(result.Dimensions, FinanceProfitDimension{Dimension: dimension, Key: key, Metrics: financeProfitMetrics(groups[key], from, to).Metrics})
			}
		}
		if trend != "" {
			local := time.Unix(from, 0).In(time.FixedZone("Asia/Shanghai", 8*3600))
			for left := from; left < to; {
				var next time.Time
				if trend == "day" {
					next = time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, local.Location())
				} else {
					next = time.Date(local.Year(), local.Month()+1, 1, 0, 0, 0, 0, local.Location())
				}
				right := min(to, next.Unix())
				facts := slices.Clone(result.Facts)
				for i := range facts {
					fact := &facts[i]
					if fact.Table != "finance_expenses" || fact.Status == "expense_bill_overlap_requires_review" {
						continue
					}
					for _, expense := range expenses {
						if expense.ID != fact.ID {
							continue
						}
						// Cumulative differences telescope exactly to the report
						// amount, including 12-place accrual rounding.
						endAmount, endErr := financeProfitExpenseAmount(tx, expense, from, right)
						if endErr != nil {
							return endErr
						}
						fact.AmountCNY = endAmount
						if endAmount != nil && left > from {
							startAmount, startErr := financeProfitExpenseAmount(tx, expense, from, left)
							if startErr != nil {
								return startErr
							}
							if startAmount == nil {
								fact.AmountCNY = nil
							} else {
								a, _ := decimal.NewFromString(*endAmount)
								b, _ := decimal.NewFromString(*startAmount)
								value := a.Sub(b).String()
								fact.AmountCNY = &value
							}
						}
					}
				}
				bucket := financeProfitMetrics(facts, left, right)
				// Opening/closing stock is computed for the whole report only.
				delete(bucket.Metrics, "opening_source_value")
				delete(bucket.Metrics, "closing_source_value")
				result.Trend = append(result.Trend, bucket)
				left, local = right, next
			}
		}
		revenue, profit := result.Metrics["consumption_revenue"], result.Metrics["operating_profit"]
		if revenue.RecordedTotal != nil && profit.RecordedTotal != nil {
			denominator, _ := decimal.NewFromString(*revenue.RecordedTotal)
			if !denominator.IsZero() {
				numerator, _ := decimal.NewFromString(*profit.RecordedTotal)
				value := numerator.DivRound(denominator, 12).String()
				result.RecordedMargin = &value
			}
		}
		checksForHash := make([]FinanceSupplierLinesSummary, 0, len(result.SupplierChecks))
		for _, check := range result.SupplierChecks {
			copy := *check
			copy.CaptureFailuresSinceProcessStart = 0
			checksForHash = append(checksForHash, copy)
		}
		encoded, err := common.Marshal([]any{from, to, records, lots, allocations, wallets, receipts, refunds, expenses, bills, requests, versions, syncRows, checksForHash, result.AuditWatermark})
		if err != nil {
			return err
		}
		result.Snapshot = fmt.Sprintf("%x", sha256.Sum256(encoded))
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelSerializable, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func financeProfitExpenseAmount(db *gorm.DB, expense model.FinanceExpense, from, to int64) (*string, error) {
	amount, err := AccrueFinanceExpense(expense, from, to)
	if err != nil {
		return nil, err
	}
	if expense.Currency != "CNY" {
		if expense.FXVersionID == 0 {
			return nil, nil
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
			return nil, err
		}
		amount = amount.Mul(rate)
	}
	value := amount.String()
	return &value, nil
}

func financeProfitMetrics(facts []FinanceProfitFact, from, to int64) FinanceProfitPeriod {
	result := FinanceProfitPeriod{From: from, To: to, Metrics: map[string]FinanceProfitMetric{}}
	for _, key := range []string{"cash_receipts", "cash_refunds", "opening_source_value", "closing_source_value", "consumption_gross", "consumption_adjustments", "consumption_revenue", "direct_cost", "operating_expenses", "request_cost_estimate"} {
		metric := FinanceProfitMetric{Status: "partial", Coverage: []string{"site_sources_not_fully_connected"}, EvidenceLevel: "recorded_facts", KnownSubtotal: "0"}
		if key == "direct_cost" || key == "operating_expenses" {
			metric.Coverage = []string{"expense_registration_not_confirmed", "no_expense_confirmation_not_available"}
		}
		if key == "direct_cost" {
			metric.Coverage = append(metric.Coverage, "channel_inventory_and_bill_coverage_not_confirmed")
		}
		if strings.HasPrefix(key, "cash_") {
			metric.Coverage = []string{"owner_confirmed_offline_only_not_bank_reconciled"}
			metric.EvidenceLevel = "owner_confirmed_manual"
		}
		if key == "request_cost_estimate" {
			metric.Coverage = []string{"best_effort_attempts_only_not_profit"}
			metric.EvidenceLevel = "estimate"
		}
		known, count := decimal.Zero, 0
		for _, fact := range facts {
			matches := fact.Metric == key || (fact.Metric == "consumption_revenue" && ((key == "consumption_gross" && fact.PostingKind == "consume") || (key == "consumption_adjustments" && fact.PostingKind == "reverse")))
			if !matches {
				continue
			}
			stock := key == "opening_source_value" || key == "closing_source_value"
			if !stock && (fact.From >= to || fact.To <= from) {
				continue
			}
			count++
			value := fact.AmountCNY
			if fact.Table == "finance_supplier_bills" && (fact.From < from || fact.To > to) {
				value = nil
			}
			if value == nil {
				metric.UnknownItems++
				metric.Coverage = append(metric.Coverage, fact.Status)
				continue
			}
			amount, err := financeDecimal(*value, true)
			if err != nil {
				metric.UnknownItems++
				metric.Coverage = append(metric.Coverage, "invalid_recorded_decimal")
				continue
			}
			known = known.Add(amount)
		}
		metric.KnownSubtotal = known.String()
		if metric.UnknownItems == 0 && (count > 0 || (key != "direct_cost" && key != "operating_expenses")) {
			value := known.String()
			metric.RecordedTotal = &value
		}
		if count == 0 && (key == "direct_cost" || key == "operating_expenses") {
			metric.UnknownItems++
			metric.Coverage = append(metric.Coverage, "no_registered_expense_evidence")
		}
		slices.Sort(metric.Coverage)
		metric.Coverage = slices.Compact(metric.Coverage)
		result.Metrics[key] = metric
	}
	for _, relation := range []struct{ key, left, right string }{{"net_cash_receipts", "cash_receipts", "cash_refunds"}, {"gross_profit", "consumption_revenue", "direct_cost"}, {"operating_profit", "gross_profit", "operating_expenses"}} {
		left, right := result.Metrics[relation.left], result.Metrics[relation.right]
		a, _ := decimal.NewFromString(left.KnownSubtotal)
		b, _ := decimal.NewFromString(right.KnownSubtotal)
		metric := FinanceProfitMetric{KnownSubtotal: a.Sub(b).String(), UnknownItems: left.UnknownItems + right.UnknownItems, Status: "partial", Coverage: append(slices.Clone(left.Coverage), right.Coverage...), EvidenceLevel: "known_item_difference_only"}
		if left.RecordedTotal != nil && right.RecordedTotal != nil {
			value := metric.KnownSubtotal
			metric.RecordedTotal = &value
		}
		slices.Sort(metric.Coverage)
		metric.Coverage = slices.Compact(metric.Coverage)
		result.Metrics[relation.key] = metric
	}
	return result
}
