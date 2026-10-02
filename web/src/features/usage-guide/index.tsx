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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { getRouteApi, Link } from '@tanstack/react-router'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout/components/section-page-layout'
import { LoadingState } from '@/components/loading-state'
import { RichContent } from '@/components/rich-content'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { useAuthStore } from '@/stores/auth-store'

import { getGuide, getGuideAccess } from './api'
import { GuideEditor } from './editor'

import './guide.css'

const route = getRouteApi('/_authenticated/usage-guide')

export function UsageGuidePage() {
  const { t } = useTranslation()
  const { article } = route.useSearch()
  const user = useAuthStore((state) => state.auth.user)
  const client = useQueryClient()
  const [editing, setEditing] = useState(false)
  const key = ['published-usage-guide', user?.id]
  const query = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => getGuide(signal),
    staleTime: 0,
    refetchInterval: editing ? false : 60000,
    refetchOnWindowFocus: !editing,
  })
  const access = useQuery({
    queryKey: ['guide-edit-access', user?.id],
    queryFn: ({ signal }) => getGuideAccess(signal),
    enabled: Boolean(user),
    retry: false,
  })
  const page = query.data?.pages[article]
  const catalog = query.data?.catalog
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('console.guide')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        {query.isPending && <LoadingState />}
        {query.isError && <ErrorState onRetry={() => void query.refetch()} />}
        {query.data && (
          <div className='grid min-w-0 gap-6 pb-6 lg:grid-cols-[240px_minmax(0,1fr)]'>
            <nav
              aria-label={t('guide.catalog')}
              className='bg-card h-fit min-w-0 space-y-5 rounded-2xl border p-4 lg:sticky lg:top-4 lg:max-h-[80vh] lg:overflow-y-auto'
            >
              <Link
                to='/usage-guide'
                search={{ article: '/' }}
                className='text-primary block font-medium'
              >
                {t('guide.home')}
              </Link>
              {catalog?.map((group) => (
                <section key={group.title}>
                  <h2 className='text-muted-foreground mb-2 text-xs font-semibold'>
                    {group.title}
                  </h2>
                  <div className='space-y-1'>
                    {group.items.map((item) => (
                      <Link
                        key={item.path}
                        to='/usage-guide'
                        search={{ article: item.path }}
                        aria-current={
                          article === item.path ? 'page' : undefined
                        }
                        className='hover:bg-muted focus-visible:ring-ring aria-[current=page]:bg-primary/10 aria-[current=page]:text-primary block rounded-lg px-2 py-2 text-sm break-words focus-visible:ring-2'
                      >
                        {item.title}
                      </Link>
                    ))}
                  </div>
                </section>
              ))}
            </nav>
            <div className='min-w-0 space-y-4'>
              {access.data?.can_edit && page && (
                <div className='flex justify-end'>
                  <Button onClick={() => setEditing(true)}>
                    {t('guideEdit.edit')}
                  </Button>
                </div>
              )}
              <Alert>
                <AlertTitle>{t('guide.sourceTitle')}</AlertTitle>
                <AlertDescription>
                  {t('guide.sourceNotice')}{' '}
                  {article === '/' && <>{t('guide.staticControls')} </>}
                  {page && (
                    <a
                      href={page.source}
                      target='_blank'
                      rel='noopener noreferrer'
                      className='underline'
                    >
                      {t('guide.original')}
                    </a>
                  )}
                </AlertDescription>
              </Alert>
              {page ? (
                <>
                  <article className='bg-card min-w-0 rounded-2xl border p-5 md:p-8'>
                    <RichContent
                      mode='html'
                      content={page.html}
                      className='migrated-guide'
                    />
                  </article>
                  <nav
                    aria-label={t('guide.articleNavigation')}
                    className='flex flex-wrap justify-between gap-3'
                  >
                    {page.previous && (
                      <Button
                        variant='outline'
                        className='h-auto whitespace-normal'
                        render={
                          <Link
                            to='/usage-guide'
                            search={{ article: page.previous }}
                          />
                        }
                      >
                        {t('guide.previous')} ·{' '}
                        {query.data.pages[page.previous]?.title}
                      </Button>
                    )}
                    {page.next && (
                      <Button
                        variant='outline'
                        className='h-auto whitespace-normal'
                        render={
                          <Link
                            to='/usage-guide'
                            search={{ article: page.next }}
                          />
                        }
                      >
                        {t('guide.next')} · {query.data.pages[page.next]?.title}
                      </Button>
                    )}
                  </nav>
                </>
              ) : (
                <EmptyState
                  title={t('guide.unavailable')}
                  description={t('guide.unavailableDescription')}
                />
              )}
            </div>
          </div>
        )}
        {editing && query.data && page && access.data?.can_edit && (
          <GuideEditor
            key={article}
            open={editing}
            article={article}
            document={query.data}
            onClose={() => setEditing(false)}
            onSaved={(document) => {
              client.setQueryData(key, document)
              void client.invalidateQueries({ queryKey: ['guide-versions'] })
            }}
          />
        )}
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
