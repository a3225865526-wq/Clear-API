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
import { useMutation } from '@tanstack/react-query'
import { isCancel } from 'axios'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { handleServerError } from '@/lib/handle-server-error'

import { financeRequest } from './access'
import {
  billingDecisionSchema,
  type BillingDecisionValues,
  type BillingReservation,
} from './billing-review-types'

export function BillingDecisionForm(props: {
  row: BillingReservation
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const [pending, setPending] = useState<BillingDecisionValues | null>(null)
  const [message, setMessage] = useState('')
  const form = useForm<BillingDecisionValues>({
    resolver: zodResolver(billingDecisionSchema(props.row.applied)),
    defaultValues: { refund: '0', reason: '', ended: false },
    mode: 'onBlur',
  })
  const mutation = useMutation({
    retry: false,
    mutationFn: (values: BillingDecisionValues) =>
      financeRequest(`/billing-reservations/${props.row.id}/decision`, {
        method: 'POST',
        body: {
          expected_state: props.row.state,
          expected_applied: props.row.applied,
          expected_target: props.row.target,
          refund_quota: Number(values.refund),
          reason: values.reason,
          execution_ended: values.ended,
        },
      }),
    onSuccess: props.onSaved,
    onError: (error) => {
      if (isCancel(error)) return
      setMessage(
        t(
          'Decision failed. Refresh the reservation before retrying; no partial refund is committed.'
        )
      )
      handleServerError(error)
    },
    onSettled: () => setPending(null),
    meta: { errorToast: false },
  })
  return (
    <Form {...form}>
      <form
        className='space-y-4'
        onSubmit={form.handleSubmit((values) => {
          setMessage('')
          setPending(values)
        })}
      >
        <p className='text-muted-foreground text-sm'>
          {t(
            'Quota units are wallet credit, not a cash refund. Zero retains the charged amount. One final decision is allowed per request.'
          )}
        </p>
        <FormField
          control={form.control}
          name='refund'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Refund quota units')}</FormLabel>
              <FormControl>
                <Input
                  {...field}
                  inputMode='numeric'
                  disabled={mutation.isPending}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='reason'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Reconciliation evidence')}</FormLabel>
              <FormControl>
                <Textarea
                  {...field}
                  maxLength={500}
                  disabled={mutation.isPending}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='ended'
          render={({ field }) => (
            <FormItem>
              <div className='flex items-center gap-2'>
                <FormControl>
                  <Checkbox
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    onBlur={field.onBlur}
                    disabled={mutation.isPending}
                  />
                </FormControl>
                <FormLabel>
                  {t(
                    'I verified that the request has ended and checked the provider charge.'
                  )}
                </FormLabel>
              </div>
              <FormMessage />
            </FormItem>
          )}
        />
        {message && (
          <p role='alert' className='text-destructive text-sm'>
            {message}
          </p>
        )}
        <Button
          type='submit'
          disabled={
            mutation.isPending ||
            props.row.executing ||
            Boolean(props.row.decision)
          }
        >
          {t('Review decision')}
        </Button>
      </form>
      <ConfirmDialog
        open={pending !== null}
        onOpenChange={(open) => {
          if (!open && !mutation.isPending) setPending(null)
        }}
        title={t('Confirm billing decision')}
        isLoading={mutation.isPending}
        confirmText={t('Apply billing decision')}
        desc={
          <div className='space-y-2'>
            <p>
              {t('Refund quota units')}: {pending?.refund}
            </p>
            <p>
              {t('Final charged quota')}:{' '}
              {props.row.applied - Number(pending?.refund ?? 0)}
            </p>
            <p>
              {t(
                'This decision changes wallet credit and cannot be edited. Original billing evidence remains in history.'
              )}
            </p>
          </div>
        }
        handleConfirm={() => {
          if (pending && !mutation.isPending) mutation.mutate(pending)
        }}
      />
    </Form>
  )
}
