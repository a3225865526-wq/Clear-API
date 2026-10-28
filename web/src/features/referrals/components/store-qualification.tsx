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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import { getServerErrorMessage } from '@/lib/server-error-message'

import {
  getReferralStockPreview,
  qualifyReferralStock,
  type ReferralStockPreview,
} from '../api'

export function StoreQualification() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const client = useQueryClient()
  const [preview, setPreview] = useState<ReferralStockPreview | null>(null)
  const check = useMutation({
    mutationFn: getReferralStockPreview,
    onSuccess: (data) => {
      apply.reset()
      setPreview(data.count > 0 ? data : null)
    },
  })
  const apply = useMutation({
    mutationFn: qualifyReferralStock,
    onSuccess: async () => {
      setPreview(null)
      await client.invalidateQueries({ queryKey: ['referral-admin'] })
    },
    onError: () => setPreview(null),
  })
  const error = check.error ?? apply.error
  return (
    <div className='space-y-3'>
      <Button
        type='button'
        aria-busy={check.isPending || apply.isPending}
        disabled={check.isPending || apply.isPending}
        onClick={() => check.mutate()}
      >
        {t('Check existing store reward eligibility')}
      </Button>
      {check.isSuccess && check.data.count === 0 ? (
        <p role='status'>{t('No eligible unqualified store codes found.')}</p>
      ) : null}
      {apply.isSuccess ? (
        <p role='status'>
          {t('Updated reward eligibility for {{codes}} codes.', {
            codes: formatNumber(apply.data.updated_count, locale),
          })}
        </p>
      ) : null}
      {error ? (
        <ErrorState description={t(getServerErrorMessage(error))} />
      ) : null}
      <ConfirmDialog
        open={preview !== null}
        onOpenChange={(open) => {
          if (!open && !apply.isPending) setPreview(null)
        }}
        title={t('Enable rewards for existing store inventory')}
        desc={t(
          'Confirm these codes are intended for paid sales. Samples, free, test, compensation and used codes are excluded. Past redemptions will not receive rewards.'
        )}
        confirmText={t('Enable eligibility')}
        isLoading={apply.isPending}
        handleConfirm={() => {
          if (preview && !apply.isPending) apply.mutate(preview.digest)
        }}
      >
        {preview ? (
          <div className='space-y-2'>
            <p>
              {t('{{codes}} eligible codes in {{batches}} batches', {
                codes: formatNumber(preview.count, locale),
                batches: formatNumber(preview.batches.length, locale),
              })}
            </p>
            <ul className='max-h-60 space-y-1 overflow-y-auto text-sm'>
              {preview.batches.map((batch) => (
                <li key={batch.id} className='break-words'>
                  #{batch.id} · {batch.name} · {batch.product_id}@
                  {batch.product_revision} · {formatNumber(batch.count, locale)}
                </li>
              ))}
            </ul>
          </div>
        ) : null}
      </ConfirmDialog>
    </div>
  )
}
