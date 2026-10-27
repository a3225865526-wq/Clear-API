import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { creditProducts, type CreditProduct } from '../catalog'
import { CatalogEditor } from '../components/catalog-editor'
import { ProductOperations } from '../components/product-operations'

vi.mock(
  '@/features/auth/secure-verification/hooks/use-secure-verification',
  () => ({
    useSecureVerification: () => ({
      requestVerification: vi
        .fn()
        .mockResolvedValue({ proof_token: 'test-operation-proof' }),
      dialogProps: { state: { phase: 'idle' } },
    }),
  })
)

vi.mock(
  '@/features/auth/secure-verification/components/secure-verification-dialog',
  () => ({ SecureVerificationDialog: () => null })
)

afterEach(() => {
  vi.restoreAllMocks()
  useAuthStore.getState().auth.reset()
})

function renderOperations(
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
  return product
}

it.each([1, 10])('role %s has no review actions', (role) => {
  renderOperations('pending', role)
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
})

it('archives with the current revision proof and keeps restoration separate from publishing', async () => {
  const product = renderOperations('published')
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
  const product = renderOperations('draft')
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

it('offers restore for an archived product while publishing remains disabled', () => {
  renderOperations('approved', 100, true)
  expect(screen.getByRole('button', { name: 'Restore product' })).toBeEnabled()
  expect(screen.getByRole('button', { name: 'Publish product' })).toBeDisabled()
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
  expect(screen.getByRole('button', { name: 'Restore product' })).toBeEnabled()
})

it('requires explicit confirmation before permanently deleting an unrelated draft', async () => {
  const product = renderOperations('draft', 100, false, {
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
  expect(screen.getByDisplayValue('credit-new-1')).toBeInTheDocument()
  await user.click(
    screen.getByRole('button', { name: 'Remove unsaved product' })
  )
  await user.click(screen.getByRole('button', { name: 'Continue' }))
  expect(screen.queryByDisplayValue('credit-new-1')).not.toBeInTheDocument()
  expect(post).not.toHaveBeenCalled()
})

it.each(['draft', 'pending', 'approved', 'published', 'revoked'])(
  'shows %s and only allows legal publish actions',
  (state) => {
    renderOperations(state)
    const publish = screen.getByRole('button', { name: 'Publish product' })
    if (state === 'approved') expect(publish).toBeEnabled()
    else expect(publish).toBeDisabled()
    expect(screen.getByRole('status')).toHaveTextContent('2')
  }
)

it('requires typing the exact revision confirmation before sending a solo approval', async () => {
  const product = renderOperations('pending')
  const get = vi.spyOn(api, 'get').mockResolvedValue({
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
  await user.click(
    screen.getByRole('button', { name: 'Review product revision' })
  )
  const confirm = await screen.findByRole('button', {
    name: 'Confirm product operation',
  })
  expect(confirm).toBeDisabled()
  expect(post).not.toHaveBeenCalled()
  await user.type(
    screen.getByPlaceholderText('Type the confirmation text here'),
    'APPROVE one@2:review-digest'
  )
  expect(confirm).toBeEnabled()
  await user.click(confirm)
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/clearapi/credit-catalog/products/one/approve',
      expect.objectContaining({
        version: 4,
        revision: 2,
        confirmation: 'APPROVE one@2:review-digest',
      }),
      { headers: { 'X-Security-Proof': 'test-operation-proof' } }
    )
  )
  expect(get).toHaveBeenCalledWith(
    '/api/clearapi/credit-catalog/products/one/review'
  )
})

it('revocation requires explicit confirmation and sends the current revision', async () => {
  const product = renderOperations('published')
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
