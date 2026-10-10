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
import {
  getRouteApi,
  Link,
  Outlet,
  useRouterState,
} from '@tanstack/react-router'
import { useMemo } from 'react'
import { I18nextProvider, useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { PublicLayout } from '@/components/layout/components/public-layout'
import { Button } from '@/components/ui/button'
import { INTERFACE_LANGUAGE_OPTIONS } from '@/i18n/languages'

import { DocumentationMarkdown } from './components/documentation-markdown'
import { documentationChapters, getDocumentationTitle } from './lib/content'

const route = getRouteApi('/docs/clearapi')
const documentationLanguages = INTERFACE_LANGUAGE_OPTIONS.filter(
  (item) => item.code === 'zhCN' || item.code === 'en'
)

export function DocumentationLayout() {
  const { lang } = route.useSearch()
  const { i18n } = useTranslation()
  const scopedI18n = useMemo(
    () => i18n.cloneInstance({ lng: lang === 'en' ? 'en' : 'zhCN' }),
    [i18n, lang]
  )
  return (
    <I18nextProvider i18n={scopedI18n}>
      <DocumentationShell />
    </I18nextProvider>
  )
}

function DocumentationShell() {
  const { t } = useTranslation()
  const { lang } = route.useSearch()
  const pathname = useRouterState({
    select: (state) => state.location.pathname,
  })
  const chapter = pathname.split('/').filter(Boolean)[2] ?? 'overview'

  return (
    <PublicLayout headerProps={{ showLanguageSwitcher: false }}>
      <div
        lang={lang === 'en' ? 'en' : 'zh-CN'}
        className='mx-auto max-w-7xl space-y-6'
      >
        <header className='flex flex-wrap items-center justify-between gap-4'>
          <p className='text-xl font-semibold'>{t('Docs')}</p>
          <nav aria-label={t('Change language')} className='flex gap-2'>
            {documentationLanguages.map((item) => {
              const targetLanguage = item.code === 'en' ? 'en' : 'zh'
              return (
                <Button
                  key={item.code}
                  variant={lang === targetLanguage ? 'default' : 'outline'}
                  size='sm'
                  render={
                    <Link
                      to='/docs/clearapi/$chapter'
                      params={{ chapter }}
                      search={{ lang: targetLanguage }}
                      aria-current={
                        lang === targetLanguage ? 'true' : undefined
                      }
                    />
                  }
                >
                  {item.label}
                </Button>
              )
            })}
          </nav>
        </header>
        <div className='grid min-w-0 gap-6 lg:grid-cols-[240px_minmax(0,1fr)]'>
          <nav
            aria-label={t('Docs')}
            className='bg-card h-fit min-w-0 rounded-2xl border p-3 lg:sticky lg:top-24'
          >
            <ul className='grid gap-1 sm:grid-cols-2 lg:grid-cols-1'>
              {documentationChapters.map((item) => (
                <li key={item.id}>
                  <Link
                    to='/docs/clearapi/$chapter'
                    params={{ chapter: item.id }}
                    search={{ lang }}
                    aria-current={item.id === chapter ? 'page' : undefined}
                    className='hover:bg-muted focus-visible:ring-ring aria-[current=page]:bg-primary/10 aria-[current=page]:text-primary block rounded-lg px-3 py-2 text-sm break-words focus-visible:ring-2'
                  >
                    {getDocumentationTitle(item.content[lang])}
                  </Link>
                </li>
              ))}
            </ul>
          </nav>
          <Outlet />
        </div>
      </div>
    </PublicLayout>
  )
}

export function DocumentationPage(props: { chapter: string }) {
  const { t } = useTranslation()
  const { lang } = route.useSearch()
  // FAQ reuses the approved troubleshooting article, not another copy of its body.
  const chapter = props.chapter === 'faq' ? 'pricing-errors' : props.chapter
  const document = documentationChapters.find((item) => item.id === chapter)
  if (!document) {
    return (
      <EmptyState
        title={t('Content not found.')}
        action={
          <Button
            variant='outline'
            render={<Link to='/docs/clearapi' search={{ lang }} />}
          >
            {t('Docs')}
          </Button>
        }
      />
    )
  }
  return (
    <DocumentationMarkdown
      key={`${lang}:${chapter}`}
      content={document.content[lang]}
      chapter={chapter}
      language={lang}
    />
  )
}
