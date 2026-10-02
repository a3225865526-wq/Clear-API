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
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, expect, it, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import { getGuide, getGuideAccess } from '../api'
import { UsageGuidePage } from '../index'

vi.mock('../api', () => ({ getGuide: vi.fn(), getGuideAccess: vi.fn() }))
vi.mock('@tanstack/react-router', async (original) => ({
  ...(await original<typeof import('@tanstack/react-router')>()),
  getRouteApi: () => ({ useSearch: () => ({ article: '/a' }) }),
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
}))
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))
afterEach(() => {
  cleanup()
  useAuthStore.setState(useAuthStore.getInitialState())
})
it.each([false, true])(
  'edit affordance follows server permission (%s)',
  async (canEdit) => {
    useAuthStore.getState().auth.setUser({ id: 5, username: 'QA', role: 100 })
    vi.mocked(getGuide).mockResolvedValue({
      revision: 0,
      catalog: [],
      inaccessible: {},
      pages: {
        '/a': {
          title: 'A',
          source: 'https://example.test',
          html: '<h1>Guide</h1>',
          sections: [],
        },
      },
    })
    vi.mocked(getGuideAccess).mockResolvedValue({ can_edit: canEdit })
    render(
      <QueryClientProvider
        client={
          new QueryClient({ defaultOptions: { queries: { retry: false } } })
        }
      >
        <UsageGuidePage />
      </QueryClientProvider>
    )
    await screen.findByRole('heading', { name: 'Guide' })
    expect(screen.queryByText('guide.sourceTitle')).not.toBeInTheDocument()
    expect(screen.queryByText('guide.sourceNotice')).not.toBeInTheDocument()
    expect(screen.queryByText('guide.original')).not.toBeInTheDocument()
    await waitFor(() => expect(getGuideAccess).toHaveBeenCalled())
    if (canEdit) {
      expect(
        await screen.findByRole('button', { name: 'guideEdit.edit' })
      ).toBeInTheDocument()
    } else {
      expect(
        screen.queryByRole('button', { name: 'guideEdit.edit' })
      ).not.toBeInTheDocument()
    }
  }
)
