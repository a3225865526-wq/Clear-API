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
import { CheckCircle2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { formatQuota } from '@/lib/format'

import type { StoreRedeemResult } from '../../types'

interface StoreRedeemSuccessDialogProps {
  result: StoreRedeemResult | null
  onOpenChange: (open: boolean) => void
}

/**
 * Success summary for a store code redemption. Values are rendered exactly
 * as the backend returned them; no redemption/batch identifiers are shown.
 */
export function StoreRedeemSuccessDialog(props: StoreRedeemSuccessDialogProps) {
  const { t } = useTranslation()
  const result = props.result
  const upgraded =
    !!result &&
    !!result.new_vip_name &&
    result.new_vip_name !== result.vip_name

  return (
    <Dialog
      open={!!result}
      onOpenChange={props.onOpenChange}
      title={t('Redemption successful')}
      contentClassName='sm:max-w-md'
      bodyClassName='space-y-4'
      footer={
        <Button onClick={() => props.onOpenChange(false)}>{t('Done')}</Button>
      }
    >
      {result && (
        <>
          <div className='flex items-center gap-2'>
            <CheckCircle2 className='h-5 w-5 text-green-600' />
            <span className='text-sm font-medium'>
              {t('Redemption successful')}
            </span>
            {upgraded && (
              <Badge variant='secondary'>
                {t('Upgraded to {{name}}', { name: result.new_vip_name })}
              </Badge>
            )}
          </div>

          <div className='grid grid-cols-2 gap-3'>
            <div className='space-y-1'>
              <Label className='text-muted-foreground text-xs'>
                {t('Base credited')}
              </Label>
              <div className='text-sm font-semibold tabular-nums'>
                {formatQuota(result.base_quota)}
              </div>
            </div>
            <div className='space-y-1'>
              <Label className='text-muted-foreground text-xs'>
                {t('Member bonus')}
              </Label>
              <div className='text-sm font-semibold tabular-nums'>
                {formatQuota(result.bonus_quota)}
              </div>
            </div>
            <div className='space-y-1'>
              <Label className='text-muted-foreground text-xs'>
                {t('Total credited')}
              </Label>
              <div className='text-foreground text-sm font-bold tabular-nums'>
                {formatQuota(result.total_quota)}
              </div>
            </div>
            <div className='space-y-1'>
              <Label className='text-muted-foreground text-xs'>
                {t('Current Balance')}
              </Label>
              <div className='text-foreground text-sm font-bold tabular-nums'>
                {formatQuota(result.balance)}
              </div>
            </div>
          </div>
        </>
      )}
    </Dialog>
  )
}
