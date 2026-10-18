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

import { getImageSourcePolicies, saveImageSourcePolicy } from '../api'
import { POOL_QUERY_KEY } from '../constants'
import type { ImageSourcePolicy, ImageSourceStrategy } from '../types'

const strategies: ImageSourceStrategy[] = [
  'pool_first',
  'api_first',
  'pool_only',
  'api_only',
]

const strategyLabels: Record<ImageSourceStrategy, string> = {
  pool_first: 'Account pool first, then upstream API',
  api_first: 'Upstream API first, then account pool',
  pool_only: 'Account pool only',
  api_only: 'Upstream API only',
}

export function ImageSourcePolicyPanel() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [model, setModel] = useState('')
  const [strategy, setStrategy] = useState<ImageSourceStrategy>('pool_first')
  const [enabled, setEnabled] = useState(true)
  const [expectedVersion, setExpectedVersion] = useState(0)

  const query = useQuery({
    queryKey: [...POOL_QUERY_KEY, 'image-policies'],
    queryFn: ({ signal }) => getImageSourcePolicies(signal),
    staleTime: 15_000,
    refetchOnWindowFocus: false,
  })
  const current = query.data?.find((item) => item.model === model.trim())
  const stale = Boolean(current && current.version !== expectedVersion)

  const mutation = useMutation({
    mutationFn: () =>
      saveImageSourcePolicy({
        model: model.trim(),
        strategy,
        enabled,
        expected_version: expectedVersion,
      }),
    onSuccess: (policy) => {
      if (policy) {
        setExpectedVersion(policy.version)
        setStrategy(policy.strategy)
        setEnabled(policy.enabled)
      }
      toast.success(t('Image source policy saved'))
      void client.invalidateQueries({
        queryKey: [...POOL_QUERY_KEY, 'image-policies'],
      })
    },
    onError: (error) => {
      handleServerError(error)
      // A version conflict means the stored revision moved on; reload it so the
      // next save carries the current revision instead of losing again.
      void client.invalidateQueries({
        queryKey: [...POOL_QUERY_KEY, 'image-policies'],
      })
    },
  })

  const editPolicy = (policy: ImageSourcePolicy) => {
    setModel(policy.model)
    setStrategy(policy.strategy)
    setEnabled(policy.enabled)
    setExpectedVersion(policy.version)
  }

  return (
    <section
      className='space-y-4 rounded-lg border p-4'
      aria-labelledby='pool-image-source-title'
    >
      <h2 id='pool-image-source-title' className='font-semibold'>
        {t('Image generation source')}
      </h2>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Only the site owner changes this. Disabling a model returns it to the existing image routing instead of using the controlled path.'
        )}
      </p>
      <div className='grid gap-4 sm:grid-cols-2'>
        <div className='space-y-2'>
          <Label htmlFor='pool-image-model'>{t('Image model')}</Label>
          <Input
            id='pool-image-model'
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
          <Label htmlFor='pool-image-strategy'>{t('Source priority')}</Label>
          <Select
            value={strategy}
            onValueChange={(value) => {
              if (strategies.includes(value as ImageSourceStrategy)) {
                setStrategy(value as ImageSourceStrategy)
              }
            }}
          >
            <SelectTrigger
              id='pool-image-strategy'
              className='min-h-11 w-full'
            >
              <SelectValue>{t(strategyLabels[strategy])}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              {strategies.map((item) => (
                <SelectItem key={item} value={item}>
                  {t(strategyLabels[item])}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>
      <div className='flex min-h-11 items-center gap-3'>
        <Switch
          id='pool-image-enabled'
          checked={enabled}
          onCheckedChange={setEnabled}
          disabled={mutation.isPending}
        />
        <Label htmlFor='pool-image-enabled'>
          {t('Use the controlled image path for this model')}
        </Label>
      </div>
      {!enabled ? (
        <p role='status' className='text-sm'>
          {t(
            'Disabled: image requests for this model keep the existing routing and never use the controlled path.'
          )}
        </p>
      ) : null}
      {query.isError ? (
        <p role='alert'>{t('Image source policy query failed.')}</p>
      ) : null}
      {stale ? (
        <p role='status'>
          {t(
            'The stored policy changed. Select Load to edit the current revision.'
          )}
        </p>
      ) : null}
      <Button
        className='min-h-11'
        disabled={
          !model.trim() ||
          query.isPending ||
          query.isError ||
          stale ||
          mutation.isPending
        }
        onClick={() => mutation.mutate()}
      >
        {t('Save image source policy')}
      </Button>
      <ul className='space-y-2'>
        {query.data?.map((policy) => (
          <li
            key={policy.model}
            className='flex flex-wrap items-center justify-between gap-2 rounded-md border p-3'
          >
            <span className='min-w-0 break-all'>
              {policy.model} · {t(strategyLabels[policy.strategy])} ·{' '}
              {policy.enabled ? t('Controlled') : t('Disabled')}
            </span>
            <Button
              variant='outline'
              className='min-h-11'
              onClick={() => editPolicy(policy)}
            >
              {t('Load')}
            </Button>
          </li>
        ))}
      </ul>
    </section>
  )
}
