import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { handleServerError } from '@/lib/handle-server-error'

import {
  getPoolChannels,
  getPoolMaster,
  setChannelPoolRouting,
  setPoolMaster,
} from '../api'
import { POOL_QUERY_KEY } from '../constants'

// STEP 4 operator release controls. Capability (what an account can serve) and
// Published (what users may call) are deliberately different states here: a
// published model with no capable account shows an explicit empty hint, and a
// capable account never publishes anything by itself.
export function ReleaseControls() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [confirmOpen, setConfirmOpen] = useState(false)
  const masterQuery = useQuery({
    queryKey: [...POOL_QUERY_KEY, 'master'],
    queryFn: ({ signal }) => getPoolMaster(signal),
    staleTime: 15_000,
    refetchOnWindowFocus: false,
  })
  const channelsQuery = useQuery({
    queryKey: [...POOL_QUERY_KEY, 'channels'],
    queryFn: ({ signal }) => getPoolChannels(signal),
    staleTime: 15_000,
    refetchOnWindowFocus: false,
  })
  const invalidate = () => {
    void client.invalidateQueries({ queryKey: [...POOL_QUERY_KEY, 'master'] })
    void client.invalidateQueries({ queryKey: [...POOL_QUERY_KEY, 'channels'] })
  }
  const masterMutation = useMutation({
    mutationFn: (enabled: boolean) => setPoolMaster(enabled),
    onSuccess: () => {
      toast.success(t('Pool master switch updated'))
      invalidate()
    },
    onError: (error) => {
      handleServerError(error)
      invalidate()
    },
  })
  const routingMutation = useMutation({
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) =>
      setChannelPoolRouting(id, enabled),
    onSuccess: () => {
      toast.success(t('Channel pool routing updated'))
      invalidate()
    },
    onError: (error) => {
      handleServerError(error)
      invalidate()
    },
  })
  const masterEnabled = masterQuery.data?.enabled === true
  const channels = channelsQuery.data ?? []
  const poolChannelCount = channels.length

  return (
    <section
      className='space-y-4 rounded-lg border p-4'
      aria-labelledby='pool-release-title'
    >
      <h2 id='pool-release-title' className='font-semibold'>
        {t('Pool release controls')}
      </h2>
      <div className='flex min-h-11 items-center gap-3'>
        <Switch
          id='pool-master-switch'
          checked={masterEnabled}
          disabled={masterQuery.isPending || masterMutation.isPending}
          onCheckedChange={(checked) => {
            if (checked) {
              setConfirmOpen(true)
            } else {
              masterMutation.mutate(false)
            }
          }}
        />
        <Label htmlFor='pool-master-switch'>
          {masterEnabled ? t('Pool is open') : t('Pool is closed')}
        </Label>
        {masterQuery.isError ? (
          <span role='alert' className='text-destructive text-sm'>
            {t('Pool master switch query failed. Refresh before saving.')}
          </span>
        ) : null}
      </div>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Closing the switch stops new pool requests. In-flight requests finish their normal lifecycle; trust and capabilities are untouched.'
        )}
      </p>
      <div className='space-y-2'>
        <h3 className='text-sm font-medium'>{t('Channel pool routing')}</h3>
        {channelsQuery.isError ? (
          <p role='alert' className='text-destructive text-sm'>
            {t('Channel pool status query failed.')}
          </p>
        ) : null}
        {poolChannelCount === 0 && !channelsQuery.isError ? (
          <p className='text-muted-foreground text-sm'>
            {t('No pool-bound channels.')}
          </p>
        ) : null}
        <ul className='space-y-2'>
          {channels.map((channel) => (
            <li
              key={channel.id}
              className='flex flex-wrap items-center justify-between gap-2 rounded-md border p-3'
            >
              <div className='min-w-0'>
                <div className='flex flex-wrap items-center gap-2'>
                  <span className='font-medium'>
                    {channel.name} (#{channel.id})
                  </span>
                  <Badge
                    variant={
                      channel.pool_routing_enabled ? 'default' : 'secondary'
                    }
                  >
                    {channel.pool_routing_enabled
                      ? t('Pool routing on')
                      : t('Pool routing off')}
                  </Badge>
                </div>
                <div className='text-muted-foreground mt-1 text-sm break-all'>
                  {t('Channel models')}: {channel.models.join(', ') || '—'}
                  <br />
                  {t('Published pool models')}:{' '}
                  {channel.published_models.join(', ') || t('None')}
                </div>
              </div>
              <Switch
                checked={channel.pool_routing_enabled}
                disabled={routingMutation.isPending}
                aria-label={t('Pool routing for channel', {
                  name: channel.name,
                })}
                onCheckedChange={(checked) =>
                  routingMutation.mutate({ id: channel.id, enabled: checked })
                }
              />
            </li>
          ))}
        </ul>
      </div>
      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Open the account pool?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'Once enabled, only requests that satisfy the channel, model, and capability conditions may enter the account pool. Everything else stays denied.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                setConfirmOpen(false)
                masterMutation.mutate(true)
              }}
            >
              {t('Enable pool')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      {masterQuery.isFetching || channelsQuery.isFetching ? (
        <Button variant='ghost' size='sm' disabled>
          {t('Refreshing…')}
        </Button>
      ) : null}
    </section>
  )
}
