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
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { getPoolAccountCapabilities, getPoolAccountQuota } from '../api'
import type { PoolQuota } from '../types'
import {
  AccountQuotaPanel,
  AccountModelAdmissionPanel,
  QuotaSummary,
} from './account-observations'

vi.mock('../api', () => ({
  getPoolAccountCapabilities: vi.fn(),
  getPoolAccountQuota: vi.fn(),
}))
const snapshot: PoolQuota = {
  query_capability: 'observations_only',
  result: 'failed',
  checked_at: 1000,
  next_refresh_at: 2000,
  stale: true,
  metrics: [
    {
      kind: 'remaining_percent',
      value: 75,
      unit: '%',
      scope: 'shared_pool',
      pool: 'primary',
      source: 'upstream_derived',
      updated_at: 1000,
      stale: true,
      query_result: 'failed',
      window_seconds: 18000,
      reset_at: 2000,
    },
  ],
}
const mount = (component: React.ReactNode) =>
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      {component}
    </QueryClientProvider>
  )
describe('pool observations', () => {
  it('retains and marks previous data stale when the refresh endpoint fails', async () => {
    const now = Math.floor(Date.now() / 1000)
    vi.mocked(getPoolAccountQuota)
      .mockResolvedValueOnce({
        ...snapshot,
        result: 'success',
        stale: false,
        metrics: snapshot.metrics.map((metric) => ({
          ...metric,
          updated_at: now,
          reset_at: now + 7200,
          stale: false,
          query_result: 'success',
        })),
      })
      .mockRejectedValueOnce(new Error('fixed transport failure'))
    mount(<AccountQuotaPanel accountId={3} />)
    await screen.findByText('Source: Derived from upstream data')
    expect(screen.queryByText('Stale')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Refresh quota' }))
    expect(
      await screen.findByText('Quota query failed; previous data is retained')
    ).toBeInTheDocument()
    expect(screen.getAllByText('Stale').length).toBe(2)
    expect(
      screen.getByText('Source: Derived from upstream data')
    ).toBeInTheDocument()
  })
  it('ages cached data without querying the upstream again', () => {
    vi.useFakeTimers()
    try {
      const now = Math.floor(Date.now() / 1000)
      render(
        <QuotaSummary
          quota={{
            ...snapshot,
            result: 'success',
            stale: false,
            metrics: snapshot.metrics.map((metric) => ({
              ...metric,
              stale: false,
              updated_at: now,
              reset_at: now + 7200,
              query_result: 'success',
            })),
          }}
        />
      )
      expect(screen.queryByText('Stale')).not.toBeInTheDocument()
      act(() => vi.advanceTimersByTime(360_000))
      expect(screen.getByText('Stale')).toBeInTheDocument()
      expect(getPoolAccountQuota).not.toHaveBeenCalled()
    } finally {
      vi.useRealTimers()
    }
  })
  it('keeps unknown distinct from zero and unsupported', () => {
    const view = render(<QuotaSummary />)
    expect(screen.getByText('Unknown')).toBeInTheDocument()
    expect(screen.queryByText(/0 %/)).not.toBeInTheDocument()
    view.rerender(
      <QuotaSummary
        quota={{ ...snapshot, result: 'unsupported', metrics: [] }}
      />
    )
    expect(
      screen.getByText('Quota query is not supported yet')
    ).toBeInTheDocument()
  })
  it('shows retained shared window, provenance and failed freshness with manual refresh', async () => {
    vi.mocked(getPoolAccountQuota).mockResolvedValue(snapshot)
    mount(<AccountQuotaPanel accountId={1} />)
    expect(
      await screen.findByText('Quota query failed; previous data is retained')
    ).toBeInTheDocument()
    expect(
      screen.getByText('Source: Derived from upstream data')
    ).toBeInTheDocument()
    expect(screen.getByText('Scope: Shared pool')).toBeInTheDocument()
    expect(screen.getAllByText('Stale').length).toBe(2)
    const button = screen.getByRole('button', { name: 'Refresh quota' })
    expect(button).toHaveClass('min-h-11')
    fireEvent.click(button)
    await waitFor(() => expect(getPoolAccountQuota).toHaveBeenCalledTimes(2))
    expect(
      screen.queryByRole('button', { name: /reset/i })
    ).not.toBeInTheDocument()
  })
  it('only inspects an exact submitted model, keeps unverified capabilities closed', async () => {
    vi.mocked(getPoolAccountCapabilities).mockResolvedValue({
      platform: { platform: 'codex', label: 'Codex', access_types: [] },
      access_type: null,
      account_status: 'not_admitted',
      model_admission: {
        platform: 'codex',
        access_type: 'oauth_browser',
        model: 'synthetic-model',
        adapter_version: 1,
        open: false,
        reasons: ['continuous_isolation_unverified'],
        metering_source: 'unknown',
        quota_capability: 'observations_only',
        capabilities: [
          {
            capability: 'tools',
            status: 'unverified',
            native: false,
            evidence: [],
          },
        ],
      },
    })
    mount(<AccountModelAdmissionPanel accountId={2} />)
    fireEvent.change(screen.getByLabelText('Exact model identifier'), {
      target: { value: 'synthetic-model' },
    })
    expect(getPoolAccountCapabilities).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Inspect admission' }))
    expect(
      await screen.findByText('External channel closed')
    ).toBeInTheDocument()
    expect(screen.getByText('Tool calls: Unverified')).toBeInTheDocument()
    expect(
      screen.getByText('No model-specific validation evidence')
    ).toBeInTheDocument()
  })
})
