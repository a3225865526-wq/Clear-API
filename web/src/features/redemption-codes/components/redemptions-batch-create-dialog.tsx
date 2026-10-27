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
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { formatCnyCents } from '@/features/wallet/lib/money'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { ROLE } from '@/lib/roles'
import { getServerErrorMessage } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import { createRedemptionBatch, getRedemptionBatchProducts } from '../api'
import type { RedemptionBatchProduct } from '../types'

interface RedemptionsBatchCreateDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  maxCount: number
  onCreated: (batchId: number, count: number) => void
}

/**
 * Create a store batch: server freezes the quota from the current Price,
 * generates all codes in one transaction and returns their keys (which this
 * dialog intentionally does NOT display — use the export dialog instead).
 */
export function RedemptionsBatchCreateDialog(
  props: RedemptionsBatchCreateDialogProps
) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [name, setName] = useState('')
  const [count, setCount] = useState('100')
  const [expiredDate, setExpiredDate] = useState('')
  const [note, setNote] = useState('')
  const [productID, setProductID] = useState('')
  const [products, setProducts] = useState<RedemptionBatchProduct[]>([])
  const [loadingProducts, setLoadingProducts] = useState(false)
  const [evidenceOnly, setEvidenceOnly] = useState(false)
  const root = useAuthStore((s) => s.auth.user?.role === ROLE.SUPER_ADMIN)
  const [submitting, setSubmitting] = useState(false)
  const submitPending = useRef(false)
  const [errorMessage, setErrorMessage] = useState('')
  const selectedProduct = products.find((p) => p.id === productID)
  const eligibleProduct =
    selectedProduct &&
    (evidenceOnly ? selectedProduct.sample_ready : selectedProduct.stock_ready)

  useEffect(() => {
    if (!props.open) return
    let active = true
    setLoadingProducts(true)
    void getRedemptionBatchProducts()
      .then((response) => {
        if (!active) return
        if (!response.success || !response.data) {
          setProducts([])
          setErrorMessage(
            t(response.message || 'Failed to load batch products')
          )
          return
        }
        setProducts(response.data)
      })
      .catch((error: unknown) => {
        if (!active) return
        setProducts([])
        setErrorMessage(t('Failed to load batch products'))
        handleServerError(error)
      })
      .finally(() => {
        if (active) setLoadingProducts(false)
      })
    return () => {
      active = false
    }
  }, [props.open, t])

  const reset = () => {
    setName('')
    setCount('100')
    setExpiredDate('')
    setNote('')
    setProductID('')
    setEvidenceOnly(false)
  }

  const handleSubmit = async () => {
    if (submitPending.current) return
    setErrorMessage('')
    const nameLength = [...name.trim()].length
    if (nameLength < 1 || nameLength > 64) {
      setErrorMessage(
        t('Name must be between {{min}} and {{max}} characters', {
          min: 1,
          max: 64,
        })
      )
      return
    }
    if (!eligibleProduct || !selectedProduct) {
      setErrorMessage(
        t(
          'Select a product revision with valid approval and delivery evidence.'
        )
      )
      return
    }
    const parsedCount = Number(count)
    if (
      !Number.isInteger(parsedCount) ||
      parsedCount < 1 ||
      parsedCount > props.maxCount
    ) {
      toast.error(
        t('Count must be between 1 and {{max}}', { max: props.maxCount })
      )
      return
    }
    const expiredTime = expiredDate
      ? Math.floor(new Date(`${expiredDate}T23:59:59`).getTime() / 1000)
      : 0

    try {
      submitPending.current = true
      setSubmitting(true)
      const response = await createRedemptionBatch({
        name: name.trim(),
        face_value_cents: selectedProduct.price_cents,
        count: parsedCount,
        expired_time: expiredTime,
        note: note.trim(),
        product_id: selectedProduct.id,
        product_revision: selectedProduct.revision,
        evidence_only: evidenceOnly,
      })
      if (response.success && response.data) {
        toast.success(
          t('Batch created, {{count}} codes generated ({{face}} each)', {
            count: response.data.keys.length,
            face: formatCnyCents(response.data.batch.face_value_cents),
          })
        )
        reset()
        props.onCreated(response.data.batch.id, response.data.keys.length)
      } else {
        const message = t(response.message || 'Failed to create batch')
        setErrorMessage(message)
        handleServerError(response, undefined, { title: message })
      }
    } catch (error) {
      const message = t(
        getServerErrorMessage(error, t('Failed to create batch'))
      )
      setErrorMessage(message)
      handleServerError(error, undefined, { title: message })
    } finally {
      submitPending.current = false
      setSubmitting(false)
    }
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={(open) => {
        if (!submitPending.current) {
          setErrorMessage('')
          props.onOpenChange(open)
        }
      }}
      title={t('Create Store Batch')}
      contentClassName='sm:max-w-md'
      bodyClassName='space-y-4'
      footer={
        <div className='flex gap-2'>
          <Button
            variant='outline'
            onClick={() => props.onOpenChange(false)}
            disabled={submitting}
          >
            {t('Cancel')}
          </Button>
          <Button
            onClick={() => void handleSubmit()}
            disabled={submitting || loadingProducts || !eligibleProduct}
          >
            {submitting ? t('Creating...') : t('Create')}
          </Button>
        </div>
      }
    >
      {errorMessage ? (
        <p role='alert' className='text-destructive text-sm'>
          {errorMessage}
        </p>
      ) : null}
      <p className='text-muted-foreground text-xs'>
        {t(
          'An approved revision can supply multiple batches. Face value and quota remain fixed; batch name, quantity and expiry are independent.'
        )}
      </p>
      <Label>
        {t('Product ID')}
        <NativeSelect
          aria-label={t('Product ID')}
          value={productID}
          onChange={(e) => setProductID(e.target.value)}
          disabled={loadingProducts || submitting}
        >
          <NativeSelectOption value=''>
            {t('Select a product')}
          </NativeSelectOption>
          {products.map((p) => (
            <NativeSelectOption
              key={p.id}
              value={p.id}
              disabled={evidenceOnly ? !p.sample_ready : !p.stock_ready}
            >
              {p.name} · {p.id} @ {p.revision}
              {(evidenceOnly ? !p.sample_ready : !p.stock_ready)
                ? ` · ${t('Unavailable')}`
                : ''}
            </NativeSelectOption>
          ))}
        </NativeSelect>
      </Label>
      {!loadingProducts &&
      !products.some((p) => (evidenceOnly ? p.sample_ready : p.stock_ready)) ? (
        <p role='status' className='text-muted-foreground text-sm'>
          {t(
            'No eligible product revisions. Review the product and its delivery evidence.'
          )}
        </p>
      ) : null}
      <Label>
        {t('Approved product revision')}
        <Input value={selectedProduct?.revision ?? ''} readOnly />
      </Label>
      {root ? (
        <Label>
          <Checkbox
            checked={evidenceOnly}
            onCheckedChange={(value) => {
              setEvidenceOnly(value === true)
              setProductID('')
              if (value) setCount('1')
            }}
          />
          {t('Delivery evidence sample only (one code, preparation mode)')}
        </Label>
      ) : null}
      <div className='space-y-1.5'>
        <Label htmlFor='batch-name'>{t('Batch Name')}</Label>
        <Input
          id='batch-name'
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder='20261003-100YUAN'
          maxLength={64}
        />
      </div>
      <div className='grid grid-cols-2 gap-3'>
        <div className='space-y-1.5'>
          <Label htmlFor='batch-face'>{t('Face Value (CNY)')}</Label>
          <Input
            id='batch-face'
            value={
              selectedProduct ? formatCnyCents(selectedProduct.price_cents) : ''
            }
            readOnly
          />
        </div>
        <div className='space-y-1.5'>
          <Label htmlFor='batch-count'>{t('Count')}</Label>
          <Input
            id='batch-count'
            disabled={evidenceOnly || submitting}
            value={count}
            onChange={(event) => setCount(event.target.value)}
            inputMode='numeric'
          />
        </div>
      </div>
      {selectedProduct ? (
        <p className='text-muted-foreground text-xs'>
          {t('Base API quota')}:{' '}
          {formatNumber(selectedProduct.base_quota, locale)} ·{' '}
          {t('Evidence batch ID')}: {selectedProduct.evidence_batch_id} ·{' '}
          {selectedProduct.delivery_binding}
        </p>
      ) : null}
      <div className='space-y-1.5'>
        <Label htmlFor='batch-expiry'>{t('Expires At')}</Label>
        <Input
          id='batch-expiry'
          type='date'
          value={expiredDate}
          onChange={(event) => setExpiredDate(event.target.value)}
        />
        <p className='text-muted-foreground text-xs'>
          {t('Leave empty for codes that never expire')}
        </p>
      </div>
      <div className='space-y-1.5'>
        <Label htmlFor='batch-note'>{t('Note')}</Label>
        <Input
          id='batch-note'
          value={note}
          onChange={(event) => setNote(event.target.value)}
          placeholder={t('e.g. third-party store ¥100 stock')}
          maxLength={255}
        />
      </div>
    </Dialog>
  )
}
