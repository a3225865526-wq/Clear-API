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
import { BillingDecisionForm } from './billing-decision-form'
import type { BillingReservation } from './billing-review-types'
import { Panel } from './components'
import { financeTime } from './format'

export function BillingReconciliation(props: { ready: boolean }) {
  const { t, i18n } = useTranslation()
  const client = useQueryClient()
  const [scope, setScope] = useState('pending')
  const [selected, setSelected] = useState<BillingReservation | null>(null)
  const [saved, setSaved] = useState(false)
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 10 })
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const query = useQuery({
    queryKey: [...financeKey, 'billing-reservations', scope, pagination],
    enabled: props.ready,
    queryFn: ({ signal }) =>
      financeRequest<{ items: BillingReservation[]; total: number }>(
        '/billing-reservations',
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
    <Panel title={t('Billing reservation review')}>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Unknown-usage charges and abandoned reservations require evidence. Age alone never authorizes a refund.'
        )}
      </p>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Wallet refunds append evidence. Any already posted finance revenue requires separate ledger reconciliation.'
        )}
      </p>
      {!props.ready ? (
        <p role='status'>
          {t(
            'Billing review requires an approved migration and the single-instance management switch.'
          )}
        </p>
      ) : (
        <>
          <div className='flex flex-wrap items-end gap-3'>
            <div>
              <Label htmlFor='billing-review-scope'>{t('Review scope')}</Label>
              <NativeSelect
                id='billing-review-scope'
                value={scope}
                onChange={(event) => {
                  setScope(event.target.value)
                  setPagination({ ...pagination, pageIndex: 0 })
                  setSaved(false)
                }}
              >
                <NativeSelectOption value='pending'>
                  {t('Awaiting decision')}
                </NativeSelectOption>
                <NativeSelectOption value='reviewed'>
                  {t('Decision history')}
                </NativeSelectOption>
              </NativeSelect>
            </div>
            <Button
              variant='outline'
              disabled={query.isFetching}
              onClick={() => void query.refetch()}
            >
              {t('Refresh reservations')}
            </Button>
          </div>
          {saved && <p role='status'>{t('Billing decision saved.')}</p>}
          {query.isFetching && (
            <LoadingState message={t('Reading reservations')} />
          )}
          {query.isError ? (
            <ErrorState
              title={t('Reservation read failed')}
              onRetry={() => void query.refetch()}
            />
          ) : (
            <>
              <StaticDataTable
                data={query.data?.items ?? []}
                getRowKey={(row) => row.id}
                tableProps={{ 'aria-label': t('Billing reservations') }}
                emptyContent={t('No reservations in this review scope.')}
                columns={[
                  {
                    id: 'request',
                    header: t('Request reference'),
                    cell: (row) => (
                      <span className='font-mono text-xs break-all'>
                        {row.request_id} · {t('User')} #{row.user_id}
                      </span>
                    ),
                  },
                  {
                    id: 'state',
                    header: t('Billing state'),
                    cell: (row) => (
                      <span>
                        {t(`billing_reservation_state_${row.state}`)}
                        {row.executing &&
                          ` · ${t('Request is still executing')}`}
                      </span>
                    ),
                  },
                  {
                    id: 'reason',
                    header: t('Review reason'),
                    cell: (row) =>
                      t(`billing_reservation_reason_${row.reason}`),
                  },
                  {
                    id: 'quota',
                    header: t('Charged quota units'),
                    cell: (row) => formatNumber(row.applied, locale),
                  },
                  {
                    id: 'updated',
                    header: t('Updated at'),
                    cell: (row) => financeTime(row.updated_at, i18n.language),
                  },
                  {
                    id: 'action',
                    header: t('Actions'),
                    cell: (row) => (
                      <Button
                        variant='outline'
                        disabled={row.executing}
                        onClick={() => {
                          setSelected(row)
                          setSaved(false)
                        }}
                      >
                        {row.decision
                          ? t('View billing decision')
                          : t('Resolve reservation')}
                      </Button>
                    ),
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
        title={t('Billing decision')}
      >
        {selected && (
          <div className='space-y-4'>
            <p className='font-mono text-xs break-all'>{selected.request_id}</p>
            <p>
              {t('Reserved quota')}: {formatNumber(selected.reserved, locale)} ·{' '}
              {t('Reported target quota')}:{' '}
              {formatNumber(selected.target, locale)} ·{' '}
              {t('Charged quota units')}:{' '}
              {formatNumber(selected.applied, locale)}
            </p>
            {selected.decision ? (
              <dl className='grid grid-cols-2 gap-2 text-sm'>
                <dt>{t('Refund quota units')}</dt>
                <dd>{formatNumber(selected.decision.refund_quota, locale)}</dd>
                <dt>{t('Final charged quota')}</dt>
                <dd>{formatNumber(selected.decision.final_quota, locale)}</dd>
                <dt>{t('Reconciliation evidence')}</dt>
                <dd className='break-words'>{selected.decision.reason}</dd>
                <dt>{t('Decision actor')}</dt>
                <dd>#{selected.decision.actor_id}</dd>
                <dt>{t('Token quota correction')}</dt>
                <dd>
                  {t(`billing_token_refund_${selected.decision.token_refund}`)}
                </dd>
              </dl>
            ) : (
              <BillingDecisionForm
                key={selected.id}
                row={selected}
                onSaved={() => {
                  setSelected(null)
                  setSaved(true)
                  void client.invalidateQueries({ queryKey: financeKey })
                }}
              />
            )}
          </div>
        )}
      </Dialog>
    </Panel>
  )
}
