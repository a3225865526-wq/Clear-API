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
import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { formatNumber } from '@/lib/format'

import { getPoolAccountCapabilities, getPoolAccountQuota } from '../api'
import { POOL_QUERY_KEY } from '../constants'
import { formatPoolTime } from '../lib/time'
import type { PoolQuota } from '../types'

const sourceLabels = {
  upstream: 'Upstream response',
  upstream_derived: 'Derived from upstream data',
  local_statistics: 'Local statistics',
  local_prediction: 'Local prediction',
  unknown: 'Unknown',
}

const capabilityStatusLabels = {
  supported: 'Supported',
  unsupported: 'Unsupported',
  unverified: 'Unverified',
}
const queryResultLabels = {
  success: 'Successful',
  failed: 'Failed',
  unknown: 'Unknown',
  unsupported: 'Unsupported',
}
const capabilityLabels: Record<string, string> = {
  text_chat: 'Text chat',
  stream_text: 'Streaming text',
  tools: 'Tool calls',
  image_input: 'Image input',
  image_generation: 'Image generation',
  long_context: 'Long context',
  exact_usage: 'Exact token usage',
}
const evidenceLabels: Record<string, string> = {
  source: 'Source confirmed',
  test: 'Directed test passed',
  document: 'Documented',
  runtime: 'Real runtime confirmed',
}
const metricLabels: Record<string, string> = {
  used_percent: 'Used percentage',
  remaining_percent: 'Remaining percentage',
  remaining_requests: 'Remaining requests',
  remaining_tokens: 'Remaining tokens',
  balance: 'Balance',
}

function useQuotaFreshness(quota?: PoolQuota) {
  const [now, setNow] = useState(() => Math.floor(Date.now() / 1000))
  useEffect(() => {
    if (!quota?.metrics.length) return
    const timer = setInterval(
      () => setNow(Math.floor(Date.now() / 1000)),
      60_000
    )
    return () => clearInterval(timer)
  }, [quota?.metrics.length])
  if (!quota) return quota
  const metrics = quota.metrics.map((metric) => ({
    ...metric,
    stale:
      metric.stale ||
      metric.updated_at <= now - 300 ||
      (metric.reset_at !== undefined && metric.reset_at <= now),
  }))
  return {
    ...quota,
    metrics,
    stale: quota.stale || metrics.some((metric) => metric.stale),
  }
}

export function QuotaSummary(props: { quota?: PoolQuota }) {
  const { t } = useTranslation()
  const quota = useQuotaFreshness(props.quota)
  if (!quota || quota.result === 'unknown') {
    return <span>{t('Unknown')}</span>
  }
  if (quota.result === 'unsupported') {
    return <span>{t('Quota query is not supported yet')}</span>
  }
  const remaining = quota.metrics.filter(
    (metric) => metric.kind.startsWith('remaining') || metric.kind === 'balance'
  )
  return (
    <div className='space-y-1 text-xs'>
      {remaining.map((metric) => (
        <div key={`${metric.pool}:${metric.model}:${metric.kind}`}>
          {metric.model || metric.pool}: {formatNumber(metric.value)}{' '}
          {metric.unit}
        </div>
      ))}
      {remaining.length === 0 ? <span>{t('Unknown')}</span> : null}
      {quota.stale ? <Badge variant='outline'>{t('Stale')}</Badge> : null}
      {quota.result === 'failed' ? (
        <span>{t('Quota query failed; previous data is retained')}</span>
      ) : null}
    </div>
  )
}

export function AccountQuotaPanel(props: { accountId: number }) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: [...POOL_QUERY_KEY, 'quota', props.accountId],
    queryFn: ({ signal }) => getPoolAccountQuota(props.accountId, signal),
    staleTime: 300_000,
    retry: false,
    refetchOnWindowFocus: false,
  })
  const observed =
    query.data && query.isError
      ? {
          ...query.data,
          result: 'failed' as const,
          stale: true,
          metrics: query.data.metrics.map((metric) => ({
            ...metric,
            stale: true,
            query_result: 'failed' as const,
          })),
        }
      : query.data
  const data = useQuotaFreshness(observed)
  return (
    <section className='bg-card space-y-3 rounded-xl border p-4'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <h3 className='text-sm font-semibold'>{t('Account quota')}</h3>
        <Button
          variant='outline'
          className='min-h-11'
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          {t('Refresh quota')}
        </Button>
      </div>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Shared quota is shown once. Local counters and cooldown changes do not restore upstream quota.'
        )}
      </p>
      {query.isPending ? <LoadingState message={t('Loading quota')} /> : null}
      {query.isError ? (
        <ErrorState
          title={t('Quota could not be loaded')}
          onRetry={() => void query.refetch()}
        />
      ) : null}
      {data ? (
        <div className='space-y-3'>
          <QuotaSummary quota={data} />
          {data.query_capability === 'observations_only' ? (
            <p className='text-muted-foreground text-sm'>
              {t(
                'Only previous upstream quota observations are available; active upstream quota query is not supported.'
              )}
            </p>
          ) : null}
          <p className='text-muted-foreground text-xs'>
            {t('Last check')}: {formatPoolTime(data.checked_at || null)} ·{' '}
            {t('Next refresh')}: {formatPoolTime(data.next_refresh_at || null)}
          </p>
          <div className='grid gap-3 sm:grid-cols-2'>
            {data.metrics.map((metric) => (
              <div
                key={`${metric.scope}:${metric.pool}:${metric.model}:${metric.kind}`}
                className='min-w-0 space-y-1 rounded-lg border p-3 text-sm'
              >
                <p className='break-words'>
                  {metric.model || metric.pool} ·{' '}
                  {t(metricLabels[metric.kind] || metric.kind)}:{' '}
                  {formatNumber(metric.value)} {metric.unit}
                </p>
                <p>
                  {t('Scope')}:{' '}
                  {metric.scope === 'shared_pool'
                    ? t('Shared pool')
                    : metric.scope}
                </p>
                <p>
                  {t('Window')}:{' '}
                  {metric.window_seconds === undefined
                    ? t('Unknown')
                    : `${formatNumber(metric.window_seconds)} ${t('seconds')}`}
                </p>
                <p>
                  {t('Recovery time')}:{' '}
                  {formatPoolTime(metric.reset_at ?? null)}
                </p>
                <p>
                  {t('Source')}: {t(sourceLabels[metric.source])}
                </p>
                <p>
                  {t('Updated')}: {formatPoolTime(metric.updated_at || null)}
                </p>
                <p>
                  {t('Query result')}:{' '}
                  {t(queryResultLabels[metric.query_result])}
                </p>
                {metric.stale ? (
                  <Badge variant='outline'>{t('Stale')}</Badge>
                ) : null}
              </div>
            ))}
          </div>
        </div>
      ) : null}
    </section>
  )
}

export function AccountModelAdmissionPanel(props: { accountId: number }) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState('')
  const [model, setModel] = useState('')
  const query = useQuery({
    queryKey: [...POOL_QUERY_KEY, 'model-admission', props.accountId, model],
    queryFn: ({ signal }) =>
      getPoolAccountCapabilities(props.accountId, signal, model),
    enabled: model.length > 0,
    retry: false,
    staleTime: 300_000,
    refetchOnWindowFocus: false,
  })
  const admission = query.data?.model_admission
  return (
    <section className='bg-card space-y-3 rounded-xl border p-4'>
      <h3 className='text-sm font-semibold'>
        {t('Model admission and evidence')}
      </h3>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Unverified capabilities are disabled. Account pool channels remain closed until continuous account isolation and exact metering are verified.'
        )}
      </p>
      <form
        className='flex flex-wrap items-end gap-2'
        onSubmit={(event) => {
          event.preventDefault()
          setModel(draft.trim())
        }}
      >
        <div className='min-w-0 flex-1 space-y-1'>
          <Label htmlFor={`pool-model-${props.accountId}`}>
            {t('Exact model identifier')}
          </Label>
          <Input
            id={`pool-model-${props.accountId}`}
            className='min-h-11'
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            maxLength={200}
          />
        </div>
        <Button
          className='min-h-11'
          type='submit'
          disabled={!draft.trim() || query.isFetching}
        >
          {t('Inspect admission')}
        </Button>
      </form>
      {query.isError ? (
        <ErrorState
          title={t('Admission could not be loaded')}
          onRetry={() => void query.refetch()}
        />
      ) : null}
      {admission ? (
        <div className='space-y-3'>
          <p className='text-sm break-all'>
            {admission.platform} · {admission.access_type} · {admission.model} ·{' '}
            {t('Adapter version')} {admission.adapter_version}
          </p>
          <Badge variant='outline'>{t('External channel closed')}</Badge>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Executor usage fields do not establish complete upstream metering provenance.'
            )}
          </p>
          <div className='grid gap-3 sm:grid-cols-2'>
            {admission.capabilities.map((capability) => (
              <div
                key={capability.capability}
                className='min-w-0 rounded-lg border p-3 text-sm'
              >
                <p>
                  {t(
                    capabilityLabels[capability.capability] ||
                      capability.capability
                  )}
                  : {t(capabilityStatusLabels[capability.status])}
                </p>
                <ul className='mt-1 space-y-1 text-xs break-all'>
                  {capability.evidence.map((evidence) => (
                    <li key={`${evidence.kind}:${evidence.reference}`}>
                      {t(evidenceLabels[evidence.kind] || evidence.kind)} ·{' '}
                      {evidence.reference} · {evidence.revision}
                    </li>
                  ))}
                </ul>
                {capability.evidence.length === 0 ? (
                  <p className='text-muted-foreground text-xs'>
                    {t('No model-specific validation evidence')}
                  </p>
                ) : null}
              </div>
            ))}
          </div>
        </div>
      ) : null}
    </section>
  )
}
