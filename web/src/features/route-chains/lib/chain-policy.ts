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

import type { CandidateView } from '../types'

// Candidate policy form model ↔ policy JSON. Bounds mirror the backend's
// ParseRouteCandidatePolicy (model/route_chain.go); the backend re-validates
// authoritatively on save. There is deliberately NO 5xx failover switch —
// post-dispatch behavior stays owned by the no-replay safety layer (§十二).

export interface PolicyFormValues {
  failure_threshold?: number
  cooldown_seconds?: number
  failover_on_429?: boolean
  failover_on_404?: boolean
  connect_timeout_ms?: number
}

interface PolicyHealth {
  failure_threshold?: number
  cooldown_seconds?: number
}
interface PolicyFailure {
  failover_on_429?: boolean
  failover_on_404?: boolean
}
interface PolicyTimeout {
  connect_timeout_ms?: number
}
interface PolicyShape {
  health_policy?: PolicyHealth
  failure_policy?: PolicyFailure
  timeout_policy?: PolicyTimeout
  capabilities?: string[]
}

export function parsePolicyForm(policyJson: string): PolicyFormValues {
  if (!policyJson.trim()) return {}
  let parsed: PolicyShape
  try {
    parsed = JSON.parse(policyJson) as PolicyShape
  } catch {
    return {}
  }
  return {
    failure_threshold: parsed.health_policy?.failure_threshold,
    cooldown_seconds: parsed.health_policy?.cooldown_seconds,
    failover_on_429: parsed.failure_policy?.failover_on_429,
    failover_on_404: parsed.failure_policy?.failover_on_404,
    connect_timeout_ms: parsed.timeout_policy?.connect_timeout_ms,
  }
}

export function buildPolicyJson(values: PolicyFormValues): string {
  const policy: PolicyShape = {}
  const health: PolicyHealth = {}
  if (typeof values.failure_threshold === 'number' && values.failure_threshold > 0) {
    health.failure_threshold = values.failure_threshold
  }
  if (typeof values.cooldown_seconds === 'number' && values.cooldown_seconds > 0) {
    health.cooldown_seconds = values.cooldown_seconds
  }
  if (Object.keys(health).length > 0) policy.health_policy = health
  const failure: PolicyFailure = {}
  if (values.failover_on_429 === true) failure.failover_on_429 = true
  if (values.failover_on_404 === false) failure.failover_on_404 = false
  if (Object.keys(failure).length > 0) policy.failure_policy = failure
  if (
    typeof values.connect_timeout_ms === 'number' &&
    values.connect_timeout_ms > 0
  ) {
    policy.timeout_policy = { connect_timeout_ms: values.connect_timeout_ms }
  }
  return Object.keys(policy).length > 0 ? JSON.stringify(policy) : ''
}

/** Candidates beyond the engine's per-request attempt cap can never be reached. */
export function enabledCandidatesBeyondCap(
  candidates: { enabled: boolean }[],
  cap: number
): boolean {
  return candidates.filter((candidate) => candidate.enabled).length > cap
}

/** Mixed pool + API candidates require the explicit cross-source flag (§十一). */
export function hasMixedCandidateTypes(
  candidates: { candidate_type: string }[]
): boolean {
  const types = new Set(candidates.map((candidate) => candidate.candidate_type))
  return types.size > 1
}

/** Duplicate target channels inside one chain are blocked client-side too. */
export function hasDuplicateTargets(
  candidates: { target_id: number }[]
): boolean {
  const seen = new Set<number>()
  for (const candidate of candidates) {
    if (candidate.target_id <= 0) continue
    if (seen.has(candidate.target_id)) return true
    seen.add(candidate.target_id)
  }
  return false
}

/**
 * Client-side target filtering for the editor picker (design F8). UX only —
 * the backend re-validates the type/target pairing authoritatively.
 */
export function filterTargetsByType<T extends { pool_bound: boolean }>(
  targets: T[],
  type: 'pool' | 'api_channel'
): T[] {
  return targets.filter((target) =>
    type === 'pool' ? target.pool_bound : !target.pool_bound
  )
}

/** Parses an existing candidate row into editor form values. */
export function candidateToFormValues(candidate: CandidateView) {
  return {
    id: candidate.id,
    candidate_type: candidate.candidate_type,
    target_id: candidate.archived
      ? (candidate.archived_target_id ?? 0)
      : candidate.target_id,
    enabled: candidate.enabled,
    ...parsePolicyForm(candidate.policy),
  }
}
