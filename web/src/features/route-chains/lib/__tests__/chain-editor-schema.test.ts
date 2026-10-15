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

import { chainEditorFormSchema } from '../chain-editor-schema'

function candidate(overrides: Record<string, unknown> = {}) {
  return {
    candidate_type: 'api_channel' as const,
    target_id: 21,
    enabled: true,
    ...overrides,
  }
}

const baseInput = {
  model: 'gpt-chain',
  group: 'default',
  enabled: true,
  allow_cross_source: false,
  candidates: [candidate()],
}

describe('F9: cross-source gate', () => {
  test('mixed pool and API candidates without the flag are rejected', () => {
    const result = chainEditorFormSchema.safeParse({
      ...baseInput,
      candidates: [
        candidate(),
        candidate({ candidate_type: 'pool', target_id: 22 }),
      ],
    })
    expect(result.success).toBe(false)
    if (!result.success) {
      const issue = result.error.issues.find(
        (item) => item.path[0] === 'allow_cross_source'
      )
      expect(issue).toBeDefined()
      expect(issue?.message).toContain('cross-source failover')
    }
  })

  test('mixed candidates with the explicit flag pass', () => {
    const result = chainEditorFormSchema.safeParse({
      ...baseInput,
      allow_cross_source: true,
      candidates: [
        candidate(),
        candidate({ candidate_type: 'pool', target_id: 22 }),
      ],
    })
    expect(result.success).toBe(true)
  })

  test('single-source chains never require the flag', () => {
    expect(chainEditorFormSchema.safeParse(baseInput).success).toBe(true)
  })
})

describe('validation bounds and structure', () => {
  test('empty model and empty candidate list are rejected', () => {
    const noModel = chainEditorFormSchema.safeParse({
      ...baseInput,
      model: ' ',
    })
    expect(noModel.success).toBe(false)
    const noCandidates = chainEditorFormSchema.safeParse({
      ...baseInput,
      candidates: [],
    })
    expect(noCandidates.success).toBe(false)
  })

  test('a missing target is rejected', () => {
    const result = chainEditorFormSchema.safeParse({
      ...baseInput,
      candidates: [candidate({ target_id: 0 })],
    })
    expect(result.success).toBe(false)
  })

  test('F6-adjacent: breaker bounds mirror the backend (threshold 1..20, cooldown 1..3600)', () => {
    const over = chainEditorFormSchema.safeParse({
      ...baseInput,
      candidates: [candidate({ failure_threshold: 99 })],
    })
    expect(over.success).toBe(false)
    const under = chainEditorFormSchema.safeParse({
      ...baseInput,
      candidates: [candidate({ cooldown_seconds: 0 })],
    })
    expect(under.success).toBe(false)
    const ok = chainEditorFormSchema.safeParse({
      ...baseInput,
      candidates: [candidate({ failure_threshold: 5, cooldown_seconds: 300 })],
    })
    expect(ok.success).toBe(true)
  })

  test('duplicate targets are rejected', () => {
    const result = chainEditorFormSchema.safeParse({
      ...baseInput,
      candidates: [candidate({ target_id: 21 }), candidate({ target_id: 21 })],
    })
    expect(result.success).toBe(false)
  })
})
