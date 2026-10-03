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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { toIntlLocale } from '@/i18n/languages'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatNumber } from '@/lib/format'

import { financeKey, financeRequest } from './access'
import { Panel } from './components'
import { beijingInput, financeTime, validRange } from './format'
import { OverviewDetails } from './overview-details'
import { OverviewFinance } from './overview-finance'
import type { OverviewData, OverviewRange } from './overview-types'

export function OwnerOverview(props: { epoch: number; financeReady: boolean }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const client = useQueryClient()
  const [selection, setSelection] = useState('today')
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [invalid, setInvalid] = useState(false)
  const [range, setRange] = useState<OverviewRange>({ period: 'today' })
  const query = useQuery({
    queryKey: [...financeKey, props.epoch, 'overview', range],
    queryFn: ({ signal }) =>
      financeRequest<OverviewData>('/overview', { params: range, signal }),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    meta: { errorToast: false },
  })
  const data = query.data
  const number = (value: number | null | undefined) =>
    value == null ? t('Unknown') : formatNumber(value, locale)
  const cards = data
    ? [
        { label: t('ops.totalUsers'), value: number(data.total_users) },
        { label: t('ops.newUsers'), value: number(data.new_users) },
        { label: t('ops.activeUsers'), value: number(data.active_users) },
        {
          label: t('ops.paidCreditUsers'),
          value: number(data.paid_credit_users),
        },
        { label: t('ops.tokens'), value: number(data.input + data.output) },
        { label: t('console.input'), value: number(data.input) },
        { label: t('console.output'), value: number(data.output) },
        { label: t('ops.consumeEntries'), value: number(data.requests) },
        {
          label: t('ops.consumption'),
          value: formatQuotaWithCurrency(data.quota, { locale }),
        },
        {
          label: t('ops.quotaRefund'),
          value: formatQuotaWithCurrency(data.refund_quota, { locale }),
        },
        {
          label: t('ops.netConsumption'),
          value: formatQuotaWithCurrency(data.quota - data.refund_quota, {
            locale,
          }),
        },
        { label: t('ops.errors'), value: number(data.errors) },
        { label: t('ops.averageSeconds'), value: number(data.average_seconds) },
        {
          label: t('ops.tokensPerUser'),
          value: data.active_users
            ? number((data.input + data.output) / data.active_users)
            : t('Unknown'),
        },
        {
          label: t('ops.consumptionPerUser'),
          value: data.active_users
            ? formatQuotaWithCurrency(data.quota / data.active_users, {
                locale,
              })
            : t('Unknown'),
        },
        { label: t('ops.pendingReview'), value: number(data.pending_review) },
      ]
    : []
  return (
    <Panel title={t('ops.title')}>
      <form
        className='flex flex-wrap items-end gap-3'
        onSubmit={(event) => {
          event.preventDefault()
          const start = beijingInput(from),
            end = beijingInput(to)
          if (selection === 'custom' && !validRange(start, end)) {
            setInvalid(true)
            return
          }
          setInvalid(false)
          setRange(
            selection === 'custom'
              ? { from: start, to: end }
              : { period: selection as 'today' | '7d' | '30d' }
          )
        }}
      >
        <div className='space-y-1'>
          <Label htmlFor='ops-period'>{t('ops.period')}</Label>
          <Select
            value={selection}
            onValueChange={(value) => {
              if (value) setSelection(value)
            }}
          >
            <SelectTrigger id='ops-period' className='min-h-11 min-w-36'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='today'>{t('Today')}</SelectItem>
              <SelectItem value='7d'>{t('ops.last7')}</SelectItem>
              <SelectItem value='30d'>{t('ops.last30')}</SelectItem>
              <SelectItem value='custom'>{t('Custom period')}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        {selection === 'custom' && (
          <>
            <div className='min-w-0 space-y-1'>
              <Label htmlFor='ops-from'>
                {t('Start, Beijing time (inclusive)')}
              </Label>
              <Input
                id='ops-from'
                type='datetime-local'
                required
                value={from}
                onChange={(event) => setFrom(event.target.value)}
              />
            </div>
            <div className='min-w-0 space-y-1'>
              <Label htmlFor='ops-to'>
                {t('End, Beijing time (exclusive)')}
              </Label>
              <Input
                id='ops-to'
                type='datetime-local'
                required
                value={to}
                onChange={(event) => setTo(event.target.value)}
              />
            </div>
          </>
        )}
        <Button type='submit' className='min-h-11'>
          {t('ops.apply')}
        </Button>
        <Button
          type='button'
          variant='outline'
          className='min-h-11'
          disabled={query.isFetching}
          onClick={() =>
            void client.invalidateQueries({
              queryKey: [...financeKey, props.epoch],
            })
          }
        >
          {t('ops.refresh')}
        </Button>
        {invalid && (
          <p role='alert' className='text-destructive basis-full text-sm'>
            {t('Enter a valid Beijing period of up to 366 days.')}
          </p>
        )}
      </form>
      <p className='text-muted-foreground text-sm'>{t('ops.scope')}</p>
      {query.isFetching && (
        <div role='status'>
          <LoadingState message={t('ops.loading')} />
        </div>
      )}
      {query.isError && (
        <ErrorState
          title={t('ops.failed')}
          onRetry={() => void query.refetch()}
        />
      )}
      {!query.isError && data && (
        <>
          <p className='text-muted-foreground text-xs'>
            {financeTime(data.from, i18n.language)} –{' '}
            {financeTime(data.to, i18n.language)} · {t('ops.readAt')}:{' '}
            {financeTime(data.read_at, i18n.language)}
          </p>
          <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
            {cards.map((card) => (
              <div
                key={card.label}
                className='bg-background rounded-lg border p-4'
              >
                <p className='text-muted-foreground text-sm'>{card.label}</p>
                <p className='mt-2 text-xl font-semibold break-all tabular-nums'>
                  {card.value}
                </p>
              </div>
            ))}
          </div>
          <Alert>
            <AlertDescription>
              {t('ops.coverage')}
              {data.unknown_registration > 0 && (
                <p>
                  {t('ops.unknownRegistration', {
                    count: data.unknown_registration,
                  })}
                </p>
              )}
            </AlertDescription>
          </Alert>
          <OverviewFinance
            epoch={props.epoch}
            ready={props.financeReady}
            range={
              'period' in range && range.period === 'today'
                ? { period: 'today', trend: 'day' }
                : { from: data.from, to: data.to, trend: 'day' }
            }
          />
          <OverviewDetails data={data} />
        </>
      )}
    </Panel>
  )
}
