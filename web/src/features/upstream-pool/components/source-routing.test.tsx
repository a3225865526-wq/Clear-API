import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { getPoolModelRoutes, savePoolModelRoute } from '../api'
import { SourceRouting } from './source-routing'

vi.mock('../api', () => ({
  getPoolModelRoutes: vi.fn(),
  savePoolModelRoute: vi.fn(),
}))

function mount() {
  return render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <SourceRouting />
    </QueryClientProvider>
  )
}

describe('model source control', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(getPoolModelRoutes).mockResolvedValue([])
  })

  it('saves an exact API model and preserves existing routing for unconfigured models', async () => {
    mount()
    fireEvent.change(screen.getByLabelText('External model name'), {
      target: { value: 'synthetic-model' },
    })
    const save = screen.getByRole('button', { name: 'Save model source' })
    await waitFor(() => expect(save).toBeEnabled())
    fireEvent.click(save)
    await waitFor(() =>
      expect(savePoolModelRoute).toHaveBeenCalledWith({
        model: 'synthetic-model',
        platform: 'codex',
        access_type: 'oauth_browser',
        source: 'api',
        enabled: true,
        expected_version: 0,
      })
    )
  })

  it('forces pool selection paused and explains that API fallback is disabled', async () => {
    const user = userEvent.setup()
    mount()
    await waitFor(() => expect(getPoolModelRoutes).toHaveBeenCalled())
    fireEvent.change(screen.getByLabelText('External model name'), {
      target: { value: 'synthetic-model' },
    })
    await user.click(screen.getByLabelText('Request source'))
    await user.click(
      await screen.findByRole('option', { name: 'Account pool' })
    )
    expect(screen.getByRole('switch')).toHaveAttribute('aria-disabled', 'true')
    expect(screen.getByRole('switch')).not.toBeChecked()
    expect(
      screen.getByText(
        'Pool admission is incomplete. Saving this source pauses the model and never falls back to API.'
      )
    ).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Save model source' }))
    await waitFor(() =>
      expect(savePoolModelRoute).toHaveBeenCalledWith(
        expect.objectContaining({ source: 'pool', enabled: false })
      )
    )
  })

  it('requires explicitly loading an existing revision before saving', async () => {
    vi.mocked(getPoolModelRoutes).mockResolvedValue([
      {
        model: 'synthetic-model',
        platform: 'claude',
        access_type: 'oauth_browser',
        source: 'api',
        enabled: false,
        version: 7,
        updated_at: 1,
      },
    ])
    mount()
    await screen.findByRole('button', { name: 'Edit' })
    fireEvent.change(screen.getByLabelText('External model name'), {
      target: { value: 'synthetic-model' },
    })
    expect(
      screen.getByRole('button', { name: 'Save model source' })
    ).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }))
    fireEvent.click(screen.getByRole('button', { name: 'Save model source' }))
    await waitFor(() =>
      expect(savePoolModelRoute).toHaveBeenCalledWith(
        expect.objectContaining({
          expected_version: 7,
          platform: 'claude',
          enabled: false,
        })
      )
    )
  })

  it('does not save after a query failure', async () => {
    vi.mocked(getPoolModelRoutes).mockRejectedValueOnce(
      new Error('fixed query failure')
    )
    mount()
    fireEvent.change(screen.getByLabelText('External model name'), {
      target: { value: 'synthetic-model' },
    })
    await screen.findByRole('alert')
    expect(
      screen.getByRole('button', { name: 'Save model source' })
    ).toBeDisabled()
    expect(savePoolModelRoute).not.toHaveBeenCalled()
  })
})
