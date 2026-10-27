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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { RedemptionsBatchCreateDialog } from '../redemptions-batch-create-dialog'

const create = vi.hoisted(() => vi.fn())
vi.mock('../../api', () => ({ createRedemptionBatch: create }))

async function fill(count = '100') {
  const user = userEvent.setup()
  render(
    <RedemptionsBatchCreateDialog
      open
      onOpenChange={vi.fn()}
      maxCount={100}
      onCreated={vi.fn()}
    />
  )
  await user.type(screen.getByLabelText('Product ID'), 'credit-1')
  await user.type(screen.getByLabelText('Approved product revision'), '4')
  await user.type(screen.getByLabelText('Batch Name'), 'stock')
  await user.type(screen.getByLabelText('Face Value (CNY)'), '1')
  const quantity = screen.getByLabelText('Count')
  await user.clear(quantity)
  await user.type(quantity, count)
  return user
}

describe('batch creation', () => {
  it('disables repeated creation while a request is pending', async () => {
    let finish!: (value: unknown) => void
    create.mockReturnValue(
      new Promise((resolve) => {
        finish = resolve
      })
    )
    const user = await fill()
    await user.click(screen.getByRole('button', { name: 'Create' }))
    const pending = screen.getByRole('button', { name: 'Creating...' })
    expect(pending).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled()
    await user.click(pending)
    expect(create).toHaveBeenCalledTimes(1)
    finish({
      success: false,
      message:
        'The product requires a valid approval for its current revision.',
    })
    await screen.findByRole('alert')
  })
  it('rejects fractional quantities instead of truncating and creating stock', async () => {
    const user = await fill('1.5')
    await user.click(screen.getByRole('button', { name: 'Create' }))
    expect(create).not.toHaveBeenCalled()
  })

  it('shows the backend reason without closing the form', async () => {
    create.mockResolvedValue({
      success: false,
      message:
        'The product requires a valid approval for its current revision.',
    })
    const user = await fill()
    await user.click(screen.getByRole('button', { name: 'Create' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'The product requires a valid approval for its current revision.'
    )
    expect(screen.getByLabelText('Batch Name')).toHaveValue('stock')
    expect(create).toHaveBeenCalledWith(
      expect.objectContaining({
        product_id: 'credit-1',
        product_revision: 4,
        face_value_cents: 100,
        count: 100,
        expired_time: 0,
      })
    )
  })
})
