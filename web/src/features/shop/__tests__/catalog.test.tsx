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
import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { afterEach, expect, it, vi } from 'vitest'

import en from '@/i18n/locales/en.json'
import zh from '@/i18n/locales/zh.json'
import { api } from '@/lib/api'

import { CreditCatalog } from '..'
import {
  creditProducts,
  legacyPurchaseUrl,
  trustedPurchaseUrl,
  type CreditProduct,
} from '../catalog'

function renderCatalog(products?: readonly CreditProduct[]) {
  const root = createRootRoute({ component: Outlet })
  const shop = createRoute({
    getParentRoute: () => root,
    path: '/shop',
    component: () => <CreditCatalog products={products} />,
  })
  const wallet = createRoute({
    getParentRoute: () => root,
    path: '/wallet',
    component: () => <p>Wallet destination</p>,
  })
  const router = createRouter({
    routeTree: root.addChildren([shop, wallet]),
    history: createMemoryHistory({ initialEntries: ['/shop'] }),
  })
  render(<RouterProvider router={router} />)
  return router
}

afterEach(async () => {
  await i18next.changeLanguage('en')
  vi.restoreAllMocks()
})

it('shows the official product and safe external link with no payment or redemption request', async () => {
  const post = vi.spyOn(api, 'post')
  renderCatalog()
  expect(
    await screen.findByRole('heading', {
      name: 'ClearAPI API credit recharge code',
    })
  ).toBeVisible()
  const buy = screen.getByRole('link', { name: 'View price and buy' })
  expect(buy).toHaveAttribute('href', 'https://catfk.com/item/qmcse2')
  expect(buy).toHaveAttribute('target', '_blank')
  expect(buy).toHaveAttribute('rel', 'noopener noreferrer')
  expect(screen.getAllByText(/Cannot be withdrawn as cash/)).toHaveLength(8)
  expect(screen.getAllByText(/does not automatically credit/)).toHaveLength(8)
  expect(screen.queryByText(/Quota/)).not.toBeInTheDocument()
  expect(screen.getAllByRole('button', { name: 'Coming soon' })).toHaveLength(8)
  expect(post).not.toHaveBeenCalled()
})

it('routes users to the existing wallet instead of redeeming or creating an order', async () => {
  const post = vi.spyOn(api, 'post')
  renderCatalog()
  await userEvent.click(
    await screen.findByRole('link', { name: 'Go to wallet to redeem' })
  )
  expect(await screen.findByText('Wallet destination')).toBeVisible()
  expect(post).not.toHaveBeenCalled()
})

it('fails closed on invalid configured product links', async () => {
  renderCatalog([
    {
      ...creditProducts[0],
      id: 'invalid',
      published: true,
      delivery_verified: true,
      purchase_url: 'https://evil.example/item/qmcse2',
    },
  ])
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'Invalid store product link'
  )
  expect(screen.getByRole('button', { name: 'Coming soon' })).toBeDisabled()
  expect(
    screen.queryByRole('link', { name: 'Buy API credits' })
  ).not.toBeInTheDocument()
})

it.each([
  'http://catfk.com/item/qmcse2',
  'https://catfk.com.evil.example/item/qmcse2',
  'https://user@catfk.com/item/qmcse2',
  'https://catfk.com/item/qmcse2?redirect=https://evil.example',
  'https://catfk.com/item/qmcse2#redirect',
  '//catfk.com/item/qmcse2',
  'javascript:alert(1)',
  'https://catfk.com:8443/item/qmcse2',
])('rejects untrusted URL %s', (url) => {
  expect(trustedPurchaseUrl(url)).toBeNull()
})

it('keeps a single column on mobile and switches the purchase content to Chinese', async () => {
  i18next.addResourceBundle('en', 'translation', en.translation, true, true)
  i18next.addResourceBundle('zhCN', 'translation', zh.translation, true, true)
  renderCatalog()
  const product = (await screen.findAllByRole('article'))[0]
  expect(product.parentElement).toHaveClass('grid-cols-1', 'sm:grid-cols-2')
  await act(async () => {
    await i18next.changeLanguage('zhCN')
  })
  expect(screen.getByRole('heading', { name: '购买额度' })).toBeVisible()
  expect(screen.getByRole('link', { name: '查看价格并购买' })).toHaveAttribute(
    'href',
    legacyPurchaseUrl
  )
})

it('only enables verified published products with their own purchase links', async () => {
  renderCatalog([
    {
      ...creditProducts[0],
      published: true,
      delivery_verified: true,
      purchase_url: 'https://catfk.com/item/first',
    },
    {
      ...creditProducts[1],
      published: true,
      delivery_verified: true,
      purchase_url: 'https://catfk.com/item/second',
    },
    {
      ...creditProducts[2],
      published: true,
      delivery_verified: false,
      purchase_url: 'https://catfk.com/item/unverified',
    },
  ])
  const links = await screen.findAllByRole('link', { name: 'Buy API credits' })
  expect(links.map((l) => l.getAttribute('href'))).toEqual([
    'https://catfk.com/item/first',
    'https://catfk.com/item/second',
  ])
  expect(screen.getByRole('button', { name: 'Coming soon' })).toBeDisabled()
  expect(
    screen.getByText(/Confirm the delivered API credit amount/)
  ).toBeVisible()
})
