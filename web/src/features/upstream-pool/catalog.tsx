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
import { ArrowLeft } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { LoadingState } from '@/components/loading-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'

import { getPoolCatalog } from './api'
import { AccessTypeCapabilities } from './components/access-type-capabilities'
import { POOL_QUERY_KEY } from './constants'

/** Platform & capability catalog. All platforms render, including disabled. */
export function PoolCatalog() {
  const { t } = useTranslation()
  const catalogQuery = useQuery({
    queryKey: [...POOL_QUERY_KEY, 'catalog'],
    queryFn: ({ signal }) => getPoolCatalog(signal),
    staleTime: 5 * 60 * 1000,
  })

  let content
  if (catalogQuery.isPending) {
    content = (
      <div role='status'>
        <LoadingState message={t('Loading platform catalog')} />
      </div>
    )
  } else if (catalogQuery.isError) {
    content = (
      <ErrorState
        title={t('Upstream account pool is unavailable.')}
        description={t('The platform catalog could not be loaded.')}
        onRetry={() => void catalogQuery.refetch()}
      />
    )
  } else {
    const platforms = catalogQuery.data?.platforms ?? []
    content =
      platforms.length === 0 ? (
        <p className='text-muted-foreground text-sm'>
          {t('No platforms in the catalog.')}
        </p>
      ) : (
        <div className='space-y-4'>
          {platforms.map((platform) => (
            <section
              key={platform.platform}
              className='bg-card space-y-4 rounded-xl border p-4'
            >
              <div className='flex flex-wrap items-center gap-2'>
                <h3 className='text-sm font-semibold'>{platform.label}</h3>
                <Badge
                  variant='secondary'
                  className='font-mono font-normal'
                  aria-label={platform.platform}
                >
                  {platform.platform}
                </Badge>
                {platform.disabled ? (
                  <Badge variant='destructive'>{t('Disabled')}</Badge>
                ) : null}
              </div>
              {platform.access_types.length === 0 ? (
                <p className='text-muted-foreground text-sm'>
                  {t('No access types for this platform.')}
                </p>
              ) : (
                platform.access_types.map((accessType) => (
                  <AccessTypeCapabilities
                    key={accessType.type}
                    platformLabel={platform.label}
                    platform={platform.platform}
                    accessType={accessType}
                  />
                ))
              )}
            </section>
          ))}
        </div>
      )
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        <span className='flex min-w-0 items-center gap-2'>
          <Button
            variant='ghost'
            size='icon-sm'
            className='shrink-0'
            render={<Link to='/pool' />}
            aria-label={t('Back to accounts')}
          >
            <ArrowLeft />
          </Button>
          <span className='truncate'>{t('Platform & capabilities')}</span>
        </span>
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>{content}</SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
