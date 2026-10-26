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
import { describe, expect, it } from 'vitest'

import {
  documentationChapters,
  getDocumentationTitle,
  resolveDocumentationLink,
} from '../lib/content'

describe('bundled Clear API documentation', () => {
  it('loads Chinese setup with placeholder credentials and a bounded first request', () => {
    const quickstart = documentationChapters.find(
      (chapter) => chapter.id === 'quickstart'
    )

    expect(quickstart?.content.zh).toContain('https://api.example.com/v1')
    expect(quickstart?.content.zh).toContain(
      "Read-Host 'API Key' -AsSecureString"
    )
    expect(quickstart?.content.zh).toContain('"max_tokens":128')
    expect(quickstart?.content.zh).toContain('YOUR_MODEL_ID')
    expect(quickstart?.content.zh).toContain('/v1/chat/completions')
    expect(getDocumentationTitle(quickstart?.content.zh ?? '')).toBe('快速开始')
  })

  it('resolves all bundled article links and heading fragments in both languages', () => {
    for (const chapter of documentationChapters) {
      for (const language of ['zh', 'en'] as const) {
        for (const match of chapter.content[language].matchAll(
          /\]\(([^)]+\.md(?:#[^)]*)?|\.\.\/(?:zh|en)\/)\)/g
        )) {
          const resolved = resolveDocumentationLink(
            match[1],
            chapter.id,
            language
          )
          expect(resolved, `${chapter.id}: ${match[1]}`).toBeDefined()
          const url = new URL(resolved ?? '', 'https://documentation.invalid')
          if (!url.hash) continue
          const targetId = url.pathname.split('/')[3] || 'overview'
          const target = documentationChapters.find(
            (item) => item.id === targetId
          )
          const targetLanguage = url.searchParams.get('lang') as 'zh' | 'en'
          const headings = [
            ...(target?.content[targetLanguage] ?? '').matchAll(
              /^#{1,6}\s+(.+)$/gm
            ),
          ].map((heading) =>
            heading[1]
              .toLowerCase()
              .trim()
              .replaceAll(/[^\p{L}\p{N}\s-]/gu, '')
              .replaceAll(/\s+/g, '-')
          )
          expect(headings, match[1]).toContain(
            decodeURIComponent(url.hash.slice(1))
          )
        }
      }
    }
  })

  it('keeps English quickstart and client guides available from source Markdown', () => {
    const englishQuickstart = documentationChapters.find(
      (chapter) => chapter.id === 'quickstart'
    )
    const englishClients = documentationChapters.find(
      (chapter) => chapter.id === 'clients'
    )

    expect(englishQuickstart?.content.en).toContain('Quickstart')
    expect(englishClients?.content.en).toContain('client')
  })

  it('resolves relative article links to public chapter URLs and preserves fragments', () => {
    expect(
      resolveDocumentationLink('quickstart.md#connection', 'overview', 'zh')
    ).toBe('/docs/clearapi/quickstart?lang=zh#connection')
    expect(resolveDocumentationLink('../en/', 'overview', 'zh')).toBe(
      '/docs/clearapi?lang=en'
    )
    expect(resolveDocumentationLink('#connection', 'quickstart', 'zh')).toBe(
      '#connection'
    )
  })

  it('rejects unknown local pages and unsafe schemes without exposing filesystem paths', () => {
    expect(
      resolveDocumentationLink('missing.md', 'overview', 'zh')
    ).toBeUndefined()
    expect(
      resolveDocumentationLink('javascript:alert(1)', 'overview', 'zh')
    ).toBeUndefined()
    expect(
      resolveDocumentationLink('file:///C:/private/docs.md', 'overview', 'zh')
    ).toBeUndefined()
  })
})
