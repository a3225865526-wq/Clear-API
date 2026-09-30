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
import type { TopupInfo } from '../types'

/**
 * Keep the wallet's redemption and account history features while the product
 * is running without online checkout.
 */
export function disableOnlinePayments(
  topupInfo: TopupInfo | null
): TopupInfo | null {
  if (!topupInfo) return null

  return {
    ...topupInfo,
    enable_online_topup: false,
    enable_stripe_topup: false,
    pay_methods: [],
    topup_link: undefined,
    enable_creem_topup: false,
    creem_products: [],
    enable_waffo_topup: false,
    waffo_pay_methods: [],
    enable_waffo_pancake_topup: false,
    enable_redemption: true,
  }
}
