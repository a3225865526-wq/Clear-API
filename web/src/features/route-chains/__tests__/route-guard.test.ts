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
import { beforeEach, describe, expect, test } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'

// F24: non-owners cannot open the console. The route guard mirrors the
// backend's RootAuth — which remains the real authority (§三十九).

async function loadGuards() {
  const listRoute = await import('@/routes/_authenticated/route-chains')
  const detailRoute = await import('@/routes/_authenticated/route-chains/$id')
  return { listRoute, detailRoute }
}

function setUserRole(role: number) {
  useAuthStore.setState((state) => ({
    auth: {
      ...state.auth,
      user: state.auth.user
        ? { ...state.auth.user, role }
        : ({
            id: 1,
            username: 'tester',
            role,
            status: 1,
            permissions: {},
          } as never),
    },
  }))
}

describe('F24: route guards', () => {
  beforeEach(() => {
    setUserRole(ROLE.SUPER_ADMIN)
  })

  test('a super admin passes both guards', async () => {
    const { listRoute, detailRoute } = await loadGuards()
    expect(() =>
      (listRoute.Route.options.beforeLoad as () => void)()
    ).not.toThrow()
    expect(() =>
      (detailRoute.Route.options.beforeLoad as () => void)()
    ).not.toThrow()
  })

  test('a plain admin is redirected away from the list', async () => {
    setUserRole(ROLE.ADMIN)
    const { listRoute } = await loadGuards()
    expect(() =>
      (listRoute.Route.options.beforeLoad as () => void)()
    ).toThrow()
  })

  test('a plain admin is redirected away from the detail page', async () => {
    setUserRole(ROLE.ADMIN)
    const { detailRoute } = await loadGuards()
    expect(() =>
      (detailRoute.Route.options.beforeLoad as () => void)()
    ).toThrow()
  })

  test('a signed-out user is redirected away', async () => {
    useAuthStore.setState((state) => ({ auth: { ...state.auth, user: null } }))
    const { listRoute } = await loadGuards()
    expect(() =>
      (listRoute.Route.options.beforeLoad as () => void)()
    ).toThrow()
  })
})
