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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test } from 'vitest'

import { AttemptsPanel, DecisionsPanel } from '../activity-panels'
import { attemptView, decisionView } from '../../__tests__/fixtures'

// F20/F21: UNKNOWN_REVIEW and unavailable usage are never displayed as zero.
// F22/F23: attempts and decisions tables render the safe columns.

describe('F20/F21: uncertain cost and usage render review markers, not zeros', () => {
  test('UNKNOWN_REVIEW shows the review marker with no ￥0 / 0 cost', () => {
    render(
      <AttemptsPanel
        attempts={[
          attemptView({
            provider_cost_state: 'UNKNOWN_REVIEW',
            usage_state: 'unavailable',
            user_billing_state: 'settled',
          }),
        ]}
      />
    )
    expect(screen.getByText('Needs Review')).toBeInTheDocument()
    expect(screen.getByText('Usage Unavailable')).toBeInTheDocument()
    expect(screen.getByText('Settled')).toBeInTheDocument()
    const body = document.body.textContent ?? ''
    expect(body).not.toContain('￥0')
  })

  test('confirmed zero cost stays distinct from unknown', () => {
    render(
      <AttemptsPanel attempts={[attemptView({ provider_cost_state: 'ZERO_CONFIRMED' })]}
      />
    )
    expect(screen.getByText('Zero (confirmed)')).toBeInTheDocument()
  })

  test('user billing settled and provider cost unknown appear in separate columns', () => {
    render(
      <AttemptsPanel
        attempts={[attemptView({ provider_cost_state: 'UNKNOWN_REVIEW' })]}
      />
    )
    expect(screen.getByText('User Billing')).toBeInTheDocument()
    expect(screen.getByText('Provider Cost')).toBeInTheDocument()
  })
})

describe('F22: attempts table', () => {
  test('renders terminal state, http status and failure facts', () => {
    render(
      <AttemptsPanel
        attempts={[
          attemptView(),
          attemptView({
            attempt_id: 'att-2',
            terminal_state: 'OUTCOME_UNCERTAIN',
            http_status: 0,
            failure_class: 'transport_failure',
          }),
        ]}
      />
    )
    expect(screen.getByText('SUCCEEDED')).toBeInTheDocument()
    expect(screen.getByText('OUTCOME_UNCERTAIN')).toBeInTheDocument()
    expect(screen.getByText('transport_failure')).toBeInTheDocument()
  })

  test('the details dialog exposes safe ids only', async () => {
    const user = userEvent.setup()
    render(
      <AttemptsPanel
        attempts={[
          attemptView({
            attempt_id: 'att-safe-1',
            request_id: 'req-safe-1',
            upstream_response_id: 'resp-safe-9',
          }),
        ]}
      />
    )
    await user.click(screen.getByRole('button', { name: 'Attempt details' }))
    expect(screen.getByTestId('attempt-details')).toHaveTextContent('att-safe-1')
    expect(screen.getByTestId('attempt-details')).toHaveTextContent('req-safe-1')
    expect(screen.getByTestId('attempt-details')).toHaveTextContent('resp-safe-9')
  })
})

describe('F23: decisions table', () => {
  test('renders safe reason codes for skips and failovers', () => {
    render(
      <DecisionsPanel
        decisions={[
          decisionView(),
          decisionView({ id: 2, reason: 'pricing_missing', decision: 'skipped' }),
          decisionView({ id: 3, reason: 'failover_advanced', decision: 'visited' }),
        ]}
      />
    )
    expect(screen.getByText('circuit_open')).toBeInTheDocument()
    expect(screen.getByText('pricing_missing')).toBeInTheDocument()
    expect(screen.getByText('failover_advanced')).toBeInTheDocument()
  })
})
