import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

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
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import {
  monitorSchema,
  numericFields,
  type Monitor,
  type MonitorInput,
  type MonitorTargets,
  type MonitorValues,
} from './lib/monitor-admin-schema'

export function MonitorAdminForm(props: {
  targets: MonitorTargets
  monitor?: Monitor
  onSaved?: () => void
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [target, setTarget] = useState(
    props.monitor
      ? `${props.monitor.target_kind}:${props.monitor.target_ref}`
      : ''
  )
  const [model, setModel] = useState(props.monitor?.model ?? '')
  const [protocol, setProtocol] = useState(
    props.monitor?.protocol ?? 'openai_chat'
  )
  const form = useForm<MonitorInput, unknown, MonitorValues>({
    resolver: zodResolver(monitorSchema),
    mode: 'onBlur',
    defaultValues: props.monitor ?? {
      display_name: '',
      interval_seconds: 600,
      timeout_seconds: 30,
      stale_seconds: 900,
      daily_requests: 0,
      request_cost_micros: 0,
    },
  })
  const save = useMutation({
    mutationFn: async (values: MonitorValues) => {
      const separator = target.indexOf(':')
      const kind = target.slice(0, separator)
      const ref = target.slice(separator + 1)
      const input = {
        ...values,
        id: props.monitor?.id ?? 0,
        target_kind: kind,
        target_ref: ref,
        channel_id: kind === 'channel' ? Number(ref) : 0,
        model,
        protocol,
        platform: props.monitor?.platform ?? '',
        enabled: false,
        published: false,
        archived: false,
      }
      const options = { headers: { 'X-ClearAPI-Monitor': '1' } }
      const response = props.monitor
        ? await api.put('/api/console/monitor-admin', input, options)
        : await api.post('/api/console/monitor-admin', input, options)
      return requireServerSuccess(response.data)
    },
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ['monitor-admin-catalog'] })
      void client.invalidateQueries({ queryKey: ['console-monitoring'] })
      props.onSaved?.()
    },
  })
  const groupNames = [
    ...new Set(props.targets.groups.map((item) => item.group)),
  ]
  let models: string[] = []
  if (target.startsWith('channel:')) {
    models =
      props.targets.channels
        .find((item) => item.id === Number(target.slice(8)))
        ?.models.split(',')
        .map((item) => item.trim())
        .filter(Boolean) ?? []
  }
  if (target.startsWith('group_route:')) {
    models = props.targets.groups
      .filter((item) => item.group === target.slice(12))
      .map((item) => item.model)
  }
  return (
    <form onSubmit={form.handleSubmit((values) => save.mutate(values))}>
      <fieldset disabled={save.isPending} className='grid gap-4 sm:grid-cols-2'>
        {props.monitor ? (
          <p className='break-words sm:col-span-2'>
            {props.monitor.target_kind} · {props.monitor.target_ref} · {model} ·{' '}
            {protocol}
          </p>
        ) : (
          <>
            <div className='space-y-2'>
              <Label id='monitor-target-label'>{t('Channel')}</Label>
              <Select
                value={target}
                onValueChange={(value) => {
                  setTarget(value ?? '')
                  setModel('')
                }}
              >
                <SelectTrigger
                  aria-labelledby='monitor-target-label'
                  className='w-full'
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {props.targets.channels.map((item) => (
                    <SelectItem
                      key={item.id}
                      value={`channel:${item.id}`}
                      disabled={item.status !== 1}
                    >
                      {item.name} ·{' '}
                      {t(item.status === 1 ? 'Enabled' : 'Disabled')}
                    </SelectItem>
                  ))}
                  {groupNames.map((name) => (
                    <SelectItem key={name} value={`group_route:${name}`}>
                      {name} · group_route
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className='space-y-2'>
              <Label id='monitor-model-label'>{t('Model')}</Label>
              <Select
                value={model}
                onValueChange={(value) => setModel(value ?? '')}
              >
                <SelectTrigger
                  aria-labelledby='monitor-model-label'
                  className='w-full'
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {models.map((name) => (
                    <SelectItem key={name} value={name}>
                      {name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className='space-y-2'>
              <Label id='monitor-protocol-label'>
                {t('Monitoring protocol')}
              </Label>
              <Select
                value={protocol}
                onValueChange={(value) => setProtocol(value ?? 'openai_chat')}
              >
                <SelectTrigger
                  aria-labelledby='monitor-protocol-label'
                  className='w-full'
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='openai_chat'>OpenAI Chat</SelectItem>
                  <SelectItem value='openai_responses'>
                    OpenAI Responses
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>
          </>
        )}
        <div className='space-y-2'>
          <Label htmlFor='monitor-name'>{t('Display Name')}</Label>
          <Input
            id='monitor-name'
            {...form.register('display_name')}
            aria-invalid={Boolean(form.formState.errors.display_name)}
            aria-describedby={
              form.formState.errors.display_name
                ? 'monitor-name-error'
                : undefined
            }
          />
          {form.formState.errors.display_name && (
            <p role='alert' id='monitor-name-error'>
              {t('Invalid value')}
            </p>
          )}
        </div>
        {numericFields.map(([key, label]) => (
          <div key={key} className='space-y-2'>
            <Label htmlFor={`monitor-${key}`}>{t(label)}</Label>
            <Input
              id={`monitor-${key}`}
              type='number'
              {...form.register(key)}
              aria-invalid={Boolean(form.formState.errors[key])}
              aria-describedby={
                form.formState.errors[key] ? `monitor-${key}-error` : undefined
              }
            />
            {form.formState.errors[key] && (
              <p role='alert' id={`monitor-${key}-error`}>
                {t('Invalid value')}
              </p>
            )}
          </div>
        ))}
        <Button type='submit' disabled={!model || !target || save.isPending}>
          {t(props.monitor ? 'Save' : 'Add')}
        </Button>
        {save.isError && <p role='alert'>{t('Error')}</p>}
      </fieldset>
    </form>
  )
}
