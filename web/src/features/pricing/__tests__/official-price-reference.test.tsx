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
import { act, fireEvent, render, screen, within } from '@testing-library/react'
import i18next from 'i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import zhLocale from '@/i18n/locales/zh.json'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { ModelCard } from '../components/model-card'
import { OfficialPriceReference } from '../components/official-price-reference'
import { PricingTable } from '../components/pricing-table'
import type { PricingModel } from '../types'

function model(overrides: Partial<PricingModel> = {}): PricingModel {
  return {
    id: 1,
    model_name: 'gpt-4.1',
    quota_type: 0,
    model_ratio: 1,
    completion_ratio: 4,
    enable_groups: ['default', 'premium'],
    group_ratio: { default: 0.5, premium: 2 },
    ...overrides,
  }
}
beforeEach(() => {
  useSystemConfigStore.getState().setConfig({
    currency: { ...DEFAULT_CURRENCY_CONFIG, quotaDisplayType: 'USD' },
  })
})
afterEach(async () => {
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
  await i18next.changeLanguage('en')
  i18next.removeResourceBundle('zhCN', 'translation')
})

describe('official price reference display', () => {
  it('uses the actual Chinese translation resource for official references', async () => {
    i18next.addResourceBundle(
      'zhCN',
      'translation',
      zhLocale.translation,
      true,
      true
    )
    await i18next.changeLanguage('zhCN')
    render(<OfficialPriceReference model={model()} />)
    expect(screen.getByRole('region', { name: '官方参考价' })).toBeVisible()
    expect(screen.getAllByText('本平台为官方价的 50%')).toHaveLength(2)
    expect(screen.getByText('核对日期 2026-10-03')).toBeVisible()
  })
  it('shows missing references explicitly instead of synthesizing a price', () => {
    render(
      <OfficialPriceReference model={model({ model_name: 'gpt-4.1-custom' })} />
    )
    expect(screen.getByText('Official price unavailable')).toBeVisible()
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })
  it('shows source, checked date and prices in the selected token unit', () => {
    const { rerender } = render(<OfficialPriceReference model={model()} />)
    const ref = screen.getByRole('region', { name: 'Official reference price' })
    expect(within(ref).getByText('2')).toBeVisible()
    expect(within(ref).getByText('8')).toBeVisible()
    expect(within(ref).getByText('Checked 2026-10-03')).toBeVisible()
    expect(
      within(ref).getByRole('link', { name: 'OpenAI official pricing source' })
    ).toHaveAttribute(
      'href',
      'https://developers.openai.com/api/docs/models/gpt-4.1'
    )
    rerender(
      <OfficialPriceReference model={model()} options={{ tokenUnit: 'K' }} />
    )
    expect(within(ref).getByText('0.002')).toBeVisible()
    expect(within(ref).getByText('0.008')).toBeVisible()
    expect(
      within(ref).getByText(
        'USD / 1K tokens · Standard API · text tokens · global pricing'
      )
    ).toBeVisible()
  })
  it('changes percentages with group and recharge options without changing official prices', () => {
    const { rerender } = render(<OfficialPriceReference model={model()} />)
    expect(screen.getAllByText('Platform is 50% of official')).toHaveLength(2)
    rerender(
      <OfficialPriceReference
        model={model()}
        options={{
          selectedGroup: 'premium',
          showRechargePrice: true,
          priceRate: 3,
          usdExchangeRate: 6,
        }}
      />
    )
    expect(screen.getAllByText('Platform is 100% of official')).toHaveLength(2)
    expect(screen.getByText('2')).toBeVisible()
    expect(screen.getByText('8')).toBeVisible()
  })
  it('converts official prices with the same display currency and reacts to changes', () => {
    useSystemConfigStore.getState().setConfig({
      currency: {
        ...DEFAULT_CURRENCY_CONFIG,
        quotaDisplayType: 'CNY',
        usdExchangeRate: 7,
      },
    })
    render(
      <OfficialPriceReference
        model={model()}
        options={{
          showRechargePrice: true,
          priceRate: 3.5,
          usdExchangeRate: 7,
        }}
      />
    )
    expect(screen.getByText('14')).toBeVisible()
    expect(screen.getByText('56')).toBeVisible()
    expect(screen.getAllByText('Platform is 25% of official')).toHaveLength(2)
    act(() => {
      useSystemConfigStore.getState().setConfig({
        currency: { ...DEFAULT_CURRENCY_CONFIG, quotaDisplayType: 'USD' },
      })
    })
    expect(screen.getByText('2')).toBeVisible()
    expect(screen.getByText('8')).toBeVisible()
    expect(screen.getAllByText('Platform is 25% of official')).toHaveLength(2)
  })
  it.each(['zhCN', 'zhTW', 'en', 'fr', 'ru', 'ja', 'vi', 'invalid_locale'])(
    'formats references after switching to %s',
    async (language) => {
      render(
        <OfficialPriceReference model={model({ model_name: 'gpt-4o-mini' })} />
      )
      expect(screen.getByText('0.15')).toBeVisible()
      await act(async () => {
        await i18next.changeLanguage(language)
      })
      const region = screen.getByRole('region', {
        name: 'Official reference price',
      })
      const input = within(region).getAllByRole('definition')[0]
      if (language === 'fr' || language === 'ru' || language === 'vi') {
        expect(input).toHaveTextContent('0,15')
      } else {
        expect(input).toHaveTextContent('0.15')
      }
    }
  )
  it('shows both official tiers with conditions without advertising an unmatched discount', () => {
    render(
      <OfficialPriceReference model={model({ model_name: 'gemini-2.5-pro' })} />
    )
    expect(screen.getByText('Prompt ≤ 200,000 tokens')).toBeVisible()
    expect(screen.getByText('Prompt > 200,000 tokens')).toBeVisible()
    expect(
      screen.getByText('Reference only; billing conditions may differ')
    ).toBeVisible()
    expect(screen.queryByText(/Platform is .*%/)).not.toBeInTheDocument()
  })
  it('integrates both prices in a card and keeps source clicks independent of model details', () => {
    const onClick = vi.fn()
    render(<ModelCard model={model()} onClick={onClick} />)
    expect(screen.getByText('Platform price')).toBeVisible()
    expect(
      screen.getByText('Lowest price across available groups')
    ).toBeVisible()
    expect(
      screen.getByRole('region', { name: 'Official reference price' })
    ).toBeVisible()
    fireEvent.click(
      screen.getByRole('link', { name: 'OpenAI official pricing source' })
    )
    expect(onClick).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Details' }))
    expect(onClick).toHaveBeenCalledWith('gpt-4.1')
  })
  it('integrates comparison in list rows and prevents source clicks from opening details', () => {
    const onModelClick = vi.fn()
    render(<PricingTable models={[model()]} onModelClick={onModelClick} />)
    expect(screen.getByText('Platform price')).toBeVisible()
    expect(
      screen.getByRole('region', { name: 'Official reference price' })
    ).toBeVisible()
    fireEvent.click(
      screen.getByRole('link', { name: 'OpenAI official pricing source' })
    )
    expect(onModelClick).not.toHaveBeenCalled()
    fireEvent.click(screen.getByText('gpt-4.1'))
    expect(onModelClick).toHaveBeenCalledWith('gpt-4.1')
  })
})
