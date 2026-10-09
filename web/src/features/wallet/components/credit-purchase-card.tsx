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

import { Button } from '@/components/ui/button'

export function CreditPurchaseCard(props: { onRedeem: () => void }) {
  const { t } = useTranslation()
  return (
    <section className='bg-card space-y-3 rounded-lg border p-3 sm:p-5'>
      <h2 className='font-semibold'>{t('Buy API credits')}</h2>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Buy a recharge code at Catfk, then return to your wallet to redeem it.'
        )}
      </p>
      <div className='flex flex-col gap-2 sm:flex-row'>
        <Button role='link' className='min-h-11' render={<Link to='/shop' />}>
          {t('Go to shop')}
        </Button>
        <Button className='min-h-11' variant='outline' onClick={props.onRedeem}>
          {t('Redeem an existing code')}
        </Button>
      </div>
    </section>
  )
}
