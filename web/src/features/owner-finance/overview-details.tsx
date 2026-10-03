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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { CartesianGrid, Line, LineChart, XAxis, YAxis } from 'recharts'

import { EmptyState } from '@/components/empty-state'
import { Button } from '@/components/ui/button'
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
} from '@/components/ui/chart'
import { toIntlLocale } from '@/i18n/languages'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatNumber } from '@/lib/format'

import { FinanceTable, Panel } from './components'
import type { OverviewData, OverviewRanking } from './overview-types'

export function OverviewDetails(props: { data: OverviewData }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const number = (value: number) => formatNumber(value, locale)
  const money = (value: number) => formatQuotaWithCurrency(value, { locale })
  const [series, setSeries] = useState('tokens')
  const chartData = props.data.trend.map((day) => ({
    ...day,
    tokens: day.input + day.output,
  }))
  const labels: Record<string, string> = {
    tokens: t('ops.tokens'),
    new_users: t('ops.newUsers'),
    active_users: t('ops.activeUsers'),
    requests: t('ops.consumeEntries'),
  }
  const rankingColumns = [
    {
      id: 'name',
      header: t('Name'),
      cell: (row: OverviewRanking) => (
        <span className='break-all'>{row.name || t('Unknown')}</span>
      ),
    },
    {
      id: 'requests',
      header: t('ops.consumeEntries'),
      cell: (row: OverviewRanking) => number(row.requests),
    },
    {
      id: 'tokens',
      header: t('ops.tokens'),
      cell: (row: OverviewRanking) => number(row.input + row.output),
    },
    {
      id: 'quota',
      header: t('ops.consumption'),
      cell: (row: OverviewRanking) => money(row.quota),
    },
  ]
  return (
    <div className='space-y-4'>
      <Panel title={t('ops.dailyTrend')}>
        <div className='flex flex-wrap gap-2'>
          {Object.entries(labels).map(([key, label]) => (
            <Button
              key={key}
              type='button'
              variant={series === key ? 'default' : 'outline'}
              aria-pressed={series === key}
              className='min-h-11'
              onClick={() => setSeries(key)}
            >
              {label}
            </Button>
          ))}
        </div>
        <ChartContainer
          className='h-64 w-full'
          config={{
            [series]: { label: labels[series], color: 'var(--chart-1)' },
          }}
          aria-label={labels[series]}
        >
          <LineChart data={chartData} accessibilityLayer>
            <CartesianGrid vertical={false} />
            <XAxis dataKey='date' />
            <YAxis tickFormatter={number} width={80} />
            <ChartTooltip content={<ChartTooltipContent />} />
            <Line
              type='monotone'
              dataKey={series}
              stroke='var(--chart-1)'
              dot={false}
              isAnimationActive={false}
            />
          </LineChart>
        </ChartContainer>
        <FinanceTable
          label={t('ops.dailyTrend')}
          data={props.data.trend}
          columns={[
            { id: 'date', header: t('Date'), cell: (row) => row.date },
            {
              id: 'new',
              header: t('ops.newUsers'),
              cell: (row) => number(row.new_users),
            },
            {
              id: 'active',
              header: t('ops.activeUsers'),
              cell: (row) => number(row.active_users),
            },
            {
              id: 'requests',
              header: t('ops.consumeEntries'),
              cell: (row) => number(row.requests),
            },
            {
              id: 'input',
              header: t('console.input'),
              cell: (row) => number(row.input),
            },
            {
              id: 'output',
              header: t('console.output'),
              cell: (row) => number(row.output),
            },
            {
              id: 'gross',
              header: t('ops.consumption'),
              cell: (row) => money(row.quota),
            },
            {
              id: 'refund',
              header: t('ops.quotaRefund'),
              cell: (row) => money(row.refund_quota),
            },
            {
              id: 'net',
              header: t('ops.netConsumption'),
              cell: (row) => money(row.quota - row.refund_quota),
            },
            {
              id: 'errors',
              header: t('ops.errors'),
              cell: (row) => number(row.errors),
            },
          ]}
        />
      </Panel>
      <div className='grid min-w-0 gap-4 xl:grid-cols-2'>
        <Panel title={t('ops.models')}>
          {props.data.models.length ? (
            <FinanceTable
              label={t('ops.models')}
              data={props.data.models}
              columns={rankingColumns}
            />
          ) : (
            <EmptyState title={t('console.noUsage')} />
          )}
        </Panel>
        <Panel title={t('ops.channels')}>
          <p className='text-muted-foreground text-xs'>{t('ops.channelIds')}</p>
          {props.data.channels.length ? (
            <FinanceTable
              label={t('ops.channels')}
              data={props.data.channels}
              columns={rankingColumns}
            />
          ) : (
            <EmptyState title={t('console.noUsage')} />
          )}
        </Panel>
      </div>
    </div>
  )
}
