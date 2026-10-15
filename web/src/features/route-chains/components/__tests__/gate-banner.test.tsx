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
import { describe, expect, test } from 'vitest'

import { GateBanner } from '../gate-banner'
import { gatesOff, gatesOn } from '../../__tests__/fixtures'

// F11: the console must show "configuration exists" and "runtime active" as
// different facts, with the non-production gate notice always visible (§23/§24).

describe('F11: feature gate banner', () => {
  test('shows runtime OFF for both gates when disabled', () => {
    render(<GateBanner gates={gatesOff} />)
    expect(screen.getByText('Runtime Route Chain: OFF')).toBeInTheDocument()
    expect(screen.getByText('Circuit Breaker: OFF')).toBeInTheDocument()
    expect(
      screen.getByText('Route Chain production release gate is not approved')
    ).toBeInTheDocument()
  })

  test('shows the gates as ON when enabled, notice still present', () => {
    render(<GateBanner gates={gatesOn} />)
    expect(screen.getByText('Runtime Route Chain: ON')).toBeInTheDocument()
    expect(screen.getByText('Circuit Breaker: ON')).toBeInTheDocument()
    expect(
      screen.getByText('Route Chain production release gate is not approved')
    ).toBeInTheDocument()
  })

  test('never offers a one-click production enable', () => {
    render(<GateBanner gates={gatesOff} />)
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })
})
