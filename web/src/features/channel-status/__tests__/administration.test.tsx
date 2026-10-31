import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
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

function renderArchivedAdmin() {
  client.setQueryData(['monitor-admin-catalog'], {
    monitors: [
      {
        id: 1,
        display_name: 'Archived target',
        model: 'model-a',
        target_kind: 'channel',
        published: false,
        enabled: false,
        archived: true,
      },
      {
        id: 2,
        display_name: 'Active target',
        model: 'model-a',
        target_kind: 'channel',
        published: true,
        enabled: true,
        archived: false,
      },
    ],
  })
  client.setQueryDefaults(['monitor-admin-catalog'], { staleTime: Infinity })
  return renderAdmin()
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
  it('accepts model and protocol names outside the previous choices', async () => {
    renderAdmin()
    await screen.findByText('Configured target · model-a')
    await userEvent.type(
      screen.getByRole('textbox', { name: 'Model' }),
      'unlisted-provider-model'
    )
    const protocol = screen.getByRole('textbox', {
      name: 'Monitoring protocol',
    })
    await userEvent.clear(protocol)
    await userEvent.type(protocol, 'video-generation')
    expect(screen.getByRole('textbox', { name: 'Model' })).toHaveValue(
      'unlisted-provider-model'
    )
    expect(protocol).toHaveValue('video-generation')
  })

  it('deletes only an archived directory after showing its name and ID', async () => {
    const remove = vi.spyOn(api, 'delete').mockResolvedValue({
      data: { success: true },
    })
    renderArchivedAdmin()
    await screen.findByText('Archived target · model-a')
    expect(screen.getAllByRole('button', { name: 'Delete' })).toHaveLength(1)
    await userEvent.click(screen.getByRole('button', { name: 'Delete' }))
    expect(remove).not.toHaveBeenCalled()
    expect(screen.getByRole('alertdialog')).toHaveTextContent(
      'Delete archived monitoring entry "Archived target" (ID 1)?'
    )
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(remove).not.toHaveBeenCalled()
    await userEvent.click(screen.getByRole('button', { name: 'Delete' }))
    const dialog = screen.getByRole('alertdialog')
    await userEvent.click(
      within(dialog).getByRole('button', { name: 'Delete' })
    )
    await waitFor(() =>
      expect(remove).toHaveBeenCalledWith('/api/console/monitor-admin/1', {
        data: { confirm: true },
        headers: { 'X-ClearAPI-Monitor': '1' },
      })
    )
    await waitFor(() =>
      expect(
        screen.queryByText('Archived target · model-a')
      ).not.toBeInTheDocument()
    )
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  })

  it('keeps the directory and confirmation visible after a failed delete', async () => {
    vi.spyOn(api, 'delete').mockResolvedValue({
      data: {
        success: false,
        message:
          'An unfinished monitoring attempt must be resolved before this operation.',
      },
    })
    renderArchivedAdmin()
    await screen.findByText('Archived target · model-a')
    await userEvent.click(screen.getByRole('button', { name: 'Delete' }))
    await userEvent.click(
      within(screen.getByRole('alertdialog')).getByRole('button', {
        name: 'Delete',
      })
    )
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'An unfinished monitoring attempt must be resolved before this operation.'
    )
    expect(screen.getByText('Archived target · model-a')).toBeInTheDocument()
    expect(screen.getByRole('alertdialog')).toBeVisible()
  })

  it('keeps the enable confirmation open and explains a missing cost contract', async () => {
    client.setQueryData(['monitor-admin-catalog'], {
      monitors: [
        {
          id: 1,
          display_name: 'Configured target',
          model: 'model-a',
          target_kind: 'channel',
          published: true,
          enabled: false,
          archived: false,
        },
      ],
    })
    client.setQueryDefaults(['monitor-admin-catalog'], { staleTime: Infinity })
    const message =
      'Configure a valid upstream cost contract in this monitor’s execution settings.'
    vi.spyOn(api, 'put').mockRejectedValue({
      response: {
        status: 409,
        data: { success: false, code: 'MONITOR_COST_UNVERIFIED', message },
      },
    })
    renderAdmin()
    await screen.findByText('Configured target · model-a')
    await userEvent.click(screen.getByRole('button', { name: 'Enable' }))
    await userEvent.click(screen.getByRole('button', { name: 'Continue' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(message)
    expect(screen.getByRole('alertdialog')).toBeVisible()
  })

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
      'upstream cost contract'
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
  it('confirms a probe and displays a returned cost denial instead of claiming health', async () => {
    const original = vi.mocked(api.get).getMockImplementation()
    if (!original) throw new Error('missing API fixture')
    vi.mocked(api.get).mockImplementation(async (...args) => {
      const response = (await original(...args)) as {
        data: {
          data: {
            monitors?: Array<{ enabled: boolean; published: boolean }>
            readiness?: Record<
              number,
              { configuration_ready: boolean; blockers: string[] }
            >
          }
        }
      }
      const item = response.data.data.monitors?.[0]
      if (item) {
        item.enabled = true
        item.published = true
        response.data.data.readiness = {
          1: { configuration_ready: true, blockers: [] },
        }
      }
      return response
    })
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      data: {
        success: true,
        data: { status: 'error', error_code: 'MONITOR_COST_EXCEEDED' },
      },
    })
    renderAdmin()
    await screen.findByText('Configured target · model-a')
    await userEvent.click(screen.getByRole('button', { name: 'Test' }))
    expect(post).not.toHaveBeenCalled()
    await userEvent.click(screen.getByRole('button', { name: 'Continue' }))
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith(
        '/api/console/monitor-admin/1/probe',
        { confirm: true },
        { headers: { 'X-ClearAPI-Monitor': '1' } }
      )
    )
    expect(await screen.findByText(/MONITOR_COST_EXCEEDED/)).toHaveTextContent(
      'error'
    )
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

describe('monitor readiness diagnostics', () => {
  it('shows channel-specific blockers while an independently ready channel remains testable', async () => {
    client.setQueryData(['monitor-admin-catalog'], {
      monitors: [
        {
          id: 16,
          channel_id: 16,
          display_name: 'DeepSeek control',
          model: 'deepseek-chat',
          protocol: 'openai_chat',
          target_kind: 'channel',
          published: true,
          enabled: false,
          archived: false,
        },
        {
          id: 17,
          channel_id: 17,
          display_name: 'Other control',
          model: 'other-model',
          protocol: 'anthropic',
          target_kind: 'channel',
          published: true,
          enabled: true,
          archived: false,
        },
      ],
      readiness: {
        16: {
          configuration_ready: false,
          channel_id: 16,
          allowlisted: false,
          blockers: [
            'MONITOR_NOT_ALLOWLISTED',
            'MONITOR_COST_UNVERIFIED',
            'MONITOR_POLICY_DISABLED',
          ],
        },
        17: {
          configuration_ready: true,
          channel_id: 17,
          allowlisted: true,
          blockers: [],
        },
      },
      scheduler: { local_status: 'stale_on_this_node' },
    })
    client.setQueryDefaults(['monitor-admin-catalog'], { staleTime: Infinity })
    const post = vi.spyOn(api, 'post')
    renderAdmin()
    const channel = await screen.findByText('DeepSeek control · deepseek-chat')
    const blocked = channel.closest('article')
    if (!blocked) throw new Error('missing channel row')
    expect(within(blocked).getByRole('button', { name: 'Test' })).toBeDisabled()
    expect(
      within(blocked).getByText(
        'Add this target in Monitoring targets before enabling probes.'
      )
    ).toBeInTheDocument()
    expect(
      within(blocked).getByText(
        'Review and enable the global monitoring budget policy.'
      )
    ).toBeInTheDocument()
    const other = screen
      .getByText('Other control · other-model')
      .closest('article')
    if (!other) throw new Error('missing channel row')
    expect(within(other).getByRole('button', { name: 'Test' })).toBeEnabled()
    expect(
      screen.getByText(/No recent scheduler heartbeat on this node/)
    ).toBeInTheDocument()
    expect(post).not.toHaveBeenCalled()
  })
})


it('requires explicit policy confirmation and never dispatches a probe while saving budgets', async () => {
  const policy = { id: 1, enabled: true, max_concurrent: 1, daily_requests: 2, daily_cost_micros: 1000, retention_days: 90 }
  client.setQueryData(['monitor-admin-catalog'], { policy, monitors: [] })
  client.setQueryDefaults(['monitor-admin-catalog'], { staleTime: Infinity })
  const put = vi.spyOn(api, 'put').mockResolvedValue({ data: { success: true } })
  const post = vi.spyOn(api, 'post')
  renderAdmin()
  const summary = await screen.findByText('Global monitoring budget policy')
  await userEvent.click(summary)
  const details = summary.closest('details')
  if (!details) throw new Error('missing policy editor')
  await userEvent.click(within(details).getByRole('button', { name: 'Save' }))
  expect(put).not.toHaveBeenCalled()
  expect(screen.getByText('Enabling this policy permits scheduled probes for eligible monitors within the saved budgets.')).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Continue' }))
  await waitFor(() => expect(put).toHaveBeenCalledWith('/api/console/monitor-admin/policy', { confirm: true, policy }, { headers: { 'X-ClearAPI-Monitor': '1' } }))
  expect(post).not.toHaveBeenCalled()
})
