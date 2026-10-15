import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import {
  getPoolChannels,
  getPoolMaster,
  setChannelPoolRouting,
  setPoolMaster,
} from '../api'
import { ReleaseControls } from './release-controls'

vi.mock('../api', () => ({
  getPoolMaster: vi.fn(),
  setPoolMaster: vi.fn(),
  getPoolChannels: vi.fn(),
  setChannelPoolRouting: vi.fn(),
}))

function mount() {
  return render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <ReleaseControls />
    </QueryClientProvider>
  )
}

describe('pool release controls', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(getPoolMaster).mockResolvedValue({ enabled: false })
    vi.mocked(getPoolChannels).mockResolvedValue([
      {
        id: 11,
        name: 'synthetic-pool',
        status: 1,
        pool_routing_enabled: false,
        models: ['synthetic-model-a', 'synthetic-model-b'],
        published_models: ['synthetic-model-a'],
      },
    ])
  })

  it('requires an explicit confirmation before opening the pool', async () => {
    const user = userEvent.setup()
    mount()
    expect(screen.getByText('Pool is closed')).toBeInTheDocument()
    await user.click(screen.getByRole('switch', { name: 'Pool is closed' }))
    expect(
      await screen.findByText('Open the account pool?')
    ).toBeInTheDocument()
    expect(setPoolMaster).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(setPoolMaster).not.toHaveBeenCalled()
    await user.click(screen.getByRole('switch', { name: 'Pool is closed' }))
    await user.click(
      await screen.findByRole('button', { name: 'Enable pool' })
    )
    await waitFor(() => expect(setPoolMaster).toHaveBeenCalledWith(true))
  })

  it('closes the pool immediately without a confirmation dialog', async () => {
    vi.mocked(getPoolMaster).mockResolvedValue({ enabled: true })
    const user = userEvent.setup()
    mount()
    expect(await screen.findByText('Pool is open')).toBeInTheDocument()
    await user.click(screen.getByRole('switch', { name: 'Pool is open' }))
    await waitFor(() => expect(setPoolMaster).toHaveBeenCalledWith(false))
  })

  it('toggles channel pool routing and separates channel models from published models', async () => {
    const user = userEvent.setup()
    mount()
    expect(await screen.findByText('synthetic-pool (#11)')).toBeInTheDocument()
    expect(screen.getByText(/Channel models: synthetic-model-a, synthetic-model-b/)).toBeInTheDocument()
    expect(screen.getByText(/Published pool models: synthetic-model-a/)).toBeInTheDocument()
    expect(screen.queryByText(/Published pool models:.*synthetic-model-b/)).not.toBeInTheDocument()
    const item = screen.getByText('synthetic-pool (#11)').closest('li')
    expect(item).not.toBeNull()
    const channelSwitch = within(item as HTMLElement).getByRole('switch')
    await user.click(channelSwitch)
    await waitFor(() =>
      expect(setChannelPoolRouting).toHaveBeenCalledWith(11, true)
    )
  })
})
