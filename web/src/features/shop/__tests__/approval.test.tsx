import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { useSecureVerification } from '@/features/auth/secure-verification/hooks/use-secure-verification'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { creditProducts, type CreditProduct } from '../catalog'
import { CatalogEditor } from '../components/catalog-editor'
import { ProductOperations } from '../components/product-operations'

vi.mock(
  '@/features/auth/secure-verification/hooks/use-secure-verification',
  () => ({ useSecureVerification: vi.fn() })
)
beforeEach(() => {
  vi.mocked(useSecureVerification).mockReturnValue({
    requestVerification: vi
      .fn()
      .mockResolvedValue({ proof_token: 'test-operation-proof' }),
    requestLoginVerification: vi.fn(),
    cancel: vi.fn(),
    isActive: false,
    dialogProps: {
      state: { phase: 'idle' },
      passkeyDomains: null,
      onCancel: vi.fn(),
      onRetry: vi.fn(),
      onInputChange: vi.fn(),
      onVerify: vi.fn(),
    },
  })
})

afterEach(() => {
  vi.restoreAllMocks()
  useAuthStore.getState().auth.reset()
})

async function renderOperations(
  state: string,
  role = 100,
  archived = false,
  extra: Partial<CreditProduct> = {}
) {
  useAuthStore.setState((s) => ({
    auth: {
      ...s.auth,
      user: { id: 7, role, username: 'operator', display_name: 'operator' },
    },
  }))
  const product: CreditProduct = {
    ...creditProducts[0],
    id: 'one',
    state,
    archived,
    currency: 'CNY',
    revision: 2,
    base_quota: 500000,
    evidence_batch_id: 9,
    delivery_evidence: 'receipt/reference',
    delivery_binding: 'catfk/item/test',
    published: state === 'published',
    delivery_verified: ['approved', 'published'].includes(state),
    purchase_url: 'https://catfk.com/item/test',
    ...extra,
  }
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <ProductOperations product={product} version={4} disabled={false} />
    </QueryClientProvider>
  )
  const more = screen.queryByRole('button', { name: 'More operations' })
  if (more) await userEvent.click(more)
  return product
}

it.each([1, 10])('role %s has no review actions', async (role) => {
  await renderOperations('pending', role)
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
})

it('archives with the current revision proof and keeps restoration separate from publishing', async () => {
  const product = await renderOperations('published')
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        product,
        confirmation: 'one@2:review-digest',
        digest: 'review-digest',
        events: [],
        deletion: {
          batches: 2,
          codes: 10,
          used_codes: 1,
          orders: 1,
          balance_entries: 1,
          finance_events: 0,
          cash_receipts: 0,
          rewards: 0,
          events: 4,
          can_delete: false,
        },
      },
    },
  })
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true } })
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Archive product' }))
  const dialog = await screen.findByRole('alertdialog')
  expect(dialog).toHaveTextContent('one')
  expect(dialog).toHaveTextContent('Archiving stops public sales')
  await user.type(
    screen.getByPlaceholderText('Type the confirmation text here'),
    'ARCHIVE one@2:review-digest'
  )
  await user.click(
    screen.getByRole('button', { name: 'Confirm product operation' })
  )
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/clearapi/credit-catalog/products/one/archive',
      expect.objectContaining({
        version: 4,
        revision: 2,
        confirmation: 'ARCHIVE one@2:review-digest',
      }),
      { headers: { 'X-Security-Proof': 'test-operation-proof' } }
    )
  )
})

it('requires deletion inspection and blocks hard deletion when history exists', async () => {
  const product = await renderOperations('draft')
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        product,
        confirmation: 'one@2:review-digest',
        digest: 'review-digest',
        events: [],
        deletion: {
          batches: 1,
          codes: 1,
          used_codes: 1,
          orders: 1,
          balance_entries: 1,
          finance_events: 0,
          cash_receipts: 0,
          rewards: 0,
          events: 1,
          can_delete: false,
        },
      },
    },
  })
  const post = vi.spyOn(api, 'post')
  await userEvent.click(
    screen.getByRole('button', { name: 'Delete permanently' })
  )
  const dialog = await screen.findByRole('alertdialog')
  expect(
    within(dialog).getByRole('button', {
      name: 'Permanently delete this product',
    })
  ).toBeDisabled()
  expect(post).not.toHaveBeenCalled()
})

it('offers restore for an archived product without publishing it', async () => {
  await renderOperations('approved', 100, true)
  expect(screen.getByRole('button', { name: 'Restore product' })).toBeEnabled()
  expect(
    screen.queryByRole('button', { name: 'Publish product' })
  ).not.toBeInTheDocument()
})

it('hides archived products by default and reveals their disabled fields on request', async () => {
  useAuthStore.setState((s) => ({
    auth: {
      ...s.auth,
      user: {
        id: 7,
        role: 100,
        username: 'operator',
        display_name: 'operator',
      },
    },
  }))
  const product = {
    ...creditProducts[0],
    id: 'archived-item',
    name: 'Archived item',
    archived: true,
    state: 'draft',
    revision: 1,
    currency: 'CNY',
  }
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: { version: 4, products: [product] } },
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <CatalogEditor />
    </QueryClientProvider>
  )
  const toggle = await screen.findByRole('button', { name: 'Show archived' })
  expect(screen.queryByDisplayValue('archived-item')).not.toBeInTheDocument()
  await userEvent.click(toggle)
  expect(screen.getByDisplayValue('archived-item')).toBeDisabled()
  await userEvent.click(screen.getByRole('button', { name: 'More operations' }))
  expect(screen.getByRole('button', { name: 'Restore product' })).toBeEnabled()
})

it('requires explicit confirmation before permanently deleting an unrelated draft', async () => {
  const product = await renderOperations('draft', 100, false, {
    revision: 1,
    evidence_batch_id: 0,
    delivery_evidence: '',
    delivery_binding: '',
  })
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        product,
        confirmation: 'one@1:review-digest',
        digest: 'review-digest',
        events: [],
        deletion: {
          batches: 0,
          codes: 0,
          used_codes: 0,
          orders: 0,
          balance_entries: 0,
          finance_events: 0,
          cash_receipts: 0,
          rewards: 0,
          events: 0,
          can_delete: true,
        },
      },
    },
  })
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true } })
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Delete permanently' }))
  await user.click(
    await screen.findByRole('button', {
      name: 'Permanently delete this product',
    })
  )
  const confirm = screen.getByRole('button', {
    name: 'Confirm product operation',
  })
  expect(confirm).toBeDisabled()
  await user.type(
    screen.getByPlaceholderText('Type the confirmation text here'),
    'DELETE one@1:review-digest'
  )
  await user.click(confirm)
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/clearapi/credit-catalog/products/one/delete',
      expect.objectContaining({
        revision: 1,
        confirmation: 'DELETE one@1:review-digest',
      }),
      { headers: { 'X-Security-Proof': 'test-operation-proof' } }
    )
  )
})

it('discards an unsaved product locally after confirmation', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: { version: 4, products: [] } },
  })
  const post = vi.spyOn(api, 'post')
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <CatalogEditor />
    </QueryClientProvider>
  )
  const user = userEvent.setup()
  await user.click(await screen.findByRole('button', { name: 'Add product' }))
  const productID = (screen.getByLabelText('Product ID') as HTMLInputElement)
    .value
  expect(productID).toMatch(/^credit-/)
  await user.click(
    screen.getByRole('button', { name: 'Remove unsaved product' })
  )
  await user.click(screen.getByRole('button', { name: 'Continue' }))
  expect(screen.queryByLabelText('Product ID')).not.toBeInTheDocument()
  expect(post).not.toHaveBeenCalled()
})

it('shows revision history without offering a manual approval action', async () => {
  const product = await renderOperations('pending')
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        product,
        digest: 'digest',
        confirmation: 'one@2:digest',
        events: [],
      },
    },
  })
  const post = vi.spyOn(api, 'post')
  await userEvent.click(
    screen.getByRole('button', { name: 'Review product revision' })
  )
  expect(await screen.findByRole('button', { name: 'Close' })).toBeEnabled()
  expect(
    screen.queryByRole('button', { name: 'Submit for review' })
  ).not.toBeInTheDocument()
  expect(post).not.toHaveBeenCalled()
})

it.each([true, false])(
  'activates with one verification and preserves failures after refreshing (success=%s)',
  async (success) => {
    useAuthStore.setState((s) => ({
      auth: {
        ...s.auth,
        user: {
          id: 7,
          role: 100,
          username: 'operator',
          display_name: 'operator',
        },
      },
    }))
    const product = {
      ...creditProducts[0],
      id: 'automatic',
      name: 'Automatic credit',
      currency: 'CNY',
      state: 'pending',
      revision: 5,
      base_quota: 500000,
      evidence_batch_id: 4,
      purchase_url: 'https://catfk.com/item/qmcse2',
    }
    vi.spyOn(api, 'get').mockImplementation(async (url) => ({
      data: {
        success: true,
        data: String(url).endsWith('/review')
          ? {
              product,
              digest: 'bound-digest',
              confirmation: 'automatic@5:bound-digest',
              events: [],
            }
          : { version: 24, products: [product] },
      },
    }))
    const put = vi.spyOn(api, 'put')
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      data: {
        success,
        message: success ? undefined : 'Evidence unavailable',
      },
    })
    const client = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    render(
      <QueryClientProvider client={client}>
        <CatalogEditor />
      </QueryClientProvider>
    )
    await userEvent.click(
      await screen.findByRole('button', { name: 'Save and enable' })
    )
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith(
        '/api/clearapi/credit-catalog/products/automatic/activate',
        expect.objectContaining({
          version: 24,
          revision: 5,
          confirmation: 'ACTIVATE automatic@5:bound-digest',
          digest: 'bound-digest',
        }),
        { headers: { 'X-Security-Proof': 'test-operation-proof' } }
      )
    )
    if (!success) {
      expect(
        await screen.findByText('Evidence unavailable')
      ).toBeInTheDocument()
    }
    expect(put).not.toHaveBeenCalled()
    expect(
      screen.queryByRole('button', { name: 'Confirm product operation' })
    ).not.toBeInTheDocument()
  }
)

it('revocation requires explicit confirmation and sends the current revision', async () => {
  const product = await renderOperations('published')
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        product,
        confirmation: 'one@2:review-digest',
        digest: 'review-digest',
        events: [],
      },
    },
  })
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true } })
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Revoke approval' }))
  const confirm = await screen.findByRole('button', {
    name: 'Confirm product operation',
  })
  expect(confirm).toBeDisabled()
  await user.type(
    screen.getByPlaceholderText('Type the confirmation text here'),
    'REVOKE one@2:review-digest'
  )
  await user.click(confirm)
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/clearapi/credit-catalog/products/one/revoke',
      expect.objectContaining({ version: 4, revision: 2 }),
      { headers: { 'X-Security-Proof': 'test-operation-proof' } }
    )
  )
})

it('opens batch generation with the published product already selected and does not issue stock on navigation', async () => {
  useAuthStore.setState((s) => ({
    auth: {
      ...s.auth,
      user: {
        id: 7,
        role: 100,
        username: 'operator',
        display_name: 'operator',
      },
    },
  }))
  const product = {
    ...creditProducts[0],
    id: 'automatic',
    name: 'Automatic credit',
    currency: 'CNY',
    state: 'published',
    published: true,
    delivery_verified: true,
    revision: 5,
    base_quota: 500000,
    evidence_batch_id: 4,
    purchase_url: 'https://catfk.com/item/qmcse2',
  }
  vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: {
      success: true,
      data: String(url).includes('/batches/products')
        ? [{ ...product, stock_ready: true, sample_ready: false }]
        : { version: 24, products: [product] },
    },
  }))
  const post = vi.spyOn(api, 'post')
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <CatalogEditor />
    </QueryClientProvider>
  )
  await userEvent.click(
    await screen.findByRole('button', { name: 'Generate and download' })
  )
  const dialog = await screen.findByRole('dialog')
  await waitFor(() =>
    expect(
      within(dialog).getByRole('combobox', { name: 'Product ID' })
    ).toHaveValue('automatic')
  )
  expect(post).not.toHaveBeenCalled()
})

it.each(['click', 'enter'])(
  'verifies identity with %s without submitting the catalog form or changing its version',
  async (submission) => {
    const actual = await vi.importActual<
      typeof import('@/features/auth/secure-verification/hooks/use-secure-verification')
    >('@/features/auth/secure-verification/hooks/use-secure-verification')
    vi.mocked(useSecureVerification).mockImplementation(
      actual.useSecureVerification
    )
    useAuthStore.setState((s) => ({
      auth: {
        ...s.auth,
        user: {
          id: 7,
          role: 100,
          username: 'operator',
          display_name: 'operator',
        },
      },
    }))
    const product = {
      ...creditProducts[0],
      id: 'automatic',
      name: 'Automatic credit',
      currency: 'CNY',
      state: 'pending',
      revision: 5,
      base_quota: 500000,
      evidence_batch_id: 4,
      purchase_url: 'https://catfk.com/item/qmcse2',
    }
    vi.spyOn(api, 'get').mockImplementation(async (url) => {
      if (String(url) === '/api/verify/methods') {
        return {
          data: {
            success: true,
            data: {
              scope: 'store.product.operation',
              methods: [{ method: 'password', available: true }],
              oauth_providers: [],
              password_encryption_enabled: false,
            },
          },
        }
      }
      if (String(url).endsWith('/review')) {
        return {
          data: {
            success: true,
            data: {
              product,
              digest: 'bound-digest',
              confirmation: 'automatic@5:bound-digest',
              events: [],
            },
          },
        }
      }
      return {
        data: { success: true, data: { version: 29, products: [product] } },
      }
    })
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    const post = vi.spyOn(api, 'post').mockImplementation(async (url) => ({
      data: {
        success: true,
        data:
          String(url) === '/api/verify'
            ? {
                proof_token: 'test-real-dialog-proof',
                method: 'password',
                scope: 'store.product.operation',
                expires_at: Math.floor(Date.now() / 1000) + 300,
              }
            : undefined,
      },
    }))
    const client = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    render(
      <QueryClientProvider client={client}>
        <CatalogEditor />
      </QueryClientProvider>
    )
    const user = userEvent.setup()
    await user.click(
      await screen.findByRole('button', { name: 'Save and enable' })
    )
    const password = await screen.findByLabelText('Password', {
      selector: 'input',
    })
    await user.type(password, 'fixture-password')
    if (submission === 'click') {
      await user.click(screen.getByRole('button', { name: 'Verify' }))
    } else {
      await user.type(password, '{Enter}')
    }
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith(
        '/api/clearapi/credit-catalog/products/automatic/activate',
        expect.objectContaining({ version: 29, revision: 5 }),
        { headers: { 'X-Security-Proof': 'test-real-dialog-proof' } }
      )
    )
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: 'Save and enable' })
      ).toBeEnabled()
    )
    expect(put).not.toHaveBeenCalled()
    expect(
      post.mock.calls.filter(([url]) => url === '/api/verify')
    ).toHaveLength(1)
    expect(
      post.mock.calls.filter(([url]) => String(url).endsWith('/activate'))
    ).toHaveLength(1)
  }
)

it('still saves drafts when the catalog form itself is submitted', async () => {
  useAuthStore.setState((s) => ({
    auth: {
      ...s.auth,
      user: {
        id: 7,
        role: 100,
        username: 'operator',
        display_name: 'operator',
      },
    },
  }))
  const product = {
    ...creditProducts[0],
    id: 'draft-item',
    currency: 'CNY',
    state: 'draft',
    revision: 1,
    base_quota: 500000,
  }
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: { version: 29, products: [product] } },
  })
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  const post = vi.spyOn(api, 'post')
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <CatalogEditor />
    </QueryClientProvider>
  )
  await userEvent.click(
    await screen.findByRole('button', { name: 'Save drafts' })
  )
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith(
      '/api/clearapi/credit-catalog',
      expect.objectContaining({ version: 29 })
    )
  )
  expect(put).toHaveBeenCalledTimes(1)
  expect(post).not.toHaveBeenCalled()
})
