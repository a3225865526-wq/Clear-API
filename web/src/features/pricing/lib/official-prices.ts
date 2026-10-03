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
import { TOKEN_UNIT_DIVISORS } from '../constants'
import type { PricingModel, TokenUnit } from '../types'
import { getDynamicPricingSummary, hasTaskUsageSchema } from './dynamic-price'
import { getDisplayGroupRatio } from './model-helpers'
import { calculateDisplayTokenPrice } from './price'

export type OfficialPriceTier = {
  /** Standard text token prices in USD per million tokens. */
  input: number
  output: number
  maxPromptTokens?: number
  minPromptTokens?: number
}

export type OfficialModelPrice = {
  modelNames: readonly string[]
  provider: string
  source: string
  checkedAt: string
  currency: 'USD'
  tokenUnit: '1M'
  tiers: readonly OfficialPriceTier[]
}

// Reviewed snapshots, never platform pricing defaults or upstream account costs.
// Match only explicitly verified IDs; custom aliases and suffixes stay unmatched.
const openaiSource = 'https://developers.openai.com/api/docs/models/'
const claudeSource = 'https://platform.claude.com/docs/en/about-claude/pricing'
const googleSource = 'https://ai.google.dev/gemini-api/docs/pricing'
const snapshot = {
  checkedAt: '2026-10-03',
  currency: 'USD',
  tokenUnit: '1M',
} as const

export const OFFICIAL_MODEL_PRICES: readonly OfficialModelPrice[] = [
  {
    ...snapshot,
    modelNames: ['gpt-4.1', 'gpt-4.1-2025-04-14'],
    provider: 'OpenAI',
    source: `${openaiSource}gpt-4.1`,
    tiers: [{ input: 2, output: 8 }],
  },
  {
    ...snapshot,
    modelNames: ['gpt-4o-mini', 'gpt-4o-mini-2024-07-18'],
    provider: 'OpenAI',
    source: `${openaiSource}gpt-4o-mini`,
    tiers: [{ input: 0.15, output: 0.6 }],
  },
  {
    ...snapshot,
    modelNames: ['gpt-5.2', 'gpt-5.2-2025-12-11'],
    provider: 'OpenAI',
    source: `${openaiSource}gpt-5.2`,
    tiers: [{ input: 1.75, output: 14 }],
  },
  {
    ...snapshot,
    modelNames: ['gpt-5.4'],
    provider: 'OpenAI',
    source: `${openaiSource}gpt-5.4`,
    tiers: [
      { input: 2.5, output: 15, maxPromptTokens: 272000 },
      { input: 5, output: 22.5, minPromptTokens: 272000 },
    ],
  },
  {
    ...snapshot,
    modelNames: ['gpt-5.3-codex'],
    provider: 'OpenAI',
    source: 'https://developers.openai.com/api/docs/pricing#specialized-models',
    tiers: [{ input: 1.75, output: 14 }],
  },
  {
    ...snapshot,
    modelNames: ['claude-opus-4-6'],
    provider: 'Anthropic',
    source: claudeSource,
    tiers: [{ input: 5, output: 25 }],
  },
  {
    ...snapshot,
    modelNames: ['claude-sonnet-4-6'],
    provider: 'Anthropic',
    source: claudeSource,
    tiers: [{ input: 3, output: 15 }],
  },
  {
    ...snapshot,
    modelNames: ['claude-haiku-4-5'],
    provider: 'Anthropic',
    source: claudeSource,
    tiers: [{ input: 1, output: 5 }],
  },
  {
    ...snapshot,
    modelNames: ['gemini-2.5-pro'],
    provider: 'Google',
    source: `${googleSource}#gemini-2.5-pro`,
    tiers: [
      { input: 1.25, output: 10, maxPromptTokens: 200000 },
      { input: 2.5, output: 15, minPromptTokens: 200000 },
    ],
  },
  {
    ...snapshot,
    modelNames: ['gemini-2.5-flash'],
    provider: 'Google',
    source: `${googleSource}#gemini-2.5-flash`,
    tiers: [{ input: 0.3, output: 2.5 }],
  },
  {
    ...snapshot,
    modelNames: ['gemini-2.5-flash-lite'],
    provider: 'Google',
    source: `${googleSource}#gemini-2.5-flash-lite`,
    tiers: [{ input: 0.1, output: 0.4 }],
  },
]

export function getOfficialModelPrice(
  modelName: string
): OfficialModelPrice | undefined {
  return OFFICIAL_MODEL_PRICES.find((price) =>
    price.modelNames.includes(modelName)
  )
}

export type OfficialComparisonOptions = {
  tokenUnit?: TokenUnit
  selectedGroup?: string
  showRechargePrice?: boolean
  priceRate?: number
  usdExchangeRate?: number
}

/** Percent of official price using unrounded comparable prices, including free sales. */
export function getOfficialPricePercent(
  model: PricingModel,
  official: OfficialModelPrice,
  type: 'input' | 'output',
  options: OfficialComparisonOptions = {}
): number | undefined {
  if (
    official.tiers.length !== 1 ||
    official.tiers[0].maxPromptTokens !== undefined ||
    official.tiers[0].minPromptTokens !== undefined ||
    model.quota_type !== 0 ||
    hasTaskUsageSchema(model) ||
    model.billing_plugin_variants?.length
  ) {
    return undefined
  }

  const tokenUnit = options.tokenUnit ?? 'M'
  const officialPrice = official.tiers[0][type] / TOKEN_UNIT_DIVISORS[tokenUnit]
  if (!Number.isFinite(officialPrice) || officialPrice <= 0) return undefined

  let platformPrice: number
  const dynamic = getDynamicPricingSummary(model, { tokenUnit })
  if (dynamic) {
    // Only unconditional token expressions describe a single comparable price.
    if (
      dynamic.isSpecialExpression ||
      dynamic.isTaskUsage ||
      dynamic.isTimePricing ||
      dynamic.isMixedBilling ||
      dynamic.hasRequestRules ||
      dynamic.tierCount !== 1 ||
      !dynamic.tier ||
      dynamic.tier.conditions.length > 0 ||
      dynamic.tier.conditionText
    ) {
      return undefined
    }
    const entry = dynamic.entries.find(
      (item) => item.field === `${type}Price` && item.unit === 'token'
    )
    if (!entry) return undefined
    platformPrice =
      entry.value * getDisplayGroupRatio(model, options.selectedGroup)
    if (options.showRechargePrice) {
      platformPrice *= (options.priceRate ?? 1) / (options.usdExchangeRate ?? 1)
    }
    platformPrice /= TOKEN_UNIT_DIVISORS[tokenUnit]
  } else {
    // An expression in an unsupported billing mode must not fall back to ratios.
    if (model.billing_mode === 'tiered_expr') return undefined
    platformPrice = calculateDisplayTokenPrice(
      model,
      type,
      tokenUnit,
      options.showRechargePrice,
      options.priceRate,
      options.usdExchangeRate,
      options.selectedGroup
    )
  }
  if (!Number.isFinite(platformPrice) || platformPrice < 0) return undefined
  const percent = (platformPrice / officialPrice) * 100
  return Number.isFinite(percent) ? percent : undefined
}
