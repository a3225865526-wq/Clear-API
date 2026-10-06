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
import type { StatusVariant } from '@/components/status-badge'

// The five display dimensions (design §2.1): persistent config, target
// channel state, eligibility, pool authority and runtime health are separate
// concepts — they are NEVER folded into one label, and every state carries
// text (§二十一), not just color.

export interface StateLabel {
  labelKey: string
  variant: StatusVariant | null
}

/** Per-candidate runtime/presentation summary from the backend (health_summary). */
export const HEALTH_SUMMARY_LABELS: Record<string, StateLabel> = {
  HEALTHY: { labelKey: 'Healthy', variant: 'success' },
  DEGRADED: { labelKey: 'Degraded', variant: 'warning' },
  CIRCUIT_OPEN: { labelKey: 'Circuit Open', variant: 'danger' },
  HALF_OPEN: { labelKey: 'Half Open (probing)', variant: 'warning' },
  DISABLED: { labelKey: 'Candidate Disabled', variant: 'neutral' },
  ARCHIVED: { labelKey: 'Archived', variant: 'neutral' },
  UNKNOWN: { labelKey: 'Unknown', variant: 'neutral' },
}

/** Chain-level presentation summary for the list (overall_health). */
export const OVERALL_HEALTH_LABELS: Record<string, StateLabel> = {
  HEALTHY: { labelKey: 'Healthy', variant: 'success' },
  DEGRADED: { labelKey: 'Degraded', variant: 'warning' },
  CIRCUIT_OPEN: { labelKey: 'Circuit Open', variant: 'danger' },
  HALF_OPEN: { labelKey: 'Half Open (probing)', variant: 'warning' },
  CHAIN_DISABLED: { labelKey: 'Chain Disabled', variant: 'neutral' },
  NO_CANDIDATES: { labelKey: 'No Enabled Candidates', variant: 'warning' },
  ARCHIVED: { labelKey: 'Archived', variant: 'neutral' },
  UNKNOWN: { labelKey: 'Unknown', variant: 'neutral' },
}

export const CHANNEL_STATE_LABELS: Record<string, StateLabel> = {
  ENABLED: { labelKey: 'Channel Enabled', variant: 'success' },
  DISABLED: { labelKey: 'Channel Disabled', variant: 'danger' },
  MISSING: { labelKey: 'Channel Missing', variant: 'danger' },
}

export const ELIGIBILITY_LABELS = {
  model_served: {
    ok: { labelKey: 'Model Served', variant: 'success' },
    bad: { labelKey: 'Model Not Served', variant: 'danger' },
  },
  pricing: {
    ok: { labelKey: 'Pricing Configured', variant: 'success' },
    bad: { labelKey: 'PRICING MISSING', variant: 'danger' },
  },
  source: {
    ok: { labelKey: 'Source Compatible', variant: 'success' },
    bad: { labelKey: 'Source Mismatch', variant: 'danger' },
  },
} as const

export const POOL_PUBLICATION_LABELS: Record<string, StateLabel> = {
  published: { labelKey: 'Model Published', variant: 'success' },
  unpublished: { labelKey: 'Model Unpublished', variant: 'danger' },
  paused: { labelKey: 'Model Source Paused', variant: 'warning' },
  api_source: { labelKey: 'Model Pinned To API Source', variant: 'danger' },
  unknown: { labelKey: 'Unknown', variant: 'neutral' },
}

export const CANDIDATE_TYPE_LABELS: Record<string, string> = {
  pool: 'Pool',
  api_channel: 'API Channel',
}

/** Stable backend issue codes → operator copy (§三十七). */
export const ROUTE_CHAIN_ISSUE_LABELS: Record<string, string> = {
  'route_chain.model_required': 'Model is required.',
  'route_chain.group_required': 'Group is required.',
  'route_chain.candidate_empty':
    'A route chain needs at least one candidate.',
  'route_chain.candidate_overflow':
    'A route chain can hold at most 10 candidates.',
  'route_chain.candidate_type_invalid':
    'Candidate type must be Pool or API Channel.',
  'route_chain.target_invalid': 'A candidate needs a target channel.',
  'route_chain.target_missing':
    'The selected target channel no longer exists.',
  'route_chain.type_target_mismatch':
    'Candidate type does not match the target channel (pool vs API).',
  'route_chain.duplicate_target':
    'The same target channel appears more than once.',
  'route_chain.duplicate_priority':
    'Two candidates share the same priority.',
  'route_chain.policy_invalid':
    'A candidate policy is invalid or out of bounds.',
  'route_chain.mixed_sources_without_cross_source':
    'This configuration mixes pool and API candidates, but cross-source failover is disabled. Enable cross-source failover explicitly or use a single source.',
  'route_chain.model_group_immutable':
    'Model and group cannot be changed after creation.',
  'route_chain.foreign_candidate':
    'One of the candidates belongs to another route chain.',
  'route_chain.pricing_missing':
    'User billing pricing is not configured for this model.',
  'route_chain.model_not_served':
    'A candidate channel does not serve this model in the chain group.',
  'route_chain.source_mismatch':
    'A candidate source does not match the model publication.',
  'route_chain.engine_attempt_cap':
    'More enabled candidates than the per-request attempt cap — extras stay unreachable.',
}

export const CONFLICT_MESSAGE_KEY =
  'This route chain changed while you were working. Please review the latest state and retry.'

export const HISTORY_DELETE_MESSAGE_KEY =
  'This route chain has attempt history. Archive or disable it instead of deleting.'

export const UNKNOWN_REVIEW_LABEL = 'Needs Review'
export const USAGE_UNAVAILABLE_LABEL = 'Usage Unavailable'
