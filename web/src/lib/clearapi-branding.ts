/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or (at your
option) any later version.

This program is distributed in the hope that it will be useful, but WITHOUT
ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or
FITNESS FOR A PARTICULAR PURPOSE. See the GNU Affero General Public License
for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
export const CLEARAPI_BRAND_NAME = '澄光 API / ClearAPI'
export const CLEARAPI_LOGO = '/clearapi.svg'

export function isDefaultNewApiName(name?: string | null): boolean {
  return !name?.trim() || name.trim().toLowerCase() === 'new api'
}

export function getClearApiSystemName(name?: string | null): string {
  const trimmedName = name?.trim()
  if (!trimmedName || /^(new[ -]?api|clearapi|澄光 API)( \/ ClearAPI)?$/i.test(trimmedName)) {
    return CLEARAPI_BRAND_NAME
  }
  return trimmedName
}

export function getClearApiLogo(logo?: string | null): string {
  const value = logo?.trim()
  if (!value || /^\/?(?:logo\.png|favicon\.ico)$/.test(value)) {
    return CLEARAPI_LOGO
  }
  return value
}

export function getClearApiDocsLink(link?: string | null): string | undefined {
  const value = link?.trim()
  if (!value) return undefined
  try {
    const url = new URL(value, 'https://clearapi.invalid')
    if (url.hostname === 'newapi.pro' || url.hostname.endsWith('.newapi.pro')) {
      return undefined
    }
  } catch {
    return undefined
  }
  return value
}
