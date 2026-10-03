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
  formatNumber: (value: number) => `N${value}`,
  formatTimestampToDate: (value: number) => `T${value}`,
}))

const mockGet = vi.fn()
vi.mock('@/lib/api', () => ({
  api: { get: (...args: unknown[]) => mockGet(...args) },
}))

function pagePayload(page: number) {
  return {
    data: {
      success: true,
      data: {
        items: [
          {
            id: (page - 1) * 10 + 1,
            user_id: 7,
            created_at: 1759473600,
            type: page === 1 ? 'RECHARGE' : 'BONUS',
            amount_quota: page === 1 ? 6849315 : 136986,
            balance_before: 0,
            balance_after: 6986301,
            recharge_cents: page === 1 ? 10000 : 0,
            reference_type: 'REDEMPTION',
            reference_id: 42,
            description: page === 1 ? 'redeem-recharge' : 'vip-bonus',
            code_tail: 'abcd',
            metadata: '{"code_tail":"abcd"}',
          },
        ],
        total: 11,
        page,
        page_size: 10,
      },
    },
  }
}

describe('BalanceTransactionsDialog', () => {
  it('renders DTO fields and never leaks reference_id/metadata', async () => {
    mockGet.mockResolvedValue(pagePayload(1))
    const { BalanceTransactionsDialog } = await import(
      '../balance-transactions-dialog'
    )
    render(<BalanceTransactionsDialog open onOpenChange={() => {}} />)
    expect(await screen.findByText('Recharge')).toBeInTheDocument()
    expect(screen.getAllByText(/Q6849315/).length).toBeGreaterThan(0)
    expect(screen.getByText('¥100.00')).toBeInTheDocument()
    expect(screen.getByText(/····abcd/)).toBeInTheDocument()
    expect(screen.queryByText('REDEMPTION')).not.toBeInTheDocument()
    expect(screen.queryByText(/code_tail/)).not.toBeInTheDocument()
    expect(mockGet.mock.calls[0][0]).toContain(
      '/api/user/balance_transactions/self'
    )
  })
})
