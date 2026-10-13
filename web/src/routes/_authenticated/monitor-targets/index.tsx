import { createFileRoute, redirect } from '@tanstack/react-router'

import { MonitorTargetsPage } from '@/features/channel-status/monitor-targets-page'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute('/_authenticated/monitor-targets/')({
  beforeLoad: () => {
    if (useAuthStore.getState().auth.user?.role !== ROLE.SUPER_ADMIN) {
      throw redirect({ to: '/403' })
    }
  },
  component: MonitorTargetsPage,
})
