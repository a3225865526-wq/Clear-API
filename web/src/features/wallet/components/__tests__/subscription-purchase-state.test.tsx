import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
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
import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { SubscriptionPlansCard } from '../subscription-plans-card'

vi.mock('@/features/subscriptions/api', () => ({
  getPublicPlans: vi.fn(),
  getSelfSubscriptionFull: vi.fn(),
  updateBillingPreference: vi.fn(),
}))

describe('subscription purchase state', () => {
  beforeEach(async () => {
    const api = await import('@/features/subscriptions/api')
    vi.mocked(api.getPublicPlans).mockResolvedValue({
      success: true,
      data: [
        {
          plan: {
            id: 10,
            title: 'Local plan',
            price_amount: 5,
            currency: 'USD',
            duration_unit: 'month',
            duration_value: 1,
            quota_reset_period: 'never',
            enabled: true,
            sort_order: 1,
            allow_balance_pay: true,
            allow_wallet_overflow: true,
            max_purchase_per_user: 0,
            total_amount: 0,
          },
        },
      ],
    })
    vi.mocked(api.getSelfSubscriptionFull).mockResolvedValue({
      success: true,
      data: {
        billing_preference: 'wallet_first',
        subscriptions: [],
        all_subscriptions: [],
      },
    })
  })

  it('shows the configured plan but disables its purchase action when checkout is off', async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={queryClient}>
        <SubscriptionPlansCard topupInfo={null} purchaseEnabled={false} />
      </QueryClientProvider>
    )

    expect(await screen.findByText('Local plan')).toBeVisible()
    expect(
      screen.getByRole('button', {
        name: 'Online plan purchases are disabled',
      })
    ).toBeDisabled()
    expect(
      screen.queryByRole('button', { name: 'Subscribe Now' })
    ).not.toBeInTheDocument()
  })
})
