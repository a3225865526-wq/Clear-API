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

import {
  DataTablePagination,
  StaticDataTable,
  useDataTable,
} from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

import { financeKey, financeRequest } from './access'
import { Code, Money, Panel } from './components'
import { financeTime } from './format'
import { integer } from './operation-form'

type LedgerEvent = {
  id: number
  revision: number
  at: number
  kind: string
  quota: number
  source: string
  value_cny: string | null
  evidence: string
  reverses: number
}
type Allocation = {
  id: number
  lot_id: number
  quota: number
  value_cny: string | null
}
type Balance = {
  business_quota: number
  ledger_quota: number | null
  business_minus_ledger: number | null
  unprocessed_sync_events: number | null
  needs_review_sync_events: number | null
  status: string
  warnings: string[]
}
function safeInteger(value: number | null | undefined, unknown: string) {
  return value != null && Number.isSafeInteger(value) ? String(value) : unknown
}
export function WalletReconciliation() {
  const { t } = useTranslation()
  const [input, setInput] = useState('')
  const [user, setUser] = useState(0)
  const [event, setEvent] = useState(0)
  const [invalid, setInvalid] = useState(false)
  const [pagination, setPagination] = useState({ pageSize: 10, pageIndex: 0 })
  const [allocationPage, setAllocationPage] = useState({
    pageSize: 10,
    pageIndex: 0,
  })
  const balance = useQuery({
    queryKey: [...financeKey, 'balance', user],
    enabled: user > 0,
    queryFn: ({ signal }) =>
      financeRequest<Balance>(`/wallets/${user}/balance-check`, { signal }),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    meta: { errorToast: false },
  })
  const events = useQuery({
    queryKey: [...financeKey, 'events', user, pagination],
    enabled: user > 0,
    queryFn: ({ signal }) =>
      financeRequest<{ items: LedgerEvent[]; total: number }>(
        `/wallets/${user}/events`,
        {
          signal,
          params: {
            p: pagination.pageIndex + 1,
            page_size: pagination.pageSize,
          },
        }
      ),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    meta: { errorToast: false },
  })
  const allocations = useQuery({
    queryKey: [...financeKey, 'allocations', user, event, allocationPage],
    enabled: user > 0 && event > 0,
    queryFn: ({ signal }) =>
      financeRequest<{ items: Allocation[]; total: number }>(
        `/wallets/${user}/allocations`,
        {
          signal,
          params: {
            event_id: event,
            p: allocationPage.pageIndex + 1,
            page_size: allocationPage.pageSize,
          },
        }
      ),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    meta: { errorToast: false },
  })
  return (
    <Panel title={t('Wallet balance and FIFO reconciliation')}>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Wallet views cover their full ledger, not the selected profit period. Balance reads are non-atomic and matching balances do not prove complete sources. Quota is not a CNY amount.'
        )}
      </p>
      <form
        className='flex flex-wrap items-end gap-2'
        onSubmit={(e) => {
          e.preventDefault()
          if (!integer(input, 1) || Number(input) > 2147483647) {
            setInvalid(true)
            return
          }
          setInvalid(false)
          setEvent(0)
          setPagination({ ...pagination, pageIndex: 0 })
          setUser(Number(input))
          if (user === Number(input)) {
            void balance.refetch()
            void events.refetch()
          }
        }}
      >
        <div className='space-y-1'>
          <Label htmlFor='finance-wallet-user'>{t('Wallet user ID')}</Label>
          <Input
            id='finance-wallet-user'
            inputMode='numeric'
            value={input}
            onChange={(e) => setInput(e.target.value)}
          />
        </div>
        <Button className='min-h-10' type='submit'>
          {t('Read wallet ledger')}
        </Button>
      </form>
      {invalid && (
        <p role='alert' className='text-destructive text-sm'>
          {t('Enter a valid value for this field.')}
        </p>
      )}
      {(balance.isFetching || events.isFetching || allocations.isFetching) && (
        <div role='status'>
          <LoadingState message={t('Reading wallet facts')} />
        </div>
      )}
      {(balance.isError || events.isError || allocations.isError) && (
        <ErrorState
          title={t('Wallet read failed')}
          description={t(
            'Check the selected wallet and fact IDs. No missing balance is inferred as zero.'
          )}
        />
      )}
      {balance.data && (
        <>
          <dl className='grid gap-2 text-sm sm:grid-cols-2'>
            {[
              ['Business quota', balance.data.business_quota],
              ['Ledger quota', balance.data.ledger_quota],
              ['Business minus ledger', balance.data.business_minus_ledger],
              ['Pending sync events', balance.data.unprocessed_sync_events],
              [
                'Needs review sync events',
                balance.data.needs_review_sync_events,
              ],
            ].map(([label, value]) => (
              <div key={String(label)}>
                <dt className='text-muted-foreground'>{t(String(label))}</dt>
                <dd className='font-mono'>
                  {safeInteger(value as number | null, t('Unknown'))}
                </dd>
              </div>
            ))}
          </dl>
          <p className='text-sm'>
            <Code code={balance.data.status} />
          </p>
          <ul className='list-inside list-disc text-xs'>
            {balance.data.warnings.map((code) => (
              <li key={code}>
                <Code code={code} />
              </li>
            ))}
          </ul>
        </>
      )}
      {events.data && (
        <ServerLedgerTable
          data={events.data.items}
          total={events.data.total}
          pagination={pagination}
          onPagination={setPagination}
          onPick={(id) => {
            setEvent(id)
            setAllocationPage({ pageSize: 10, pageIndex: 0 })
          }}
        />
      )}
      {event > 0 && allocations.data && (
        <AllocationTable
          event={event}
          data={allocations.data.items}
          total={allocations.data.total}
          pagination={allocationPage}
          onPagination={setAllocationPage}
        />
      )}
    </Panel>
  )
}
type Pagination = { pageSize: number; pageIndex: number }
function ServerLedgerTable({
  data,
  total,
  pagination,
  onPagination,
  onPick,
}: {
  data: LedgerEvent[]
  total: number
  pagination: Pagination
  onPagination: React.Dispatch<React.SetStateAction<Pagination>>
  onPick: (id: number) => void
}) {
  const { t, i18n } = useTranslation()
  const { table } = useDataTable({
    data,
    columns: [{ id: 'id' }],
    totalCount: total,
    manualPagination: true,
    pagination,
    onPaginationChange: onPagination,
    columnVisibilityStorageKey: false,
    columnSizingStorageKey: false,
  })
  return (
    <div className='space-y-2'>
      <StaticDataTable
        data={data}
        tableProps={{ 'aria-label': t('Wallet events') }}
        emptyContent={t('No registered records. Coverage remains unconfirmed.')}
        columns={[
          {
            id: 'id',
            header: t('Event / revision'),
            cell: (row) => `#${row.id} / ${row.revision}`,
          },
          {
            id: 'time',
            header: t('Business time'),
            cell: (row) => financeTime(row.at, i18n.language),
          },
          {
            id: 'kind',
            header: t('Kind / source'),
            cell: (row) => (
              <>
                <Code code={row.kind} /> /{' '}
                <Code code={row.source || 'unknown'} />
              </>
            ),
          },
          {
            id: 'quota',
            header: t('Quota'),
            cell: (row) => safeInteger(row.quota, t('Unknown')),
          },
          {
            id: 'value',
            header: t('Source value, CNY'),
            cell: (row) => <Money value={row.value_cny} />,
          },
          {
            id: 'evidence',
            header: t('Evidence / reversed event'),
            cell: (row) => (
              <span className='break-all'>
                {row.evidence} / {row.reverses || '—'}
              </span>
            ),
          },
          {
            id: 'parts',
            header: t('FIFO parts'),
            cell: (row) => (
              <Button
                className='min-h-10'
                variant='outline'
                onClick={() => onPick(row.id)}
              >
                {t('Inspect allocations')}
                <span className='sr-only'> #{row.id}</span>
              </Button>
            ),
          },
        ]}
      />
      <DataTablePagination table={table} compact />
    </div>
  )
}
function AllocationTable({
  event,
  data,
  total,
  pagination,
  onPagination,
}: {
  event: number
  data: Allocation[]
  total: number
  pagination: Pagination
  onPagination: React.Dispatch<React.SetStateAction<Pagination>>
}) {
  const { t } = useTranslation()
  const { table } = useDataTable({
    data,
    columns: [{ id: 'id' }],
    totalCount: total,
    manualPagination: true,
    pagination,
    onPaginationChange: onPagination,
    columnVisibilityStorageKey: false,
    columnSizingStorageKey: false,
  })
  return (
    <div className='space-y-2'>
      <h4 className='font-medium'>
        {t('FIFO allocations for event')} #{event}
      </h4>
      <StaticDataTable
        data={data}
        tableProps={{ 'aria-label': t('FIFO allocations') }}
        emptyContent={t('No registered allocations.')}
        columns={[
          {
            id: 'id',
            header: t('Allocation / source lot'),
            cell: (row) => `#${row.id} / #${row.lot_id}`,
          },
          {
            id: 'quota',
            header: t('Quota'),
            cell: (row) => safeInteger(row.quota, t('Unknown')),
          },
          {
            id: 'value',
            header: t('Frozen contribution, CNY'),
            cell: (row) => <Money value={row.value_cny} />,
          },
        ]}
      />
      <DataTablePagination table={table} compact />
    </div>
  )
}
