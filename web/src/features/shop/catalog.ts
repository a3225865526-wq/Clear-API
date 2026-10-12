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
export interface CreditProduct {
  id: string
  name: string
  description: string
  price_cents: number
  credit_cents: number
  purchase_url: string
  published: boolean
  delivery_verified: boolean
  sort: number
}

// Reviewed public product links only. No order or payment integration.
export const creditProducts: readonly CreditProduct[] = [
  1, 5, 10, 20, 50, 100, 500, 1000,
].map((face, sort) => ({
  id: `credit-${face}`,
  name: `ClearAPI $${face} API credit`,
  description: 'Platform API usage credit. Cannot be withdrawn as cash.',
  price_cents: face * 100,
  credit_cents: face * 100,
  purchase_url: '',
  published: false,
  delivery_verified: false,
  sort,
}))

export const legacyPurchaseUrl = 'https://catfk.com/item/qmcse2'
export const storePurchaseUrl = 'https://catfk.com/shop/YPMYXR7N'

export function trustedPurchaseUrl(raw: string): string | null {
  try {
    const url = new URL(raw)
    if (
      url.protocol !== 'https:' ||
      url.hostname !== 'catfk.com' ||
      url.port !== '' ||
      url.username !== '' ||
      url.password !== '' ||
      !/^\/item\/[a-zA-Z0-9]+$/.test(url.pathname) ||
      url.search ||
      url.hash
    ) {
      return null
    }
    return url.href
  } catch {
    return null
  }
}
