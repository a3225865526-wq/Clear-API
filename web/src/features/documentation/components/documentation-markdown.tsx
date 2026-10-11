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
import { useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Markdown } from '@/components/ui/markdown'

import {
  resolveDocumentationLink,
  type DocumentationLanguage,
} from '../lib/content'

type Heading = { id: string; title: string }
export function DocumentationMarkdown(props: {
  content: string
  chapter: string
  language: DocumentationLanguage
}) {
  const { t } = useTranslation()
  const root = useRef<HTMLDivElement>(null)
  const [headings, setHeadings] = useState<Heading[]>([])
  const [codeValues, setCodeValues] = useState<string[]>([])
  const resolveLink = useMemo(
    () => (href: string) =>
      resolveDocumentationLink(href, props.chapter, props.language),
    [props.chapter, props.language]
  )

  useLayoutEffect(() => {
    const article = root.current
    if (!article) return
    const ids = new Set<string>()
    const items = [...article.querySelectorAll('h1,h2,h3,h4,h5,h6')].map(
      (heading) => {
        const title = heading.textContent ?? ''
        const slug =
          title
            .toLowerCase()
            .trim()
            .replaceAll(/[^\p{L}\p{N}\s-]/gu, '')
            .replaceAll(/\s+/g, '-') || 'section'
        let id = slug
        let suffix = 1
        while (ids.has(id)) id = `${slug}-${suffix++}`
        ids.add(id)
        heading.id = id
        heading.classList.add('scroll-mt-24')
        return { id, title }
      }
    )
    setHeadings(items)

    setCodeValues(
      [...article.querySelectorAll('pre')].map(
        (pre) => pre.querySelector('code')?.textContent ?? pre.textContent ?? ''
      )
    )

    // Route chunks mount asynchronously; restore fragment navigation after headings exist.
    try {
      const id = decodeURIComponent(window.location.hash.slice(1))
      if (id) document.querySelector(`#${CSS.escape(id)}`)?.scrollIntoView()
    } catch {
      // Invalid URL fragments do not prevent reading an article.
    }
  }, [props.content])

  return (
    <div className='min-w-0 space-y-5'>
      {headings.length > 0 && (
        <nav
          aria-label={t('Docs')}
          className='bg-muted/40 rounded-xl border p-4'
        >
          <p className='mb-2 text-sm font-semibold'>{t('Docs')}</p>
          <ul className='flex flex-wrap gap-x-5 gap-y-2 text-sm'>
            {headings.map((heading) => (
              <li key={heading.id}>
                <a
                  className='text-primary break-words underline-offset-4 hover:underline'
                  href={`#${heading.id}`}
                >
                  {heading.title}
                </a>
              </li>
            ))}
          </ul>
        </nav>
      )}
      <article className='bg-card min-w-0 rounded-2xl border p-4 sm:p-6 lg:p-8'>
        <div ref={root}>
          <Markdown allowHtml={false} resolveLink={resolveLink}>
            {props.content}
          </Markdown>
        </div>
        {codeValues.length > 0 && (
          <div className='flex flex-wrap justify-end gap-2 border-t pt-3'>
            {codeValues.map((value) => (
              <CopyButton
                key={value}
                value={value}
                aria-label={t('Copy code')}
              />
            ))}
          </div>
        )}
      </article>
    </div>
  )
}
