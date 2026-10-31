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
import { render, screen } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'

import { SidebarProvider } from '@/components/ui/sidebar'
import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'

import { Sub2APIAdminLink } from '../sub2api-admin-link'

const auth = vi.hoisted(() => ({ role: 1 }))
vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (selector: (value: object) => unknown) =>
    selector({ auth: { user: { id: 1, role: auth.role } } }),
}))
vi.mock('@/lib/api', () => ({ api: { get: vi.fn() } }))
beforeEach(() => {
  vi.resetAllMocks()
  auth.role = ROLE.USER
})

function showLink() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <SidebarProvider>
        <Sub2APIAdminLink />
      </SidebarProvider>
    </QueryClientProvider>
  )
}

it('hides the entry for ordinary users and does not request its target', () => {
  showLink()
  expect(
    screen.queryByRole('link', { name: 'Sub2API admin console' })
  ).not.toBeInTheDocument()
  expect(api.get).not.toHaveBeenCalled()
})

it('opens the configured administrator origin in an isolated new tab', async () => {
  auth.role = ROLE.ADMIN
  vi.mocked(api.get).mockResolvedValue({
    data: { success: true, data: { url: 'https://admin.example.invalid' } },
  })
  showLink()
  const link = await screen.findByRole('link', {
    name: 'Sub2API admin console',
  })
  expect(link).toHaveAttribute('href', 'https://admin.example.invalid')
  expect(link).toHaveAttribute('target', '_blank')
  expect(link).toHaveAttribute('rel', 'noopener noreferrer')
})
