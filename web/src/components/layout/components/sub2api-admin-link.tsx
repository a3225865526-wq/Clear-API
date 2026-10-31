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
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import {
  SidebarGroup,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuItem,
  SidebarMenuButton,
} from '@/components/ui/sidebar'
import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { requireServerSuccess } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

export function Sub2APIAdminLink() {
  const { t } = useTranslation()
  const role = useAuthStore((state) => state.auth.user?.role)
  const isAdmin = (role ?? ROLE.GUEST) >= ROLE.ADMIN
  const access = useQuery({
    queryKey: ['sub2api-admin-access', role],
    enabled: isAdmin,
    retry: false,
    meta: { errorToast: false },
    queryFn: async () => {
      const response = await api.get('/api/group/sub2api-admin')
      requireServerSuccess(response.data)
      return response.data.data.url as string
    },
  })
  if (!isAdmin || !access.data) return null
  return (
    <SidebarGroup>
      <SidebarGroupLabel>{t('Account pool management')}</SidebarGroupLabel>
      <SidebarMenu>
        <SidebarMenuItem>
          <SidebarMenuButton
            render={
              <a href={access.data} target='_blank' rel='noopener noreferrer' />
            }
          >
            {t('Sub2API admin console')}
          </SidebarMenuButton>
        </SidebarMenuItem>
      </SidebarMenu>
    </SidebarGroup>
  )
}

export function GroupAvailabilityNotice() {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const status = useQuery({
    queryKey: ['own-group-availability', userId],
    enabled: !!userId,
    queryFn: async () => {
      const response = await api.get('/api/user/self/group-availability')
      requireServerSuccess(response.data)
      return response.data.data.enabled as boolean
    },
  })
  if (status.data !== false) return null
  return (
    <p role='alert' className='bg-destructive/10 text-destructive px-4 py-2'>
      {t('Your group is temporarily unavailable. Contact an administrator.')}
    </p>
  )
}
