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
import { createFileRoute, redirect } from '@tanstack/react-router'

import { checkPoolAccess } from '@/features/upstream-pool/access'
import { PoolAccountDetail } from '@/features/upstream-pool/detail'

function PoolAccountDetailRoute() {
  const params = Route.useParams()
  return <PoolAccountDetail accountId={Number(params.accountId)} />
}

export const Route = createFileRoute('/_authenticated/pool/$accountId')({
  // Numeric account id only, then the same capability probe (fail closed).
  beforeLoad: async ({ params }) => {
    if (!/^\d+$/.test(params.accountId)) {
      throw redirect({ to: '/pool' })
    }
    try {
      if (await checkPoolAccess()) return
    } catch {
      /* fail closed */
    }
    throw redirect({ to: '/403' })
  },
  component: PoolAccountDetailRoute,
})
