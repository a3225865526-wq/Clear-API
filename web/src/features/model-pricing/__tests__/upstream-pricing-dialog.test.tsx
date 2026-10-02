import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
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
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'

import { SettingsPageProvider } from '@/features/system-settings/components/settings-page-context'
import { RatioSettingsCard } from '@/features/system-settings/models/ratio-settings-card'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { pricingOptions } from '../pricing'
import { UpstreamPricingDialog } from '../upstream-pricing-dialog'

it('generates A pricing, invalidates the preview on multiplier changes, and applies only after review', async () => {
  const apply = vi.fn().mockReturnValue(true)
  const close = vi.fn()
  render(<UpstreamPricingDialog open onOpenChange={close} onApply={apply} />)
  const user = userEvent.setup()
  const applyButton = screen.getByRole('button', {
    name: 'Apply generated prices to draft',
  })
  expect(applyButton).toBeDisabled()
  await user.type(
    screen.getByRole('textbox', { name: 'Upstream account multiplier' }),
    '0.5'
  )
  await user.type(
    screen.getByRole('textbox', { name: 'Sales multiplier' }),
    '0.8'
  )
  fireEvent.change(
    screen.getByRole('textbox', { name: 'Upstream price table' }),
    {
      target: {
        value: JSON.stringify({
          currency: 'USD',
          token_unit: '1M',
          models: [{ model: 'example', input: 10, output: 30 }],
        }),
      },
    }
  )
  await user.click(
    screen.getByRole('button', { name: 'Generate price preview' })
  )
  const table = await screen.findByRole('table', {
    name: 'Generated price preview',
  })
  expect(within(table).getByText('$5')).toBeVisible()
  expect(within(table).getByText('$8')).toBeVisible()
  expect(apply).not.toHaveBeenCalled()
  expect(applyButton).toBeEnabled()
  await user.clear(screen.getByRole('textbox', { name: 'Sales multiplier' }))
  await user.type(
    screen.getByRole('textbox', { name: 'Sales multiplier' }),
    '1.2'
  )
  expect(applyButton).toBeDisabled()
  expect(screen.queryByRole('table')).not.toBeInTheDocument()
  await user.click(
    screen.getByRole('button', { name: 'Generate price preview' })
  )
  expect(await screen.findByRole('table')).toHaveTextContent('$12')
  await user.click(applyButton)
  expect(apply.mock.calls[0][0].models[0]).toEqual({
    model: 'example',
    expression: 'tier("upstream_1", p * 12 + c * 36)',
  })
  expect(close).toHaveBeenCalledWith(false)
})

it('blocks malformed tables and reports below-cost sales without silently changing the chosen multiplier', async () => {
  const apply = vi.fn()
  render(<UpstreamPricingDialog open onOpenChange={vi.fn()} onApply={apply} />)
  const user = userEvent.setup()
  fireEvent.change(
    screen.getByRole('textbox', { name: 'Upstream account multiplier' }),
    { target: { value: '0.8' } }
  )
  fireEvent.change(screen.getByRole('textbox', { name: 'Sales multiplier' }), {
    target: { value: '0.5' },
  })
  fireEvent.change(
    screen.getByRole('textbox', { name: 'Upstream price table' }),
    { target: { value: 'broken' } }
  )
  await user.click(
    screen.getByRole('button', { name: 'Generate price preview' })
  )
  expect(await screen.findByRole('alert')).toHaveTextContent('not valid JSON')
  expect(
    screen.getByRole('button', { name: 'Apply generated prices to draft' })
  ).toBeDisabled()
  fireEvent.change(
    screen.getByRole('textbox', { name: 'Upstream price table' }),
    {
      target: {
        value: JSON.stringify({
          currency: 'USD',
          token_unit: '1M',
          models: [{ model: 'example', input: 10, output: 30 }],
        }),
      },
    }
  )
  await user.click(
    screen.getByRole('button', { name: 'Generate price preview' })
  )
  expect(
    await screen.findByText(
      'Some sales prices are below upstream cost. Review the price difference before applying.'
    )
  ).toBeVisible()
  expect(screen.getByRole('textbox', { name: 'Sales multiplier' })).toHaveValue(
    '0.5'
  )
  await user.click(screen.getByRole('button', { name: 'Cancel' }))
  expect(apply).not.toHaveBeenCalled()
})

it('validates required multipliers and preserves the preview when the draft rejects application', async () => {
  const close = vi.fn()
  render(
    <UpstreamPricingDialog open onOpenChange={close} onApply={() => false} />
  )
  const user = userEvent.setup()
  await user.click(
    screen.getByRole('button', { name: 'Generate price preview' })
  )
  await waitFor(() =>
    expect(
      screen.getByRole('textbox', { name: 'Sales multiplier' })
    ).toHaveAttribute('aria-invalid', 'true')
  )
  fireEvent.change(
    screen.getByRole('textbox', { name: 'Upstream account multiplier' }),
    { target: { value: '0.5' } }
  )
  fireEvent.change(screen.getByRole('textbox', { name: 'Sales multiplier' }), {
    target: { value: '0.8' },
  })
  fireEvent.change(
    screen.getByRole('textbox', { name: 'Upstream price table' }),
    {
      target: {
        value: JSON.stringify({
          currency: 'USD',
          token_unit: '1M',
          models: [{ model: 'example', input: 10, output: 30 }],
        }),
      },
    }
  )
  await user.click(
    screen.getByRole('button', { name: 'Generate price preview' })
  )
  await user.click(
    await screen.findByRole('button', {
      name: 'Apply generated prices to draft',
    })
  )
  expect(close).not.toHaveBeenCalled()
  expect(screen.getByRole('table')).toBeVisible()
})

it.each([true, false])(
  'applies generated prices to the real settings draft and submits only on Save (server success: %s)',
  async (success) => {
    const client = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    const actions = document.createElement('div')
    document.body.append(actions)
    useAuthStore
      .getState()
      .auth.setUser({ id: 1, username: 'test-administrator', role: 100 })
    const options = pricingOptions({ ModelRatio: '{"example":1}' })
    vi.spyOn(api, 'get').mockImplementation(async (url) => {
      if (url === '/api/option/model_pricing') {
        return {
          data: {
            success: true,
            data: {
              entries: [
                {
                  model_name: 'example',
                  version: 'existing-version',
                  configured: { ModelRatio: 1 },
                  effective: { ModelRatio: 1 },
                },
              ],
              options,
              empty_version: 'empty',
            },
          },
        }
      }
      if (url === '/api/pricing') {
        return { data: { success: true, data: [], vendors: [] } }
      }
      return { data: { success: true, data: {} } }
    })
    const patch = vi
      .spyOn(api, 'patch')
      .mockResolvedValue({
        data: { success, message: 'Pricing version changed' },
      })
    try {
      render(
        <QueryClientProvider client={client}>
          <SettingsPageProvider actionsContainer={actions}>
            <RatioSettingsCard
              visibleTabs={['models']}
              modelDefaults={{
                ...options,
                BillingMode: '{}',
                BillingExpr: '{}',
                PluginBillingExpr: '{}',
                ExposeRatioEnabled: false,
              }}
              groupDefaults={{
                GroupRatio: '{}',
                TopupGroupRatio: '{}',
                UserUsableGroups: '{}',
                GroupGroupRatio: '{}',
                AutoGroups: '[]',
                MaxTokenAutoGroups: 5,
                DefaultUseAutoGroup: false,
                GroupSpecialUsableGroup: '{}',
              }}
              toolPricesDefault='{}'
            />
          </SettingsPageProvider>
        </QueryClientProvider>
      )
      const user = userEvent.setup()
      await user.click(
        await screen.findByRole('button', {
          name: 'Generate prices from upstream',
        })
      )
      fireEvent.change(
        screen.getByRole('textbox', { name: 'Upstream account multiplier' }),
        { target: { value: '0.5' } }
      )
      fireEvent.change(
        screen.getByRole('textbox', { name: 'Sales multiplier' }),
        { target: { value: '0.8' } }
      )
      fireEvent.change(
        screen.getByRole('textbox', { name: 'Upstream price table' }),
        {
          target: {
            value: JSON.stringify({
              currency: 'USD',
              token_unit: '1M',
              models: [{ model: 'example', input: 10, output: 30 }],
            }),
          },
        }
      )
      await user.click(
        screen.getByRole('button', { name: 'Generate price preview' })
      )
      await user.click(
        screen.getByRole('button', { name: 'Apply generated prices to draft' })
      )
      expect(patch).not.toHaveBeenCalled()
      await user.click(
        screen.getByRole('button', { name: 'Save generated prices' })
      )
      await waitFor(() => expect(patch).toHaveBeenCalledOnce())
      expect(patch.mock.calls[0][1]).toEqual({
        changes: [
          {
            model_name: 'example',
            expected_version: 'existing-version',
            pricing: {
              'billing_setting.billing_mode': 'tiered_expr',
              'billing_setting.billing_expr':
                'tier("upstream_1", p * 8 + c * 24)',
            },
          },
        ],
      })
      if (!success) {
        await waitFor(() =>
          expect(
            screen.getByRole('button', { name: 'Save generated prices' })
          ).toBeEnabled()
        )
        expect(screen.getByText('example')).toBeVisible()
      }
    } finally {
      client.clear()
      actions.remove()
      useAuthStore.getState().auth.reset()
      vi.restoreAllMocks()
    }
  }
)
