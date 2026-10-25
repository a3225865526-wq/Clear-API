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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useNotifications } from '@/hooks/use-notifications'
import { useNotificationStore } from '@/stores/notification-store'

import type { UpdateOptionRequest } from '../../types'
import { useUpdateOption } from '../use-update-option'

const server = vi.hoisted(() => ({
  announcements: [] as Array<{ id: number; content: string }>,
  enabled: true,
  notice: '',
}))

vi.mock('@/lib/api', () => ({
  api: {
    put: async (
      _url: string,
      request: { key: string; value: string | boolean }
    ) => {
      if (request.key === 'console_setting.announcements') {
        server.announcements = JSON.parse(String(request.value))
      }
      if (request.key === 'console_setting.announcements_enabled') {
        server.enabled = Boolean(request.value)
      }
      if (request.key === 'Notice') server.notice = String(request.value)
      return { data: { success: true } }
    },
  },
  getStatus: async () => ({
    announcements: server.announcements,
    announcements_enabled: server.enabled,
  }),
  getNotice: async () => ({ success: true, data: server.notice }),
}))

function NotificationSettingsFixture(props: { update: UpdateOptionRequest }) {
  const notifications = useNotifications()
  const update = useUpdateOption()
  return (
    <>
      <button type='button' onClick={() => update.mutate(props.update)}>
        Save
      </button>
      <output aria-label='announcements'>
        {notifications.announcements.map((item) => item.content).join('|') ||
          'empty'}
      </output>
      <output aria-label='notice'>{notifications.notice || 'empty'}</output>
      <output aria-label='loading'>{String(notifications.loading)}</output>
    </>
  )
}

beforeEach(() => {
  server.announcements = []
  server.enabled = true
  server.notice = ''
  window.localStorage.clear()
  useNotificationStore.setState({
    lastReadNotice: '',
    readAnnouncementKeys: [],
    closedUntilDate: null,
  })
})

describe('notification settings cache refresh', () => {
  it.each([
    {
      key: 'console_setting.announcements',
      value: JSON.stringify([
        { id: 1, content: 'Published audit announcement' },
      ]),
      output: 'announcements',
      expected: 'Published audit announcement',
    },
    {
      key: 'console_setting.announcements_enabled',
      value: false,
      output: 'announcements',
      expected: 'empty',
    },
    {
      key: 'Notice',
      value: 'Updated system notice',
      output: 'notice',
      expected: 'Updated system notice',
    },
  ])(
    'updates the visible $output after saving $key without a page reload',
    async (example) => {
      if (example.key.endsWith('_enabled')) {
        server.announcements = [
          { id: 1, content: 'Previously visible announcement' },
        ]
      }
      const client = new QueryClient({
        defaultOptions: {
          queries: { retry: false, gcTime: 0 },
          mutations: { retry: false },
        },
      })
      render(
        <QueryClientProvider client={client}>
          <NotificationSettingsFixture
            update={{ key: example.key, value: example.value }}
          />
        </QueryClientProvider>
      )
      await waitFor(() =>
        expect(screen.getByLabelText('loading')).toHaveTextContent('false')
      )
      if (example.key.endsWith('_enabled')) {
        expect(screen.getByLabelText('announcements')).toHaveTextContent(
          'Previously visible announcement'
        )
      }
      await userEvent
        .setup()
        .click(screen.getByRole('button', { name: 'Save' }))
      await waitFor(() =>
        expect(screen.getByLabelText(example.output)).toHaveTextContent(
          example.expected
        )
      )
      client.clear()
    }
  )
})
