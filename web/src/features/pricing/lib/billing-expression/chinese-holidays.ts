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
import calendar from '../../../../../../pkg/billingexpr/chinese_holidays.json'
import { BillingExpressionError } from './types'

/** Shared with the Go engine; weekends and makeup workdays are separate conditions. */
export function chineseHoliday(
  now: Date,
  timezone = calendar.timezone
): number {
  if (timezone.trim() !== calendar.timezone) {
    throw new BillingExpressionError({
      code: 'type',
      detail: 'cn_holiday requires Asia/Shanghai timezone',
    })
  }
  if (!Number.isFinite(now.getTime())) {
    throw new BillingExpressionError({
      code: 'type',
      detail: 'simulation time',
    })
  }
  // China has used UTC+8 throughout the calendar's supported modern years.
  const date = new Date(now.getTime() + 8 * 60 * 60 * 1000)
    .toISOString()
    .slice(0, 10)
  const year = date.slice(0, 4)
  const days = (calendar.years as Record<string, { dates: string[] }>)[year]
  if (!days) {
    throw new BillingExpressionError({
      code: 'missing_context',
      detail: `Chinese holiday calendar unavailable for ${year}`,
    })
  }
  return days.dates.includes(date) ? 1 : 0
}
