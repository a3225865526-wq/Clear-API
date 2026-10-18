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
import { useEffect, useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { CopyButton } from '@/components/copy-button'
import {
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import { formatPricingAmount, USD_PRICING_CURRENCY } from './currency'
import {
  generatePriceTableFromExpression,
  generateUpstreamPricing,
  upstreamMultiplierSchema,
  type GeneratedPriceRow,
  type GeneratedUpstreamPricing,
} from './upstream-pricing'

const fields = ['upstream', 'sales', 'exchange'] as const
const exampleTable = JSON.stringify(
  {
    currency: 'USD',
    token_unit: '1M',
    models: [
      { model: 'your-model-name', input: 10, output: 30, cache_read: 1 },
    ],
  },
  null,
  2
)

export function UpstreamPricingDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onApply: (pricing: GeneratedUpstreamPricing) => boolean
  /** Stored upstream cost prices for the model being edited, if any. */
  initialModels?: { model: string; expression: string }[]
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const fileInput = useRef<HTMLInputElement>(null)
  const [error, setError] = useState('')
  const [preview, setPreview] = useState<{
    fingerprint: string
    pricing: GeneratedUpstreamPricing
  } | null>(null)
  const positiveNumber = z
    .string()
    .trim()
    .min(1, t('Enter a positive multiplier'))
    .refine(
      (value) => upstreamMultiplierSchema.safeParse(Number(value)).success,
      t('Enter a positive multiplier')
    )
  const schema = z.object({
    table: z
      .string()
      .trim()
      .min(1, t('Import or paste the upstream price table')),
    upstream: positiveNumber,
    sales: positiveNumber,
    exchange: positiveNumber,
  })
  const form = useForm({
    resolver: zodResolver(schema),
    defaultValues: { table: '', upstream: '', sales: '', exchange: '1' },
  })
  // Seed the table from the model's stored upstream cost so the administrator
  // can generate a sales price without retyping prices already on record.
  useEffect(() => {
    if (!props.open) return
    const seeded = (props.initialModels ?? []).flatMap((entry) => {
      const table = generatePriceTableFromExpression(
        entry.model,
        entry.expression
      )
      return table ? table.models : []
    })
    if (!seeded.length) return
    form.setValue(
      'table',
      JSON.stringify(
        { currency: 'USD', token_unit: '1M', models: seeded },
        null,
        2
      )
    )
  }, [props.open, props.initialModels, form])
  const values = form.watch()
  const fingerprint = JSON.stringify(values)
  const current = preview?.fingerprint === fingerprint ? preview.pricing : null
  const labels = {
    upstream: t('Upstream account multiplier'),
    sales: t('Sales multiplier'),
    exchange: t('CNY per USD'),
  }
  const generate = form.handleSubmit((data) => {
    try {
      const pricing = generateUpstreamPricing(
        JSON.parse(data.table),
        Number(data.upstream),
        Number(data.sales),
        Number(data.exchange)
      )
      setPreview({ fingerprint, pricing })
      setError('')
    } catch (cause) {
      setPreview(null)
      if (cause instanceof z.ZodError) {
        setError(
          `${t('Check the price table fields, currency, units and tier limits.')} ${cause.issues.map((issue) => issue.path.join('.')).join(', ')}`
        )
      } else if (cause instanceof SyntaxError) {
        setError(
          t(
            'The price table is not valid JSON. Please import the complete table.'
          )
        )
      } else {
        setError(t('Generated price is outside the supported range'))
      }
    }
  })
  const columns: StaticDataTableColumn<GeneratedPriceRow>[] = [
    { id: 'model', header: t('Model'), cell: (row) => row.model },
    {
      id: 'category',
      header: t('Price item'),
      cell: (row) => (
        <div>
          <p>{t(row.label)}</p>
          <p className='text-muted-foreground text-xs'>
            {t('Tier {{tier}}', { tier: row.tier })} ·{' '}
            {row.unit === '1M' ? t('1M token') : t('request')}
          </p>
          {row.maxContext !== undefined && (
            <p className='text-muted-foreground text-xs'>
              {t('Context up to {{limit}} tokens', {
                limit: formatNumber(row.maxContext, locale),
              })}
            </p>
          )}
        </div>
      ),
    },
    {
      id: 'base',
      header: t('Base price'),
      cell: (row) => formatPricingAmount(row.base),
    },
    {
      id: 'cost',
      header: t('Upstream cost'),
      cell: (row) => formatPricingAmount(row.cost),
    },
    {
      id: 'sale',
      header: t('Generated sales price'),
      cell: (row) => formatPricingAmount(row.sale),
    },
    {
      id: 'difference',
      header: t('Price difference'),
      cell: (row) => (
        <span className={row.difference < 0 ? 'text-destructive' : undefined}>
          {formatPricingAmount(row.difference, USD_PRICING_CURRENCY)}
        </span>
      ),
    },
  ]

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Generate prices from upstream')}
      description={t(
        'Import the price table prepared from your upstream screenshot, then enter both multipliers.'
      )}
      contentClassName='sm:max-w-5xl'
      footer={
        <>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button
            disabled={!current}
            onClick={() => {
              if (!current) return
              if (props.onApply(current)) props.onOpenChange(false)
            }}
          >
            {t('Apply generated prices to draft')}
          </Button>
        </>
      }
    >
      <form onSubmit={generate} className='space-y-4'>
        <p className='bg-muted/40 rounded-lg p-3 text-sm'>
          {t(
            'Cost = base price × upstream multiplier. Sales price = base price × sales multiplier. Both multipliers use the same base price.'
          )}
        </p>
        <div className='grid gap-3 sm:grid-cols-3'>
          {fields.map((field) => (
            <div key={field} className='space-y-2'>
              <Label htmlFor={`upstream-pricing-${field}`}>
                {labels[field]}
              </Label>
              <Input
                id={`upstream-pricing-${field}`}
                inputMode='decimal'
                {...form.register(field)}
                aria-invalid={Boolean(form.formState.errors[field])}
                aria-describedby={
                  form.formState.errors[field]
                    ? `upstream-pricing-${field}-error`
                    : undefined
                }
              />
              {form.formState.errors[field] && (
                <p
                  id={`upstream-pricing-${field}-error`}
                  role='alert'
                  className='text-destructive text-sm'
                >
                  {form.formState.errors[field]?.message}
                </p>
              )}
            </div>
          ))}
        </div>
        <p className='text-muted-foreground text-xs'>
          {t(
            'For a CNY price table, enter how many CNY equal 1 USD. Preview and saved prices are in USD.'
          )}
        </p>
        <div className='space-y-2'>
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <Label htmlFor='upstream-price-table'>
              {t('Upstream price table')}
            </Label>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => fileInput.current?.click()}
            >
              {t('Import price table file')}
            </Button>
          </div>
          <input
            ref={fileInput}
            type='file'
            hidden
            accept='.json,application/json'
            onChange={async (event) => {
              const file = event.target.files?.[0]
              event.target.value = ''
              if (!file) return
              if (file.size > 1024 * 1024) {
                setError(t('Price table files must be smaller than 1 MB'))
                return
              }
              try {
                form.setValue('table', await file.text(), {
                  shouldValidate: true,
                })
                setError('')
              } catch {
                setError(t('Could not read the price table file'))
              }
            }}
          />
          <Textarea
            id='upstream-price-table'
            className='min-h-40 font-mono'
            {...form.register('table')}
            aria-invalid={Boolean(form.formState.errors.table)}
          />
          {form.formState.errors.table && (
            <p role='alert' className='text-destructive text-sm'>
              {form.formState.errors.table.message}
            </p>
          )}
          <details className='text-muted-foreground rounded-lg border p-3 text-xs'>
            <summary className='cursor-pointer'>
              {t('Price table format')}
            </summary>
            <p className='my-2'>
              {t(
                'Prices must be undiscounted base prices. Use USD or CNY and 1M or 1K tokens. Cache prices are optional; use request for per-request prices. Tiered models use tiers with increasing max_context limits and an unlimited final tier.'
              )}
            </p>
            <div className='flex items-start gap-2'>
              <pre className='min-w-0 flex-1 overflow-x-auto'>
                {exampleTable}
              </pre>
              <CopyButton
                value={exampleTable}
                aria-label={t('Copy price table template')}
              />
            </div>
          </details>
        </div>
        {error && (
          <p role='alert' className='text-destructive text-sm'>
            {error}
          </p>
        )}
        <Button type='submit'>{t('Generate price preview')}</Button>
      </form>
      {current && (
        <section
          className='mt-4 space-y-3'
          aria-label={t('Generated price preview')}
        >
          {current.rows.some((row) => row.difference < 0) && (
            <p className='text-destructive text-sm'>
              {t(
                'Some sales prices are below upstream cost. Review the price difference before applying.'
              )}
            </p>
          )}
          <p className='text-muted-foreground text-xs'>
            {t(
              'These are base model sales prices. Existing group multipliers and tool charges still apply. Applying updates the draft; save model prices to activate them.'
            )}
          </p>
          <StaticDataTable
            columns={columns}
            data={current.rows}
            getRowKey={(row) => `${row.model}:${row.tier}:${row.label}`}
            tableProps={{ 'aria-label': t('Generated price preview') }}
          />
        </section>
      )}
    </Dialog>
  )
}
