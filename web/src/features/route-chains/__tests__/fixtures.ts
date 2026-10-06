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
import type {
  AttemptView,
  CandidateView,
  ChainDetail,
  ChainListItem,
  DecisionView,
  GateStates,
} from '../types'

// Minimal explicit fixtures mirroring the backend payloads.

export const gatesOn: GateStates = {
  route_chain_enabled: true,
  circuit_breaker_enabled: true,
  max_candidates: 3,
  overall_deadline_ms: 0,
}

export const gatesOff: GateStates = {
  route_chain_enabled: false,
  circuit_breaker_enabled: false,
  max_candidates: 3,
  overall_deadline_ms: 0,
}

export function candidateView(
  overrides: Partial<CandidateView> = {}
): CandidateView {
  return {
    id: 1,
    chain_id: 1,
    candidate_type: 'api_channel',
    target_id: 21,
    archived: false,
    priority: 2,
    enabled: true,
    policy: '',
    channel: {
      id: 21,
      name: 'primary-api',
      type: 1,
      type_name: 'OpenAI',
      status: 1,
      enabled: true,
      group: 'default',
      models: ['gpt-chain'],
      pool_bound: false,
      source: 'api',
    },
    model_served: true,
    pricing_configured: true,
    source_match: true,
    runtime: {
      state: 'closed',
      consecutive_failures: 0,
      failure_threshold: 3,
      cooldown_seconds: 60,
      opened_at: 0,
      open_until: 0,
      half_open_active_probes: 0,
      half_open_max_probes: 1,
      forced: false,
      last_success_at: 0,
      last_failure_at: 0,
      last_failure_class: '',
      last_failure_code: '',
      last_health_outcome: '',
      counters: {},
    },
    health_summary: 'HEALTHY',
    effective_breaker: {
      failure_threshold: 3,
      failure_threshold_override: false,
      cooldown_seconds: 60,
      cooldown_override: false,
      half_open_max_probes: 1,
      half_open_probes_override: false,
      retry_after_max: 300,
      retry_after_override: false,
    },
    ...overrides,
  }
}

export function chainDetail(
  overrides: Partial<ChainDetail> = {}
): ChainDetail {
  return {
    id: 1,
    model: 'gpt-chain',
    group: 'default',
    enabled: true,
    allow_cross_source: false,
    archived: false,
    version: 4,
    created_time: 1759800000,
    updated_time: 1759800600,
    gates: gatesOn,
    candidates: [candidateView()],
    archived_candidates: [],
    ...overrides,
  }
}

export function chainListItem(
  overrides: Partial<ChainListItem> = {}
): ChainListItem {
  return {
    id: 1,
    model: 'gpt-chain',
    group: 'default',
    enabled: true,
    archived: false,
    allow_cross_source: false,
    candidate_count: 2,
    primary_candidate: { candidate_id: 1, type: 'api_channel', target_id: 21 },
    fallback_count: 1,
    overall_health: 'HEALTHY',
    version: 4,
    updated_time: 1759800600,
    ...overrides,
  }
}

export function attemptView(
  overrides: Partial<AttemptView> = {}
): AttemptView {
  return {
    attempt_id: 'att-1',
    request_id: 'req-1',
    sequence: 1,
    candidate_id: 1,
    candidate_type: 'api_channel',
    channel_id: 21,
    started_at: 1759800000,
    finished_at: 1759800005,
    dispatch_state: 'dispatched',
    terminal_state: 'SUCCEEDED',
    final_outcome: 'success',
    http_status: 200,
    failure_class: '',
    failure_code: '',
    upstream_response_id: '',
    usage_state: 'observed',
    user_billing_state: 'settled',
    provider_cost_state: 'KNOWN',
    provider_cost_amount: '',
    provider_cost_currency: '',
    health_outcome: 'health_success',
    breaker_effect: 'success',
    stream_completion: '',
    outcome_uncertain: false,
    safe_account_ref: '',
    ...overrides,
  }
}

export function decisionView(
  overrides: Partial<DecisionView> = {}
): DecisionView {
  return {
    id: 1,
    request_id: 'req-1',
    candidate_id: 2,
    phase: 'selection',
    decision: 'skipped',
    reason: 'circuit_open',
    outcome: '',
    at: 1759800000,
    ...overrides,
  }
}
