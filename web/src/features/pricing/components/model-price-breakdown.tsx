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
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { toIntlLocale } from '@/i18n/languages'

import { getDisplayGroup, getDisplayGroupRatio } from '../lib/model-helpers'
import type { PricingModel } from '../types'
import { ModelPriceCell, type ModelPriceCellOptions } from './model-price-cell'

/** Uses the same configured base prices and effective group ratios as the final price cells. */
export function ModelPriceBreakdown(props: {
  model: PricingModel
  options?: ModelPriceCellOptions
  children?: ReactNode
  showBasePrice?: boolean
}) {
  const { t, i18n } = useTranslation()
  const options = props.options ?? {}
  const group = getDisplayGroup(props.model, options.selectedGroup)
  const ratio = getDisplayGroupRatio(props.model, options.selectedGroup)
  // Group ratios allow more precision than ordinary two-decimal number displays.
  const multiplier = new Intl.NumberFormat(
    toIntlLocale(i18n.resolvedLanguage || i18n.language),
    {
      maximumSignificantDigits: 15,
    }
  ).format(ratio)
  const finalLabel = options.showRechargePrice
    ? t('Recharge equivalent price')
    : t('Final price')

  return (
    <div className='min-w-0 space-y-2'>
      {props.showBasePrice !== false && (
        <section aria-label={t('Base Price')} className='min-w-0 space-y-1'>
          <p className='text-muted-foreground text-xs font-medium'>
            {t('Base Price')}
          </p>
          <ModelPriceCell
            model={{ ...props.model, group_ratio: undefined }}
            options={{ tokenUnit: options.tokenUnit }}
            basePrice
            showExpression={false}
          />
        </section>
      )}
      <section aria-label={finalLabel} className='min-w-0 space-y-1.5'>
        <p className='text-muted-foreground text-xs font-medium'>
          {finalLabel}
        </p>
        {group && (
          <p className='text-muted-foreground text-xs break-words'>
            {t('Group: {{group}} · multiplier: {{multiplier}}×', {
              group,
              multiplier,
            })}
          </p>
        )}
        {props.children ?? (
          <ModelPriceCell model={props.model} options={options} />
        )}
        <p className='text-muted-foreground text-xs break-words'>
          {options.showRechargePrice
            ? t('Base price × effective group multiplier × recharge conversion')
            : t('Base price × effective group multiplier')}
        </p>
        {(!options.selectedGroup || options.selectedGroup === 'all') && (
          <p className='text-muted-foreground text-xs'>
            {t('Lowest price across available groups')}
          </p>
        )}
      </section>
    </div>
  )
}
