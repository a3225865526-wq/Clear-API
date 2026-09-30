import { describe, expect, it } from 'vitest'

import { offlineOrderSchema, toOfflineOrderInput } from '../order-form'

describe('offline order form', () => {
  it('rejects an invalid order number before requesting code creation', () => {
    const result = offlineOrderSchema.safeParse({
      orderNo: 'bad order',
      moneyYuan: '12.34',
      quotaAmount: 1,
      count: 1,
      paidAt: '2026-09-30T12:00',
      expiresAt: '',
    })
    expect(result.success).toBe(false)
  })

  it('converts a paid order amount to integer cents', () => {
    const input = toOfflineOrderInput({
      orderNo: 'LOCAL-1',
      moneyYuan: '12.34',
      quotaAmount: 1,
      count: 2,
      paidAt: '2026-09-30T12:00',
      expiresAt: '',
    })
    expect(input.order_no).toBe('LOCAL-1')
    expect(input.money_cents).toBe(1234)
    expect(input.quota_per_code).toBeGreaterThan(0)
    expect(input.count).toBe(2)
  })
})
