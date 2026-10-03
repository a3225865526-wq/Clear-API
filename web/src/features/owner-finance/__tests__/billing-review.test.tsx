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
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { bindFinanceClient } from '../access'
import { BillingReconciliation } from '../billing-review'
import {
  billingDecisionSchema,
  type BillingReservation,
} from '../billing-review-types'

const row: BillingReservation = {
  id: 9,
  request_id: 'synthetic-interrupted-request',
  user_id: 21,
  token_id: 30,
  state: 'settled',
  reserved: 100,
  target: 100,
  applied: 100,
  updated_at: 1790899200,
  reason: 'upstream_usage_unknown',
  executing: false,
}
const originalAdapter = api.defaults.adapter
let client: QueryClient
let release: () => void
let rows: BillingReservation[]
let writes: InternalAxiosRequestConfig[]
let respond: (config: InternalAxiosRequestConfig) => unknown | Promise<unknown>
beforeEach(() => {
  useAuthStore.getState().auth.reset()
  useAuthStore.getState().auth.setBundle({
    access_token: 'synthetic-billing-review-access',
    token_type: 'Bearer',
    access_expires_at: 1900000000,
    user: { id: 12, role: 100, username: 'synthetic-review-owner', status: 1 },
    session: {
      sid: 'synthetic-review-session',
      current: true,
      login_method: 'synthetic',
      ip: '',
      user_agent: '',
      created_at: 1,
      last_active_at: 1,
      expires_at: 1900000000,
    },
  })
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  release = bindFinanceClient(client)
  rows = [{ ...row }]
  writes = []
  respond = (config) => {
    if (config.method === 'post') {
      writes.push(config)
      rows = []
      return { id: 1 }
    }
    return { items: rows, total: rows.length }
  }
  api.defaults.adapter = (async (config) => ({
    data: { success: true, data: await respond(config) },
    status: 200,
    statusText: 'OK',
    headers: {},
    config,
  })) as AxiosAdapter
})
afterEach(() => {
  cleanup()
  release()
  client.clear()
  useAuthStore.getState().auth.reset()
  api.defaults.adapter = originalAdapter
})
function mount(ready = true) {
  return render(
    <QueryClientProvider client={client}>
      <BillingReconciliation ready={ready} />
    </QueryClientProvider>
  )
}
async function openDecision() {
  fireEvent.click(
    await screen.findByRole('button', { name: 'Resolve reservation' })
  )
  return await screen.findByRole('dialog', { name: 'Billing decision' })
}
function completeForm(dialog: HTMLElement, refund = '40') {
  fireEvent.change(
    within(dialog).getByRole('textbox', { name: 'Refund quota units' }),
    { target: { value: refund } }
  )
  fireEvent.change(
    within(dialog).getByRole('textbox', { name: 'Reconciliation evidence' }),
    { target: { value: 'provider invoice checked' } }
  )
  fireEvent.click(within(dialog).getByRole('checkbox'))
}
describe('billing reservation review', () => {
  it('does not request or enable money operations before migration capability is ready', () => {
    mount(false)
    expect(screen.getByRole('status')).toHaveTextContent('approved migration')
    expect(
      screen.queryByRole('button', { name: 'Resolve reservation' })
    ).not.toBeInTheDocument()
    expect(writes).toHaveLength(0)
  })
  it('displays interrupted settled charges and disables a currently executing request', async () => {
    rows = [{ ...row, executing: true }]
    mount()
    expect(
      await screen.findByText('synthetic-interrupted-request · User #21')
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Resolve reservation' })
    ).toBeDisabled()
    expect(screen.getByText(/Request is still executing/)).toBeInTheDocument()
  })
  it('rejects decimal overdraw missing evidence and missing ended confirmation', () => {
    const schema = billingDecisionSchema(100)
    for (const refund of ['-1', '0.5', '101', '1e2', '']) {
      expect(
        schema.safeParse({ refund, reason: 'verified', ended: true }).success
      ).toBe(false)
    }
    expect(
      schema.safeParse({ refund: '40', reason: '', ended: true }).success
    ).toBe(false)
    expect(
      schema.safeParse({ refund: '40', reason: 'verified', ended: false })
        .success
    ).toBe(false)
    expect(
      schema.safeParse({ refund: '40', reason: '核'.repeat(167), ended: true })
        .success
    ).toBe(false)
    expect(
      schema.safeParse({ refund: '0', reason: 'verified', ended: true }).success
    ).toBe(true)
  })
  it('shows inline invalid refund feedback and sends no write', async () => {
    mount()
    const dialog = await openDecision()
    completeForm(dialog, '101')
    fireEvent.click(
      within(dialog).getByRole('button', { name: 'Review decision' })
    )
    expect(
      await within(dialog).findByText(
        'Enter whole quota units between zero and the charged quota.'
      )
    ).toBeInTheDocument()
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    expect(writes).toHaveLength(0)
  })
  it('requires a final confirmation before posting the exact snapshot and refreshes after success', async () => {
    mount()
    const dialog = await openDecision()
    completeForm(dialog)
    fireEvent.click(
      within(dialog).getByRole('button', { name: 'Review decision' })
    )
    const confirmation = await screen.findByRole('alertdialog', {
      name: 'Confirm billing decision',
    })
    expect(
      within(confirmation).getByText('Final charged quota: 60')
    ).toBeInTheDocument()
    expect(writes).toHaveLength(0)
    fireEvent.click(
      within(confirmation).getByRole('button', {
        name: 'Apply billing decision',
      })
    )
    expect(
      await screen.findByText('Billing decision saved.')
    ).toBeInTheDocument()
    expect(writes).toHaveLength(1)
    expect(JSON.parse(writes[0].data)).toEqual({
      expected_state: 'settled',
      expected_applied: 100,
      expected_target: 100,
      refund_quota: 40,
      reason: 'provider invoice checked',
      execution_ended: true,
    })
    await waitFor(() =>
      expect(
        screen.queryByRole('button', { name: 'Resolve reservation' })
      ).not.toBeInTheDocument()
    )
  })
  it('blocks a second submit while the money request is pending', async () => {
    let resolve!: (result: unknown) => void
    respond = (config) => {
      if (config.method === 'post') {
        writes.push(config)
        return new Promise((done) => {
          resolve = done
        })
      }
      return { items: rows, total: 1 }
    }
    mount()
    const dialog = await openDecision()
    completeForm(dialog)
    fireEvent.click(
      within(dialog).getByRole('button', { name: 'Review decision' })
    )
    const confirm = await screen.findByRole('button', {
      name: 'Apply billing decision',
    })
    fireEvent.click(confirm)
    await waitFor(() => expect(confirm).toBeDisabled())
    fireEvent.click(confirm)
    expect(writes).toHaveLength(1)
    await act(async () => resolve({ id: 1 }))
    expect(
      await screen.findByText('Billing decision saved.')
    ).toBeInTheDocument()
  })
  it('keeps failed decisions visible without claiming refund success or automatic retry', async () => {
    respond = (config) => {
      if (config.method === 'post') {
        writes.push(config)
        throw new AxiosError(
          'synthetic conflict',
          undefined,
          config,
          undefined,
          {
            data: { code: 'FINANCE_CONFLICT' },
            status: 409,
            statusText: 'Conflict',
            headers: {},
            config,
          }
        )
      }
      return { items: rows, total: 1 }
    }
    mount()
    const dialog = await openDecision()
    completeForm(dialog)
    fireEvent.click(
      within(dialog).getByRole('button', { name: 'Review decision' })
    )
    fireEvent.click(
      await screen.findByRole('button', { name: 'Apply billing decision' })
    )
    expect(await within(dialog).findByRole('alert')).toHaveTextContent(
      'Decision failed.'
    )
    expect(writes).toHaveLength(1)
    expect(
      screen.queryByText('Billing decision saved.')
    ).not.toBeInTheDocument()
  })
  it('history shows immutable evidence without offering another decision', async () => {
    rows = [
      {
        ...row,
        state: 'reviewed',
        applied: 60,
        decision: {
          id: 1,
          actor_id: 12,
          created_at: row.updated_at,
          original_state: 'settled',
          applied: 100,
          refund_quota: 40,
          final_quota: 60,
          reason: 'provider invoice checked',
          token_refund: 'credited',
        },
      },
    ]
    mount()
    fireEvent.change(screen.getByRole('combobox', { name: 'Review scope' }), {
      target: { value: 'reviewed' },
    })
    fireEvent.click(
      await screen.findByRole('button', { name: 'View billing decision' })
    )
    const dialog = await screen.findByRole('dialog')
    expect(
      within(dialog).getByText('provider invoice checked')
    ).toBeInTheDocument()
    expect(
      within(dialog).queryByRole('button', { name: 'Review decision' })
    ).not.toBeInTheDocument()
  })
  it('shows list failure with retry instead of an empty success', async () => {
    respond = () => {
      throw new Error('synthetic read failure')
    }
    mount()
    expect(
      await screen.findByText('Reservation read failed')
    ).toBeInTheDocument()
    expect(
      screen.queryByText('No reservations in this review scope.')
    ).not.toBeInTheDocument()
  })
})
