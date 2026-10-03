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
import { ShoppingBag } from 'lucide-react'
import { toast } from 'sonner'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { openStoreUrl } from '@/lib/store-url'

import { formatCnyCents } from '../lib/money'
import type { StoreProductDto } from '../types'

interface StoreProductsCardProps {
  enabled?: boolean
  products?: StoreProductDto[]
}

/**
 * Quick-buy grid for the third-party store. Products are jump links only —
 * the real credited value is decided by the redeemed code on the backend.
 * Hidden entirely while the store recharge feature flag is off.
 */
export function StoreProductsCard(props: StoreProductsCardProps) {
  const { t } = useTranslation()

  if (!props.enabled) {
    return null
  }

  const products = props.products ?? []

  return (
    <div className='rounded-lg border p-3 sm:p-5'>
      <div className='flex items-center gap-2'>
        <ShoppingBag className='text-primary h-4 w-4' />
        <h3 className='text-foreground text-sm font-semibold'>
          {t('Quick Purchase')}
        </h3>
        <span className='text-muted-foreground hidden text-xs sm:inline'>
          {t('Pay at the third-party store, then redeem the code below')}
        </span>
      </div>

      {products.length === 0 ? (
        <p className='text-muted-foreground mt-3 text-sm'>
          {t('No store products configured yet')}
        </p>
      ) : (
        <div className='mt-3 grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6'>
          {products.map((product) => (
            <Button
              key={product.id}
              variant='outline'
              className='h-auto flex-col items-start gap-0.5 px-3 py-2.5'
              onClick={() => {
                if (!openStoreUrl(product.store_url)) {
                  toast.error(t('Invalid store product link'))
                }
              }}
            >
              <span className='text-base font-semibold tabular-nums'>
                {formatCnyCents(product.face_value_cents)}
              </span>
              <span className='text-muted-foreground w-full truncate text-left text-xs'>
                {product.name}
              </span>
            </Button>
          ))}
        </div>
      )}
    </div>
  )
}
