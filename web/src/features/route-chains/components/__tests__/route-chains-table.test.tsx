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
import { describe, expect, test, vi } from 'vitest'


import { RouteChainsTable } from '../route-chains-table'
import { RouteChainsProvider } from '../route-chains-provider'
import { chainListItem } from '../../__tests__/fixtures'

vi.mock('@tanstack/react-router', () => ({
  Link: (props: { children?: React.ReactNode; 'aria-label'?: string }) => (
    <a aria-label={props['aria-label']}>{props.children}</a>
  ),
}))

function renderTable(ui: React.ReactElement) {
  return render(<RouteChainsProvider>{ui}</RouteChainsProvider>)
}

// F1/F2: the list renders chains with the mandated columns and distinct
// overall-health states. F27: no secret material can appear — the row type
// has no such fields, asserted here with a poisoned fixture.

describe('F1/F2: route chains list', () => {
  test('renders chain rows with the mandated summary columns', () => {
    renderTable(<RouteChainsTable items={[chainListItem()]} />)
    expect(screen.getByText('gpt-chain')).toBeInTheDocument()
    expect(screen.getByText('default')).toBeInTheDocument()
    expect(screen.queryByText('Circuit Open')).not.toBeInTheDocument()
    expect(screen.getByText('v4')).toBeInTheDocument()
    expect(screen.getByText(/Primary:/)).toBeInTheDocument()
  })

  test('overall health states render as distinct text (F12-adjacent)', () => {
    renderTable(
      <RouteChainsTable
        items={[
          chainListItem({ id: 1, model: 'a', overall_health: 'HEALTHY' }),
          chainListItem({ id: 2, model: 'b', overall_health: 'CIRCUIT_OPEN' }),
          chainListItem({ id: 3, model: 'c', overall_health: 'CHAIN_DISABLED' }),
          chainListItem({ id: 4, model: 'd', overall_health: 'ARCHIVED' }),
        ]}
      />
    )
    expect(screen.getByText('Circuit Open')).toBeInTheDocument()
    expect(screen.getByText('Chain Disabled')).toBeInTheDocument()
    expect(screen.getAllByText('Archived').length).toBeGreaterThan(0)
  })

  test('archived rows disable edit and archive actions', () => {
    renderTable(<RouteChainsTable items={[chainListItem({ archived: true })]} />)
    expect(screen.getByRole('button', { name: 'Edit Route Chain' })).toBeDisabled()
    expect(
      screen.getByRole('button', { name: 'Archive route chain' })
    ).toBeDisabled()
  })
})

describe('F27: no secret material in rendered output', () => {
  test('channel credentials present in fixtures never leak into the DOM', () => {
    const poisoned = chainListItem({
      primary_candidate: {
        candidate_id: 1,
        type: 'api_channel',
        target_id: 21,
      },
    })
    // Poison the fixture beyond the row contract — the type cannot carry it.
    const payload = poisoned as unknown as Record<string, unknown>
    payload['key'] = 'sk-poisoned-secret'
    payload['base_url'] = 'https://upstream.example/secret-path'
    const { container } = renderTable(<RouteChainsTable items={[poisoned]} />)
    expect(container.textContent).not.toContain('sk-poisoned-secret')
    expect(container.textContent).not.toContain('secret-path')
  })
})
