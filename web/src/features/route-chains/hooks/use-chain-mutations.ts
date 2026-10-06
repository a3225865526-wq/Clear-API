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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { useTranslation } from 'react-i18next'

import {
  archiveRouteChain,
  createRouteChain,
  deleteRouteChain,
  forceOpenRouteChainCandidateCircuit,
  reorderRouteChainCandidates,
  resetRouteChainCandidateCircuit,
  setRouteChainCandidateEnabled,
  setRouteChainEnabled,
  updateRouteChain,
} from '../api'
import {
  chainErrorMessage,
  isConflictError,
  isHistoryDeleteError,
} from '../lib/chain-errors'
import { ROUTE_CHAINS_QUERY_KEY, routeChainDetailKey } from '../lib/query-keys'
import type { ChainSpecInput } from '../types'

// Mutations own their toasts; no optimistic writes (§三十六) — the UI only
// confirms a change after the server accepted the CAS version. A 409 always
// refreshes the server state and asks the operator to retry (§五).

function useInvalidateChains() {
  const queryClient = useQueryClient()
  return () => {
    void queryClient.invalidateQueries({ queryKey: ROUTE_CHAINS_QUERY_KEY })
  }
}

function useChainMutation() {
  const { t } = useTranslation()
  const invalidate = useInvalidateChains()
  const onError = (error: unknown, fallbackKey: string) => {
    if (isConflictError(error)) {
      toast.error(t('This route chain changed while you were working. Please review the latest state and retry.'))
      invalidate()
      return
    }
    if (isHistoryDeleteError(error)) {
      toast.error(t('This route chain has attempt history. Archive or disable it instead of deleting.'))
      return
    }
    toast.error(
      chainErrorMessage(error, t(fallbackKey))
    )
    if (!isConflictError(error)) invalidate()
  }
  return { t, invalidate, onError }
}

export function useSaveChain(onSuccess?: (chainId: number) => void) {
  const { t, invalidate, onError } = useChainMutation()
  const create = useMutation({
    mutationFn: (input: ChainSpecInput) => createRouteChain(input),
    onSuccess: (result) => {
      toast.success(t('Route chain created.'))
      invalidate()
      onSuccess?.(result.chain.id)
    },
    onError: (error) => onError(error, 'Failed to create route chain.'),
  })
  const update = useMutation({
    mutationFn: (input: ChainSpecInput & { id: number; expected_version: number }) => {
      const { id, expected_version, ...spec } = input
      return updateRouteChain(id, { ...spec, expected_version })
    },
    onSuccess: () => {
      toast.success(t('Route chain saved.'))
      invalidate()
      onSuccess?.(-1)
    },
    onError: (error) => onError(error, 'Failed to save route chain.'),
  })
  return { create, update }
}

export function useChainToggleEnabled() {
  const { t, invalidate, onError } = useChainMutation()
  return useMutation({
    mutationFn: (input: { id: number; enabled: boolean; expectedVersion: number }) =>
      setRouteChainEnabled(input.id, input.enabled, input.expectedVersion),
    onSuccess: (result, input) => {
      toast.success(
        input.enabled ? t('Route chain enabled.') : t('Route chain disabled.')
      )
      void result
      invalidate()
    },
    onError: (error) => onError(error, 'Failed to update route chain.'),
  })
}

export function useChainReorder() {
  const { t, invalidate, onError } = useChainMutation()
  return useMutation({
    mutationFn: (input: {
      id: number
      orderedCandidateIds: number[]
      expectedVersion: number
    }) =>
      reorderRouteChainCandidates(
        input.id,
        input.orderedCandidateIds,
        input.expectedVersion
      ),
    onSuccess: () => {
      toast.success(t('Candidate order saved.'))
      invalidate()
    },
    onError: (error) => onError(error, 'Failed to reorder candidates.'),
  })
}

export function useChainArchive() {
  const { t, invalidate, onError } = useChainMutation()
  return useMutation({
    mutationFn: (input: { id: number; expectedVersion: number }) =>
      archiveRouteChain(input.id, input.expectedVersion),
    onSuccess: () => {
      toast.success(t('Route chain archived.'))
      invalidate()
    },
    onError: (error) => onError(error, 'Failed to archive route chain.'),
  })
}

export function useChainDelete() {
  const { t, invalidate, onError } = useChainMutation()
  return useMutation({
    mutationFn: (input: { id: number; expectedVersion: number }) =>
      deleteRouteChain(input.id, input.expectedVersion),
    onSuccess: () => {
      toast.success(t('Route chain deleted.'))
      invalidate()
    },
    onError: (error) => onError(error, 'Failed to delete route chain.'),
  })
}

export function useCandidateToggle() {
  const { t, invalidate, onError } = useChainMutation()
  return useMutation({
    mutationFn: (input: {
      chainId: number
      candidateId: number
      enabled: boolean
      expectedVersion: number
    }) =>
      setRouteChainCandidateEnabled(
        input.chainId,
        input.candidateId,
        input.enabled,
        input.expectedVersion
      ),
    onSuccess: (result, input) => {
      toast.success(
        input.enabled
          ? t('Candidate enabled.')
          : t('Candidate disabled — new requests skip it; in-flight requests are not cancelled.')
      )
      void result
      invalidate()
    },
    onError: (error) => onError(error, 'Failed to update candidate.'),
  })
}

export function useCircuitReset() {
  const { t, invalidate, onError } = useChainMutation()
  return useMutation({
    mutationFn: (input: { chainId: number; candidateId: number }) =>
      resetRouteChainCandidateCircuit(input.chainId, input.candidateId),
    onSuccess: () => {
      toast.success(
        t('Circuit reset — runtime breaker back to probeable. This does not change persistent configuration.')
      )
      invalidate()
    },
    onError: (error) => onError(error, 'Failed to reset circuit.'),
  })
}

export function useCircuitForceOpen() {
  const { t, invalidate, onError } = useChainMutation()
  return useMutation({
    mutationFn: (input: {
      chainId: number
      candidateId: number
      durationSeconds: number
    }) =>
      forceOpenRouteChainCandidateCircuit(
        input.chainId,
        input.candidateId,
        input.durationSeconds
      ),
    onSuccess: () => {
      toast.success(
        t('Circuit force-opened. This is a temporary runtime state and disappears on service restart.')
      )
      invalidate()
    },
    onError: (error) => onError(error, 'Failed to force-open circuit.'),
  })
}

export { routeChainDetailKey }
