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
import { getCoreRowModel, useReactTable } from '@tanstack/react-table'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTablePagination, StaticDataTable } from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import { useAuthStore } from '@/stores/auth-store'

import { getMonitorHistory, type Monitor } from './api'
import { probeReasonKeys, probeStatusKeys, statusColors } from './constants'

export function MonitorHistory(props: { monitor: Monitor }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const userId = useAuthStore((state) => state.auth.user?.id)
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 20 })
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const invalidRange = Boolean(from && to && from > to)
  const query = useQuery({
    queryKey: [
      'console-monitor-history',
      userId,
      props.monitor.id,
      pagination,
      from,
      to,
    ],
    queryFn: ({ signal }) =>
      getMonitorHistory(
        props.monitor.id,
        pagination.pageIndex + 1,
        pagination.pageSize,
        from,
        to,
        signal
      ),
    enabled: !invalidRange,
    refetchInterval: 60000,
  })
  const table = useReactTable({
    data: query.data?.items ?? [],
    columns: [],
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    rowCount: query.data?.total ?? 0,
    state: { pagination },
    onPaginationChange: setPagination,
  })
  const date = (value: number) =>
    new Date(value * 1000).toLocaleString(locale, { timeZone: 'Asia/Shanghai' })
  return (
    <section
      className='mt-6 min-w-0 space-y-3'
      aria-label={`${t('Monitoring history')} · ${props.monitor.model}`}
    >
      <h3 className='font-semibold'>
        {t('Monitoring history')} · {props.monitor.model}
      </h3>
      <p className='text-muted-foreground text-xs'>
        {t('Dates use Asia/Shanghai; newest records appear first')}
      </p>
      {props.monitor.running && (
        <p role='status'>
          {t(
            props.monitor.running.unresolved
              ? 'Previous upstream execution remains unresolved'
              : 'Running'
          )}{' '}
          · {date(props.monitor.running.started_at)}
        </p>
      )}
      <div className='flex flex-wrap gap-3'>
        <div>
          <Label htmlFor={`from-${props.monitor.id}`}>{t('Start date')}</Label>
          <Input
            id={`from-${props.monitor.id}`}
            type='date'
            value={from}
            aria-invalid={invalidRange}
            onChange={(event) => {
              setFrom(event.target.value)
              setPagination((old) => ({ ...old, pageIndex: 0 }))
            }}
          />
        </div>
        <div>
          <Label htmlFor={`to-${props.monitor.id}`}>{t('End date')}</Label>
          <Input
            id={`to-${props.monitor.id}`}
            type='date'
            value={to}
            aria-invalid={invalidRange}
            onChange={(event) => {
              setTo(event.target.value)
              setPagination((old) => ({ ...old, pageIndex: 0 }))
            }}
          />
        </div>
      </div>
      {invalidRange && <p role='alert'>{t('Invalid date range')}</p>}
      {query.isPending && !invalidRange && <LoadingState />}
      {query.isError && (
        <ErrorState
          title={t('console.loadFailed')}
          onRetry={() => void query.refetch()}
        />
      )}
      {!invalidRange && query.isSuccess && (
        <>
          <StaticDataTable
            data={query.data.items ?? []}
            getRowKey={(record) => record.id}
            emptyContent={t('monitor.noRecords')}
            columns={[
              {
                id: 'time',
                header: t('Execution time'),
                cell: (record) => date(record.checked_at),
              },
              {
                id: 'status',
                header: t('Status'),
                cell: (record) => (
                  <Badge
                    className={
                      statusColors[record.status] ?? statusColors.unknown
                    }
                  >
                    {t(probeStatusKeys[record.status] ?? 'monitor.unknown')}
                  </Badge>
                ),
              },
              {
                id: 'reason',
                header: t('Reason'),
                cell: (record) =>
                  record.error_code
                    ? t(
                        probeReasonKeys[record.error_code] ??
                          'monitor.probeFailed'
                      )
                    : '-',
              },
              {
                id: 'latency',
                header: t('monitor.latestLatency'),
                cell: (record) =>
                  record.latency_ms == null
                    ? '-'
                    : formatNumber(record.latency_ms, locale),
              },
              {
                id: 'task',
                header: t('Task ID'),
                cell: (record) => record.task_id || '-',
              },
            ]}
          />
          <DataTablePagination table={table} compact />
        </>
      )}
    </section>
  )
}
