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
import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'

import { VipProgressCard } from '../vip-progress-card'
import type { VipInfo } from '../../types'

describe('VipProgressCard', () => {
  it('falls back to the generic level name when backend returns none', () => {
    render(
      <VipProgressCard
        vip={{
          name: '',
          bonus_basis_points: 0,
          total_recharge_cents: 0,
          next_level: null,
        }}
      />
    )
    expect(screen.getByText('Regular user')).toBeInTheDocument()
    expect(screen.getByText('Highest level reached')).toBeInTheDocument()
  })

  it('shows next level and remaining amount', () => {
    render(
      <VipProgressCard
        vip={
          {
            name: 'VIP1',
            bonus_basis_points: 200,
            total_recharge_cents: 190000,
            current_threshold_cents: 50000,
            next_level: {
              name: 'VIP2',
              threshold_cents: 200000,
              remaining_cents: 10000,
            },
          } satisfies VipInfo
        }
      />
    )
    expect(screen.getByText('VIP1')).toBeInTheDocument()
    expect(screen.getByText(/VIP2/)).toBeInTheDocument()
    expect(screen.getByText(/¥100\.00/)).toBeInTheDocument()
  })

  it('hides the upgrade hint at the highest level', () => {
    render(
      <VipProgressCard
        vip={{
          name: 'VIP4',
          bonus_basis_points: 800,
          total_recharge_cents: 10000000,
          next_level: null,
        }}
      />
    )
    expect(screen.queryByText(/To /i)).not.toBeInTheDocument()
  })
})
