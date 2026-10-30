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
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { getUserModels } from '@/features/playground/api'
import { getPricing } from '@/features/pricing/api'
import type { PricingData } from '@/features/pricing/types'
import { useAuthStore } from '@/stores/auth-store'

import { apiRoot, buildClientConfig } from '../client-config'
import { ClientSetup } from '../client-setup'

vi.mock('@/features/playground/api', () => ({ getUserModels: vi.fn() }))
vi.mock('@/features/pricing/api', () => ({ getPricing: vi.fn() }))
vi.mock('@/hooks/use-status', () => ({
  useStatus: () => ({
    status: { server_address: 'https://gateway.test/proxy/v1/' },
  }),
}))
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

beforeEach(() => {
  useAuthStore.getState().auth.setUser({ id: 5, username: 'QA', role: 1 })
  vi.mocked(getUserModels).mockResolvedValue([
    { label: 'responses-model', value: 'responses-model' },
    { label: 'chat-model', value: 'chat-model' },
  ])
  vi.mocked(getPricing).mockResolvedValue({
    success: true,
    data: [
      {
        model_name: 'responses-model',
        supported_endpoint_types: ['openai-response'],
      },
      { model_name: 'chat-model', supported_endpoint_types: ['openai'] },
      {
        model_name: 'unavailable-model',
        supported_endpoint_types: ['anthropic'],
      },
    ],
  } as PricingData)
})
afterEach(() => {
  cleanup()
  useAuthStore.setState(useAuthStore.getInitialState())
})

function setup() {
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <ClientSetup />
    </QueryClientProvider>
  )
}

describe('client configuration', () => {
  it('uses the API address, filters models by protocol, and copies the generated configuration', async () => {
    const user = userEvent.setup()
    setup()
    await screen.findByText(/model = "responses-model"/)
    expect(
      screen.getByText(/base_url = "https:\/\/gateway.test\/proxy\/v1"/)
    ).toBeVisible()
    await user.selectOptions(screen.getByLabelText('Client'), 'opencode')
    expect(screen.getByText(/"model": "clearapi\/chat-model"/)).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Copy to clipboard' }))
    expect(await navigator.clipboard.readText()).toContain(
      '"apiKey": "{env:CLEARAPI_API_KEY}"'
    )
    await user.selectOptions(screen.getByLabelText('Client'), 'claude')
    expect(
      screen.getByText('No models available for this protocol')
    ).toBeVisible()
    expect(
      screen.queryByRole('button', { name: 'Copy to clipboard' })
    ).not.toBeInTheDocument()
    expect(screen.queryByText(/ANTHROPIC_MODEL/)).not.toBeInTheDocument()
  })

  it('shows a recoverable error when model loading fails', async () => {
    vi.mocked(getUserModels).mockRejectedValue(new Error('offline'))
    setup()
    await screen.findByRole('button', { name: /retry|try again/i })
    expect(
      screen.queryByRole('button', { name: 'Copy to clipboard' })
    ).not.toBeInTheDocument()
  })

  it('keeps credentials out of generated configs and escapes model names in JSON and TOML', () => {
    const model = 'model"\\name'
    const root = apiRoot('https://gateway.test/proxy/v1/')
    expect(root).toBe('https://gateway.test/proxy')
    expect(buildClientConfig('codex', root, model)).toContain(
      `model = ${JSON.stringify(model)}`
    )
    const claude = JSON.parse(buildClientConfig('claude', root, model))
    expect(claude.env).toEqual({
      ANTHROPIC_BASE_URL: root,
      ANTHROPIC_AUTH_TOKEN: '<YOUR_API_KEY>',
      ANTHROPIC_MODEL: model,
    })
    const opencode = JSON.parse(buildClientConfig('opencode', root, model))
    expect(opencode.provider.clearapi.models[model].name).toBe(model)
    expect(buildClientConfig('cherry', root, 'chat-model')).toContain(
      'Base URL: https://gateway.test/proxy/v1'
    )
  })

  it.each([
    'javascript:alert(1)',
    'https://secret@gateway.test',
    'https://gateway.test?key=secret',
  ])('rejects an unsafe API address (%s)', (address) => {
    expect(() => apiRoot(address)).toThrow('Invalid API address')
  })
})
