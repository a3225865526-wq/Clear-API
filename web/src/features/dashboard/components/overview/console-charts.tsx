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
import {
  Cell,
  Line,
  LineChart,
  Pie,
  PieChart,
  XAxis,
  YAxis,
  CartesianGrid,
} from 'recharts'

import { EmptyState } from '@/components/empty-state'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
} from '@/components/ui/chart'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { toIntlLocale } from '@/i18n/languages'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatNumber } from '@/lib/format'

import type { ConsoleDashboardData } from './console-api'

const colors = [
  'var(--chart-1)',
  'var(--chart-2)',
  'var(--chart-3)',
  'var(--chart-4)',
  'var(--chart-5)',
]

export function ConsoleCharts(props: { data: ConsoleDashboardData }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [hidden, setHidden] = useState<string[]>([])
  const series = [
    { key: 'input', label: t('console.input'), color: colors[0] },
    { key: 'output', label: t('console.output'), color: colors[1] },
  ]
  const config = Object.fromEntries(
    series.map((item) => [item.key, { label: item.label, color: item.color }])
  )
  return (
    <div className='grid min-w-0 gap-6 xl:grid-cols-2'>
      <Card className='min-w-0 rounded-2xl'>
        <CardHeader>
          <CardTitle>{t('console.modelDistribution')}</CardTitle>
          <p className='text-muted-foreground text-sm'>
            {t('console.byRequests')}
          </p>
        </CardHeader>
        <CardContent>
          {props.data.models.length === 0 ? (
            <EmptyState title={t('console.noUsage')} />
          ) : (
            <>
              <ChartContainer
                config={config}
                className='mx-auto h-56 w-full max-w-sm'
              >
                <PieChart>
                  <Pie
                    data={props.data.models}
                    dataKey='requests'
                    nameKey='model'
                    innerRadius={60}
                    outerRadius={90}
                    isAnimationActive={false}
                  >
                    {props.data.models.map((item, index) => (
                      <Cell
                        key={item.model}
                        fill={colors[index % colors.length]}
                      />
                    ))}
                  </Pie>
                  <ChartTooltip content={<ChartTooltipContent />} />
                </PieChart>
              </ChartContainer>
              <div className='max-w-full overflow-x-auto'>
                <Table>
                  <TableHeader>
                    <TableRow>
                      {[
                        'console.model',
                        'console.requests',
                        'console.tokens',
                        'console.actualCost',
                      ].map((key) => (
                        <TableHead key={key}>{t(key)}</TableHead>
                      ))}
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {props.data.models.map((item, index) => (
                      <TableRow key={item.model}>
                        <TableCell className='max-w-64 break-all whitespace-normal'>
                          <span
                            className='mr-2 inline-block size-2 rounded-full'
                            style={{
                              backgroundColor: colors[index % colors.length],
                            }}
                          />
                          {item.model}
                        </TableCell>
                        <TableCell>
                          {formatNumber(item.requests, locale)}
                        </TableCell>
                        <TableCell>
                          {formatNumber(item.input + item.output, locale)}
                        </TableCell>
                        <TableCell>
                          {formatQuotaWithCurrency(item.quota)}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            </>
          )}
        </CardContent>
      </Card>
      <Card className='min-w-0 rounded-2xl'>
        <CardHeader>
          <CardTitle>{t('console.tokenTrend')}</CardTitle>
          <p className='text-muted-foreground text-sm'>
            {t('console.cacheUnavailable')}
          </p>
        </CardHeader>
        <CardContent>
          {props.data.trend.length === 0 ? (
            <EmptyState title={t('console.noUsage')} />
          ) : (
            <>
              <div className='mb-4 flex flex-wrap gap-2'>
                {series.map((item) => (
                  <Button
                    key={item.key}
                    variant={
                      hidden.includes(item.key) ? 'outline' : 'secondary'
                    }
                    size='sm'
                    aria-pressed={!hidden.includes(item.key)}
                    onClick={() =>
                      setHidden((current) =>
                        current.includes(item.key)
                          ? current.filter((key) => key !== item.key)
                          : [...current, item.key]
                      )
                    }
                  >
                    {item.label}
                  </Button>
                ))}
              </div>
              <ChartContainer config={config} className='h-72 w-full'>
                <LineChart data={props.data.trend}>
                  <CartesianGrid vertical={false} />
                  <XAxis dataKey='date' tickMargin={8} />
                  <YAxis
                    width={64}
                    label={{
                      value: 'Token',
                      angle: -90,
                      position: 'insideLeft',
                    }}
                  />
                  <ChartTooltip content={<ChartTooltipContent />} />
                  {series.map((item) => (
                    <Line
                      key={item.key}
                      dataKey={item.key}
                      name={item.label}
                      stroke={item.color}
                      hide={hidden.includes(item.key)}
                      dot
                      isAnimationActive={false}
                      connectNulls={false}
                    />
                  ))}
                </LineChart>
              </ChartContainer>
              <p className='text-muted-foreground mt-3 text-xs'>
                {t('console.sparseData')}
              </p>
            </>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
