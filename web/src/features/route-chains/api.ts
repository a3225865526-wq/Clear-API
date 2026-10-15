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
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import type {
  AttemptView,
  ChainDetail,
  ChainListItem,
  ChainSpecInput,
  DecisionView,
  GateStates,
  TargetInfo,
} from './types'

interface ApiEnvelope<T> {
  success: boolean
  message?: string
  data: T
  code?: string
  errors?: unknown
}

export async function listRouteChains(
  includeArchived: boolean,
  signal?: AbortSignal
): Promise<{ items: ChainListItem[]; gates: GateStates }> {
  const response = await api.get<ApiEnvelope<{ items: ChainListItem[]; gates: GateStates }>>(
    '/api/route-chains',
    {
      params: includeArchived ? { include_archived: 'true' } : undefined,
      signal,
    }
  )
  return requireServerSuccess(response.data).data
}

export async function getRouteChainGates(signal?: AbortSignal): Promise<GateStates> {
  const response = await api.get<ApiEnvelope<GateStates>>('/api/route-chains/gates', {
    signal,
  })
  return requireServerSuccess(response.data).data
}

export async function getRouteChainTargets(
  signal?: AbortSignal
): Promise<TargetInfo[]> {
  const response = await api.get<ApiEnvelope<TargetInfo[]>>(
    '/api/route-chains/targets',
    { signal }
  )
  return requireServerSuccess(response.data).data
}

export async function getRouteChainDetail(
  id: number,
  signal?: AbortSignal
): Promise<ChainDetail> {
  const response = await api.get<ApiEnvelope<{ chain: ChainDetail }>>(
    `/api/route-chains/${id}`,
    { signal }
  )
  return requireServerSuccess(response.data).data.chain
}

export async function createRouteChain(
  input: ChainSpecInput
): Promise<{ chain: ChainDetail; warnings: unknown[] }> {
  const response = await api.post<ApiEnvelope<{ chain: ChainDetail; warnings: unknown[] }>>(
    '/api/route-chains',
    input,
    { skipBusinessError: true, skipErrorHandler: true }
  )
  return requireServerSuccess(response.data).data
}

export async function updateRouteChain(
  id: number,
  input: ChainSpecInput & { expected_version: number }
): Promise<{ chain: ChainDetail; version: number; warnings: unknown[] }> {
  const response = await api.put<
    ApiEnvelope<{ chain: ChainDetail; version: number; warnings: unknown[] }>
  >(`/api/route-chains/${id}`, input, {
    skipBusinessError: true,
    skipErrorHandler: true,
  })
  return requireServerSuccess(response.data).data
}

export async function setRouteChainEnabled(
  id: number,
  enabled: boolean,
  expectedVersion: number
): Promise<{ version: number }> {
  const response = await api.post<ApiEnvelope<{ version: number }>>(
    `/api/route-chains/${id}/enable`,
    { enabled, expected_version: expectedVersion },
    { skipBusinessError: true, skipErrorHandler: true }
  )
  return requireServerSuccess(response.data).data
}

export async function reorderRouteChainCandidates(
  id: number,
  orderedCandidateIds: number[],
  expectedVersion: number
): Promise<{ version: number }> {
  const response = await api.post<ApiEnvelope<{ version: number }>>(
    `/api/route-chains/${id}/reorder`,
    { ordered_candidate_ids: orderedCandidateIds, expected_version: expectedVersion },
    { skipBusinessError: true, skipErrorHandler: true }
  )
  return requireServerSuccess(response.data).data
}

export async function archiveRouteChain(
  id: number,
  expectedVersion: number
): Promise<{ version: number }> {
  const response = await api.post<ApiEnvelope<{ version: number }>>(
    `/api/route-chains/${id}/archive`,
    { expected_version: expectedVersion },
    { skipBusinessError: true, skipErrorHandler: true }
  )
  return requireServerSuccess(response.data).data
}

export async function deleteRouteChain(
  id: number,
  expectedVersion: number
): Promise<{ deleted: boolean }> {
  const response = await api.delete<ApiEnvelope<{ deleted: boolean }>>(
    `/api/route-chains/${id}`,
    { data: { expected_version: expectedVersion }, skipBusinessError: true, skipErrorHandler: true }
  )
  return requireServerSuccess(response.data).data
}

export async function setRouteChainCandidateEnabled(
  chainId: number,
  candidateId: number,
  enabled: boolean,
  expectedVersion: number
): Promise<{ version: number }> {
  const response = await api.post<ApiEnvelope<{ version: number }>>(
    `/api/route-chains/${chainId}/candidates/${candidateId}/enabled`,
    { enabled, expected_version: expectedVersion },
    { skipBusinessError: true, skipErrorHandler: true }
  )
  return requireServerSuccess(response.data).data
}

export async function resetRouteChainCandidateCircuit(
  chainId: number,
  candidateId: number
): Promise<unknown> {
  const response = await api.post<ApiEnvelope<unknown>>(
    `/api/route-chains/${chainId}/candidates/${candidateId}/circuit/reset`,
    {},
    { skipBusinessError: true, skipErrorHandler: true }
  )
  return requireServerSuccess(response.data).data
}

export async function forceOpenRouteChainCandidateCircuit(
  chainId: number,
  candidateId: number,
  durationSeconds: number
): Promise<unknown> {
  const response = await api.post<ApiEnvelope<unknown>>(
    `/api/route-chains/${chainId}/candidates/${candidateId}/circuit/open`,
    { duration_seconds: durationSeconds },
    { skipBusinessError: true, skipErrorHandler: true }
  )
  return requireServerSuccess(response.data).data
}

export async function getRouteChainAttempts(
  chainId: number,
  params: { limit?: number; candidate_id?: number },
  signal?: AbortSignal
): Promise<AttemptView[]> {
  const response = await api.get<ApiEnvelope<{ items: AttemptView[] }>>(
    `/api/route-chains/${chainId}/attempts`,
    { params, signal }
  )
  return requireServerSuccess(response.data).data.items
}

export async function getRouteChainDecisions(
  chainId: number,
  params: { limit?: number; candidate_id?: number },
  signal?: AbortSignal
): Promise<DecisionView[]> {
  const response = await api.get<ApiEnvelope<{ items: DecisionView[] }>>(
    `/api/route-chains/${chainId}/decisions`,
    { params, signal }
  )
  return requireServerSuccess(response.data).data.items
}
