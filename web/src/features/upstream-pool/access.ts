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
import { CanceledError, isAxiosError, isCancel } from 'axios'
import { useEffect, useSyncExternalStore } from 'react'

import { api } from '@/lib/api'
import { safeServerErrorMessage } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import {
  POOL_ERROR_MESSAGE_KEYS,
  POOL_QUERY_KEY,
  POOL_UNAVAILABLE_MESSAGE_KEY,
} from './constants'
import type { PoolCapability } from './types'

export const poolKey = POOL_QUERY_KEY
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
    void client.cancelQueries({ queryKey: poolKey })
    client.removeQueries({ queryKey: poolKey })
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
export function bindPoolClient(client: QueryClient) {
  clients.set(client, (clients.get(client) ?? 0) + 1)
  return () => {
    const count = (clients.get(client) ?? 1) - 1
    if (count) clients.set(client, count)
    else {
      clients.delete(client)
      void client.cancelQueries({ queryKey: poolKey })
      client.removeQueries({ queryKey: poolKey })
    }
  }
}

/**
 * Maps a pool failure to a translated, secret-free error. Server payloads are
 * never carried: only the stable i18n key derived from the response code. The
 * original error stays as `cause` so status-based handling (e.g. 409) works.
 */
function safePoolError(messageKey: string, cause?: unknown): Error {
  const error = new Error(messageKey)
  Object.defineProperty(error, safeServerErrorMessage, { value: true })
  if (cause !== undefined) {
    ;(error as { cause?: unknown }).cause = cause
  }
  return error
}
function poolError(error: unknown): Error {
  if (isAxiosError(error)) {
    const code = error.response?.data?.code
    const key =
      typeof code === 'string' && POOL_ERROR_MESSAGE_KEYS[code]
        ? POOL_ERROR_MESSAGE_KEYS[code]
        : POOL_UNAVAILABLE_MESSAGE_KEY
    return safePoolError(key, error)
  }
  return safePoolError(POOL_UNAVAILABLE_MESSAGE_KEY, error)
}

// No refresh/replay across identities; canceled or late responses cannot enter
// the new owner's cache. Controllers also cover direct reads and writes.
export async function poolRequest<T>(
  path: string,
  options: {
    method?: 'GET' | 'POST' | 'PUT'
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
      url: `/api/owner/pool${path}`,
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
    if (!response.data.success) throw new Error('POOL_UNAVAILABLE')
    return response.data.data
  } catch (error) {
    if (expected !== generation || controller.signal.aborted) {
      throw new CanceledError()
    }
    if (isCancel(error)) throw error
    if (
      isAxiosError(error) &&
      [401, 403].includes(error.response?.status ?? 0)
    ) {
      revoke()
    }
    throw poolError(error)
  } finally {
    requests.delete(controller)
    options.signal?.removeEventListener('abort', abort)
  }
}
export async function checkPoolAccess() {
  const capability = await poolRequest<PoolCapability>('/capability')
  if (!capability.can_access) {
    revoke()
    return false
  }
  return true
}
export function usePoolAccess() {
  const epoch = useSyncExternalStore(
    subscribe,
    () => generation,
    () => 0
  )
  const client = useQueryClient()
  useEffect(() => bindPoolClient(client), [client])
  const query = useQuery({
    queryKey: [...poolKey, epoch, 'capability'],
    queryFn: async ({ signal }) => {
      const result = await poolRequest<PoolCapability>('/capability', {
        signal,
      })
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
