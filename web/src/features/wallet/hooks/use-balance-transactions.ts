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
import { useCallback, useEffect, useState } from 'react'

import { handleServerError } from '@/lib/handle-server-error'

import { getBalanceTransactions } from '../api'
import type { BalanceTransactionDto } from '../types'

// ============================================================================
// Balance Transactions Hook (own ledger only; user id comes from session)
// ============================================================================

export function useBalanceTransactions() {
  const [records, setRecords] = useState<BalanceTransactionDto[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [type, setType] = useState('')
  const [loading, setLoading] = useState(false)

  const fetchRecords = useCallback(
    async (nextPage: number, nextPageSize: number, nextType: string) => {
      try {
        setLoading(true)
        const response = await getBalanceTransactions(
          nextPage,
          nextPageSize,
          nextType || undefined
        )
        if (response.success && response.data) {
          setRecords(response.data.items ?? [])
          setTotal(response.data.total ?? 0)
        } else {
          handleServerError(response)
        }
      } catch (error) {
        handleServerError(error)
      } finally {
        setLoading(false)
      }
    },
    []
  )

  useEffect(() => {
    void fetchRecords(page, pageSize, type)
  }, [fetchRecords, page, pageSize, type])

  const handlePageChange = useCallback((nextPage: number) => {
    setPage(nextPage)
  }, [])

  const handlePageSizeChange = useCallback((nextPageSize: number) => {
    setPage(1)
    setPageSize(nextPageSize)
  }, [])

  const handleTypeChange = useCallback((nextType: string) => {
    setPage(1)
    setType(nextType)
  }, [])

  return {
    records,
    total,
    page,
    pageSize,
    type,
    loading,
    handlePageChange,
    handlePageSizeChange,
    handleTypeChange,
    refetch: fetchRecords,
  }
}
