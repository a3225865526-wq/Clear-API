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
import { useTranslation } from 'react-i18next'

import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import { POOL_FAIL_REASON_LABELS, POOL_TASK_KIND_LABELS } from '../constants'
import { formatPoolTime } from '../lib/time'
import type { PoolTask } from '../types'
import { PoolTaskStatusBadge } from './pool-badges'

export function AccountTasks(props: { tasks: PoolTask[] }) {
  const { t } = useTranslation()
  if (props.tasks.length === 0) {
    return <p className='text-muted-foreground text-sm'>{t('No tasks yet.')}</p>
  }
  return (
    <>
      {/* Desktop table */}
      <div className='hidden overflow-x-auto sm:block'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('ID')}</TableHead>
              <TableHead>{t('Kind')}</TableHead>
              <TableHead>{t('Status')}</TableHead>
              <TableHead>{t('Fail reason')}</TableHead>
              <TableHead>{t('Expires at')}</TableHead>
              <TableHead>{t('Created at')}</TableHead>
              <TableHead>{t('Updated at')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {props.tasks.map((task) => (
              <TableRow key={task.id}>
                <TableCell className='font-mono text-xs'>
                  {task.id.slice(0, 12)}
                </TableCell>
                <TableCell>{t(POOL_TASK_KIND_LABELS[task.kind])}</TableCell>
                <TableCell>
                  <PoolTaskStatusBadge status={task.status} />
                </TableCell>
                <TableCell className='text-destructive text-xs'>
                  {task.fail_reason
                    ? t(POOL_FAIL_REASON_LABELS[task.fail_reason])
                    : '-'}
                </TableCell>
                <TableCell className='text-muted-foreground text-xs tabular-nums'>
                  {formatPoolTime(task.expires_at)}
                </TableCell>
                <TableCell className='text-muted-foreground text-xs tabular-nums'>
                  {formatPoolTime(task.created_at)}
                </TableCell>
                <TableCell className='text-muted-foreground text-xs tabular-nums'>
                  {formatPoolTime(task.updated_at)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
      {/* Mobile cards */}
      <div className='space-y-2 sm:hidden'>
        {props.tasks.map((task) => (
          <div
            key={task.id}
            className='bg-card space-y-1.5 rounded-lg border p-3'
          >
            <div className='flex items-center justify-between gap-2'>
              <span className='font-mono text-xs'>{task.id.slice(0, 12)}</span>
              <PoolTaskStatusBadge status={task.status} />
            </div>
            <div className='text-sm'>
              {t(POOL_TASK_KIND_LABELS[task.kind])}
              {task.fail_reason
                ? ` · ${t(POOL_FAIL_REASON_LABELS[task.fail_reason])}`
                : ''}
            </div>
            <dl className='text-muted-foreground grid grid-cols-2 gap-1 text-xs'>
              <div>
                <dt className='inline'>{t('Expires at')}: </dt>
                <dd className='inline tabular-nums'>
                  {formatPoolTime(task.expires_at)}
                </dd>
              </div>
              <div>
                <dt className='inline'>{t('Created at')}: </dt>
                <dd className='inline tabular-nums'>
                  {formatPoolTime(task.created_at)}
                </dd>
              </div>
            </dl>
          </div>
        ))}
      </div>
    </>
  )
}
