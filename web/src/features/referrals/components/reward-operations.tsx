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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import { getReferralAdmin, qualifyCodes, resumeReward } from '../api'
import { formatReferralCredit } from '../lib/format-credit'
import { StoreQualification } from './store-qualification'

const qualificationSchema = z.object({
  ids: z.string().regex(/^\d+(\s*,\s*\d+)*$/),
  kind: z.enum(['paid', 'free', 'test', 'compensation']),
  reason: z.string().min(5).max(256),
})

export function RewardOperations() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const client = useQueryClient()
  const [page, setPage] = useState(0)
  const [resumeID, setResumeID] = useState<number | null>(null)
  const [reason, setReason] = useState('')
  const query = useQuery({
    queryKey: ['referral-admin', page],
    queryFn: () => getReferralAdmin(page),
  })
  const form = useForm<z.infer<typeof qualificationSchema>>({
    resolver: zodResolver(qualificationSchema),
    defaultValues: { ids: '', kind: 'free', reason: '' },
  })
  const refresh = () =>
    client.invalidateQueries({ queryKey: ['referral-admin'] })
  const qualify = useMutation({ mutationFn: qualifyCodes, onSuccess: refresh })
  const resume = useMutation({
    mutationFn: resumeReward,
    onSuccess: async () => {
      setResumeID(null)
      setReason('')
      await refresh()
    },
  })
  return (
    <div className='space-y-5'>
      <h3 className='font-semibold'>{t('Paid code reward qualification')}</h3>
      <p className='text-muted-foreground text-sm'>
        {t(
          'New formal store inventory is marked paid automatically. Delivery samples, free, test and compensation codes do not earn rewards. Qualify existing codes only after verifying their paid-sale purpose. Used codes cannot be requalified.'
        )}
      </p>
      <StoreQualification />
      <Collapsible>
        <CollapsibleTrigger render={<Button type='button' variant='outline' />}>
          {t('Advanced settings')}
        </CollapsibleTrigger>
        <CollapsibleContent className='space-y-3 pt-3'>
          <form
            className='grid grid-cols-1 gap-3 sm:grid-cols-2'
            onSubmit={form.handleSubmit((v) =>
              qualify.mutate({ ...v, ids: v.ids.split(',').map(Number) })
            )}
          >
            <label className='space-y-1'>
              {t('Code IDs separated by commas')}
              <Input {...form.register('ids')} />
            </label>
            <label className='space-y-1'>
              {t('Code qualification')}
              <NativeSelect {...form.register('kind')}>
                <NativeSelectOption value='paid'>
                  {t('Paid and eligible')}
                </NativeSelectOption>
                <NativeSelectOption value='free'>
                  {t('Free')}
                </NativeSelectOption>
                <NativeSelectOption value='test'>
                  {t('Test')}
                </NativeSelectOption>
                <NativeSelectOption value='compensation'>
                  {t('Compensation')}
                </NativeSelectOption>
              </NativeSelect>
            </label>
            <label className='space-y-1 sm:col-span-2'>
              {t('Verification reason')}
              <Input {...form.register('reason')} />
            </label>
            <Button type='submit' disabled={qualify.isPending}>
              {t('Save qualification')}
            </Button>
            {Object.entries(form.formState.errors).map(([key, error]) => (
              <p role='alert' key={key}>
                {t(error.message ?? 'Invalid qualification')}
              </p>
            ))}
          </form>
          {qualify.isSuccess ? (
            <p role='status'>{t('Saved successfully')}</p>
          ) : null}
          {qualify.isError ? <ErrorState /> : null}
        </CollapsibleContent>
      </Collapsible>
      <h3 className='font-semibold'>{t('Reward payout log and summary')}</h3>
      {query.isPending ? <p>{t('Loading...')}</p> : null}
      {query.isError ? <ErrorState onRetry={() => query.refetch()} /> : null}
      {query.isSuccess ? (
        <>
          <div className='flex flex-wrap gap-3'>
            {query.data.summary.map((s) => (
              <p key={s.status}>
                {t(s.status)}: {formatNumber(s.count, locale)} ·{' '}
                {formatReferralCredit(s.quota, locale)}
              </p>
            ))}
          </div>
          <ul className='divide-y'>
            {query.data.items.map(({ reward, inviter_id, invitee_id }) => (
              <li
                key={reward.id}
                className='flex flex-wrap items-center justify-between gap-2 py-3'
              >
                <span>
                  #{reward.id} · {t('Inviter')} #{inviter_id} · {t('Invitee')} #
                  {invitee_id} ·{' '}
                  {formatReferralCredit(reward.reward_quota, locale)} ·{' '}
                  {t(reward.status)}
                </span>
                {reward.status === 'paused' ? (
                  <Button
                    variant='outline'
                    onClick={() => setResumeID(reward.id)}
                  >
                    {t('Review and resume')}
                  </Button>
                ) : null}
              </li>
            ))}
          </ul>
          <div className='flex gap-2'>
            <Button
              variant='outline'
              disabled={page === 0}
              onClick={() => setPage(page - 1)}
            >
              {t('Previous')}
            </Button>
            <Button
              variant='outline'
              disabled={(page + 1) * 50 >= query.data.total}
              onClick={() => setPage(page + 1)}
            >
              {t('Next')}
            </Button>
          </div>
          <h3 className='font-semibold'>{t('Administrator audit log')}</h3>
          <ul className='divide-y'>
            {query.data.audits.map((a) => (
              <li key={a.id} className='space-y-1 py-2'>
                <p>
                  #{a.id} · {t(a.action)} · {t('Administrator')} #{a.actor_id}
                </p>
                <p className='text-muted-foreground text-xs break-all'>
                  {a.details}
                </p>
              </li>
            ))}
          </ul>
        </>
      ) : null}
      <ConfirmDialog
        open={resumeID !== null}
        onOpenChange={(open) => {
          if (!open) {
            setResumeID(null)
            setReason('')
          }
        }}
        title={t('Review and resume')}
        desc={t(
          'Confirm the original redemption, reward snapshot and accounting ledger before resuming. Rewards must be enabled.'
        )}
        disabled={reason.length < 5 || reason.length > 256}
        isLoading={resume.isPending}
        handleConfirm={() => {
          if (resumeID !== null) resume.mutate({ id: resumeID, reason })
        }}
      >
        <label className='space-y-1'>
          {t('Verification reason')}
          <Input value={reason} onChange={(e) => setReason(e.target.value)} />
        </label>
      </ConfirmDialog>
    </div>
  )
}
