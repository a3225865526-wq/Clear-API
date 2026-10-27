import { useMutation, useQueryClient } from '@tanstack/react-query'
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

import { ErrorState } from '@/components/error-state'
import { RiskAcknowledgementDialog } from '@/components/risk-acknowledgement-dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { SecureVerificationDialog } from '@/features/auth/secure-verification/components/secure-verification-dialog'
import { useSecureVerification } from '@/features/auth/secure-verification/hooks/use-secure-verification'
import { formatCnyCents } from '@/features/wallet/lib/money'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import { ROLE } from '@/lib/roles'
import { getServerErrorMessage } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import {
  changeProductState,
  getProductReview,
  type ProductReview,
} from '../api'
import type { CreditProduct } from '../catalog'

const stateLabels: Record<string, string> = {
  draft: 'Draft',
  pending: 'Pending review',
  approved: 'Approved',
  published: 'Published',
  rejected: 'Rejected',
  revoked: 'Approval revoked',
}

export function ProductOperations(props: {
  product: CreditProduct
  version: number
  disabled: boolean
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const root = useAuthStore((s) => s.auth.user?.role === ROLE.SUPER_ADMIN)
  const client = useQueryClient()
  const verification = useSecureVerification()
  const [review, setReview] = useState<ProductReview | null>(null)
  const [decision, setDecision] = useState('approve')
  const [legacyID, setLegacyID] = useState('')
  const product = props.product
  const mutation = useMutation({
    mutationFn: async (action: string) => {
      const mappingSuffix = action === 'map' ? `:${Number(legacyID)}` : ''
      const input = {
        version: props.version,
        revision: product.revision ?? 0,
        confirmation: review
          ? `${action.toUpperCase()} ${review.confirmation}${mappingSuffix}`
          : undefined,
        legacy_store_id: action === 'map' ? Number(legacyID) : 0,
        digest: review?.digest,
      }
      let proof: string | undefined
      if (action !== 'submit') {
        if (!review) throw new Error('Review product revision')
        const result = await verification.requestVerification({
          scope: 'store.product.operation',
          context: {
            product_id: product.id,
            version: input.version,
            revision: input.revision,
            action,
            digest: review.digest,
            legacy_store_id: input.legacy_store_id,
          },
          title: t('Review product revision'),
        })
        if (!result) return
        proof = result.proof_token
      }
      await changeProductState(product.id, action, input, proof)
    },
    onSuccess: async () => {
      setReview(null)
      setDecision('approve')
      await client.invalidateQueries({ queryKey: ['credit-catalog'] })
      await client.invalidateQueries({ queryKey: ['topup-info'] })
    },
  })
  const inspect = useMutation({
    mutationFn: getProductReview,
    onSuccess: setReview,
  })
  const mappingSuffix = decision === 'map' ? `:${Number(legacyID)}` : ''
  const requiredConfirmation = review
    ? `${decision.toUpperCase()} ${review.confirmation}${mappingSuffix}`
    : ''
  const disabled = !root || props.disabled || mutation.isPending
  const contractDisabled = disabled || !!product.archived
  if (!root) return null
  return (
    <div className='space-y-3 sm:col-span-2'>
      <p role='status'>
        {t(stateLabels[product.state ?? 'draft'])} · {t('Revision')}{' '}
        {product.revision ?? 0} · CNY
      </p>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Solo operator review is recorded. Confirmation text prevents mistakes; it is not multi-factor authentication.'
        )}
      </p>
      <div className='flex flex-wrap gap-2'>
        <Button
          type='button'
          variant='outline'
          disabled={
            contractDisabled ||
            !['draft', 'rejected', 'revoked'].includes(product.state ?? 'draft')
          }
          onClick={() => mutation.mutate('submit')}
        >
          {t('Submit for review')}
        </Button>
        <Button
          type='button'
          variant='outline'
          disabled={disabled}
          onClick={() => {
            setDecision(product.state === 'pending' ? 'approve' : 'inspect')
            inspect.mutate(product.id)
          }}
        >
          {t('Review product revision')}
        </Button>
        <Button
          type='button'
          disabled={contractDisabled || product.state !== 'approved'}
          onClick={() => {
            setDecision('publish')
            inspect.mutate(product.id)
          }}
        >
          {t('Publish product')}
        </Button>
        <Button
          type='button'
          variant='outline'
          disabled={contractDisabled || product.state !== 'published'}
          onClick={() => {
            setDecision('unpublish')
            inspect.mutate(product.id)
          }}
        >
          {t('Unpublish product')}
        </Button>
        <Button
          type='button'
          variant='destructive'
          disabled={
            contractDisabled ||
            !['approved', 'published'].includes(product.state ?? '')
          }
          onClick={() => {
            setDecision('revoke')
            inspect.mutate(product.id)
          }}
        >
          {t('Revoke approval')}
        </Button>
        <Button
          type='button'
          variant='outline'
          disabled={disabled}
          onClick={() => {
            setDecision(product.archived ? 'restore' : 'archive')
            inspect.mutate(product.id)
          }}
        >
          {product.archived ? t('Restore product') : t('Archive product')}
        </Button>
        <Button
          type='button'
          variant='destructive'
          disabled={disabled}
          onClick={() => {
            setDecision('inspect')
            inspect.mutate(product.id)
          }}
        >
          {t('Delete permanently')}
        </Button>
      </div>
      <Label className='block'>
        {t('Legacy store product ID')}
        <Input
          value={legacyID}
          onChange={(e) => setLegacyID(e.target.value)}
          inputMode='numeric'
        />
      </Label>
      <Button
        type='button'
        variant='outline'
        disabled={
          contractDisabled ||
          !Number.isSafeInteger(Number(legacyID)) ||
          Number(legacyID) <= 0
        }
        onClick={() => {
          setDecision('map')
          inspect.mutate(product.id)
        }}
      >
        {t('Map legacy product')}
      </Button>
      {mutation.isError || inspect.isError ? (
        <ErrorState
          description={t(
            getServerErrorMessage(mutation.error || inspect.error)
          )}
        />
      ) : null}
      <SecureVerificationDialog {...verification.dialogProps} />
      <RiskAcknowledgementDialog
        open={
          review !== null && verification.dialogProps.state.phase === 'idle'
        }
        onOpenChange={(open) => {
          if (!open && verification.dialogProps.state.phase === 'idle') {
            setReview(null)
            setDecision('approve')
          }
        }}
        title={t('Review product revision')}
        description={
          review ? (
            <div className='space-y-2 break-words'>
              <p>
                {review.product.id} @ {review.product.revision} ·{' '}
                {review.product.name}
              </p>
              <p>
                CNY {formatCnyCents(review.product.price_cents)} ·{' '}
                {t('Base API quota')}:{' '}
                {formatNumber(review.product.base_quota ?? 0, locale)}
              </p>
              <p>{review.product.purchase_url}</p>
              <p>
                {review.product.delivery_binding} ·{' '}
                {review.product.delivery_evidence} · {t('Evidence batch ID')}:{' '}
                {review.product.evidence_batch_id}
              </p>
              {['archive', 'restore'].includes(decision) ? (
                <p>
                  {t(
                    'Archiving stops public sales and preserves history. Restoring does not publish the product or bypass approval.'
                  )}
                </p>
              ) : null}
              {review.deletion ? (
                <div className='space-y-2'>
                  <p>
                    {t(
                      'Product history: {{batches}} batches, {{codes}} codes, {{orders}} orders, {{balance}} balance entries, {{events}} revision and approval events',
                      {
                        batches: formatNumber(review.deletion.batches, locale),
                        codes: formatNumber(review.deletion.codes, locale),
                        orders: formatNumber(review.deletion.orders, locale),
                        balance: formatNumber(
                          review.deletion.balance_entries,
                          locale
                        ),
                        events: formatNumber(review.deletion.events, locale),
                      }
                    )}
                  </p>
                  <p>
                    {t(
                      'Only an unpublished initial draft without revision, approval, evidence, inventory or financial history can be permanently deleted.'
                    )}
                  </p>
                  {decision === 'inspect' ? (
                    <Button
                      type='button'
                      variant='destructive'
                      disabled={!review.deletion.can_delete}
                      onClick={() => setDecision('delete')}
                    >
                      {t('Permanently delete this product')}
                    </Button>
                  ) : null}
                </div>
              ) : null}
              <p>
                {t(stateLabels[review.product.state ?? 'draft'])} →{' '}
                {decision === 'inspect'
                  ? t('Review product revision')
                  : t(decision)}
              </p>
              {review.events.map((event) => (
                <p key={event.id}>
                  {t(event.decision)} · {t('Operator ID')} {event.actor_id} ·{' '}
                  {new Date(event.created_time * 1000).toLocaleString(locale)} ·{' '}
                  {event.solo_operator ? t('Solo operator') : ''}
                </p>
              ))}
              {review.product.state === 'pending' && decision === 'approve' ? (
                <Button
                  type='button'
                  variant='outline'
                  onClick={() => setDecision('reject')}
                >
                  {t('Reject product')}
                </Button>
              ) : null}
            </div>
          ) : null
        }
        requiredText={decision === 'inspect' ? '' : requiredConfirmation}
        confirmText={
          decision === 'inspect' ? t('Close') : t('Confirm product operation')
        }
        isLoading={mutation.isPending}
        onConfirm={() => {
          if (decision === 'inspect') {
            setReview(null)
            return
          }
          mutation.mutate(decision)
        }}
      />
    </div>
  )
}
