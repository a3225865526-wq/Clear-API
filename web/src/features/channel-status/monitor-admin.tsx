import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'
import { api } from '@/lib/api'
import { formatCurrencyFromUSD } from '@/lib/currency'
import { formatNumber, formatTimestamp } from '@/lib/format'
import { requireServerSuccess } from '@/lib/server-error-message'

import type { Monitor, MonitorTargets } from './lib/monitor-admin-schema'
import { MonitorAdminForm } from './monitor-admin-form'

export function MonitorAdmin(props: { allowlist?: boolean }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const base = props.allowlist
    ? '/api/console/monitor-targets'
    : '/api/console/monitor-admin'
  const catalogKey = props.allowlist
    ? 'monitor-allowlist-catalog'
    : 'monitor-admin-catalog'
  const client = useQueryClient()
  const [edit, setEdit] = useState<Monitor | null>(null)
  const [probeResult, setProbeResult] = useState<{
    status: string
    error_code: string
  } | null>(null)
  const [confirmation, setConfirmation] = useState<{
    id: number
    action: string
    value: boolean
  } | null>(null)
  const targets = useQuery({
    queryKey: ['monitor-admin-targets', base],
    queryFn: async () =>
      requireServerSuccess(
        (
          await api.get<{ success: boolean; data: MonitorTargets }>(
            `${base}/targets`
          )
        ).data
      ).data,
    retry: false,
  })
  const catalog = useQuery({
    queryKey: [catalogKey],
    queryFn: async () =>
      requireServerSuccess(
        (
          await api.get<{
            success: boolean
            data: {
              monitors: Monitor[]
              policy?: { max_concurrent: number; daily_cost_micros: number }
            }
          }>(base)
        ).data
      ).data,
    retry: false,
  })
  const toggle = useMutation({
    mutationFn: async () => {
      if (!confirmation) return
      return requireServerSuccess(
        (
          await api.put(
            `${base}/${confirmation.id}/switch/${confirmation.action}`,
            { value: confirmation.value, confirm: true },
            { headers: { 'X-ClearAPI-Monitor': '1' } }
          )
        ).data
      )
    },
    onSuccess: () => {
      setConfirmation(null)
      void client.invalidateQueries({ queryKey: [catalogKey] })
      void client.invalidateQueries({ queryKey: ['monitor-admin-catalog'] })
      void client.invalidateQueries({ queryKey: ['monitor-allowlist-catalog'] })
      void client.invalidateQueries({ queryKey: ['console-monitoring'] })
    },
  })
  const probe = useMutation({
    mutationFn: async (id: number) =>
      requireServerSuccess(
        (
          await api.post<{
            success: boolean
            data: { status: string; error_code: string }
          }>(
            `/api/console/monitor-admin/${id}/probe`,
            { confirm: true },
            { headers: { 'X-ClearAPI-Monitor': '1' } }
          )
        ).data
      ).data,
    onSuccess: (result) => {
      setConfirmation(null)
      setProbeResult(result)
      void client.invalidateQueries({ queryKey: ['console-monitoring'] })
    },
  })
  return (
    <section
      aria-label={t('Monitor administration')}
      className='space-y-4 rounded-lg border p-4'
    >
      <h2 className='font-semibold'>{t('Monitor administration')}</h2>
      <p role='status' className='text-muted-foreground text-sm'>
        {t(
          'Monitoring requires an upstream cost contract and an enabled budget policy.'
        )}
      </p>
      {(catalog.isPending || targets.isPending) && <LoadingState />}
      {(catalog.isError || targets.isError) && (
        <ErrorState
          title={t('console.loadFailed')}
          onRetry={() => {
            void catalog.refetch()
            void targets.refetch()
          }}
        />
      )}
      {catalog.data && targets.data && (
        <>
          {props.allowlist && catalog.data.policy && (
            <div className='rounded border p-3 text-sm'>
              <p>{t('Global monitoring policy (read only)')}</p>
              <p>
                {t('Maximum concurrent probes')}:{' '}
                {formatNumber(catalog.data.policy.max_concurrent, locale)}
              </p>
              <p>
                {t('Daily monitoring budget')}:{' '}
                {formatCurrencyFromUSD(
                  catalog.data.policy.daily_cost_micros / 1000000,
                  { locale }
                )}
              </p>
              <p>
                {t('Allowlist membership does not enable probe execution.')}
              </p>
            </div>
          )}
          <MonitorAdminForm
            targets={targets.data}
            allowlist={props.allowlist}
          />
          <div className='space-y-3'>
            {catalog.data.monitors.map((item) => (
              <article
                key={item.id}
                className='flex flex-wrap items-center justify-between gap-3 rounded border p-3'
              >
                <div className='min-w-0 break-words'>
                  <p>
                    {item.display_name} · {item.model}
                  </p>
                  {props.allowlist && (
                    <p className='text-muted-foreground text-sm'>
                      {t('Created by')}:{' '}
                      {item.created_by
                        ? formatNumber(item.created_by, locale)
                        : t('Unknown')}{' '}
                      · {t('Created at')}:{' '}
                      {item.created_at
                        ? formatTimestamp(item.created_at)
                        : t('Unknown')}
                    </p>
                  )}
                  <p className='text-muted-foreground text-sm'>
                    {item.target_kind || 'channel'} ·{' '}
                    {t(item.published ? 'Published' : 'Disabled')} ·{' '}
                    {t(item.enabled ? 'Enabled' : 'Pause')}
                  </p>
                </div>
                <div className='flex flex-wrap gap-2'>
                  <Button
                    disabled={
                      item.archived || toggle.isPending || !item.target_kind
                    }
                    variant='outline'
                    onClick={() => setEdit(item)}
                  >
                    {t('Edit')}
                  </Button>
                  <Button
                    disabled={
                      item.archived ||
                      toggle.isPending ||
                      (props.allowlist &&
                        item.target_kind === 'account_pool_group')
                    }
                    variant='outline'
                    onClick={() =>
                      setConfirmation({
                        id: item.id,
                        action: 'published',
                        value: !item.published,
                      })
                    }
                  >
                    {t(item.published ? 'Unpublish' : 'Publish')}
                  </Button>
                  <Button
                    disabled={
                      item.archived ||
                      toggle.isPending ||
                      (!props.allowlist && !item.published) ||
                      item.target_kind === 'account_pool_group'
                    }
                    variant='outline'
                    onClick={() =>
                      setConfirmation({
                        id: item.id,
                        action: 'enabled',
                        value: !item.enabled,
                      })
                    }
                  >
                    {t(item.enabled ? 'Disable' : 'Enable')}
                  </Button>
                  <Button
                    disabled={
                      props.allowlist ||
                      item.archived ||
                      !item.enabled ||
                      !item.published ||
                      probe.isPending
                    }
                    variant='outline'
                    onClick={() =>
                      setConfirmation({
                        id: item.id,
                        action: 'probe',
                        value: true,
                      })
                    }
                  >
                    {t('Test')}
                  </Button>
                  {!props.allowlist && (
                    <Button
                      disabled={item.archived || toggle.isPending}
                      variant='outline'
                      onClick={() =>
                        setConfirmation({
                          id: item.id,
                          action: 'archived',
                          value: true,
                        })
                      }
                    >
                      {t('Archive monitor')}
                    </Button>
                  )}
                </div>
              </article>
            ))}
          </div>
          <Dialog
            open={Boolean(edit)}
            onOpenChange={(open) => {
              if (!open) setEdit(null)
            }}
            title={t('Edit')}
            description={t(
              'Monitoring requires an upstream cost contract and an enabled budget policy.'
            )}
          >
            {edit && (
              <MonitorAdminForm
                key={edit.id}
                targets={targets.data}
                monitor={edit}
                allowlist={props.allowlist}
                onSaved={() => setEdit(null)}
              />
            )}
          </Dialog>
        </>
      )}
      <ConfirmDialog
        open={Boolean(confirmation)}
        onOpenChange={(open) => {
          if (!open) setConfirmation(null)
        }}
        title={t('Confirm')}
        desc={t(
          'Monitoring requires an upstream cost contract and an enabled budget policy.'
        )}
        isLoading={toggle.isPending || probe.isPending}
        handleConfirm={() =>
          confirmation?.action === 'probe'
            ? probe.mutate(confirmation.id)
            : toggle.mutate()
        }
      >
        {(toggle.isError || probe.isError) && <p role='alert'>{t('Error')}</p>}
      </ConfirmDialog>
      {probeResult && (
        <p role='status'>
          {t('Status')}: {probeResult.status} {probeResult.error_code}
        </p>
      )}
    </section>
  )
}
