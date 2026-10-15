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
import { useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'

import { getRouteChainDetail } from '../api'
import { useChainArchive, useChainDelete } from '../hooks/use-chain-mutations'
import { ROUTE_CHAINS_QUERY_KEY } from '../lib/query-keys'
import type { ChainDetail } from '../types'
import { ChainEditorDrawer } from './chain-editor-drawer'
import { useRouteChainsDialogs } from './route-chains-provider'

// Dialog host: every dialog mounted once, opened via the provider (channels
// pattern). Delete is offered as-is — the backend refuses history-bearing
// chains with a dedicated message, which the error handler surfaces (§十七).

export function RouteChainsDialogs() {
  const { open, setOpen, currentRow } = useRouteChainsDialogs()
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const archive = useChainArchive()
  const remove = useChainDelete()

  // Editing needs the full detail (candidates + CAS version), not the list row.
  const [editDetail, setEditDetail] = useState<ChainDetail | null>(null)
  useEffect(() => {
    if (open !== 'edit-chain' || !currentRow) return
    let cancelled = false
    getRouteChainDetail(currentRow.id)
      .then((chain) => {
        if (!cancelled) setEditDetail(chain)
      })
      .catch(() => {
        setOpen(null)
        void queryClient.invalidateQueries({ queryKey: ROUTE_CHAINS_QUERY_KEY })
      })
    return () => {
      cancelled = true
    }
  }, [open, currentRow, setOpen, queryClient])

  if (open === 'create-chain') {
    return (
      <ChainEditorDrawer
        open
        onOpenChange={(next) => !next && setOpen(null)}
        chain={null}
        maxCandidates={10}
      />
    )
  }
  if (open === 'edit-chain') {
    return (
      <ChainEditorDrawer
        open
        onOpenChange={(next) => {
          if (!next) {
            setOpen(null)
            setEditDetail(null)
          }
        }}
        chain={editDetail}
        maxCandidates={10}
      />
    )
  }
  if (open === 'archive-chain' && currentRow) {
    return (
      <ConfirmDialog
        open
        onOpenChange={(next) => !next && setOpen(null)}
        title={t('Archive route chain')}
        desc={t(
          'The chain is disabled and kept for audit history. Archived chains are read-only.'
        )}
        confirmText={t('Archive')}
        isLoading={archive.isPending}
        handleConfirm={() =>
          archive.mutate(
            { id: currentRow.id, expectedVersion: currentRow.version },
            { onSuccess: () => setOpen(null) }
          )
        }
      />
    )
  }
  if (open === 'delete-chain' && currentRow) {
    return (
      <ConfirmDialog
        open
        onOpenChange={(next) => !next && setOpen(null)}
        title={t('Delete route chain')}
        desc={t(
          'Permanently delete this chain and its candidates. Chains referenced by the attempt history cannot be deleted.'
        )}
        confirmText={t('Delete')}
        destructive
        isLoading={remove.isPending}
        handleConfirm={() =>
          remove.mutate(
            { id: currentRow.id, expectedVersion: currentRow.version },
            {
              onSuccess: () => {
                setOpen(null)
                void navigate({ to: '/route-chains' })
              },
            }
          )
        }
      />
    )
  }
  return null
}
