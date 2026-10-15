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
import { isAxiosError } from 'axios'
import { t } from 'i18next'

import { getServerErrorMessage } from '@/lib/server-error-message'

import { ROUTE_CHAIN_ISSUE_LABELS } from '../constants'
import type { RouteIssue } from '../types'

export const ROUTE_CHAIN_CONFLICT_CODE = 'ROUTE_CHAIN_VERSION_CONFLICT'
export const ROUTE_CHAIN_HAS_HISTORY_CODE = 'ROUTE_CHAIN_HAS_HISTORY'

function responseCode(error: unknown): string | undefined {
  if (isAxiosError(error)) {
    const code = error.response?.data?.code
    return typeof code === 'string' ? code : undefined
  }
  return undefined
}

/** Stale CAS version — the operator must review the latest state (§五). */
export function isConflictError(error: unknown): boolean {
  return responseCode(error) === ROUTE_CHAIN_CONFLICT_CODE
}

/** Hard-delete refused because the attempt ledger references the chain. */
export function isHistoryDeleteError(error: unknown): boolean {
  return responseCode(error) === ROUTE_CHAIN_HAS_HISTORY_CODE
}

interface IssuePayload {
  code?: string
  errors?: RouteIssue[]
}

/** Backend validation issues from a 400 ROUTE_CHAIN_INVALID response. */
function issuePayload(error: unknown): IssuePayload | null {
  if (!isAxiosError(error)) return null
  const data = error.response?.data
  if (!data || typeof data !== 'object') return null
  const payload = data as IssuePayload
  return payload.code === 'ROUTE_CHAIN_INVALID' ? payload : null
}

/**
 * Maps backend issue codes onto operator-readable copy (§三十七). Returns
 * localized lines — never raw Go/SQL errors.
 */
export function chainIssueMessages(error: unknown): string[] {
  const payload = issuePayload(error)
  const issues = payload?.errors ?? []
  const messages: string[] = []
  for (const issue of issues) {
    const template = ROUTE_CHAIN_ISSUE_LABELS[issue.code]
    if (template) messages.push(t(template))
  }
  return messages
}

/** One localized error line for toasts: issues first, server message second. */
export function chainErrorMessage(error: unknown, fallback: string): string {
  const messages = chainIssueMessages(error)
  if (messages.length > 0) return messages.join(' ')
  return getServerErrorMessage(error, fallback)
}
