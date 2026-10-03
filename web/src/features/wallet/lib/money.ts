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
// ============================================================================
// CNY cents / basis points precision helpers (display + request boundary)
// ============================================================================
// All money math for the store recharge feature uses integers (CNY cents,
// basis points). Conversions happen only at the request/display boundary
// via these helpers — never float arithmetic on business values.

const CNY_FORMATTER = new Intl.NumberFormat('zh-CN', {
  style: 'currency',
  currency: 'CNY',
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
})

/** Format CNY cents (int) as ¥X,XXX.XX for display. */
export function formatCnyCents(cents: number): string {
  if (!Number.isFinite(cents)) return CNY_FORMATTER.format(0)
  return CNY_FORMATTER.format(Math.round(cents) / 100)
}

/**
 * Parse a user-entered CNY amount string ("100", "100.5", "100.50") into
 * integer cents. Returns null for invalid input: empty, negative, more than
 * two decimal places, or non-numeric characters.
 */
export function parseCnyToCents(input: string): number | null {
  const trimmed = input.trim()
  if (!trimmed) return null
  if (!/^\d+(\.\d{1,2})?$/.test(trimmed)) return null
  const dotIndex = trimmed.indexOf('.')
  if (dotIndex === -1) {
    return Number(trimmed) * 100
  }
  const yuanPart = trimmed.slice(0, dotIndex)
  const centsPart = trimmed.slice(dotIndex + 1).padEnd(2, '0')
  return Number(yuanPart) * 100 + Number(centsPart)
}

/** Format basis points (int) as a percentage string, e.g. 250 -> "2.5%". */
export function formatBasisPoints(basisPoints: number): string {
  if (!Number.isFinite(basisPoints)) return '0%'
  const percent = Math.round(basisPoints) / 100
  return `${percent}%`
}

/**
 * Parse a user-entered percentage ("2", "2.5", "2.55") into integer basis
 * points (10000 = 100%). Returns null for invalid input: empty, negative,
 * over 100, or more than two decimal places.
 */
export function parsePercentToBasisPoints(input: string): number | null {
  const trimmed = input.trim()
  if (!trimmed) return null
  if (!/^\d+(\.\d{1,2})?$/.test(trimmed)) return null
  const dotIndex = trimmed.indexOf('.')
  const whole = dotIndex === -1 ? trimmed : trimmed.slice(0, dotIndex)
  const frac = dotIndex === -1 ? '0' : trimmed.slice(dotIndex + 1).padEnd(2, '0')
  if (Number(whole) > 100) return null
  const basisPoints = Number(whole) * 100 + Number(frac)
  if (basisPoints > 10000) return null
  return basisPoints
}
