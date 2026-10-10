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
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber, formatTimestamp } from '@/lib/format'

import { getReferralSelf } from './api'
import { formatReferralCredit } from './lib/format-credit'

export function ReferralPanel() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [page, setPage] = useState(0)
  const query = useQuery({
    queryKey: ['referral-self', page],
    queryFn: () => getReferralSelf(page),
  })
  if (query.isPending) return <Skeleton className='h-48 w-full' />
  if (query.isError) return <ErrorState onRetry={() => query.refetch()} />
  const data = query.data
  const credit = (q: number) => formatReferralCredit(q, locale)
  return (
    <section
      id='referrals'
      className='bg-card space-y-4 rounded-xl border p-4 sm:p-6'
    >
      <h2 className='text-xl font-semibold'>{t('Invite friends')}</h2>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Referral rewards are platform API usage credit, not cash. Only qualified paid code redemptions earn rewards.'
        )}
      </p>
      {!data.rule.invites_enabled ? (
        <p>{t('New invitations are currently disabled.')}</p>
      ) : null}
      <div className='flex min-w-0 items-center gap-2'>
        <Input
          aria-label={t('Your referral link')}
          value={data.link}
          readOnly
        />
        <CopyButton value={data.link} tooltip={t('Copy referral link')} />
      </div>
      <p>
        {t('My invitation code')}: {data.code}
      </p>
      <div className='grid grid-cols-1 gap-3 sm:grid-cols-3'>
        <p>
          {t('Successful invitations')}: {formatNumber(data.count, locale)}
        </p>
        <p>
          {t('Total Earned')}: {credit(data.paid_quota)}{' '}
          {t('platform API usage credit')}
        </p>
        <p>
          {t('Current reward rule')}:{' '}
          {data.rule.mode === 'percentage'
            ? `${formatNumber(data.rule.basis_points / 100, locale)}%`
            : credit(data.rule.fixed_quota)}{' '}
          · {data.rule.rewards_enabled ? t('Enabled') : t('Disabled')}
        </p>
      </div>
      <h3 className='font-semibold'>{t('Referral reward records')}</h3>
      {data.rewards.length === 0 ? (
        <EmptyState title={t('No referral rewards yet')} />
      ) : (
        <ul className='divide-y'>
          {data.rewards.map((r) => (
            <li
              key={r.id}
              className='flex flex-wrap justify-between gap-2 py-3'
            >
              <span>
                #{r.id} · {credit(r.reward_quota)} ·{' '}
                {formatTimestamp(r.created_time)}
              </span>
              <span>
                {t(r.status)} {r.paid_time ? formatTimestamp(r.paid_time) : ''}
              </span>
            </li>
          ))}
        </ul>
      )}
      <div className='flex gap-2'>
        <Button
          variant='outline'
          disabled={page === 0}
          onClick={() => setPage(page - 1)}
        >
          {t('Previous')}
        </Button>
        <Button
          variant='outline'
          disabled={(page + 1) * 20 >= data.total}
          onClick={() => setPage(page + 1)}
        >
          {t('Next')}
        </Button>
      </div>
    </section>
  )
}
