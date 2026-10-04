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
import { flexRender, type Row } from '@tanstack/react-table'
import { memo } from 'react'
import { useTranslation } from 'react-i18next'

import { formatPoolTime } from '../lib/time'
import type { PoolAccount } from '../types'
import { QuotaSummary } from './account-observations'

/**
 * Bespoke account card for the card view. Reuses the column cell renderers via
 * `flexRender` so both views share one implementation of the badges and
 * identity, and every action stays an always-visible button.
 */
function AccountCardComponent({ row }: { row: Row<PoolAccount> }) {
  const { t } = useTranslation()
  const cells = row.getAllCells()

  const renderCell = (id: string) => {
    const cell = cells.find((c) => c.column.id === id)
    if (!cell || !cell.column.columnDef.cell) {
      return null
    }
    return flexRender(cell.column.columnDef.cell, cell.getContext())
  }

  const labelClass = 'text-muted-foreground text-[11px] font-medium select-none'

  return (
    <div className='flex h-full flex-col gap-2'>
      <div className='flex items-start justify-between gap-2'>
        <div className='min-w-0'>
          <div className={labelClass}>#{row.original.id}</div>
          {renderCell('platform')}
        </div>
        {renderCell('actions')}
      </div>

      <div className='space-y-1'>
        <div className={labelClass}>{t('Masked identity')}</div>
        {renderCell('identity_masked') ?? (
          <span className='text-muted-foreground text-sm'>-</span>
        )}
      </div>

      <div className='flex flex-wrap items-center gap-1.5'>
        {renderCell('admin_status')}
        {renderCell('run_status')}
      </div>

      <div className='grid grid-cols-2 gap-2'>
        <div className='min-w-0 space-y-1'>
          <div className={labelClass}>{t('Credential ref')}</div>
          {renderCell('credential_ref') ?? (
            <span className='text-muted-foreground text-sm'>-</span>
          )}
        </div>
        <div className='min-w-0 space-y-1'>
          <div className={labelClass}>{t('Last refresh')}</div>
          <span className='text-muted-foreground block text-xs tabular-nums'>
            {formatPoolTime(row.original.last_refresh_at)}
          </span>
        </div>
      </div>

      <QuotaSummary quota={row.original.quota} />
      {row.original.note ? (
        <p className='text-muted-foreground line-clamp-2 text-xs'>
          {row.original.note}
        </p>
      ) : null}
    </div>
  )
}

export const AccountCard = memo(AccountCardComponent)
