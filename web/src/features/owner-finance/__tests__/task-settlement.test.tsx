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
import {
  TaskSettlementReview,
  type TaskSettlement,
} from '../task-settlement-review'

const fixture: TaskSettlement = {
  id: 9,
  task_id: 24,
  request_id: 'synthetic-task-request',
  user_id: 12,
  original_quota: 100,
  target_quota: 140,
  state: 'needs_review',
  error_class: 'wallet_insufficient_or_limit',
  attempts: 1,
  log_state: '',
  log_attempts: 0,
  token_outcome: '',
}
const originalAdapter = api.defaults.adapter
let client: QueryClient
let release: () => void
let rows: TaskSettlement[]
let writes: InternalAxiosRequestConfig[]
let reply: (config: InternalAxiosRequestConfig) => unknown | Promise<unknown>
beforeEach(() => {
  useAuthStore.getState().auth.reset()
  useAuthStore.getState().auth.setBundle({
    access_token: 'synthetic-task-settlement-access',
    token_type: 'Bearer',
    access_expires_at: 1900000000,
    user: { id: 12, role: 100, username: 'synthetic-owner', status: 1 },
    session: {
      sid: 'synthetic-task-session',
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
  rows = [{ ...fixture }]
  writes = []
  reply = (config) =>
    config.method === 'post'
      ? { ...fixture, state: 'applied', log_state: 'delivered' }
      : { items: rows, total: rows.length }
  api.defaults.adapter = (async (config) => {
    if (config.method === 'post') writes.push(config)
    return {
      data: { success: true, data: await reply(config) },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }) as AxiosAdapter
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
      <TaskSettlementReview ready={ready} />
    </QueryClientProvider>
  )
}
async function prepare() {
  fireEvent.click(
    await screen.findByRole('button', { name: 'Inspect settlement retry' })
  )
  const dialog = await screen.findByRole('dialog', {
    name: 'Task settlement retry',
  })
  fireEvent.change(
    within(dialog).getByRole('textbox', { name: 'Reconciliation evidence' }),
    { target: { value: 'restored quota verified' } }
  )
  fireEvent.click(within(dialog).getByRole('checkbox'))
  fireEvent.click(
    within(dialog).getByRole('button', { name: 'Review settlement retry' })
  )
  return await screen.findByRole('alertdialog')
}
describe('task settlement operator review', () => {
  it('requires readiness before exposing operations', () => {
    mount(false)
    expect(screen.getByRole('status')).toHaveTextContent(
      'approved main and log migrations'
    )
    expect(
      screen.queryByRole('button', { name: 'Inspect settlement retry' })
    ).not.toBeInTheDocument()
    expect(writes).toHaveLength(0)
  })
  it('does not let the operator invent missing final facts', async () => {
    rows = [
      { ...fixture, target_quota: null, error_class: 'final_facts_missing' },
    ]
    mount()
    expect(await screen.findByText('Unknown')).toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Inspect settlement retry' })
    ).not.toBeInTheDocument()
  })
  it('validates evidence and confirmation before any write', async () => {
    mount()
    fireEvent.click(
      await screen.findByRole('button', { name: 'Inspect settlement retry' })
    )
    fireEvent.click(
      await screen.findByRole('button', { name: 'Review settlement retry' })
    )
    await waitFor(() =>
      expect(screen.getByRole('textbox')).toHaveAttribute(
        'aria-invalid',
        'true'
      )
    )
    expect(writes).toHaveLength(0)
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  })
  it('confirms and submits the exact frozen snapshot then refreshes', async () => {
    mount()
    const confirmation = await prepare()
    expect(writes).toHaveLength(0)
    fireEvent.click(
      within(confirmation).getByRole('button', {
        name: 'Retry frozen settlement',
      })
    )
    expect(await screen.findByRole('status')).toHaveTextContent(
      'Settlement retry completed'
    )
    expect(writes).toHaveLength(1)
    expect(JSON.parse(writes[0].data)).toEqual({
      expected_attempts: 1,
      expected_target: 140,
      expected_log_attempts: 0,
      log_only: false,
      reason: 'restored quota verified',
      confirmed: true,
    })
  })
  it('shows a rejected retry without a successful funding message or automatic replay', async () => {
    reply = (config) => {
      if (config.method === 'post') {
        throw new AxiosError(
          'synthetic conflict',
          'ERR_BAD_REQUEST',
          config,
          undefined,
          {
            status: 409,
            statusText: 'Conflict',
            headers: {},
            config,
            data: { success: false },
          }
        )
      }
      return { items: rows, total: rows.length }
    }
    mount()
    const confirmation = await prepare()
    fireEvent.click(
      within(confirmation).getByRole('button', {
        name: 'Retry frozen settlement',
      })
    )
    expect(await screen.findByRole('alert')).toHaveTextContent('Retry failed')
    expect(writes).toHaveLength(1)
    expect(
      screen.queryByText(/Settlement retry completed/)
    ).not.toBeInTheDocument()
  })
  it('uses the log-only retry for already settled funding', async () => {
    rows = [
      {
        ...fixture,
        state: 'applied',
        log_state: 'needs_review',
        log_attempts: 5,
      },
    ]
    mount()
    const confirmation = await prepare()
    expect(screen.getByText(/Funding is already settled/)).toBeInTheDocument()
    fireEvent.click(
      within(confirmation).getByRole('button', {
        name: 'Retry frozen settlement',
      })
    )
    await screen.findByRole('status')
    expect(JSON.parse(writes[0].data)).toMatchObject({
      log_only: true,
      expected_log_attempts: 5,
      expected_target: 140,
    })
  })
  it('disables the confirmation while the request is in flight', async () => {
    let finish: (value: unknown) => void = () => undefined
    reply = (config) =>
      config.method === 'post'
        ? new Promise((resolve) => {
            finish = resolve
          })
        : { items: rows, total: rows.length }
    mount()
    const confirmation = await prepare()
    const button = within(confirmation).getByRole('button', {
      name: 'Retry frozen settlement',
    })
    fireEvent.click(button)
    await waitFor(() => expect(button).toBeDisabled())
    fireEvent.click(button)
    expect(writes).toHaveLength(1)
    await act(async () =>
      finish({ ...fixture, state: 'applied', log_state: 'delivered' })
    )
    await screen.findByRole('status')
  })
})
