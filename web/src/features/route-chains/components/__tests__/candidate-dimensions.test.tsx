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
import { describe, expect, test } from 'vitest'

import { CandidateDiagnostics } from '../candidate-diagnostics'
import { candidateView } from '../../__tests__/fixtures'

// F12–F17: every unavailable dimension renders its OWN text — a circuit-open
// candidate is never labeled as disabled, a disabled channel is distinct,
// pool authority states have their own badges.

function renderCandidate(overrides = {}) {
  return render(<CandidateDiagnostics candidate={candidateView(overrides)} />)
}

describe('candidate status dimensions', () => {
  test('F14: a healthy enabled candidate shows enabled + channel enabled, not disabled', () => {
    renderCandidate()
    expect(screen.getByTestId('dim-config')).toHaveTextContent('Candidate Enabled')
    expect(screen.getByTestId('dim-channel')).toHaveTextContent('Channel Enabled')
    expect(screen.getByTestId('dim-runtime')).toHaveTextContent('Healthy')
  })

  test('F12: circuit open is distinct from candidate disabled', () => {
    renderCandidate({
      health_summary: 'CIRCUIT_OPEN',
      runtime: {
        ...candidateView().runtime,
        state: 'open',
        consecutive_failures: 3,
        opened_at: 1759800000,
        open_until: 1759800060,
        forced: true,
      },
    })
    expect(screen.getByTestId('dim-runtime')).toHaveTextContent('Circuit Open')
    expect(screen.getByTestId('dim-config')).toHaveTextContent('Candidate Enabled')
    expect(screen.queryByText('Candidate Disabled')).not.toBeInTheDocument()
  })

  test('F13: half-open renders its own probing label', () => {
    renderCandidate({ health_summary: 'HALF_OPEN' })
    expect(screen.getByTestId('dim-runtime')).toHaveTextContent('Half Open')
  })

  test('F14: persistent disable is distinct from runtime circuit state', () => {
    renderCandidate({ enabled: false, health_summary: 'DISABLED' })
    expect(screen.getByTestId('dim-config')).toHaveTextContent('Candidate Disabled')
    expect(screen.getByTestId('dim-runtime')).toHaveTextContent('Candidate Disabled')
  })

  test('F15: a disabled channel carries its own badge', () => {
    const channel = candidateView().channel
    renderCandidate({
      channel: {
        ...channel,
        enabled: false,
        status: 2,
      },
    })
    expect(screen.getByTestId('dim-channel')).toHaveTextContent('Channel Disabled')
    expect(screen.getByTestId('dim-config')).toHaveTextContent('Candidate Enabled')
  })

  test('F10: missing pricing renders PRICING MISSING, never a zero price', () => {
    renderCandidate({ pricing_configured: false })
    expect(screen.getByTestId('dim-pricing')).toHaveTextContent('PRICING MISSING')
  })

  test('pool candidates show master, routing and publication as separate states (F16/F17)', () => {
    renderCandidate({
      candidate_type: 'pool',
      pool_authority: {
        master_enabled: false,
        channel_routing: true,
        publication: 'unpublished',
      },
    })
    expect(screen.getByTestId('dim-pool-master')).toHaveTextContent(
      'Pool Master: OFF'
    )
    expect(screen.getByTestId('dim-pool-routing')).toHaveTextContent(
      'Channel Routing: ON'
    )
    expect(screen.getByTestId('dim-pool-publication')).toHaveTextContent(
      'Model Unpublished'
    )
  })

  test('archived candidates render the archived marker and skip eligibility', () => {
    renderCandidate({
      archived: true,
      enabled: false,
      health_summary: 'ARCHIVED',
      target_id: -9,
      archived_target_id: 21,
    })
    expect(screen.getByTestId('dim-config')).toHaveTextContent('Archived')
    expect(screen.queryByTestId('dim-pricing')).not.toBeInTheDocument()
    expect(screen.getByText(/Archived candidate/)).toBeInTheDocument()
  })
})
