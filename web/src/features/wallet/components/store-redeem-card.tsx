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
import { Ticket } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

import { useStoreRedeem } from '../hooks/use-store-redeem'
import { StoreRedeemSuccessDialog } from './dialogs/store-redeem-success-dialog'

interface StoreRedeemCardProps {
  /** Refresh user balance + topup info (VIP, products) after a success. */
  onSuccess?: () => void | Promise<void>
}

/**
 * Store card code redemption. Posts ONLY the trimmed key to the dedicated
 * store endpoint — amount, quota, VIP and bonus are always backend-decided.
 * Business boundary: offline-order codes have their own separate entry and
 * no automatic fallback happens here.
 */
export function StoreRedeemCard(props: StoreRedeemCardProps) {
  const { t } = useTranslation()
  const [code, setCode] = useState('')
  const { redeeming, result, redeem, clearResult } = useStoreRedeem()

  const handleRedeem = async () => {
    if (!code.trim()) return
    const success = await redeem(code, { onSuccess: props.onSuccess })
    if (success) {
      setCode('')
    }
  }

  return (
    <div className='rounded-lg border p-3 sm:p-5'>
      <div className='flex items-center gap-2'>
        <Ticket className='text-primary h-4 w-4' />
        <h3 className='text-foreground text-sm font-semibold'>
          {t('Store Redeem Code')}
        </h3>
      </div>
      <p className='text-muted-foreground mt-1 text-xs'>
        {t('Enter the code you purchased from the recharge store')}
      </p>
      <div className='mt-3 flex flex-col gap-2 sm:flex-row'>
        <Input
          value={code}
          onChange={(event) => setCode(event.target.value)}
          placeholder={t('Redemption code')}
          className='flex-1'
          onKeyDown={(event) => {
            if (event.key === 'Enter' && !redeeming) {
              void handleRedeem()
            }
          }}
        />
        <Button onClick={() => void handleRedeem()} disabled={redeeming}>
          {redeeming ? t('Redeeming...') : t('Redeem Now')}
        </Button>
      </div>

      <StoreRedeemSuccessDialog
        result={result}
        onOpenChange={(open) => {
          if (!open) clearResult()
        }}
      />
    </div>
  )
}
