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
import { z } from 'zod'

import { trustedPurchaseUrl } from '../catalog'

export const productSchema = z
  .object({
    id: z.string().regex(/^[a-zA-Z0-9_-]{1,64}$/),
    name: z.string().min(1).max(128),
    description: z.string().max(2000),
    price_cents: z.number().int().positive().max(1000000000),
    credit_cents: z.number().int().nonnegative().max(1000000000),
    purchase_url: z.string(),
    published: z.boolean(),
    archived: z.boolean().optional(),
    delivery_verified: z.boolean(),
    sort: z.number().int(),
    currency: z.literal('CNY').optional(),
    revision: z.number().int().nonnegative().optional(),
    state: z.string().optional(),
    edited_by: z.number().int().optional(),
    approval_id: z.number().int().optional(),
    legacy_store_id: z.number().int().optional(),
    legacy_withdrawn: z.boolean().optional(),
    evidence_batch_id: z.number().int().nonnegative().optional(),
    delivery_evidence: z.string().max(256).optional(),
    delivery_binding: z.string().max(256).optional(),
    base_quota: z
      .number()
      .int()
      .nonnegative()
      .max(Number.MAX_SAFE_INTEGER)
      .optional(),
  })
  .refine(
    (p) => !p.purchase_url || trustedPurchaseUrl(p.purchase_url) !== null,
    { path: ['purchase_url'], message: 'Invalid store product link' }
  )
  .refine((p) => !p.published || (p.delivery_verified && !!p.purchase_url), {
    path: ['published'],
    message: 'Verify delivery and purchase URL before publishing',
  })

export const catalogSchema = z
  .object({
    version: z.number().int(),
    products: z.array(productSchema),
  })
  .refine(
    (c) => new Set(c.products.map((p) => p.id)).size === c.products.length,
    { path: ['products'], message: 'Product IDs must be unique' }
  )

// Parse human decimal amounts without binary floating point arithmetic.
export function decimalUnits(raw: string, units: number): number {
  if (!/^\d+(\.\d{1,6})?$/.test(raw)) return Number.NaN
  const [whole, fraction = ''] = raw.split('.')
  const scaled =
    ((BigInt(whole) * 1000000n + BigInt(fraction.padEnd(6, '0'))) *
      BigInt(units)) /
    1000000n
  if (scaled > BigInt(Number.MAX_SAFE_INTEGER)) return Number.NaN
  return Number(scaled)
}
