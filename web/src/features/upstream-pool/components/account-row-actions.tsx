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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Eye, Pause, Play, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { handleServerError } from '@/lib/handle-server-error'
import { getServerErrorStatus } from '@/lib/server-error-message'

import { deletePoolAccount, pausePoolAccount, resumePoolAccount } from '../api'
import { POOL_QUERY_KEY } from '../constants'
import type { PoolAccount } from '../types'

function invalidatePool(queryClient: ReturnType<typeof useQueryClient>) {
  void queryClient.invalidateQueries({ queryKey: [...POOL_QUERY_KEY] })
}

/**
 * Always-visible row actions shared by the desktop table and the mobile card
 * view. Primary actions never depend on hover menus.
 */
export function AccountRowActions(props: { account: PoolAccount }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [deleteOpen, setDeleteOpen] = useState(false)
  const paused = props.account.admin_status === 'paused'

  const pauseMutation = useMutation({
    mutationFn: () => pausePoolAccount(props.account.id, props.account.version),
    onSuccess: () => {
      toast.success(t('Account paused'))
      invalidatePool(queryClient)
    },
    onError: (error) => {
      handleServerError(error)
      invalidatePool(queryClient)
    },
  })
  const resumeMutation = useMutation({
    mutationFn: () =>
      resumePoolAccount(props.account.id, props.account.version),
    onSuccess: () => {
      toast.success(t('Account resumed'))
      invalidatePool(queryClient)
    },
    onError: (error) => {
      handleServerError(error)
      invalidatePool(queryClient)
    },
  })
  const deleteMutation = useMutation({
    mutationFn: () => deletePoolAccount(props.account.id),
    onSuccess: () => {
      toast.success(t('Account deleted'))
      setDeleteOpen(false)
      invalidatePool(queryClient)
    },
    onError: (error) => {
      // 502: the executor blocked the deletion; the account is kept.
      if (getServerErrorStatus(error) === 502) {
        toast.error(
          t(
            'Deletion was blocked because the executor is unreachable. The account is kept.'
          )
        )
        invalidatePool(queryClient)
        return
      }
      handleServerError(error)
      invalidatePool(queryClient)
    },
  })
  const busy =
    pauseMutation.isPending ||
    resumeMutation.isPending ||
    deleteMutation.isPending

  return (
    <div className='flex shrink-0 items-center gap-0.5'>
      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant='ghost'
              size='icon-sm'
              className='text-muted-foreground hover:text-foreground'
              render={
                <Link
                  to='/pool/$accountId'
                  params={{ accountId: String(props.account.id) }}
                />
              }
            />
          }
        >
          <Eye />
        </TooltipTrigger>
        <TooltipContent>{t('Details')}</TooltipContent>
      </Tooltip>
      {paused ? (
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='ghost'
                size='icon-sm'
                className='text-muted-foreground hover:text-foreground'
                disabled={busy}
                aria-label={t('Resume')}
                onClick={() => resumeMutation.mutate()}
              />
            }
          >
            <Play />
          </TooltipTrigger>
          <TooltipContent>{t('Resume')}</TooltipContent>
        </Tooltip>
      ) : (
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='ghost'
                size='icon-sm'
                className='text-muted-foreground hover:text-foreground'
                disabled={busy}
                aria-label={t('Pause')}
                onClick={() => pauseMutation.mutate()}
              />
            }
          >
            <Pause />
          </TooltipTrigger>
          <TooltipContent>{t('Pause')}</TooltipContent>
        </Tooltip>
      )}
      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant='ghost'
              size='icon-sm'
              className='text-destructive hover:text-destructive'
              disabled={busy}
              aria-label={t('Delete')}
              onClick={() => setDeleteOpen(true)}
            />
          }
        >
          <Trash2 />
        </TooltipTrigger>
        <TooltipContent>{t('Delete')}</TooltipContent>
      </Tooltip>
      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title={t('Delete account #{{id}}?', { id: props.account.id })}
        destructive
        isLoading={deleteMutation.isPending}
        confirmText={t('Delete')}
        handleConfirm={() => deleteMutation.mutate()}
        desc={t(
          'The upstream credential is deleted on the executor first. If the executor is unreachable, deletion is blocked and the account is kept.'
        )}
      />
    </div>
  )
}
