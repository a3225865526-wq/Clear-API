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
import { beforeEach, describe, expect, test } from 'vitest'

import zhLocale from '@/i18n/locales/zh.json'

// F26: the console's user-facing copy is registered for i18n. The zh locale
// must carry real translations (not the English fallback) for the core
// state vocabulary that protects operators from misreading states (§二十一).

const CORE_KEYS = [
  'Route Chains',
  'Circuit Open',
  'Half Open (probing)',
  'Candidate Disabled',
  'Channel Disabled',
  'PRICING MISSING',
  'Model Unpublished',
  'Pool Master',
  'Runtime Route Chain: OFF',
  'Circuit Breaker: OFF',
  'Route Chain production release gate is not approved',
  'Needs Review',
  'User Billing',
  'Provider Cost',
  'This route chain changed while you were working. Please review the latest state and retry.',
  'Mixed pool and API candidates require cross-source failover — confirm this is intended.',
]

describe('F26: i18n registration', () => {
  beforeEach(() => {
    expect(zhLocale).toBeDefined()
  })

  test('core state vocabulary has Chinese translations', () => {
    for (const key of CORE_KEYS) {
      const value = (zhLocale.translation as Record<string, string>)[key]
      expect(value, `missing zh translation for: ${key}`).toBeTruthy()
      expect(value, `untranslated key: ${key}`).not.toBe(key)
    }
  })

  test('the conflict copy warns about concurrent edits', () => {
    expect(
      (zhLocale.translation as Record<string, string>)[
        'This route chain changed while you were working. Please review the latest state and retry.'
      ]
    ).toContain('修改')
  })
})
