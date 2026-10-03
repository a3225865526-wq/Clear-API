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

import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'

vi.mock('@/lib/format', () => ({
  formatQuota: (value: number) => `Q${value}`,
}))

describe('StoreRedeemSuccessDialog', () => {
  it('renders base, bonus, total and balance from the backend', async () => {
    const { StoreRedeemSuccessDialog } = await import(
      '../store-redeem-success-dialog'
    )
    const result = {
      base_quota: 6849315,
      bonus_quota: 136986,
      total_quota: 6986301,
      balance: 6986301,
      face_value_cents: 10000,
      vip_name: 'VIP1',
      vip_bonus_basis_points: 200,
      new_vip_name: 'VIP1',
    }
    render(<StoreRedeemSuccessDialog result={result} onOpenChange={() => {}} />)
    expect(screen.getAllByText('Q6849315').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Q136986').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Q6986301').length).toBeGreaterThan(0)
    expect(screen.queryByText(/Upgraded/)).not.toBeInTheDocument()
  })

  it('shows the upgrade badge when the level changed', async () => {
    const { StoreRedeemSuccessDialog } = await import(
      '../store-redeem-success-dialog'
    )
    const result = {
      base_quota: 1,
      bonus_quota: 2,
      total_quota: 3,
      balance: 4,
      face_value_cents: 10000,
      vip_name: 'VIP1',
      vip_bonus_basis_points: 200,
      new_vip_name: 'VIP2',
    }
    render(<StoreRedeemSuccessDialog result={result} onOpenChange={() => {}} />)
    expect(screen.getByText('Upgraded to VIP2')).toBeInTheDocument()
  })

  it('renders no dialog content without a result', async () => {
    const { StoreRedeemSuccessDialog } = await import(
      '../store-redeem-success-dialog'
    )
    const { container } = render(
      <StoreRedeemSuccessDialog result={null} onOpenChange={() => {}} />
    )
    expect(container.querySelector('[role="dialog"]')).toBeNull()
  })
})
