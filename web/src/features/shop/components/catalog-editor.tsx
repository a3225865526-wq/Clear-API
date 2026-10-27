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
import { useFieldArray, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { ErrorState } from '@/components/error-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { SecureVerificationDialog } from '@/features/auth/secure-verification/components/secure-verification-dialog'
import { useSecureVerification } from '@/features/auth/secure-verification/hooks/use-secure-verification'
import { RedemptionsBatchCreateDialog } from '@/features/redemption-codes/components/redemptions-batch-create-dialog'
import { RedemptionsBatchExportDialog } from '@/features/redemption-codes/components/redemptions-batch-export-dialog'
import { SettingsSection } from '@/features/system-settings/components/settings-section'
import { formatCnyCents } from '@/features/wallet/lib/money'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import { ROLE } from '@/lib/roles'
import { getServerErrorMessage } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import {
  changeProductState,
  getAdminCatalog,
  getProductReview,
  saveCatalog,
  type Catalog,
} from '../api'
import { trustedPurchaseUrl } from '../catalog'
import { catalogSchema, decimalUnits } from '../lib/catalog-form'
import { ProductOperations } from './product-operations'

function CatalogForm(props: {
  catalog: Catalog
  onActivationError: (message: string) => void
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const client = useQueryClient()
  const verification = useSecureVerification()
  const root = useAuthStore((s) => s.auth.user?.role === ROLE.SUPER_ADMIN)
  const [issuingProductID, setIssuingProductID] = useState<string | null>(null)
  const [exportBatchID, setExportBatchID] = useState<number | null>(null)
  const [showArchived, setShowArchived] = useState(false)
  const [discardIndex, setDiscardIndex] = useState<number | null>(null)
  const form = useForm<Catalog>({
    resolver: zodResolver(catalogSchema),
    values: props.catalog,
  })
  const fields = useFieldArray({
    control: form.control,
    name: 'products',
    keyName: 'fieldKey',
  })
  const save = useMutation({
    mutationFn: saveCatalog,
    onSuccess: () => client.invalidateQueries({ queryKey: ['credit-catalog'] }),
  })
  const activate = useMutation({
    mutationFn: async (input: { values: Catalog; id: string }) => {
      props.onActivationError('')
      let saved = false
      try {
        if (form.formState.isDirty) {
          await saveCatalog(input.values)
          saved = true
        }
        const [catalog, review] = await Promise.all([
          getAdminCatalog(),
          getProductReview(input.id),
        ])
        const result = await verification.requestVerification({
          scope: 'store.product.operation',
          context: {
            product_id: input.id,
            version: catalog.version,
            revision: review.product.revision ?? 0,
            action: 'activate',
            digest: review.digest,
            legacy_store_id: 0,
          },
          title: t('Automatically validate and publish'),
        })
        if (!result) {
          throw new Error(
            t('Activation cancelled. The saved draft is retained.')
          )
        }
        await changeProductState(
          input.id,
          'activate',
          {
            version: catalog.version,
            revision: review.product.revision ?? 0,
            digest: review.digest,
            confirmation: `ACTIVATE ${review.confirmation}`,
            legacy_store_id: 0,
          },
          result.proof_token
        )
      } catch (error) {
        const reason = t(getServerErrorMessage(error))
        props.onActivationError(
          saved
            ? t('Draft saved. Activation failed: {{reason}}', { reason })
            : reason
        )
        throw error
      }
    },
    onSettled: async () => {
      await Promise.all([
        client.invalidateQueries({ queryKey: ['credit-catalog'] }),
        client.invalidateQueries({ queryKey: ['topup-info'] }),
      ])
    },
  })
  return (
    <form
      onSubmit={form.handleSubmit((values) => save.mutate(values))}
      className='space-y-5'
    >
      <p className='text-muted-foreground text-sm'>
        {t(
          'Save a draft, or save and enable with automatic validation. Internal checks do not verify third-party order delivery.'
        )}
      </p>
      <Button
        type='button'
        variant='outline'
        aria-pressed={showArchived}
        onClick={() => setShowArchived((value) => !value)}
      >
        {t('Show archived')}
      </Button>
      <fieldset
        disabled={save.isPending || activate.isPending}
        className='space-y-4'
      >
        {fields.fields.map((field, i) =>
          !field.archived || showArchived ? (
            <section
              key={field.fieldKey}
              className='bg-card space-y-3 rounded-xl border p-4'
            >
              <div className='flex flex-wrap items-center justify-between gap-3'>
                <div className='min-w-0 space-y-1'>
                  <p className='font-medium break-words'>
                    {field.name || t('New product')}
                  </p>
                  <p className='text-muted-foreground text-sm'>
                    {formatCnyCents(field.price_cents)} · {t('Base API quota')}:{' '}
                    {formatNumber(field.base_quota ?? 0, locale)}
                  </p>
                </div>
                {field.archived ? <Badge>{t('Archived')}</Badge> : null}
                {root &&
                !field.archived &&
                field.published &&
                !form.formState.isDirty ? (
                  <Button
                    type='button'
                    onClick={() => setIssuingProductID(field.id)}
                  >
                    {t('Generate and download')}
                  </Button>
                ) : null}
                {root &&
                !field.archived &&
                (!field.published || form.formState.isDirty) ? (
                  <Button
                    type='button'
                    disabled={activate.isPending}
                    onClick={() => {
                      void form.handleSubmit((values) =>
                        activate.mutate({ values, id: values.products[i].id })
                      )()
                    }}
                  >
                    {t('Save and enable')}
                  </Button>
                ) : null}
              </div>
              <Collapsible defaultOpen={(field.revision ?? 0) === 0}>
                <CollapsibleTrigger
                  render={<Button type='button' variant='outline' />}
                >
                  {t('Edit product')}
                </CollapsibleTrigger>
                <CollapsibleContent keepMounted>
                  <fieldset
                    disabled={field.archived}
                    className='mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2'
                  >
                    <Label>
                      {t('Product name')}
                      <Input {...form.register(`products.${i}.name`)} />
                    </Label>
                    <Label>
                      {t('Price in RMB')}
                      <Input
                        type='number'
                        step='0.01'
                        min='0.01'
                        defaultValue={field.price_cents / 100}
                        onChange={(event) =>
                          form.setValue(
                            `products.${i}.price_cents`,
                            decimalUnits(event.target.value, 100),
                            { shouldValidate: true, shouldDirty: true }
                          )
                        }
                      />
                    </Label>
                    <Label>
                      {t('Base API quota')}
                      <Input
                        type='number'
                        min='1'
                        {...form.register(`products.${i}.base_quota`, {
                          valueAsNumber: true,
                        })}
                      />
                    </Label>
                    <Label>
                      {t('Catfk purchase URL')}
                      <Input
                        {...form.register(`products.${i}.purchase_url`)}
                        onChange={(event) => {
                          form.setValue(
                            `products.${i}.purchase_url`,
                            event.target.value,
                            { shouldDirty: true }
                          )
                          if (trustedPurchaseUrl(event.target.value)) {
                            form.setValue(
                              `products.${i}.delivery_binding`,
                              event.target.value.split('/').at(-1) ?? '',
                              { shouldDirty: true }
                            )
                            form.setValue(
                              `products.${i}.delivery_evidence`,
                              event.target.value,
                              { shouldDirty: true }
                            )
                          }
                        }}
                      />
                    </Label>
                    <Collapsible className='sm:col-span-2'>
                      <CollapsibleTrigger
                        render={<Button type='button' variant='ghost' />}
                      >
                        {t('Advanced settings')}
                      </CollapsibleTrigger>
                      <CollapsibleContent
                        keepMounted
                        className='mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2'
                      >
                        <Label>
                          {t('Product ID')}
                          <Input
                            readOnly
                            {...form.register(`products.${i}.id`)}
                          />
                        </Label>
                        <Label>
                          {t('Sort order')}
                          <Input
                            type='number'
                            {...form.register(`products.${i}.sort`, {
                              valueAsNumber: true,
                            })}
                          />
                        </Label>
                        <Label className='sm:col-span-2'>
                          {t('Description')}
                          <Textarea
                            {...form.register(`products.${i}.description`)}
                          />
                        </Label>
                        <Label>
                          {t('Evidence batch ID')}
                          <Input
                            readOnly
                            value={field.evidence_batch_id ?? 0}
                          />
                        </Label>
                        <Label>
                          {t('Delivery evidence reference')}
                          <Input
                            {...form.register(
                              `products.${i}.delivery_evidence`
                            )}
                          />
                        </Label>
                        <Label>
                          {t('Delivery binding')}
                          <Input
                            {...form.register(`products.${i}.delivery_binding`)}
                          />
                        </Label>
                      </CollapsibleContent>
                    </Collapsible>
                  </fieldset>
                </CollapsibleContent>
              </Collapsible>
              {props.catalog.products[i] ? (
                <ProductOperations
                  product={props.catalog.products[i]}
                  version={props.catalog.version}
                  disabled={form.formState.isDirty || activate.isPending}
                />
              ) : null}
              {(field.revision ?? 0) === 0 ? (
                <Button
                  type='button'
                  variant='outline'
                  onClick={() => setDiscardIndex(i)}
                >
                  {t('Remove unsaved product')}
                </Button>
              ) : null}
              {Object.entries(form.formState.errors.products?.[i] ?? {}).map(
                ([key, error]) =>
                  typeof error === 'object' &&
                  error &&
                  'message' in error &&
                  typeof error.message === 'string' ? (
                    <p
                      role='alert'
                      key={key}
                      className='text-destructive text-sm'
                    >
                      {t(error.message)}
                    </p>
                  ) : null
              )}
            </section>
          ) : null
        )}
        <Button
          type='button'
          variant='outline'
          onClick={() =>
            fields.append({
              id: `credit-${crypto.randomUUID()}`,
              name: '',
              description: '',
              price_cents: 100,
              credit_cents: 100,
              purchase_url: '',
              published: false,
              delivery_verified: false,
              sort: fields.fields.length,
              currency: 'CNY',
              state: 'draft',
              revision: 0,
              base_quota: 0,
              evidence_batch_id: 0,
              delivery_evidence: '',
              delivery_binding: '',
            })
          }
        >
          {t('Add product')}
        </Button>
        <Button type='submit' className='ml-2'>
          {t('Save drafts')}
        </Button>
      </fieldset>
      {form.formState.errors.products?.root?.message ? (
        <p role='alert'>{t(form.formState.errors.products.root.message)}</p>
      ) : null}
      {save.isSuccess ? <p role='status'>{t('Saved successfully')}</p> : null}
      {save.isError ? (
        <ErrorState description={t(getServerErrorMessage(save.error))} />
      ) : null}
      <SecureVerificationDialog {...verification.dialogProps} />
      <RedemptionsBatchCreateDialog
        open={issuingProductID !== null}
        productID={issuingProductID ?? undefined}
        maxCount={100}
        onOpenChange={(open) => {
          if (!open) setIssuingProductID(null)
        }}
        onCreated={(batchID, _count, downloaded) => {
          setIssuingProductID(null)
          if (!downloaded) setExportBatchID(batchID)
        }}
      />
      {exportBatchID !== null ? (
        <RedemptionsBatchExportDialog
          batchId={exportBatchID}
          onClose={() => setExportBatchID(null)}
        />
      ) : null}
      <ConfirmDialog
        open={discardIndex !== null}
        onOpenChange={(open) => {
          if (!open) setDiscardIndex(null)
        }}
        title={t('Remove unsaved product')}
        desc={t('Remove this unsaved product from the form?')}
        destructive
        handleConfirm={() => {
          if (discardIndex !== null) fields.remove(discardIndex)
          setDiscardIndex(null)
        }}
      />
    </form>
  )
}

export function CatalogEditor() {
  const { t } = useTranslation()
  const [activationError, setActivationError] = useState('')
  const query = useQuery({
    queryKey: ['credit-catalog', 'admin'],
    queryFn: getAdminCatalog,
  })
  return (
    <SettingsSection title={t('API credit products')}>
      {activationError ? <ErrorState description={activationError} /> : null}
      {query.isPending ? <p>{t('Loading...')}</p> : null}
      {query.isError ? <ErrorState onRetry={() => query.refetch()} /> : null}
      {query.isSuccess ? (
        <CatalogForm
          key={query.data.version}
          catalog={query.data}
          onActivationError={setActivationError}
        />
      ) : null}
    </SettingsSection>
  )
}
