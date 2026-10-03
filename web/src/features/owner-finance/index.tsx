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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { LineChart, Line, CartesianGrid, XAxis, YAxis } from 'recharts'

import { CopyButton } from '@/components/copy-button'
import { Dialog } from '@/components/dialog'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout/components/section-page-layout'
import { LoadingState } from '@/components/loading-state'
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ChartContainer } from '@/components/ui/chart'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { useFinanceAccess, useFinanceSummary } from './access'
import { BillingReconciliation } from './billing-review'
import { Code, FinanceTable, Money, Panel } from './components'
import { beijingInput, financeTime, validRange } from './format'
import { metricLabels } from './labels'
import { FinanceOperations } from './operations'
import { OwnerOverview } from './overview'
import { FinanceRecords } from './records'
import { TaskSettlementReview } from './task-settlement-review'
import type { Fact, Metric, Range, Summary } from './types'
import { WalletReconciliation } from './wallet'

const profitKeys = [
  'consumption_revenue',
  'direct_cost',
  'gross_profit',
  'operating_expenses',
  'operating_profit',
]
export function OwnerFinance() {
  const { t } = useTranslation()
  const access = useFinanceAccess()
  let content
  if (!access.allowed) {
    content = access.isFetching ? (
      <div role='status'>
        <LoadingState message={t('Checking owner access')} />
      </div>
    ) : (
      <ErrorState
        title={t('Owner finance access unavailable')}
        description={t(
          'Only the server-authorized owner can view this ledger. Sign in again if access changed.'
        )}
      />
    )
  } else if (
    !access.data?.supplier_bill_lines_ready ||
    !access.data?.profit_summary_available
  ) {
    content = (
      <ErrorState
        title={t('Finance schema is not ready')}
        description={t(
          'The owner page requires schema v9. Production migration requires separate approval.'
        )}
      />
    )
  } else {
    content = (
      <FinanceWorkspace
        key={access.epoch}
        epoch={access.epoch}
        billingReady={access.data?.billing_reconciliation_ready === true}
        taskReady={access.data?.task_settlement_ready === true}
      />
    )
  }
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Owner finance')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='space-y-6'>
          {access.allowed && (
            <OwnerOverview
              key={`overview-${access.epoch}`}
              epoch={access.epoch}
              financeReady={
                access.data?.supplier_bill_lines_ready === true &&
                access.data?.profit_summary_available === true
              }
            />
          )}
          {content}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
function FinanceWorkspace({
  epoch,
  billingReady,
  taskReady,
}: {
  epoch: number
  billingReady: boolean
  taskReady: boolean
}) {
  const { t } = useTranslation()
  const [range, setRange] = useState<Range>({ period: 'today', trend: 'day' })
  const [selection, setSelection] = useState('today')
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [invalid, setInvalid] = useState(false)
  const query = useFinanceSummary(range, epoch, true)
  return (
    <div className='space-y-4'>
      <form
        className='bg-card flex flex-wrap items-end gap-3 rounded-xl border p-4'
        onSubmit={(event) => {
          event.preventDefault()
          const start = beijingInput(from),
            end = beijingInput(to)
          if (selection === 'custom' && !validRange(start, end)) {
            setInvalid(true)
            return
          }
          setInvalid(false)
          setRange(
            selection === 'custom'
              ? { from: start, to: end, trend: range.trend }
              : { period: selection as 'today' | 'month', trend: range.trend }
          )
        }}
      >
        <div className='space-y-1'>
          <Label htmlFor='finance-period'>{t('Period')}</Label>
          <select
            id='finance-period'
            className='bg-background h-10 rounded-md border px-3 text-sm'
            value={selection}
            onChange={(e) => setSelection(e.target.value)}
          >
            <option value='today'>{t('Today')}</option>
            <option value='month'>{t('Current calendar month')}</option>
            <option value='custom'>{t('Custom period')}</option>
          </select>
        </div>
        {selection === 'custom' && (
          <>
            <div className='min-w-0 space-y-1'>
              <Label htmlFor='finance-from'>
                {t('Start, Beijing time (inclusive)')}
              </Label>
              <Input
                id='finance-from'
                type='datetime-local'
                required
                value={from}
                onChange={(e) => setFrom(e.target.value)}
              />
            </div>
            <div className='min-w-0 space-y-1'>
              <Label htmlFor='finance-to'>
                {t('End, Beijing time (exclusive)')}
              </Label>
              <Input
                id='finance-to'
                type='datetime-local'
                required
                value={to}
                onChange={(e) => setTo(e.target.value)}
              />
            </div>
          </>
        )}
        <div className='space-y-1'>
          <Label htmlFor='finance-grain'>{t('Trend interval')}</Label>
          <select
            id='finance-grain'
            className='bg-background h-10 rounded-md border px-3 text-sm'
            value={range.trend}
            onChange={(e) =>
              setRange({ ...range, trend: e.target.value as 'day' | 'month' })
            }
          >
            <option value='day'>{t('Daily')}</option>
            <option value='month'>{t('Monthly')}</option>
          </select>
        </div>
        <Button type='submit' className='min-h-10'>
          {t('Apply period')}
        </Button>
        <Button
          type='button'
          variant='outline'
          className='min-h-10'
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          {t('Refresh')}
        </Button>
        <p className='text-muted-foreground basis-full text-xs'>
          {t(
            'CNY · Asia/Shanghai · start inclusive, end exclusive · maximum 366 days'
          )}
        </p>
        {invalid && (
          <p role='alert' className='text-destructive basis-full text-sm'>
            {t('Enter a valid Beijing period of up to 366 days.')}
          </p>
        )}
      </form>
      {query.isFetching && (
        <div role='status'>
          <LoadingState message={t('Reading finance snapshot')} />
        </div>
      )}
      {query.isError ? (
        <ErrorState
          title={t('Finance read failed')}
          description={t(
            'The schema may be unavailable or the period may exceed the fact limit. No partial truncated result is shown.'
          )}
          onRetry={() => void query.refetch()}
        />
      ) : (
        query.data && (
          <SummaryView
            key={JSON.stringify(range)}
            summary={query.data}
            billingReady={billingReady}
            taskReady={taskReady}
            onChanged={() => void query.refetch()}
          />
        )
      )}
    </div>
  )
}
function SummaryView({
  summary: s,
  onChanged,
  billingReady,
  taskReady,
}: {
  summary: Summary
  onChanged: () => void
  billingReady: boolean
  taskReady: boolean
}) {
  const { t, i18n } = useTranslation()
  const [detail, setDetail] = useState<string | null>(null)
  const [dimension, setDimension] = useState('channel')
  const [metric, setMetric] = useState('consumption_revenue')
  const [factFilter, setFactFilter] = useState('all')
  const [tab, setTab] = useState('overview')
  const unresolved = s.facts.filter((fact) => fact.amount_cny == null)
  const factsFor = (key: string) =>
    s.facts.filter((fact) => {
      if (key === 'gross_profit' || key === 'operating_profit') {
        return [
          'consumption_revenue',
          'direct_cost',
          ...(key === 'operating_profit' ? ['operating_expenses'] : []),
        ].includes(fact.metric)
      }
      if (key === 'net_cash_receipts') {
        return ['cash_receipts', 'cash_refunds'].includes(fact.metric)
      }
      if (key === 'consumption_gross') {
        return (
          fact.metric === 'consumption_revenue' &&
          fact.posting_kind === 'consume'
        )
      }
      if (key === 'consumption_adjustments') {
        return (
          fact.metric === 'consumption_revenue' &&
          fact.posting_kind === 'reverse'
        )
      }
      return fact.metric === key
    })
  const factColumns = [
    {
      id: 'metric',
      header: t('Metric'),
      cell: (f: Fact) => t(metricLabels[f.metric] ?? 'Unknown'),
    },
    {
      id: 'origin',
      header: t('Fact reference'),
      cell: (f: Fact) => (
        <span className='font-mono text-xs'>
          {f.table} #{f.id}
          {f.related_id > 0 && ` → #${f.related_id}`}
        </span>
      ),
    },
    {
      id: 'time',
      header: t('Attribution period'),
      cell: (f: Fact) =>
        `${financeTime(f.from, i18n.language)} – ${financeTime(f.to, i18n.language)}`,
    },
    {
      id: 'amount',
      header: t('Contribution, CNY'),
      cell: (f: Fact) => <Money value={f.amount_cny} />,
    },
    {
      id: 'status',
      header: t('Evidence / gap'),
      cell: (f: Fact) => <Code code={f.status} />,
    },
    {
      id: 'scope',
      header: t('Channel / model / pool'),
      cell: (f: Fact) => (
        <span className='break-all'>
          {f.channel_id || '—'} / {f.upstream_model || '—'} /{' '}
          {f.pool_key || '—'}
        </span>
      ),
    },
  ]
  const showFacts = (data: Fact[], label: string) => (
    <FinanceTable data={data} columns={factColumns} label={label} />
  )
  const warnings = [
    ...new Set([
      ...s.warnings,
      ...s.supplier_checks.flatMap((check) => check.warnings),
    ]),
  ]
  return (
    <div className='space-y-4'>
      <Alert className='border-primary/30 bg-primary/5'>
        <AlertTitle>
          {t('Partial coverage · full site profit unavailable')}
        </AlertTitle>
        <AlertDescription className='space-y-2'>
          <p>
            {t(
              'Registered subtotals and known differences cover listed facts only. Unknown income, costs and unconfirmed expenses prevent full site totals.'
            )}
          </p>
          <p>
            {t('Unknown posting contributions')}: {unresolved.length} ·{' '}
            {t('Review classes')}:{' '}
            {Object.values(s.sync_health.needs_review_classes).reduce(
              (a, b) => a + b,
              0
            )}{' '}
            · {t('Account revenue and utilization are unavailable')}
          </p>
        </AlertDescription>
      </Alert>
      <Panel title={t('Snapshot and attribution')}>
        <dl className='grid gap-2 text-sm sm:grid-cols-2'>
          <div>
            <dt className='text-muted-foreground'>
              {t('Selected period, Beijing time')}
            </dt>
            <dd>
              {financeTime(s.from, i18n.language)} –{' '}
              {financeTime(s.to, i18n.language)}
            </dd>
          </div>
          <div>
            <dt className='text-muted-foreground'>{t('Read cutoff')}</dt>
            <dd>
              {financeTime(s.read_at, i18n.language)} · {t('Schema version')}:{' '}
              {s.schema_version}
            </dd>
          </div>
          <div>
            <dt className='text-muted-foreground'>
              {t('Audit / sync watermark')}
            </dt>
            <dd>
              {s.audit_watermark_id} / {s.sync_health.watermark_id}
            </dd>
          </div>
          <div>
            <dt className='text-muted-foreground'>
              {t('Snapshot fingerprint')}
            </dt>
            <dd className='flex min-w-0 items-center gap-2'>
              <code className='min-w-0 text-xs break-all'>
                {s.snapshot_sha256}
              </code>
              <CopyButton
                value={s.snapshot_sha256}
                aria-label={t('Copy snapshot fingerprint')}
              />
            </dd>
          </div>
        </dl>
        <p className='text-muted-foreground text-xs'>
          {t(
            'Late facts restate historical periods. Live cache and process counters are outside this database snapshot.'
          )}
        </p>
      </Panel>
      {s.facts.length === 0 && (
        <EmptyState
          title={t('No registered facts in this period')}
          description={t(
            'No records does not confirm zero expenses or complete site coverage.'
          )}
          className='min-h-32'
        />
      )}
      <Tabs value={tab} onValueChange={(value) => setTab(String(value))}>
        <div className='overflow-x-auto'>
          <TabsList className='h-auto min-h-11'>
            {Object.entries({
              overview: 'Overview',
              trend: 'Trend',
              dimensions: 'Dimensions',
              reconciliation: 'Reconciliation',
              exceptions: 'Exceptions',
              operations: 'Finance operations',
            }).map(([key, label]) => (
              <TabsTrigger key={key} value={key} className='min-h-10 px-3'>
                {t(label)}
              </TabsTrigger>
            ))}
          </TabsList>
        </div>
        <TabsContent value='overview' className='space-y-4'>
          {[
            { title: 'Consumption and operating results', keys: profitKeys },
            {
              title: 'Cash flows, separate from profit',
              keys: ['cash_receipts', 'cash_refunds', 'net_cash_receipts'],
            },
            {
              title: 'Source balances and adjustments',
              keys: [
                'opening_source_value',
                'closing_source_value',
                'consumption_gross',
                'consumption_adjustments',
              ],
            },
          ].map((group) => (
            <Panel key={group.title} title={t(group.title)}>
              <div className='grid gap-3 md:grid-cols-2 xl:grid-cols-3'>
                {group.keys.map((key) => (
                  <MetricCard
                    key={key}
                    name={t(metricLabels[key])}
                    metric={s.metrics[key]}
                    onDrill={() => setDetail(key)}
                  />
                ))}
              </div>
            </Panel>
          ))}
          <p className='text-sm'>
            {t('Full site profit margin')}:{' '}
            <span>{s.profit_margin ?? t('Not calculable')}</span> ·{' '}
            {t('Registered fact margin (ratio)')}:{' '}
            <span>{s.recorded_profit_margin ?? t('Not calculable')}</span>
          </p>
          <p className='text-muted-foreground text-xs'>
            {t(
              'A zero revenue denominator has no margin. Cash receipts and unused source value are not profit revenue.'
            )}
          </p>
        </TabsContent>
        <TabsContent value='trend'>
          <Panel title={t('Registered trend, not full site totals')}>
            <div className='space-y-1'>
              <Label htmlFor='finance-trend-metric'>{t('Metric')}</Label>
              <select
                id='finance-trend-metric'
                className='bg-background h-10 rounded-md border px-3'
                value={metric}
                onChange={(e) => setMetric(e.target.value)}
              >
                {profitKeys.map((key) => (
                  <option key={key} value={key}>
                    {t(metricLabels[key])}
                  </option>
                ))}
              </select>
            </div>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Chart positions are approximate. Exact registered values are below. Unknown buckets remain gaps, never zero or connected lines.'
              )}
            </p>
            <ChartContainer
              className='h-56 w-full'
              config={{
                value: {
                  label: t(metricLabels[metric]),
                  color: 'var(--chart-1)',
                },
              }}
            >
              <LineChart
                data={s.trend.map((period) => ({
                  label: financeTime(period.from, i18n.language),
                  value:
                    period.metrics[metric]?.recorded_total == null
                      ? null
                      : Number(period.metrics[metric].recorded_total),
                }))}
              >
                <CartesianGrid vertical={false} />
                <XAxis dataKey='label' tickLine={false} minTickGap={60} />
                <YAxis width={70} />
                <Line
                  type='linear'
                  dataKey='value'
                  stroke='var(--color-value)'
                  connectNulls={false}
                  isAnimationActive={false}
                  dot
                />
              </LineChart>
            </ChartContainer>
            <FinanceTable
              label={t('Exact trend values')}
              data={s.trend}
              columns={[
                {
                  id: 'date',
                  header: t('Attribution period'),
                  cell: (p) =>
                    `${financeTime(p.from, i18n.language)} – ${financeTime(p.to, i18n.language)}`,
                },
                ...profitKeys.map((key) => ({
                  id: key,
                  header: t(metricLabels[key]),
                  cell: (p: Summary['trend'][number]) => (
                    <Money value={p.metrics[key]?.recorded_total} />
                  ),
                })),
              ]}
            />
          </Panel>
        </TabsContent>
        <TabsContent value='dimensions'>
          <Panel title={t('Dimensions, including unattributed facts')}>
            <Label htmlFor='finance-dimension'>{t('Dimension')}</Label>
            <select
              id='finance-dimension'
              className='bg-background ml-2 h-10 rounded-md border px-3'
              value={dimension}
              onChange={(e) => setDimension(e.target.value)}
            >
              <option value='channel'>{t('Channel')}</option>
              <option value='upstream_model'>{t('Upstream model')}</option>
              <option value='pool'>{t('Pool')}</option>
            </select>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Bill totals have no reliable model or pool split. Unattributed facts remain visible; account profit is unavailable.'
              )}
            </p>
            <FinanceTable
              data={s.dimensions.filter((row) => row.dimension === dimension)}
              label={t('Dimension subtotals')}
              columns={[
                {
                  id: 'key',
                  header: t('Dimension'),
                  cell: (row) =>
                    row.key === 'unattributed' ? t('Unattributed') : row.key,
                },
                ...profitKeys.map((key) => ({
                  id: key,
                  header: t(metricLabels[key]),
                  cell: (row: Summary['dimensions'][number]) => (
                    <>
                      <Money value={row.metrics[key]?.known_subtotal} />
                      <span className='ml-2 text-xs'>
                        {t('Known subtotal')} · {t('Unknown items')}:{' '}
                        {row.metrics[key]?.unknown_items}
                      </span>
                    </>
                  ),
                })),
              ]}
            />
          </Panel>
        </TabsContent>
        <TabsContent value='reconciliation' className='space-y-4'>
          <Panel title={t('Posted facts and source references')}>
            <Label htmlFor='finance-fact-filter'>{t('Metric')}</Label>
            <select
              id='finance-fact-filter'
              className='bg-background ml-2 h-10 rounded-md border px-3'
              value={factFilter}
              onChange={(e) => setFactFilter(e.target.value)}
            >
              <option value='all'>{t('All')}</option>
              {Object.entries(metricLabels).map(([key, label]) => (
                <option value={key} key={key}>
                  {t(label)}
                </option>
              ))}
            </select>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Derived results subtract component facts; do not add source balances, estimates or bill lines to profit.'
              )}
            </p>
            {showFacts(
              factFilter === 'all' ? s.facts : factsFor(factFilter),
              t('Posted facts')
            )}
          </Panel>
          <Panel title={t('Supplier bill reconciliation')}>
            <p className='text-muted-foreground text-sm'>
              {t(
                'Only bill totals post to costs. Line sums and request estimates are reconciliation evidence. Matching references do not verify amounts.'
              )}
            </p>
            <FinanceTable
              data={s.supplier_checks}
              label={t('Bill checks')}
              columns={[
                {
                  id: 'bill',
                  header: t('Bill / channel'),
                  cell: (c) => `#${c.bill.id} / ${c.bill.channel_id}`,
                },
                {
                  id: 'billAmount',
                  header: t('Bill total'),
                  cell: (c) => <Money value={c.bill_amount_cny} />,
                },
                {
                  id: 'lines',
                  header: t('Line net'),
                  cell: (c) => <Money value={c.net_cny} />,
                },
                {
                  id: 'variance',
                  header: t('Variance'),
                  cell: (c) => <Money value={c.variance_cny} />,
                },
                {
                  id: 'nonusage',
                  header: t('Non-usage line net'),
                  cell: (c) => <Money value={c.non_usage_net_cny} />,
                },
                {
                  id: 'review',
                  header: t('FX / out of period / unmatched'),
                  cell: (c) =>
                    `${c.unknown_fx_lines} / ${c.out_of_bill_period_lines} / ${c.match_stats.unmatched_ref ?? 0}`,
                },
              ]}
            />
          </Panel>
          <WalletReconciliation />
          <BillingReconciliation ready={billingReady} />
          <TaskSettlementReview ready={taskReady} />
          <FinanceRecords />
        </TabsContent>
        <TabsContent value='exceptions' className='space-y-4'>
          <FinanceRecords defaultSource='sync-events' />
          <Panel title={t('Unknown contributions requiring review')}>
            {showFacts(unresolved, t('Unknown contributions'))}
          </Panel>
          <Panel title={t('Sync and capture health')}>
            <dl className='grid gap-2 text-sm sm:grid-cols-2'>
              {Object.entries({
                ...s.sync_health.counts,
                ...s.sync_health.needs_review_classes,
              }).map(([code, count]) => (
                <div key={code}>
                  <dt>
                    <Code code={code} />
                  </dt>
                  <dd className='font-mono'>{count}</dd>
                </div>
              ))}
            </dl>
            <p className='text-sm'>
              {t('Oldest pending age, seconds')}:{' '}
              {s.sync_health.oldest_pending_age_seconds ?? t('Not available')} ·{' '}
              {t('Unimported settlement sample')}:{' '}
              {s.sync_health.unimported_settlement_sample}
            </p>
            <ul className='list-inside list-disc space-y-2 text-sm'>
              {warnings.map((code) => (
                <li key={code}>
                  <Code code={code} />
                </li>
              ))}
            </ul>
          </Panel>
        </TabsContent>
        <TabsContent value='operations'>
          <FinanceOperations onChanged={onChanged} />
        </TabsContent>
      </Tabs>
      <Dialog
        open={detail !== null}
        onOpenChange={(open) => {
          if (!open) setDetail(null)
        }}
        title={detail ? t(metricLabels[detail]) : t('Metric')}
        description={t(
          'Listed contributions only. Coverage gaps prevent full site totals.'
        )}
      >
        {detail && (
          <>
            <MetricCard
              name={t(metricLabels[detail])}
              metric={s.metrics[detail]}
            />
            <ul className='list-inside list-disc space-y-2 text-sm'>
              {[...new Set(s.metrics[detail].coverage)].map((code) => (
                <li key={code}>
                  <Code code={code} />
                </li>
              ))}
            </ul>
            {showFacts(factsFor(detail), t('Metric contributions'))}
          </>
        )}
      </Dialog>
    </div>
  )
}
function MetricCard({
  name,
  metric,
  onDrill,
}: {
  name: string
  metric: Metric
  onDrill?: () => void
}) {
  const { t } = useTranslation()
  return (
    <div className='min-w-0 space-y-2 rounded-lg border p-3'>
      <div className='flex flex-wrap items-start justify-between gap-2'>
        <h4 className='text-sm font-medium'>{name}</h4>
        <Badge variant='outline'>{t('Partial coverage')}</Badge>
      </div>
      <div className='text-sm'>
        {t('Full site total')}: <Money value={metric.total} />
      </div>
      <div className='text-base'>
        {t('Known subtotal')}: <Money value={metric.known_subtotal} />
      </div>
      <div className='text-muted-foreground text-xs'>
        {t('Registered total')}: <Money value={metric.recorded_total} />
      </div>
      <div className='text-xs'>
        {t('Unknown items')}: {metric.unknown_items} ·{' '}
        <Code code={metric.evidence_level} />
      </div>
      {onDrill && (
        <Button variant='outline' className='min-h-10 w-full' onClick={onDrill}>
          {t('Inspect facts')}
          <span className='sr-only'>: {name}</span>
        </Button>
      )}
    </div>
  )
}
