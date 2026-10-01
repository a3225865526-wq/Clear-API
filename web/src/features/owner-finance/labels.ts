/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
export const metricLabels: Record<string, string> = {
  cash_receipts: 'Cash receipts',
  cash_refunds: 'Cash refunds',
  net_cash_receipts: 'Net cash receipts',
  opening_source_value: 'Opening unused source value',
  closing_source_value: 'Closing unused source value',
  consumption_gross: 'Gross consumption revenue',
  consumption_adjustments: 'Consumption reversals',
  consumption_revenue: 'Consumption revenue',
  direct_cost: 'Direct cost',
  gross_profit: 'Gross profit',
  operating_expenses: 'Operating expenses',
  operating_profit: 'Operating profit',
  request_cost_estimate: 'Request cost estimate',
}
export const codeLabels: Record<string, string> = {
  recorded: 'Recorded fact',
  owner_confirmed_manual: 'Owner confirmed, not bank reconciled',
  recorded_facts: 'Recorded facts',
  estimate: 'Estimate only',
  estimate_not_posted: 'Estimate only',
  known_items_difference_not_site_profit: 'Difference of known items',
  unknown_source_value: 'Missing source value',
  forfeiture_value_requires_review: 'Forfeited source value needs review',
  clipped_bill_no_proration: 'Bill crosses the selected period; amount unknown',
  unknown_fx: 'Missing frozen FX',
  expense_bill_overlap_requires_review:
    'Expense and bill may overlap; excluded pending review',
  unknown_cost: 'Missing procurement measurement or price',
  attempt_without_supplier_bill: 'Attempt without supplier bill',
  pool_expense_registration_missing: 'Pool expense has not been registered',
  site_sources_not_fully_connected: 'Site sources are not fully connected',
  expense_registration_not_confirmed: 'Expense coverage has not been confirmed',
  no_expense_confirmation_not_available:
    'No explicit zero expense confirmation exists',
  channel_inventory_and_bill_coverage_not_confirmed:
    'Channel inventory and bill coverage are unconfirmed',
  owner_confirmed_offline_only_not_bank_reconciled:
    'Confirmed offline receipts only; bank reconciliation unavailable',
  best_effort_attempts_only_not_profit:
    'Best effort request estimates; excluded from profit',
  no_registered_expense_evidence: 'No registered expense evidence',
  invalid_recorded_decimal: 'Invalid recorded decimal',
  registered_facts_only_not_complete_site_profit:
    'Registered facts only; full site profit is unavailable',
  late_facts_restate_business_period_and_opening_closing_stock:
    'Late facts restate historical periods and source balances',
  stock_is_not_cash_or_period_revenue:
    'Source balances are stocks, separate from period cash and revenue',
  cash_refunds_do_not_reverse_consumption:
    'Cash refunds do not reverse consumption revenue',
  supplier_cost_attributed_by_send_period_not_vendor_usage_time:
    'Supplier costs use request send periods, not vendor usage periods',
  estimates_and_bill_lines_are_reconciliation_only:
    'Estimates and bill lines reconcile only; bill totals post once',
  expense_absence_is_not_confirmed_zero:
    'Missing expenses do not mean zero cost',
  account_attribution_not_sufficient_no_account_profit:
    'Account revenue and utilization are unavailable',
  known_differences_are_not_complete_profit:
    'Known differences are not full site profit',
  serializable_database_snapshot_excludes_runtime_cache_and_process_counters:
    'Database snapshot excludes live cache and process counters',
  matched_reference_does_not_verify_amount:
    'Matching a reference does not verify its amount',
  non_usage_lines_must_be_deduplicated_with_expense_ledger:
    'Non-usage bill lines require expense deduplication',
  bill_total_remains_the_only_posting_value:
    'Bill total is the only posted procurement value',
  unknown_fx_lines_excluded_from_cny_net:
    'Lines without FX are excluded from known CNY subtotals',
  partial: 'Partial coverage',
  unknown: 'Unknown',
  matched: 'Matched',
  balanced: 'Balanced',
  consume: 'Consumption',
  reverse: 'Reversal',
  credit: 'Source credit',
  adjust: 'Source adjustment',
  ready: 'Ready for selected import',
  already_imported: 'Already imported',
  no_charge: 'No confirmed charge',
  refund_requires_review: 'Refund requires review',
  pending: 'Pending',
  processing: 'Processing',
  done: 'Processed',
  blocked: 'Blocked',
  needs_review: 'Needs review',
  receipt_unconfirmed: 'Receipt is unconfirmed',
  out_of_order: 'Late or out-of-order fact',
  invalid_fact: 'Invalid source fact',
  settlement_unresolved: 'Settlement is unresolved',
  fact_conflict: 'Fact conflict',
  version_conflict: 'Wallet revision conflict',
  processing_error: 'Processing failed',
  attempts_exhausted: 'Retry limit reached',
  best_effort_capture_not_atomic_with_wallet:
    'Request capture and wallet writes are not atomic',
  failure_counter_resets_on_restart:
    'Capture failure counter resets at process restart',
  transport_internal_retries_not_observed:
    'Internal provider retries are not observed',
  violation_fees_not_observed: 'Violation fees are not observed',
  midjourney_path_not_observed: 'Midjourney facts are not connected',
  image_count_procurement_meter_not_observed:
    'Image count procurement measurements are missing',
  task_facts_not_procurement_normalized:
    'Task facts are not mapped to procurement meters',
  reused_ws_responses_collapse_outbox_keys:
    'Reused WebSocket requests may share outbox keys',
  cash_receipts_and_source_allocation_not_connected:
    'Cash receipts and source allocations have incomplete connections',
  auto_emission_covers_offline_redemption_only:
    'Automatic source capture covers offline redemption only',
  consumption_emission_not_connected_to_settlement_boundary:
    'Consumption capture has incomplete settlement coverage',
  parked_events_require_owner_resolution: 'Parked facts require owner review',
  balance_layers_cache_batch_and_db_not_atomic:
    'Balance cache, batch and database layers are not atomic',
  point_in_time_non_atomic_reads: 'Balance reads are not one atomic snapshot',
  matching_does_not_prove_source_or_capture_completeness:
    'Matching balances do not prove complete sources or capture',
  in_flight_reservations_and_batch_updates_not_reconciled:
    'In-flight reservations and batch updates are unreconciled',
  unprocessed_sync_events_may_explain_difference:
    'Pending sync facts may explain balance differences',
  captured_settlements_only: 'Captured confirmed settlements only',
  late_refunds_require_review: 'Late refunds require review',
  historical_opening_and_balance_not_reconciled:
    'Historical opening balances are unreconciled',
  cash_confirmation_required_for_import:
    'Cash confirmation is required before importing sources',
  offline: 'Confirmed offline source',
  gift: 'Gift source with zero value',
  offline_redemption: 'Offline source redemption',
  consumption: 'Captured consumption',
  mismatch: 'Balance mismatch',
}
