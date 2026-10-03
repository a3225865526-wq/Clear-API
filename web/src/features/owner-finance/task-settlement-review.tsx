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

import {
  DataTablePagination,
  StaticDataTable,
  useDataTable,
} from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import { financeKey, financeRequest } from './access'
import { Panel } from './components'
import { TaskSettlementRetry } from './task-settlement-retry'

export type TaskSettlement = {
  id: number
  task_id: number
  request_id: string
  user_id: number
  original_quota: number
  target_quota: number | null
  state: string
  error_class: string
  attempts: number
  log_state: string
  log_attempts: number
  token_outcome: string
}

export function TaskSettlementReview(props: { ready: boolean }) {
  const { t, i18n } = useTranslation()
  const client = useQueryClient()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [scope, setScope] = useState('pending')
  const [selected, setSelected] = useState<TaskSettlement | null>(null)
  const [saved, setSaved] = useState(false)
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 10 })
  const query = useQuery({
    queryKey: [...financeKey, 'task-settlements', scope, pagination],
    enabled: props.ready,
    queryFn: ({ signal }) =>
      financeRequest<{ items: TaskSettlement[]; total: number }>(
        '/task-settlements',
        {
          signal,
          params: {
            scope,
            p: pagination.pageIndex + 1,
            page_size: pagination.pageSize,
          },
        }
      ),
    retry: false,
    gcTime: 0,
    meta: { errorToast: false },
  })
  const { table } = useDataTable({
    data: query.data?.items ?? [],
    columns: [{ id: 'id' }],
    manualPagination: true,
    totalCount: query.data?.total ?? 0,
    pagination,
    onPaginationChange: setPagination,
    columnVisibilityStorageKey: false,
    columnSizingStorageKey: false,
  })
  return (
    <Panel title={t('Async task settlement review')}>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Task completion and funding settlement are separate. Missing facts never authorize a guessed charge or refund.'
        )}
      </p>
      {!props.ready ? (
        <p role='status'>
          {t(
            'Task settlement requires approved main and log migrations and the single-instance switch.'
          )}
        </p>
      ) : (
        <>
          <div className='flex flex-wrap items-end gap-3'>
            <div>
              <Label htmlFor='task-settlement-scope'>{t('Review scope')}</Label>
              <NativeSelect
                id='task-settlement-scope'
                value={scope}
                onChange={(event) => {
                  setScope(event.target.value)
                  setPagination((current) => ({ ...current, pageIndex: 0 }))
                }}
              >
                <NativeSelectOption value='pending'>
                  {t('Pending review')}
                </NativeSelectOption>
                <NativeSelectOption value='applied'>
                  {t('Reviewed history')}
                </NativeSelectOption>
              </NativeSelect>
            </div>
            <Button
              variant='outline'
              onClick={() => void query.refetch()}
              disabled={query.isFetching}
            >
              {t('Refresh')}
            </Button>
          </div>
          {saved && (
            <p role='status'>
              {t(
                'Settlement retry completed. Refresh the ledger to verify related finance records.'
              )}
            </p>
          )}
          {query.isLoading && <LoadingState />}
          {query.isError && <ErrorState onRetry={() => void query.refetch()} />}
          {query.isSuccess && (
            <>
              <StaticDataTable
                data={query.data.items}
                getRowKey={(row) => row.id}
                columns={[
                  {
                    id: 'task',
                    header: t('Task ID'),
                    cell: (row) => `#${row.task_id}`,
                  },
                  {
                    id: 'user',
                    header: t('User'),
                    cell: (row) => `#${row.user_id}`,
                  },
                  {
                    id: 'state',
                    header: t('State'),
                    cell: (row) => t(`task_settlement_state_${row.state}`),
                  },
                  {
                    id: 'reason',
                    header: t('Review reason'),
                    cell: (row) =>
                      row.error_class
                        ? t(`task_settlement_error_${row.error_class}`)
                        : t('No settlement error'),
                  },
                  {
                    id: 'charged',
                    header: t('Charged quota units'),
                    cell: (row) => formatNumber(row.original_quota, locale),
                  },
                  {
                    id: 'target',
                    header: t('Frozen target quota'),
                    cell: (row) =>
                      row.target_quota === null
                        ? t('Unknown')
                        : formatNumber(row.target_quota, locale),
                  },
                  {
                    id: 'log',
                    header: t('Settlement log delivery'),
                    cell: (row) =>
                      t(`task_settlement_log_${row.log_state || 'not_ready'}`),
                  },
                  {
                    id: 'action',
                    header: t('Actions'),
                    cell: (row) => {
                      const canRetry =
                        row.target_quota !== null &&
                        (row.state === 'needs_review' ||
                          (row.state === 'applied' &&
                            row.log_state === 'needs_review'))
                      return canRetry ? (
                        <Button
                          variant='outline'
                          onClick={() => {
                            setSelected(row)
                            setSaved(false)
                          }}
                        >
                          {t('Inspect settlement retry')}
                        </Button>
                      ) : (
                        <span>{t('No safe retry available')}</span>
                      )
                    },
                  },
                ]}
              />
              <DataTablePagination table={table} compact />
            </>
          )}
        </>
      )}
      <Dialog
        open={selected !== null}
        onOpenChange={(open) => {
          if (!open) setSelected(null)
        }}
        title={t('Task settlement retry')}
      >
        {selected && (
          <div className='space-y-4'>
            <p className='font-mono text-xs break-all'>{selected.request_id}</p>
            <p>
              {t('Charged quota units')}:{' '}
              {formatNumber(selected.original_quota, locale)} ·{' '}
              {t('Frozen target quota')}:{' '}
              {formatNumber(selected.target_quota ?? 0, locale)}
            </p>
            <p>
              {t('Task quota difference')}:{' '}
              {formatNumber(
                (selected.target_quota ?? 0) - selected.original_quota,
                locale
              )}
            </p>
            <p>
              {t('Token quota correction')}:{' '}
              {t(
                `task_settlement_token_${selected.token_outcome || 'not_ready'}`
              )}
            </p>
            {selected.state === 'applied' && (
              <p>
                {t(
                  'Funding is already settled. This retry only delivers the missing log.'
                )}
              </p>
            )}
            <TaskSettlementRetry
              key={selected.id}
              row={selected}
              onSaved={() => {
                setSelected(null)
                setSaved(true)
                void client.invalidateQueries({ queryKey: financeKey })
              }}
            />
          </div>
        )}
      </Dialog>
    </Panel>
  )
}
