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
import { describe, expect, it } from 'vitest'

import {
  getOfficialModelPrice,
  getOfficialPricePercent,
  OFFICIAL_MODEL_PRICES,
  type OfficialModelPrice,
} from '../lib/official-prices'
import type { PricingModel } from '../types'

const official: OfficialModelPrice = {
  modelNames: ['gpt-4.1'],
  provider: 'OpenAI',
  currency: 'USD',
  tokenUnit: '1M',
  source: 'https://developers.openai.com/api/docs/models/gpt-4.1',
  checkedAt: '2026-10-03',
  tiers: [{ input: 2, output: 8 }],
}
function model(overrides: Partial<PricingModel> = {}): PricingModel {
  return {
    id: 1,
    model_name: 'gpt-4.1',
    quota_type: 0,
    model_ratio: 1,
    completion_ratio: 2,
    enable_groups: ['default', 'premium'],
    group_ratio: { default: 0.5, premium: 2 },
    ...overrides,
  }
}

describe('official pricing catalog', () => {
  it('matches verified snapshots without guessing custom aliases or suffixes', () => {
    expect(getOfficialModelPrice('gpt-4.1-2025-04-14')).toBe(
      getOfficialModelPrice('gpt-4.1')
    )
    for (const name of [
      'gpt-4.1-custom',
      'gpt-4.1-mini',
      'vendor/gpt-4.1',
      'GPT-4.1',
      'unknown',
    ]) {
      expect(getOfficialModelPrice(name)).toBeUndefined()
    }
  })
  it('keeps catalog model IDs unique and all prices finite and nonnegative', () => {
    const names = OFFICIAL_MODEL_PRICES.flatMap((entry) => [
      ...entry.modelNames,
    ])
    expect(new Set(names).size).toBe(names.length)
    for (const entry of OFFICIAL_MODEL_PRICES) {
      expect(entry.source).toMatch(
        /^https:\/\/(developers\.openai\.com|platform\.claude\.com|ai\.google\.dev)\//
      )
      expect(entry.checkedAt).toBe('2026-10-03')
      for (const tier of entry.tiers) {
        expect(Number.isFinite(tier.input)).toBe(true)
        expect(Number.isFinite(tier.output)).toBe(true)
        expect(tier.input).toBeGreaterThanOrEqual(0)
        expect(tier.output).toBeGreaterThanOrEqual(0)
      }
    }
  })
})

describe('official price comparisons', () => {
  it('uses the lowest available group by default and compares input and output separately', () => {
    expect(getOfficialPricePercent(model(), official, 'input')).toBe(50)
    expect(getOfficialPricePercent(model(), official, 'output')).toBe(25)
    expect(
      getOfficialPricePercent(model(), official, 'input', {
        selectedGroup: 'premium',
      })
    ).toBe(200)
  })
  it('preserves a zero group multiplier and zero model price', () => {
    expect(
      getOfficialPricePercent(
        model({ group_ratio: { default: 0 } }),
        official,
        'input'
      )
    ).toBe(0)
    expect(
      getOfficialPricePercent(model({ model_ratio: 0 }), official, 'output')
    ).toBe(0)
  })
  it('uses raw prices without rounding and is independent of token display unit', () => {
    const fractional = model({ model_ratio: 0.00000013 })
    expect(
      getOfficialPricePercent(fractional, official, 'input', { tokenUnit: 'M' })
    ).toBeCloseTo(0.0000065, 12)
    expect(
      getOfficialPricePercent(fractional, official, 'input', { tokenUnit: 'K' })
    ).toBeCloseTo(0.0000065, 12)
  })
  it('applies recharge conversion only to platform prices', () => {
    expect(
      getOfficialPricePercent(model(), official, 'input', {
        showRechargePrice: true,
        priceRate: 3,
        usdExchangeRate: 6,
      })
    ).toBe(25)
    expect(
      getOfficialPricePercent(model(), official, 'input', {
        priceRate: 3,
        usdExchangeRate: 6,
      })
    ).toBe(50)
  })
  it('supports unconditional token expressions and group selection', () => {
    const dynamic = model({
      billing_mode: 'tiered_expr',
      billing_expr: 'tier("base", p * 1 + c * 2)',
    })
    expect(getOfficialPricePercent(dynamic, official, 'input')).toBe(25)
    expect(getOfficialPricePercent(dynamic, official, 'output')).toBe(12.5)
    expect(
      getOfficialPricePercent(dynamic, official, 'output', {
        selectedGroup: 'premium',
        showRechargePrice: true,
        priceRate: 3,
        usdExchangeRate: 6,
      })
    ).toBe(25)
  })
  it.each([
    {
      billing_mode: 'tiered_expr',
      billing_expr:
        'len <= 200000 ? tier("short", p * 1 + c * 2) : tier("long", p * 2 + c * 4)',
    },
    {
      billing_mode: 'tiered_expr',
      billing_expr:
        'hour("Asia/Shanghai") < 12 ? tier("early", p * 1 + c * 2) : tier("late", p * 3 + c * 4)',
    },
    {
      billing_mode: 'tiered_expr',
      billing_expr: 'tier("request", fixed(0.01))',
    },
    { billing_mode: 'tiered_expr', billing_expr: '' },
    { quota_type: 1, model_price: 0.01 },
    {
      billing_usage_schema: {
        seconds: { type: 'number' as const, unit: 'second' as const },
      },
    },
    { model_ratio: Number.NaN },
    { model_ratio: -1 },
  ])(
    'does not compute a discount for incomparable or invalid billing: %j',
    (overrides) => {
      expect(
        getOfficialPricePercent(model(overrides), official, 'input')
      ).toBeUndefined()
    }
  )
  it('does not compare a tiered reference with a flat platform price', () => {
    const pro = getOfficialModelPrice('gemini-2.5-pro')
    if (!pro) throw new Error('Gemini 2.5 Pro official price is missing')
    expect(getOfficialPricePercent(model(), pro, 'input')).toBeUndefined()
    expect(
      getOfficialPricePercent(
        model(),
        {
          ...official,
          tiers: [{ input: 1, output: 2, maxPromptTokens: 1000 }],
        },
        'input'
      )
    ).toBeUndefined()
  })
  it('does not divide by zero or produce an invalid percentage', () => {
    expect(
      getOfficialPricePercent(
        model(),
        { ...official, tiers: [{ input: 0, output: 8 }] },
        'input'
      )
    ).toBeUndefined()
    expect(
      getOfficialPricePercent(model(), official, 'input', {
        showRechargePrice: true,
        usdExchangeRate: 0,
      })
    ).toBeUndefined()
  })
})
