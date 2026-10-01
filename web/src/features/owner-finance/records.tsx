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

const sources = {
  expenses: 'Expenses and correction evidence',
  'cash-receipts': 'Offline cash receipt evidence',
  'cash-refunds': 'Cash refund history',
  'supplier-bills': 'Supplier bill version history',
  'request-events': 'Captured request facts',
  audit: 'Finance audit trail',
  'versions/fx': 'Frozen FX versions',
  'supplier-bill-lines': 'Supplier bill line evidence',
  'sync-events': 'Sync facts for owner review',
} as const
type Source = keyof typeof sources
type RecordView = {
  id: number
  at: number
  reference: string
  kind: string
  currency: string
  amount: string | null
  amountCNY: string | null
  evidence: string
  note: string
  revision: string
  range: number[]
  status: string
  error: string
  attempts: number
}
// Keep only review fields in the query cache, excluding raw capture payloads.
function view(row: Record<string, unknown>): RecordView {
  const text = (key: string) => (row[key] == null ? '' : String(row[key]))
  const number = (key: string) => (typeof row[key] === 'number' ? row[key] : 0)
  const amount = text('amount') || text('amount_cny')
  const payload = row.payload as
    | { currency?: string; cny_per_unit?: string }
    | undefined
  return {
    id: number('id'),
    at:
      number('at') ||
      number('paid_at') ||
      number('refunded_at') ||
      number('created_at'),
    reference: [
      'user_id',
      'ref_id',
      'order_id',
      'receipt_id',
      'request_id',
      'attempt_id',
      'channel_id',
      'bill_id',
      'entity_id',
      'pool_key',
      'account_ref',
      'reference',
      'replaces_line_id',
      'references_line_id',
    ]
      .filter((key) => row[key])
      .map((key) => `${key}: ${text(key)}`)
      .join(' · '),
    kind:
      text('phase') ||
      text('action') ||
      text('state') ||
      text('category') ||
      text('kind') ||
      text('topic'),
    currency: text('currency') || payload?.currency || 'CNY',
    amount: amount || payload?.cny_per_unit || null,
    amountCNY:
      text('amount_cny') ||
      (text('currency') === 'CNY' ? text('amount') : '') ||
      null,
    evidence: text('evidence') || text('source'),
    note: text('note'),
    revision: [
      text('revision'),
      text('parent_id') ||
        text('replaces_id') ||
        text('adjusts_id') ||
        text('previous_id'),
      text('fx_version_id'),
    ]
      .filter(Boolean)
      .join(' / '),
    range: [
      number('period_from') || number('usage_from') || number('effective_from'),
      number('period_to') || number('usage_to') || number('effective_to'),
    ],
    status: text('status'),
    error: text('error_class'),
    attempts: number('attempts'),
  }
}
export function FinanceRecords({
  defaultSource = 'expenses',
}: {
  defaultSource?: Source
}) {
  const { t, i18n } = useTranslation()
  const [source, setSource] = useState<Source>(defaultSource)
  const [reference, setReference] = useState('')
  const [selection, setSelection] = useState<{
    source: Source
    reference: string
  } | null>(null)
  const [invalid, setInvalid] = useState(false)
  const [pagination, setPagination] = useState({ pageSize: 10, pageIndex: 0 })
  const query = useQuery({
    queryKey: [
      ...financeKey,
      'records',
      selection,
      selection?.source === 'supplier-bill-lines' ? 'all-lines' : pagination,
    ],
    enabled: selection !== null,
    queryFn: async ({ signal }) => {
      if (!selection) throw new Error('FINANCE_UNAVAILABLE')
      const filter: Record<string, string> = {}
      if (selection.source === 'supplier-bills') {
        filter.channel_id = selection.reference
      }
      if (selection.source === 'supplier-bill-lines') {
        filter.bill_id = selection.reference
      }
      if (selection.source === 'request-events' && selection.reference) {
        filter.request_id = selection.reference
      }
      if (selection.source === 'sync-events' && selection.reference) {
        filter.user_id = selection.reference
      }
      const params = {
        p: pagination.pageIndex + 1,
        page_size: pagination.pageSize,
        ...filter,
      }
      const result = await financeRequest<{
        items: Record<string, unknown>[]
        total?: number
      }>(`/${selection.source}`, { signal, params })
      return {
        items: result.items.map(view),
        total: result.total ?? result.items.length,
      }
    },
    retry: false,
    staleTime: 0,
    gcTime: 0,
    meta: { errorToast: false },
  })
  const { table } = useDataTable({
    data: query.data?.items ?? [],
    columns: [{ id: 'id' }],
    totalCount: query.data?.total ?? 0,
    manualPagination: selection?.source !== 'supplier-bill-lines',
    pagination,
    onPaginationChange: setPagination,
    columnVisibilityStorageKey: false,
    columnSizingStorageKey: false,
  })
  return (
    <Panel title={t('Evidence and immutable history')}>
      <p className='text-muted-foreground text-sm'>
        {t(
          'These records cover full ledger history. Use fact IDs to trace postings. FX rates are frozen CNY per original currency unit; they are not expense amounts.'
        )}
      </p>
      <form
        className='flex flex-wrap items-end gap-3'
        onSubmit={(e) => {
          e.preventDefault()
          if (
            (['supplier-bills', 'supplier-bill-lines'].includes(source) &&
              !integer(reference, 1)) ||
            (source === 'request-events' && reference.length > 64) ||
            (source === 'sync-events' &&
              reference &&
              (!integer(reference, 1) || Number(reference) > 2147483647))
          ) {
            setInvalid(true)
            return
          }
          setInvalid(false)
          setPagination({ ...pagination, pageIndex: 0 })
          setSelection({ source, reference })
          if (
            selection?.source === source &&
            selection.reference === reference
          ) {
            void query.refetch()
          }
        }}
      >
        <div className='min-w-0 space-y-1'>
          <Label htmlFor='finance-record-source'>{t('Evidence source')}</Label>
          <select
            id='finance-record-source'
            value={source}
            className='bg-background h-10 w-full rounded-md border px-3 text-sm'
            onChange={(e) => {
              setSource(e.target.value as Source)
              setReference('')
              setSelection(null)
            }}
          >
            {Object.entries(sources).map(([key, label]) => (
              <option value={key} key={key}>
                {t(label)}
              </option>
            ))}
          </select>
        </div>
        {[
          'supplier-bills',
          'supplier-bill-lines',
          'request-events',
          'sync-events',
        ].includes(source) && (
          <div className='space-y-1'>
            <Label htmlFor='finance-record-reference'>
              {t(
                {
                  'supplier-bills': 'Channel ID',
                  'supplier-bill-lines': 'Bill ID',
                  'sync-events': 'Wallet user ID (optional)',
                }[
                  source as
                    | 'supplier-bills'
                    | 'supplier-bill-lines'
                    | 'sync-events'
                ] ?? 'Request ID (optional)'
              )}
            </Label>
            <Input
              id='finance-record-reference'
              value={reference}
              onChange={(e) => setReference(e.target.value)}
            />
          </div>
        )}
        <Button type='submit' className='min-h-10'>
          {t('Read evidence')}
        </Button>
      </form>
      {invalid && (
        <p role='alert'>{t('Enter a valid value for this field.')}</p>
      )}
      {source === 'sync-events' && (
        <p className='text-muted-foreground text-xs'>
          {t(
            'Use the sync event ID for rebuild. Its reference ID is a redemption ID for offline sources or an observation ID for consumption. Check the wallet user and topic before selecting facts.'
          )}
        </p>
      )}
      {query.isFetching && (
        <div role='status'>
          <LoadingState message={t('Reading evidence')} />
        </div>
      )}
      {query.isError && (
        <ErrorState
          title={t('Evidence read failed')}
          description={t('Check the selected scope and access.')}
        />
      )}
      {query.data && (
        <>
          <StaticDataTable
            tableProps={{ 'aria-label': t('Finance evidence records') }}
            data={table.getRowModel().rows.map((row) => row.original)}
            emptyContent={t(
              'No registered records. Coverage remains unconfirmed.'
            )}
            columns={[
              { id: 'id', header: t('Fact ID'), cell: (row) => `#${row.id}` },
              {
                id: 'time',
                header: t('Business / registration time'),
                cell: (row) => financeTime(row.at, i18n.language),
              },
              {
                id: 'kind',
                header: t('Kind'),
                cell: (row) => <Code code={row.kind || 'partial'} />,
              },
              ...(selection?.source === 'sync-events'
                ? [
                    {
                      id: 'status',
                      header: t('Status / review class / attempts'),
                      cell: (row: RecordView) => (
                        <>
                          <Code code={row.status} /> /{' '}
                          {row.error ? <Code code={row.error} /> : '—'} /{' '}
                          {row.attempts}
                        </>
                      ),
                    },
                  ]
                : []),
              {
                id: 'refs',
                header: t('Related scope references'),
                cell: (row) => (
                  <span className='break-all'>{row.reference || '—'}</span>
                ),
              },
              {
                id: 'period',
                header: t('Attribution period'),
                cell: (row) =>
                  row.range[0]
                    ? `${financeTime(row.range[0], i18n.language)} – ${financeTime(row.range[1], i18n.language)}`
                    : '—',
              },
              {
                id: 'amount',
                header: t('Original amount / FX rate'),
                cell: (row) => (
                  <span className='font-mono'>
                    {row.currency} {row.amount ?? t('Not available')}
                  </span>
                ),
              },
              {
                id: 'cny',
                header: t('Frozen amount, CNY'),
                cell: (row) => <Money value={row.amountCNY} />,
              },
              {
                id: 'revision',
                header: t('Revision / predecessor or correction / FX ID'),
                cell: (row) => row.revision || '—',
              },
              {
                id: 'evidence',
                header: t('Evidence and reason'),
                cell: (row) => (
                  <span className='break-all'>
                    {row.evidence} {row.note}
                  </span>
                ),
              },
            ]}
          />
          <DataTablePagination table={table} compact />
        </>
      )}
    </Panel>
  )
}
