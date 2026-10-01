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
import { toIntlLocale } from '@/i18n/languages'

// The shared currency helper accepts floats and the site's display currency.
// Finance amounts are frozen CNY decimals; preserve every decimal digit here.
export function financeMoney(
  value: string | null | undefined,
  language: string
) {
  if (value == null || !/^-?\d+(\.\d+)?$/.test(value)) return null
  const negative = value.startsWith('-')
  const [integer, fraction = ''] = value.replace(/^-/, '').split('.')
  const locale = toIntlLocale(language)
  const parts = new Intl.NumberFormat(locale).formatToParts(1.1)
  const separator = parts.find((part) => part.type === 'decimal')?.value ?? '.'
  const grouped = new Intl.NumberFormat(locale, {
    maximumFractionDigits: 0,
  }).format(BigInt(integer))
  const digits = fraction.padEnd(2, '0')
  return `CNY ${negative ? '−' : ''}${grouped}${separator}${digits}`
}
export function financeTime(value: number, language: string) {
  return new Intl.DateTimeFormat(toIntlLocale(language), {
    timeZone: 'Asia/Shanghai',
    dateStyle: 'short',
    timeStyle: 'short',
    hour12: false,
  }).format(new Date(value * 1000))
}
export function beijingInput(value: string) {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(value)) return Number.NaN
  const parsed = Date.parse(`${value}:00+08:00`)
  if (!Number.isFinite(parsed)) return Number.NaN
  // Date.parse normalizes invalid calendar dates; reject that normalization.
  const canonical = new Date(parsed + 8 * 3600_000).toISOString().slice(0, 16)
  return canonical === value ? parsed / 1000 : Number.NaN
}
export function validRange(from: number, to: number) {
  return (
    Number.isSafeInteger(from) &&
    Number.isSafeInteger(to) &&
    from > 0 &&
    to > from &&
    to - from <= 366 * 86400
  )
}
