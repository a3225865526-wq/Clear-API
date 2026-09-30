/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful, but WITHOUT
ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or
FITNESS FOR A PARTICULAR PURPOSE. See the GNU Affero General Public License
for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { Footer } from '../footer'
import { SystemBrand } from '../system-brand'

const fixture = vi.hoisted(() => ({ footerHtml: '', demoSiteEnabled: false }))
vi.mock('@/hooks/use-system-config', () => ({
  useSystemConfig: () => ({
    systemName: '澄光 API / ClearAPI',
    logo: '/clearapi.svg',
    ...fixture,
  }),
}))
vi.mock('@/hooks/use-status', () => ({
  useStatus: () => ({ status: { version: 'v1.0.0-rc.40' } }),
}))
vi.mock('@tanstack/react-router', () => ({
  Link: (props: { to: string; children: ReactNode; className?: string }) => (
    <a href={props.to} className={props.className}>{props.children}</a>
  ),
}))
vi.mock('@/components/ui/sidebar', () => {
  const Item = (props: { children: ReactNode }) => <div>{props.children}</div>
  return { SidebarMenu: Item, SidebarMenuItem: Item, SidebarMenuButton: Item }
})

describe('ClearAPI shared brand presentation', () => {
  it('shows the sidebar brand without the upstream version', () => {
    render(<SystemBrand />)
    expect(screen.getByText('澄光 API / ClearAPI')).toBeVisible()
    expect(screen.queryByText(/v1.0.0|Based on New API/)).not.toBeInTheDocument()
    expect(screen.getByRole('img')).toHaveAttribute('src', '/clearapi.svg')
  })

  it('shows only the product brand without upstream attribution or promotions', () => {
    fixture.demoSiteEnabled = true
    render(<Footer />)
    expect(screen.getByText('澄光 API / ClearAPI')).toBeVisible()
    expect(screen.queryByText(/New API contributors/)).not.toBeInTheDocument()
    expect(document.querySelector('a[href*="QuantumNous/new-api"]')).toBeNull()
    expect(document.querySelector('a[href*="docs.newapi.pro"]')).toBeNull()
    expect(screen.getByRole('img')).toHaveAttribute('src', '/clearapi.svg')
    fixture.demoSiteEnabled = false
  })

  it('keeps configured footer content without adding upstream attribution', () => {
    fixture.footerHtml = '<span>Custom footer content</span>'
    render(<Footer />)
    expect(screen.getByText('Custom footer content')).toBeVisible()
    expect(screen.queryByText(/New API contributors/)).not.toBeInTheDocument()
    expect(document.querySelector('a[href*="QuantumNous/new-api"]')).toBeNull()
    fixture.footerHtml = ''
  })
})
