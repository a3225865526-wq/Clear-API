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
import z from 'zod'

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
import { Textarea } from '@/components/ui/textarea'
import { handleServerError } from '@/lib/handle-server-error'

import { financeRequest } from './access'
import type { TaskSettlement } from './task-settlement-review'

const schema = z.object({
  reason: z
    .string()
    .trim()
    .min(1)
    .refine((value) => new TextEncoder().encode(value).length <= 500),
  confirmed: z.boolean().refine((value) => value === true),
})
type Values = z.infer<typeof schema>

export function TaskSettlementRetry(props: {
  row: TaskSettlement
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const [pending, setPending] = useState<Values | null>(null)
  const [message, setMessage] = useState('')
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { reason: '', confirmed: false },
  })
  const logOnly = props.row.state === 'applied'
  const mutation = useMutation({
    retry: false,
    mutationFn: (values: Values) =>
      financeRequest<TaskSettlement>(
        `/task-settlements/${props.row.id}/retry`,
        {
          method: 'POST',
          body: {
            expected_attempts: props.row.attempts,
            expected_target: props.row.target_quota,
            expected_log_attempts: props.row.log_attempts,
            log_only: logOnly,
            reason: values.reason,
            confirmed: values.confirmed,
          },
        }
      ),
    onSuccess: (row) => {
      if (
        row.state === 'applied' &&
        (!logOnly || row.log_state === 'delivered')
      ) {
        props.onSaved()
        return
      }
      setMessage(
        t(
          'Retry remains unresolved. Refresh and inspect the current settlement state.'
        )
      )
    },
    onError: (error) => {
      if (isCancel(error)) return
      setMessage(
        t(
          'Retry failed. No partial funding adjustment is committed. Refresh before retrying.'
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
        <p>
          {t(
            'Only the frozen target is retried. This operation cannot change the price, waive a debt or refund cash.'
          )}
        </p>
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
          name='confirmed'
          render={({ field }) => (
            <FormItem>
              <div className='flex items-center gap-2'>
                <FormControl>
                  <Checkbox
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={mutation.isPending}
                  />
                </FormControl>
                <FormLabel>
                  {t(
                    'I verified the frozen charge and resolved the reported failure.'
                  )}
                </FormLabel>
              </div>
              <FormMessage />
            </FormItem>
          )}
        />
        {message && <p role='alert'>{message}</p>}
        <Button type='submit' disabled={mutation.isPending}>
          {t('Review settlement retry')}
        </Button>
      </form>
      <ConfirmDialog
        open={pending !== null}
        onOpenChange={(open) => {
          if (!open && !mutation.isPending) setPending(null)
        }}
        title={t('Confirm settlement retry')}
        desc={t(
          'Retry the displayed settlement snapshot? A stale snapshot will be rejected.'
        )}
        confirmText={t('Retry frozen settlement')}
        isLoading={mutation.isPending}
        handleConfirm={() => {
          if (pending && !mutation.isPending) mutation.mutate(pending)
        }}
      />
    </Form>
  )
}
