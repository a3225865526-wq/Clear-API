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
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { CreditPurchaseCard } from '../credit-purchase-card'
import { OtherRedeemMethodsCard } from '../other-redeem-methods-card'

function WalletHarness() {
  const [request, setRequest] = useState(0)
  return (
    <>
      <CreditPurchaseCard onRedeem={() => setRequest((value) => value + 1)} />
      <OtherRedeemMethodsCard openLegacy={request} />
    </>
  )
}

function renderWallet() {
  const root = createRootRoute({ component: Outlet })
  const wallet = createRoute({
    getParentRoute: () => root,
    path: '/wallet',
    component: WalletHarness,
  })
  const shop = createRoute({
    getParentRoute: () => root,
    path: '/shop',
    component: () => <p>Shop destination</p>,
  })
  const router = createRouter({
    routeTree: root.addChildren([wallet, shop]),
    history: createMemoryHistory({ initialEntries: ['/wallet'] }),
  })
  render(<RouterProvider router={router} />)
}

afterEach(() => {
  vi.restoreAllMocks()
})

it('offers buying with payments and Store disabled and does not redeem on navigation', async () => {
  const post = vi.spyOn(api, 'post')
  renderWallet()
  await userEvent.click(await screen.findByRole('link', { name: 'Go to shop' }))
  expect(await screen.findByText('Shop destination')).toBeVisible()
  expect(post).not.toHaveBeenCalled()
})

it('opens, focuses and reopens the existing Legacy form and calls only the original redemption endpoint', async () => {
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true, data: 500000 } })
  vi.spyOn(api, 'get').mockResolvedValue({ data: { success: true, data: {} } })
  renderWallet()
  await userEvent.click(
    await screen.findByRole('button', { name: 'Redeem an existing code' })
  )
  const code = await screen.findByLabelText('Have a Code?')
  expect(screen.getByLabelText('Redemption code type')).toHaveValue('legacy')
  await waitFor(() => expect(code).toHaveFocus())
  expect(post).not.toHaveBeenCalled()
  await userEvent.click(
    screen.getByRole('button', { name: /Other redemption methods/ })
  )
  await userEvent.click(
    screen.getByRole('button', { name: 'Redeem an existing code' })
  )
  await userEvent.type(
    await screen.findByLabelText('Have a Code?'),
    'synthetic-legacy-code'
  )
  await userEvent.click(screen.getByRole('button', { name: 'Redeem Now' }))
  await waitFor(() =>
    expect(screen.getByLabelText('Have a Code?')).toHaveValue('')
  )
  expect(post).toHaveBeenCalledTimes(1)
  expect(post).toHaveBeenCalledWith('/api/user/topup', {
    key: 'synthetic-legacy-code',
  })
})
