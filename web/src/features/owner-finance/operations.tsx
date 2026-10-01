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
import { isCancel } from 'axios'
import { useEffect, useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

import { financeRequest } from './access'
import { Code, FinanceTable, Money, Panel } from './components'
import {
  buildOperation,
  operationFieldLabel,
  fields,
  operationLabels,
  operationSchema,
  options,
  type Operation,
  type Values,
} from './operation-form'

type Preview = {
  sources?: {
    observation_id?: number
    redemption_id?: number
    receipt_id?: number
    status?: string
    already_imported?: boolean
    event?: { value_cny?: string | null; quota?: number; kind?: string }
  }[]
  expected_revision?: number
  snapshot_hash?: string
  fact_count?: number
  current_remaining_quota?: number
  rebuilt_remaining_quota?: number
  current_known_revenue_cny?: string
  rebuilt_known_consumption_cny?: string | null
  current_known_backing_cny?: string
  rebuilt_credit_backing_cny?: string | null
  warnings?: string[]
}
export function FinanceOperations({ onChanged }: { onChanged: () => void }) {
  const { t } = useTranslation()
  const [kind, setKind] = useState<Operation>('expense')
  return (
    <Panel title={t('Finance operations')}>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Owner-confirmed facts append history and audit records. Corrections never erase old evidence. These operations do not change selling prices or business balances.'
        )}
      </p>
      <Label htmlFor='finance-operation'>{t('Operation')}</Label>
      <select
        id='finance-operation'
        className='bg-background h-10 w-full rounded-md border px-3 text-sm'
        value={kind}
        onChange={(e) => setKind(e.target.value as Operation)}
      >
        {Object.entries(operationLabels).map(([value, label]) => (
          <option key={value} value={value}>
            {t(label)}
          </option>
        ))}
      </select>
      <OperationForm key={kind} kind={kind} onChanged={onChanged} />
    </Panel>
  )
}
function OperationForm({
  kind,
  onChanged,
}: {
  kind: Operation
  onChanged: () => void
}) {
  const { t } = useTranslation()
  const needsPreview = ['offline', 'consumption', 'rebuild'].includes(kind)
  const [preview, setPreview] = useState<Preview | null>(null)
  const [pending, setPending] = useState<ReturnType<
    typeof buildOperation
  > | null>(null)
  const [confirm, setConfirm] = useState(false)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')
  const active = useRef<AbortController | null>(null)
  useEffect(
    () => () => {
      active.current?.abort()
    },
    []
  )
  const form = useForm<Values>({
    resolver: zodResolver(operationSchema(kind)),
    defaultValues: Object.fromEntries(
      fields[kind].map((key) => [
        key,
        {
          currency: kind === 'fx' ? 'USD' : 'CNY',
          ledger: 'operating',
          recurrence: 'once',
          state: 'pending',
          line_category: 'usage',
          fx_version_id: '0',
          adjusts_id: '0',
          expected_id: '0',
          replaces_id: '0',
          expected_revision: '0',
          replaces_line_id: '0',
          references_line_id: '0',
        }[key] ?? '',
      ])
    ),
  })
  const execute = async (
    operation: ReturnType<typeof buildOperation>,
    previewOnly: boolean
  ) => {
    const controller = new AbortController()
    active.current?.abort()
    active.current = controller
    setBusy(true)
    setMessage('')
    try {
      let path = operation.path
      if (needsPreview) {
        if (previewOnly) path += '/preview'
        else path += kind === 'rebuild' ? '/apply' : '/import'
      }
      const result = await financeRequest<Preview>(path, {
        method: 'POST',
        body: operation.body,
        signal: controller.signal,
      })
      if (controller.signal.aborted) return
      if (
        previewOnly &&
        kind === 'rebuild' &&
        (!Number.isSafeInteger(result.expected_revision) ||
          (result.expected_revision ?? -1) < 0 ||
          !/^[a-f0-9]{64}$/.test(result.snapshot_hash ?? ''))
      ) {
        throw new Error('FINANCE_UNAVAILABLE')
      }
      if (previewOnly) {
        setPreview(result)
        setPending(operation)
      } else {
        setConfirm(false)
        setPreview(null)
        setPending(null)
        form.reset()
        setMessage('Finance fact saved. Refreshing the snapshot.')
        onChanged()
      }
    } catch (error) {
      if (controller.signal.aborted || isCancel(error)) return
      setConfirm(false)
      setPreview(null)
      setPending(null)
      setMessage(
        error instanceof Error && error.message === 'FINANCE_CONFLICT'
          ? 'The ledger changed or the event already exists. Read current records and preview again; nothing was overwritten.'
          : 'Finance operation failed. Check the scope, evidence and schema before trying again.'
      )
    } finally {
      if (!controller.signal.aborted) setBusy(false)
    }
  }
  return (
    <div className='space-y-4'>
      <p className='text-sm'>
        {t(
          needsPreview
            ? 'Select existing fact IDs only. Review the server preview before applying. Wallet revisions and rebuild fingerprints reject stale changes.'
            : 'Review the scope and original currency before confirming. Missing FX stays unknown. A supplier bill covers all request costs for one channel and period.'
        )}
      </p>
      {kind === 'expense' && (
        <p className='text-muted-foreground text-xs'>
          {t(
            'Direct expenses may overlap supplier bills. Pool expenses post once. Signed corrections reference the original expense and require a reason; use once accrual.'
          )}
        </p>
      )}
      {kind === 'refund' && (
        <p className='text-muted-foreground text-xs'>
          {t(
            'Only succeeded cash refunds reduce net receipts. A cash refund does not reverse consumption revenue.'
          )}
        </p>
      )}
      {kind === 'fx' && (
        <p className='text-muted-foreground text-xs'>
          {t(
            'Amount is CNY per one original currency unit. Register a new frozen version; historical records retain their referenced FX. Current head ID is read from FX history.'
          )}
        </p>
      )}
      {kind === 'line' && (
        <p className='text-muted-foreground text-xs'>
          {t(
            'Bill lines are reconciliation only. Enter credit amounts as positive magnitudes; the ledger subtracts them from the referenced original line. Replacement and credit references are mutually exclusive.'
          )}
        </p>
      )}
      <Form {...form}>
        <form
          className='grid gap-4 sm:grid-cols-2'
          onChange={() => {
            setConfirm(false)
            setPreview(null)
            setPending(null)
            setMessage('')
          }}
          onSubmit={form.handleSubmit((values) => {
            const operation = buildOperation(kind, values)
            if (needsPreview) void execute(operation, true)
            else {
              setPending(operation)
              setConfirm(true)
            }
          })}
        >
          {fields[kind].map((key) => (
            <FormField
              key={key}
              control={form.control}
              name={key}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t(operationFieldLabel(kind, key))}</FormLabel>
                  <FormControl>
                    {options[key] ? (
                      <select
                        disabled={busy}
                        className='bg-background h-10 rounded-md border px-3 text-sm'
                        {...field}
                      >
                        {Object.entries(options[key]).map(([value, label]) => (
                          <option value={value} key={value}>
                            {t(label)}
                          </option>
                        ))}
                      </select>
                    ) : (
                      <Input
                        disabled={busy}
                        className='min-h-10'
                        {...field}
                        autoComplete='off'
                        type={
                          ['from', 'to', 'at'].includes(key)
                            ? 'datetime-local'
                            : 'text'
                        }
                        inputMode={
                          key.endsWith('_id') || key === 'expected_revision'
                            ? 'numeric'
                            : undefined
                        }
                      />
                    )}
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          ))}
          <div className='sm:col-span-2'>
            <Button type='submit' className='min-h-10' disabled={busy}>
              {busy
                ? t('Working…')
                : t(
                    needsPreview ? 'Read import preview' : 'Review registration'
                  )}
            </Button>
          </div>
        </form>
      </Form>
      {message && (
        <p role='status' className='text-sm'>
          {t(message)}
        </p>
      )}
      {preview && (
        <Panel title={t('Server preview · partial coverage')}>
          {preview.sources && (
            <FinanceTable
              data={preview.sources}
              label={t('Selected import facts')}
              columns={[
                {
                  id: 'id',
                  header: t('Fact ID / receipt'),
                  cell: (s) =>
                    `${s.observation_id ?? s.redemption_id} / ${s.receipt_id ?? '—'}`,
                },
                {
                  id: 'quota',
                  header: t('Quota'),
                  cell: (s) => s.event?.quota ?? t('Unknown'),
                },
                {
                  id: 'value',
                  header: t('Source value, CNY'),
                  cell: (s) => <Money value={s.event?.value_cny} />,
                },
                {
                  id: 'status',
                  header: t('Status'),
                  cell: (s) =>
                    s.already_imported ? (
                      t('Already imported')
                    ) : (
                      <Code code={s.status ?? 'partial'} />
                    ),
                },
              ]}
            />
          )}
          {kind === 'rebuild' && (
            <dl className='grid gap-2 text-sm sm:grid-cols-2'>
              <div>
                <dt>{t('Wallet revision / selected fact count')}</dt>
                <dd>
                  {preview.expected_revision} / {preview.fact_count}
                </dd>
              </div>
              <div>
                <dt>{t('Remaining quota: before → after')}</dt>
                <dd>
                  {preview.current_remaining_quota} →{' '}
                  {preview.rebuilt_remaining_quota}
                </dd>
              </div>
              <div>
                <dt>{t('Known revenue: before → after')}</dt>
                <dd>
                  <Money value={preview.current_known_revenue_cny} /> →{' '}
                  <Money value={preview.rebuilt_known_consumption_cny} />
                </dd>
              </div>
              <div>
                <dt>{t('Source backing: before → after')}</dt>
                <dd>
                  <Money value={preview.current_known_backing_cny} /> →{' '}
                  <Money value={preview.rebuilt_credit_backing_cny} />
                </dd>
              </div>
              <div className='min-w-0 sm:col-span-2'>
                <dt>{t('Rebuild fingerprint')}</dt>
                <dd className='font-mono text-xs break-all'>
                  {preview.snapshot_hash}
                </dd>
              </div>
            </dl>
          )}
          <ul className='list-inside list-disc space-y-1 text-sm'>
            {preview.warnings?.map((code) => (
              <li key={code}>
                <Code code={code} />
              </li>
            ))}
          </ul>
          <Button
            disabled={busy}
            className='min-h-10'
            onClick={() => setConfirm(true)}
          >
            {t('Review and apply selected facts')}
          </Button>
        </Panel>
      )}
      <ConfirmDialog
        open={confirm && pending !== null}
        onOpenChange={(open) => {
          if (!open && !busy) setConfirm(false)
        }}
        title={t(
          needsPreview
            ? 'Apply reviewed finance facts?'
            : 'Append this finance fact?'
        )}
        desc={
          <div className='space-y-2'>
            <p>
              {t(
                'This writes only the selected finance scope with an audit trail. Confirm the evidence and amounts. Production migration and platform connections are separate operations.'
              )}
            </p>
            <dl className='grid gap-1 text-sm'>
              {fields[kind].map((key) => (
                <div key={key} className='break-all'>
                  <dt className='text-muted-foreground'>
                    {t(operationFieldLabel(kind, key))}
                  </dt>
                  <dd>
                    {options[key]?.[form.getValues(key)]
                      ? t(options[key][form.getValues(key)])
                      : form.getValues(key) || '—'}
                  </dd>
                </div>
              ))}
            </dl>
          </div>
        }
        isLoading={busy}
        confirmText={t('Confirm finance write')}
        handleConfirm={() => {
          if (!pending) return
          const body =
            kind === 'rebuild'
              ? {
                  ...pending.body,
                  expected_revision: preview?.expected_revision,
                  snapshot_hash: preview?.snapshot_hash,
                }
              : pending.body
          void execute({ ...pending, body }, false)
        }}
      />
    </div>
  )
}
