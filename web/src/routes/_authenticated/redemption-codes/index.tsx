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
import { useTranslation } from 'react-i18next'
import z from 'zod'

import { SectionPageLayout } from '@/components/layout'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ClearAPIOfflineOrders } from '@/features/clearapi-offline-orders'
import { RedemptionsBatchesTable } from '@/features/redemption-codes/components/redemptions-batches-table'
import { REDEMPTION_FILTER_VALUES } from '@/features/redemption-codes/constants'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

const redemptionsSearchSchema = z.object({
  page: z.number().optional().catch(1),
  pageSize: z.number().optional().catch(10),
  filter: z.string().optional().catch(''),
  status: z.array(z.enum(REDEMPTION_FILTER_VALUES)).optional().catch([]),
})

export const Route = createFileRoute('/_authenticated/redemption-codes/')({
  beforeLoad: () => {
    const { auth } = useAuthStore.getState()

    if (!auth.user || auth.user.role < ROLE.ADMIN) {
      throw redirect({
        to: '/403',
      })
    }
  },
  validateSearch: redemptionsSearchSchema,
  component: RedemptionManagement,
})

function RedemptionManagement() {
  const { t } = useTranslation()
  return (
    <Tabs defaultValue='offline' className='min-h-0 flex-1'>
      <TabsList className='mx-4 mt-4 sm:mx-6'>
        <TabsTrigger value='offline'>{t('clearapi.offline.title')}</TabsTrigger>
        <TabsTrigger value='batches'>{t('Batches')}</TabsTrigger>
      </TabsList>
      <TabsContent value='offline'>
        <ClearAPIOfflineOrders />
      </TabsContent>
      <TabsContent value='batches'>
        <SectionPageLayout>
          <SectionPageLayout.Title>{t('Batches')}</SectionPageLayout.Title>
          <SectionPageLayout.Content>
            <RedemptionsBatchesTable />
          </SectionPageLayout.Content>
        </SectionPageLayout>
      </TabsContent>
    </Tabs>
  )
}
