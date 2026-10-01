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
export type Metric = {
  total: string | null
  recorded_total: string | null
  known_subtotal: string
  unknown_items: number
  status: string
  coverage: string[]
  evidence_level: string
}
export type Fact = {
  metric: string
  table: string
  id: number
  related_id: number
  from: number
  to: number
  amount_cny: string | null
  status: string
  channel_id: number
  upstream_model: string
  pool_key: string
  posting_kind: string
}
export type Period = {
  from: number
  to: number
  metrics: Record<string, Metric>
}
export type SupplierCheck = {
  bill: {
    id: number
    channel_id: number
    period_from: number
    period_to: number
  }
  status: string
  bill_amount_cny: string | null
  net_cny: string | null
  variance_cny: string | null
  usage_net_cny: string | null
  non_usage_net_cny: string | null
  total_lines: number
  unknown_fx_lines: number
  out_of_bill_period_lines: number
  match_stats: Record<string, number>
  warnings: string[]
}
export type Summary = Period & {
  currency: 'CNY'
  timezone: 'Asia/Shanghai'
  status: string
  complete_site_profit: boolean
  profit_margin: string | null
  recorded_profit_margin: string | null
  snapshot_sha256: string
  read_at: number
  schema_version: number
  audit_watermark_id: number
  wallet_versions: Record<string, number>
  sync_health: {
    counts: Record<string, number>
    needs_review_classes: Record<string, number>
    watermark_id: number
    oldest_pending_age_seconds: number | null
    unimported_settlement_sample: number
    warnings: string[]
  }
  capture_health: { warnings?: string[] }
  supplier_checks: SupplierCheck[]
  dimensions: {
    dimension: string
    key: string
    metrics: Record<string, Metric>
  }[]
  trend: Period[]
  facts: Fact[]
  warnings: string[]
}
export type Capability = {
  can_access: boolean
  schema_ready: boolean
  supplier_bill_lines_ready: boolean
  profit_summary_available: boolean
  complete_site_profit_available: boolean
}
export type Range =
  | { period: 'today' | 'month'; trend: 'day' | 'month' }
  | {
      from: number
      to: number
      trend: 'day' | 'month'
    }
