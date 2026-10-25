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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'

import { api, getNotice } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'
import { statusQueryOptions } from '@/lib/status-query'
import { useAuthStore } from '@/stores/auth-store'
import { useNotificationStore } from '@/stores/notification-store'

type Announcement = {
  id?: number | string
  content?: string
  read_key?: string
  [key: string]: unknown
}
type ReadResponse = {
  success: boolean
  data: { key: string }[]
  notice_key: string
  notice_content: string
}

export function useNotifications() {
  const [popoverOpen, setPopoverOpen] = useState(false)
  const [activeTab, setActiveTab] = useState<'notice' | 'announcements'>(
    'notice'
  )
  const userId = useAuthStore((state) => state.auth.user?.id)
  const client = useQueryClient()
  const store = useNotificationStore()
  const {
    data: noticeResponse,
    isLoading: noticeLoading,
    refetch: refetchNotice,
  } = useQuery({
    queryKey: ['notice'],
    queryFn: async () => requireServerSuccess(await getNotice()),
    staleTime: 60000,
    refetchInterval: 60000,
  })
  const { data: status, isLoading: statusLoading } = useQuery({
    ...statusQueryOptions,
    refetchInterval: 60000,
    refetchIntervalInBackground: false,
  })
  const enabled = status?.announcements_enabled ?? false
  const announcements = useMemo(
    () => (enabled ? ((status?.announcements ?? []) as Announcement[]) : []),
    [enabled, status?.announcements]
  )
  const notificationAnnouncements = useMemo(
    () =>
      enabled
        ? ((status?.notification_announcements ?? []) as Announcement[])
        : [],
    [enabled, status?.notification_announcements]
  )
  const readQuery = useQuery({
    queryKey: ['announcement-reads', userId],
    enabled: Boolean(userId),
    queryFn: async () =>
      requireServerSuccess(
        (await api.get<ReadResponse>('/api/console/announcement-reads')).data
      ),
  })
  const mark = useMutation({
    mutationFn: async (request: { userId: number; keys: string[] }) =>
      requireServerSuccess(
        (
          await api.post('/api/console/announcement-reads', {
            keys: request.keys,
          })
        ).data
      ),
    onSuccess: (_data, request) => {
      client.setQueryData<ReadResponse>(
        ['announcement-reads', request.userId],
        (old) =>
          old
            ? {
                ...old,
                data: [...old.data, ...request.keys.map((key) => ({ key }))],
              }
            : old
      )
    },
  })
  const notice = noticeResponse?.success
    ? (noticeResponse.data ?? '').trim()
    : ''
  const readKeys = new Set(
    userId
      ? readQuery.data?.data.map((item) => item.key)
      : store.readAnnouncementKeys
  )
  const keyFor = (item: Announcement) => item.read_key ?? JSON.stringify(item)
  const noticeRead = userId
    ? Boolean(
        readQuery.data?.notice_content === notice &&
        readKeys.has(readQuery.data.notice_key)
      )
    : store.lastReadNotice === notice
  const unreadNoticeCount =
    (notice && !noticeRead ? 1 : 0) +
    notificationAnnouncements.filter((item) => !readKeys.has(keyFor(item)))
      .length
  const unreadAnnouncementsCount = announcements.filter(
    (item) => !readKeys.has(keyFor(item))
  ).length
  // A both-locations announcement contributes once to the bell badge.
  const allUnread = new Set(
    [...announcements, ...notificationAnnouncements]
      .filter((item) => !readKeys.has(keyFor(item)))
      .map(keyFor)
  )
  const markVisibleAsRead = () => {
    const visible =
      activeTab === 'notice' ? notificationAnnouncements : announcements
    const keys = visible.map(keyFor).filter((key) => !readKeys.has(key))
    if (userId) {
      if (!readQuery.isSuccess || mark.isPending) return
      if (
        activeTab === 'notice' &&
        notice &&
        !noticeRead &&
        readQuery.data.notice_content === notice
      ) {
        keys.push(readQuery.data.notice_key)
      }
      if (keys.length) mark.mutate({ userId, keys: [...new Set(keys)] })
    } else {
      store.markAnnouncementsRead(keys)
      if (activeTab === 'notice' && notice) store.markNoticeRead(notice)
    }
  }
  const openPopover = (tab?: 'notice' | 'announcements') => {
    if (tab) setActiveTab(tab)
    setPopoverOpen(true)
    void client.invalidateQueries({ queryKey: ['status'] })
    void refetchNotice()
    if (userId) void readQuery.refetch()
  }
  return {
    notice,
    announcements,
    notificationAnnouncements,
    loading:
      noticeLoading || statusLoading || Boolean(userId && readQuery.isPending),
    unreadCount: allUnread.size + (notice && !noticeRead ? 1 : 0),
    unreadNoticeCount,
    unreadAnnouncementsCount,
    popoverOpen,
    activeTab,
    setActiveTab,
    setPopoverOpen: (open: boolean) =>
      open ? openPopover() : setPopoverOpen(false),
    openPopover,
    closePopover: () => setPopoverOpen(false),
    refetchNotice,
    markVisibleAsRead,
    markingRead: mark.isPending,
  }
}
