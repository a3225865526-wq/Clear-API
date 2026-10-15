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
import { describe, expect, test } from 'vitest'

import {
  buildPolicyJson,
  candidateToFormValues,
  filterTargetsByType,
  hasDuplicateTargets,
  hasMixedCandidateTypes,
  parsePolicyForm,
} from '../chain-policy'
import type { CandidateView } from '../../types'

describe('chainEditorFormSchema input logic (via pure helpers)', () => {
  test('F9: mixed candidate types are detected', () => {
    expect(
      hasMixedCandidateTypes([
        { candidate_type: 'pool' },
        { candidate_type: 'api_channel' },
      ])
    ).toBe(true)
    expect(
      hasMixedCandidateTypes([
        { candidate_type: 'api_channel' },
        { candidate_type: 'api_channel' },
      ])
    ).toBe(false)
  })

  test('F8: target filtering matches the pool binding, not the raw type', () => {
    const targets = [
      { id: 21, pool_bound: false },
      { id: 22, pool_bound: true },
    ]
    expect(filterTargetsByType(targets, 'pool').map((t) => t.id)).toEqual([22])
    expect(filterTargetsByType(targets, 'api_channel').map((t) => t.id)).toEqual([21])
  })

  test('duplicate targets inside one chain are detected', () => {
    expect(hasDuplicateTargets([{ target_id: 21 }, { target_id: 21 }])).toBe(true)
    expect(hasDuplicateTargets([{ target_id: 21 }, { target_id: 22 }])).toBe(false)
    expect(hasDuplicateTargets([{ target_id: 0 }, { target_id: 0 }])).toBe(false)
  })
})

describe('candidate policy form ↔ JSON', () => {
  test('builds health overrides and leaves defaults empty', () => {
    const json = buildPolicyJson({
      failure_threshold: 5,
      cooldown_seconds: 120,
    })
    expect(JSON.parse(json)).toEqual({
      health_policy: { failure_threshold: 5, cooldown_seconds: 120 },
    })
  })

  test('failover_on_429 only serializes when enabled; failover_on_404 only when disabled', () => {
    expect(buildPolicyJson({ failover_on_429: true })).toBe(
      JSON.stringify({ failure_policy: { failover_on_429: true } })
    )
    expect(buildPolicyJson({ failover_on_404: false })).toBe(
      JSON.stringify({ failure_policy: { failover_on_404: false } })
    )
    expect(buildPolicyJson({ failover_on_429: false, failover_on_404: true })).toBe('')
  })

  test('parse reads an existing candidate policy back into form values', () => {
    const values = parsePolicyForm(
      '{"health_policy":{"failure_threshold":5},"failure_policy":{"failover_on_429":true}}'
    )
    expect(values).toEqual({
      failure_threshold: 5,
      cooldown_seconds: undefined,
      failover_on_429: true,
      failover_on_404: undefined,
      connect_timeout_ms: undefined,
    })
    expect(parsePolicyForm('')).toEqual({})
    expect(parsePolicyForm('not json')).toEqual({})
  })

  test('candidateToFormValues uses the archived target for archived rows', () => {
    const candidate = {
      id: 7,
      candidate_type: 'api_channel',
      target_id: -7,
      archived: true,
      archived_target_id: 21,
      enabled: false,
      policy: '{"health_policy":{"failure_threshold":5}}',
    } as unknown as CandidateView
    const values = candidateToFormValues(candidate)
    expect(values.target_id).toBe(21)
    expect(values.failure_threshold).toBe(5)
    expect(values.enabled).toBe(false)
  })
})
