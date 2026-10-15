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

// Mirrors of the Stage 6 operator console API payloads
// (controller/route_chain_management.go). Types only — the backend remains
// the authority for every state described here.

export interface GateStates {
  route_chain_enabled: boolean
  circuit_breaker_enabled: boolean
  max_candidates: number
  overall_deadline_ms: number
}

export interface RouteIssue {
  code: string
  candidate_id?: number
  target_id?: number
}

export interface TargetInfo {
  id: number
  name: string
  type: number
  type_name: string
  status: number
  enabled: boolean
  group: string
  models: string[]
  pool_bound: boolean
  source: 'pool' | 'api'
}

export interface CircuitSnapshot {
  state: string
  consecutive_failures: number
  failure_threshold: number
  cooldown_seconds: number
  opened_at: number
  open_until: number
  half_open_active_probes: number
  half_open_max_probes: number
  forced: boolean
  last_success_at: number
  last_failure_at: number
  last_failure_class: string
  last_failure_code: string
  last_health_outcome: string
  counters: Record<string, number>
}

export interface EffectiveBreakerView {
  failure_threshold: number
  failure_threshold_override: boolean
  cooldown_seconds: number
  cooldown_override: boolean
  half_open_max_probes: number
  half_open_probes_override: boolean
  retry_after_max: number
  retry_after_override: boolean
}

export interface PoolAuthorityView {
  master_enabled: boolean
  channel_routing: boolean
  publication: string
}

export interface CandidateView {
  id: number
  chain_id: number
  candidate_type: 'pool' | 'api_channel'
  target_id: number
  archived: boolean
  archived_target_id?: number
  priority: number
  enabled: boolean
  policy: string
  channel: TargetInfo | null
  model_served: boolean
  pricing_configured: boolean
  source_match: boolean
  pool_authority?: PoolAuthorityView
  runtime: CircuitSnapshot
  health_summary: string
  effective_breaker: EffectiveBreakerView
}

export interface ChainDetail {
  id: number
  model: string
  group: string
  enabled: boolean
  allow_cross_source: boolean
  archived: boolean
  version: number
  created_time: number
  updated_time: number
  gates: GateStates
  candidates: CandidateView[]
  archived_candidates: CandidateView[]
}

export interface ChainListItem {
  id: number
  model: string
  group: string
  enabled: boolean
  archived: boolean
  allow_cross_source: boolean
  candidate_count: number
  primary_candidate: {
    candidate_id: number
    type: string
    target_id: number
  } | null
  fallback_count: number
  overall_health: string
  version: number
  updated_time: number
}

export interface AttemptView {
  attempt_id: string
  request_id: string
  sequence: number
  candidate_id: number
  candidate_type: string
  channel_id: number
  started_at: number
  finished_at: number
  dispatch_state: string
  terminal_state: string
  final_outcome: string
  http_status: number
  failure_class: string
  failure_code: string
  upstream_response_id: string
  usage_state: string
  user_billing_state: string
  provider_cost_state: string
  provider_cost_amount: string
  provider_cost_currency: string
  health_outcome: string
  breaker_effect: string
  stream_completion: string
  outcome_uncertain: boolean
  safe_account_ref: string
}

export interface DecisionView {
  id: number
  request_id: string
  candidate_id: number
  phase: string
  decision: string
  reason: string
  outcome: string
  at: number
}

export interface CandidateSpecInput {
  id?: number
  candidate_type: 'pool' | 'api_channel'
  target_id: number
  priority: number
  enabled: boolean
  policy: string
}

export interface ChainSpecInput {
  model: string
  group: string
  enabled: boolean
  allow_cross_source: boolean
  candidates: CandidateSpecInput[]
  expected_version?: number
}
