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
// Front-end defense for third-party store product URLs. The backend remains
// the final authority; this check only prevents obviously unsafe targets
// (javascript:/data:/file:/ftp:/protocol-relative) from reaching window.open.

/**
 * Returns true when the URL is a well-formed http(s) URL. Production requires
 * https; plain http is only accepted for localhost / loopback hosts so local
 * development against a mall sandbox keeps working.
 */
export function isValidStoreUrl(raw: string): boolean {
  const trimmed = raw.trim()
  if (!trimmed) return false
  let parsed: URL
  try {
    parsed = new URL(trimmed)
  } catch {
    return false
  }
  if (parsed.protocol === 'https:') return true
  if (parsed.protocol === 'http:') {
    const host = parsed.hostname.toLowerCase()
    return host === 'localhost' || host === '127.0.0.1' || host === '::1'
  }
  return false
}

/**
 * Safe window.open wrapper: only http(s) URLs are opened, always with
 * noopener/noreferrer.
 */
export function openStoreUrl(raw: string): boolean {
  if (!isValidStoreUrl(raw)) return false
  window.open(raw.trim(), '_blank', 'noopener,noreferrer')
  return true
}
