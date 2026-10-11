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
import enKeys from '../../../../../docs/clearapi/en/api-key-models.md?raw'
import enReference from '../../../../../docs/clearapi/en/api-reference.md?raw'
import enClients from '../../../../../docs/clearapi/en/clients.md?raw'
import enCompatibility from '../../../../../docs/clearapi/en/compatibility.md?raw'
import enPricing from '../../../../../docs/clearapi/en/pricing-errors.md?raw'
import enQuickstart from '../../../../../docs/clearapi/en/quickstart.md?raw'
import enOverview from '../../../../../docs/clearapi/en/README.md?raw'
import enSdk from '../../../../../docs/clearapi/en/sdk-examples.md?raw'
import zhKeys from '../../../../../docs/clearapi/zh/api-key-models.md?raw'
import zhReference from '../../../../../docs/clearapi/zh/api-reference.md?raw'
import zhClients from '../../../../../docs/clearapi/zh/clients.md?raw'
import zhCompatibility from '../../../../../docs/clearapi/zh/compatibility.md?raw'
import zhPricing from '../../../../../docs/clearapi/zh/pricing-errors.md?raw'
import zhQuickstart from '../../../../../docs/clearapi/zh/quickstart.md?raw'
import zhOverview from '../../../../../docs/clearapi/zh/README.md?raw'
import zhSdk from '../../../../../docs/clearapi/zh/sdk-examples.md?raw'

export type DocumentationLanguage = 'zh' | 'en'

// Only metadata lives here. All article text is bundled from the approved source.
export const documentationChapters = [
  {
    id: 'overview',
    file: 'README.md',
    content: { zh: zhOverview, en: enOverview },
  },
  {
    id: 'quickstart',
    file: 'quickstart.md',
    content: { zh: zhQuickstart, en: enQuickstart },
  },
  {
    id: 'api-key-models',
    file: 'api-key-models.md',
    content: { zh: zhKeys, en: enKeys },
  },
  {
    id: 'api-reference',
    file: 'api-reference.md',
    content: { zh: zhReference, en: enReference },
  },
  {
    id: 'sdk-examples',
    file: 'sdk-examples.md',
    content: { zh: zhSdk, en: enSdk },
  },
  {
    id: 'clients',
    file: 'clients.md',
    content: { zh: zhClients, en: enClients },
  },
  {
    id: 'pricing-errors',
    file: 'pricing-errors.md',
    content: { zh: zhPricing, en: enPricing },
  },
  {
    id: 'compatibility',
    file: 'compatibility.md',
    content: { zh: zhCompatibility, en: enCompatibility },
  },
] as const

export function getDocumentationTitle(content: string): string {
  return /^#\s+(.+)$/m.exec(content)?.[1] ?? ''
}

export function resolveDocumentationLink(
  href: string,
  chapter: string,
  language: DocumentationLanguage
): string | undefined {
  if (href.startsWith('#')) return href
  // Preserve real site actions without changing their existing permission guards.
  if (href.startsWith('/') && !href.startsWith('//')) return href
  const source = documentationChapters.find((item) => item.id === chapter)
  let url: URL
  try {
    url = new URL(
      href,
      `https://documentation.invalid/${language}/${source?.file ?? 'README.md'}`
    )
  } catch {
    return undefined
  }
  if (!['https:', 'http:', 'mailto:'].includes(url.protocol)) return undefined
  if (url.origin !== 'https://documentation.invalid') return url.href

  const match = /^\/(zh|en)\/(.*)$/.exec(url.pathname)
  if (!match) return undefined
  const targetLanguage = match[1]
  const file = decodeURIComponent(match[2]) || 'README.md'
  const target = documentationChapters.find((item) => item.file === file)
  if (!target) return undefined
  const path =
    target.id === 'overview' ? '/docs/clearapi' : `/docs/clearapi/${target.id}`
  return `${path}?lang=${targetLanguage}${url.hash}`
}
