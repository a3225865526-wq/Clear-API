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
import {
  useQuery,
  useQueryClient,
  type QueryClient,
} from '@tanstack/react-query'
import { CanceledError, isAxiosError } from 'axios'
import { useEffect, useSyncExternalStore } from 'react'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import type { Capability, Range, Summary } from './types'

export const financeKey = ['owner-finance'] as const
function identity() {
  const { user, session, accessToken } = useAuthStore.getState().auth
  return user && session && accessToken
    ? JSON.stringify([
        user.id,
        session.sid,
        user.role,
        user.status,
        user.permissions,
      ])
    : ''
}
let scope = identity()
let generation = 0
let denied = false
const listeners = new Set<() => void>()
const clients = new Map<QueryClient, number>()
const requests = new Set<AbortController>()
function purge() {
  generation++
  for (const request of requests) request.abort()
  requests.clear()
  for (const client of clients.keys()) {
    void client.cancelQueries({ queryKey: financeKey })
    client.removeQueries({ queryKey: financeKey })
  }
  for (const notify of listeners) notify()
}
useAuthStore.subscribe(() => {
  const next = identity()
  if (next !== scope) {
    scope = next
    denied = false
    purge()
  }
})
function revoke() {
  denied = true
  purge()
}
function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}
export function bindFinanceClient(client: QueryClient) {
  clients.set(client, (clients.get(client) ?? 0) + 1)
  return () => {
    const count = (clients.get(client) ?? 1) - 1
    if (count) clients.set(client, count)
    else {
      clients.delete(client)
      void client.cancelQueries({ queryKey: financeKey })
      client.removeQueries({ queryKey: financeKey })
    }
  }
}

// No refresh/replay across identities; canceled or late responses cannot enter
// the new owner's cache. Controllers also cover direct previews and writes.
export async function financeRequest<T>(
  path: string,
  options: {
    method?: 'GET' | 'POST'
    params?: object
    body?: unknown
    signal?: AbortSignal
  } = {}
): Promise<T> {
  const expected = generation
  if (!scope || denied || options.signal?.aborted) throw new CanceledError()
  const controller = new AbortController()
  const abort = () => controller.abort()
  options.signal?.addEventListener('abort', abort, { once: true })
  requests.add(controller)
  try {
    const response = await api.request<{ success: boolean; data: T }>({
      url: `/api/owner/finance${path}`,
      method: options.method ?? 'GET',
      params: options.params,
      data: options.body,
      signal: controller.signal,
      disableDuplicate: true,
      skipAuthRefresh: true,
      skipErrorHandler: true,
    })
    if (expected !== generation || controller.signal.aborted) {
      throw new CanceledError()
    }
    if (!response.data.success) throw new Error('FINANCE_UNAVAILABLE')
    return response.data.data
  } catch (error) {
    if (expected !== generation || controller.signal.aborted) {
      throw new CanceledError()
    }
    if (
      isAxiosError(error) &&
      [401, 403].includes(error.response?.status ?? 0)
    ) {
      revoke()
    }
    // Never retain a driver response, rejected evidence or payload in an error.
    throw new Error(
      isAxiosError(error) && error.response?.status === 409
        ? 'FINANCE_CONFLICT'
        : 'FINANCE_UNAVAILABLE'
    )
  } finally {
    requests.delete(controller)
    options.signal?.removeEventListener('abort', abort)
  }
}
export async function checkFinanceAccess() {
  const capability = await financeRequest<Capability>('/capability')
  if (!capability.can_access) {
    revoke()
    return false
  }
  return true
}
export function useFinanceAccess() {
  const epoch = useSyncExternalStore(
    subscribe,
    () => generation,
    () => 0
  )
  const client = useQueryClient()
  useEffect(() => bindFinanceClient(client), [client])
  const query = useQuery({
    queryKey: [...financeKey, epoch, 'capability'],
    queryFn: async ({ signal }) => {
      const result = await financeRequest<Capability>('/capability', { signal })
      if (!result.can_access) revoke()
      return result
    },
    enabled: Boolean(scope) && !denied,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnWindowFocus: true,
    refetchInterval: 60_000,
    meta: { errorToast: false },
  })
  return {
    ...query,
    epoch,
    allowed: Boolean(scope) && !denied && query.data?.can_access === true,
  }
}
export function useFinanceSummary(
  range: Range,
  epoch: number,
  enabled: boolean
) {
  return useQuery({
    queryKey: [...financeKey, epoch, 'summary', range],
    queryFn: ({ signal }) =>
      financeRequest<Summary>('/profit-summary', { params: range, signal }),
    enabled,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnWindowFocus: true,
    meta: { errorToast: false },
  })
}
