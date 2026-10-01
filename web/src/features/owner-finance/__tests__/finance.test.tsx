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
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import {
  AxiosError,
  type AxiosAdapter,
  type InternalAxiosRequestConfig,
} from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useSidebarData } from '@/hooks/use-sidebar-data'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { OwnerFinance } from '..'
import { checkFinanceAccess, financeKey, financeRequest } from '../access'
import { beijingInput, financeMoney, validRange } from '../format'
import { metricLabels } from '../labels'
import { buildOperation, operationSchema, type Values } from '../operation-form'
import type { Summary } from '../types'

export function syntheticSummary(): Summary {
  const metrics = Object.fromEntries(
    Object.keys(metricLabels).map((key) => [
      key,
      {
        total: null,
        recorded_total:
          key === 'operating_expenses'
            ? null
            : ({ operating_profit: '-26' }[key] ?? '0'),
        known_subtotal:
          { operating_profit: '-26', consumption_revenue: '20' }[key] ?? '0',
        unknown_items: key === 'operating_expenses' ? 1 : 0,
        status: 'partial',
        coverage: ['site_sources_not_fully_connected'],
        evidence_level: 'recorded_facts',
      },
    ])
  )
  const from = beijingInput('2026-10-01T00:00'),
    to = beijingInput('2026-10-02T00:00')
  return {
    from,
    to,
    metrics,
    currency: 'CNY',
    timezone: 'Asia/Shanghai',
    status: 'partial',
    complete_site_profit: false,
    profit_margin: null,
    recorded_profit_margin: null,
    snapshot_sha256: 'a'.repeat(64),
    read_at: from + 3600,
    schema_version: 9,
    audit_watermark_id: 11,
    wallet_versions: { 12: 3 },
    sync_health: {
      counts: { pending: 1 },
      needs_review_classes: { unknown_fx: 1 },
      watermark_id: 10,
      oldest_pending_age_seconds: 20,
      unimported_settlement_sample: 1,
      warnings: [],
    },
    capture_health: { warnings: [] },
    supplier_checks: [],
    dimensions: [{ dimension: 'channel', key: 'unattributed', metrics }],
    trend: [{ from, to, metrics }],
    warnings: ['expense_absence_is_not_confirmed_zero'],
    facts: [
      {
        metric: 'operating_expenses',
        table: 'finance_expenses',
        id: 1,
        related_id: 0,
        from,
        to,
        amount_cny: null,
        status: 'unknown_fx',
        channel_id: 0,
        upstream_model: '',
        pool_key: '',
        posting_kind: '',
      },
    ],
  }
}
function login(id = 12, role = 100) {
  useAuthStore.getState().auth.setBundle({
    access_token: 'synthetic-finance-session',
    token_type: 'Bearer',
    access_expires_at: 1900000000,
    user: { id, role, username: 'synthetic-owner', status: 1 },
    session: {
      sid: `synthetic-${id}`,
      current: true,
      login_method: 'synthetic',
      ip: '',
      user_agent: '',
      created_at: 1,
      last_active_at: 1,
      expires_at: 1900000000,
    },
  })
}
const originalAdapter = api.defaults.adapter
let client: QueryClient
let endpoint: (config: InternalAxiosRequestConfig) => unknown | Promise<unknown>
let calls: InternalAxiosRequestConfig[]
function reply(config: InternalAxiosRequestConfig, data: unknown) {
  return {
    data: { success: true, data },
    status: 200,
    statusText: 'OK',
    headers: { 'Cache-Control': 'no-store' },
    config,
  }
}
function reject(config: InternalAxiosRequestConfig, status: number): never {
  throw new AxiosError('synthetic', undefined, config, undefined, {
    data: {},
    status,
    statusText: '',
    headers: {},
    config,
  })
}
function mount(node = <OwnerFinance />) {
  return render(
    <QueryClientProvider client={client}>{node}</QueryClientProvider>
  )
}
beforeEach(() => {
  useAuthStore.getState().auth.reset()
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  calls = []
  endpoint = (config) =>
    config.url?.endsWith('/capability')
      ? {
          can_access: true,
          schema_ready: true,
          supplier_bill_lines_ready: true,
          profit_summary_available: true,
        }
      : syntheticSummary()
  api.defaults.adapter = (async (config) => {
    calls.push(config)
    return reply(config, await endpoint(config))
  }) as AxiosAdapter
})
afterEach(() => {
  cleanup()
  client.clear()
  useAuthStore.getState().auth.reset()
  api.defaults.adapter = originalAdapter
  vi.restoreAllMocks()
})

describe('finance semantics', () => {
  it('preserves decimal CNY, negatives, genuine zero and unknown across interface locales', () => {
    expect(financeMoney('9999999999999999.123456789012', 'zhCN')).toBe(
      'CNY 9,999,999,999,999,999.123456789012'
    )
    expect(financeMoney('-26', 'zhTW')).toBe('CNY −26.00')
    expect(financeMoney('0', 'en')).toBe('CNY 0.00')
    expect(financeMoney(null, 'zhCN')).toBeNull()
    expect(financeMoney('1e6', 'en')).toBeNull()
  })
  it('uses Beijing half-open ranges and rejects impossible dates and oversized periods', () => {
    expect(beijingInput('2026-10-01T00:00')).toBe(
      Date.parse('2026-09-30T16:00:00Z') / 1000
    )
    expect(beijingInput('2026-02-30T00:00')).toBeNaN()
    expect(beijingInput('invalid')).toBeNaN()
    const from = beijingInput('2026-10-01T00:00')
    expect(validRange(from, from)).toBe(false)
    expect(validRange(from, from + 366 * 86400)).toBe(true)
    expect(validRange(from, from + 366 * 86400 + 1)).toBe(false)
  })
  it('validates exact correction scope, immutable FX and positive credit magnitudes', () => {
    const values: Values = {
      event_key: 'correction-1',
      ledger: 'operating',
      category: 'hosting',
      pool_key: '',
      account_ref: '',
      amount: '-2.123456789012',
      currency: 'CNY',
      fx_version_id: '0',
      from: '2026-10-01T00:00',
      to: '2026-10-02T00:00',
      recurrence: 'once',
      adjusts_id: '1',
      evidence: 'synthetic-invoice',
      note: 'synthetic correction',
    }
    expect(operationSchema('expense').safeParse(values).success).toBe(true)
    expect(buildOperation('expense', values).body).toMatchObject({
      amount: '-2.123456789012',
      adjusts_id: 1,
      source: 'synthetic-invoice',
    })
    expect(
      operationSchema('expense').safeParse({ ...values, adjusts_id: '0' })
        .success
    ).toBe(false)
    expect(
      operationSchema('expense').safeParse({ ...values, fx_version_id: '5' })
        .success
    ).toBe(false)
    expect(
      operationSchema('expense').safeParse({ ...values, note: '' }).success
    ).toBe(false)
    const line = {
      ...values,
      amount: '2',
      line_category: 'credit',
      bill_id: '3',
      channel_id: '1',
      upstream_model: '',
      reference: '',
      replaces_line_id: '0',
      references_line_id: '9',
    }
    expect(operationSchema('line').safeParse(line).success).toBe(true)
    expect(buildOperation('line', line).body).toMatchObject({
      category: 'credit',
      amount: '2',
      references_line_id: 9,
    })
    expect(
      operationSchema('line').safeParse({ ...line, amount: '-2' }).success
    ).toBe(false)
    expect(
      operationSchema('offline').safeParse({
        user_id: '1',
        ids: '1,1',
        expected_revision: '0',
        evidence: 'test',
      }).success
    ).toBe(false)
  })
})
describe('owner finance isolation and daily workflow', () => {
  it('separates partial, known subtotal, negative result, true zero and unknown; no storage writes', async () => {
    login()
    const storage = vi.spyOn(Storage.prototype, 'setItem')
    mount()
    expect(
      await screen.findByText('Partial coverage · full site profit unavailable')
    ).toBeVisible()
    expect(screen.getAllByText('CNY −26.00')[0]).toBeVisible()
    expect(screen.getAllByText('CNY 0.00').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Unknown').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Not calculable')).toHaveLength(2)
    fireEvent.click(
      screen.getByRole('button', { name: 'Inspect facts: Operating profit' })
    )
    expect(await screen.findByRole('dialog')).toHaveTextContent(
      'Missing frozen FX'
    )
    expect(storage).not.toHaveBeenCalled()
    expect(
      calls.every((call) => call.skipAuthRefresh && call.skipErrorHandler)
    ).toBe(true)
  })
  it.each([
    [7, 100],
    [8, 10],
    [9, 1],
  ])(
    'server denies another root/admin/user %i without requesting profit',
    async (id, role) => {
      login(id, role)
      endpoint = (config) => reject(config, 403)
      mount()
      expect(
        await screen.findByText('Owner finance access unavailable')
      ).toBeVisible()
      expect(calls.some((call) => call.url?.endsWith('/profit-summary'))).toBe(
        false
      )
    }
  )
  it('anonymous clients issue no finance requests, and route capability fails closed', async () => {
    mount()
    expect(screen.getByText('Owner finance access unavailable')).toBeVisible()
    await expect(checkFinanceAccess()).rejects.toThrow()
    expect(calls).toHaveLength(0)
  })
  it.each([401, 403])(
    'clears displayed facts, previews and caches on HTTP %i',
    async (status) => {
      login()
      mount()
      await screen.findByText('Partial coverage · full site profit unavailable')
      endpoint = (config) =>
        config.url?.endsWith('/profit-summary')
          ? reject(config, status)
          : {
              can_access: true,
              supplier_bill_lines_ready: true,
              profit_summary_available: true,
            }
      fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))
      await screen.findByText('Owner finance access unavailable')
      expect(screen.queryByText('CNY −26.00')).not.toBeInTheDocument()
      expect(
        client
          .getQueriesData({ queryKey: financeKey })
          .some(([, data]) => (data as Summary)?.metrics)
      ).toBe(false)
    }
  )
  it('aborts late owner responses when switching identities and never restores their facts', async () => {
    login()
    let resolve!: (data: Summary) => void
    endpoint = (config) =>
      config.url?.endsWith('/profit-summary')
        ? new Promise<Summary>((done) => {
            resolve = done
          })
        : {
            can_access: useAuthStore.getState().auth.user?.id === 12,
            supplier_bill_lines_ready: true,
            profit_summary_available: true,
          }
    mount()
    await waitFor(() =>
      expect(calls.some((call) => call.url?.endsWith('/profit-summary'))).toBe(
        true
      )
    )
    const request = calls.find((call) => call.url?.endsWith('/profit-summary'))
    act(() => login(9, 1))
    await act(async () => {
      resolve(syntheticSummary())
      await Promise.resolve()
    })
    expect(request?.signal?.aborted).toBe(true)
    expect(
      await screen.findByText('Owner finance access unavailable')
    ).toBeVisible()
    expect(screen.queryByText('CNY −26.00')).not.toBeInTheDocument()
    expect(
      client
        .getQueriesData({ queryKey: financeKey })
        .some(([, data]) => (data as Summary)?.metrics)
    ).toBe(false)
  })
  it('clears all local finance state on logout and permission changes', async () => {
    login()
    mount()
    await screen.findByText('Partial coverage · full site profit unavailable')
    act(() => useAuthStore.getState().auth.reset())
    await screen.findByText('Owner finance access unavailable')
    expect(
      screen.queryByRole('tab', { name: 'Finance operations' })
    ).not.toBeInTheDocument()
    act(() => login())
    await screen.findByText('Partial coverage · full site profit unavailable')
    endpoint = (config) => reject(config, 403)
    act(() =>
      useAuthStore.getState().auth.setUser({
        id: 12,
        role: 1,
        username: 'synthetic-owner',
        status: 1,
      })
    )
    await screen.findByText('Owner finance access unavailable')
    expect(
      client
        .getQueriesData({ queryKey: financeKey })
        .some(([, data]) => (data as Summary)?.metrics)
    ).toBe(false)
  })
  it('shows the menu only after server capability, and route preflight reads no financial facts', async () => {
    function Menu() {
      const data = useSidebarData()
      return (
        <div>
          {data.navGroups
            .flatMap((group) => group.items)
            .filter((item) => item.title === 'Owner finance')
            .map((item) => (
              <span key={item.title}>{item.title}</span>
            ))}
        </div>
      )
    }
    login()
    mount(<Menu />)
    expect(await screen.findByText('Owner finance')).toBeVisible()
    expect(await checkFinanceAccess()).toBe(true)
    expect(calls.every((call) => call.url?.endsWith('/capability'))).toBe(true)
    act(() => useAuthStore.getState().auth.reset())
    await waitFor(() =>
      expect(screen.queryByText('Owner finance')).not.toBeInTheDocument()
    )
  })
  it('saves a synthetic expense only after review and rereads the profit snapshot', async () => {
    login()
    mount()
    await screen.findByText('Partial coverage · full site profit unavailable')
    fireEvent.click(screen.getByRole('tab', { name: 'Finance operations' }))
    const fill = (label: string, value: string) =>
      fireEvent.change(screen.getByLabelText(label), { target: { value } })
    fill('Unique event key', 'synthetic-expense-1')
    fill('Expense category', 'hosting')
    fill('Original amount, exact decimal', '14.000000000001')
    fill('Attribution start, Beijing time', '2026-10-01T00:00')
    fill('Attribution end, exclusive, Beijing time', '2026-10-02T00:00')
    fill('Evidence reference (no credentials)', 'synthetic-invoice')
    fireEvent.click(screen.getByRole('button', { name: 'Review registration' }))
    const dialog = await screen.findByRole('alertdialog')
    expect(calls.some((call) => call.url?.endsWith('/expenses'))).toBe(false)
    fireEvent.click(
      within(dialog).getByRole('button', { name: 'Confirm finance write' })
    )
    await waitFor(() =>
      expect(
        calls.filter((call) => call.url?.endsWith('/profit-summary')).length
      ).toBe(2)
    )
    expect(
      JSON.parse(calls.find((call) => call.url?.endsWith('/expenses'))?.data)
    ).toMatchObject({
      amount: '14.000000000001',
      currency: 'CNY',
      source: 'synthetic-invoice',
      period_from: beijingInput('2026-10-01T00:00'),
    })
  })
  it('requires a server import preview and explicit apply, then preserves the selected revision', async () => {
    login()
    mount()
    await screen.findByText('Partial coverage · full site profit unavailable')
    endpoint = (config) =>
      config.url?.endsWith('/preview')
        ? {
            sources: [
              { observation_id: 22, status: 'ready', event: { quota: 3 } },
            ],
            warnings: ['captured_settlements_only'],
          }
        : syntheticSummary()
    fireEvent.click(screen.getByRole('tab', { name: 'Finance operations' }))
    fireEvent.change(screen.getByLabelText('Operation'), {
      target: { value: 'consumption' },
    })
    for (const [label, value] of [
      ['Wallet user ID', '12'],
      ['Selected fact IDs, comma separated', '22'],
      ['Expected wallet revision', '3'],
      ['Evidence reference (no credentials)', 'synthetic-review'],
    ]) {
      fireEvent.change(screen.getByLabelText(label), { target: { value } })
    }
    fireEvent.click(screen.getByRole('button', { name: 'Read import preview' }))
    await screen.findByText('Server preview · partial coverage')
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    expect(calls.some((call) => call.url?.endsWith('/import'))).toBe(false)
    fireEvent.click(
      screen.getByRole('button', { name: 'Review and apply selected facts' })
    )
    fireEvent.click(
      within(await screen.findByRole('alertdialog')).getByRole('button', {
        name: 'Confirm finance write',
      })
    )
    await waitFor(() =>
      expect(calls.some((call) => call.url?.endsWith('/import'))).toBe(true)
    )
    expect(
      JSON.parse(calls.find((call) => call.url?.endsWith('/import'))?.data)
    ).toEqual({
      observation_ids: [22],
      expected_revision: 3,
      evidence: 'synthetic-review',
    })
  })
  it('cancels a pending preview on logout without retaining evidence or posting an import', async () => {
    login()
    mount()
    await screen.findByText('Partial coverage · full site profit unavailable')
    let resolve!: (data: object) => void
    endpoint = (config) =>
      config.url?.endsWith('/preview')
        ? new Promise((done) => {
            resolve = done
          })
        : syntheticSummary()
    fireEvent.click(screen.getByRole('tab', { name: 'Finance operations' }))
    fireEvent.change(screen.getByLabelText('Operation'), {
      target: { value: 'offline' },
    })
    for (const [label, value] of [
      ['Wallet user ID', '12'],
      ['Selected fact IDs, comma separated', '22'],
      ['Evidence reference (no credentials)', 'synthetic-private-reference'],
    ]) {
      fireEvent.change(screen.getByLabelText(label), { target: { value } })
    }
    fireEvent.click(screen.getByRole('button', { name: 'Read import preview' }))
    await waitFor(() =>
      expect(calls.some((call) => call.url?.endsWith('/preview'))).toBe(true)
    )
    act(() => useAuthStore.getState().auth.reset())
    await act(async () => resolve({ sources: [] }))
    expect(
      await screen.findByText('Owner finance access unavailable')
    ).toBeVisible()
    expect(
      calls.find((call) => call.url?.endsWith('/preview'))?.signal?.aborted
    ).toBe(true)
    expect(
      screen.queryByText('synthetic-private-reference')
    ).not.toBeInTheDocument()
    expect(calls.some((call) => call.url?.endsWith('/import'))).toBe(false)
  })
  it('denies all direct finance calls after server revocation until identity changes', async () => {
    login()
    endpoint = (config) => reject(config, 403)
    await expect(financeRequest('/capability')).rejects.toThrow()
    const count = calls.length
    await expect(financeRequest('/profit-summary')).rejects.toThrow()
    expect(calls).toHaveLength(count)
  })
  it('rejects invalid custom dates before any summary request and sends Beijing timestamps for valid selection', async () => {
    login()
    mount()
    await screen.findByText('Partial coverage · full site profit unavailable')
    fireEvent.change(screen.getByLabelText('Period'), {
      target: { value: 'custom' },
    })
    fireEvent.change(screen.getByLabelText('Start, Beijing time (inclusive)'), {
      target: { value: '2026-10-02T00:00' },
    })
    fireEvent.change(screen.getByLabelText('End, Beijing time (exclusive)'), {
      target: { value: '2026-10-01T00:00' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Apply period' }))
    expect(
      await screen.findByText('Enter a valid Beijing period of up to 366 days.')
    ).toBeVisible()
    expect(
      calls.filter((call) => call.url?.endsWith('/profit-summary'))
    ).toHaveLength(1)
    fireEvent.change(screen.getByLabelText('End, Beijing time (exclusive)'), {
      target: { value: '2026-10-03T00:00' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Apply period' }))
    await waitFor(() =>
      expect(
        calls.filter((call) => call.url?.endsWith('/profit-summary'))
      ).toHaveLength(2)
    )
    expect(
      calls.filter((call) => call.url?.endsWith('/profit-summary'))[1].params
    ).toEqual({
      from: beijingInput('2026-10-02T00:00'),
      to: beijingInput('2026-10-03T00:00'),
      trend: 'day',
    })
  })
  it('applies a controlled rebuild using the server revision and fingerprint, with no automatic conflict retry', async () => {
    login()
    mount()
    await screen.findByText('Partial coverage · full site profit unavailable')
    endpoint = (config) => {
      if (config.url?.endsWith('/preview')) {
        return {
          expected_revision: 3,
          snapshot_hash: 'b'.repeat(64),
          current_remaining_quota: 8,
          rebuilt_remaining_quota: 8,
          warnings: [],
        }
      }
      if (config.url?.endsWith('/apply')) return reject(config, 409)
      return syntheticSummary()
    }
    fireEvent.click(screen.getByRole('tab', { name: 'Finance operations' }))
    fireEvent.change(screen.getByLabelText('Operation'), {
      target: { value: 'rebuild' },
    })
    for (const [label, value] of [
      ['Wallet user ID', '12'],
      ['Selected fact IDs, comma separated', '31'],
      ['Evidence reference (no credentials)', 'synthetic-rebuild'],
    ]) {
      fireEvent.change(screen.getByLabelText(label), { target: { value } })
    }
    fireEvent.click(screen.getByRole('button', { name: 'Read import preview' }))
    await screen.findByText('Server preview · partial coverage')
    fireEvent.click(
      screen.getByRole('button', { name: 'Review and apply selected facts' })
    )
    fireEvent.click(
      within(await screen.findByRole('alertdialog')).getByRole('button', {
        name: 'Confirm finance write',
      })
    )
    expect(
      await screen.findByText(
        'The ledger changed or the event already exists. Read current records and preview again; nothing was overwritten.'
      )
    ).toBeVisible()
    const requests = calls.filter((call) => call.url?.endsWith('/apply'))
    expect(requests).toHaveLength(1)
    expect(JSON.parse(requests[0].data)).toEqual({
      sync_event_ids: [31],
      expected_revision: 3,
      snapshot_hash: 'b'.repeat(64),
      evidence: 'synthetic-rebuild',
    })
    expect(
      screen.queryByText('Server preview · partial coverage')
    ).not.toBeInTheDocument()
  })
  it('discovers review IDs with owner filtering and excludes raw payloads from the cache', async () => {
    login()
    mount()
    await screen.findByText('Partial coverage · full site profit unavailable')
    endpoint = (config) =>
      config.url?.endsWith('/sync-events')
        ? {
            total: 1,
            items: [
              {
                id: 31,
                topic: 'consumption',
                user_id: 12,
                ref_id: 22,
                at: 100,
                status: 'needs_review',
                attempts: 2,
                error_class: 'out_of_order',
                payload: { private_marker: 'synthetic-do-not-cache-payload' },
              },
            ],
          }
        : syntheticSummary()
    fireEvent.click(screen.getByRole('tab', { name: 'Exceptions' }))
    fireEvent.change(screen.getByLabelText('Wallet user ID (optional)'), {
      target: { value: '12' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Read evidence' }))
    expect(await screen.findByText('#31')).toBeVisible()
    expect(screen.getByText('user_id: 12 · ref_id: 22')).toBeVisible()
    expect(
      calls.find((call) => call.url?.endsWith('/sync-events'))?.params
    ).toMatchObject({ user_id: '12', page_size: 10, p: 1 })
    expect(
      JSON.stringify(client.getQueriesData({ queryKey: financeKey }))
    ).not.toContain('synthetic-do-not-cache-payload')
    act(() => useAuthStore.getState().auth.reset())
    await screen.findByText('Owner finance access unavailable')
    expect(screen.queryByText('#31')).not.toBeInTheDocument()
  })
  it('keeps empty ledgers partial, and prevents summary reads before schema readiness', async () => {
    login()
    const empty = syntheticSummary()
    empty.facts = []
    endpoint = (config) =>
      config.url?.endsWith('/capability')
        ? {
            can_access: true,
            supplier_bill_lines_ready: true,
            profit_summary_available: true,
          }
        : empty
    const mounted = mount()
    expect(
      await screen.findByText('No registered facts in this period')
    ).toBeVisible()
    expect(
      screen.getByText('Partial coverage · full site profit unavailable')
    ).toBeVisible()
    mounted.unmount()
    act(() => login(13))
    calls = []
    endpoint = () => ({
      can_access: true,
      supplier_bill_lines_ready: false,
      profit_summary_available: true,
    })
    mount()
    expect(await screen.findByText('Finance schema is not ready')).toBeVisible()
    expect(calls.every((call) => call.url?.endsWith('/capability'))).toBe(true)
  })
})
