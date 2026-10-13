import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
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
import { afterEach, describe, expect, test, vi } from 'vitest'

import type { StoreProductDto } from '../../types'
import { StoreProductsCard } from '../store-products-card'

const PRODUCTS: StoreProductDto[] = [
  {
    id: 1,
    name: '¥100 API 充值卡',
    face_value_cents: 10000,
    store_url: 'https://store.example.com/p/100',
    description: '',
    sort: 1,
  },
]

describe('StoreProductsCard', () => {
  test('uses the server admission route and displays the exact approved base credit', async () => {
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    render(
      <StoreProductsCard
        enabled
        products={[
          {
            ...PRODUCTS[0],
            id: 'one',
            base_credit: '1.000002',
            purchase_path: '/api/clearapi/purchase/product/one?revision=2',
          },
        ]}
      />
    )
    expect(screen.getByText(/1.000002/)).toBeVisible()
    await userEvent.click(screen.getByRole('button'))
    expect(open).toHaveBeenCalledWith(
      '/api/clearapi/purchase/product/one?revision=2',
      '_blank',
      'noopener,noreferrer'
    )
  })
  afterEach(() => {
    vi.restoreAllMocks()
  })

  test('renders nothing while the feature flag is off', () => {
    const { container } = render(
      <StoreProductsCard enabled={false} products={PRODUCTS} />
    )
    expect(container).toBeEmptyDOMElement()
  })

  test('shows the empty hint when no products are configured', () => {
    render(<StoreProductsCard enabled products={[]} />)
    expect(
      screen.getByText('No store products configured yet')
    ).toBeInTheDocument()
  })

  test('renders product face values without parsing the name', () => {
    render(<StoreProductsCard enabled products={PRODUCTS} />)
    expect(screen.getByText('¥100.00')).toBeInTheDocument()
    expect(screen.getByText('¥100 API 充值卡')).toBeInTheDocument()
  })

  test('opens the store URL with noopener/noreferrer', async () => {
    const openSpy = vi
      .spyOn(window, 'open')
      .mockReturnValue(null as unknown as Window)
    const user = userEvent.setup()
    render(<StoreProductsCard enabled products={PRODUCTS} />)
    await user.click(screen.getByRole('button'))
    expect(openSpy).toHaveBeenCalledWith(
      'https://store.example.com/p/100',
      '_blank',
      'noopener,noreferrer'
    )
  })
})
