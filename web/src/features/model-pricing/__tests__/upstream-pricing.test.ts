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

import { compileBillingExpression } from '@/features/pricing/lib/billing-expression/parser'

import { buildPricingChanges, type ModelPricingConfig } from '../api'
import { pricingOptions } from '../pricing'
import {
  applyGeneratedUpstreamPricing,
  generatePriceTableFromExpression,
  generateUpstreamPricing,
} from '../upstream-pricing'

const table = {
  currency: 'USD',
  token_unit: '1M',
  models: [{ model: 'example', input: 10, output: 30, cache_read: 0 }],
}

describe('upstream screenshot pricing', () => {
  it('prices sales against the base rather than the discounted upstream cost', () => {
    const generated = generateUpstreamPricing(table, 0.5, 0.8, 1)
    expect(generated.rows[0]).toMatchObject({
      base: 10,
      cost: 5,
      sale: 8,
      difference: 3,
    })
    expect(generated.models[0].expression).toBe(
      'tier("upstream_1", p * 8 + c * 24 + cr * 0)'
    )
    expect(
      compileBillingExpression(generated.models[0].expression).status
    ).toBe('ready')
    expect(generateUpstreamPricing(table, 0.2, 0.8, 1).rows[0]).toMatchObject({
      cost: 2,
      sale: 8,
    })
  })

  it('converts CNY per thousand tokens to USD per million and preserves free cache', () => {
    const generated = generateUpstreamPricing(
      {
        ...table,
        currency: 'CNY',
        token_unit: '1K',
        models: [
          { model: 'example', input: 0.07, output: 0.21, cache_read: 0 },
        ],
      },
      0.5,
      0.8,
      7
    )
    expect(generated.rows[0]).toMatchObject({
      base: 10,
      cost: 5,
      sale: 8,
      unit: '1M',
    })
    expect(generated.models[0].expression).toContain('cr * 0')
    expect(generated.models[0].expression).toContain('c * 24')
  })

  it('keeps per-request prices independent of the token unit', () => {
    const generated = generateUpstreamPricing(
      {
        ...table,
        token_unit: '1K',
        models: [{ model: 'request-model', request: 0.1 }],
      },
      0.5,
      2,
      1
    )
    expect(generated.rows[0]).toMatchObject({
      cost: 0.05,
      sale: 0.2,
      unit: 'request',
    })
    expect(generated.models[0].expression).toBe(
      'tier("upstream_1", fixed(0.2))'
    )
    expect(
      compileBillingExpression(generated.models[0].expression).status
    ).toBe('ready')
  })

  it('uses total context for tier limits and generates all cache categories', () => {
    const generated = generateUpstreamPricing(
      {
        ...table,
        models: [
          {
            model: 'tiered',
            tiers: [
              {
                max_context: 200000,
                input: 10,
                output: 30,
                cache_read: 1,
                cache_write: 12.5,
                cache_write_1h: 20,
              },
              { input: 20, output: 60 },
            ],
          },
        ],
      },
      0.5,
      0.8,
      1
    )
    expect(generated.models[0].expression).toBe(
      'len <= 200000 ? tier("upstream_1", p * 8 + c * 24 + cr * 0.8 + cc * 10 + cc1h * 16) : tier("upstream_2", p * 16 + c * 48)'
    )
    expect(
      compileBillingExpression(generated.models[0].expression).status
    ).toBe('ready')
  })

  it.each([0, -1, Number.NaN, Infinity, 1e7])(
    'rejects invalid multiplier %s',
    (value) => {
      expect(() => generateUpstreamPricing(table, value, 0.8, 1)).toThrow()
      expect(() => generateUpstreamPricing(table, 0.5, value, 1)).toThrow()
    }
  )

  it.each([
    { ...table, models: [] },
    { ...table, currency: 'unknown' },
    { ...table, token_unit: 'unknown' },
    { ...table, models: [table.models[0], table.models[0]] },
    { ...table, models: [{ model: 'missing-output', input: 10 }] },
    {
      ...table,
      models: [{ model: 'unknown-field', input: 10, output: 30, duration: 1 }],
    },
    { ...table, models: [{ model: 'negative', input: -1, output: 30 }] },
    {
      ...table,
      models: [
        {
          model: 'bounded-last-tier',
          tiers: [{ input: 10, output: 30, max_context: 200000 }],
        },
      ],
    },
    {
      ...table,
      models: [
        {
          model: 'unordered',
          tiers: [
            { input: 10, output: 30, max_context: 200000 },
            { input: 10, output: 30, max_context: 100000 },
            { input: 10, output: 30 },
          ],
        },
      ],
    },
  ])('rejects ambiguous or incomplete price tables', (input) => {
    expect(() => generateUpstreamPricing(input, 0.5, 0.8, 1)).toThrow()
  })

  it('refuses invalid exchange rates, overflowing prices and underflow to free prices', () => {
    expect(() =>
      generateUpstreamPricing({ ...table, currency: 'CNY' }, 0.5, 0.8, 0)
    ).toThrow()
    expect(() =>
      generateUpstreamPricing(
        { ...table, models: [{ model: 'large', input: 1e9, output: 1e9 }] },
        1e6,
        1e6,
        1
      )
    ).toThrow()
    expect(() =>
      generateUpstreamPricing(table, Number.MIN_VALUE, Number.MIN_VALUE, 1)
    ).not.toThrow()
    expect(() =>
      generateUpstreamPricing(
        {
          ...table,
          models: [{ model: 'tiny', input: Number.MIN_VALUE, output: 1 }],
        },
        0.5,
        0.8,
        1
      )
    ).toThrow()
  })

  it('imports only named models while retaining unrelated draft prices, provider prices and version checks', () => {
    const options = pricingOptions({
      ModelRatio: '{"example":1,"unrelated":7}',
      PluginBillingExpr:
        '{"provider::example":"tier(\\"base\\", u(\\"seconds\\") * 1)"}',
    })
    const snapshot: ModelPricingConfig = {
      options,
      empty_version: 'empty',
      entries: [
        {
          model_name: 'example',
          version: 'v1',
          configured: { ModelRatio: 1 },
          effective: { ModelRatio: 1 },
        },
      ],
    }
    const generated = generateUpstreamPricing(table, 0.5, 0.8, 1)
    const updated = applyGeneratedUpstreamPricing(options, generated)
    expect(JSON.parse(updated.ModelRatio)).toEqual({ unrelated: 7 })
    expect(updated['billing_setting.plugin_billing_expr']).toBe(
      options['billing_setting.plugin_billing_expr']
    )
    const changes = buildPricingChanges(snapshot, options, updated)
    expect(changes).toHaveLength(1)
    expect(changes[0]).toMatchObject({
      model_name: 'example',
      expected_version: 'v1',
      pricing: {
        'billing_setting.billing_mode': 'tiered_expr',
        'billing_setting.billing_expr': generated.models[0].expression,
      },
    })
    expect(options.ModelRatio).toBe('{"example":1,"unrelated":7}')
  })

  it('recovers the upstream price table from a stored expression so a sales price can be generated', () => {
    const generated = generateUpstreamPricing(table, 0.5, 0.8, 1)

    const recovered = generatePriceTableFromExpression(
      'example',
      generated.models[0].expression
    )

    // The recovered table carries the coefficients stored in the expression
    // (the generated sales prices), so regenerating from it is lossless.
    expect(recovered).toEqual({
      currency: 'USD',
      token_unit: '1M',
      models: [{ model: 'example', input: 8, output: 24, cache_read: 0 }],
    })
  })

  it('recovers a per-request upstream price and reports nothing for an expression without prices', () => {
    expect(
      generatePriceTableFromExpression('flat', 'tier("request", fixed(0.02))')
    ).toEqual({
      currency: 'USD',
      token_unit: '1M',
      models: [{ model: 'flat', request: 0.02 }],
    })

    expect(
      generatePriceTableFromExpression('usage', 'tier("base", u("seconds") * 1)')
    ).toBeNull()
  })
})
