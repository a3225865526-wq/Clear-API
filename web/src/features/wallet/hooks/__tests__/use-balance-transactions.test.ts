import { renderHook, waitFor } from '@testing-library/react'
import { expect, test, vi } from 'vitest'

const mockGet = vi.fn()
vi.mock('@/lib/api', () => ({
  api: { get: (...args: unknown[]) => mockGet(...args) },
}))

test('opening balance details reloads transactions created since the last opening', async () => {
  mockGet.mockResolvedValueOnce({
    data: { success: true, data: { items: [], total: 0 } },
  })
  const { useBalanceTransactions } = await import('../use-balance-transactions')
  const { result, rerender } = renderHook(
    ({ open }) => useBalanceTransactions(open),
    { initialProps: { open: false } }
  )
  expect(mockGet).not.toHaveBeenCalled()
  rerender({ open: true })
  await waitFor(() => expect(mockGet).toHaveBeenCalledTimes(1))
  await waitFor(() => expect(result.current.loading).toBe(false))
  rerender({ open: false })
  const recharge = {
    id: 1,
    type: 'RECHARGE',
    amount_quota: 684931,
    recharge_cents: 1000,
  }
  mockGet.mockResolvedValueOnce({
    data: { success: true, data: { items: [recharge], total: 1 } },
  })
  rerender({ open: true })
  await waitFor(() => expect(result.current.records).toEqual([recharge]))
  expect(result.current.total).toBe(1)
  expect(mockGet).toHaveBeenCalledTimes(2)
})
