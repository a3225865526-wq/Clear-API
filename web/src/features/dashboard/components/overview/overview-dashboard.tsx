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
import { useQuery, keepPreviousData } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import {
  KeyRound,
  BookOpen,
  RefreshCw,
  FileText,
  Gift,
  Wallet,
  Activity,
  Coins,
  Clock,
  Database,
  Layers,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout/components/section-page-layout'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from '@/components/ui/select'
import { toIntlLocale } from '@/i18n/languages'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatNumber } from '@/lib/format'
import { useAuthStore } from '@/stores/auth-store'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { getConsoleDashboard } from './console-api'
import { ConsoleCharts } from './console-charts'

export function OverviewDashboard() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const user = useAuthStore((state) => state.auth.user)
  const brand = useSystemConfigStore((state) => state.config.systemName)
  const [days, setDays] = useState(7)
  const query = useQuery({
    queryKey: ['console-dashboard', user?.id, days],
    queryFn: ({ signal }) => getConsoleDashboard(days, signal),
    staleTime: 60000,
    placeholderData: keepPreviousData,
  })
  const data = query.data
  const number = (value: number | undefined | null) =>
    value == null ? t('console.unknown') : formatNumber(value, locale)
  const money = (value: number | undefined | null) =>
    value == null ? t('console.unknown') : formatQuotaWithCurrency(value)
  const summary = [
    {
      title: 'console.balance',
      value: money(data?.balance),
      detail: t('console.availableBalance'),
      icon: Wallet,
    },
    {
      title: 'console.keys',
      value: number(data?.keys),
      detail: t('console.enabledKeys', { count: data?.enabled_keys ?? '?' }),
      icon: KeyRound,
    },
    {
      title: 'console.todayRequests',
      value: number(data?.today.requests),
      detail: t('console.accountTotal', {
        value: number(data?.account_requests),
      }),
      icon: Activity,
    },
    {
      title: 'console.todayCost',
      value: money(data?.today.quota),
      detail: t('console.accountTotal', { value: money(data?.account_quota) }),
      icon: Coins,
    },
    {
      title: 'console.todayTokens',
      value: data
        ? number(data.today.input + data.today.output)
        : t('console.unknown'),
      detail: t('console.tokenDetail', {
        input: number(data?.today.input),
        output: number(data?.today.output),
      }),
      icon: Layers,
    },
    {
      title: 'console.retainedTokens',
      value: data
        ? number(data.retained.input + data.retained.output)
        : t('console.unknown'),
      detail: t('console.tokenDetail', {
        input: number(data?.retained.input),
        output: number(data?.retained.output),
      }),
      icon: Database,
    },
    {
      title: 'console.performance',
      value: `${number(data?.minute.requests)} RPM`,
      detail: `${number(
        data ? data.minute.input + data.minute.output : undefined
      )} TPM · ${t('console.lastMinute')}`,
      icon: Activity,
    },
    {
      title: 'console.averageResponse',
      value:
        data?.retained.average_seconds == null
          ? t('console.noData')
          : `${number(data.retained.average_seconds)} s`,
      detail: t('console.retainedScope'),
      icon: Clock,
    },
  ]
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('console.dashboard')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='space-y-6 pb-6' aria-busy={query.isFetching}>
          <section className='from-primary/15 via-card to-primary/5 grid gap-8 rounded-3xl border bg-gradient-to-br p-6 md:grid-cols-[1fr_220px] md:p-10'>
            <div className='min-w-0 space-y-5'>
              <p className='text-primary text-sm font-semibold tracking-widest'>
                {brand} CONSOLE
              </p>
              <h1 className='text-3xl font-bold break-words sm:text-4xl'>
                {t('console.welcome', {
                  name: user?.display_name || user?.username,
                })}
              </h1>
              <p className='text-muted-foreground'>
                {t('console.welcomeHint')}
              </p>
              <div className='flex flex-wrap gap-3'>
                <Button
                  render={<Link to='/usage-guide' search={{ article: '/' }} />}
                >
                  <BookOpen aria-hidden='true' />
                  {t('console.viewGuide')}
                </Button>
                <Button variant='outline' render={<Link to='/keys' />}>
                  <KeyRound aria-hidden='true' />
                  {t('console.manageKeys')}
                </Button>
              </div>
            </div>
            <div className='grid gap-3 sm:grid-cols-2 md:grid-cols-1'>
              {[
                {
                  label: 'console.todayRequests',
                  value: number(data?.today.requests),
                },
                { label: 'console.balance', value: money(data?.balance) },
              ].map((item) => (
                <div
                  key={item.label}
                  className='bg-card/70 rounded-2xl border p-5'
                >
                  <p className='text-muted-foreground text-sm'>
                    {t(item.label)}
                  </p>
                  <p className='mt-2 text-2xl font-bold'>{item.value}</p>
                </div>
              ))}
            </div>
          </section>
          {query.isPending && <LoadingState />}
          {query.isError && (
            <ErrorState
              title={t('console.loadFailed')}
              description={t('console.retainingData')}
              onRetry={() => void query.refetch()}
            />
          )}
          <p className='text-muted-foreground text-xs'>
            {t('console.fixedScope')}{' '}
            {data &&
              t('console.updatedAt', {
                time: new Date(data.read_at * 1000).toLocaleString(locale, {
                  timeZone: data.timezone,
                }),
              })}
          </p>
          <div className='grid gap-4 sm:grid-cols-2 xl:grid-cols-4'>
            {summary.map((item) => (
              <Card key={item.title} className='min-w-0 rounded-2xl'>
                <CardContent className='flex gap-4 p-5'>
                  <span className='bg-primary/10 text-primary flex size-11 shrink-0 items-center justify-center rounded-xl'>
                    <item.icon aria-hidden='true' className='size-5' />
                  </span>
                  <div className='min-w-0'>
                    <p className='text-muted-foreground text-sm'>
                      {t(item.title)}
                    </p>
                    <p
                      className='my-1 text-2xl font-semibold break-words'
                      title={item.value}
                    >
                      {item.value}
                    </p>
                    <p className='text-muted-foreground text-xs break-words'>
                      {item.detail}
                    </p>
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
          <Card className='rounded-2xl'>
            <CardHeader>
              <CardTitle className='flex flex-wrap justify-between gap-2'>
                {t('console.platforms')}
                <span className='text-muted-foreground text-sm'>
                  {data?.platforms.length ?? '—'}
                </span>
              </CardTitle>
              <p className='text-muted-foreground text-xs'>
                {t('console.platformScope')}
              </p>
            </CardHeader>
            <CardContent>
              {data?.platforms.length ? (
                <div className='grid gap-4 md:grid-cols-2 xl:grid-cols-3'>
                  {data.platforms.map((item) => (
                    <div
                      key={item.name}
                      className='space-y-2 rounded-xl border p-4'
                    >
                      <h3 className='font-semibold break-words'>
                        {item.name || t('console.unclassified')}
                      </h3>
                      <dl className='grid grid-cols-2 gap-2 text-sm'>
                        <dt>{t('console.retainedCost')}</dt>
                        <dd className='text-right'>
                          {money(item.total.quota)}
                        </dd>
                        <dt>{t('console.todayCost')}</dt>
                        <dd className='text-right'>
                          {money(item.today.quota)}
                        </dd>
                        <dt>{t('console.requests')}</dt>
                        <dd className='text-right'>
                          {number(item.total.requests)}
                        </dd>
                        <dt>Token</dt>
                        <dd className='text-right'>
                          {number(item.total.input + item.total.output)}
                        </dd>
                      </dl>
                    </div>
                  ))}
                </div>
              ) : (
                <EmptyState title={t('console.noUsage')} />
              )}
            </CardContent>
          </Card>
          <div className='bg-card flex flex-wrap items-center gap-3 rounded-2xl border p-4'>
            <label id='console-range-label' className='text-sm'>
              {t('console.timeRange')}
            </label>
            <Select
              value={String(days)}
              onValueChange={(value) => {
                if (value) setDays(Number(value))
              }}
            >
              <SelectTrigger
                aria-labelledby='console-range-label'
                className='w-36'
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {[7, 15, 30].map((value) => (
                  <SelectItem key={value} value={String(value)}>
                    {t('console.lastDays', { count: value })}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
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
              {query.isFetching
                ? t('console.refreshing')
                : t('console.refresh')}
            </Button>
            <span className='text-muted-foreground text-sm md:ml-auto'>
              {t('console.daily')}
            </span>
            <p className='text-muted-foreground w-full text-xs'>
              {t('console.filterScope')}
            </p>
          </div>
          {data && <ConsoleCharts data={data} />}
          <div className='grid min-w-0 gap-6 lg:grid-cols-[2fr_1fr]'>
            <Card className='min-w-0 rounded-2xl'>
              <CardHeader>
                <CardTitle>{t('console.recent')}</CardTitle>
              </CardHeader>
              <CardContent className='space-y-3'>
                {data?.recent.length ? (
                  data.recent.map((item) => (
                    <div
                      key={item.id}
                      className='bg-muted/50 flex flex-wrap justify-between gap-3 rounded-xl p-4'
                    >
                      <div className='min-w-0'>
                        <p className='font-medium break-all'>{item.model}</p>
                        <p className='text-muted-foreground text-xs'>
                          {new Date(item.created_at * 1000).toLocaleString(
                            locale,
                            { timeZone: data.timezone }
                          )}
                        </p>
                      </div>
                      <div className='text-right'>
                        <p className='text-primary font-semibold'>
                          {money(item.quota)}
                        </p>
                        <p className='text-muted-foreground text-xs'>
                          {number(item.input + item.output)} Token
                        </p>
                      </div>
                    </div>
                  ))
                ) : (
                  <EmptyState title={t('console.noUsage')} />
                )}
                <Button
                  variant='ghost'
                  className='w-full'
                  render={<Link to='/usage-logs' />}
                >
                  {t('console.viewAll')}
                </Button>
              </CardContent>
            </Card>
            <Card className='rounded-2xl'>
              <CardHeader>
                <CardTitle>{t('console.quickActions')}</CardTitle>
              </CardHeader>
              <CardContent className='space-y-3'>
                {[
                  { title: 'console.createKey', to: '/keys', icon: KeyRound },
                  {
                    title: 'console.usageLogs',
                    to: '/usage-logs',
                    icon: FileText,
                  },
                  { title: 'console.redeem', to: '/wallet', icon: Gift },
                ].map((item) => (
                  <Button
                    key={item.title}
                    variant='secondary'
                    className='h-auto min-h-16 w-full justify-start gap-4 p-4 whitespace-normal'
                    render={<Link to={item.to} />}
                  >
                    <item.icon aria-hidden='true' />
                    {t(item.title)}
                  </Button>
                ))}
              </CardContent>
            </Card>
          </div>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
