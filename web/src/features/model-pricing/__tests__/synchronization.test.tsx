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
import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { RatioSettingsCard } from '@/features/system-settings/models/ratio-settings-card'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'
import { usePricingPreferencesStore } from '@/stores/pricing-preferences-store'

import {
  invalidateModelPricing,
  type ModelPricingConfig,
  type ModelPricingChange,
} from '../api'
import { ModelPricingPanel } from '../model-pricing-panel'
import { pricingOptions, type PricingValues } from '../pricing'

let client: QueryClient

afterEach(() => {
  client?.clear()
  useAuthStore.getState().auth.setUser(null)
  localStorage.clear()
  vi.restoreAllMocks()
})

function snapshot(values: PricingValues, version: string): ModelPricingConfig {
  return {
    entries: [
      { model_name: 'example', version, configured: values, effective: values },
    ],
    options: pricingOptions(
      Object.fromEntries(
        Object.entries(values).map(([key, value]) => [
          key,
          JSON.stringify({ example: value }),
        ])
      )
    ),
    empty_version: 'empty',
  }
}

async function setup() {
  useAuthStore
    .getState()
    .auth.setUser({ id: 1, username: 'administrator', role: 100 })
  usePricingPreferencesStore.setState({ currency: 'USD' })
  let stored = snapshot(
    {
      'billing_setting.billing_mode': 'tiered_expr',
      'billing_setting.billing_expr':
        'tier("base", p * 2 + c * 8 + cr * 0 + cc * 2.5)',
    },
    'v1'
  )
  vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: {
      success: true,
      data: url === '/api/option/model_pricing' ? stored : [],
      vendors: [],
    },
  }))
  const submitted: ModelPricingChange[][] = []
  const patch = vi
    .spyOn(api, 'patch')
    .mockImplementation(async (_url, request) => {
      const { changes } = request as { changes: ModelPricingChange[] }
      submitted.push(changes)
      if (changes[0].expected_version !== stored.entries[0].version) {
        return {
          data: {
            success: false,
            message: 'Model pricing changed; reload before saving',
          },
        }
      }
      stored = snapshot(changes[0].pricing, 'v2')
      return { data: { success: true } }
    })
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <section aria-label='Models'>
        <ModelPricingPanel modelName='example' />
      </section>
      <section aria-label='Billing'>
        <RatioSettingsCard
          titleKey='Model Pricing'
          visibleTabs={['models']}
          modelDefaults={{
            ...pricingOptions({}),
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
      </section>
    </QueryClientProvider>
  )
  const models = within(screen.getByRole('region', { name: 'Models' }))
  const billing = within(screen.getByRole('region', { name: 'Billing' }))
  await models.findByRole('textbox', { name: 'Input price' })
  const user = userEvent.setup()
  await waitFor(() => expect(client.isFetching()).toBe(0))
  await user.click(await billing.findByRole('button', { name: 'Edit' }))
  await billing.findByRole('textbox', { name: 'Input price' })
  return {
    models,
    billing,
    user,
    patch,
    submitted,
    refresh: async () => {
      stored = snapshot(
        {
          'billing_setting.billing_mode': 'tiered_expr',
          'billing_setting.billing_expr':
            'tier("base", p * 3 + c * 12 + cr * 0.3 + cc * 4)',
        },
        'remote-v2'
      )
      await act(() => invalidateModelPricing(client))
    },
  }
}

it.each(['Models', 'Billing'] as const)(
  'updates both open pricing editors after saving from %s',
  async (source) => {
    const { models, billing, user, patch } = await setup()
    const editor = source === 'Models' ? models : billing
    for (const [name, value] of [
      ['Input price', '3'],
      ['Output price', '12'],
      ['Cache read price', '0.3'],
      ['Cache create price', '4'],
    ]) {
      const input = editor.getByRole('textbox', { name })
      await user.clear(input)
      await user.type(input, value)
    }
    await user.click(editor.getByRole('button', { name: 'Save model prices' }))
    await waitFor(() => expect(patch).toHaveBeenCalledTimes(1))
    for (const target of [models, billing]) {
      await waitFor(() =>
        expect(
          target.getByRole('textbox', { name: 'Input price' })
        ).toHaveValue('3')
      )
      expect(target.getByRole('textbox', { name: 'Output price' })).toHaveValue(
        '12'
      )
      expect(
        target.getByRole('textbox', { name: 'Cache read price' })
      ).toHaveValue('0.3')
      expect(
        target.getByRole('textbox', { name: 'Cache create price' })
      ).toHaveValue('4')
    }
  }
)

it.each(['Models', 'Billing'] as const)(
  'preserves the %s draft and its conflict version during a pricing refresh',
  async (source) => {
    const { models, billing, user, patch, submitted, refresh } = await setup()
    const editor = source === 'Models' ? models : billing
    const price = editor.getByRole('textbox', { name: 'Input price' })
    await user.clear(price)
    await user.type(price, '5')
    await refresh()
    expect(price).toHaveValue('5')
    await user.click(editor.getByRole('button', { name: 'Save model prices' }))
    await waitFor(() => expect(patch).toHaveBeenCalled())
    expect(submitted[0][0].expected_version).toBe('v1')
    expect(price).toHaveValue('5')
    await user.click(
      await editor.findByRole('button', { name: 'Reload pricing' })
    )
    await waitFor(() =>
      expect(editor.getByRole('textbox', { name: 'Input price' })).toHaveValue(
        '3'
      )
    )
  }
)
