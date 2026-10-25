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
import { RefreshCw } from 'lucide-react'
import { lazy, Suspense, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout/components/section-page-layout'
import { LoadingState } from '@/components/loading-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { toIntlLocale } from '@/i18n/languages'
import { useAuthStore } from '@/stores/auth-store'

import { aggregateMonitorStatus, getMonitoring } from './api'
import { ChannelCard } from './channel-card'
import { statusColors } from './constants'

const MonitorAdmin = lazy(() =>
  import('./monitor-admin').then((module) => ({ default: module.MonitorAdmin }))
)

export function ChannelStatusPage() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const time = (value: number) =>
    new Date(value * 1000).toLocaleString(locale, { timeZone: 'Asia/Shanghai' })
  const userId = useAuthStore((state) => state.auth.user?.id)
  const isRoot = useAuthStore((state) => state.auth.user?.role === 100)
  const [manage, setManage] = useState(false)
  const [days, setDays] = useState(7)
  const [now, setNow] = useState(Date.now())
  const query = useQuery({
    queryKey: ['console-monitoring', userId],
    queryFn: ({ signal }) => getMonitoring(signal),
    refetchInterval: 60000,
    refetchIntervalInBackground: false,
    retry: false,
  })
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [])
  const data = query.data
  const lastAttempt = Math.max(query.dataUpdatedAt, query.errorUpdatedAt)
  const seconds = Math.min(
    60,
    Math.max(0, Math.ceil((lastAttempt + 60000 - now) / 1000))
  )
  const stale = Boolean(
    query.isError ||
    (data && now / 1000 - data.read_at > data.stale_after_seconds)
  )
  const overall = stale
    ? 'unknown'
    : aggregateMonitorStatus(
        data?.channels.flatMap((channel) =>
          channel.models.map((item) => item.status)
        ) ?? []
      )
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('console.channelStatus')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='space-y-6 pb-6'>
          {isRoot && (
            <Button
              variant='outline'
              aria-expanded={manage}
              onClick={() => setManage(!manage)}
            >
              {t('Monitor administration')}
            </Button>
          )}
          {isRoot && manage && (
            <Suspense fallback={<LoadingState />}>
              <MonitorAdmin />
            </Suspense>
          )}
          <div className='flex flex-wrap items-center justify-end gap-3'>
            <Tabs
              value={String(days)}
              onValueChange={(value) => setDays(Number(value))}
            >
              <TabsList>
                {[7, 15, 30].map((value) => (
                  <TabsTrigger key={value} value={String(value)}>
                    {t('monitor.days', { count: value })}
                  </TabsTrigger>
                ))}
              </TabsList>
            </Tabs>
            <Badge className={statusColors[overall]}>
              {t('monitor.overall')} · {t(`monitor.${overall}`)}
            </Badge>
            <Button
              variant='outline'
              disabled={query.isFetching}
              onClick={() => void query.refetch()}
            >
              <RefreshCw
                className={
                  query.isFetching
                    ? 'animate-spin motion-reduce:animate-none'
                    : ''
                }
                aria-hidden='true'
              />
              {t('console.refresh')}
            </Button>
            <span className='text-muted-foreground text-sm'>
              {query.isFetching
                ? t('console.refreshing')
                : t('monitor.autoRefresh', { seconds })}
            </span>
          </div>
          {data?.schedule && (
            <div className='space-y-1 rounded-xl border p-4 text-sm'>
              <p>
                {t('Daily monitoring schedule')} ·{' '}
                {t('Every 24 hours after the previous task finishes')} ·
                Asia/Shanghai
              </p>
              <p>
                {t(
                  data.schedule.execution_enabled
                    ? 'Monitoring execution enabled'
                    : 'Monitoring execution paused; daily observations still record skipped targets'
                )}
              </p>
              <p>
                {t('Last scheduler run')}:{' '}
                {data.schedule.last_run_at
                  ? time(data.schedule.last_run_at)
                  : t('monitor.notChecked')}
              </p>
              <p>
                {t('Next scheduler run')}:{' '}
                {data.schedule.next_run_at
                  ? time(data.schedule.next_run_at)
                  : t('Running')}
              </p>
            </div>
          )}
          {query.isPending && <LoadingState />}
          {query.isError && (
            <ErrorState
              title={t('console.loadFailed')}
              description={t('console.retainingData')}
              onRetry={() => void query.refetch()}
            />
          )}
          {data?.channels.length === 0 && (
            <EmptyState
              title={t('monitor.noRecords')}
              description={
                data.ready
                  ? t('monitor.noPublishedChannels')
                  : t('monitor.notInitialized')
              }
            />
          )}
          <div className='grid min-w-0 gap-6 md:grid-cols-2 xl:grid-cols-3'>
            {data?.channels.map((channel) => (
              <ChannelCard
                key={channel.id}
                channel={channel}
                days={days}
                stale={stale}
              />
            ))}
          </div>
          <div className='text-muted-foreground space-y-2 rounded-xl border p-4 text-xs'>
            <p>{t('monitor.rules')}</p>
            <p>{t('monitor.pingDefinition')}</p>
            <p>{t('monitor.coverageRule')}</p>
            <p>
              {t(
                'Stored monitoring results expire according to target validity; refresh does not run probes'
              )}
            </p>
          </div>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
