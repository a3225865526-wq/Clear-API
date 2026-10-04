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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ArrowLeft, Pause, Play, RefreshCw, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { handleServerError } from '@/lib/handle-server-error'
import { getServerErrorStatus } from '@/lib/server-error-message'

import {
  getPoolAccount,
  getPoolAccountCapabilities,
  pausePoolAccount,
  resumePoolAccount,
  syncPoolAccount,
  deletePoolAccount,
} from './api'
import { AccessTypeCapabilities } from './components/access-type-capabilities'
import {
  AccountQuotaPanel,
  AccountModelAdmissionPanel,
} from './components/account-observations'
import { AccountTasks } from './components/account-tasks'
import { AuthPanel } from './components/auth-panel'
import {
  PoolAdminStatusBadge,
  PoolRunStatusBadge,
} from './components/pool-badges'
import { POOL_QUERY_KEY } from './constants'
import { formatPoolTime } from './lib/time'
import type { PoolAuthStart } from './types'

function InfoItem(props: { label: string; children: React.ReactNode }) {
  return (
    <div className='min-w-0'>
      <dt className='text-muted-foreground text-xs'>{props.label}</dt>
      <dd className='mt-0.5 text-sm break-all'>{props.children}</dd>
    </div>
  )
}

export function PoolAccountDetail(props: { accountId: number }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [authStart, setAuthStart] = useState<PoolAuthStart | null>(null)
  const [deleteOpen, setDeleteOpen] = useState(false)

  const detailQuery = useQuery({
    queryKey: [...POOL_QUERY_KEY, 'account', props.accountId],
    queryFn: ({ signal }) => getPoolAccount(props.accountId, signal),
    retry: false,
  })
  const capabilitiesQuery = useQuery({
    queryKey: [...POOL_QUERY_KEY, 'account', props.accountId, 'capabilities'],
    queryFn: ({ signal }) =>
      getPoolAccountCapabilities(props.accountId, signal),
    enabled: detailQuery.isSuccess,
  })
  const account = detailQuery.data?.account

  const invalidate = () =>
    void queryClient.invalidateQueries({ queryKey: [...POOL_QUERY_KEY] })

  const pauseMutation = useMutation({
    mutationFn: (version: number) => pausePoolAccount(props.accountId, version),
    onSuccess: () => {
      toast.success(t('Account paused'))
      invalidate()
    },
    onError: (error) => {
      handleServerError(error)
      invalidate()
    },
  })
  const resumeMutation = useMutation({
    mutationFn: (version: number) =>
      resumePoolAccount(props.accountId, version),
    onSuccess: () => {
      toast.success(t('Account resumed'))
      invalidate()
    },
    onError: (error) => {
      handleServerError(error)
      invalidate()
    },
  })
  const syncMutation = useMutation({
    mutationFn: () => syncPoolAccount(props.accountId),
    onSuccess: () => {
      toast.success(t('Status synced'))
      invalidate()
    },
    onError: (error) => {
      handleServerError(error)
      invalidate()
    },
  })
  const deleteMutation = useMutation({
    mutationFn: () => deletePoolAccount(props.accountId),
    onSuccess: () => {
      toast.success(t('Account deleted'))
      void queryClient.invalidateQueries({ queryKey: [...POOL_QUERY_KEY] })
      queryClient.removeQueries({
        queryKey: [...POOL_QUERY_KEY, 'account', props.accountId],
      })
    },
    onError: (error) => {
      // 502: the executor blocked the deletion; the account is kept.
      if (getServerErrorStatus(error) === 502) {
        toast.error(
          t(
            'Deletion was blocked because the executor is unreachable. The account is kept.'
          )
        )
        invalidate()
        return
      }
      handleServerError(error)
      invalidate()
    },
  })

  let content
  if (detailQuery.isPending) {
    content = (
      <div role='status'>
        <LoadingState message={t('Loading account')} />
      </div>
    )
  } else if (detailQuery.isError || !account) {
    content = (
      <ErrorState
        title={t('Account not found')}
        description={t('The pool account may have been deleted.')}
        onRetry={() => void detailQuery.refetch()}
      />
    )
  } else {
    const paused = account.admin_status === 'paused'
    const pendingTask = detailQuery.data?.tasks.find(
      (task) => task.status === 'pending'
    )
    const actionBusy =
      pauseMutation.isPending ||
      resumeMutation.isPending ||
      syncMutation.isPending ||
      deleteMutation.isPending
    content = (
      <div className='space-y-4'>
        <section className='bg-card rounded-xl border p-4'>
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <h3 className='text-sm font-semibold'>
              {t('Account information')}
            </h3>
            <div className='flex flex-wrap gap-2'>
              <Button
                variant='outline'
                size='sm'
                disabled={actionBusy}
                onClick={() => syncMutation.mutate()}
              >
                <RefreshCw />
                {t('Sync status')}
              </Button>
              {paused ? (
                <Button
                  variant='outline'
                  size='sm'
                  disabled={actionBusy}
                  onClick={() => resumeMutation.mutate(account.version)}
                >
                  <Play />
                  {t('Resume')}
                </Button>
              ) : (
                <Button
                  variant='outline'
                  size='sm'
                  disabled={actionBusy}
                  onClick={() => pauseMutation.mutate(account.version)}
                >
                  <Pause />
                  {t('Pause')}
                </Button>
              )}
              <Button
                variant='destructive'
                size='sm'
                disabled={actionBusy}
                onClick={() => setDeleteOpen(true)}
              >
                <Trash2 />
                {t('Delete')}
              </Button>
            </div>
          </div>
          <dl className='mt-3 grid gap-x-6 gap-y-3 sm:grid-cols-2 lg:grid-cols-3'>
            <InfoItem label={t('ID')}>
              <span className='font-mono'>#{account.id}</span>
            </InfoItem>
            <InfoItem label={t('Platform')}>{account.platform}</InfoItem>
            <InfoItem label={t('Access type')}>
              <span className='font-mono text-xs'>{account.access_type}</span>
            </InfoItem>
            <InfoItem label={t('Masked identity')}>
              <span className='font-mono text-xs'>
                {account.identity_masked || '-'}
              </span>
            </InfoItem>
            <InfoItem label={t('Admin status')}>
              <PoolAdminStatusBadge status={account.admin_status} />
            </InfoItem>
            <InfoItem label={t('Run status')}>
              <PoolRunStatusBadge status={account.run_status} />
            </InfoItem>
            <InfoItem label={t('Executor')}>
              <span className='font-mono text-xs'>
                {account.executor_id || '-'}
              </span>
            </InfoItem>
            <InfoItem label={t('Credential ref')}>
              <span className='font-mono text-xs'>
                {account.credential_ref || '-'}
              </span>
            </InfoItem>
            <InfoItem label={t('Version')}>
              <span className='font-mono tabular-nums'>{account.version}</span>
            </InfoItem>
            <InfoItem label={t('Last refresh')}>
              <span className='tabular-nums'>
                {formatPoolTime(account.last_refresh_at)}
              </span>
            </InfoItem>
            <InfoItem label={t('Created at')}>
              <span className='tabular-nums'>
                {formatPoolTime(account.created_at)}
              </span>
            </InfoItem>
            <InfoItem label={t('Updated at')}>
              <span className='tabular-nums'>
                {formatPoolTime(account.updated_at)}
              </span>
            </InfoItem>
            <InfoItem label={t('Note')}>{account.note || '-'}</InfoItem>
            <InfoItem label={t('Last error')}>
              <span className='text-destructive'>
                {account.last_error || '-'}
              </span>
            </InfoItem>
          </dl>
        </section>

        <AccountQuotaPanel accountId={account.id} />
        <AccountModelAdmissionPanel accountId={account.id} />
        <AuthPanel
          account={account}
          pendingTask={pendingTask}
          authStart={authStart}
          onStarted={(start) => {
            setAuthStart(start)
            invalidate()
          }}
          onSettled={() => {
            setAuthStart(null)
            invalidate()
          }}
        />

        <section className='bg-card space-y-3 rounded-xl border p-4'>
          <h3 className='text-sm font-semibold'>{t('Tasks')}</h3>
          <AccountTasks tasks={detailQuery.data?.tasks ?? []} />
        </section>

        {capabilitiesQuery.data ? (
          <section className='bg-card space-y-3 rounded-xl border p-4'>
            <h3 className='text-sm font-semibold'>{t('Capabilities')}</h3>
            <AccessTypeCapabilities
              platformLabel={capabilitiesQuery.data.platform.label}
              platform={capabilitiesQuery.data.platform.platform}
              accessType={capabilitiesQuery.data.access_type}
              accountStatus={capabilitiesQuery.data.account_status}
            />
          </section>
        ) : null}
      </div>
    )
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        <span className='flex min-w-0 items-center gap-2'>
          <Button
            variant='ghost'
            size='icon-sm'
            className='shrink-0'
            render={<Link to='/pool' />}
            aria-label={t('Back to accounts')}
          >
            <ArrowLeft />
          </Button>
          <span className='truncate'>
            {t('Account detail')}
            {account ? ` · #${account.id} · ${account.platform}` : ''}
          </span>
        </span>
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>{content}</SectionPageLayout.Content>
      {account ? (
        <ConfirmDialog
          open={deleteOpen}
          onOpenChange={setDeleteOpen}
          title={t('Delete account #{{id}}?', { id: account.id })}
          destructive
          isLoading={deleteMutation.isPending}
          confirmText={t('Delete')}
          handleConfirm={() => deleteMutation.mutate()}
          desc={t(
            'The upstream credential is deleted on the executor first. If the executor is unreachable, deletion is blocked and the account is kept.'
          )}
        />
      ) : null}
    </SectionPageLayout>
  )
}
