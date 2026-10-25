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
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { normalizeAnnouncementStatus } from '../announcements'
import { AnnouncementsSection } from '../announcements-section'

const mockPut = vi.fn()
beforeEach(() => mockPut.mockReset())
vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn(),
    put: (...args: unknown[]) => mockPut(...args),
  },
}))

const legacyRecord = {
  id: 1,
  content: 'Legacy announcement',
  publishDate: '2026-10-01T00:00:00.000Z',
  type: 'default',
}

const draftRecord = {
  id: 2,
  content: 'Draft announcement',
  publishDate: '2026-10-02T00:00:00.000Z',
  type: 'warning',
  status: 'draft',
}

const withdrawnRecord = {
  id: 3,
  content: 'Withdrawn announcement',
  publishDate: '2026-10-03T00:00:00.000Z',
  type: 'error',
  status: 'withdrawn',
}

function renderSection(data: unknown[]) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const utils = render(
    <QueryClientProvider client={client}>
      <AnnouncementsSection enabled data={JSON.stringify(data)} />
    </QueryClientProvider>
  )
  return { client, ...utils }
}

describe('announcement release lifecycle', () => {
  it('creates a draft announcement and persists it only after saving settings', async () => {
    mockPut.mockResolvedValue({ data: { success: true } })
    const user = userEvent.setup()
    renderSection([])
    await user.click(screen.getByRole('button', { name: 'Add Announcement' }))
    const dialog = await screen.findByRole('dialog')
    await user.type(
      within(dialog).getByRole('textbox', { name: 'Content' }),
      'New release announcement'
    )
    await user.click(within(dialog).getByRole('button', { name: 'Add' }))
    expect(await screen.findByText('New release announcement')).toBeVisible()
    expect(screen.getByText('Draft')).toBeVisible()
    expect(mockPut).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Save Settings' }))
    await waitFor(() => expect(mockPut).toHaveBeenCalledTimes(1))
    expect(JSON.parse(mockPut.mock.calls[0][1].value)).toEqual([
      expect.objectContaining({
        content: 'New release announcement',
        status: 'draft',
      }),
    ])
  })

  it('edits an existing announcement while preserving its publication state', async () => {
    mockPut.mockResolvedValue({ data: { success: true } })
    const user = userEvent.setup()
    renderSection([draftRecord])
    await screen.findByText('Draft announcement')
    await user.click(screen.getByRole('button', { name: 'Edit' }))
    const dialog = await screen.findByRole('dialog')
    const content = within(dialog).getByRole('textbox', { name: 'Content' })
    await user.clear(content)
    await user.type(content, 'Edited release announcement')
    await user.click(within(dialog).getByRole('button', { name: 'Update' }))
    expect(await screen.findByText('Edited release announcement')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Save Settings' }))
    await waitFor(() => expect(mockPut).toHaveBeenCalledTimes(1))
    expect(JSON.parse(mockPut.mock.calls[0][1].value)).toEqual([
      {
        ...draftRecord,
        content: 'Edited release announcement',
        extra: '',
        display: 'timeline',
      },
    ])
  })

  it('deletes an announcement after confirmation and saves the empty list', async () => {
    mockPut.mockResolvedValue({ data: { success: true } })
    const user = userEvent.setup()
    renderSection([draftRecord])
    await screen.findByText('Draft announcement')
    await user.click(screen.getByRole('button', { name: 'Open menu' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Delete' }))
    const dialog = await screen.findByRole('alertdialog')
    expect(screen.getByText('Draft announcement')).toBeVisible()
    await user.click(within(dialog).getByRole('button', { name: 'Delete' }))
    await waitFor(() =>
      expect(screen.queryByText('Draft announcement')).not.toBeInTheDocument()
    )
    await user.click(screen.getByRole('button', { name: 'Save Settings' }))
    await waitFor(() => expect(mockPut).toHaveBeenCalledTimes(1))
    expect(JSON.parse(mockPut.mock.calls[0][1].value)).toEqual([])
  })

  it('treats records without a status as published and unknown statuses as drafts', () => {
    expect(normalizeAnnouncementStatus(undefined)).toBe('published')
    expect(normalizeAnnouncementStatus('')).toBe('published')
    expect(normalizeAnnouncementStatus('published')).toBe('published')
    expect(normalizeAnnouncementStatus('draft')).toBe('draft')
    expect(normalizeAnnouncementStatus('withdrawn')).toBe('withdrawn')
    expect(normalizeAnnouncementStatus('archived')).toBe('draft')
  })

  it('shows every stored record with its release state', async () => {
    renderSection([legacyRecord, draftRecord, withdrawnRecord])

    expect(await screen.findByText('Legacy announcement')).toBeVisible()
    expect(screen.getByText('Draft announcement')).toBeVisible()
    expect(screen.getByText('Withdrawn announcement')).toBeVisible()

    const rows = screen.getAllByRole('row')
    // The header row plus one row per announcement.
    expect(rows).toHaveLength(4)
    expect(within(rows[1]).getByText('Withdrawn')).toBeVisible()
    expect(within(rows[2]).getByText('Draft')).toBeVisible()
    expect(within(rows[3]).getByText('Published')).toBeVisible()
  })

  it('publishes a draft while preserving the old announcement until explicitly deleted', async () => {
    mockPut.mockResolvedValue({
      data: { success: true, message: '', data: null },
    })
    const user = userEvent.setup()
    renderSection([legacyRecord, draftRecord])

    await screen.findByText('Draft announcement')
    const draftRow = screen.getByRole('row', { name: /Draft announcement/ })
    await user.click(
      within(draftRow).getByRole('button', { name: 'Open menu' })
    )
    await user.click(await screen.findByRole('menuitem', { name: 'Publish' }))

    expect(within(draftRow).getByText('Published')).toBeVisible()
    expect(screen.getByText('Legacy announcement')).toBeVisible()

    await user.click(screen.getByRole('button', { name: 'Save Settings' }))

    await waitFor(() => expect(mockPut).toHaveBeenCalledTimes(1))
    const [, payload] = mockPut.mock.calls[0]
    const saved = JSON.parse(payload.value)
    expect(payload.key).toBe('console_setting.announcements')
    expect(saved).toEqual([
      { ...legacyRecord, status: 'published' },
      {
        ...draftRecord,
        status: 'published',
      },
    ])
  })
})

describe('announcement display placement', () => {
  it.each([
    ['notice', 'Notice only'],
    ['timeline', 'Timeline only'],
    ['both', 'Both locations'],
  ])(
    'saves %s placement on the same announcement and retains legacy records',
    async (placement, label) => {
      mockPut.mockResolvedValue({ data: { success: true } })
      const user = userEvent.setup()
      renderSection([legacyRecord, { ...draftRecord, display: 'both' }])
      const row = await screen.findByRole('row', { name: /Draft announcement/ })
      await user.click(within(row).getByRole('button', { name: 'Edit' }))
      const dialog = await screen.findByRole('dialog')
      await user.click(
        within(dialog).getByRole('combobox', { name: 'Display location' })
      )
      await user.click(await screen.findByRole('option', { name: label }))
      await user.click(within(dialog).getByRole('button', { name: 'Update' }))
      await user.click(screen.getByRole('button', { name: 'Save Settings' }))
      await waitFor(() => expect(mockPut).toHaveBeenCalledTimes(1))
      const saved = JSON.parse(mockPut.mock.calls[0][1].value)
      expect(saved).toHaveLength(2)
      expect(
        saved.find((item: { id: number }) => item.id === draftRecord.id)
      ).toEqual(expect.objectContaining({ ...draftRecord, display: placement }))
      expect(
        saved.find((item: { id: number }) => item.id === legacyRecord.id)
      ).toEqual(expect.objectContaining(legacyRecord))
    }
  )
})
