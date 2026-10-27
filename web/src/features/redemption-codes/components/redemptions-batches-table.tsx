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
import { Download, Plus, ShieldBan, Trash2 } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatCnyCents } from '@/features/wallet/lib/money'
import { formatQuota } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import {
  archiveRedemptionBatch,
  disableRedemptionBatch,
  getRedemptionBatches,
  inspectRedemptionBatchDeletion,
} from '../api'
import type { RedemptionBatch, RedemptionBatchDeletion } from '../types'
import { RedemptionsBatchCreateDialog } from './redemptions-batch-create-dialog'
import { RedemptionsBatchExportDialog } from './redemptions-batch-export-dialog'

const MAX_BATCH_COUNT = 100 // matches backend cap; backend stays authoritative

function sourceLabel(source: string, t: (key: string) => string) {
  return source === 'STORE' ? t('Store') : source
}

export function RedemptionsBatchesTable() {
  const { t } = useTranslation()
  const [batches, setBatches] = useState<RedemptionBatch[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(true)
  const [createOpen, setCreateOpen] = useState(false)
  const [exportBatchId, setExportBatchId] = useState<number | null>(null)
  const [disablingBatch, setDisablingBatch] = useState<RedemptionBatch | null>(
    null
  )
  const [disabling, setDisabling] = useState(false)
  const [deletion, setDeletion] = useState<RedemptionBatchDeletion | null>(null)
  const [checkingDeletion, setCheckingDeletion] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const deletionPending = useRef(false)

  const fetchBatches = useCallback(async (targetPage: number) => {
    try {
      setLoading(true)
      const response = await getRedemptionBatches(targetPage, 10)
      if (response.success && response.data) {
        setBatches(response.data.items ?? [])
        setTotal(response.data.total ?? 0)
        setPage(targetPage)
      } else {
        handleServerError(response)
      }
    } catch (error) {
      handleServerError(error)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void fetchBatches(1)
  }, [fetchBatches])

  const handleDisable = async () => {
    if (!disablingBatch) return
    try {
      setDisabling(true)
      const response = await disableRedemptionBatch(disablingBatch.id)
      if (response.success) {
        toast.success(
          t('Batch disabled, {{count}} unused codes invalidated', {
            count: response.data ?? 0,
          })
        )
        setDisablingBatch(null)
        await fetchBatches(page)
      } else {
        handleServerError(response)
      }
    } catch (error) {
      handleServerError(error)
    } finally {
      setDisabling(false)
    }
  }

  const totalPages = Math.max(1, Math.ceil(total / 10))

  const inspectDeletion = async (id: number) => {
    if (deletionPending.current) return
    deletionPending.current = true
    setCheckingDeletion(true)
    try {
      const response = await inspectRedemptionBatchDeletion(id)
      if (response.success && response.data) {
        setDeletion(response.data)
      } else {
        handleServerError(response, undefined, {
          title: t(response.message || 'Failed to inspect batch deletion'),
        })
      }
    } catch (error) {
      handleServerError(error)
    } finally {
      deletionPending.current = false
      setCheckingDeletion(false)
    }
  }

  const handleDelete = async () => {
    if (!deletion || deletion.blocked || deletionPending.current) return
    deletionPending.current = true
    setDeleting(true)
    try {
      const response = await archiveRedemptionBatch(deletion.batch.id)
      if (response.success) {
        toast.success(
          t(
            'Batch deleted. Unused codes are invalidated and historical records are preserved.'
          )
        )
        setDeletion(null)
        await fetchBatches(batches.length === 1 ? Math.max(1, page - 1) : page)
      } else {
        handleServerError(response, undefined, {
          title: t(response.message || 'Failed to delete batch'),
        })
      }
    } catch (error) {
      handleServerError(error)
    } finally {
      deletionPending.current = false
      setDeleting(false)
    }
  }

  return (
    <div className='space-y-3'>
      <div className='flex items-center justify-between'>
        <p className='text-muted-foreground text-sm'>
          {t('Generate store codes in batches and export them for the mall')}
        </p>
        <Button size='sm' onClick={() => setCreateOpen(true)}>
          <Plus className='h-4 w-4' />
          {t('Create Batch')}
        </Button>
      </div>

      <div className='overflow-x-auto rounded-lg border'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Batch Name')}</TableHead>
              <TableHead>{t('Source')}</TableHead>
              <TableHead>{t('Face Value')}</TableHead>
              <TableHead>{t('Quota')}</TableHead>
              <TableHead>{t('Total')}</TableHead>
              <TableHead>{t('Used')}</TableHead>
              <TableHead>{t('Unused')}</TableHead>
              <TableHead>{t('Disabled')}</TableHead>
              <TableHead>{t('Status')}</TableHead>
              <TableHead>{t('Created Time')}</TableHead>
              <TableHead className='text-right'>{t('Actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {loading && (
              <TableRow>
                <TableCell colSpan={11}>
                  <Skeleton className='h-8 w-full' />
                </TableCell>
              </TableRow>
            )}
            {!loading && batches.length === 0 && (
              <TableRow>
                <TableCell
                  colSpan={11}
                  className='text-muted-foreground py-10 text-center text-sm'
                >
                  {t('No batches yet')}
                </TableCell>
              </TableRow>
            )}
            {!loading &&
              batches.map((batch) => (
                <TableRow key={batch.id}>
                  <TableCell className='font-medium'>{batch.name}</TableCell>
                  <TableCell>{sourceLabel(batch.source, t)}</TableCell>
                  <TableCell className='tabular-nums'>
                    {formatCnyCents(batch.face_value_cents)}
                  </TableCell>
                  <TableCell className='tabular-nums'>
                    {formatQuota(batch.quota)}
                  </TableCell>
                  <TableCell className='tabular-nums'>{batch.count}</TableCell>
                  <TableCell className='tabular-nums'>
                    {batch.used_count}
                  </TableCell>
                  <TableCell className='tabular-nums'>
                    {batch.unused_count}
                  </TableCell>
                  <TableCell className='tabular-nums'>
                    {batch.disabled_count}
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant={batch.status === 1 ? 'secondary' : 'destructive'}
                    >
                      {batch.status === 1 ? t('Enabled') : t('Disabled')}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    {new Date(batch.created_time * 1000).toLocaleString()}
                  </TableCell>
                  <TableCell className='text-right'>
                    <div className='flex justify-end gap-1'>
                      <Button
                        variant='ghost'
                        size='sm'
                        className='h-8 px-2'
                        onClick={() => setExportBatchId(batch.id)}
                      >
                        <Download className='h-4 w-4' />
                        {t('Export Unused')}
                      </Button>
                      <Button
                        variant='ghost'
                        size='sm'
                        className='text-destructive h-8 px-2'
                        disabled={checkingDeletion || deleting}
                        onClick={() => void inspectDeletion(batch.id)}
                      >
                        <Trash2 className='h-4 w-4' />
                        {t('Delete')}
                      </Button>
                      <Button
                        variant='ghost'
                        size='sm'
                        className='text-destructive h-8 px-2'
                        disabled={batch.status !== 1}
                        onClick={() => setDisablingBatch(batch)}
                      >
                        <ShieldBan className='h-4 w-4' />
                        {t('Disable Batch')}
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
          </TableBody>
        </Table>
      </div>

      {total > 10 && (
        <div className='flex items-center justify-end gap-2'>
          <Button
            variant='outline'
            size='sm'
            disabled={page <= 1}
            onClick={() => void fetchBatches(page - 1)}
          >
            {t('Previous')}
          </Button>
          <span className='text-muted-foreground text-sm'>
            {page} / {totalPages}
          </span>
          <Button
            variant='outline'
            size='sm'
            disabled={page >= totalPages}
            onClick={() => void fetchBatches(page + 1)}
          >
            {t('Next')}
          </Button>
        </div>
      )}

      <RedemptionsBatchCreateDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        maxCount={MAX_BATCH_COUNT}
        onCreated={(batchId, _count, downloaded) => {
          setCreateOpen(false)
          void fetchBatches(1)
          if (!downloaded) setExportBatchId(batchId)
        }}
      />

      {exportBatchId !== null && (
        <RedemptionsBatchExportDialog
          batchId={exportBatchId}
          onClose={() => setExportBatchId(null)}
        />
      )}

      <ConfirmDialog
        destructive
        open={!!disablingBatch}
        onOpenChange={(open: boolean) => !open && setDisablingBatch(null)}
        handleConfirm={handleDisable}
        isLoading={disabling}
        className='max-w-md'
        title={t('Disable Batch?')}
        desc={t(
          'All unused codes in this batch will be invalidated. This cannot be undone for now.'
        )}
        confirmText={t('Disable Batch')}
      />
      <ConfirmDialog
        destructive
        open={deletion !== null}
        onOpenChange={(open) => {
          if (!open && !deleting) setDeletion(null)
        }}
        handleConfirm={() => void handleDelete()}
        isLoading={deleting}
        disabled={!deletion || deletion.blocked}
        title={t('Delete Batch')}
        confirmText={t('Delete Batch')}
        desc={
          <div className='space-y-2'>
            <p>
              {deletion?.batch.name} · #{deletion?.batch.id}
            </p>
            <p>
              {t(
                'Codes: {{total}} total, {{used}} used, {{unused}} unused, {{disabled}} disabled',
                {
                  total: deletion?.total,
                  used: deletion?.used,
                  unused: deletion?.unused,
                  disabled: deletion?.disabled,
                }
              )}
            </p>
            <p>
              {t(
                'Related records: {{orders}} orders, {{balance}} balance entries, {{finance}} finance events, {{rewards}} rewards, {{approvals}} approval events',
                {
                  orders: deletion?.orders,
                  balance: deletion?.balance_entries,
                  finance: deletion?.finance_events,
                  rewards: deletion?.rewards,
                  approvals: deletion?.approval_events,
                }
              )}
            </p>
            <p>
              {t(
                'The batch will be archived and hidden. Unused codes will stop working. Used codes, orders and accounting history will be retained.'
              )}
            </p>
            {deletion?.blocked ? (
              <p className='text-destructive'>
                {t(
                  'This batch is current product delivery evidence. Replace and review the evidence before deleting it.'
                )}
              </p>
            ) : null}
          </div>
        }
      />
    </div>
  )
}
