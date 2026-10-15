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
import { Eye } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import dayjs from '@/lib/dayjs'

import {
  UNKNOWN_REVIEW_LABEL,
  USAGE_UNAVAILABLE_LABEL,
} from '../constants'
import type { AttemptView, DecisionView } from '../types'

// Recent activity (§二十五–§二十八): safe reason codes only; user billing and
// provider cost are two separate columns — settled billing never implies a
// confirmed upstream cost. UNKNOWN_REVIEW / unavailable are never rendered
// as zero.

export function AttemptsPanel(props: { attempts: AttemptView[] }) {
  const { attempts } = props
  const { t } = useTranslation()
  const [selected, setSelected] = useState<AttemptView | null>(null)

  const columns: StaticDataTableColumn<AttemptView>[] = [
    {
      id: 'time',
      header: t('Time'),
      cell: (row) =>
        row.started_at > 0 ? dayjs.unix(row.started_at).format('MM-DD HH:mm:ss') : '—',
    },
    { id: 'seq', header: '#', cell: (row) => row.sequence },
    {
      id: 'candidate',
      header: t('Candidate'),
      cell: (row) => `#${row.candidate_id} ${t(row.candidate_type === 'pool' ? 'Pool' : 'API Channel')}`,
    },
    {
      id: 'terminal',
      header: t('Terminal'),
      cell: (row) => (
        <Badge variant={terminalVariant(row.terminal_state)}>{row.terminal_state || '—'}</Badge>
      ),
    },
    { id: 'http', header: t('HTTP'), cell: (row) => row.http_status > 0 ? row.http_status : '—' },
    {
      id: 'failure',
      header: t('Failure class'),
      cell: (row) => row.failure_class || '—',
    },
    {
      id: 'usage',
      header: t('Usage'),
      cell: (row) => usageCell(row, t),
    },
    {
      id: 'billing',
      header: t('User Billing'),
      cell: (row) => billingCell(row.user_billing_state, t),
    },
    {
      id: 'cost',
      header: t('Provider Cost'),
      cell: (row) => costCell(row, t),
    },
    {
      id: 'details',
      header: '',
      cell: (row) => (
        <Button
          variant='ghost'
          size='icon-xs'
          aria-label={t('Attempt details')}
          onClick={() => setSelected(row)}
        >
          <Eye />
        </Button>
      ),
    },
  ]

  return (
    <>
      <StaticDataTable
        columns={columns}
        data={attempts}
        getRowKey={(row) => row.attempt_id}
      />
      <Dialog
        open={selected !== null}
        onOpenChange={(next) => !next && setSelected(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('Attempt details')}</DialogTitle>
          </DialogHeader>
          {selected ? <AttemptDetails attempt={selected} /> : null}
        </DialogContent>
      </Dialog>
    </>
  )
}

export function DecisionsPanel(props: { decisions: DecisionView[] }) {
  const { decisions } = props
  const { t } = useTranslation()

  const columns: StaticDataTableColumn<DecisionView>[] = [
    {
      id: 'time',
      header: t('Time'),
      cell: (row) => (row.at > 0 ? dayjs.unix(row.at).format('MM-DD HH:mm:ss') : '—'),
    },
    { id: 'candidate', header: t('Candidate'), cell: (row) => `#${row.candidate_id}` },
    { id: 'phase', header: t('Phase'), cell: (row) => row.phase },
    { id: 'decision', header: t('Decision'), cell: (row) => row.decision },
    {
      id: 'reason',
      header: t('Reason'),
      cell: (row) => <Badge variant='outline'>{row.reason || '—'}</Badge>,
    },
    { id: 'outcome', header: t('Outcome'), cell: (row) => row.outcome || '—' },
  ]

  return (
    <StaticDataTable columns={columns} data={decisions} getRowKey={(row) => row.id} />
  )
}

function usageCell(row: AttemptView, t: (key: string) => string) {
  if (row.usage_state === 'unavailable') {
    return <Badge variant='warning'>{t(USAGE_UNAVAILABLE_LABEL)}</Badge>
  }
  if (row.usage_state === 'observed') {
    return t('Observed')
  }
  return row.usage_state
}

function billingCell(state: string, t: (key: string) => string) {
  if (!state) return '—'
  return t(state.charAt(0).toUpperCase() + state.slice(1))
}

function costCell(row: AttemptView, t: (key: string) => string) {
  if (row.provider_cost_state === 'UNKNOWN_REVIEW') {
    return <Badge variant='warning'>{t(UNKNOWN_REVIEW_LABEL)}</Badge>
  }
  if (!row.provider_cost_state || row.provider_cost_state === 'NONE') return '—'
  if (row.provider_cost_state === 'ZERO_CONFIRMED') return t('Zero (confirmed)')
  const amount =
    row.provider_cost_amount !== '' && row.provider_cost_state === 'KNOWN'
      ? `${row.provider_cost_amount} ${row.provider_cost_currency}`
      : row.provider_cost_state
  return amount
}

function terminalVariant(state: string): 'default' | 'secondary' | 'destructive' | 'outline' | 'warning' {
  if (state === 'SUCCEEDED') return 'default'
  if (state === 'OUTCOME_UNCERTAIN') return 'warning'
  if (state === '') return 'outline'
  return 'secondary'
}

function AttemptDetails(props: { attempt: AttemptView }) {
  const { attempt } = props
  const { t } = useTranslation()
  const fields: [string, string][] = [
    [t('Attempt ID'), attempt.attempt_id],
    [t('Request ID'), attempt.request_id],
    [t('Upstream response ID'), attempt.upstream_response_id || '—'],
    [t('Safe account ref'), attempt.safe_account_ref || '—'],
    [t('Dispatch state'), attempt.dispatch_state],
    [t('Health outcome'), attempt.health_outcome || '—'],
    [t('Breaker effect'), attempt.breaker_effect || '—'],
    [t('Stream completion'), attempt.stream_completion || '—'],
    [t('Failure code'), attempt.failure_code || '—'],
    [t('Outcome uncertain'), attempt.outcome_uncertain ? t('Yes') : t('No')],
  ]
  return (
    <dl className='grid grid-cols-1 gap-2 text-sm sm:grid-cols-2' data-testid='attempt-details'>
      {fields.map(([label, value]) => (
        <div key={label} className='flex flex-col'>
          <dt className='text-xs text-muted-foreground'>{label}</dt>
          <dd className='break-all font-medium'>{value}</dd>
        </div>
      ))}
    </dl>
  )
}
