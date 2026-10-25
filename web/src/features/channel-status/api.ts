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
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

export interface Probe {
  id: number
  checked_at: number
  status: string
  latency_ms: number | null
  endpoint_ms: number | null
  error_code: string
  attempt_id?: number
  task_id?: string
}
export interface Availability {
  percent: number | null
  samples: number
  first_sample: number | null
  average_ms: number | null
}
export interface Monitor {
  id: number
  model: string
  status: string
  latest: Probe | null
  running?: { attempt_id: number; started_at: number; unresolved: boolean }
  history: Probe[]
  windows: Record<string, Availability>
}
export interface Channel {
  id: number
  name: string
  platform: string
  models: Monitor[]
  business_status?: number
}
export interface MonitoringData {
  ready: boolean
  read_at: number
  stale_after_seconds: number
  channels: Channel[]
  schedule?: {
    timezone: string
    interval_seconds: number
    execution_enabled: boolean
    last_run_at: number
    next_run_at: number
    task_status: string
  }
}

export async function getMonitoring(signal: AbortSignal) {
  const response = await api.get<{ success: boolean; data: MonitoringData }>(
    '/api/console/channel-status',
    { signal }
  )
  return requireServerSuccess(response.data).data
}

export function aggregateMonitorStatus(statuses: string[]): string {
  if (statuses.includes('error')) return 'error'
  if (statuses.includes('degraded')) return 'degraded'
  if (statuses.length === 0 || statuses.some((status) => status !== 'normal')) {
    return 'unknown'
  }
  return 'normal'
}

export interface MonitorHistoryData {
  items: Probe[]
  total: number
  page: number
  page_size: number
  timezone: string
}

export async function getMonitorHistory(
  id: number,
  page: number,
  size: number,
  from: string,
  to: string,
  signal: AbortSignal
) {
  const response = await api.get<{
    success: boolean
    data: MonitorHistoryData
  }>(`/api/console/channel-status/${id}/history`, {
    signal,
    params: { page, page_size: size, from, to },
  })
  return requireServerSuccess(response.data).data
}
