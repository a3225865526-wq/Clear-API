import * as z from 'zod'

import { parseQuotaFromDollars } from '@/lib/format'

export const offlineOrderSchema = z.object({
  orderNo: z.string().regex(/^[A-Za-z0-9_-]{1,100}$/),
  moneyYuan: z.string().regex(/^\d+(\.\d{1,2})?$/),
  quotaAmount: z.number().positive(),
  count: z.number().int().min(1).max(100),
  paidAt: z.string().min(1),
  expiresAt: z.string(),
})

export type OfflineOrderValues = z.infer<typeof offlineOrderSchema>

export function toOfflineOrderInput(values: OfflineOrderValues) {
  const quota = parseQuotaFromDollars(values.quotaAmount)
  const moneyCents = Math.round(Number(values.moneyYuan) * 100)
  if (!Number.isSafeInteger(moneyCents) || moneyCents <= 0 || quota <= 0) {
    throw new Error('Invalid order amount')
  }
  const paidAt = Math.floor(new Date(values.paidAt).getTime() / 1000)
  const expiredTime = values.expiresAt
    ? Math.floor(new Date(values.expiresAt).getTime() / 1000)
    : 0
  if (
    !Number.isFinite(paidAt) ||
    (expiredTime !== 0 && expiredTime < Date.now() / 1000)
  ) {
    throw new Error('Invalid order dates')
  }
  return {
    order_no: values.orderNo,
    money_cents: moneyCents,
    quota_per_code: quota,
    count: values.count,
    paid_at: paidAt,
    expired_time: expiredTime,
  }
}
