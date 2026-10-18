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
import type { StatusVariant } from '@/components/status-badge'

export type AnnouncementStatus = 'draft' | 'published' | 'withdrawn'

export type AnnouncementType =
  | 'default'
  | 'ongoing'
  | 'success'
  | 'warning'
  | 'error'

export type Announcement = {
  id: number
  content: string
  publishDate: string
  type: AnnouncementType
  status: AnnouncementStatus
  extra?: string
}

export const ANNOUNCEMENT_TYPES: ReadonlyArray<{
  value: AnnouncementType
  labelKey: string
  color: string
  badgeVariant: StatusVariant
}> = [
  {
    value: 'default',
    labelKey: 'Default',
    color: 'bg-gray-500',
    badgeVariant: 'neutral',
  },
  {
    value: 'ongoing',
    labelKey: 'Ongoing',
    color: 'bg-blue-500',
    badgeVariant: 'info',
  },
  {
    value: 'success',
    labelKey: 'Success',
    color: 'bg-green-500',
    badgeVariant: 'success',
  },
  {
    value: 'warning',
    labelKey: 'Warning',
    color: 'bg-orange-500',
    badgeVariant: 'warning',
  },
  {
    value: 'error',
    labelKey: 'Error',
    color: 'bg-red-500',
    badgeVariant: 'danger',
  },
]

export const ANNOUNCEMENT_STATUSES: ReadonlyArray<{
  value: AnnouncementStatus
  labelKey: string
  badgeVariant: StatusVariant
}> = [
  { value: 'draft', labelKey: 'Draft', badgeVariant: 'neutral' },
  { value: 'published', labelKey: 'Published', badgeVariant: 'success' },
  { value: 'withdrawn', labelKey: 'Withdrawn', badgeVariant: 'warning' },
]

/**
 * Announcements written before the publish lifecycle existed carry no status.
 * They were already visible to users, so they are treated as published. The
 * backend also hides any status it cannot recognize; showing such a record as
 * a draft here keeps the administrator able to fix it.
 */
export function normalizeAnnouncementStatus(
  status: unknown
): AnnouncementStatus {
  if (status === 'draft' || status === 'published' || status === 'withdrawn') {
    return status
  }
  return status === undefined || status === null || status === ''
    ? 'published'
    : 'draft'
}

export function getAnnouncementTypeOption(type: string) {
  return ANNOUNCEMENT_TYPES.find((option) => option.value === type)
}

export function getAnnouncementStatusOption(status: AnnouncementStatus) {
  return ANNOUNCEMENT_STATUSES.find((option) => option.value === status)
}
