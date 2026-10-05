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
import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'

import { usePoolAccess } from './access'
import { getPoolCatalog } from './api'
import { AccountsTable } from './components/accounts-table'
import { CreateAccountDialog } from './components/create-account-dialog'
import { ReleaseControls } from './components/release-controls'
import { SourceRouting } from './components/source-routing'
import { POOL_QUERY_KEY } from './constants'

export function PoolAccounts() {
  const { t } = useTranslation()
  const access = usePoolAccess()
  const [createOpen, setCreateOpen] = useState(false)
  const catalogQuery = useQuery({
    queryKey: [...POOL_QUERY_KEY, 'catalog'],
    queryFn: ({ signal }) => getPoolCatalog(signal),
    staleTime: 5 * 60 * 1000,
    enabled: access.allowed,
  })

  let content
  if (!access.allowed) {
    content = access.isFetching ? (
      <div role='status'>
        <LoadingState message={t('Checking pool access')} />
      </div>
    ) : (
      <ErrorState
        title={t('Upstream account pool is unavailable.')}
        description={t(
          'Only the server-authorized owner can manage the upstream account pool.'
        )}
      />
    )
  } else {
    content = <AccountsTable />
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Upstream account pool')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          variant='outline'
          size='sm'
          render={<Link to='/pool/capabilities' />}
        >
          {t('Platform & capabilities')}
        </Button>
        <Button size='sm' onClick={() => setCreateOpen(true)}>
          {t('Add account')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='space-y-4'>
          {access.allowed ? <ReleaseControls /> : null}
          {access.allowed ? <SourceRouting /> : null}
          {content}
        </div>
      </SectionPageLayout.Content>
      {access.allowed ? (
        <CreateAccountDialog
          open={createOpen}
          onOpenChange={setCreateOpen}
          catalog={catalogQuery.data}
        />
      ) : null}
    </SectionPageLayout>
  )
}
