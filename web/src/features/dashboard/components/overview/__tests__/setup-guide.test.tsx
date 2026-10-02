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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import en from '@/i18n/locales/en.json'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import type { ConsoleDashboardData } from '../console-api'
import { OverviewDashboard } from '../overview-dashboard'

let client: QueryClient
const usage = {
  requests: 0,
  quota: 0,
  input: 0,
  output: 0,
  average_seconds: null,
}
const fixture: ConsoleDashboardData = {
  read_at: 1790812800,
  timezone: 'Asia/Shanghai',
  start: 1790294400,
  end: 1790812800,
  balance: 0,
  account_quota: 0,
  account_requests: 0,
  keys: 0,
  enabled_keys: 0,
  today: usage,
  retained: usage,
  minute: usage,
  models: [],
  platforms: [],
  trend: [],
  recent: [],
}

beforeEach(() => {
  i18next.addResourceBundle('en', 'translation', en.translation, true, true)
  useAuthStore
    .getState()
    .auth.setUser({ id: 1, username: 'dashboard-user', role: 1 })
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: fixture },
  })
})
afterEach(() => {
  client.clear()
  useAuthStore.setState(useAuthStore.getInitialState(), true)
})
async function renderOverview() {
  const router = createRouter({
    routeTree: createRootRoute({ component: OverviewDashboard }),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  await router.load()
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

describe('dashboard guide and account usage', () => {
  it('offers retained business links and separates zero usage from unknown response time', async () => {
    await renderOverview()
    expect(
      await screen.findByText('Welcome back, dashboard-user')
    ).toBeVisible()
    expect(await screen.findByText('0 RPM')).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'View usage guide' })
    ).toHaveAttribute('href', '/usage-guide?article=%2F')
    expect(
      screen.getByRole('button', { name: 'Manage API keys' })
    ).toHaveAttribute('href', '/keys')
    expect(
      screen.getByRole('button', { name: 'Redeem a code in wallet' })
    ).toHaveAttribute('href', '/wallet')
    expect(screen.queryByText('Playground')).not.toBeInTheDocument()
    expect(screen.getAllByText('No data').length).toBeGreaterThan(0)
    expect(api.get).toHaveBeenCalledWith(
      '/api/console/dashboard',
      expect.objectContaining({ params: { days: 7 } })
    )
  })
  it('updates the selected range through the account-only endpoint', async () => {
    await renderOverview()
    await screen.findByText('0 RPM')
    await userEvent.click(screen.getByRole('combobox'))
    await userEvent.click(
      await screen.findByRole('option', { name: 'Last 15 days' })
    )
    await waitFor(() =>
      expect(api.get).toHaveBeenCalledWith(
        '/api/console/dashboard',
        expect.objectContaining({ params: { days: 15 } })
      )
    )
    expect(screen.getByText('0 RPM')).toBeVisible()
  })
  it('preserves valid data and offers retry after a refresh fails', async () => {
    await renderOverview()
    await screen.findByText('0 RPM')
    vi.mocked(api.get).mockRejectedValueOnce(new Error('offline'))
    await userEvent.click(screen.getByRole('button', { name: 'Refresh' }))
    expect(await screen.findByText('Unable to load data')).toBeVisible()
    expect(screen.getByText('0 RPM')).toBeVisible()
    expect(screen.getByRole('button', { name: 'Retry' })).toBeEnabled()
  })
})
