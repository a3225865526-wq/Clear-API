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
import { Plus } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

import { listRouteChains } from './api'
import { GateBanner } from './components/gate-banner'
import { RouteChainsDialogs } from './components/route-chains-dialogs'
import { RouteChainsProvider, useRouteChainsDialogs } from './components/route-chains-provider'
import { RouteChainsTable } from './components/route-chains-table'
import { routeChainListKey } from './lib/query-keys'

// Route Chains operator console (Stage 6 design): owner-only management for
// high-availability route chains. Low-frequency polling (§四十六); no
// optimistic writes (§三十六).

export function RouteChains() {
  return (
    <RouteChainsProvider>
      <RouteChainsPage />
      <RouteChainsDialogs />
    </RouteChainsProvider>
  )
}

function ListBody(props: {
  query: ReturnType<typeof useListQuery>
  items: import('./types').ChainListItem[]
}) {
  const { t } = useTranslation()
  if (props.query.isPending) {
    return <LoadingState message={t('Loading route chains…')} />
  }
  if (props.query.isError) {
    return (
      <ErrorState
        title={t('Failed to load route chains')}
        onRetry={() => void props.query.refetch()}
      />
    )
  }
  if (props.items.length === 0) {
    return (
      <EmptyState
        title={t('No route chains yet')}
        description={t(
          'Create a chain to define an ordered failover list for one model and group.'
        )}
      />
    )
  }
  return <RouteChainsTable items={props.items} />
}

function useListQuery(includeArchived: boolean) {
  return useQuery({
    queryKey: routeChainListKey(includeArchived),
    queryFn: ({ signal }) => listRouteChains(includeArchived, signal),
    refetchInterval: 10_000,
    refetchIntervalInBackground: false,
  })
}

function RouteChainsPage() {
  const { t } = useTranslation()
  const { setOpen } = useRouteChainsDialogs()
  const [includeArchived, setIncludeArchived] = useState(false)

  const listQuery = useListQuery(includeArchived)

  const items = listQuery.data?.items ?? []
  const gates = listQuery.data?.gates

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>
        <span>{t('Route Chains')}</span>
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <div className='flex items-center gap-3'>
          <div className='flex items-center gap-2'>
            <Switch
              id='route-chains-include-archived'
              checked={includeArchived}
              onCheckedChange={setIncludeArchived}
              aria-label={t('Show archived')}
            />
            <Label htmlFor='route-chains-include-archived'>
              {t('Show archived')}
            </Label>
          </div>
          <Button onClick={() => setOpen('create-chain')}>
            <Plus data-icon='inline-start' />
            {t('Create Route Chain')}
          </Button>
        </div>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-4'>
          {gates ? <GateBanner gates={gates} /> : null}
          <ListBody
            query={listQuery}
            items={items}
          />
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
