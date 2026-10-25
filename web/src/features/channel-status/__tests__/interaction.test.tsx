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
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { act, render, screen, within, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import en from '@/i18n/locales/en.json'
import { api } from '@/lib/api'

import { ChannelStatusPage } from '..'
import { aggregateMonitorStatus, type MonitoringData } from '../api'

let client: QueryClient
const windowStats = {
  percent: 50,
  samples: 2,
  first_sample: 1790812800,
  average_ms: 120,
}
const model = {
  id: 1,
  model: 'model-alpha',
  status: 'normal',
  latest: {
    id: 1,
    checked_at: 1790812800,
    status: 'normal',
    latency_ms: 120,
    endpoint_ms: null,
    error_code: '',
  },
  history: [],
  windows: {
    '7': windowStats,
    '15': { ...windowStats, percent: 75 },
    '30': { ...windowStats, percent: 80 },
  },
}
const fixture: MonitoringData = {
  ready: true,
  read_at: Math.floor(Date.now() / 1000),
  stale_after_seconds: 900,
  channels: [
    {
      id: 10,
      name: 'First channel',
      platform: 'Provider A',
      models: [
        model,
        { ...model, id: 2, model: 'model-beta', status: 'unknown' },
      ],
    },
    {
      id: 11,
      name: 'Second channel',
      platform: 'Provider B',
      models: [{ ...model, id: 3, model: 'model-gamma' }],
    },
  ],
}

beforeEach(() => {
  i18next.addResourceBundle('en', 'translation', en.translation, true, true)
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: fixture },
  })
})
afterEach(() => {
  client.clear()
  vi.useRealTimers()
})
async function renderPage() {
  const router = createRouter({
    routeTree: createRootRoute({ component: ChannelStatusPage }),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  await router.load()
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

describe('channel monitoring interaction', () => {
  it('keeps channel status controls while hiding scheduler metadata returned by the API', async () => {
    vi.mocked(api.get).mockResolvedValue({
      data: {
        success: true,
        data: {
          ...fixture,
          schedule: {
            timezone: 'Asia/Shanghai',
            interval_seconds: 86400,
            execution_enabled: false,
            last_run_at: 1790812800,
            next_run_at: 1790899200,
            task_status: 'succeeded',
          },
        },
      },
    })
    await renderPage()
    expect(
      await screen.findByRole('button', { name: 'View First channel details' })
    ).toBeVisible()
    expect(screen.getByRole('button', { name: 'Refresh' })).toBeVisible()
    expect(screen.getByRole('tab', { name: '7 days' })).toBeVisible()
    expect(
      screen.queryByText(/Daily monitoring schedule/)
    ).not.toBeInTheDocument()
    expect(
      screen.queryByText(/Monitoring execution paused/)
    ).not.toBeInTheDocument()
    expect(screen.queryByText(/Last scheduler run/)).not.toBeInTheDocument()
    expect(screen.queryByText(/Next scheduler run/)).not.toBeInTheDocument()
  })

  it('stops automatic requests when the page is unmounted', async () => {
    const view = await renderPage()
    await screen.findByRole('button', { name: 'View First channel details' })
    vi.useFakeTimers()
    vi.mocked(api.get).mockClear()
    view.unmount()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(120000)
    })
    expect(api.get).not.toHaveBeenCalled()
  })
  it('shows unknown when there are no samples instead of claiming normal operation', async () => {
    vi.mocked(api.get).mockResolvedValue({
      data: { success: true, data: { ...fixture, ready: false, channels: [] } },
    })
    await renderPage()
    expect(await screen.findByText('No detection history')).toBeVisible()
    expect(screen.getByText('Overall · Unknown')).toBeVisible()
    expect(aggregateMonitorStatus(['normal', 'unknown'])).toBe('unknown')
    expect(aggregateMonitorStatus(['normal', 'error'])).toBe('error')
  })
  it('shows the selected channel models and all windows, then restores focus after Escape', async () => {
    await renderPage()
    const trigger = await screen.findByRole('button', {
      name: 'View First channel details',
    })
    await userEvent.click(trigger)
    const dialog = await screen.findByRole('dialog', { name: 'First channel' })
    expect(within(dialog).getByText('model-alpha')).toBeVisible()
    expect(within(dialog).getByText('model-beta')).toBeVisible()
    expect(within(dialog).queryByText('model-gamma')).not.toBeInTheDocument()
    expect(within(dialog).getAllByText('75%')).toHaveLength(2)
    await userEvent.keyboard('{Escape}')
    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    )
    await waitFor(() => expect(trigger).toHaveFocus())
    await userEvent.click(
      screen.getByRole('button', { name: 'View Second channel details' })
    )
    const second = await screen.findByRole('dialog', { name: 'Second channel' })
    expect(within(second).getByText('model-gamma')).toBeVisible()
    await userEvent.click(
      within(second).getByRole('button', {
        name: 'Close channel details',
      })
    )
    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    )
  })
  it('changes card availability without changing the three detail windows', async () => {
    await renderPage()
    await screen.findByRole('button', { name: 'View First channel details' })
    await userEvent.click(screen.getByRole('tab', { name: '15 days' }))
    expect(screen.getAllByText('75%')).toHaveLength(2)
    await userEvent.click(
      screen.getByRole('button', { name: 'View First channel details' })
    )
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getAllByText('50%')).toHaveLength(2)
    expect(within(dialog).getAllByText('80%')).toHaveLength(2)
  })
  it('retains cards and marks status unknown after a failed refresh', async () => {
    await renderPage()
    await screen.findByRole('button', { name: 'View First channel details' })
    vi.mocked(api.get).mockRejectedValueOnce(new Error('offline'))
    await userEvent.click(screen.getByRole('button', { name: 'Refresh' }))
    expect(await screen.findByText('Unable to load data')).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'View First channel details' })
    ).toBeVisible()
    expect(screen.getByText('Overall · Unknown')).toBeVisible()
  })
})
