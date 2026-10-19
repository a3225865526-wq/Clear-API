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
import * as z from 'zod'

import { applyPricingDraft, type PricingOptions } from './pricing'

const amount = z.number().finite().nonnegative().max(1e9)
const tokenPrices = z
  .object({
    input: amount,
    output: amount,
    cache_read: amount.optional(),
    cache_write: amount.optional(),
    cache_write_1h: amount.optional(),
    image_input: amount.optional(),
    image_output: amount.optional(),
    image_cache_read: amount.optional(),
    audio_input: amount.optional(),
    audio_output: amount.optional(),
  })
  .strict()
const tier = tokenPrices
  .extend({
    max_context: z.number().int().positive().max(2147483647).optional(),
  })
  .strict()
const modelName = z.string().trim().min(1).max(200)
const model = z.union([
  tokenPrices.extend({ model: modelName }).strict(),
  z.object({ model: modelName, tiers: z.array(tier).min(1).max(20) }).strict(),
  z.object({ model: modelName, request: amount }).strict(),
])

export const upstreamPriceTableSchema = z
  .object({
    currency: z.enum(['USD', 'CNY']),
    token_unit: z.enum(['1M', '1K']),
    models: z.array(model).min(1).max(500),
  })
  .strict()
  .superRefine((table, context) => {
    const names = new Set<string>()
    table.models.forEach((entry, index) => {
      if (names.has(entry.model)) {
        context.addIssue({
          code: 'custom',
          path: ['models', index, 'model'],
          message: 'Duplicate model name',
        })
      }
      names.add(entry.model)
      if (!('tiers' in entry)) return
      let previous = 0
      entry.tiers.forEach((item, tierIndex) => {
        const last = tierIndex === entry.tiers.length - 1
        if (
          (last && item.max_context !== undefined) ||
          (!last &&
            (item.max_context === undefined || item.max_context <= previous))
        ) {
          context.addIssue({
            code: 'custom',
            path: ['models', index, 'tiers', tierIndex, 'max_context'],
            message:
              'Tier limits must increase; the final tier must have no limit',
          })
        }
        previous = item.max_context ?? previous
      })
    })
  })

export const upstreamMultiplierSchema = z.number().finite().positive().max(1e6)
export type UpstreamPriceTable = z.infer<typeof upstreamPriceTableSchema>

export const UPSTREAM_PRICE_FIELDS = [
  ['input', 'p', 'Input price'],
  ['output', 'c', 'Output price'],
  ['cache_read', 'cr', 'Cache read price'],
  ['cache_write', 'cc', 'Cache write price'],
  ['cache_write_1h', 'cc1h', 'Cache write price (1 hour)'],
  ['image_input', 'img', 'Image input price'],
  ['image_output', 'img_o', 'Image output price'],
  ['image_cache_read', 'img_cr', 'Image cache read price'],
  ['audio_input', 'ai', 'Audio input price'],
  ['audio_output', 'ao', 'Audio output price'],
] as const

// Convert an applied billing expression back into the upstream price table the
// generator accepts, so the same sales-price workflow is available for a model
// whose upstream cost is already stored as an expression.
export function generatePriceTableFromExpression(
  model: string,
  expression: string
): UpstreamPriceTable | null {
  const fixed = /fixed\(\s*([0-9.]+)\s*\)/.exec(expression)
  if (fixed) {
    const request = Number(fixed[1])
    return Number.isFinite(request)
      ? { currency: 'USD', token_unit: '1M', models: [{ model, request }] }
      : null
  }
  // The generator emits `<variable> * <USD per 1M tokens>` terms, so the
  // variable name decides which upstream price field the coefficient is.
  const prices: Record<string, number> = {}
  for (const [field, variable] of UPSTREAM_PRICE_FIELDS) {
    const match = new RegExp(`\\b${variable}\\s*\\*\\s*([0-9.]+)`).exec(
      expression
    )
    if (!match) continue
    const price = Number(match[1])
    if (Number.isFinite(price)) prices[field] = price
  }
  if (!Object.keys(prices).length) return null
  return {
    currency: 'USD',
    token_unit: '1M',
    models: [
      {
        model,
        input: prices.input ?? 0,
        output: prices.output ?? 0,
        ...Object.fromEntries(
          Object.entries(prices).filter(
            ([field]) => field !== 'input' && field !== 'output'
          )
        ),
      } as UpstreamPriceTable['models'][number],
    ],
  }
}

export type GeneratedPriceRow = {
  model: string
  tier: number
  maxContext?: number
  label: string
  unit: '1M' | 'request'
  base: number
  cost: number
  sale: number
  difference: number
}
export type GeneratedUpstreamPricing = {
  rows: GeneratedPriceRow[]
  models: { model: string; expression: string }[]
}

// Upstream and sales multipliers both use the undiscounted base price.
// Money is normalized to USD; coefficients are USD per million tokens.
export function generateUpstreamPricing(
  input: unknown,
  upstreamMultiplier: number,
  salesMultiplier: number,
  cnyPerUsd: number
): GeneratedUpstreamPricing {
  const table = upstreamPriceTableSchema.parse(input)
  upstreamMultiplierSchema.parse(upstreamMultiplier)
  upstreamMultiplierSchema.parse(salesMultiplier)
  if (table.currency === 'CNY') upstreamMultiplierSchema.parse(cnyPerUsd)
  const exchange = table.currency === 'CNY' ? cnyPerUsd : 1
  const rows: GeneratedPriceRow[] = []
  const models: GeneratedUpstreamPricing['models'] = []
  for (const entry of table.models) {
    const tiers = 'tiers' in entry ? entry.tiers : [entry]
    const expressions: string[] = []
    tiers.forEach((prices, index) => {
      const request = 'request' in prices
      const unitScale = !request && table.token_unit === '1K' ? 1000 : 1
      const terms: string[] = []
      const fields = request
        ? [['request', '', 'Price per request'] as const]
        : UPSTREAM_PRICE_FIELDS
      for (const [field, variable, label] of fields) {
        const value = (prices as Record<string, unknown>)[field]
        if (typeof value !== 'number') continue
        const base = (value / exchange) * unitScale
        const cost = base * upstreamMultiplier
        const sale = base * salesMultiplier
        if (
          ![base, cost, sale].every(
            (price) => Number.isFinite(price) && price <= 1e12
          ) ||
          (value > 0 && (base === 0 || cost === 0 || sale === 0))
        ) {
          throw new Error('Generated price is outside the supported range')
        }
        // Retain 15 significant digits while removing multiplication artifacts.
        const normalizedSale = Number(sale.toPrecision(15))
        const maxContext =
          'max_context' in prices ? prices.max_context : undefined
        rows.push({
          model: entry.model,
          tier: index + 1,
          maxContext,
          label,
          unit: request ? 'request' : '1M',
          base,
          cost,
          sale: normalizedSale,
          difference: normalizedSale - cost,
        })
        terms.push(
          request
            ? `fixed(${normalizedSale})`
            : `${variable} * ${normalizedSale}`
        )
      }
      const expression = `tier("upstream_${index + 1}", ${terms.join(' + ')})`
      if ('max_context' in prices && prices.max_context !== undefined) {
        expressions.push(`len <= ${prices.max_context} ? ${expression} : `)
      } else {
        expressions.push(expression)
      }
    })
    models.push({ model: entry.model, expression: expressions.join('') })
  }
  return { rows, models }
}

// Apply only the explicitly imported models to the current unsaved options.
// Existing provider/plugin prices are preserved by the shared draft helper.
export function applyGeneratedUpstreamPricing(
  options: PricingOptions,
  generated: GeneratedUpstreamPricing
): PricingOptions {
  return generated.models.reduce(
    (draft, entry) =>
      applyPricingDraft(draft, {
        name: entry.model,
        billingMode: 'tiered_expr',
        billingExpr: entry.expression,
      }),
    options
  )
}
