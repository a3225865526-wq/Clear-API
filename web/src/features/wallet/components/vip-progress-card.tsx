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
import { Crown } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Progress } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'

import { formatBasisPoints, formatCnyCents } from '../lib/money'
import type { VipInfo } from '../types'

interface VipProgressCardProps {
  vip: VipInfo | null | undefined
  loading?: boolean
}

/**
 * VIP member status card. All level rules (names, thresholds, bonus rates)
 * come from the backend via topup info — the frontend only renders them and
 * never re-derives level rules.
 */
export function VipProgressCard(props: VipProgressCardProps) {
  const { t } = useTranslation()

  if (props.loading) {
    return (
      <div className='rounded-lg border p-3 sm:p-5'>
        <Skeleton className='h-4 w-32' />
        <Skeleton className='mt-3 h-3 w-48' />
        <Skeleton className='mt-2 h-2 w-full' />
      </div>
    )
  }

  const vip = props.vip
  if (!vip) {
    return null
  }

  const levelName = vip.name || t('Regular user')
  const nextLevel = vip.next_level ?? null
  const isMaxLevel = !nextLevel

  let progressPercent = 100
  if (!isMaxLevel) {
    const { current_threshold_cents: currentThreshold } = vip
    const nextThreshold = nextLevel.threshold_cents
    if (typeof currentThreshold === 'number' && nextThreshold > currentThreshold) {
      const segment = vip.total_recharge_cents - currentThreshold
      const span = nextThreshold - currentThreshold
      progressPercent = Math.min(100, Math.max(0, (segment / span) * 100))
    } else if (nextThreshold > 0) {
      progressPercent = Math.min(
        100,
        Math.max(0, (vip.total_recharge_cents / nextThreshold) * 100)
      )
    }
  }

  return (
    <div className='rounded-lg border p-3 sm:p-5'>
      <div className='flex flex-wrap items-center gap-2'>
        <Crown className='text-primary h-4 w-4' />
        <h3 className='text-foreground text-sm font-semibold'>
          {t('Member Status')}
        </h3>
        <Badge variant='secondary'>{levelName}</Badge>
        <Badge variant='outline'>
          {t('Recharge bonus')} {formatBasisPoints(vip.bonus_basis_points)}
        </Badge>
        <span className='text-muted-foreground ml-auto text-xs'>
          {t('Total recharged')}{' '}
          <span className='text-foreground font-medium tabular-nums'>
            {formatCnyCents(vip.total_recharge_cents)}
          </span>
        </span>
      </div>

      <div className='mt-3 flex items-center gap-3'>
        <Progress value={progressPercent} className='h-2 flex-1' />
        {isMaxLevel ? (
          <span className='text-muted-foreground text-xs whitespace-nowrap'>
            {t('Highest level reached')}
          </span>
        ) : (
          <span className='text-muted-foreground text-xs whitespace-nowrap'>
            {t('To {{name}}: left {{amount}}', {
              name: nextLevel.name,
              amount: formatCnyCents(nextLevel.remaining_cents),
            })}
          </span>
        )}
      </div>
    </div>
  )
}
