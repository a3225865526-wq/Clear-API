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
import z from 'zod'

import { beijingInput, validRange } from './format'

export const operationLabels = {
  expense: 'Register expense or append correction',
  bill: 'Register or replace supplier bill',
  receipt: 'Confirm offline receipt',
  refund: 'Register cash refund fact',
  offline: 'Preview and import offline sources',
  consumption: 'Preview and import captured consumption',
  rebuild: 'Preview and apply controlled ledger rebuild',
  fx: 'Register frozen FX version',
  line: 'Register supplier line or append line correction',
} as const
export type Operation = keyof typeof operationLabels
export type Values = Record<string, string>
export const fields: Record<Operation, string[]> = {
  expense: [
    'event_key',
    'ledger',
    'category',
    'pool_key',
    'account_ref',
    'amount',
    'currency',
    'fx_version_id',
    'from',
    'to',
    'recurrence',
    'adjusts_id',
    'evidence',
    'note',
  ],
  bill: [
    'event_key',
    'channel_id',
    'expected_id',
    'replaces_id',
    'from',
    'to',
    'amount',
    'currency',
    'fx_version_id',
    'evidence',
  ],
  receipt: ['order_id', 'amount', 'evidence'],
  refund: ['event_key', 'receipt_id', 'state', 'amount', 'at', 'evidence'],
  offline: ['user_id', 'ids', 'expected_revision', 'evidence'],
  consumption: ['user_id', 'ids', 'expected_revision', 'evidence'],
  rebuild: ['user_id', 'ids', 'evidence'],
  fx: [
    'event_key',
    'expected_id',
    'from',
    'to',
    'currency',
    'amount',
    'evidence',
  ],
  line: [
    'event_key',
    'bill_id',
    'channel_id',
    'line_category',
    'upstream_model',
    'reference',
    'from',
    'to',
    'currency',
    'amount',
    'fx_version_id',
    'replaces_line_id',
    'references_line_id',
    'evidence',
  ],
}
export const fieldLabels: Record<string, string> = {
  event_key: 'Unique event key',
  ledger: 'Expense ledger',
  category: 'Expense category',
  pool_key: 'Pool key (optional)',
  account_ref: 'Account reference (optional)',
  amount: 'Original amount, exact decimal',
  currency: 'Original currency',
  fx_version_id: 'Frozen FX version ID (0 means missing; CNY requires 0)',
  from: 'Attribution start, Beijing time',
  to: 'Attribution end, exclusive, Beijing time',
  recurrence: 'Expense accrual',
  adjusts_id: 'Original expense ID (0 for new expense)',
  evidence: 'Evidence reference (no credentials)',
  note: 'Correction reason / note',
  channel_id: 'Channel ID',
  expected_id: 'Current bill head ID (0 for first bill)',
  replaces_id: 'Bill ID to replace (0 for new bill)',
  order_id: 'Offline order ID',
  receipt_id: 'Confirmed receipt ID',
  state: 'Refund lifecycle state',
  at: 'Refund time, Beijing time',
  user_id: 'Wallet user ID',
  ids: 'Selected fact IDs, comma separated',
  expected_revision: 'Expected wallet revision',
  bill_id: 'Bill ID',
  line_category: 'Bill line category',
  upstream_model: 'Upstream model (optional)',
  reference: 'Request or task reference (optional)',
  replaces_line_id: 'Line ID to replace (0 for new line)',
  references_line_id: 'Original line ID for credit (0 otherwise)',
}
export const options: Record<string, Record<string, string>> = {
  ledger: { operating: 'Operating expenses', direct: 'Direct cost' },
  recurrence: {
    once: 'Once',
    month: 'Monthly accrual',
    year: 'Yearly accrual',
  },
  state: {
    pending: 'Pending',
    queued: 'Queued',
    succeeded: 'Succeeded',
    failed: 'Failed',
  },
  line_category: {
    usage: 'Usage',
    fixed: 'Fixed fee',
    minimum: 'Minimum charge',
    late_fee: 'Late fee',
    credit: 'Credit line',
  },
}
export function operationFieldLabel(kind: Operation, key: string) {
  if (kind === 'fx') {
    const labels: Record<string, string> = {
      event_key: 'FX record key (reuse for next version)',
      expected_id: 'Current FX head ID (0 for first version)',
      amount: 'Frozen CNY per original currency unit',
    }
    return labels[key] ?? fieldLabels[key]
  }
  return fieldLabels[key]
}
export function integer(value: string, minimum = 0) {
  return (
    /^\d+$/.test(value) &&
    Number.isSafeInteger(Number(value)) &&
    Number(value) >= minimum
  )
}
export function selectedIDs(value: string) {
  return value.split(',').map((part) => Number(part.trim()))
}
export function operationSchema(kind: Operation) {
  return z.record(z.string(), z.string()).superRefine((values, ctx) => {
    const invalid = (key: string) =>
      ctx.addIssue({
        code: 'custom',
        path: [key],
        message: 'Enter a valid value for this field.',
      })
    for (const key of fields[kind]) {
      const value = values[key] ?? ''
      if (['upstream_model', 'reference'].includes(key)) {
        if (key === 'upstream_model' && value.length > 200) invalid(key)
        if (
          key === 'reference' &&
          value &&
          !/^[A-Za-z0-9][A-Za-z0-9_:-]{0,99}$/.test(value)
        ) {
          invalid(key)
        }
      } else if (['pool_key', 'account_ref', 'note'].includes(key)) {
        if (
          value &&
          key !== 'note' &&
          !/^[A-Za-z0-9][A-Za-z0-9_-]{0,99}$/.test(value)
        ) {
          invalid(key)
        }
        if (key === 'note' && value.length > 1000) invalid(key)
      } else if (key === 'ids') {
        const ids = selectedIDs(value)
        if (
          !value.split(',').every((part) => integer(part.trim(), 1)) ||
          ids.length > (kind === 'rebuild' ? 50 : 100) ||
          new Set(ids).size !== ids.length
        ) {
          invalid(key)
        }
      } else if (key === 'from' || key === 'to' || key === 'at') {
        if (
          !Number.isSafeInteger(beijingInput(value)) ||
          beijingInput(value) <= 0
        ) {
          invalid(key)
        }
      } else if (key.endsWith('_id') || key === 'expected_revision') {
        if (
          !integer(
            value,
            [
              'channel_id',
              'user_id',
              'order_id',
              'receipt_id',
              'bill_id',
            ].includes(key)
              ? 1
              : 0
          )
        ) {
          invalid(key)
        }
        if (
          ['channel_id', 'user_id', 'order_id'].includes(key) &&
          Number(value) > 2147483647
        ) {
          invalid(key)
        }
      } else if (key === 'event_key') {
        if (!/^[A-Za-z0-9][A-Za-z0-9_-]{0,99}$/.test(value)) invalid(key)
      } else if (key === 'currency') {
        if (!/^[A-Z]{3}$/.test(value)) invalid(key)
      } else if (key === 'amount') {
        if (!/^-?\d{1,16}(\.\d{1,12})?$/.test(value)) invalid(key)
        else {
          const zero = /^-?0+(\.0+)?$/.test(value)
          if (
            value.startsWith('-') &&
            !(kind === 'expense' && Number(values.adjusts_id) > 0)
          ) {
            invalid(key)
          }
          if (zero && !['bill', 'line'].includes(kind)) invalid(key)
        }
      } else if (options[key]) {
        if (!Object.hasOwn(options[key], value)) invalid(key)
      } else if (
        !value.trim() ||
        value.length > (key === 'category' ? 100 : 500)
      ) {
        invalid(key)
      }
    }
    if (
      fields[kind].includes('from') &&
      !validRange(beijingInput(values.from), beijingInput(values.to))
    ) {
      invalid('to')
    }
    if (
      kind === 'expense' &&
      Number(values.adjusts_id) > 0 &&
      (!values.note.trim() || values.recurrence !== 'once')
    ) {
      invalid('note')
    }
    if (values.currency === 'CNY' && Number(values.fx_version_id) !== 0) {
      invalid('fx_version_id')
    }
    if (values.account_ref && !values.pool_key) invalid('account_ref')
    if (kind === 'refund' && beijingInput(values.at) > Date.now() / 1000) {
      invalid('at')
    }
    if (kind === 'fx' && values.currency === 'CNY') invalid('currency')
    if (kind === 'line') {
      if (
        Number(values.replaces_line_id) > 0 &&
        Number(values.references_line_id) > 0
      ) {
        invalid('references_line_id')
      }
      if (
        (values.line_category === 'credit') !==
        Number(values.references_line_id) > 0
      ) {
        invalid('references_line_id')
      }
    }
  })
}
export function buildOperation(
  kind: Operation,
  v: Values
): { path: string; body: Record<string, unknown> } {
  const n = (key: string) => Number(v[key])
  const period = {
    period_from: beijingInput(v.from),
    period_to: beijingInput(v.to),
  }
  const money = {
    amount: v.amount,
    currency: v.currency,
    fx_version_id: n('fx_version_id'),
  }
  if (kind === 'fx') {
    return {
      path: '/versions/fx',
      body: {
        record_key: v.event_key,
        expected_id: n('expected_id'),
        effective_from: period.period_from,
        effective_to: period.period_to,
        active: true,
        source: v.evidence,
        payload: { currency: v.currency, cny_per_unit: v.amount },
      },
    }
  }
  if (kind === 'line') {
    return {
      path: '/supplier-bill-lines',
      body: {
        ...money,
        scope: 'supplier_bill_detail',
        event_key: v.event_key,
        bill_id: n('bill_id'),
        channel_id: n('channel_id'),
        category: v.line_category,
        upstream_model: v.upstream_model,
        reference: v.reference,
        usage_from: period.period_from,
        usage_to: period.period_to,
        replaces_line_id: n('replaces_line_id'),
        references_line_id: n('references_line_id'),
        evidence: v.evidence,
      },
    }
  }
  if (kind === 'expense') {
    return {
      path: '/expenses',
      body: {
        ...period,
        ...money,
        event_key: v.event_key,
        ledger: v.ledger,
        category: v.category,
        pool_key: v.pool_key,
        account_ref: v.account_ref,
        recurrence: v.recurrence,
        adjusts_id: n('adjusts_id'),
        source: v.evidence,
        note: v.note,
      },
    }
  }
  if (kind === 'bill') {
    return {
      path: '/supplier-bills',
      body: {
        ...period,
        ...money,
        scope: 'all_channel_request_costs',
        event_key: v.event_key,
        channel_id: n('channel_id'),
        expected_id: n('expected_id'),
        replaces_id: n('replaces_id'),
        evidence: v.evidence,
      },
    }
  }
  if (kind === 'receipt') {
    return {
      path: '/cash-receipts',
      body: {
        order_id: n('order_id'),
        confirmed_amount_cny: v.amount,
        evidence: v.evidence,
      },
    }
  }
  if (kind === 'refund') {
    return {
      path: '/cash-refunds',
      body: {
        event_key: v.event_key,
        receipt_id: n('receipt_id'),
        state: v.state,
        amount_cny: v.amount,
        refunded_at: beijingInput(v.at),
        evidence: v.evidence,
      },
    }
  }
  const suffix = {
    offline: 'offline-sources',
    consumption: 'consumptions',
    rebuild: 'rebuild',
  }[kind]
  const idKey = {
    offline: 'redemption_ids',
    consumption: 'observation_ids',
    rebuild: 'sync_event_ids',
  }[kind]
  return {
    path: `/wallets/${n('user_id')}/${suffix}`,
    body: {
      [idKey]: selectedIDs(v.ids),
      expected_revision: kind === 'rebuild' ? 0 : n('expected_revision'),
      evidence: v.evidence,
    },
  }
}
