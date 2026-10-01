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
import { useMemo, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import {
  DataTablePagination,
  StaticDataTable,
  useDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { Badge } from '@/components/ui/badge'

import { financeMoney } from './format'
import { codeLabels } from './labels'

export function Code({ code }: { code: string }) {
  const { t } = useTranslation()
  return (
    <span className='break-words'>
      {codeLabels[code] ? (
        t(codeLabels[code])
      ) : (
        <>
          {t('Coverage or review code')}: <code>{code}</code>
        </>
      )}
    </span>
  )
}
export function Money({ value }: { value: string | null | undefined }) {
  const { t, i18n } = useTranslation()
  const formatted = financeMoney(value, i18n.language)
  return formatted == null ? (
    <Badge variant='outline'>{t('Unknown')}</Badge>
  ) : (
    <span
      className={`font-mono break-all tabular-nums ${value?.startsWith('-') ? 'text-destructive' : ''}`}
    >
      {formatted}
    </span>
  )
}
export function FinanceTable<T>({
  data,
  columns,
  label,
}: {
  data: T[]
  columns: StaticDataTableColumn<T>[]
  label: string
}) {
  const { t } = useTranslation()
  const definitions = useMemo(
    () => columns.map((column) => ({ id: column.id })),
    [columns]
  )
  const { table } = useDataTable({
    data,
    columns: definitions,
    columnVisibilityStorageKey: false,
    columnSizingStorageKey: false,
    initialPagination: { pageSize: 10, pageIndex: 0 },
  })
  return (
    <div className='min-w-0 space-y-2'>
      <StaticDataTable
        columns={columns}
        data={table.getRowModel().rows.map((row) => row.original)}
        tableProps={{ 'aria-label': label }}
        emptyContent={t('No registered records. Coverage remains unconfirmed.')}
      />
      {data.length > 10 && <DataTablePagination table={table} compact />}
    </div>
  )
}
export function Panel({
  title,
  children,
}: {
  title: string
  children: ReactNode
}) {
  return (
    <section className='bg-card min-w-0 space-y-3 rounded-xl border p-4 sm:p-5'>
      <h3 className='font-semibold'>{title}</h3>
      {children}
    </section>
  )
}
