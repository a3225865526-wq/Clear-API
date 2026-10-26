import { RadioTower } from 'lucide-react'
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
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { EmptyState } from '@/components/empty-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { DialogClose } from '@/components/ui/dialog'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import { aggregateMonitorStatus, type Channel } from './api'
import { probeReasonKeys, probeStatusKeys, statusColors } from './constants'
import { MonitorHistory } from './monitor-history'

export function ChannelCard(props: {
  channel: Channel
  days: number
  stale: boolean
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const primary = props.channel.models[0]
  const availability = primary?.windows[String(props.days)]
  const status = props.stale
    ? 'unknown'
    : aggregateMonitorStatus(props.channel.models.map((item) => item.status))
  const percent = (value: number | null | undefined) =>
    value == null ? t('console.noData') : `${formatNumber(value, locale)}%`
  const milliseconds = (value: number | null | undefined) =>
    value == null ? t('console.noData') : formatNumber(value, locale)
  const timestamp = (value: number) =>
    new Date(value * 1000).toLocaleString(locale, { timeZone: 'Asia/Shanghai' })
  return (
    <Card className='relative min-w-0 rounded-2xl'>
      <CardContent className='space-y-5 p-5'>
        <Dialog
          title={props.channel.name}
          description={t('monitor.detailDescription')}
          contentClassName='sm:max-w-6xl'
          trigger={
            <Button
              variant='ghost'
              className='h-auto w-full justify-start gap-4 p-0 text-left whitespace-normal after:absolute after:inset-0 after:rounded-2xl'
              aria-label={t('monitor.viewChannel', {
                name: props.channel.name,
              })}
            >
              <span className='bg-primary/10 text-primary flex size-12 shrink-0 items-center justify-center rounded-xl'>
                <RadioTower aria-hidden='true' />
              </span>
              <span className='min-w-0 flex-1'>
                <span className='block text-xl font-semibold break-all'>
                  {props.channel.name}
                </span>
                <span className='text-muted-foreground mt-1 block text-xs break-all'>
                  {props.channel.platform || t('console.unclassified')} ·{' '}
                  {primary?.model || t('console.noData')}
                </span>
              </span>
              <Badge className={statusColors[status]}>
                {t(`monitor.${status}`)}
              </Badge>
            </Button>
          }
          footer={
            <DialogClose
              render={<Button aria-label={t('monitor.closeDetails')} />}
            >
              {t('monitor.close')}
            </DialogClose>
          }
        >
          {props.channel.models.length === 0 ? (
            <EmptyState title={t('monitor.noModels')} />
          ) : (
            <div className='max-w-full overflow-x-auto'>
              <Table>
                <TableHeader>
                  <TableRow>
                    {[
                      'console.model',
                      'monitor.latestStatus',
                      'monitor.latestLatency',
                      'monitor.availability7',
                      'monitor.availability15',
                      'monitor.availability30',
                      'monitor.average7',
                    ].map((key) => (
                      <TableHead key={key} className='whitespace-nowrap'>
                        {t(key)}
                      </TableHead>
                    ))}
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {props.channel.models.map((item) => (
                    <TableRow key={item.id}>
                      <TableCell className='max-w-64 min-w-40 break-all whitespace-normal'>
                        {item.model}
                      </TableCell>
                      <TableCell>
                        <Badge
                          className={
                            statusColors[props.stale ? 'unknown' : item.status]
                          }
                        >
                          {t(
                            props.stale
                              ? 'monitor.unknown'
                              : (probeStatusKeys[item.status] ??
                                  'monitor.unknown')
                          )}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        {milliseconds(item.latest?.latency_ms)}
                      </TableCell>
                      {[7, 15, 30].map((days) => (
                        <TableCell
                          key={days}
                          title={t('monitor.samples', {
                            count: item.windows[String(days)]?.samples ?? 0,
                          })}
                        >
                          {percent(item.windows[String(days)]?.percent)}
                        </TableCell>
                      ))}
                      <TableCell>
                        {milliseconds(item.windows['7']?.average_ms)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              <p className='mt-4 text-sm'>
                {t('Channel business status')} ·{' '}
                {t(
                  (
                    { 1: 'Enabled', 2: 'Disabled', 3: 'Disabled' } as Record<
                      number,
                      string
                    >
                  )[props.channel.business_status ?? 0] ?? 'monitor.unknown'
                )}
              </p>
              <p className='text-muted-foreground text-xs'>
                {t('Monitoring results do not change channel business status')}
              </p>
              {props.channel.models.map((item) => (
                <MonitorHistory key={item.id} monitor={item} />
              ))}
            </div>
          )}
        </Dialog>
        <div className='grid grid-cols-2 gap-3'>
          <div className='bg-muted/40 rounded-xl p-4'>
            <p className='text-muted-foreground text-xs'>
              {t('monitor.dialogLatency')}
            </p>
            <p className='mt-2 font-semibold'>
              {milliseconds(primary?.latest?.latency_ms)}{' '}
              <span className='text-muted-foreground text-xs'>ms</span>
            </p>
          </div>
          <div className='bg-muted/40 rounded-xl p-4'>
            <p className='text-muted-foreground text-xs'>
              {t('monitor.endpointPing')}
            </p>
            <p className='mt-2 font-semibold'>
              {milliseconds(primary?.latest?.endpoint_ms)}{' '}
              <span className='text-muted-foreground text-xs'>ms</span>
            </p>
          </div>
        </div>
        <div className='flex flex-wrap items-center justify-between gap-2 border-y py-4'>
          <span className='text-muted-foreground text-sm'>
            {t('monitor.availability', { count: props.days })}
          </span>
          <strong className='text-primary text-3xl'>
            {percent(availability?.percent)}
          </strong>
        </div>
        <p className='text-muted-foreground text-xs'>
          {t('monitor.samples', { count: availability?.samples ?? 0 })}
          {availability?.first_sample &&
            ` · ${t('monitor.coverageSince', {
              time: timestamp(availability.first_sample),
            })}`}
        </p>
        <div>
          <p className='text-muted-foreground mb-2 text-xs'>
            {t('monitor.history')} · {primary?.model || t('console.noData')}
          </p>
          <div className='flex gap-1 overflow-x-auto py-1'>
            {primary?.history.length ? (
              primary.history.map((record) => (
                <Tooltip key={record.id}>
                  <TooltipTrigger
                    render={
                      <Button
                        variant='ghost'
                        size='icon'
                        className={`relative z-10 h-7 w-2 min-w-2 shrink-0 rounded-sm p-0 ${statusColors[record.status] || statusColors.unknown}`}
                        aria-label={`${timestamp(record.checked_at)} · ${t(probeStatusKeys[record.status] ?? 'monitor.unknown')} · ${milliseconds(record.latency_ms)} ms`}
                      />
                    }
                  />
                  <TooltipContent>
                    {timestamp(record.checked_at)} ·{' '}
                    {t(probeStatusKeys[record.status] ?? 'monitor.unknown')} ·{' '}
                    {milliseconds(record.latency_ms)} ms
                    {record.error_code &&
                      ` · ${t(probeReasonKeys[record.error_code] ?? 'monitor.probeFailed')}`}
                  </TooltipContent>
                </Tooltip>
              ))
            ) : (
              <span className='text-muted-foreground text-sm'>
                {t('monitor.noRecords')}
              </span>
            )}
          </div>
          <div className='text-muted-foreground mt-1 flex justify-between text-xs'>
            <span>{t('monitor.past')}</span>
            <span>{t('monitor.now')}</span>
          </div>
        </div>
        {primary?.latest && (
          <p className='text-sm'>
            {t('Latest monitoring result')} ·{' '}
            {t(probeStatusKeys[primary.latest.status] ?? 'monitor.unknown')}{' '}
            {primary.latest.error_code &&
              `· ${t(probeReasonKeys[primary.latest.error_code] ?? 'monitor.probeFailed')}`}
          </p>
        )}
        <p className='text-muted-foreground text-xs'>
          {primary?.latest
            ? t('monitor.checkedAt', {
                time: timestamp(primary.latest.checked_at),
              })
            : t('monitor.notChecked')}
        </p>
      </CardContent>
    </Card>
  )
}
