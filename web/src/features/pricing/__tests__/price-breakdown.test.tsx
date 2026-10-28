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
import { act, render, screen, within } from '@testing-library/react'
import i18next from 'i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import enLocale from '@/i18n/locales/en.json'
import frLocale from '@/i18n/locales/fr.json'
import jaLocale from '@/i18n/locales/ja.json'
import ruLocale from '@/i18n/locales/ru.json'
import viLocale from '@/i18n/locales/vi.json'
import zhTwLocale from '@/i18n/locales/zh-TW.json'
import zhLocale from '@/i18n/locales/zh.json'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { ModelCard } from '../components/model-card'
import { ModelDetailsContent } from '../components/model-details'
import { ModelPriceBreakdown } from '../components/model-price-breakdown'
import { PricingTable } from '../components/pricing-table'
import type { PricingModel } from '../types'

vi.mock('@visactor/react-vchart', () => ({ VChart: () => null }))

const model: PricingModel = {
  id: 1,
  model_name: 'custom-official-base',
  quota_type: 0,
  model_ratio: 5,
  completion_ratio: 3,
  enable_groups: ['plus', 'pro'],
  group_ratio: { plus: 0.08, pro: 0.16 },
}

beforeEach(() => {
  useSystemConfigStore.getState().setConfig({
    currency: { ...DEFAULT_CURRENCY_CONFIG, quotaDisplayType: 'USD' },
  })
})

afterEach(() => {
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
})

describe('model price breakdown', () => {
  it('shows a holiday price after applying the group multiplier and no guessed price when the calendar is missing', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-10-01T10:00:00+08:00'))
    const holidayModel = {
      ...model,
      billing_mode: 'tiered_expr',
      billing_expr:
        'cn_holiday("Asia/Shanghai") == 0 ? tier("peak", p * 0.3 + c * 1.2) : tier("off_peak", p * 0.15 + c * 0.6)',
    }
    const view = render(
      <ModelCard model={holidayModel} selectedGroup='plus' onClick={vi.fn()} />
    )
    try {
      const final = screen.getByRole('region', { name: 'Final price' })
      expect(final).toHaveTextContent('$0.012')
      expect(final).toHaveTextContent('$0.048')
      expect(screen.getByText('Current period price')).toBeVisible()
      act(() => {
        vi.setSystemTime(new Date('2027-01-01T10:00:00+08:00'))
        vi.advanceTimersByTime(60000)
      })
      expect(final).toHaveTextContent(
        'Holiday calendar unavailable; update it before using this price.'
      )
      expect(within(final).queryByText(/\$[\d.]+/)).not.toBeInTheDocument()
    } finally {
      view.unmount()
      vi.useRealTimers()
    }
  })

  it('keeps configured base prices and updates final prices and the effective group together', () => {
    const { rerender } = render(
      <ModelCard model={model} onClick={vi.fn()} selectedGroup='plus' />
    )
    const base = screen.getByRole('region', { name: 'Base Price' })
    expect(within(base).getByText('10')).toBeVisible()
    expect(within(base).getByText('30')).toBeVisible()
    expect(screen.getByText('Group: plus · multiplier: 0.08×')).toBeVisible()
    expect(
      screen.getByRole('region', { name: 'Final price' })
    ).toHaveTextContent('$0.8')
    expect(
      screen.getByRole('region', { name: 'Final price' })
    ).toHaveTextContent('$2.4')

    rerender(<ModelCard model={model} onClick={vi.fn()} selectedGroup='pro' />)
    expect(base).toHaveTextContent('10')
    expect(screen.getByText('Group: pro · multiplier: 0.16×')).toBeVisible()
    expect(
      screen.getByRole('region', { name: 'Final price' })
    ).toHaveTextContent('$1.6')
    expect(
      screen.getByRole('region', { name: 'Final price' })
    ).toHaveTextContent('$4.8')
  })

  it('identifies the group behind the lowest price and preserves a free group', () => {
    const free = { ...model, group_ratio: { plus: 0.08, pro: 0 } }
    render(<ModelCard model={free} onClick={vi.fn()} />)
    expect(screen.getByText('Group: pro · multiplier: 0×')).toBeVisible()
    expect(
      screen.getByText('Lowest price across available groups')
    ).toBeVisible()
    expect(
      within(screen.getByRole('region', { name: 'Final price' })).getAllByText(
        '$0'
      )
    ).toHaveLength(2)
  })

  it('labels recharge conversion separately and leaves the base prices unchanged', () => {
    render(
      <ModelCard
        model={model}
        onClick={vi.fn()}
        selectedGroup='plus'
        showRechargePrice
        priceRate={3}
        usdExchangeRate={6}
      />
    )
    expect(
      screen.getByRole('region', { name: 'Base Price' })
    ).toHaveTextContent('10')
    const converted = screen.getByRole('region', {
      name: 'Recharge equivalent price',
    })
    expect(converted).toHaveTextContent('$0.4')
    expect(converted).toHaveTextContent('$1.2')
    expect(
      screen.getByText(
        'Base price × effective group multiplier × recharge conversion'
      )
    ).toBeVisible()
  })

  it('shows the same base and final prices in the list view', () => {
    render(
      <PricingTable
        models={[model]}
        onModelClick={vi.fn()}
        selectedGroup='pro'
      />
    )
    expect(screen.getByText('Group: pro · multiplier: 0.16×')).toBeVisible()
    expect(
      screen.getByRole('region', { name: 'Base Price' })
    ).toHaveTextContent('10')
    const final = screen.getByRole('region', { name: 'Final price' })
    expect(final).toHaveTextContent('1.6')
    expect(final).toHaveTextContent('4.8')
  })

  it.each([
    { name: 'per-request', quota_type: 1, model_price: 10 },
    {
      name: 'tiered expression',
      billing_mode: 'tiered_expr',
      billing_expr:
        'len <= 1000 ? tier("short", p * 10 + c * 30) : tier("long", p * 20 + c * 60)',
    },
  ])(
    'applies group pricing to $name without changing the configured base',
    (overrides) => {
      render(
        <ModelPriceBreakdown
          model={{ ...model, ...overrides }}
          options={{ selectedGroup: 'plus', tokenUnit: 'M' }}
        />
      )
      expect(
        screen.getByRole('region', { name: 'Base Price' })
      ).toHaveTextContent('10')
      expect(
        screen.getByRole('region', { name: 'Final price' })
      ).toHaveTextContent('0.8')
      expect(screen.getByText('Group: plus · multiplier: 0.08×')).toBeVisible()
    }
  )

  it('uses the viewer’s effective groups when the model supports all groups', () => {
    render(
      <ModelPriceBreakdown
        model={{ ...model, enable_groups: ['all'] }}
        options={{ selectedGroup: 'pro' }}
      />
    )
    expect(screen.getByText('Group: pro · multiplier: 0.16×')).toBeVisible()
    expect(
      screen.getByRole('region', { name: 'Final price' })
    ).toHaveTextContent('1.6')
  })

  it.each([
    ['en', enLocale],
    ['zhCN', zhLocale],
    ['zhTW', zhTwLocale],
    ['fr', frLocale],
    ['ja', jaLocale],
    ['ru', ruLocale],
    ['vi', viLocale],
    ['invalid_locale', enLocale],
  ] as const)(
    'updates translated labels and preserves precise multipliers after switching to %s',
    async (language, locale) => {
      render(
        <ModelPriceBreakdown
          model={{ ...model, group_ratio: { pro: 0.0001234 } }}
          options={{ selectedGroup: 'pro' }}
        />
      )
      try {
        i18next.addResourceBundle(
          language,
          'translation',
          locale.translation,
          true,
          true
        )
        await act(() => i18next.changeLanguage(language))
        expect(
          screen.getByText(locale.translation['Final price'])
        ).toBeVisible()
        expect(screen.getByText(/0[.,]0001234×/)).toBeVisible()
      } finally {
        await act(() => i18next.changeLanguage('en'))
        i18next.removeResourceBundle(language, 'translation')
      }
    }
  )

  it('keeps expression prices and zero group ratios consistent in model details', () => {
    const client = new QueryClient({
      defaultOptions: { queries: { enabled: false } },
    })
    render(
      <QueryClientProvider client={client}>
        <ModelDetailsContent
          model={{
            ...model,
            billing_mode: 'tiered_expr',
            billing_expr: 'tier("base", p * 10 + c * 30)',
            group_ratio: { plus: 0, pro: 0.16 },
          }}
          groupRatio={{ plus: 0, pro: 0.16 }}
          usableGroup={{
            plus: { desc: '', ratio: 0 },
            pro: { desc: '', ratio: 0.16 },
          }}
          endpointMap={{}}
          autoGroups={[]}
          priceRate={3}
          usdExchangeRate={6}
          tokenUnit='M'
          showRechargePrice
        />
      </QueryClientProvider>
    )
    const freeGroup = screen.getByRole('group', { name: 'plus' })
    expect(freeGroup).toHaveTextContent('0x')
    expect(freeGroup).not.toHaveTextContent('$10')
    expect(freeGroup).toHaveTextContent('$0')
    expect(screen.getByText('$10')).toBeVisible()
    client.clear()
  })
})
