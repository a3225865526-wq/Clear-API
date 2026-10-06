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

import { Route as listRoute } from '@/routes/_authenticated/route-chains'
import { Route as detailRoute } from '@/routes/_authenticated/route-chains/$id'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

// F24: non-owners cannot open the console. The route guard mirrors the
// backend's RootAuth — which remains the real authority (§三十九).
// Route modules are imported statically: their cost must not land inside a
// test's timeout window when the suite runs under load.

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

  test('a super admin passes both guards', () => {
    expect(() =>
      (listRoute.options.beforeLoad as () => void)()
    ).not.toThrow()
    expect(() =>
      (detailRoute.options.beforeLoad as () => void)()
    ).not.toThrow()
  })

  test('a plain admin is redirected away from the list', () => {
    setUserRole(ROLE.ADMIN)
    expect(() =>
      (listRoute.options.beforeLoad as () => void)()
    ).toThrow()
  })

  test('a plain admin is redirected away from the detail page', () => {
    setUserRole(ROLE.ADMIN)
    expect(() =>
      (detailRoute.options.beforeLoad as () => void)()
    ).toThrow()
  })

  test('a signed-out user is redirected away', () => {
    useAuthStore.setState((state) => ({ auth: { ...state.auth, user: null } }))
    expect(() =>
      (listRoute.options.beforeLoad as () => void)()
    ).toThrow()
  })
})
