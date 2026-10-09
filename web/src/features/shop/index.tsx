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

import { PublicLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'

import {
  creditProducts,
  trustedPurchaseUrl,
  type CreditProduct,
} from './catalog'

export function CreditCatalog(props: { products?: readonly CreditProduct[] }) {
  const { t } = useTranslation()
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
      <div className='grid grid-cols-1 gap-4 sm:grid-cols-2'>
        {(props.products ?? creditProducts).map((product) => {
          const url = trustedPurchaseUrl(product.purchaseUrl)
          return (
            <article
              key={product.id}
              className='bg-card text-card-foreground min-w-0 space-y-5 rounded-xl border p-5 sm:p-6'
            >
              <h2 className='text-xl font-semibold'>
                {t('ClearAPI API credit recharge code')}
              </h2>
              <p>
                {t(
                  '500,000 Quota = $1 nominal platform credit. This credit cannot be withdrawn as dollars.'
                )}
              </p>
              <p className='text-muted-foreground text-sm'>
                {t(
                  'Use credits for approved API models in your account group.'
                )}
              </p>
              <p className='text-muted-foreground text-sm'>
                {t(
                  'Price and availability are shown on Catfk. Payment does not automatically credit your wallet.'
                )}
              </p>
              {url ? (
                <Button
                  role='link'
                  className='min-h-11 w-full'
                  render={
                    <a href={url} target='_blank' rel='noopener noreferrer' />
                  }
                >
                  {t('View price and buy')}
                </Button>
              ) : (
                <>
                  <p role='alert'>{t('Invalid store product link')}</p>
                  <Button disabled className='min-h-11 w-full'>
                    {t('View price and buy')}
                  </Button>
                </>
              )}
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
  return (
    <PublicLayout>
      <CreditCatalog />
    </PublicLayout>
  )
}
