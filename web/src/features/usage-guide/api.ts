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
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

export interface GuideSection {
  title: string
  items: { path: string; title: string }[]
}
export interface GuideArticle {
  title: string
  source: string
  html: string
  previous?: string
  next?: string
  sections: GuideSection[]
}
export interface GuideDocument {
  pages: Record<string, GuideArticle>
  catalog: GuideSection[]
  inaccessible: Record<string, string>
  revision: number
}
export interface GuideVersion {
  revision: number
  path: string
  at: number
  article: GuideArticle
  catalog: GuideSection[]
}
export interface GuideChange {
  revision: number
  path: string
  title: string
  html: string
  previous: string
  next: string
  catalog: GuideSection[]
}
export async function getGuide(signal?: AbortSignal): Promise<GuideDocument> {
  const response = await api.get('/api/guide', { signal })
  return requireServerSuccess(response.data).data
}
export async function getGuideAccess(
  signal?: AbortSignal
): Promise<{ can_edit: boolean }> {
  const response = await api.get('/api/guide/access', { signal })
  return requireServerSuccess(response.data).data
}
export async function getGuideVersions(
  path: string,
  signal?: AbortSignal
): Promise<GuideVersion[]> {
  const response = await api.get('/api/guide/versions', {
    params: { article: path },
    signal,
  })
  return requireServerSuccess(response.data).data
}
export async function saveGuide(change: GuideChange): Promise<GuideDocument> {
  const response = await api.put('/api/guide/article', change, {
    headers: { 'X-Guide-Editor': '1' },
    skipErrorHandler: true,
  })
  return requireServerSuccess(response.data).data
}
export async function uploadGuideImage(image: File): Promise<{ url: string }> {
  const form = new FormData()
  form.append('image', image)
  const response = await api.post('/api/guide/images', form, {
    headers: { 'X-Guide-Editor': '1', 'Content-Type': 'multipart/form-data' },
    skipErrorHandler: true,
  })
  return requireServerSuccess(response.data).data
}
