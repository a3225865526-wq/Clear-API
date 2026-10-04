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
import type { ColumnDef, ColumnFiltersState } from '@tanstack/react-table'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTablePage, useDataTable } from '@/components/data-table'

import { getPoolAccounts, getPoolCatalog } from '../api'
import { POOL_QUERY_KEY, POOL_VIEW_MODE_STORAGE_KEY } from '../constants'
import { formatPoolTime } from '../lib/time'
import type { PoolAccount } from '../types'
import { AccountCard } from './account-card'
import { QuotaSummary } from './account-observations'
import { AccountRowActions } from './account-row-actions'
import { PoolAdminStatusBadge, PoolRunStatusBadge } from './pool-badges'

function matchesFilterValue(
  row: { getValue: (id: string) => unknown },
  id: string,
  value: unknown
) {
  if (!Array.isArray(value) || value.length === 0 || value.includes('all')) {
    return true
  }
  return value.includes(String(row.getValue(id)))
}

export function AccountsTable() {
  const { t } = useTranslation()
  const [columnFilters, setColumnFilters] = useState<ColumnFiltersState>([])
  const [globalFilter, setGlobalFilter] = useState('')

  const accountsQuery = useQuery({
    queryKey: [...POOL_QUERY_KEY, 'accounts'],
    queryFn: ({ signal }) => getPoolAccounts({}, signal),
  })
  const catalogQuery = useQuery({
    queryKey: [...POOL_QUERY_KEY, 'catalog'],
    queryFn: ({ signal }) => getPoolCatalog(signal),
    staleTime: 5 * 60 * 1000,
  })

  const accounts = accountsQuery.data ?? []

  const platformFilterOptions = useMemo(
    () => [
      { label: t('All platforms'), value: 'all' },
      ...(catalogQuery.data?.platforms ?? []).map((p) => ({
        label: p.label,
        value: p.platform,
      })),
    ],
    [catalogQuery.data, t]
  )

  const columns = useMemo<ColumnDef<PoolAccount, unknown>[]>(
    () => [
      {
        accessorKey: 'id',
        header: t('ID'),
        cell: ({ row }) => (
          <span className='font-mono text-xs tabular-nums'>
            #{row.original.id}
          </span>
        ),
      },
      {
        accessorKey: 'platform',
        header: t('Platform / Access type'),
        cell: ({ row }) => (
          <div className='flex min-w-0 flex-col'>
            <span className='truncate text-sm font-medium'>
              {row.original.platform}
            </span>
            <span className='text-muted-foreground truncate font-mono text-xs'>
              {row.original.access_type}
            </span>
          </div>
        ),
        filterFn: matchesFilterValue,
      },
      {
        accessorKey: 'identity_masked',
        header: t('Masked identity'),
        cell: ({ row }) => (
          <span className='font-mono text-xs break-all'>
            {row.original.identity_masked || '-'}
          </span>
        ),
      },
      {
        accessorKey: 'admin_status',
        header: t('Admin status'),
        cell: ({ row }) => (
          <PoolAdminStatusBadge status={row.original.admin_status} />
        ),
        filterFn: matchesFilterValue,
      },
      {
        accessorKey: 'run_status',
        header: t('Run status'),
        cell: ({ row }) => (
          <PoolRunStatusBadge status={row.original.run_status} />
        ),
        filterFn: matchesFilterValue,
      },
      {
        id: 'quota',
        header: t('Account quota'),
        cell: ({ row }) => <QuotaSummary quota={row.original.quota} />,
      },
      {
        accessorKey: 'credential_ref',
        header: t('Credential ref'),
        cell: ({ row }) => (
          <span
            className='block max-w-48 truncate font-mono text-xs'
            title={row.original.credential_ref}
          >
            {row.original.credential_ref || '-'}
          </span>
        ),
      },
      {
        accessorKey: 'last_refresh_at',
        header: t('Last refresh'),
        cell: ({ row }) => (
          <span className='text-muted-foreground text-xs tabular-nums'>
            {formatPoolTime(row.original.last_refresh_at)}
          </span>
        ),
      },
      {
        id: 'actions',
        header: t('Actions'),
        cell: ({ row }) => <AccountRowActions account={row.original} />,
      },
    ],
    [t]
  )

  const { table } = useDataTable({
    data: accounts,
    columns,
    totalCount: accounts.length,
    columnFilters,
    onColumnFiltersChange: setColumnFilters,
    globalFilter,
    onGlobalFilterChange: setGlobalFilter,
    withFilteredRowModel: true,
    withPaginationRowModel: true,
    withSortedRowModel: true,
    getRowId: (row: PoolAccount) => String(row.id),
  })

  return (
    <DataTablePage
      table={table}
      columns={columns}
      isLoading={accountsQuery.isLoading}
      isFetching={accountsQuery.isFetching}
      emptyTitle={t('No accounts found')}
      emptyDescription={t('Create your first upstream account to get started.')}
      skeletonKeyPrefix='pool-account'
      enableCardView
      viewModeStorageKey={POOL_VIEW_MODE_STORAGE_KEY}
      renderCard={(row) => <AccountCard row={row} />}
      cardGridClassName='grid grid-cols-1 gap-3 sm:gap-4 lg:grid-cols-2 xl:grid-cols-3'
      applyHeaderSize
      toolbarProps={{
        collapsibleOnMobile: true,
        searchPlaceholder: t('Filter by identity, platform, note...'),
        filters: [
          {
            columnId: 'platform',
            title: t('Platform'),
            options: platformFilterOptions,
            singleSelect: true,
          },
          {
            columnId: 'admin_status',
            title: t('Admin status'),
            options: [
              { label: t('All'), value: 'all' },
              { label: t('Enabled'), value: 'enabled' },
              { label: t('Paused'), value: 'paused' },
            ],
            singleSelect: true,
          },
          {
            columnId: 'run_status',
            title: t('Run status'),
            options: [
              { label: t('All'), value: 'all' },
              { label: t('Available'), value: 'available' },
              { label: t('Awaiting authorization'), value: 'awaiting_auth' },
              { label: t('Authorizing'), value: 'authorizing' },
              {
                label: t('Re-authorization required'),
                value: 'reauth_required',
              },
              { label: t('Cooling down'), value: 'cooldown' },
              { label: t('Executor offline'), value: 'executor_offline' },
              { label: t('Not admitted'), value: 'not_admitted' },
            ],
            singleSelect: true,
          },
        ],
      }}
    />
  )
}
