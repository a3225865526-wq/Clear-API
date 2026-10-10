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
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { SignUpForm } from '../components/sign-up-form'

let client: QueryClient | undefined
function renderRegistration(emailVerification: boolean, entry = '/sign-up') {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  client.setQueryData(['status'], {
    email_verification: emailVerification,
    turnstile_check: false,
    oauth_register_enabled: false,
  })
  vi.spyOn(api, 'get').mockResolvedValue({ data: { success: true } })
  const root = createRootRoute({ component: Outlet })
  const register = createRoute({
    getParentRoute: () => root,
    path: '/sign-up',
    component: SignUpForm,
  })
  const login = createRoute({
    getParentRoute: () => root,
    path: '/sign-in',
    component: () => <p>Sign-in destination</p>,
  })
  const router = createRouter({
    routeTree: root.addChildren([register, login]),
    history: createMemoryHistory({ initialEntries: [entry] }),
  })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

afterEach(() => {
  client?.clear()
  localStorage.clear()
  vi.restoreAllMocks()
})

it('registers with only email, password and code when verification is enabled', async () => {
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true } })
  renderRegistration(true)
  const email = await screen.findByLabelText(
    'Email (required for verification)'
  )
  expect(screen.queryByLabelText('Username')).not.toBeInTheDocument()
  expect(screen.queryByLabelText('Confirm password')).not.toBeInTheDocument()
  await userEvent.type(email, 'new@example.com')
  await userEvent.type(
    screen.getByLabelText('Password'),
    'registration-password'
  )
  await userEvent.type(screen.getByLabelText('Verification code'), '123456')
  await userEvent.click(screen.getByRole('button', { name: 'Create account' }))
  expect(await screen.findByText('Sign-in destination')).toBeVisible()
  expect(post).toHaveBeenCalledWith(
    '/api/user/register',
    expect.objectContaining({
      username: '',
      email: 'new@example.com',
      password: 'registration-password',
      verification_code: '123456',
    }),
    expect.any(Object)
  )
})

it('keeps the invitation parameter while requiring the email verification code', async () => {
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true } })
  renderRegistration(true, '/sign-up?aff=stable-referral-code')
  await userEvent.type(
    await screen.findByLabelText('Email (required for verification)'),
    'referral-new@example.com'
  )
  await userEvent.type(
    screen.getByLabelText('Password'),
    'registration-password'
  )
  await userEvent.click(screen.getByRole('button', { name: 'Create account' }))
  expect(post).not.toHaveBeenCalled()
  await userEvent.type(screen.getByLabelText('Verification code'), '123456')
  await userEvent.click(screen.getByRole('button', { name: 'Create account' }))
  expect(await screen.findByText('Sign-in destination')).toBeVisible()
  expect(post).toHaveBeenCalledWith(
    '/api/user/register',
    expect.objectContaining({
      aff_code: 'stable-referral-code',
      verification_code: '123456',
    }),
    expect.any(Object)
  )
})

it('sends the email challenge and disables resending during cooldown', async () => {
  renderRegistration(true)
  await userEvent.type(
    await screen.findByLabelText('Email (required for verification)'),
    'new@example.com'
  )
  await userEvent.click(screen.getByRole('button', { name: 'Send code' }))
  expect(await screen.findByRole('button', { name: /Resend/ })).toBeDisabled()
  expect(api.get).toHaveBeenCalledWith(
    '/api/verification',
    expect.objectContaining({
      params: expect.objectContaining({ email: 'new@example.com' }),
    })
  )
})

it('blocks registration without a code and clears proof when the email changes', async () => {
  const post = vi.spyOn(api, 'post')
  renderRegistration(true)
  const email = await screen.findByLabelText(
    'Email (required for verification)'
  )
  await userEvent.type(email, 'new@example.com')
  await userEvent.type(
    screen.getByLabelText('Password'),
    'registration-password'
  )
  await userEvent.type(screen.getByLabelText('Verification code'), '123456')
  await userEvent.clear(email)
  await userEvent.type(email, 'other@example.com')
  expect(screen.getByLabelText('Verification code')).toHaveValue('')
  await userEvent.click(screen.getByRole('button', { name: 'Create account' }))
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Create account' })).toBeEnabled()
  )
  expect(post).not.toHaveBeenCalled()
})

it('retains username registration and confirmation while the rollout flag is disabled', async () => {
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true } })
  renderRegistration(false)
  await userEvent.type(await screen.findByLabelText('Username'), 'legacy-name')
  expect(screen.queryByLabelText('Verification code')).not.toBeInTheDocument()
  await userEvent.type(
    screen.getByLabelText('Password'),
    'registration-password'
  )
  await userEvent.type(
    screen.getByLabelText('Confirm password'),
    'registration-password'
  )
  await userEvent.click(screen.getByRole('button', { name: 'Create account' }))
  expect(await screen.findByText('Sign-in destination')).toBeVisible()
  expect(post).toHaveBeenCalledWith(
    '/api/user/register',
    expect.objectContaining({ username: 'legacy-name' }),
    expect.any(Object)
  )
})
