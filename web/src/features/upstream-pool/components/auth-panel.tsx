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
import { useMutation } from '@tanstack/react-query'
import { ExternalLink } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { CopyButton } from '@/components/copy-button'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { handleServerError } from '@/lib/handle-server-error'
import { getServerErrorStatus } from '@/lib/server-error-message'

import {
  cancelPoolAccountAuth,
  completePoolAccountAuth,
  startPoolAccountAuth,
  startPoolAccountReauth,
} from '../api'
import type { PoolAccount, PoolAuthStart, PoolTask } from '../types'

function useCountdownSeconds(expiresAt: number | null) {
  const [now, setNow] = useState(() => Math.floor(Date.now() / 1000))
  useEffect(() => {
    if (expiresAt == null) return
    const timer = setInterval(() => setNow(Math.floor(Date.now() / 1000)), 1000)
    return () => clearInterval(timer)
  }, [expiresAt])
  if (expiresAt == null) return null
  return Math.max(0, expiresAt - now)
}

function formatCountdown(totalSeconds: number) {
  const minutes = Math.floor(totalSeconds / 60)
  const seconds = totalSeconds % 60
  return `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`
}

function OneTimeField(props: {
  id: string
  label: string
  value: string
  openable?: boolean
}) {
  const { t } = useTranslation()
  return (
    <div className='space-y-1.5'>
      <Label htmlFor={props.id}>{props.label}</Label>
      <div className='flex flex-wrap items-center gap-2'>
        <code
          id={props.id}
          className='bg-muted min-w-0 flex-1 rounded-md px-2 py-1.5 font-mono text-xs break-all'
        >
          {props.value}
        </code>
        {props.openable ? (
          <Button
            variant='outline'
            size='sm'
            render={<a href={props.value} target='_blank' rel='noreferrer' />}
          >
            <ExternalLink />
            {t('Open in new tab')}
          </Button>
        ) : null}
        <CopyButton
          value={props.value}
          variant='outline'
          size='sm'
          aria-label={t('Copy to clipboard')}
        />
      </div>
    </div>
  )
}

/**
 * One-time authorization view. The URL / user code come from the start
 * response held in page state only: never stored, never refetchable.
 */
function OneTimeAuthView(props: {
  authStart: PoolAuthStart
  remaining: number | null
  callbackInput: string
  completing: boolean
  cancelling: boolean
  onCallbackChange: (value: string) => void
  onComplete: () => void
  onCancel: () => void
}) {
  const { t } = useTranslation()
  const start = props.authStart
  const isDevice = start.method === 'oauth_device'
  return (
    <div className='space-y-3'>
      <p className='text-warning text-sm font-medium'>
        {t(
          'One-time display. If you lose it, cancel this task and start again.'
        )}
        {props.remaining != null
          ? ` ${t('Expires in {{time}}', {
              time: formatCountdown(props.remaining),
            })}`
          : null}
      </p>
      {!isDevice && start.auth_url ? (
        <OneTimeField
          id='pool-auth-url'
          label={t('Auth URL')}
          value={start.auth_url}
          openable
        />
      ) : null}
      {isDevice && start.verification_uri ? (
        <OneTimeField
          id='pool-verification-uri'
          label={t('Verification URL')}
          value={start.verification_uri}
          openable
        />
      ) : null}
      {isDevice && start.user_code ? (
        <OneTimeField
          id='pool-user-code'
          label={t('User code')}
          value={start.user_code}
        />
      ) : null}
      <div className='space-y-1.5'>
        <Label htmlFor='pool-callback-input'>
          {t('Callback or authorization code')}
        </Label>
        <Textarea
          id='pool-callback-input'
          value={props.callbackInput}
          rows={3}
          onChange={(event) => props.onCallbackChange(event.target.value)}
        />
        <p className='text-muted-foreground text-xs'>
          {t(
            'Supports pasting the full callback URL or the authorization code.'
          )}
        </p>
      </div>
      <div className='flex flex-wrap gap-2'>
        <Button
          size='sm'
          disabled={!props.callbackInput.trim() || props.completing}
          onClick={props.onComplete}
        >
          {t('Complete authorization')}
        </Button>
        <Button
          variant='outline'
          size='sm'
          disabled={props.cancelling}
          onClick={props.onCancel}
        >
          {t('Cancel authorization')}
        </Button>
      </div>
    </div>
  )
}

type AuthPanelProps = {
  account: PoolAccount
  pendingTask: PoolTask | undefined
  authStart: PoolAuthStart | null
  onStarted: (start: PoolAuthStart) => void
  onSettled: () => void
}

/**
 * Authorization panel. The one-time auth URL / user code only live in this
 * page's state: they are never stored and can never be fetched again.
 */
export function AuthPanel(props: AuthPanelProps) {
  const { t } = useTranslation()
  const [callbackInput, setCallbackInput] = useState('')
  const authStart = props.authStart
  const pendingTask = props.pendingTask
  const remaining = useCountdownSeconds(authStart ? authStart.expires_at : null)

  const startMutation = useMutation({
    mutationFn: () => startPoolAccountAuth(props.account.id),
    onSuccess: (start) => {
      setCallbackInput('')
      props.onStarted(start)
    },
    onError: (error) => handleServerError(error),
  })
  const reauthMutation = useMutation({
    mutationFn: () => startPoolAccountReauth(props.account.id),
    onSuccess: (start) => {
      setCallbackInput('')
      props.onStarted(start)
    },
    onError: (error) => handleServerError(error),
  })
  const completeMutation = useMutation({
    mutationFn: (input: {
      callback_url?: string
      code?: string
      state?: string
    }) => completePoolAccountAuth(props.account.id, input),
    onSuccess: () => {
      toast.success(t('Authorization completed'))
      setCallbackInput('')
      props.onSettled()
    },
    onError: (error) => {
      handleServerError(error)
      const status = getServerErrorStatus(error)
      if (status === 409 || status === 410) {
        setCallbackInput('')
        props.onSettled()
      }
    },
  })
  const cancelMutation = useMutation({
    mutationFn: (taskId: string) =>
      cancelPoolAccountAuth(props.account.id, taskId),
    onSuccess: () => {
      toast.success(t('Authorization cancelled'))
      setCallbackInput('')
      props.onSettled()
    },
    onError: (error) => handleServerError(error),
  })
  const busy =
    startMutation.isPending ||
    reauthMutation.isPending ||
    completeMutation.isPending ||
    cancelMutation.isPending

  const submitComplete = () => {
    const value = callbackInput.trim()
    if (!value) return
    if (/^https?:\/\//i.test(value)) {
      completeMutation.mutate({ callback_url: value })
    } else {
      completeMutation.mutate({ code: value, state: '' })
    }
  }

  let panelBody: React.ReactNode
  if (authStart) {
    panelBody = (
      <OneTimeAuthView
        authStart={authStart}
        remaining={remaining}
        callbackInput={callbackInput}
        completing={completeMutation.isPending}
        cancelling={cancelMutation.isPending}
        onCallbackChange={setCallbackInput}
        onComplete={submitComplete}
        onCancel={() => cancelMutation.mutate(authStart.task_id)}
      />
    )
  } else if (pendingTask) {
    panelBody = (
      <div className='space-y-2'>
        <p className='text-muted-foreground text-sm'>
          {t(
            'The one-time authorization details are no longer available. Cancel this task and start again.'
          )}
        </p>
        <Button
          variant='outline'
          size='sm'
          disabled={cancelMutation.isPending}
          onClick={() => cancelMutation.mutate(pendingTask.id)}
        >
          {t('Cancel authorization')}
        </Button>
      </div>
    )
  } else {
    panelBody = (
      <p className='text-muted-foreground text-sm'>
        {t(
          'Start authorization to create a one-time authorization task for this account.'
        )}
      </p>
    )
  }

  return (
    <section className='bg-card space-y-3 rounded-xl border p-4'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <h3 className='text-sm font-semibold'>{t('Authorization')}</h3>
        <div className='flex flex-wrap gap-2'>
          <Button
            variant='outline'
            size='sm'
            disabled={busy || Boolean(authStart)}
            onClick={() => startMutation.mutate()}
          >
            {t('Start authorization')}
          </Button>
          <Button
            variant='outline'
            size='sm'
            disabled={busy || Boolean(authStart)}
            onClick={() => reauthMutation.mutate()}
          >
            {t('Re-authorize')}
          </Button>
        </div>
      </div>
      {panelBody}
    </section>
  )
}
