import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import {
  getImageSourcePolicies,
  saveImageSourcePolicy,
} from '../api'
import { ImageSourcePolicyPanel } from './image-source-policy'

vi.mock('../api', () => ({
  getImageSourcePolicies: vi.fn(),
  saveImageSourcePolicy: vi.fn(),
}))

function mount() {
  return render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <ImageSourcePolicyPanel />
    </QueryClientProvider>
  )
}

describe('image source policy panel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(getImageSourcePolicies).mockResolvedValue([])
  })

  it('saves the account-pool-first preset with the current revision', async () => {
    vi.mocked(saveImageSourcePolicy).mockResolvedValue({
      model: 'gpt-image-2',
      strategy: 'pool_first',
      group: '',
      enabled: true,
      version: 1,
      updated_at: 1,
    })
    mount()
    fireEvent.change(screen.getByLabelText('Image model'), {
      target: { value: 'gpt-image-2' },
    })
    const save = screen.getByRole('button', {
      name: 'Save image source policy',
    })
    await waitFor(() => expect(save).toBeEnabled())
    fireEvent.click(save)
    await waitFor(() =>
      expect(saveImageSourcePolicy).toHaveBeenCalledWith({
        model: 'gpt-image-2',
        strategy: 'pool_first',
        enabled: true,
        expected_version: 0,
      })
    )
  })

  it('explains that disabling a model returns it to the existing routing', async () => {
    mount()
    const toggle = screen.getByRole('switch')
    expect(
      screen.queryByText(/Disabled: image requests for this model/)
    ).not.toBeInTheDocument()
    fireEvent.click(toggle)
    expect(
      screen.getByText(/Disabled: image requests for this model/)
    ).toBeInTheDocument()
  })

  it('blocks saving a stale revision and offers to load the stored one', async () => {
    vi.mocked(getImageSourcePolicies).mockResolvedValue([
      {
        model: 'gpt-image-2',
        strategy: 'api_only',
        group: '',
        enabled: true,
        version: 7,
        updated_at: 1,
      },
    ])
    mount()
    await waitFor(() =>
      expect(screen.getByText(/gpt-image-2/)).toBeInTheDocument()
    )
    fireEvent.click(screen.getByRole('button', { name: 'Load' }))
    expect(screen.getByLabelText('Image model')).toHaveValue('gpt-image-2')
    expect(screen.getByLabelText('Source priority')).toHaveTextContent(
      'Upstream API only'
    )
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: 'Save image source policy' })
      ).toBeEnabled()
    )
  })
})
