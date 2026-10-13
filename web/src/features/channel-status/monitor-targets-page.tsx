import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { MonitorAdmin } from './monitor-admin'

export function MonitorTargetsPage() {
  const { t } = useTranslation()
  const role = useAuthStore((state) => state.auth.user?.role)
  if (role !== ROLE.SUPER_ADMIN) return null

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Monitoring targets')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <MonitorAdmin allowlist />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
