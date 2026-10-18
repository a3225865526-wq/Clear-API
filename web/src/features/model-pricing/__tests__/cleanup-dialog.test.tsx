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
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { ModelPricingCleanupDialog } from '../model-pricing-cleanup-dialog'

const cleanupReport = {
  snapshot: 'snapshot-1',
  records: [
    {
      model_name: 'gpt-4',
      billing_mode: 'ratio',
      version: 'v-ratio',
      configured: ['ModelRatio'],
      built_in: false,
      action: 'reset_to_defaults',
      unpriced_after_reset: true,
    },
    {
      model_name: 'clean-model',
      billing_mode: 'ratio',
      version: 'v-clean',
      configured: [],
      built_in: false,
      action: 'keep',
    },
  ],
  plans: [
    {
      model_name: 'gpt-4',
      billing_mode: 'ratio',
      version: 'v-ratio',
      configured: ['ModelRatio'],
      built_in: false,
      action: 'reset_to_defaults',
      unpriced_after_reset: true,
      pricing: { ModelRatio: 15 },
    },
    {
      model_name: 'clean-model',
      billing_mode: 'ratio',
      version: 'v-clean',
      configured: [],
      built_in: false,
      action: 'keep',
      pricing: {},
    },
  ],
}

function renderDialog() {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
  render(
    <QueryClientProvider client={client}>
      <ModelPricingCleanupDialog open onOpenChange={vi.fn()} />
    </QueryClientProvider>
  )
  return client
}

afterEach(() => {
  vi.restoreAllMocks()
})

it('lists where each site price record lives and applies only the reviewed selection', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: cleanupReport },
  })
  const post = vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: { ...cleanupReport, cleared: ['gpt-4'] } },
  })
  renderDialog()
  const user = userEvent.setup()

  const table = await screen.findByRole('table', {
    name: 'Model pricing cleanup',
  })
  expect(within(table).getByText('gpt-4')).toBeVisible()
  expect(
    within(table).getByText('Site pricing: ModelRatio')
  ).toBeVisible()

  const submit = screen.getByRole('button', { name: 'Clean up 0 models' })
  expect(submit).toBeDisabled()

  await user.click(
    screen.getByRole('checkbox', { name: 'Select gpt-4' })
  )
  await user.click(
    screen.getByRole('button', { name: 'Clean up 1 models' })
  )

  await waitFor(() => expect(post).toHaveBeenCalledOnce())
  expect(post.mock.calls[0]).toEqual([
    '/api/option/model_pricing/cleanup',
    {
      snapshot: 'snapshot-1',
      changes: [
        { model_name: 'gpt-4', expected_version: 'v-ratio', pricing: { ModelRatio: 15 } },
      ],
    },
  ])
})

it('keeps models without site pricing unselectable and warns about an unpriced reset', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: cleanupReport },
  })
  renderDialog()
  await screen.findByRole('table', { name: 'Model pricing cleanup' })

  expect(
    screen.getByRole('checkbox', { name: 'Select clean-model' })
  ).toHaveAttribute('aria-disabled', 'true')
  expect(
    screen.getByText(
      'No built-in price. This model becomes unpriced after reset.'
    )
  ).toBeVisible()
})
