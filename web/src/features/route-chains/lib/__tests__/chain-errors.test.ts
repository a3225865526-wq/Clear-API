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
import { describe, expect, test } from 'vitest'

import {
  chainErrorMessage,
  chainIssueMessages,
  isConflictError,
  isHistoryDeleteError,
} from '../chain-errors'

function axiosError(status: number, data: unknown) {
  return {
    isAxiosError: true,
    response: { status, data },
  }
}

describe('F6: stale version 409 handling', () => {
  test('conflict errors are recognized from the backend code', () => {
    expect(
      isConflictError(
        axiosError(409, { code: 'ROUTE_CHAIN_VERSION_CONFLICT' })
      )
    ).toBe(true)
    expect(isConflictError(axiosError(400, { code: 'ROUTE_CHAIN_INVALID' }))).toBe(
      false
    )
  })

  test('history-guarded delete errors are recognized', () => {
    expect(
      isHistoryDeleteError(axiosError(409, { code: 'ROUTE_CHAIN_HAS_HISTORY' }))
    ).toBe(true)
  })
})

describe('F6/F37: issue codes map to operator copy, never raw errors', () => {
  test('backend issue codes render as localized lines', () => {
    const messages = chainIssueMessages(
      axiosError(400, {
        code: 'ROUTE_CHAIN_INVALID',
        errors: [
          { code: 'route_chain.model_required' },
          { code: 'route_chain.pricing_missing' },
          { code: 'route_chain.type_target_mismatch', target_id: 21 },
        ],
      })
    )
    expect(messages).toHaveLength(3)
    for (const message of messages) {
      expect(message).not.toMatch(/route_chain\./)
      expect(message).not.toMatch(/Error|exception|SQL/i)
    }
  })

  test('unknown codes contribute nothing and fall back to the server message', () => {
    const error = axiosError(400, {
      code: 'ROUTE_CHAIN_INVALID',
      errors: [{ code: 'route_chain.something_new' }],
    })
    expect(chainIssueMessages(error)).toHaveLength(0)
    const message = chainErrorMessage(error, 'fallback')
    expect(message.length).toBeGreaterThan(0)
  })

  test('non-axios errors fall back gracefully', () => {
    expect(chainErrorMessage(new Error('boom'), 'fallback')).toBeTruthy()
    expect(chainIssueMessages(undefined)).toHaveLength(0)
  })
})
