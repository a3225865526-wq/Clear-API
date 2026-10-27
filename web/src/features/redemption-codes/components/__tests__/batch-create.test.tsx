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
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { RedemptionsBatchCreateDialog } from '../redemptions-batch-create-dialog'

const create = vi.hoisted(() => vi.fn())
const products = vi.hoisted(() => vi.fn())
const download = vi.hoisted(() => vi.fn())
vi.mock('../../lib/batch-export', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/batch-export')>()),
  downloadTextFile: download,
}))
beforeEach(() => {
  download.mockReset()
})
vi.mock('../../api', () => ({
  createRedemptionBatch: create,
  getRedemptionBatchProducts: products,
}))

async function fill(count = '100') {
  products.mockResolvedValue({
    success: true,
    data: [
      {
        id: 'credit-1',
        name: 'API credit',
        revision: 4,
        price_cents: 100,
        base_quota: 500000,
        evidence_batch_id: 3,
        delivery_binding: 'qmcse2',
        state: 'published',
        stock_ready: true,
        sample_ready: false,
      },
    ],
  })
  const user = userEvent.setup()
  render(
    <RedemptionsBatchCreateDialog
      open
      onOpenChange={vi.fn()}
      maxCount={100}
      onCreated={vi.fn()}
    />
  )
  await screen.findByRole('option', { name: /API credit/ })
  await user.selectOptions(
    screen.getByRole('combobox', { name: 'Product ID' }),
    'credit-1'
  )
  await user.click(screen.getByRole('button', { name: 'Advanced settings' }))
  expect(screen.getByLabelText('Approved product revision')).toHaveValue('4')
  expect(screen.getByLabelText('Approved product revision')).toHaveAttribute(
    'readonly'
  )
  await user.clear(screen.getByLabelText('Batch Name'))
  await user.type(screen.getByLabelText('Batch Name'), 'stock')
  expect(screen.getByLabelText('Face Value (CNY)')).toHaveValue('¥1.00')
  expect(screen.getByLabelText('Face Value (CNY)')).toHaveAttribute('readonly')
  const quantity = screen.getByLabelText('Count')
  await user.clear(quantity)
  await user.type(quantity, count)
  return user
}

describe('batch creation', () => {
  it('keeps draft revisions unavailable for normal stock creation', async () => {
    products.mockResolvedValue({
      success: true,
      data: [
        {
          id: 'credit-1',
          name: 'Draft credit',
          revision: 5,
          price_cents: 100,
          base_quota: 500000,
          evidence_batch_id: 4,
          delivery_binding: 'qmcse2',
          state: 'draft',
          stock_ready: false,
          sample_ready: true,
        },
      ],
    })
    render(
      <RedemptionsBatchCreateDialog
        open
        onOpenChange={vi.fn()}
        maxCount={100}
        onCreated={vi.fn()}
      />
    )
    expect(
      await screen.findByRole('option', { name: /Draft credit/ })
    ).toBeDisabled()
    expect(
      screen.getByRole('button', { name: 'Generate and download' })
    ).toBeDisabled()
    expect(create).not.toHaveBeenCalled()
  })
  it('explains the name length error before submitting an empty name', async () => {
    const user = await fill()
    await user.clear(screen.getByLabelText('Batch Name'))
    await user.click(
      screen.getByRole('button', { name: 'Generate and download' })
    )
    expect(screen.getByRole('alert')).toHaveTextContent(
      'Name must be between 1 and 64 characters'
    )
    expect(create).not.toHaveBeenCalled()
  })
  it('disables repeated creation while a request is pending', async () => {
    let finish!: (value: unknown) => void
    create.mockReturnValue(
      new Promise((resolve) => {
        finish = resolve
      })
    )
    const user = await fill()
    await user.click(
      screen.getByRole('button', { name: 'Generate and download' })
    )
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
    await user.click(
      screen.getByRole('button', { name: 'Generate and download' })
    )
    expect(create).not.toHaveBeenCalled()
  })

  it('shows the backend reason without closing the form', async () => {
    create.mockResolvedValue({
      success: false,
      message:
        'The product requires a valid approval for its current revision.',
    })
    const user = await fill()
    await user.click(
      screen.getByRole('button', { name: 'Generate and download' })
    )
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

it('downloads the newly created batch directly without an extra export step', async () => {
  create.mockResolvedValue({
    success: true,
    data: { batch: { id: 42, face_value_cents: 100 }, keys: ['test-code-one'] },
  })
  const user = await fill('1')
  await user.click(
    screen.getByRole('button', { name: 'Generate and download' })
  )
  await screen.findByRole('button', { name: 'Generate and download' })
  expect(download).toHaveBeenCalledWith(
    expect.stringContaining('redemption-batch-42-unused-'),
    'test-code-one\n',
    'text/plain;charset=utf-8'
  )
  expect(create).toHaveBeenCalledTimes(1)
  expect(create).toHaveBeenCalledWith(
    expect.objectContaining({ retry_safe: true })
  )
})

it('returns the existing batch for export when the browser download fails', async () => {
  download.mockImplementation(() => {
    throw new Error('browser download failed')
  })
  create.mockResolvedValue({
    success: true,
    data: { batch: { id: 43, face_value_cents: 100 }, keys: ['test-code-two'] },
  })
  products.mockResolvedValue({
    success: true,
    data: [
      {
        id: 'credit-1',
        name: 'API credit',
        revision: 4,
        price_cents: 100,
        base_quota: 500000,
        stock_ready: true,
      },
    ],
  })
  const onCreated = vi.fn()
  render(
    <RedemptionsBatchCreateDialog
      open
      onOpenChange={vi.fn()}
      maxCount={100}
      onCreated={onCreated}
    />
  )
  const user = userEvent.setup()
  await user.selectOptions(
    await screen.findByRole('combobox', { name: 'Product ID' }),
    'credit-1'
  )
  await user.click(
    screen.getByRole('button', { name: 'Generate and download' })
  )
  await waitFor(() => expect(onCreated).toHaveBeenCalledWith(43, 1, false))
  expect(create).toHaveBeenCalledTimes(1)
})
