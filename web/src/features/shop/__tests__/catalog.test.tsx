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

it('shows both support contacts and copies the selected QQ number', async () => {
  const user = userEvent.setup()
  renderCatalog()
  const support = await screen.findByRole('button', { name: 'Copy support QQ' })
  expect(screen.getByText('QQ: 3225865526')).toBeVisible()
  expect(screen.getByText('Support QQ group: 660466292')).toBeVisible()
  const group = screen.getByRole('button', { name: 'Copy support QQ group' })
  expect(group.parentElement).toHaveTextContent('Support QQ group: 660466292')
  expect(group.parentElement).toHaveClass('inline-flex', 'whitespace-nowrap')

  await user.click(support)
  expect(await navigator.clipboard.readText()).toBe('3225865526')
  await user.click(group)
  expect(await navigator.clipboard.readText()).toBe('660466292')
})

it('shows the official product and safe external link with no payment or redemption request', async () => {
  const post = vi.spyOn(api, 'post')
  renderCatalog([
    {
      ...creditProducts[0],
      published: true,
      delivery_verified: true,
      purchase_url: legacyPurchaseUrl,
    },
  ])
  expect(
    await screen.findByRole('heading', { name: creditProducts[0].name })
  ).toBeVisible()
  const buy = screen.getByRole('link', { name: 'Buy API credits' })
  expect(buy).toHaveAttribute('href', 'https://catfk.com/item/qmcse2')
  expect(buy).toHaveAttribute('target', '_blank')
  expect(buy).toHaveAttribute('rel', 'noopener noreferrer')
  expect(
    screen.queryByText(/Confirm the delivered API credit amount/)
  ).not.toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'Coming soon' })
  ).not.toBeInTheDocument()
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
  await screen.findByRole('heading', { name: 'Buy credits' })
  expect(screen.queryByRole('article')).not.toBeInTheDocument()
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
  renderCatalog([
    {
      ...creditProducts[0],
      published: true,
      delivery_verified: true,
      purchase_url: legacyPurchaseUrl,
    },
  ])
  const product = (await screen.findAllByRole('article'))[0]
  expect(product.parentElement).toHaveClass('grid-cols-1', 'sm:grid-cols-2')
  await act(async () => {
    await i18next.changeLanguage('zhCN')
  })
  expect(screen.getByRole('heading', { name: '购买额度' })).toBeVisible()
  expect(screen.getByText('QQ群: 660466292')).toBeVisible()
  expect(screen.getByRole('link', { name: '购买 API 额度' })).toHaveAttribute(
    'href',
    'https://catfk.com/item/qmcse2'
  )
})

it('only enables verified published products with their own purchase links', async () => {
  renderCatalog([
    {
      ...creditProducts[0],
      published: true,
      delivery_verified: true,
      purchase_url: legacyPurchaseUrl,
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
    'https://catfk.com/item/qmcse2',
    'https://catfk.com/item/second',
  ])
  expect(screen.queryByText(creditProducts[2].name)).not.toBeInTheDocument()
  expect(
    screen.queryByText(/Confirm the delivered API credit amount/)
  ).not.toBeInTheDocument()
})
