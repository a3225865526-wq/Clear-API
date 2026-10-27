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

import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

const mockGetBatches = vi.fn()
const mockInspect = vi.fn()
const mockDelete = vi.fn()
vi.mock('../../api', () => ({
  getRedemptionBatches: (...args: unknown[]) => mockGetBatches(...args),
  disableRedemptionBatch: vi.fn(),
  createRedemptionBatch: vi.fn(),
  getRedemptionBatchCodes: vi.fn(),
  inspectRedemptionBatchDeletion: (...args: unknown[]) => mockInspect(...args),
  archiveRedemptionBatch: (...args: unknown[]) => mockDelete(...args),
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
  it('checks impact before confirmation, blocks duplicate submission and refreshes after archiving', async () => {
    const user = userEvent.setup()
    mockGetBatches.mockResolvedValue({
      success: true,
      data: { items: [batch({})], total: 1 },
    })
    mockInspect.mockResolvedValue({
      success: true,
      data: {
        batch: batch({}),
        total: 100,
        used: 23,
        unused: 77,
        disabled: 0,
        orders: 2,
        balance_entries: 23,
        finance_events: 1,
        rewards: 1,
        approval_events: 0,
        blocked: false,
      },
    })
    let finish!: (value: unknown) => void
    mockDelete.mockReturnValue(
      new Promise((resolve) => {
        finish = resolve
      })
    )
    const { RedemptionsBatchesTable } =
      await import('../redemptions-batches-table')
    render(<RedemptionsBatchesTable />)
    await user.click(await screen.findByRole('button', { name: 'Delete' }))
    const dialog = await screen.findByRole('alertdialog')
    expect(dialog).toHaveTextContent('20261003-100YUAN')
    expect(dialog).toHaveTextContent('#1')
    expect(dialog).toHaveTextContent('77')
    const confirm = screen.getByRole('button', { name: 'Delete Batch' })
    await user.click(confirm)
    expect(confirm).toBeDisabled()
    await user.click(confirm)
    expect(mockDelete).toHaveBeenCalledTimes(1)
    mockGetBatches.mockResolvedValue({
      success: true,
      data: { items: [], total: 0 },
    })
    finish({ success: true })
    await screen.findByText('No batches yet')
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    expect(mockInspect).toHaveBeenCalledWith(1)
  })

  it('refuses deleting current delivery evidence', async () => {
    mockGetBatches.mockResolvedValue({
      success: true,
      data: { items: [batch({})], total: 1 },
    })
    mockInspect.mockResolvedValue({
      success: true,
      data: {
        batch: batch({}),
        total: 100,
        used: 23,
        unused: 77,
        disabled: 0,
        orders: 0,
        balance_entries: 0,
        finance_events: 0,
        rewards: 0,
        approval_events: 1,
        blocked: true,
      },
    })
    const { RedemptionsBatchesTable } =
      await import('../redemptions-batches-table')
    render(<RedemptionsBatchesTable />)
    await userEvent.click(await screen.findByRole('button', { name: 'Delete' }))
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: 'Delete Batch' })
      ).toBeDisabled()
    )
    expect(mockDelete).not.toHaveBeenCalled()
  })
  it('renders realtime used/unused/disabled stats', async () => {
    mockGetBatches.mockResolvedValue({
      success: true,
      data: { items: [batch({})], total: 1, page: 1, page_size: 10 },
    })
    const { RedemptionsBatchesTable } =
      await import('../redemptions-batches-table')
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
    const { RedemptionsBatchesTable } =
      await import('../redemptions-batches-table')
    render(<RedemptionsBatchesTable />)
    expect(
      await screen.findByRole('button', { name: /Disable Batch/ })
    ).toBeDisabled()
  })
})
