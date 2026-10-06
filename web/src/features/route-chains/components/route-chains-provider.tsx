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
/* eslint-disable react-refresh/only-export-components */
import { createContext, useContext, useMemo, useState } from 'react'

import type { ChainListItem } from '../types'

// Dialog orchestration follows the channels feature pattern: one provider
// owns which dialog is open and for which row.

export type RouteChainDialogType =
  | 'create-chain'
  | 'edit-chain'
  | 'archive-chain'
  | 'delete-chain'
  | null

interface RouteChainsContextValue {
  open: RouteChainDialogType
  currentRow: ChainListItem | null
  setOpen: (open: RouteChainDialogType) => void
  openFor: (open: Exclude<RouteChainDialogType, null>, row: ChainListItem | null) => void
}

const RouteChainsContext = createContext<RouteChainsContextValue | null>(null)

export function RouteChainsProvider(props: { children: React.ReactNode }) {
  const [open, setOpen] = useState<RouteChainDialogType>(null)
  const [currentRow, setCurrentRow] = useState<ChainListItem | null>(null)

  const value = useMemo<RouteChainsContextValue>(
    () => ({
      open,
      currentRow,
      setOpen,
      openFor: (nextOpen, row) => {
        setCurrentRow(row)
        setOpen(nextOpen)
      },
    }),
    [open, currentRow]
  )
  return (
    <RouteChainsContext.Provider value={value}>
      {props.children}
    </RouteChainsContext.Provider>
  )
}

export function useRouteChainsDialogs(): RouteChainsContextValue {
  const context = useContext(RouteChainsContext)
  if (!context) {
    throw new Error('useRouteChainsDialogs must be used within RouteChainsProvider')
  }
  return context
}
