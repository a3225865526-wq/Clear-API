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

import { MappedStatusBadge } from './status-labels'
import { HEALTH_SUMMARY_LABELS } from '../constants'
import type { CandidateView, GateStates } from '../types'

import { Button } from '@/components/ui/button'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { Input } from '@/components/ui/input'
import { useState } from 'react'

// Runtime Health panel (§四十七 separation): the breaker is a runtime
// availability filter, NOT an authorization mechanism (§三). Reset/open
// touch memory-only state; persistent deactivation is Candidate Disable.

interface RuntimePanelProps {
  chainId: number
  candidates: CandidateView[]
  gates: GateStates | undefined
  onReset: (candidateId: number) => void
  onForceOpen: (candidateId: number, durationSeconds: number) => void
  actionPending: boolean
}

interface ConfirmState {
  kind: 'reset' | 'open'
  candidateId: number
}

export function RuntimePanel(props: RuntimePanelProps) {
  const { candidates, gates, onReset, onForceOpen, actionPending } = props
  const { t } = useTranslation()
  const [confirm, setConfirm] = useState<ConfirmState | null>(null)
  const [openDuration, setOpenDuration] = useState(300)

  const breakerOff = gates ? !gates.circuit_breaker_enabled : false

  return (
    <div className='flex flex-col gap-3' data-testid='runtime-panel'>
      {breakerOff ? (
        <p className='text-sm text-warning'>
          {t(
            'Circuit Breaker: OFF — runtime health below stays at Unknown and circuits never open.'
          )}
        </p>
      ) : null}
      <p className='text-xs text-muted-foreground'>
        {t(
          'Circuit state is memory-only per instance: a service restart clears it, and multi-instance deployments are not cluster-coordinated.'
        )}
      </p>

      {candidates.map((candidate) => {
        const runtime = candidate.runtime
        return (
          <div
            key={candidate.id}
            data-testid='runtime-row'
            data-candidate-id={candidate.id}
            className='rounded-lg border p-4'
          >
            <div className='flex flex-wrap items-center justify-between gap-2'>
              <div className='flex flex-wrap items-center gap-2'>
                <span className='text-sm font-medium'>
                  #{candidate.id}{' '}
                  {candidate.channel ? candidate.channel.name : ''}
                </span>
                <MappedStatusBadge
                  stateLabel={HEALTH_SUMMARY_LABELS[candidate.health_summary]}
                  fallbackKey={candidate.health_summary}
                  pulse={candidate.health_summary === 'CIRCUIT_OPEN'}
                  data-testid='runtime-state'
                />
              </div>
              {candidate.archived ? null : (
                <div className='flex items-center gap-2'>
                  <Button
                    size='sm'
                    variant='outline'
                    disabled={actionPending}
                    onClick={() => setConfirm({ kind: 'reset', candidateId: candidate.id })}
                  >
                    {t('Reset Circuit')}
                  </Button>
                  <Button
                    size='sm'
                    variant='outline'
                    disabled={actionPending}
                    onClick={() => setConfirm({ kind: 'open', candidateId: candidate.id })}
                  >
                    {t('Force Open')}
                  </Button>
                </div>
              )}
            </div>

            <dl className='mt-3 grid grid-cols-2 gap-x-4 gap-y-1 text-xs sm:grid-cols-3'>
              <Row
                label={t('Consecutive failures')}
                value={`${runtime.consecutive_failures} / ${runtime.failure_threshold}`}
              />
              <Row
                label={t('Open until')}
                value={
                  runtime.open_until > 0
                    ? new Date(runtime.open_until * 1000).toLocaleTimeString()
                    : '—'
                }
              />
              <Row
                label={t('Half-open probes')}
                value={`${runtime.half_open_active_probes} / ${runtime.half_open_max_probes}`}
              />
              <Row
                label={t('Last success')}
                value={
                  runtime.last_success_at > 0
                    ? new Date(runtime.last_success_at * 1000).toLocaleString()
                    : t('Never')
                }
              />
              <Row
                label={t('Last failure')}
                value={
                  runtime.last_failure_at > 0
                    ? new Date(runtime.last_failure_at * 1000).toLocaleString()
                    : t('Never')
                }
              />
              <Row
                label={t('Forced')}
                value={runtime.forced ? t('Yes') : t('No')}
              />
              <Row
                label={t('Failure threshold')}
                value={
                  candidate.effective_breaker.failure_threshold_override
                    ? `${t('Override')}: ${candidate.effective_breaker.failure_threshold}`
                    : `${t('Default')} (${candidate.effective_breaker.failure_threshold})`
                }
              />
              <Row
                label={t('Open duration')}
                value={
                  candidate.effective_breaker.cooldown_override
                    ? `${t('Override')}: ${candidate.effective_breaker.cooldown_seconds}s`
                    : `${t('Default')} (${candidate.effective_breaker.cooldown_seconds}s)`
                }
              />
              <Row
                label={t('Half-open max probes')}
                value={`${t('Default')} (${candidate.effective_breaker.half_open_max_probes})`}
              />
              <Row
                label={t('Retry-After max')}
                value={`${t('Default')} (${candidate.effective_breaker.retry_after_max}s)`}
              />
            </dl>
          </div>
        )
      })}

      <ConfirmDialog
        open={confirm?.kind === 'reset'}
        onOpenChange={(next) => !next && setConfirm(null)}
        title={t('Reset Circuit')}
        desc={t(
          'Returns the runtime breaker to the probeable CLOSED state. This only affects the in-memory breaker — it does not enable the candidate, the channel, the pool master, or any permission.'
        )}
        confirmText={t('Reset Circuit')}
        isLoading={actionPending}
        handleConfirm={() => {
          if (confirm) onReset(confirm.candidateId)
          setConfirm(null)
        }}
      />

      <ConfirmDialog
        open={confirm?.kind === 'open'}
        onOpenChange={(next) => !next && setConfirm(null)}
        title={t('Force Open')}
        desc={t(
          'Temporarily stops dispatching to this candidate. This is a temporary runtime state and disappears on service restart. For a persistent disable use Candidate Disable instead.'
        )}
        confirmText={t('Force Open')}
        isLoading={actionPending}
        handleConfirm={() => {
          if (confirm) onForceOpen(confirm.candidateId, openDuration)
          setConfirm(null)
        }}
      >
        <div className='flex items-center gap-2 py-2'>
          <label className='text-sm' htmlFor='force-open-duration'>
            {t('Duration (seconds)')}
          </label>
          <Input
            id='force-open-duration'
            type='number'
            min={1}
            value={openDuration}
            onChange={(event) => setOpenDuration(Number(event.target.value) || 0)}
            className='w-28'
          />
        </div>
      </ConfirmDialog>
    </div>
  )
}

function Row(props: { label: string; value: string }) {
  return (
    <div className='flex flex-col'>
      <dt className='text-muted-foreground'>{props.label}</dt>
      <dd className='font-medium'>{props.value}</dd>
    </div>
  )
}
