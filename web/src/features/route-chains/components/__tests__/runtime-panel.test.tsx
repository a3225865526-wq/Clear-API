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
import { describe, expect, test, vi } from 'vitest'

import { RuntimePanel } from '../runtime-panel'
import { candidateView, gatesOff } from '../../__tests__/fixtures'

// F18/F19: reset and force-open require confirmation, state the memory-only
// restart semantics, and never imply persistent authorization changes.

const baseCandidate = candidateView({
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

function renderPanel(handlers?: { onReset?: () => void; onForceOpen?: () => void }) {
  return render(
    <RuntimePanel
      chainId={1}
      candidates={[baseCandidate]}
      gates={gatesOff}
      onReset={handlers?.onReset ?? (() => {})}
      onForceOpen={handlers?.onForceOpen ?? (() => {})}
      actionPending={false}
    />
  )
}

describe('runtime panel', () => {
  test('shows effective breaker config with Default/Override provenance', () => {
    render(
      <RuntimePanel
        chainId={1}
        candidates={[
          candidateView({
            effective_breaker: {
              ...candidateView().effective_breaker,
              failure_threshold: 5,
              failure_threshold_override: true,
            },
          }),
        ]}
        gates={gatesOff}
        onReset={() => {}}
        onForceOpen={() => {}}
        actionPending={false}
      />
    )
    expect(screen.getByText('Override: 5')).toBeInTheDocument()
    expect(screen.getByText('Default (300s)')).toBeInTheDocument()
    expect(screen.getByText('Default (300s)')).toBeInTheDocument()
  })

  test('states the memory-only restart semantics up front', () => {
    renderPanel()
    expect(
      screen.getByText(/memory-only per instance: a service restart clears it/)
    ).toBeInTheDocument()
  })

  test('F18: reset requires confirmation and does not call back before it', async () => {
    const user = userEvent.setup()
    const onReset = vi.fn()
    renderPanel({ onReset })
    await user.click(screen.getByRole('button', { name: 'Reset Circuit' }))
    expect(onReset).not.toHaveBeenCalled()
    expect(
      await screen.findByText(
        /does not enable the candidate, the channel, the pool master/
      )
    ).toBeInTheDocument()
    const confirmButtons = screen.getAllByRole('button', { name: 'Reset Circuit' })
    const confirmButton = confirmButtons.at(-1)
    if (confirmButton) await user.click(confirmButton)
    expect(onReset).toHaveBeenCalledWith(1)
  })

  test('F19: force-open confirms a temporary runtime state and passes the duration', async () => {
    const user = userEvent.setup()
    const onForceOpen = vi.fn()
    renderPanel({ onForceOpen })
    await user.click(screen.getByRole('button', { name: 'Force Open' }))
    expect(
      await screen.findByText(
        /temporary runtime state and disappears on service restart/
      )
    ).toBeInTheDocument()
    const confirmButtons = screen.getAllByRole('button', { name: 'Force Open' })
    const confirmButton = confirmButtons.at(-1)
    if (confirmButton) await user.click(confirmButton)
    expect(onForceOpen).toHaveBeenCalledWith(1, 300)
  })
})
