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
import { poolRequest } from './access'
import type {
  CompletePoolAuthInput,
  CreatePoolAccountInput,
  PoolAccount,
  PoolAccountCapabilities,
  PoolAccountDetail,
  PoolAuthStart,
  PoolCatalog,
  PoolExecutor,
} from './types'

export function getPoolExecutors(signal?: AbortSignal) {
  return poolRequest<PoolExecutor[]>('/executors', { signal })
}

export function getPoolCatalog(signal?: AbortSignal) {
  return poolRequest<PoolCatalog>('/catalog', { signal })
}

export function getPoolAccounts(
  params: { platform?: string; admin_status?: string; run_status?: string },
  signal?: AbortSignal
) {
  return poolRequest<PoolAccount[]>('/accounts', { params, signal })
}

export function createPoolAccount(input: CreatePoolAccountInput) {
  return poolRequest<PoolAccount>('/accounts', { method: 'POST', body: input })
}

export function getPoolAccount(id: number, signal?: AbortSignal) {
  return poolRequest<PoolAccountDetail>(`/accounts/${id}`, { signal })
}

export function getPoolAccountCapabilities(id: number, signal?: AbortSignal) {
  return poolRequest<PoolAccountCapabilities>(`/accounts/${id}/capabilities`, {
    signal,
  })
}

export function startPoolAccountAuth(id: number) {
  return poolRequest<PoolAuthStart>(`/accounts/${id}/auth/start`, {
    method: 'POST',
    body: { kind: 'initial' },
  })
}

export function startPoolAccountReauth(id: number) {
  return poolRequest<PoolAuthStart>(`/accounts/${id}/reauth`, {
    method: 'POST',
    body: {},
  })
}

export function completePoolAccountAuth(
  id: number,
  input: CompletePoolAuthInput
) {
  return poolRequest<PoolAccount>(`/accounts/${id}/auth/complete`, {
    method: 'POST',
    body: input,
  })
}

export function cancelPoolAccountAuth(id: number, taskId: string) {
  return poolRequest<{ cancelled: boolean }>(`/accounts/${id}/auth/cancel`, {
    method: 'POST',
    body: { task_id: taskId },
  })
}

export function pausePoolAccount(id: number, expectedVersion: number) {
  return poolRequest<PoolAccount>(`/accounts/${id}/pause`, {
    method: 'POST',
    body: { expected_version: expectedVersion },
  })
}

export function resumePoolAccount(id: number, expectedVersion: number) {
  return poolRequest<PoolAccount>(`/accounts/${id}/resume`, {
    method: 'POST',
    body: { expected_version: expectedVersion },
  })
}

export function deletePoolAccount(id: number) {
  return poolRequest<{ deleted: boolean }>(`/accounts/${id}/delete`, {
    method: 'POST',
    body: {},
  })
}

export function syncPoolAccount(id: number) {
  return poolRequest<PoolAccount>(`/accounts/${id}/sync`, { method: 'POST' })
}
