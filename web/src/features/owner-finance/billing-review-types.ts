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
import z from 'zod'

export type BillingDecision = {
  id: number
  actor_id: number
  created_at: number
  original_state: string
  applied: number
  refund_quota: number
  final_quota: number
  reason: string
  token_refund: string
}
export type BillingReservation = {
  id: number
  request_id: string
  user_id: number
  token_id: number
  state: string
  reserved: number
  target: number
  applied: number
  updated_at: number
  reason: string
  executing: boolean
  decision?: BillingDecision
}

export function billingDecisionSchema(applied: number) {
  return z.object({
    refund: z
      .string()
      .trim()
      .refine(
        (text) =>
          /^(0|[1-9]\d*)$/.test(text) &&
          Number.isSafeInteger(Number(text)) &&
          Number(text) <= applied,
        'Enter whole quota units between zero and the charged quota.'
      ),
    reason: z
      .string()
      .trim()
      .min(1, 'Provide the reconciliation evidence.')
      .refine(
        (text) => new TextEncoder().encode(text).length <= 500,
        'Reconciliation evidence is too long.'
      ),
    ended: z
      .boolean()
      .refine((value) => value, 'Confirm that the request has ended.'),
  })
}
export type BillingDecisionValues = z.infer<
  ReturnType<typeof billingDecisionSchema>
>
