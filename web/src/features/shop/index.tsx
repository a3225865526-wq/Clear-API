import { useQuery } from '@tanstack/react-query'
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
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { ErrorState } from '@/components/error-state'
import { PublicLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'
import { formatLocalCurrencyAmount } from '@/lib/currency'
import { formatNumber } from '@/lib/format'

import { getCatalog } from './api'
import {
  creditProducts,
  storePurchaseUrl,
  trustedPurchaseUrl,
  type CreditProduct,
} from './catalog'

export function CreditCatalog(props: { products?: readonly CreditProduct[] }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  return (
    <div className='mx-auto w-full max-w-4xl space-y-6'>
      <div className='space-y-2'>
        <h1 className='text-3xl font-semibold'>{t('Buy credits')}</h1>
        <p className='text-muted-foreground'>
          {t(
            'Buy a recharge code at Catfk, then return to your wallet to redeem it.'
          )}
        </p>
      </div>
      <section className='bg-card text-card-foreground space-y-3 rounded-xl border p-5'>
        <h2 className='font-semibold'>
          {t('Contact support for large recharges with no handling fee!')}
        </h2>
        <div className='flex flex-wrap items-center gap-2'>
          <span>QQ: 3225865526</span>
          <CopyButton value='3225865526' tooltip={t('Copy support QQ')} />
        </div>
        <p className='text-muted-foreground text-sm'>
          {t('Confirm purchase and fee arrangements with support first.')}
        </p>
      </section>
      <section className='space-y-3 rounded-xl border p-5'>
        <h2 className='font-semibold'>
          {t('ClearAPI API credit recharge code')}
        </h2>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Existing purchase entry. Confirm the delivered API credit amount with the seller before purchasing.'
          )}
        </p>
        <Button
          role='link'
          className='min-h-11'
          render={
            <a
              href={storePurchaseUrl}
              target='_blank'
              rel='noopener noreferrer'
            />
          }
        >
          {t('View price and buy')}
        </Button>
      </section>
      <div className='grid grid-cols-1 gap-4 sm:grid-cols-2'>
        {(props.products ?? creditProducts).map((product) => {
          const url = trustedPurchaseUrl(product.purchase_url)
          if (!product.published || !product.delivery_verified || !url) {
            return null
          }
          return (
            <article
              key={product.id}
              className='bg-card text-card-foreground min-w-0 space-y-5 rounded-xl border p-5 sm:p-6'
            >
              <h2 className='text-xl font-semibold'>{product.name}</h2>
              <p>
                ¥{formatNumber(product.price_cents / 100, locale)} · $
                {formatLocalCurrencyAmount(
                  Number(product.base_credit ?? product.credit_cents / 100),
                  { showSymbol: false, digitsLarge: 6, digitsSmall: 6, locale }
                )}{' '}
                {t('platform API usage credit')} ·{' '}
                {t('Base credit; conditional bonuses are separate')}
              </p>
              <p className='text-muted-foreground text-sm'>
                {t(product.description)}
              </p>
              <p className='text-muted-foreground text-sm'>
                {t(
                  'Price and availability are shown on Catfk. Payment does not automatically credit your wallet.'
                )}
              </p>
              <Button
                role='link'
                className='min-h-11 w-full'
                render={
                  <a
                    href={product.purchase_path ?? url}
                    target='_blank'
                    rel='noopener noreferrer'
                  />
                }
              >
                {t('Buy API credits')}
              </Button>
            </article>
          )
        })}
      </div>
      <section className='space-y-4 rounded-xl border p-5 sm:p-6'>
        <h2 className='text-xl font-semibold'>{t('How to recharge')}</h2>
        <ol className='list-decimal space-y-2 pl-5'>
          <li>{t('Open Catfk and complete your purchase.')}</li>
          <li>{t('Get your recharge code from Catfk.')}</li>
          <li>
            {t(
              'Return to your wallet and select Legacy redemption code under Other redemption methods.'
            )}
          </li>
        </ol>
        <Button
          role='link'
          variant='outline'
          className='min-h-11'
          render={<Link to='/wallet' />}
        >
          {t('Go to wallet to redeem')}
        </Button>
      </section>
    </div>
  )
}

export function Shop() {
  const catalog = useQuery({
    queryKey: ['credit-catalog'],
    queryFn: getCatalog,
  })
  return (
    <PublicLayout>
      {catalog.isError ? (
        <ErrorState onRetry={() => catalog.refetch()} />
      ) : null}
      <CreditCatalog products={catalog.data?.products ?? []} />
    </PublicLayout>
  )
}
