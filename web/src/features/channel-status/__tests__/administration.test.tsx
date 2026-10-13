import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import en from '@/i18n/locales/en.json'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { MonitorAdmin } from '../monitor-admin'
import { MonitorAdminForm } from '../monitor-admin-form'
import { MonitorTargetsPage } from '../monitor-targets-page'

let client: QueryClient
beforeEach(() => {
  i18next.addResourceBundle('en', 'translation', en.translation, true, true)
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: {
      success: true,
      data: url.endsWith('/targets')
        ? {
            channels: [
              { id: 1, name: 'Allowed', status: 1, models: 'model-a,model-b' },
              { id: 2, name: 'Disabled channel', status: 2, models: 'model-a' },
            ],
            groups: [],
          }
        : {
            policy: { max_concurrent: 2, daily_cost_micros: 5000000 },
            monitors: [
              {
                id: 1,
                display_name: 'Configured target',
                model: 'model-a',
                target_kind: 'channel',
                target_ref: '1',
                channel_id: 1,
                protocol: 'openai_chat',
                platform: '',
                interval_seconds: 600,
                timeout_seconds: 30,
                stale_seconds: 900,
                daily_requests: 0,
                request_cost_micros: 0,
                published: false,
                enabled: false,
                archived: false,
              },
            ],
          },
    },
  }))
})
afterEach(() => {
  client.clear()
  useAuthStore.getState().auth.setUser(null)
  vi.restoreAllMocks()
})
function renderAdmin() {
  return render(
    <QueryClientProvider client={client}>
      <MonitorAdmin />
    </QueryClientProvider>
  )
}
function renderEditForm(allowlist = false) {
  return render(
    <QueryClientProvider client={client}>
      <MonitorAdminForm
        allowlist={allowlist}
        targets={{
          channels: [{ id: 1, name: 'Allowed', status: 1, models: 'model-a' }],
          groups: [],
        }}
        monitor={{
          id: 1,
          display_name: 'Configured target',
          channel_id: 1,
          target_kind: 'channel',
          target_ref: '1',
          model: 'model-a',
          protocol: 'openai_chat',
          platform: '',
          interval_seconds: 600,
          timeout_seconds: 30,
          stale_seconds: 900,
          daily_requests: 0,
          request_cost_micros: 0,
          published: false,
          enabled: false,
          archived: false,
        }}
      />
    </QueryClientProvider>
  )
}

describe('monitor administration', () => {
  it('saves target timing without sending per-target budget overrides', async () => {
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    renderEditForm(true)
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(put).toHaveBeenCalled())
    const [url, input] = put.mock.calls[0]
    expect(url).toBe('/api/console/monitor-targets')
    expect(input).toMatchObject({
      target_type: 'channel',
      target_ref: '1',
      model: 'model-a',
      enabled: false,
      published: false,
    })
    expect(input).not.toHaveProperty('daily_requests')
    expect(input).not.toHaveProperty('request_cost_micros')
    expect(input).not.toHaveProperty('max_concurrency')
    expect(input).not.toHaveProperty('daily_budget_limit')
  })
  it('hides target management from ordinary administrators without fetching data', () => {
    useAuthStore.getState().auth.setUser({ id: 2, username: 'admin', role: 50 })
    render(
      <QueryClientProvider client={client}>
        <MonitorTargetsPage />
      </QueryClientProvider>
    )
    expect(
      screen.queryByRole('button', { name: 'Add' })
    ).not.toBeInTheDocument()
    expect(api.get).not.toHaveBeenCalled()
  })
  it('shows global policy read only and enables only allowlist membership after confirmation', async () => {
    useAuthStore.getState().auth.setUser({ id: 1, username: 'root', role: 100 })
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    const post = vi.spyOn(api, 'post')
    render(
      <QueryClientProvider client={client}>
        <MonitorTargetsPage />
      </QueryClientProvider>
    )
    await screen.findByText('Configured target · model-a')
    expect(
      screen.getByText('Global monitoring policy (read only)')
    ).toBeVisible()
    expect(
      screen.queryByRole('spinbutton', { name: 'Daily probe request limit' })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('spinbutton', {
        name: 'Reserved cost per probe (micro USD)',
      })
    ).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Enable' }))
    expect(put).not.toHaveBeenCalled()
    await userEvent.click(screen.getByRole('button', { name: 'Continue' }))
    await waitFor(() =>
      expect(put).toHaveBeenCalledWith(
        '/api/console/monitor-targets/1/switch/enabled',
        { value: true, confirm: true },
        { headers: { 'X-ClearAPI-Monitor': '1' } }
      )
    )
    expect(post).not.toHaveBeenCalled()
  })
  it('requires explicit confirmation before publishing and never requests a probe', async () => {
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    const post = vi.spyOn(api, 'post')
    renderAdmin()
    await screen.findByText('Configured target · model-a')
    expect(screen.getByRole('status')).toHaveTextContent(
      'do not start detection'
    )
    await userEvent.click(screen.getByRole('button', { name: 'Publish' }))
    expect(put).not.toHaveBeenCalled()
    await userEvent.click(screen.getByRole('button', { name: 'Continue' }))
    await waitFor(() =>
      expect(put).toHaveBeenCalledWith(
        '/api/console/monitor-admin/1/switch/published',
        { value: true, confirm: true },
        { headers: { 'X-ClearAPI-Monitor': '1' } }
      )
    )
    expect(post).not.toHaveBeenCalled()
  })
  it('shows load failure without presenting an editable empty catalog', async () => {
    vi.mocked(api.get).mockRejectedValue(new Error('fixture offline'))
    renderAdmin()
    expect(await screen.findByText('Unable to load data')).toBeVisible()
    expect(
      screen.queryByRole('button', { name: 'Add' })
    ).not.toBeInTheDocument()
  })
  it('validates an edited monitor before sending it', async () => {
    const put = vi.spyOn(api, 'put')
    renderEditForm()
    const name = screen.getByRole('textbox', { name: 'Display Name' })
    await userEvent.clear(name)
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('Invalid value')).toBeVisible()
    expect(put).not.toHaveBeenCalled()
  })
  it('keeps the edit form open when saving fails', async () => {
    const put = vi
      .spyOn(api, 'put')
      .mockRejectedValue(new Error('fixture offline'))
    renderEditForm()
    const name = screen.getByRole('textbox', { name: 'Display Name' })
    await userEvent.clear(name)
    await userEvent.type(name, 'Updated monitor')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(put).toHaveBeenCalled())
    expect(await screen.findByRole('alert')).toHaveTextContent('Error')
    expect(screen.getByRole('textbox', { name: 'Display Name' })).toHaveValue(
      'Updated monitor'
    )
  })
})
