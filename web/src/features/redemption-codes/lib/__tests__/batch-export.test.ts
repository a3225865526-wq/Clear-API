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
  buildBatchExportContent,
  buildBatchExportFilename,
} from '../batch-export'

const KEYS = ['key-one', 'key-two']

describe('buildBatchExportContent', () => {
  test('TXT is one code per line', () => {
    expect(buildBatchExportContent(KEYS, 'txt')).toBe('key-one\nkey-two\n')
  })

  test('CSV has a single code column (API returns keys only)', () => {
    expect(buildBatchExportContent(KEYS, 'csv')).toBe(
      'code\nkey-one\nkey-two\n'
    )
  })
})

describe('buildBatchExportFilename', () => {
  test('includes batch id, unused marker and date', () => {
    const filename = buildBatchExportFilename(
      7,
      'csv',
      new Date(2026, 9, 3)
    )
    expect(filename).toBe('redemption-batch-7-unused-20261003.csv')
  })
})
