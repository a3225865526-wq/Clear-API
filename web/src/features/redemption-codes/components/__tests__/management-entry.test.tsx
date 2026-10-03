import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, test, vi } from 'vitest'

vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn(async () => ({
      data: {
        success: true,
        data: {
          total: 0,
          items: [],
        },
      },
    })),
  },
}))

test('the actual admin route retains offline orders and exposes store batches', async () => {
  const { Route } = await import('@/routes/_authenticated/redemption-codes')
  const Component = Route.options.component as () => React.ReactNode
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const user = userEvent.setup()
  render(
    <QueryClientProvider client={client}>
      <Component />
    </QueryClientProvider>
  )
  expect(
    screen.getByRole('button', { name: 'clearapi.offline.create' })
  ).toBeVisible()
  await user.click(screen.getByRole('tab', { name: 'Batches' }))
  expect(
    await screen.findByRole('button', { name: 'Create Batch' })
  ).toBeVisible()
  expect(
    screen.queryByRole('button', { name: 'clearapi.offline.create' })
  ).not.toBeInTheDocument()
  await user.click(screen.getByRole('tab', { name: 'clearapi.offline.title' }))
  expect(
    screen.getByRole('button', { name: 'clearapi.offline.create' })
  ).toBeVisible()
  client.clear()
})
