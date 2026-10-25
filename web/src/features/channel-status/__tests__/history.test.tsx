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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import en from '@/i18n/locales/en.json'
import { api } from '@/lib/api'

import type { Monitor } from '../api'
import { MonitorHistory } from '../monitor-history'

let client: QueryClient
const monitor: Monitor = {
  id: 1,
  model: 'isolated-model',
  status: 'unknown',
  latest: null,
  history: [],
  windows: {},
  running: { attempt_id: 7, started_at: 1790812800, unresolved: false },
}
const items = [
  {
    id: 4,
    checked_at: 1790812800,
    status: 'blocked',
    error_code: 'MONITOR_COST_UNVERIFIED',
    latency_ms: null,
    task_id: 'task-blocked',
  },
  {
    id: 3,
    checked_at: 1790812799,
    status: 'skipped',
    error_code: 'MONITOR_PAUSED',
    latency_ms: null,
    task_id: 'task-skipped',
  },
  {
    id: 2,
    checked_at: 1790812798,
    status: 'error',
    error_code: 'probe_failed',
    latency_ms: 12,
    task_id: 'task-failed',
  },
  {
    id: 1,
    checked_at: 1790812797,
    status: 'normal',
    error_code: '',
    latency_ms: 15,
    task_id: 'task-success',
  },
]
beforeEach(() => {
  i18next.addResourceBundle('en', 'translation', en.translation, true, true)
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  vi.spyOn(api, 'get').mockImplementation(async (_path, options) => ({
    data: {
      success: true,
      data: {
        items: options?.params?.page === 2 ? [] : items,
        total: 21,
        page: options?.params?.page,
        page_size: 20,
        timezone: 'Asia/Shanghai',
      },
    },
  }))
})
afterEach(() => client.clear())
function mount() {
  render(
    <QueryClientProvider client={client}>
      <MonitorHistory monitor={monitor} />
    </QueryClientProvider>
  )
}

describe('monitoring history', () => {
  it('shows persisted states, safe reasons and running status, then queries the next page', async () => {
    mount()
    expect(await screen.findByText('Blocked')).toBeVisible()
    expect(screen.getByText('Not executed')).toBeVisible()
    expect(screen.getByText('Failed')).toBeVisible()
    expect(screen.getByText('Normal')).toBeVisible()
    expect(
      screen.getByText('Supplier cost evidence is missing or expired')
    ).toBeVisible()
    expect(screen.getByRole('status')).toHaveTextContent('Running')
    expect(screen.getByText('task-blocked')).toBeVisible()
    await userEvent.click(
      screen.getByRole('button', { name: 'Go to next page' })
    )
    await waitFor(() =>
      expect(api.get).toHaveBeenLastCalledWith(
        '/api/console/channel-status/1/history',
        expect.objectContaining({
          params: expect.objectContaining({ page: 2 }),
        })
      )
    )
    expect(await screen.findByText('No detection history')).toBeVisible()
  })
  it('resets pagination when filtering dates and blocks reversed date ranges', async () => {
    mount()
    await screen.findByText('Blocked')
    fireEvent.change(screen.getByLabelText('Start date'), {
      target: { value: '2026-10-01' },
    })
    await waitFor(() =>
      expect(api.get).toHaveBeenLastCalledWith(
        expect.any(String),
        expect.objectContaining({
          params: expect.objectContaining({ page: 1, from: '2026-10-01' }),
        })
      )
    )
    fireEvent.change(screen.getByLabelText('End date'), {
      target: { value: '2026-09-30' },
    })
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Invalid date range'
    )
    expect(screen.getByLabelText('End date')).toHaveAttribute(
      'aria-invalid',
      'true'
    )
  })
  it('shows a query failure with a retry instead of presenting an empty success', async () => {
    vi.mocked(api.get).mockRejectedValueOnce(
      new Error('isolated history failure')
    )
    mount()
    expect(await screen.findByText('Unable to load data')).toBeVisible()
    await userEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByText('Blocked')).toBeVisible()
  })
})
