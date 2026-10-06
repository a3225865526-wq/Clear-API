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

import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'

import { MappedStatusBadge } from './status-labels'
import {
  CHANNEL_STATE_LABELS,
  ELIGIBILITY_LABELS,
  HEALTH_SUMMARY_LABELS,
  POOL_PUBLICATION_LABELS,
} from '../constants'
import type { CandidateView } from '../types'

// One candidate's five display dimensions (§二): persistent config, channel
// state, eligibility, pool authority, runtime health — separate badges with
// text, never one merged label.

interface CandidateDiagnosticsProps {
  candidate: CandidateView
  onToggleEnabled?: (enabled: boolean) => void
  togglePending?: boolean
  actions?: React.ReactNode
}

export function CandidateDiagnostics(props: CandidateDiagnosticsProps) {
  const { candidate, onToggleEnabled, togglePending, actions } = props
  const { t } = useTranslation()

  const channelState = channelStateLabel(candidate)

  return (
    <div
      data-testid='candidate-card'
      data-candidate-id={candidate.id}
      data-archived={candidate.archived ? 'true' : undefined}
      className='rounded-lg border p-4'
    >
      <div className='flex flex-wrap items-start justify-between gap-2'>
        <div className='min-w-0'>
          <div className='flex flex-wrap items-center gap-2'>
            <span className='text-sm font-medium'>
              #{candidate.id}{' '}
              {candidate.channel ? candidate.channel.name : `target ${targetId(candidate)}`}
            </span>
            <Badge variant='outline'>
              {t(candidate.candidate_type === 'pool' ? 'Pool' : 'API Channel')}
            </Badge>
            <Badge variant='secondary'>
              {t('Priority')} {candidate.priority}
            </Badge>
          </div>
          {candidate.channel ? (
            <p className='mt-1 text-xs text-muted-foreground'>
              {t('Channel group')}: {candidate.channel.group} · {t('Type')}:{' '}
              {candidate.channel.type_name}
            </p>
          ) : null}
        </div>
        <div className='flex items-center gap-2'>
          {actions}
          {onToggleEnabled && !candidate.archived ? (
            <div className='flex items-center gap-2'>
              <span className='text-xs text-muted-foreground'>
                {t('Enabled')}
              </span>
              <Switch
                checked={candidate.enabled}
                disabled={togglePending}
                aria-label={t('Candidate enabled')}
                onCheckedChange={onToggleEnabled}
              />
            </div>
          ) : null}
        </div>
      </div>

      <div className='mt-3 flex flex-wrap gap-1.5'>
        {/* A. Persistent config — exactly one config badge per candidate */}
        {configBadge(candidate)}
        {/* B. Target channel state */}
        <MappedStatusBadge
          stateLabel={channelState}
          fallbackKey='Channel Disabled'
          data-testid='dim-channel'
        />
        {/* E. Runtime health (from the backend breaker snapshot — never guessed) */}
        <MappedStatusBadge
          stateLabel={HEALTH_SUMMARY_LABELS[candidate.health_summary]}
          fallbackKey={candidate.health_summary}
          pulse={candidate.health_summary === 'CIRCUIT_OPEN'}
          data-testid='dim-runtime'
        />
        {/* C. Route eligibility */}
        {candidate.archived ? null : (
          <>
            <MappedStatusBadge
              stateLabel={
                candidate.model_served
                  ? ELIGIBILITY_LABELS.model_served.ok
                  : ELIGIBILITY_LABELS.model_served.bad
              }
              fallbackKey='Model Not Served'
              data-testid='dim-model'
            />
            <MappedStatusBadge
              stateLabel={
                candidate.pricing_configured
                  ? ELIGIBILITY_LABELS.pricing.ok
                  : ELIGIBILITY_LABELS.pricing.bad
              }
              fallbackKey='PRICING MISSING'
              data-testid='dim-pricing'
            />
            <MappedStatusBadge
              stateLabel={
                candidate.source_match
                  ? ELIGIBILITY_LABELS.source.ok
                  : ELIGIBILITY_LABELS.source.bad
              }
              fallbackKey='Source Mismatch'
              data-testid='dim-source'
            />
          </>
        )}
        {/* D. Pool authority (POOL candidates only) */}
        {candidate.pool_authority ? (
          <>
            <Badge
              variant={candidate.pool_authority.master_enabled ? 'default' : 'outline'}
              data-testid='dim-pool-master'
            >
              {t('Pool Master')}:{' '}
              {candidate.pool_authority.master_enabled ? t('ON') : t('OFF')}
            </Badge>
            <Badge
              variant={candidate.pool_authority.channel_routing ? 'default' : 'outline'}
              data-testid='dim-pool-routing'
            >
              {t('Channel Routing')}:{' '}
              {candidate.pool_authority.channel_routing ? t('ON') : t('OFF')}
            </Badge>
            <MappedStatusBadge
              stateLabel={
                POOL_PUBLICATION_LABELS[candidate.pool_authority.publication]
              }
              fallbackKey='Unknown'
              data-testid='dim-pool-publication'
            />
          </>
        ) : null}
      </div>

      {candidate.archived ? (
        <p className='mt-3 text-xs text-muted-foreground'>
          {t(
            'Archived candidate — kept so historical attempts stay explainable. It never routes again.'
          )}
        </p>
      ) : null}
    </div>
  )
}

function targetId(candidate: CandidateView): number {
  return candidate.archived
    ? (candidate.archived_target_id ?? candidate.target_id)
    : candidate.target_id
}

function configBadge(candidate: CandidateView) {
  if (candidate.archived) {
    return (
      <MappedStatusBadge
        stateLabel={HEALTH_SUMMARY_LABELS.ARCHIVED}
        fallbackKey='Archived'
        data-testid='dim-config'
      />
    )
  }
  if (candidate.enabled) {
    return (
      <Badge variant='default' data-testid='dim-config'>
        Candidate Enabled
      </Badge>
    )
  }
  return (
    <MappedStatusBadge
      stateLabel={HEALTH_SUMMARY_LABELS.DISABLED}
      fallbackKey='Candidate Disabled'
      data-testid='dim-config'
    />
  )
}

function channelStateLabel(candidate: CandidateView) {
  if (!candidate.channel) return CHANNEL_STATE_LABELS.MISSING
  if (candidate.channel.enabled) return CHANNEL_STATE_LABELS.ENABLED
  return CHANNEL_STATE_LABELS.DISABLED
}
