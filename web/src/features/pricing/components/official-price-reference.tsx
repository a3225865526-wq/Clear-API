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
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import { toIntlLocale } from '@/i18n/languages'
import { formatBillingCurrencyFromUSD, getCurrencyLabel } from '@/lib/currency'
import { formatNumber } from '@/lib/format'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { DEFAULT_TOKEN_UNIT, TOKEN_UNIT_DIVISORS } from '../constants'
import {
  getOfficialModelPrice,
  getOfficialPricePercent,
} from '../lib/official-prices'
import type { PricingModel } from '../types'
import type { ModelPriceCellOptions } from './model-price-cell'

/** Shared catalog reference for cards and list rows; it never changes billing. */
export function OfficialPriceReference(props: {
  model: PricingModel
  options?: ModelPriceCellOptions
}) {
  const { t, i18n } = useTranslation()
  const currency = useSystemConfigStore((state) => state.config.currency)
  const official = getOfficialModelPrice(props.model.model_name)
  const options = props.options ?? {}
  const tokenUnit = options.tokenUnit ?? DEFAULT_TOKEN_UNIT
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const currencyLabel =
    currency.quotaDisplayType === 'TOKENS' ? 'USD' : getCurrencyLabel()
  const unit = tokenUnit === 'K' ? '1K' : '1M'

  if (!official) {
    return (
      <p className='text-muted-foreground text-xs'>
        {t('Official price unavailable')}
      </p>
    )
  }

  return (
    <section
      aria-label={t('Official reference price')}
      className='min-w-0 space-y-1.5'
    >
      <Separator />
      <p className='text-muted-foreground text-xs font-medium'>
        {t('Official reference price')}
      </p>
      {official.tiers.map((tier) => (
        <div
          key={`${tier.minPromptTokens ?? 0}-${tier.maxPromptTokens ?? 'all'}`}
          className='min-w-0 space-y-1'
        >
          {tier.maxPromptTokens !== undefined && (
            <p className='text-muted-foreground text-xs'>
              {t('Prompt ≤ {{limit}} tokens', {
                limit: formatNumber(tier.maxPromptTokens, locale),
              })}
            </p>
          )}
          {tier.minPromptTokens !== undefined && (
            <p className='text-muted-foreground text-xs'>
              {t('Prompt > {{limit}} tokens', {
                limit: formatNumber(tier.minPromptTokens, locale),
              })}
            </p>
          )}
          <dl className='grid min-w-0 grid-cols-1 gap-x-3 gap-y-1 sm:grid-cols-2'>
            {(['input', 'output'] as const).map((type) => {
              const percent = getOfficialPricePercent(
                props.model,
                official,
                type,
                options
              )
              return (
                <div key={type} className='min-w-0'>
                  <dt className='text-muted-foreground text-xs'>
                    {type === 'input' ? t('Input') : t('Output')}
                  </dt>
                  <dd className='font-mono text-sm break-words tabular-nums'>
                    {formatBillingCurrencyFromUSD(
                      tier[type] / TOKEN_UNIT_DIVISORS[tokenUnit],
                      {
                        showSymbol: false,
                        digitsLarge: 4,
                        digitsSmall: 6,
                        abbreviate: false,
                        locale,
                      }
                    )}
                  </dd>
                  {percent !== undefined && (
                    <dd className='text-muted-foreground text-xs break-words'>
                      {t('Platform is {{percent}}% of official', {
                        percent: formatNumber(percent, locale),
                      })}
                    </dd>
                  )}
                </div>
              )
            })}
          </dl>
        </div>
      ))}
      <p className='text-muted-foreground text-xs break-words'>
        {t('{{currency}} / {{unit}} tokens', { currency: currencyLabel, unit })}
        {' · '}
        {t('Standard API · text tokens · global pricing')}
      </p>
      {(official.tiers.length > 1 ||
        getOfficialPricePercent(props.model, official, 'input', options) ===
          undefined) && (
        <p className='text-muted-foreground text-xs'>
          {t('Reference only; billing conditions may differ')}
        </p>
      )}
      <div className='flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-xs'>
        <Button
          role='link'
          variant='link'
          size='xs'
          className='h-auto min-h-7 px-0 text-xs whitespace-normal'
          render={
            <a
              href={official.source}
              target='_blank'
              rel='noopener noreferrer'
            />
          }
          onClick={(event) => event.stopPropagation()}
          onKeyDown={(event) => event.stopPropagation()}
          aria-label={t('{{provider}} official pricing source', {
            provider: official.provider,
          })}
        >
          {t('Official source')} · {official.provider}
        </Button>
        <span className='text-muted-foreground'>
          {t('Checked {{date}}', { date: official.checkedAt })}
        </span>
      </div>
    </section>
  )
}
