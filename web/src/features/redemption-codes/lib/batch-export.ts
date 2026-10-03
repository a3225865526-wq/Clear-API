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
// Store batch code export: builds TXT/CSV content in the browser from keys
// fetched live from the server. Codes are sensitive — never log, persist or
// put them into URLs.

export type BatchExportFormat = 'txt' | 'csv'

export function buildBatchExportContent(
  keys: string[],
  format: BatchExportFormat
): string {
  if (format === 'csv') {
    const rows = ['code', ...keys]
    return `${rows.join('\n')}\n`
  }
  return `${keys.join('\n')}\n`
}

export function buildBatchExportFilename(
  batchId: number,
  format: BatchExportFormat,
  now: Date = new Date()
): string {
  const y = now.getFullYear()
  const m = String(now.getMonth() + 1).padStart(2, '0')
  const d = String(now.getDate()).padStart(2, '0')
  return `redemption-batch-${batchId}-unused-${y}${m}${d}.${format}`
}

export function downloadTextFile(
  filename: string,
  content: string,
  mime: string
): void {
  const blob = new Blob([content], { type: mime })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}
