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
import { Link } from '@tanstack/react-router'
import { Archive, Eye, Pencil, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import {
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import dayjs from '@/lib/dayjs'

import { MappedStatusBadge } from './status-labels'
import { useRouteChainsDialogs } from './route-chains-provider'
import { OVERALL_HEALTH_LABELS } from '../constants'
import type { ChainListItem } from '../types'

// Route chain list (§十九): the overall-health column is a presentation
// summary — never a route authority.

interface RouteChainsTableProps {
  items: ChainListItem[]
}

export function RouteChainsTable(props: RouteChainsTableProps) {
  const { items } = props
  const { t } = useTranslation()

  const columns: StaticDataTableColumn<ChainListItem>[] = [
    {
      id: 'model',
      header: t('Model'),
      cell: (row) => (
        <Link
          to='/route-chains/$id'
          params={{ id: String(row.id) }}
          className='font-medium underline-offset-4 hover:underline'
        >
          {row.model}
        </Link>
      ),
    },
    { id: 'group', header: t('Group'), cell: (row) => row.group },
    {
      id: 'enabled',
      header: t('Enabled'),
      cell: (row) => (
        <Badge variant={row.enabled ? 'default' : 'outline'}>
          {row.enabled ? t('ON') : t('OFF')}
        </Badge>
      ),
    },
    {
      id: 'candidates',
      header: t('Candidates'),
      cell: (row) => (
        <span>
          {row.candidate_count}
          {row.primary_candidate ? (
            <span className='text-muted-foreground'>
              {' '}
              · {t('Primary')}: #{row.primary_candidate.candidate_id}
            </span>
          ) : null}
        </span>
      ),
    },
    {
      id: 'fallbacks',
      header: t('Fallbacks'),
      cell: (row) => row.fallback_count,
    },
    {
      id: 'cross_source',
      header: t('Cross-source'),
      cell: (row) =>
        row.allow_cross_source ? (
          <Badge variant='warning'>{t('Allowed')}</Badge>
        ) : (
          <Badge variant='outline'>{t('Blocked')}</Badge>
        ),
    },
    {
      id: 'health',
      header: t('Overall health'),
      cell: (row) => (
        <MappedStatusBadge
          stateLabel={OVERALL_HEALTH_LABELS[row.overall_health]}
          fallbackKey={row.overall_health}
        />
      ),
    },
    {
      id: 'version',
      header: t('Version'),
      cell: (row) => `v${row.version}`,
    },
    {
      id: 'updated',
      header: t('Updated'),
      cell: (row) =>
        row.updated_time > 0 ? dayjs.unix(row.updated_time).format('YYYY-MM-DD HH:mm') : '—',
    },
    {
      id: 'actions',
      header: '',
      className: 'w-10',
      cell: (row) => (
        <DropdownMenuHost row={row} />
      ),
    },
  ]

  return (
    <StaticDataTable columns={columns} data={items} getRowKey={(row) => row.id} />
  )
}

function DropdownMenuHost(props: { row: ChainListItem }) {
  const { row } = props
  const { t } = useTranslation()
  const { openFor } = useRouteChainsDialogs()
  return (
    <div className='flex justify-end gap-1'>
      <Link
        to='/route-chains/$id'
        params={{ id: String(row.id) }}
        aria-label={t('View details')}
        className={buttonVariants({ variant: 'ghost', size: 'icon-xs' })}
      >
        <Eye />
      </Link>
      <Button
        variant='ghost'
        size='icon-xs'
        aria-label={t('Edit Route Chain')}
        disabled={row.archived}
        onClick={() => openFor('edit-chain', row)}
      >
        <Pencil />
      </Button>
      <Button
        variant='ghost'
        size='icon-xs'
        aria-label={t('Archive route chain')}
        disabled={row.archived}
        onClick={() => openFor('archive-chain', row)}
      >
        <Archive />
      </Button>
      <Button
        variant='ghost'
        size='icon-xs'
        aria-label={t('Delete route chain')}
        onClick={() => openFor('delete-chain', row)}
      >
        <Trash2 />
      </Button>
    </div>
  )
}
