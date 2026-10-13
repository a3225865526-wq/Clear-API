import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { creditProducts, type CreditProduct } from '../catalog'
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

function renderOperations(state: string, role = 100) {
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
    currency: 'CNY',
    revision: 2,
    base_quota: 500000,
    evidence_batch_id: 9,
    delivery_evidence: 'receipt/reference',
    delivery_binding: 'catfk/item/test',
    published: state === 'published',
    delivery_verified: ['approved', 'published'].includes(state),
    purchase_url: 'https://catfk.com/item/test',
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
