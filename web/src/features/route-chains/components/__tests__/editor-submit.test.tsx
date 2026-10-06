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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test, vi } from 'vitest'

import { updateRouteChain } from '../../api'
import { chainDetail } from '../../__tests__/fixtures'

// F25: while a save is in flight the submit button is disabled, so a double
// click cannot send two CAS updates. The backend CAS remains the final guard.

const pendingForever = () => new Promise(() => {})

vi.mock('../../api', () => ({
  getRouteChainTargets: vi.fn(async () => []),
  updateRouteChain: vi.fn(() => pendingForever()),
  createRouteChain: vi.fn(() => pendingForever()),
}))

async function renderEditor() {
  const { ChainEditorDrawer } = await import('../chain-editor-drawer')
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <ChainEditorDrawer
        open
        onOpenChange={() => {}}
        chain={chainDetail()}
        maxCandidates={10}
      />
    </QueryClientProvider>
  )
}

describe('F25: double submit protection', () => {
  test('the save button disables while the update is in flight', async () => {
    const user = userEvent.setup()
    await renderEditor()
    const save = await screen.findByRole('button', { name: 'Save changes' })

    await user.click(save)
    expect(updateRouteChainMock).toHaveBeenCalledTimes(1)
    await waitFor(() => expect(save).toBeDisabled())

    if (!save.hasAttribute('disabled')) {
      await user.click(save)
    }
    expect(updateRouteChainMock).toHaveBeenCalledTimes(1)
  })

  test('the drawer renders the chain identity locked for edits', async () => {
    await renderEditor()
    const modelInput = await screen.findByDisplayValue('gpt-chain')
    expect(modelInput).toBeDisabled()
  })
})

const updateRouteChainMock = vi.mocked(updateRouteChain)
