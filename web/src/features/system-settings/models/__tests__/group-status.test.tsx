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
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { GroupStatusPanel } from '../group-status-panel'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), put: vi.fn() } }))

function renderPanel() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <GroupStatusPanel />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(api.get).mockResolvedValue({
    data: {
      success: true,
      data: [
        { name: 'default', enabled: true },
        { name: 'vip', enabled: false },
      ],
    },
  })
})

describe('Group availability', () => {
  it('shows persisted status and filters disabled groups and names', async () => {
    renderPanel()
    expect(await screen.findByRole('switch', { name: 'default' })).toBeChecked()
    expect(screen.getByRole('switch', { name: 'vip' })).not.toBeChecked()
    fireEvent.click(screen.getByRole('button', { name: 'Disabled only' }))
    expect(
      screen.queryByRole('switch', { name: 'default' })
    ).not.toBeInTheDocument()
    fireEvent.change(screen.getByRole('textbox', { name: 'Search groups' }), {
      target: { value: 'missing' },
    })
    expect(
      screen.queryByRole('switch', { name: 'vip' })
    ).not.toBeInTheDocument()
  })

  it('requires confirmation to disable and retains the persisted state on failure', async () => {
    vi.mocked(api.put).mockRejectedValue(new Error('save failed'))
    renderPanel()
    fireEvent.click(await screen.findByRole('switch', { name: 'default' }))
    expect(api.put).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    await waitFor(() =>
      expect(api.put).toHaveBeenCalledWith('/api/group/status', {
        name: 'default',
        enabled: false,
      })
    )
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Continue' })).toBeEnabled()
    )
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(await screen.findByRole('switch', { name: 'default' })).toBeChecked()
  })

  it('reenables without confirmation and refreshes the persisted status', async () => {
    vi.mocked(api.put).mockResolvedValue({ data: { success: true } })
    renderPanel()
    const control = await screen.findByRole('switch', { name: 'vip' })
    vi.mocked(api.get).mockResolvedValue({
      data: { success: true, data: [{ name: 'vip', enabled: true }] },
    })
    fireEvent.click(control)
    await waitFor(() =>
      expect(api.put).toHaveBeenCalledWith('/api/group/status', {
        name: 'vip',
        enabled: true,
      })
    )
    await waitFor(() =>
      expect(screen.getByRole('switch', { name: 'vip' })).toBeChecked()
    )
  })
})
