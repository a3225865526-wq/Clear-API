import { act, renderHook } from '@testing-library/react'
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
import { beforeEach, describe, expect, test, vi } from 'vitest'

const mockPost = vi.fn()
vi.mock('@/lib/api', () => ({
  api: {
    post: (...args: unknown[]) => mockPost(...args),
  },
}))
vi.mock('@/lib/handle-server-error', () => ({
  handleServerError: vi.fn(),
}))

describe('store redeem endpoints', () => {
  beforeEach(() => {
    mockPost.mockReset()
  })

  test('Legacy uses its original endpoint without falling back', async () => {
    mockPost.mockResolvedValue({
      data: { success: false, message: 'Compliance required' },
    })
    const { redeemLegacyCode } = await import('../../api')
    const result = await redeemLegacyCode({ key: 'synthetic-legacy' })
    expect(result.success).toBe(false)
    expect(mockPost).toHaveBeenCalledExactlyOnceWith('/api/user/topup', {
      key: 'synthetic-legacy',
    })
  })

  test('store codes go to the dedicated store endpoint', async () => {
    mockPost.mockResolvedValue({
      data: { success: true, data: { base_quota: 1 } },
    })
    const { redeemStoreCode } = await import('../../api')
    await redeemStoreCode({ key: 'store-1' })
    expect(mockPost).toHaveBeenCalledWith('/api/clearapi/store/redeem', {
      key: 'store-1',
    })
  })

  test('legacy offline redeem keeps its original endpoint', async () => {
    mockPost.mockResolvedValue({ data: { success: true, data: 1 } })
    const { redeemTopupCode } = await import('../../api')
    await redeemTopupCode({ key: 'offline-1' })
    expect(mockPost).toHaveBeenCalledWith('/api/clearapi/redeem', {
      key: 'offline-1',
    })
  })
})

describe('useStoreRedeem', () => {
  beforeEach(() => {
    mockPost.mockReset()
  })

  test('exposes the backend result on success', async () => {
    mockPost.mockResolvedValue({
      data: {
        success: true,
        data: {
          base_quota: 100,
          bonus_quota: 4,
          total_quota: 104,
          balance: 204,
          face_value_cents: 10000,
          vip_name: 'VIP1',
          vip_bonus_basis_points: 200,
          new_vip_name: 'VIP1',
        },
      },
    })
    const { useStoreRedeem } = await import('../use-store-redeem')
    const { result } = renderHook(() => useStoreRedeem())
    let ok = false
    await act(async () => {
      ok = await result.current.redeem('store-code')
    })
    expect(ok).toBe(true)
    expect(result.current.result?.total_quota).toBe(104)
  })

  test('does not fall back to the offline-order endpoint on failure', async () => {
    mockPost.mockResolvedValue({ data: { success: false, message: 'x' } })
    const { useStoreRedeem } = await import('../use-store-redeem')
    const { result } = renderHook(() => useStoreRedeem())
    let ok = true
    await act(async () => {
      ok = await result.current.redeem('bad-code')
    })
    expect(ok).toBe(false)
    const calls = mockPost.mock.calls.map((call) => call[0])
    expect(calls).toEqual(['/api/clearapi/store/redeem'])
  })
})
