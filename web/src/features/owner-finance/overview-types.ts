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
export type OverviewUsage = {
  requests: number
  errors: number
  active_users: number
  quota: number
  refund_quota: number
  input: number
  output: number
  average_seconds: number | null
}
export type OverviewDay = OverviewUsage & { date: string; new_users: number }
export type OverviewRanking = {
  name: string
  requests: number
  quota: number
  input: number
  output: number
}
export type OverviewData = OverviewUsage & {
  from: number
  to: number
  read_at: number
  timezone: 'Asia/Shanghai'
  total_users: number
  new_users: number
  unknown_registration: number
  paid_credit_users: number | null
  pending_review: number | null
  trend: OverviewDay[]
  models: OverviewRanking[]
  channels: OverviewRanking[]
}
export type OverviewRange =
  | { period: 'today' | '7d' | '30d' }
  | { from: number; to: number }
