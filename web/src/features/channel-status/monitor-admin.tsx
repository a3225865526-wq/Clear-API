import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import type { Monitor, MonitorTargets } from './lib/monitor-admin-schema'
import { MonitorAdminForm } from './monitor-admin-form'

export function MonitorAdmin() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [edit, setEdit] = useState<Monitor | null>(null)
  const [confirmation, setConfirmation] = useState<{
    id: number
    action: string
    value: boolean
  } | null>(null)
  const targets = useQuery({
    queryKey: ['monitor-admin-targets'],
    queryFn: async () =>
      requireServerSuccess(
        (
          await api.get<{ success: boolean; data: MonitorTargets }>(
            '/api/console/monitor-admin/targets'
          )
        ).data
      ).data,
    retry: false,
  })
  const catalog = useQuery({
    queryKey: ['monitor-admin-catalog'],
    queryFn: async () =>
      requireServerSuccess(
        (
          await api.get<{ success: boolean; data: { monitors: Monitor[] } }>(
            '/api/console/monitor-admin'
          )
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
            `/api/console/monitor-admin/${confirmation.id}/switch/${confirmation.action}`,
            { value: confirmation.value, confirm: true },
            { headers: { 'X-ClearAPI-Monitor': '1' } }
          )
        ).data
      )
    },
    onSuccess: () => {
      setConfirmation(null)
      void client.invalidateQueries({ queryKey: ['monitor-admin-catalog'] })
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
          'Probe transport integration is pending. Configuration and publication do not start detection.'
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
          <MonitorAdminForm targets={targets.data} />
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
                    disabled={item.archived || toggle.isPending}
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
                      item.archived || toggle.isPending || !item.enabled
                    }
                    variant='outline'
                    onClick={() =>
                      setConfirmation({
                        id: item.id,
                        action: 'enabled',
                        value: false,
                      })
                    }
                  >
                    {t('Pause')}
                  </Button>
                  <Button disabled variant='outline'>
                    {t('Test')}
                  </Button>
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
              'Probe transport integration is pending. Configuration and publication do not start detection.'
            )}
          >
            {edit && (
              <MonitorAdminForm
                key={edit.id}
                targets={targets.data}
                monitor={edit}
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
          'Probe transport integration is pending. Configuration and publication do not start detection.'
        )}
        isLoading={toggle.isPending}
        handleConfirm={() => toggle.mutate()}
      >
        {toggle.isError && <p role='alert'>{t('Error')}</p>}
      </ConfirmDialog>
    </section>
  )
}
