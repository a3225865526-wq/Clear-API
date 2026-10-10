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

import { ConfirmDialog } from '@/components/confirm-dialog'
import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Switch } from '@/components/ui/switch'
import { decimalUnits } from '@/features/shop/lib/catalog-form'
import { SettingsSection } from '@/features/system-settings/components/settings-section'

import { getReferralRule, saveReferralRule, type ReferralRule } from '../api'
import { referralRuleSchema } from '../lib/reward-form'
import { RewardOperations } from './reward-operations'

function RuleForm(props: { rule: ReferralRule }) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const form = useForm<ReferralRule>({
    resolver: zodResolver(referralRuleSchema),
    values: props.rule,
  })
  const [approval, setApproval] = useState<ReferralRule | null>(null)
  const save = useMutation({
    mutationFn: saveReferralRule,
    onSuccess: async () => {
      await Promise.all([
        client.invalidateQueries({ queryKey: ['referral-rule'] }),
        client.invalidateQueries({ queryKey: ['referral-admin'] }),
        client.invalidateQueries({ queryKey: ['referral-self'] }),
      ])
    },
  })
  return (
    <form
      onSubmit={form.handleSubmit((v) => {
        if (v.rewards_enabled && !props.rule.rewards_enabled) setApproval(v)
        else save.mutate(v)
      })}
      className='space-y-4'
    >
      <p className='text-muted-foreground text-sm'>
        {t(
          'Disabling rewards pauses outstanding payouts. Re-enabling does not resume paused rewards; each requires an audited administrator review.'
        )}
      </p>
      <fieldset
        disabled={save.isPending}
        className='grid grid-cols-1 gap-4 sm:grid-cols-2'
      >
        <label className='flex items-center gap-2'>
          <Switch
            checked={form.watch('invites_enabled')}
            onCheckedChange={(v) => form.setValue('invites_enabled', v)}
          />
          {t('Enable invitations')}
        </label>
        <label className='flex items-center gap-2'>
          <Switch
            checked={form.watch('rewards_enabled')}
            onCheckedChange={(v) => form.setValue('rewards_enabled', v)}
          />
          {t('Enable referral rewards')}
        </label>
        <RadioGroup
          value={form.watch('mode')}
          onValueChange={(v) => {
            if (v === 'fixed' || v === 'percentage') form.setValue('mode', v)
          }}
          className='sm:col-span-2'
        >
          <label className='flex items-center gap-2'>
            <RadioGroupItem value='percentage' />
            {t('Percentage reward')}
          </label>
          <label className='flex items-center gap-2'>
            <RadioGroupItem value='fixed' />
            {t('Fixed API credit reward')}
          </label>
        </RadioGroup>
        <label className='space-y-1'>
          {t('Reward percentage')}
          <Input
            type='number'
            min='0'
            max='100'
            step='0.01'
            disabled={form.watch('mode') !== 'percentage'}
            defaultValue={props.rule.basis_points / 100}
            onChange={(e) =>
              form.setValue('basis_points', decimalUnits(e.target.value, 100), {
                shouldValidate: true,
              })
            }
          />
        </label>
        <label className='space-y-1'>
          {t('Fixed reward in USD API credit')}
          <Input
            type='number'
            min='0'
            step='0.000002'
            disabled={form.watch('mode') !== 'fixed'}
            defaultValue={props.rule.fixed_quota / 500000}
            onChange={(e) =>
              form.setValue(
                'fixed_quota',
                decimalUnits(e.target.value, 500000),
                { shouldValidate: true }
              )
            }
          />
        </label>
        <label className='space-y-1'>
          {t('Per reward limit in USD API credit')}
          <Input
            type='number'
            min='0.000002'
            step='0.000002'
            defaultValue={props.rule.per_reward_cap / 500000}
            onChange={(e) =>
              form.setValue(
                'per_reward_cap',
                decimalUnits(e.target.value, 500000),
                { shouldValidate: true }
              )
            }
          />
        </label>
        <label className='space-y-1'>
          {t('Lifetime limit per inviter in USD API credit')}
          <Input
            type='number'
            min='0.000002'
            step='0.000002'
            defaultValue={props.rule.lifetime_cap / 500000}
            onChange={(e) =>
              form.setValue(
                'lifetime_cap',
                decimalUnits(e.target.value, 500000),
                { shouldValidate: true }
              )
            }
          />
        </label>
        <Button type='submit'>{t('Save reward settings')}</Button>
      </fieldset>
      {Object.entries(form.formState.errors).map(([key, error]) => (
        <p role='alert' key={key}>
          {t(error.message ?? 'Invalid settings')}
        </p>
      ))}
      {save.isError ? <ErrorState /> : null}
      {save.isSuccess ? <p role='status'>{t('Saved successfully')}</p> : null}
      <ConfirmDialog
        open={approval !== null}
        onOpenChange={(open) => {
          if (!open) setApproval(null)
        }}
        title={t('Enable referral rewards')}
        desc={t(
          'Confirm operational approval, paid code qualification and accounting acceptance before enabling rewards.'
        )}
        isLoading={save.isPending}
        handleConfirm={() => {
          if (approval) {
            save.mutate(approval)
            setApproval(null)
          }
        }}
      />
    </form>
  )
}

export function RewardSettings() {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['referral-rule'],
    queryFn: getReferralRule,
  })
  return (
    <SettingsSection title={t('Referral reward settings')}>
      {query.isPending ? <p>{t('Loading...')}</p> : null}
      {query.isError ? <ErrorState onRetry={() => query.refetch()} /> : null}
      {query.isSuccess ? (
        <RuleForm key={query.data.version} rule={query.data} />
      ) : null}
      <RewardOperations />
    </SettingsSection>
  )
}
