import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen } from '@testing-library/react'
import { expect, test, vi } from 'vitest'

const mockGet = vi.fn()
vi.mock('@/lib/api', () => ({
  api: { get: (...args: unknown[]) => mockGet(...args) },
}))

test.each(['true', 'false'])(
  'store settings show the persisted flag %s',
  async (flag) => {
    mockGet.mockImplementation(async (url: string) => ({
      data: {
        success: true,
        data:
          url === '/api/option/'
            ? [{ key: 'StoreRechargeEnabled', value: flag }]
            : [],
      },
    }))
    const { BillingSettings } = await import('../billing')
    const root = createRootRoute({ component: Outlet })
    const authenticated = createRoute({
      getParentRoute: () => root,
      id: '_authenticated',
      component: Outlet,
    })
    const page = createRoute({
      getParentRoute: () => authenticated,
      path: 'system-settings/billing/$section',
      component: BillingSettings,
    })
    const router = createRouter({
      routeTree: root.addChildren([authenticated.addChildren([page])]),
      history: createMemoryHistory({
        initialEntries: ['/system-settings/billing/store'],
      }),
    })
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    await router.load()
    render(
      <QueryClientProvider client={client}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    )
    expect(await screen.findByRole('switch')).toHaveAttribute(
      'aria-checked',
      flag
    )
    client.clear()
  }
)
