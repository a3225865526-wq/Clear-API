import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { handleServerError } from '@/lib/handle-server-error'

import { getPoolModelRoutes, savePoolModelRoute } from '../api'
import { POOL_QUERY_KEY } from '../constants'
import type { PoolModelRoute } from '../types'

const platforms = [
  { id: 'codex', name: 'ChatGPT / Codex', access: 'oauth_browser' },
  { id: 'claude', name: 'Claude Code', access: 'oauth_browser' },
  {
    id: 'gemini_code_assist',
    name: 'Gemini CLI / Code Assist',
    access: 'oauth_browser',
  },
  { id: 'grok_web_sso', name: 'Grok Web', access: 'cookie_import' },
]

export function SourceRouting() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [model, setModel] = useState('')
  const [platform, setPlatform] = useState('codex')
  const [source, setSource] = useState<'api' | 'pool'>('api')
  const [enabled, setEnabled] = useState(true)
  const [expectedVersion, setExpectedVersion] = useState(0)
  const query = useQuery({
    queryKey: [...POOL_QUERY_KEY, 'model-routes'],
    queryFn: ({ signal }) => getPoolModelRoutes(signal),
    staleTime: 30_000,
    refetchOnWindowFocus: false,
  })
  const current = query.data?.find((route) => route.model === model.trim())
  const mutation = useMutation({
    mutationFn: () =>
      savePoolModelRoute({
        model: model.trim(),
        platform,
        access_type:
          platforms.find((item) => item.id === platform)?.access ?? '',
        source,
        enabled,
        expected_version: expectedVersion,
      }),
    onSuccess: (route) => {
      if (route) setExpectedVersion(route.version)
      toast.success(t('Model source saved'))
      void client.invalidateQueries({
        queryKey: [...POOL_QUERY_KEY, 'model-routes'],
      })
    },
    onError: (error) => {
      handleServerError(error)
      void client.invalidateQueries({
        queryKey: [...POOL_QUERY_KEY, 'model-routes'],
      })
    },
  })
  const editRoute = (route: PoolModelRoute) => {
    setModel(route.model)
    setPlatform(route.platform)
    setSource(route.source)
    setEnabled(route.enabled)
    setExpectedVersion(route.version)
  }

  return (
    <section
      className='space-y-4 rounded-lg border p-4'
      aria-labelledby='pool-source-title'
    >
      <h2 id='pool-source-title' className='font-semibold'>
        {t('Model source control')}
      </h2>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Unconfigured models keep existing channel routing. Configured models never fall back to another source.'
        )}
      </p>
      <div className='grid gap-4 sm:grid-cols-3'>
        <div className='space-y-2'>
          <Label htmlFor='pool-route-model'>{t('External model name')}</Label>
          <Input
            id='pool-route-model'
            className='min-h-11'
            maxLength={128}
            value={model}
            onChange={(event) => {
              setModel(event.target.value)
              setExpectedVersion(0)
            }}
          />
        </div>
        <div className='space-y-2'>
          <Label htmlFor='pool-route-platform'>{t('Pool platform')}</Label>
          <Select
            value={platform}
            disabled={source === 'api'}
            onValueChange={(value) => {
              if (value) setPlatform(value)
            }}
          >
            <SelectTrigger id='pool-route-platform' className='min-h-11 w-full'>
              <SelectValue>
                {platforms.find((item) => item.id === platform)?.name}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {platforms.map((item) => (
                <SelectItem key={item.id} value={item.id}>
                  {item.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className='space-y-2'>
          <Label htmlFor='pool-route-source'>{t('Request source')}</Label>
          <Select
            value={source}
            onValueChange={(value) => {
              if (value !== 'api' && value !== 'pool') return
              setSource(value)
              if (value === 'pool') setEnabled(false)
            }}
          >
            <SelectTrigger id='pool-route-source' className='min-h-11 w-full'>
              <SelectValue>
                {source === 'api' ? t('Upstream API') : t('Account pool')}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='api'>{t('Upstream API')}</SelectItem>
              <SelectItem value='pool'>{t('Account pool')}</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>
      <div className='flex min-h-11 items-center gap-3'>
        <Switch
          id='pool-route-enabled'
          checked={enabled}
          onCheckedChange={setEnabled}
          disabled={source === 'pool' || mutation.isPending}
        />
        <Label htmlFor='pool-route-enabled'>
          {t('Allow external calls for this model')}
        </Label>
      </div>
      {source === 'pool' ? (
        <p role='status' className='text-sm'>
          {t(
            'Pool admission is incomplete. Saving this source pauses the model and never falls back to API.'
          )}
        </p>
      ) : null}
      {query.isError ? (
        <p role='alert'>
          {t('Model source query failed. Refresh before saving.')}
        </p>
      ) : null}
      {current && current.version !== expectedVersion ? (
        <p role='status'>
          {t(
            'A source rule already exists or has changed. Select Edit to load its current revision.'
          )}
        </p>
      ) : null}
      <Button
        className='min-h-11'
        disabled={
          !model.trim() ||
          query.isPending ||
          query.isError ||
          Boolean(current && current.version !== expectedVersion) ||
          mutation.isPending
        }
        onClick={() => mutation.mutate()}
      >
        {t('Save model source')}
      </Button>
      <ul className='space-y-2'>
        {query.data?.map((route) => (
          <li
            key={route.model}
            className='flex flex-wrap items-center justify-between gap-2 rounded-md border p-3'
          >
            <span className='min-w-0 break-all'>
              {route.model} ·{' '}
              {route.source === 'api' ? t('Upstream API') : t('Account pool')} ·{' '}
              {route.enabled ? t('Enabled') : t('Paused')}
            </span>
            <Button
              variant='outline'
              className='min-h-11'
              onClick={() => editRoute(route)}
            >
              {t('Edit')}
            </Button>
          </li>
        ))}
      </ul>
    </section>
  )
}
