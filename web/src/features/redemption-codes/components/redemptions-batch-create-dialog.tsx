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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { formatCnyCents, parseCnyToCents } from '@/features/wallet/lib/money'
import { handleServerError } from '@/lib/handle-server-error'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { createRedemptionBatch } from '../api'

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
  const { t } = useTranslation()
  const [name, setName] = useState('')
  const [faceValue, setFaceValue] = useState('')
  const [count, setCount] = useState('100')
  const [expiredDate, setExpiredDate] = useState('')
  const [note, setNote] = useState('')
  const [productID, setProductID] = useState('')
  const [revision, setRevision] = useState('')
  const [evidenceOnly, setEvidenceOnly] = useState(false)
  const root = useAuthStore((s) => s.auth.user?.role === ROLE.SUPER_ADMIN)
  const [submitting, setSubmitting] = useState(false)

  const reset = () => {
    setName('')
    setFaceValue('')
    setCount('100')
    setExpiredDate('')
    setNote('')
    setProductID('')
    setRevision('')
    setEvidenceOnly(false)
  }

  const handleSubmit = async () => {
    const faceValueCents = parseCnyToCents(faceValue)
    if (faceValueCents === null) {
      toast.error(t('Enter a valid CNY amount with up to 2 decimals'))
      return
    }
    const parsedCount = Number.parseInt(count, 10)
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
      setSubmitting(true)
      const response = await createRedemptionBatch({
        name: name.trim(),
        face_value_cents: faceValueCents,
        count: parsedCount,
        expired_time: expiredTime,
        note: note.trim(),
        product_id: productID.trim(),
        product_revision: Number(revision),
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
        handleServerError(response, t('Failed to create batch'))
      }
    } catch (error) {
      handleServerError(error, t('Failed to create batch'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
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
          <Button onClick={() => void handleSubmit()} disabled={submitting}>
            {submitting ? t('Creating...') : t('Create')}
          </Button>
        </div>
      }
    >
      <Label>
        {t('Product ID')}
        <Input
          value={productID}
          onChange={(e) => setProductID(e.target.value)}
        />
      </Label>
      <Label>
        {t('Approved product revision')}
        <Input
          inputMode='numeric'
          value={revision}
          onChange={(e) => setRevision(e.target.value)}
        />
      </Label>
      {root ? (
        <Label>
          <Checkbox
            checked={evidenceOnly}
            onCheckedChange={(value) => {
              setEvidenceOnly(value === true)
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
            value={faceValue}
            onChange={(event) => setFaceValue(event.target.value)}
            placeholder='100.00'
            inputMode='decimal'
          />
        </div>
        <div className='space-y-1.5'>
          <Label htmlFor='batch-count'>{t('Count')}</Label>
          <Input
            id='batch-count'
            value={count}
            onChange={(event) => setCount(event.target.value)}
            inputMode='numeric'
          />
        </div>
      </div>
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
