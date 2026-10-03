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
import i18next from 'i18next'
import { useCallback, useState } from 'react'

import { handleServerError } from '@/lib/handle-server-error'

import { redeemStoreCode } from '../api'
import type { StoreRedeemResult } from '../types'

// ============================================================================
// Store Code Redemption Hook (dedicated /api/clearapi/store/redeem endpoint)
// ============================================================================

export function useStoreRedeem() {
  const [redeeming, setRedeeming] = useState(false)
  const [result, setResult] = useState<StoreRedeemResult | null>(null)

  const redeem = useCallback(
    async (
      code: string,
      options?: { onSuccess?: (result: StoreRedeemResult) => void }
    ): Promise<boolean> => {
      const trimmed = code.trim()
      if (!trimmed) {
        handleServerError(
          { success: false, message: i18next.t('Please enter a redemption code') },
          i18next.t('Redemption failed')
        )
        return false
      }

      try {
        setRedeeming(true)
        const response = await redeemStoreCode({ key: trimmed })

        if (response.success && response.data) {
          setResult(response.data)
          options?.onSuccess?.(response.data)
          return true
        }

        // Show the backend's unified, deliberately vague message. Never
        // guess the code state from status/timing on the frontend, and
        // never retry against the offline-order endpoint.
        handleServerError(response, i18next.t('Redemption failed'))
        return false
      } catch (_error) {
        handleServerError(_error, i18next.t('Redemption failed'))
        return false
      } finally {
        setRedeeming(false)
      }
    },
    []
  )

  const clearResult = useCallback(() => setResult(null), [])

  return {
    redeeming,
    result,
    redeem,
    clearResult,
  }
}
