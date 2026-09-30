import { render, screen } from '@testing-library/react'
/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or (at your
option) any later version.

This program is distributed in the hope that it will be useful, but WITHOUT
ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or
FITNESS FOR A PARTICULAR PURPOSE. See the GNU Affero General Public License
for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { ClearApiLanding } from '../components/clearapi-landing'

vi.mock('@tanstack/react-router', () => ({
  Link: (props: { to: string; children: ReactNode; className?: string }) => (
    <a href={props.to} className={props.className}>
      {props.children}
    </a>
  ),
}))

vi.mock('@/components/layout', () => ({
  PublicLayout: (props: { children: ReactNode }) => <div>{props.children}</div>,
}))

vi.mock('@/components/layout/components/footer', () => ({
  Footer: (props: { name: string }) => (
    <footer>
      <span>{props.name}</span>
    </footer>
  ),
}))

describe('ClearAPI landing page', () => {
  it('shows the product headline and a real models page entry without metrics', () => {
    render(<ClearApiLanding isAuthenticated={false} />)

    expect(screen.getByRole('heading', { level: 1 })).toBeVisible()
    expect(
      screen.getByRole('link', { name: 'clearapi.landing.hero.pricing' })
    ).toHaveAttribute('href', '/pricing')
    expect(
      screen.getByRole('link', { name: 'clearapi.landing.hero.quickstart' })
    ).toHaveAttribute('href', '#quickstart')
    expect(screen.queryByText(/\b\d{2,}\+\b/)).not.toBeInTheDocument()
  })

  it('keeps the chat example scrollable and keyboard reachable on narrow screens', () => {
    render(<ClearApiLanding isAuthenticated={false} />)

    const codeRegion = screen.getByRole('region', {
      name: 'clearapi.landing.quickstart.code_label',
    })
    expect(codeRegion).toHaveClass('overflow-x-auto')
    expect(codeRegion).toHaveAttribute('tabindex', '0')
    expect(codeRegion).toHaveTextContent('/v1/chat/completions')
    expect(codeRegion).toHaveTextContent('YOUR_API_KEY')
    expect(codeRegion).toHaveTextContent('MODEL_NAME_FROM_CATALOG')
  })

  it('routes an authenticated user to the dashboard', () => {
    render(<ClearApiLanding isAuthenticated />)

    expect(
      screen.getByRole('link', { name: 'clearapi.landing.hero.dashboard' })
    ).toHaveAttribute('href', '/dashboard')
  })
})
