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
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'

import { useFinanceSummary } from './access'
import { FinanceTable, Money, Panel } from './components'
import { metricLabels } from './labels'
import type { Range } from './types'

const metrics = [
  'cash_receipts',
  'cash_refunds',
  'consumption_revenue',
  'direct_cost',
  'gross_profit',
  'operating_expenses',
  'operating_profit',
]

export function OverviewFinance(props: {
  range: Range
  epoch: number
  ready: boolean
}) {
  const { t } = useTranslation()
  const query = useFinanceSummary(props.range, props.epoch, props.ready, false)
  if (!props.ready) {
    return (
      <Alert>
        <AlertDescription>{t('ops.financeUnavailable')}</AlertDescription>
      </Alert>
    )
  }
  if (query.isError) {
    return (
      <ErrorState
        title={t('Finance read failed')}
        onRetry={() => void query.refetch()}
      />
    )
  }
  if (!query.data) {
    return <LoadingState message={t('Reading finance snapshot')} />
  }
  const summary = query.data
  return (
    <Panel title={t('ops.financeTitle')}>
      <p className='text-muted-foreground text-sm'>
        {t('ops.financeCoverage')}
      </p>
      <Button
        type='button'
        variant='outline'
        className='min-h-11'
        disabled={query.isFetching}
        onClick={() => void query.refetch()}
      >
        {t('ops.refreshFinance')}
      </Button>
      <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
        {metrics.map((key) => {
          const metric = summary.metrics[key]
          return (
            <div key={key} className='bg-background rounded-lg border p-4'>
              <p className='text-muted-foreground text-sm'>
                {t(metricLabels[key])}
              </p>
              <div className='mt-2 text-lg font-semibold'>
                <Money value={metric?.total} />
              </div>
              <p className='mt-2 text-xs'>
                {t('ops.recordedSubtotal')}:{' '}
                <Money value={metric?.known_subtotal} />
              </p>
              {metric?.total == null && (
                <p className='text-muted-foreground mt-1 text-xs'>
                  {t('ops.incomplete')}
                </p>
              )}
            </div>
          )
        })}
      </div>
      <FinanceTable
        label={t('ops.financeTrend')}
        data={summary.trend}
        columns={[
          {
            id: 'date',
            header: t('Date'),
            cell: (row) =>
              new Date((row.from + 28800) * 1000).toISOString().slice(0, 10),
          },
          ...metrics.map((key) => ({
            id: key,
            header: t(metricLabels[key]),
            cell: (row: (typeof summary.trend)[number]) => (
              <div>
                <Money value={row.metrics[key]?.total} />
                <p className='text-muted-foreground text-xs'>
                  {t('ops.recordedSubtotal')}:{' '}
                  <Money value={row.metrics[key]?.known_subtotal} />
                </p>
              </div>
            ),
          })),
        ]}
      />
    </Panel>
  )
}
