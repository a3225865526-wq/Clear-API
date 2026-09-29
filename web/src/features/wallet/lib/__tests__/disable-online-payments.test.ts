/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful, but WITHOUT
ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or
FITNESS FOR A PARTICULAR PURPOSE. See the GNU Affero General Public License
for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { describe, expect, it } from 'vitest'

import type { TopupInfo } from '../../types'
import { disableOnlinePayments } from '../disable-online-payments'

describe('disableOnlinePayments', () => {
  it('removes checkout options while keeping redemption settings', () => {
    const source: TopupInfo = {
      enable_online_topup: true,
      enable_stripe_topup: true,
      pay_methods: [{ type: 'alipay', name: 'Alipay' }],
      min_topup: 10,
      stripe_min_topup: 10,
      amount_options: [10, 20],
      discount: {},
      topup_link: 'https://example.invalid/buy',
      enable_creem_topup: true,
      creem_products: [
        {
          productId: 'product-1',
          name: 'Test',
          price: 10,
          quota: 10,
          currency: 'USD',
        },
      ],
      enable_waffo_topup: true,
      waffo_pay_methods: [{ payMethodType: 'alipay', name: 'Alipay' }],
      waffo_min_topup: 10,
      enable_waffo_pancake_topup: true,
      enable_redemption: true,
      payment_compliance_confirmed: true,
    }

    const result = disableOnlinePayments(source)

    expect(result).toMatchObject({
      enable_online_topup: false,
      enable_stripe_topup: false,
      pay_methods: [],
      enable_creem_topup: false,
      creem_products: [],
      enable_waffo_topup: false,
      waffo_pay_methods: [],
      enable_waffo_pancake_topup: false,
      enable_redemption: true,
      min_topup: 10,
    })
    expect(result?.topup_link).toBeUndefined()
    expect(source.enable_online_topup).toBe(true)
    expect(source.pay_methods).toHaveLength(1)
  })

  it('keeps a missing wallet configuration as an empty state', () => {
    expect(disableOnlinePayments(null)).toBeNull()
  })
})
