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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { creditProducts } from '@/features/shop/catalog'
import { CatalogEditor } from '@/features/shop/components/catalog-editor'
import { catalogSchema, decimalUnits } from '@/features/shop/lib/catalog-form'
import { api } from '@/lib/api'

import { ReferralPanel } from '..'
import { RewardSettings } from '../components/reward-settings'
import { formatReferralCredit } from '../lib/format-credit'

let client: QueryClient
function renderQuery(component: React.ReactNode) {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(<QueryClientProvider client={client}>{component}</QueryClientProvider>)
}
afterEach(() => {
  client?.clear()
  vi.restoreAllMocks()
})

const rule = {
  invites_enabled: true,
  rewards_enabled: false,
  mode: 'percentage',
  basis_points: 1000,
  fixed_quota: 500000,
  per_reward_cap: 50000000,
  lifetime_cap: 500000000,
  version: 1,
  epoch: 1,
}

it('shows only the current user invitation link and accurate small rewards', async () => {
  const link = 'https://www.clear999.com/sign-up?aff=stable-code'
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        code: 'stable-code',
        link,
        count: 3,
        paid_quota: 1,
        rule,
        total: 1,
        rewards: [
          {
            id: 1,
            reward_quota: 1,
            status: 'paid',
            created_time: 1700000000,
            paid_time: 1700000001,
          },
        ],
      },
    },
  })
  renderQuery(<ReferralPanel />)
  expect(await screen.findByLabelText('Your referral link')).toHaveValue(link)
  expect(
    screen.getByRole('button', { name: 'Copy referral link' })
  ).toBeEnabled()
  expect(screen.getByText(/Successful invitations.*3/)).toBeVisible()
  expect(screen.getByText(/Total Earned.*\$0\.000002/)).toBeVisible()
  expect(api.get).toHaveBeenCalledWith('/api/clearapi/referral/self?page=0')
  expect(screen.getByRole('button', { name: 'Next' })).toBeDisabled()
})

it('rejects an API business error instead of showing another user or empty success data', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: false, message: 'denied' },
  })
  renderQuery(<ReferralPanel />)
  expect(await screen.findByRole('button', { name: 'Retry' })).toBeEnabled()
  expect(screen.queryByLabelText('Your referral link')).not.toBeInTheDocument()
})

it('switches to fixed rewards and submits exactly one reward mode', async () => {
  vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: {
      success: true,
      data: String(url).includes('/settings')
        ? rule
        : { items: [], total: 0, summary: [], audits: [] },
    },
  }))
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  renderQuery(<RewardSettings />)
  await userEvent.click(
    await screen.findByRole('radio', { name: 'Fixed API credit reward' })
  )
  const fixed = screen.getByLabelText('Fixed reward in USD API credit')
  await userEvent.clear(fixed)
  await userEvent.type(fixed, '2')
  await userEvent.click(
    screen.getByRole('button', { name: 'Save reward settings' })
  )
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith(
      '/api/clearapi/referral/settings',
      expect.objectContaining({
        mode: 'fixed',
        fixed_quota: 1000000,
        rewards_enabled: false,
      })
    )
  )
  expect(screen.getByLabelText('Reward percentage')).toBeDisabled()
})

it('requires operational confirmation before submitting an enable change', async () => {
  vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: {
      success: true,
      data: String(url).includes('/settings')
        ? rule
        : { items: [], total: 0, summary: [], audits: [] },
    },
  }))
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  renderQuery(<RewardSettings />)
  await userEvent.click(
    await screen.findByRole('switch', { name: 'Enable referral rewards' })
  )
  await userEvent.click(
    screen.getByRole('button', { name: 'Save reward settings' })
  )
  expect(await screen.findByRole('alertdialog')).toBeVisible()
  expect(put).not.toHaveBeenCalled()
  await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  expect(put).not.toHaveBeenCalled()
})

it('loads eight persisted products and saves sorting and descriptions through the root endpoint', async () => {
  const catalog = { version: 1, products: [...creditProducts] }
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: catalog },
  })
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  renderQuery(<CatalogEditor />)
  expect(await screen.findAllByLabelText('Product name')).toHaveLength(8)
  const description = screen.getAllByLabelText('Description')[0]
  await userEvent.clear(description)
  await userEvent.type(description, 'Verified description')
  await userEvent.click(screen.getByRole('button', { name: 'Save products' }))
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith(
      '/api/clearapi/credit-catalog',
      expect.objectContaining({
        products: expect.arrayContaining([
          expect.objectContaining({ description: 'Verified description' }),
        ]),
      })
    )
  )
  expect(api.get).toHaveBeenCalledWith('/api/clearapi/credit-catalog/admin')
})

it('rejects missing delivery proof and parses fractional amounts without floating point drift', () => {
  const products = [
    {
      ...creditProducts[0],
      published: true,
      purchase_url: 'https://catfk.com/item/verified',
    },
  ]
  expect(catalogSchema.safeParse({ version: 1, products }).success).toBe(false)
  expect(decimalUnits('8.25', 100)).toBe(825)
  expect(decimalUnits('1.000002', 500000)).toBe(500001)
  expect(decimalUnits('0.000003', 500000)).toBe(1)
  expect(decimalUnits('1e9', 100)).toBeNaN()
  expect(formatReferralCredit(1, 'zh-CN')).toBe('$0.000002')
})
