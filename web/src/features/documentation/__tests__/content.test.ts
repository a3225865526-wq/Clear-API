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
  it('loads the approved quickstart content in Chinese by default', () => {
    const quickstart = documentationChapters.find(
      (chapter) => chapter.id === 'quickstart'
    )

    expect(quickstart?.content.zh).toContain(
      'Base URL: https://www.clear999.com'
    )
    expect(quickstart?.content.zh).toContain('/v1/chat/completions')
    expect(getDocumentationTitle(quickstart?.content.zh ?? '')).toBe('快速开始')
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
