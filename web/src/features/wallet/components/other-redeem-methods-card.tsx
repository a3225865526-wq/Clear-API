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
import { ChevronDown, Gift } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { IconBadge } from '@/components/ui/icon-badge'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'

import { useRedemption } from '../hooks'

interface OtherRedeemMethodsCardProps {
  /** Refresh user balance after a successful redemption. */
  openLegacy?: number
  onSuccess?: () => void | Promise<void>
}

/**
 * 折叠的"其他兑换方式"：离线订单兑换码（管理员人工补单）与 Legacy 兑换码。
 * 商城充值主路径走 StoreRedeemCard（POST /api/clearapi/store/redeem），
 * 此处两种码各自走独立接口，与商城码保持业务隔离、绝不自动回退。
 */
export function OtherRedeemMethodsCard(props: OtherRedeemMethodsCardProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [code, setCode] = useState('')
  const [kind, setKind] = useState<'offline' | 'legacy'>('offline')
  const { redeeming, redeemCode } = useRedemption()

  useEffect(() => {
    if (props.openLegacy) {
      setOpen(true)
      setKind('legacy')
    }
  }, [props.openLegacy])

  useEffect(() => {
    if (props.openLegacy && open && kind === 'legacy') {
      document
        .querySelector<HTMLInputElement>('#other-redemption-code')
        ?.focus()
    }
  }, [open, kind, props.openLegacy])

  const handleRedeem = async () => {
    if (!code.trim()) return
    const success = await redeemCode(code, kind)
    if (success) {
      setCode('')
      await props.onSuccess?.()
    }
  }

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <div className='rounded-lg border p-3 sm:p-5'>
        <CollapsibleTrigger
          render={
            <button
              type='button'
              className='flex w-full items-center gap-2 text-left transition-colors'
              aria-expanded={open}
            />
          }
        >
          <IconBadge tone='warning' size='xs'>
            <Gift />
          </IconBadge>
          <span className='text-foreground text-sm font-semibold'>
            {t('Other redemption methods')}
          </span>
          <span className='text-muted-foreground hidden text-xs sm:inline'>
            {t('Offline order codes and legacy redemption codes')}
          </span>
          <ChevronDown
            className={`text-muted-foreground ml-auto h-4 w-4 transition-transform ${open ? 'rotate-180' : ''}`}
          />
        </CollapsibleTrigger>
        <CollapsibleContent>
          <div className='mt-3 space-y-2.5 border-t pt-3'>
            <NativeSelect
              aria-label={t('Redemption code type')}
              value={kind}
              disabled={redeeming}
              onChange={(event) =>
                setKind(event.target.value === 'legacy' ? 'legacy' : 'offline')
              }
            >
              <NativeSelectOption value='offline'>
                {t('clearapi.offline.title')}
              </NativeSelectOption>
              <NativeSelectOption value='legacy'>
                {t('Legacy redemption code')}
              </NativeSelectOption>
            </NativeSelect>
            <Label
              htmlFor='other-redemption-code'
              className='text-muted-foreground text-xs font-medium tracking-wider uppercase'
            >
              {t('Have a Code?')}
            </Label>
            <div className='grid grid-cols-[minmax(0,1fr)_auto] gap-2'>
              <Input
                id='other-redemption-code'
                value={code}
                onChange={(event) => setCode(event.target.value)}
                placeholder={t('Enter your redemption code')}
                className='h-9 min-w-0'
                onKeyDown={(event) => {
                  if (event.key === 'Enter' && !redeeming) {
                    void handleRedeem()
                  }
                }}
              />
              <Button
                onClick={() => void handleRedeem()}
                disabled={redeeming}
                variant='outline'
                className='h-9 px-4'
              >
                {redeeming ? t('Redeeming...') : t('Redeem Now')}
              </Button>
            </div>
          </div>
        </CollapsibleContent>
      </div>
    </Collapsible>
  )
}
