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
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { formatQuota } from '@/lib/format'

import { useBalanceTransactions } from '../../hooks/use-balance-transactions'
import { formatTimestamp } from '../../lib/billing'
import { formatCnyCents } from '../../lib/money'
import type { BalanceTransactionDto } from '../../types'

const FILTER_TYPES = [
  '',
  'RECHARGE',
  'BONUS',
  'ADMIN',
  'COMPENSATION',
  'REFERRAL',
  'ACTIVITY',
] as const

function transactionTypeLabel(type: string, t: (key: string) => string) {
  switch (type) {
    case 'RECHARGE':
      return t('Recharge')
    case 'BONUS':
      return t('Member bonus')
    case 'ADMIN':
      return t('Admin adjustment')
    case 'COMPENSATION':
      return t('Compensation')
    case 'REFERRAL':
      return t('Referral reward')
    case 'ACTIVITY':
      return t('Activity reward')
    default:
      return type
  }
}

const TONE_BY_TYPE: Record<string, 'success' | 'info' | 'neutral'> = {
  RECHARGE: 'success',
  BONUS: 'info',
}

function amountLabel(amount: number, format: (value: number) => string) {
  return amount >= 0
    ? `+ ${format(amount)}`
    : `- ${format(Math.abs(amount))}`
}

interface BalanceTransactionsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

/**
 * Balance adjustment ledger (own records only). Renders the explicit DTO
 * fields; reference_id / metadata internals are never displayed — only the
 * backend-sanitized code_tail (last 4 chars) may appear.
 */
export function BalanceTransactionsDialog(props: BalanceTransactionsDialogProps) {
  const { t } = useTranslation()
  const {
    records,
    total,
    page,
    pageSize,
    type,
    loading,
    handlePageChange,
    handlePageSizeChange,
    handleTypeChange,
  } = useBalanceTransactions(props.open)

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Balance Details')}
      description={t('Recharges, bonuses and other balance adjustments')}
      contentClassName='flex max-h-(--dialog-available-height) flex-col max-sm:w-screen max-sm:max-w-none max-sm:rounded-none max-sm:p-4 sm:max-w-2xl'
      contentHeight='auto'
      bodyClassName='space-y-3'
    >
      <div className='min-h-0 space-y-3'>
        <div className='flex items-center justify-between gap-2'>
          <Select
            items={FILTER_TYPES.map((value) => ({
              value,
              label:
                value === ''
                  ? t('All types')
                  : transactionTypeLabel(value, t),
            }))}
            value={type}
            onValueChange={(value) => value !== null && handleTypeChange(value)}
          >
            <SelectTrigger className='h-9 w-40'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                {FILTER_TYPES.map((value) => (
                  <SelectItem key={value} value={value}>
                    {value === ''
                      ? t('All types')
                      : transactionTypeLabel(value, t)}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
          <Select
            items={['10', '20', '50'].map((value) => ({
              value,
              label: t('{{count}} / page', { count: Number(value) }),
            }))}
            value={pageSize.toString()}
            onValueChange={(value) =>
              value !== null && handlePageSizeChange(Number.parseInt(value))
            }
          >
            <SelectTrigger className='h-9 w-[92px]'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                {['10', '20', '50'].map((value) => (
                  <SelectItem key={value} value={value}>
                    {t('{{count}} / page', { count: Number(value) })}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </div>

        <div className='max-h-[min(54vh,520px)] overflow-y-auto pr-1'>
          {loading && (
            <div className='space-y-3'>
              {['a', 'b', 'c'].map((key) => (
                <div key={key} className='rounded-lg border p-3 sm:p-4'>
                  <Skeleton className='h-4 w-40' />
                  <Skeleton className='mt-2 h-3 w-full' />
                </div>
              ))}
            </div>
          )}
          {!loading && records.length === 0 && (
            <div className='text-muted-foreground flex min-h-40 flex-col items-center justify-center py-10 text-center'>
              <p className='text-sm font-medium'>
                {t('No balance transactions found')}
              </p>
            </div>
          )}
          {!loading && records.length > 0 && (
            <div className='space-y-3'>
              {records.map((record: BalanceTransactionDto) => {
                const positive = record.amount_quota >= 0
                return (
                  <div
                    key={record.id}
                    className='rounded-lg border p-3 sm:p-4'
                  >
                    <div className='flex items-start justify-between gap-2'>
                      <div className='flex min-w-0 items-center gap-2'>
                        <StatusBadge
                          label={transactionTypeLabel(record.type, t)}
                          variant={TONE_BY_TYPE[record.type] ?? 'neutral'}
                          showDot
                          copyable={false}
                        />
                        {record.code_tail ? (
                          <span className='text-muted-foreground font-mono text-xs'>
                            ····{record.code_tail}
                          </span>
                        ) : null}
                      </div>
                      <span
                        className={`text-sm font-semibold tabular-nums ${
                          positive ? 'text-green-600' : 'text-red-600'
                        }`}
                      >
                        {amountLabel(record.amount_quota, formatQuota)}
                      </span>
                    </div>
                    <div className='text-muted-foreground mt-1 text-xs'>
                      {formatTimestamp(record.created_at)}
                    </div>
                    <div className='mt-2 grid grid-cols-2 gap-2 sm:grid-cols-3'>
                      <div className='space-y-0.5'>
                        <Label className='text-muted-foreground text-xs'>
                          {t('Recharge amount')}
                        </Label>
                        <div className='text-sm font-medium tabular-nums'>
                          {record.type === 'RECHARGE'
                            ? formatCnyCents(record.recharge_cents)
                            : '—'}
                        </div>
                      </div>
                      <div className='space-y-0.5'>
                        <Label className='text-muted-foreground text-xs'>
                          {t('Balance after')}
                        </Label>
                        <div className='text-sm font-medium tabular-nums'>
                          {formatQuota(record.balance_after)}
                        </div>
                      </div>
                      <div className='col-span-2 space-y-0.5 sm:col-span-1'>
                        <Label className='text-muted-foreground text-xs'>
                          {t('Description')}
                        </Label>
                        <div className='truncate text-sm'>
                          {record.description || '—'}
                        </div>
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </div>

        {!loading && records.length > 0 && (
          <div className='flex flex-col items-center gap-3 border-t pt-4 sm:flex-row sm:items-center sm:justify-between'>
            <div className='text-muted-foreground text-xs sm:text-sm'>
              {t('Showing')} {(page - 1) * pageSize + 1}-
              {Math.min(page * pageSize, total)} {t('of')} {total}
            </div>
            <div className='flex items-center gap-2'>
              <Button
                variant='outline'
                size='sm'
                onClick={() => handlePageChange(page - 1)}
                disabled={page <= 1}
                className='h-8 w-8 p-0'
              >
                <ChevronLeft className='h-4 w-4' />
              </Button>
              <div className='text-muted-foreground flex items-center gap-1 text-sm'>
                <span className='font-medium'>{page}</span>
                <span>/</span>
                <span>{totalPages}</span>
              </div>
              <Button
                variant='outline'
                size='sm'
                onClick={() => handlePageChange(page + 1)}
                disabled={page >= totalPages}
                className='h-8 w-8 p-0'
              >
                <ChevronRight className='h-4 w-4' />
              </Button>
            </div>
          </div>
        )}
      </div>
    </Dialog>
  )
}
