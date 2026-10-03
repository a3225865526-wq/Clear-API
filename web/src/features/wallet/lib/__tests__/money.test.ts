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
  formatBasisPoints,
  formatCnyCents,
  parseCnyToCents,
  parsePercentToBasisPoints,
} from '../money'

describe('parseCnyToCents', () => {
  test('converts valid amounts to integer cents', () => {
    expect(parseCnyToCents('100')).toBe(10000)
    expect(parseCnyToCents('100.5')).toBe(10050)
    expect(parseCnyToCents('100.50')).toBe(10050)
    expect(parseCnyToCents('0.01')).toBe(1)
    expect(parseCnyToCents(' 2 ')).toBe(200)
  })

  test('rejects invalid amounts', () => {
    expect(parseCnyToCents('')).toBeNull()
    expect(parseCnyToCents('1.999')).toBeNull()
    expect(parseCnyToCents('-5')).toBeNull()
    expect(parseCnyToCents('abc')).toBeNull()
    expect(parseCnyToCents('1e3')).toBeNull()
  })
})

describe('parsePercentToBasisPoints', () => {
  test('converts percentages to basis points without float error', () => {
    expect(parsePercentToBasisPoints('2')).toBe(200)
    expect(parsePercentToBasisPoints('2.5')).toBe(250)
    expect(parsePercentToBasisPoints('2.55')).toBe(255)
    expect(parsePercentToBasisPoints('100')).toBe(10000)
    expect(parsePercentToBasisPoints('0')).toBe(0)
  })

  test('rejects invalid or out-of-range percentages', () => {
    expect(parsePercentToBasisPoints('')).toBeNull()
    expect(parsePercentToBasisPoints('100.01')).toBeNull()
    expect(parsePercentToBasisPoints('101')).toBeNull()
    expect(parsePercentToBasisPoints('-1')).toBeNull()
    expect(parsePercentToBasisPoints('2.555')).toBeNull()
  })
})

describe('formatCnyCents', () => {
  test('formats cents as CNY currency', () => {
    expect(formatCnyCents(10000)).toBe('¥100.00')
    expect(formatCnyCents(190000)).toBe('¥1,900.00')
    expect(formatCnyCents(0)).toBe('¥0.00')
  })
})

describe('formatBasisPoints', () => {
  test('formats basis points as percentage', () => {
    expect(formatBasisPoints(200)).toBe('2%')
    expect(formatBasisPoints(250)).toBe('2.5%')
    expect(formatBasisPoints(0)).toBe('0%')
    expect(formatBasisPoints(10000)).toBe('100%')
  })
})
