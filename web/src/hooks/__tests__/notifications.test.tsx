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
import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { NotificationPopover } from '@/components/notification-popover'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'
import { useNotificationStore } from '@/stores/notification-store'

import { useNotifications } from '../use-notifications'

const records = [
  { id: 1, content: 'Notice placement', read_key: 'key-notice' },
  { id: 2, content: 'Timeline placement', read_key: 'key-timeline' },
  { id: 3, content: 'Shared announcement', read_key: 'key-both' },
]
vi.mock('@/lib/api', () => ({
  api: { get: vi.fn(), post: vi.fn() },
  getNotice: async () => ({ success: true, data: '' }),
  getStatus: async () => ({
    announcements_enabled: true,
    announcements: [records[1], records[2]],
    notification_announcements: [records[0], records[2]],
  }),
}))
const saved = new Map<number, string[]>()
let client: QueryClient
const originalGetAnimations = Object.getOwnPropertyDescriptor(
  HTMLElement.prototype,
  'getAnimations'
)
function Fixture() {
  const state = useNotifications()
  return (
    <>
      <output aria-label='Unread'>{state.unreadCount}</output>
      <NotificationPopover
        open={state.popoverOpen}
        onOpenChange={state.setPopoverOpen}
        unreadCount={state.unreadCount}
        activeTab={state.activeTab}
        onTabChange={state.setActiveTab}
        notice={state.notice}
        announcements={state.announcements}
        notificationAnnouncements={state.notificationAnnouncements}
        onMarkRead={state.markVisibleAsRead}
        markingRead={state.markingRead}
        loading={state.loading}
      />
    </>
  )
}
function mount() {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <Fixture />
    </QueryClientProvider>
  )
}
beforeEach(() => {
  Object.defineProperty(HTMLElement.prototype, 'getAnimations', {
    configurable: true,
    value: () => [],
  })
  saved.clear()
  useAuthStore.getState().auth.setUser({ id: 1, username: 'user-a', role: 1 })
  useNotificationStore.setState({
    lastReadNotice: '',
    readAnnouncementKeys: [],
  })
  window.localStorage.clear()
  vi.mocked(api.get).mockImplementation(async () => ({
    data: {
      success: true,
      data: (saved.get(useAuthStore.getState().auth.user?.id ?? -1) ?? []).map(
        (key) => ({ key })
      ),
      notice_key: '',
      notice_content: '',
    },
  }))
  vi.mocked(api.post).mockImplementation(async (_path, body) => {
    const id = useAuthStore.getState().auth.user?.id ?? -1
    const keys = (body as { keys: string[] }).keys
    saved.set(id, [...new Set([...(saved.get(id) ?? []), ...keys])])
    return { data: { success: true } }
  })
})
afterEach(() => {
  client.clear()
  useAuthStore.getState().auth.reset()
  if (originalGetAnimations) {
    Object.defineProperty(
      HTMLElement.prototype,
      'getAnimations',
      originalGetAnimations
    )
  } else Reflect.deleteProperty(HTMLElement.prototype, 'getAnimations')
})

describe('notification placement and persistent reads', () => {
  it('shows each placement in its selected tab and counts a shared record once', async () => {
    mount()
    await waitFor(() =>
      expect(screen.getByLabelText('Unread')).toHaveTextContent('3')
    )
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Notifications' }))
    const notice = await screen.findByRole('tabpanel')
    expect(within(notice).getByText('Notice placement')).toBeVisible()
    expect(within(notice).getByText('Shared announcement')).toBeVisible()
    expect(
      within(notice).queryByText('Timeline placement')
    ).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Mark as read' }))
    await waitFor(() =>
      expect(screen.getByLabelText('Unread')).toHaveTextContent('1')
    )
    expect(saved.get(1)).toEqual(['key-notice', 'key-both'])
    await user.click(screen.getByRole('tab', { name: 'Timeline' }))
    const timeline = await screen.findByRole('tabpanel')
    expect(
      within(timeline).queryByText('Notice placement')
    ).not.toBeInTheDocument()
    expect(within(timeline).getByText('Timeline placement')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Mark as read' }))
    await waitFor(() =>
      expect(screen.getByLabelText('Unread')).toHaveTextContent('0')
    )
  })
  it('restores server reads after a fresh client and keeps a different user unread', async () => {
    saved.set(1, ['key-notice', 'key-timeline', 'key-both'])
    const view = mount()
    await waitFor(() =>
      expect(screen.getByLabelText('Unread')).toHaveTextContent('0')
    )
    view.unmount()
    client.clear()
    mount()
    await waitFor(() =>
      expect(screen.getByLabelText('Unread')).toHaveTextContent('0')
    )
    act(() =>
      useAuthStore
        .getState()
        .auth.setUser({ id: 2, username: 'user-b', role: 1 })
    )
    await waitFor(() =>
      expect(screen.getByLabelText('Unread')).toHaveTextContent('3')
    )
  })
  it('retains unread counts after a rejected save and allows a successful retry', async () => {
    vi.mocked(api.post).mockRejectedValueOnce(
      new Error('isolated save failure')
    )
    mount()
    const user = userEvent.setup()
    await waitFor(() =>
      expect(screen.getByLabelText('Unread')).toHaveTextContent('3')
    )
    await user.click(screen.getByRole('button', { name: 'Notifications' }))
    await user.click(
      await screen.findByRole('button', { name: 'Mark as read' })
    )
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: 'Mark as read' })
      ).not.toBeDisabled()
    )
    expect(screen.getByLabelText('Unread')).toHaveTextContent('3')
    expect(saved.get(1)).toBeUndefined()
    await user.click(screen.getByRole('button', { name: 'Mark as read' }))
    await waitFor(() =>
      expect(screen.getByLabelText('Unread')).toHaveTextContent('1')
    )
  })
})
