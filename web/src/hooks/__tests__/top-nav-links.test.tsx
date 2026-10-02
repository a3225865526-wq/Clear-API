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
import { renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { useTopNavLinks } from '../use-top-nav-links'

let client: QueryClient | undefined
afterEach(() => {
  client?.clear()
  useAuthStore.getState().auth.reset()
  localStorage.clear()
  vi.restoreAllMocks()
})

it.each([null, { id: 1, username: 'example', role: 1 }])(
  'removes ranking and about links even when the server enables them (user %s)',
  (user) => {
    useAuthStore.getState().auth.setUser(user)
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: {} },
    })
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    })
    client = queryClient
    queryClient.setQueryData(['status'], {
      HeaderNavModules: JSON.stringify({
        home: true,
        console: true,
        pricing: { enabled: true },
        rankings: { enabled: true },
        about: true,
      }),
    })
    const wrapper = (props: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>
        {props.children}
      </QueryClientProvider>
    )
    const { result } = renderHook(() => useTopNavLinks(), { wrapper })
    expect(result.current.map((link) => link.href)).toEqual([
      '/',
      '/dashboard',
      '/pricing',
    ])
  }
)
