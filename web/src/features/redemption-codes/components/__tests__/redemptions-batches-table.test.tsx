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

const mockGetBatches = vi.fn()
vi.mock('../../api', () => ({
  getRedemptionBatches: (...args: unknown[]) => mockGetBatches(...args),
  disableRedemptionBatch: vi.fn(),
  createRedemptionBatch: vi.fn(),
  getRedemptionBatchCodes: vi.fn(),
}))

function batch(overrides: Record<string, unknown>) {
  return {
    id: 1,
    name: '20261003-100YUAN',
    source: 'STORE',
    type: 'BALANCE',
    face_value_cents: 10000,
    quota: 6849315,
    count: 100,
    expired_time: 0,
    status: 1,
    note: '',
    created_by: 1,
    created_time: 1759473600,
    updated_time: 1759473600,
    used_count: 23,
    unused_count: 77,
    disabled_count: 0,
    ...overrides,
  }
}

describe('RedemptionsBatchesTable', () => {
  it('renders realtime used/unused/disabled stats', async () => {
    mockGetBatches.mockResolvedValue({
      success: true,
      data: { items: [batch({})], total: 1, page: 1, page_size: 10 },
    })
    const { RedemptionsBatchesTable } = await import(
      '../redemptions-batches-table'
    )
    render(<RedemptionsBatchesTable />)
    expect(await screen.findByText('20261003-100YUAN')).toBeInTheDocument()
    expect(screen.getByText('23')).toBeInTheDocument()
    expect(screen.getByText('77')).toBeInTheDocument()
    expect(screen.getByText('¥100.00')).toBeInTheDocument()
  })

  it('refuses disabling an already disabled batch', async () => {
    mockGetBatches.mockResolvedValue({
      success: true,
      data: {
        items: [batch({ status: 2, used_count: 100, unused_count: 0 })],
        total: 1,
        page: 1,
        page_size: 10,
      },
    })
    const { RedemptionsBatchesTable } = await import(
      '../redemptions-batches-table'
    )
    render(<RedemptionsBatchesTable />)
    expect(
      await screen.findByRole('button', { name: /Disable Batch/ })
    ).toBeDisabled()
  })
})
