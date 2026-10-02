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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  fireEvent,
  render,
  screen,
  waitFor,
  cleanup,
} from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { getGuideVersions, saveGuide, type GuideDocument } from '../api'
import { GuideEditor } from '../editor'

vi.mock('../api', () => ({
  getGuideVersions: vi.fn(),
  saveGuide: vi.fn(),
  uploadGuideImage: vi.fn(),
}))
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key, i18n: { language: 'en' } }),
}))
const doc: GuideDocument = {
  revision: 3,
  inaccessible: {},
  catalog: [],
  pages: {
    '/a': {
      title: 'A',
      html: '<h1>Original</h1>',
      source: 'https://example.test/a',
      sections: [],
    },
  },
}
const onSaved = vi.fn()
const onClose = vi.fn()
function mount(document = doc) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <GuideEditor
        article='/a'
        document={document}
        open
        onClose={onClose}
        onSaved={onSaved}
      />
    </QueryClientProvider>
  )
}
afterEach(cleanup)
beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(getGuideVersions).mockResolvedValue([
    {
      revision: 0,
      path: '/a',
      at: 0,
      article: { ...doc.pages['/a'], html: '<h1>Baseline</h1>' },
      catalog: [],
    },
  ])
  vi.mocked(saveGuide).mockResolvedValue({ ...doc, revision: 4 })
})
it('previews edited HTML and publishes with the revision loaded by the editor', async () => {
  mount()
  fireEvent.change(screen.getByLabelText('guideEdit.html'), {
    target: { value: '<h1>Changed</h1><script>alert(1)</script>' },
  })
  expect(screen.getByRole('heading', { name: 'Changed' })).toBeInTheDocument()
  expect(document.querySelector('script')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'guideEdit.publish' }))
  await waitFor(() => expect(onSaved).toHaveBeenCalled())
  expect(saveGuide).toHaveBeenCalledWith(
    expect.objectContaining({
      revision: 3,
      html: '<h1>Changed</h1><script>alert(1)</script>',
    }),
    expect.anything()
  )
  expect(onClose).toHaveBeenCalled()
})
it('retains the draft when a newer server publication causes a conflict', async () => {
  vi.mocked(saveGuide).mockRejectedValue({
    isAxiosError: true,
    response: { status: 409 },
  })
  mount()
  fireEvent.change(screen.getByLabelText('guideEdit.html'), {
    target: { value: '<h1>My draft</h1>' },
  })
  fireEvent.click(screen.getByRole('button', { name: 'guideEdit.publish' }))
  await waitFor(() =>
    expect(screen.getByText('guideEdit.conflict')).toBeInTheDocument()
  )
  expect(screen.getByLabelText('guideEdit.html')).toHaveValue(
    '<h1>My draft</h1>'
  )
  expect(onClose).not.toHaveBeenCalled()
})
it('restores into a draft and requires confirmation before discarding changes', async () => {
  mount()
  await waitFor(() =>
    expect(screen.getByLabelText('guideEdit.history')).not.toBeDisabled()
  )
  fireEvent.change(screen.getByLabelText('guideEdit.history'), {
    target: { value: '0' },
  })
  expect(screen.getByLabelText('guideEdit.html')).toHaveValue(
    '<h1>Baseline</h1>'
  )
  expect(saveGuide).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: 'guideEdit.cancel' }))
  expect(onClose).not.toHaveBeenCalled()
  fireEvent.click(
    await screen.findByRole('button', { name: 'guideEdit.discard' })
  )
  expect(onClose).toHaveBeenCalled()
})
