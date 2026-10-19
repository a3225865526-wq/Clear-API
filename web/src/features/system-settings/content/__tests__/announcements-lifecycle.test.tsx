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
import { describe, expect, it, vi } from 'vitest'

import { normalizeAnnouncementStatus } from '../announcements'
import { AnnouncementsSection } from '../announcements-section'

const mockPut = vi.fn()
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

  it('publishes a draft through the row menu and saves the new status', async () => {
    mockPut.mockResolvedValue({
      data: { success: true, message: '', data: null },
    })
    const user = userEvent.setup()
    renderSection([draftRecord])

    await screen.findByText('Draft announcement')
    await user.click(screen.getByRole('button', { name: 'Open menu' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Publish' }))

    expect(await screen.findByText('Published')).toBeVisible()

    await user.click(screen.getByRole('button', { name: 'Save Settings' }))

    await waitFor(() => expect(mockPut).toHaveBeenCalledTimes(1))
    const [, payload] = mockPut.mock.calls[0]
    const saved = JSON.parse(payload.value)
    expect(payload.key).toBe('console_setting.announcements')
    expect(saved).toEqual([
      {
        ...draftRecord,
        status: 'published',
      },
    ])
  })
})
