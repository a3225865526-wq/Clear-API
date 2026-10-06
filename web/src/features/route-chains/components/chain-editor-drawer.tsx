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
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { ArrowDown, ArrowUp, Loader2, Trash2 } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useFieldArray, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  chainEditorFormSchema,
  type ChainEditorFormValues,
} from '../lib/chain-editor-schema'
import {
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
  sideDrawerSectionClassName,
} from '@/components/drawer-layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Switch } from '@/components/ui/switch'

import { getRouteChainTargets } from '../api'
import { useSaveChain } from '../hooks/use-chain-mutations'
import {
  buildPolicyJson,
  candidateToFormValues,
  filterTargetsByType,
  hasMixedCandidateTypes,
} from '../lib/chain-policy'
import { ROUTE_CHAIN_TARGETS_QUERY_KEY } from '../lib/query-keys'
import type { ChainDetail, TargetInfo } from '../types'

// Chain editor (§六–§十四). Targets are picked from the server catalog —
// operators never type raw ids; the pool/API filter here is UX only and the
// backend re-validates authoritatively (§八). Priorities follow list order:
// the first candidate has the highest priority (§十八).


interface ChainEditorDrawerProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  chain: ChainDetail | null
  maxCandidates: number
}

type CandidateFormValues = ChainEditorFormValues['candidates'][number]

function emptyCandidate(): CandidateFormValues {
  return {
    candidate_type: 'api_channel' as const,
    target_id: 0,
    enabled: true,
    failure_threshold: '',
    cooldown_seconds: '',
    failover_on_429: false,
    failover_on_404: true,
    connect_timeout_ms: '',
  }
}

function chainToFormValues(chain: ChainDetail | null): ChainEditorFormValues {
  if (!chain) {
    return {
      model: '',
      group: '',
      enabled: false,
      allow_cross_source: false,
      candidates: [emptyCandidate()],
    }
  }
  return {
    model: chain.model,
    group: chain.group,
    enabled: chain.enabled,
    allow_cross_source: chain.allow_cross_source,
    candidates: chain.candidates.map((candidate) => ({
      ...candidateToFormValues(candidate),
      failure_threshold:
        candidateToFormValues(candidate).failure_threshold ?? '',
      cooldown_seconds: candidateToFormValues(candidate).cooldown_seconds ?? '',
      connect_timeout_ms:
        candidateToFormValues(candidate).connect_timeout_ms ?? '',
    })),
  }
}

export function ChainEditorDrawer(props: ChainEditorDrawerProps) {
  const { open, onOpenChange, chain, maxCandidates } = props
  const { t } = useTranslation()
  const isEdit = Boolean(chain)

  const targetsQuery = useQuery({
    queryKey: ROUTE_CHAIN_TARGETS_QUERY_KEY,
    queryFn: ({ signal }) => getRouteChainTargets(signal),
    enabled: open,
  })
  const targets: TargetInfo[] = targetsQuery.data ?? []

  const { create, update } = useSaveChain(() => onOpenChange(false))

  const form = useForm<ChainEditorFormValues>({
    resolver: zodResolver(chainEditorFormSchema),
    defaultValues: chainToFormValues(chain),
  })
  const candidatesArray = useFieldArray({
    control: form.control,
    name: 'candidates',
  })
  const allowCrossSource = form.watch('allow_cross_source')
  const candidateValues = form.watch('candidates')
  const mixedTypes = useMemo(
    () => hasMixedCandidateTypes(candidateValues ?? []),
    [candidateValues]
  )

  // Reset only when the drawer opens for a (new) chain id — detail polling
  // replaces the chain object every few seconds and must not wipe edits.
  const [openedChainId, setOpenedChainId] = useState<number | 'new' | null>(null)
  useEffect(() => {
    const currentId = chain?.id ?? 'new'
    if (open && openedChainId !== currentId) {
      setOpenedChainId(currentId)
      form.reset(chainToFormValues(chain))
    }
    if (!open && openedChainId !== null) setOpenedChainId(null)
  }, [open, chain, openedChainId, form])

  const onSubmit = (values: ChainEditorFormValues) => {
    const candidates = values.candidates.map((candidate, index) => ({
      id: candidate.id,
      candidate_type: candidate.candidate_type,
      target_id: candidate.target_id,
      enabled: candidate.enabled,
      // List order is the priority: first element highest (§十八).
      priority: values.candidates.length - index,
      policy: buildPolicyJson({
        failure_threshold:
          candidate.failure_threshold === '' ? undefined : candidate.failure_threshold,
        cooldown_seconds:
          candidate.cooldown_seconds === '' ? undefined : candidate.cooldown_seconds,
        failover_on_429: candidate.failover_on_429,
        failover_on_404: candidate.failover_on_404,
        connect_timeout_ms:
          candidate.connect_timeout_ms === '' ? undefined : candidate.connect_timeout_ms,
      }),
    }))
    if (isEdit && chain) {
      update.mutate({
        id: chain.id,
        model: values.model,
        group: values.group,
        enabled: values.enabled,
        allow_cross_source: values.allow_cross_source,
        candidates,
        expected_version: chain.version,
      })
    } else {
      create.mutate({
        model: values.model,
        group: values.group,
        enabled: values.enabled,
        allow_cross_source: values.allow_cross_source,
        candidates,
      })
    }
  }

  const pending = create.isPending || update.isPending

  const moveCandidate = (index: number, direction: -1 | 1) => {
    const target = index + direction
    if (target < 0 || target >= candidatesArray.fields.length) return
    candidatesArray.swap(index, target)
  }

  const targetsForType = (type: 'pool' | 'api_channel') =>
    filterTargetsByType(targets, type)

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className={sideDrawerContentClassName('sm:max-w-2xl')}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>
            {isEdit ? t('Edit Route Chain') : t('Create Route Chain')}
          </SheetTitle>
        </SheetHeader>

        <Form {...form}>
          <form
            onSubmit={form.handleSubmit(onSubmit)}
            className={sideDrawerFormClassName()}
          >
            <div className={sideDrawerSectionClassName()}>
              <FormField
                control={form.control}
                name='model'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Model')}</FormLabel>
                    <FormControl>
                      <Input
                        {...field}
                        disabled={isEdit}
                        placeholder='gpt-4o'
                      />
                    </FormControl>
                    <FormDescription>
                      {isEdit
                        ? t('Model and group are locked after creation.')
                        : t('The canonical model this chain routes for.')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='group'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Group')}</FormLabel>
                    <FormControl>
                      <Input {...field} disabled={isEdit} placeholder='default' />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='enabled'
                render={({ field }) => (
                  <FormItem className='flex flex-row items-center justify-between gap-3'>
                    <FormLabel>{t('Chain Enabled')}</FormLabel>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='allow_cross_source'
                render={({ field }) => (
                  <FormItem>
                    <div className='flex flex-row items-center justify-between gap-3'>
                      <FormLabel>{t('Allow cross-source failover')}</FormLabel>
                      <FormControl>
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                        />
                      </FormControl>
                    </div>
                    <FormDescription>
                      {t(
                        'Permits failing over between pool and API channel candidates.'
                      )}
                    </FormDescription>
                    {mixedTypes && allowCrossSource ? (
                      <Alert className='border-warning/40 bg-warning/10'>
                        <AlertDescription>
                          {t(
                            'Mixed pool and API candidates require cross-source failover — confirm this is intended.'
                          )}
                        </AlertDescription>
                      </Alert>
                    ) : null}
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>

            <div className={sideDrawerSectionClassName()}>
              <div className='flex items-center justify-between'>
                <h3 className='text-sm font-medium'>
                  {t('Candidates')} —{' '}
                  <span className='text-muted-foreground'>
                    {t('order defines priority')}
                  </span>
                </h3>
                <Button
                  type='button'
                  size='sm'
                  variant='outline'
                  disabled={
                    candidatesArray.fields.length >= maxCandidates || pending
                  }
                  onClick={() => candidatesArray.append(emptyCandidate())}
                >
                  {t('Add candidate')}
                </Button>
              </div>

              {candidatesArray.fields.map((field, index) => {
                const type = form.watch(`candidates.${index}.candidate_type`)
                const options = targetsForType(type)
                return (
                  <div
                    key={field.id}
                    className='rounded-lg border p-3'
                  >
                    <div className='mb-2 flex items-center justify-between gap-2'>
                      <span className='text-xs font-medium text-muted-foreground'>
                        {index + 1}. {t('Priority')}{' '}
                        {candidatesArray.fields.length - index}
                      </span>
                      <div className='flex items-center gap-1'>
                        <Button
                          type='button'
                          size='icon-xs'
                          variant='ghost'
                          aria-label={t('Move candidate up')}
                          disabled={index === 0 || pending}
                          onClick={() => moveCandidate(index, -1)}
                        >
                          <ArrowUp />
                        </Button>
                        <Button
                          type='button'
                          size='icon-xs'
                          variant='ghost'
                          aria-label={t('Move candidate down')}
                          disabled={
                            index === candidatesArray.fields.length - 1 || pending
                          }
                          onClick={() => moveCandidate(index, 1)}
                        >
                          <ArrowDown />
                        </Button>
                        <Button
                          type='button'
                          size='icon-xs'
                          variant='ghost'
                          disabled={candidatesArray.fields.length <= 1 || pending}
                          aria-label={t('Remove candidate')}
                          onClick={() => candidatesArray.remove(index)}
                        >
                          <Trash2 />
                        </Button>
                      </div>
                    </div>

                    <div className='grid gap-3 sm:grid-cols-2'>
                      <FormField
                        control={form.control}
                        name={`candidates.${index}.candidate_type`}
                        render={({ field: typeField }) => (
                          <FormItem>
                            <FormLabel>{t('Type')}</FormLabel>
                            <Select
                              value={typeField.value}
                              onValueChange={(value) => {
                                typeField.onChange(value)
                                form.setValue(`candidates.${index}.target_id`, 0)
                              }}
                            >
                              <FormControl>
                                <SelectTrigger>
                                  <SelectValue />
                                </SelectTrigger>
                              </FormControl>
                              <SelectContent>
                                <SelectItem value='api_channel'>
                                  {t('API Channel')}
                                </SelectItem>
                                <SelectItem value='pool'>{t('Pool')}</SelectItem>
                              </SelectContent>
                            </Select>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                      <FormField
                        control={form.control}
                        name={`candidates.${index}.target_id`}
                        render={({ field: targetField }) => (
                          <FormItem>
                            <FormLabel>{t('Target Channel')}</FormLabel>
                            <Select
                              value={targetField.value ? String(targetField.value) : ''}
                              onValueChange={(value) =>
                                targetField.onChange(Number(value))
                              }
                            >
                              <FormControl>
                                <SelectTrigger>
                                  <SelectValue
                                    placeholder={t('Pick a target channel.')}
                                  />
                                </SelectTrigger>
                              </FormControl>
                              <SelectContent>
                                {options.map((target) => (
                                  <SelectItem key={target.id} value={String(target.id)}>
                                    {target.name} · {t(target.source === 'pool' ? 'Pool' : 'API')}{' '}
                                    · {target.enabled ? t('Enabled') : t('Disabled')}
                                  </SelectItem>
                                ))}
                              </SelectContent>
                            </Select>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                    </div>

                    <div className='mt-3 grid gap-3 sm:grid-cols-2'>
                      <FormField
                        control={form.control}
                        name={`candidates.${index}.failure_threshold`}
                        render={({ field: thresholdField }) => (
                          <FormItem>
                            <FormLabel>
                              {t('Failure threshold')} —{' '}
                              {t('Default (3)')}
                            </FormLabel>
                            <FormControl>
                              <Input
                                type='number'
                                min={1}
                                max={20}
                                placeholder={t('Default (3)')}
                                {...thresholdField}
                                value={
                                  thresholdField.value === 0 ||
                                  thresholdField.value === ''
                                    ? ''
                                    : String(thresholdField.value)
                                }
                                onChange={(event) =>
                                  thresholdField.onChange(
                                    event.target.value === ''
                                      ? ''
                                      : Number(event.target.value)
                                  )
                                }
                              />
                            </FormControl>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                      <FormField
                        control={form.control}
                        name={`candidates.${index}.cooldown_seconds`}
                        render={({ field: cooldownField }) => (
                          <FormItem>
                            <FormLabel>
                              {t('Circuit open duration')} —{' '}
                              {t('Default (60)')}
                            </FormLabel>
                            <FormControl>
                              <Input
                                type='number'
                                min={1}
                                max={3600}
                                placeholder={t('Default (60)')}
                                {...cooldownField}
                                value={
                                  cooldownField.value === 0 ||
                                  cooldownField.value === ''
                                    ? ''
                                    : String(cooldownField.value)
                                }
                                onChange={(event) =>
                                  cooldownField.onChange(
                                    event.target.value === ''
                                      ? ''
                                      : Number(event.target.value)
                                  )
                                }
                              />
                            </FormControl>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                    </div>

                    <div className='mt-3 flex flex-wrap items-center gap-4'>
                      <FormField
                        control={form.control}
                        name={`candidates.${index}.enabled`}
                        render={({ field: enabledField }) => (
                          <FormItem className='flex flex-row items-center gap-2'>
                            <FormLabel>{t('Enabled')}</FormLabel>
                            <FormControl>
                              <Switch
                                checked={enabledField.value}
                                onCheckedChange={enabledField.onChange}
                              />
                            </FormControl>
                          </FormItem>
                        )}
                      />
                      <FormField
                        control={form.control}
                        name={`candidates.${index}.failover_on_429`}
                        render={({ field: f429Field }) => (
                          <FormItem className='flex flex-row items-center gap-2'>
                            <FormLabel>{t('Failover on 429 (this request)')}</FormLabel>
                            <FormControl>
                              <Switch
                                checked={Boolean(f429Field.value)}
                                onCheckedChange={f429Field.onChange}
                              />
                            </FormControl>
                          </FormItem>
                        )}
                      />
                      <FormField
                        control={form.control}
                        name={`candidates.${index}.failover_on_404`}
                        render={({ field: f404Field }) => (
                          <FormItem className='flex flex-row items-center gap-2'>
                            <FormLabel>{t('Failover on 404')}</FormLabel>
                            <FormControl>
                              <Switch
                                checked={f404Field.value !== false}
                                onCheckedChange={(checked) =>
                                  f404Field.onChange(checked)
                                }
                              />
                            </FormControl>
                          </FormItem>
                        )}
                      />
                    </div>
                  </div>
                )
              })}
            </div>
          </form>
        </Form>

        <div className={sideDrawerFooterClassName()}>
          <Button
            type='button'
            variant='outline'
            disabled={pending}
            onClick={() => onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            disabled={pending}
            onClick={() => void form.handleSubmit(onSubmit)()}
          >
            {pending ? <Loader2 className='animate-spin' /> : null}
            {isEdit ? t('Save changes') : t('Create')}
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  )
}
