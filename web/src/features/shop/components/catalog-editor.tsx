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
import { useFieldArray, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { SettingsSection } from '@/features/system-settings/components/settings-section'

import { getAdminCatalog, saveCatalog, type Catalog } from '../api'
import { catalogSchema, decimalUnits } from '../lib/catalog-form'
import { ProductOperations } from './product-operations'

function CatalogForm(props: { catalog: Catalog }) {
  const { t } = useTranslation()
  const client = useQueryClient()
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
  return (
    <form
      onSubmit={form.handleSubmit((values) => save.mutate(values))}
      className='space-y-5'
    >
      <p className='text-muted-foreground text-sm'>
        {t(
          'Publish only after verifying the exact delivered API credit amount and reliable paid code delivery. The existing purchase entry stays available.'
        )}
      </p>
      <fieldset disabled={save.isPending} className='space-y-5'>
        {fields.fields.map((field, i) => (
          <section
            key={field.fieldKey}
            className='bg-card grid grid-cols-1 gap-3 rounded-xl border p-4 sm:grid-cols-2'
          >
            <label className='space-y-1'>
              {t('Product ID')}
              <Input
                readOnly={(props.catalog.products[i]?.revision ?? 0) > 0}
                {...form.register(`products.${i}.id`)}
              />
            </label>
            <label className='space-y-1'>
              {t('Product name')}
              <Input {...form.register(`products.${i}.name`)} />
            </label>
            <label className='space-y-1'>
              {t('Price in RMB')}
              <Input
                type='number'
                step='0.01'
                min='0.01'
                defaultValue={field.price_cents / 100}
                onChange={(e) =>
                  form.setValue(
                    `products.${i}.price_cents`,
                    decimalUnits(e.target.value, 100),
                    { shouldValidate: true, shouldDirty: true }
                  )
                }
              />
            </label>
            <label className='space-y-1 sm:col-span-2'>
              {t('Description')}
              <Textarea {...form.register(`products.${i}.description`)} />
            </label>
            <label className='space-y-1'>
              {t('Catfk purchase URL')}
              <Input {...form.register(`products.${i}.purchase_url`)} />
            </label>
            <label className='space-y-1'>
              {t('Sort order')}
              <Input
                type='number'
                {...form.register(`products.${i}.sort`, {
                  valueAsNumber: true,
                })}
              />
            </label>
            <Label>
              {t('Base API quota')}
              <Input
                type='number'
                {...form.register(`products.${i}.base_quota`, {
                  valueAsNumber: true,
                })}
              />
            </Label>
            <Label>
              {t('Evidence batch ID')}
              <Input
                type='number'
                {...form.register(`products.${i}.evidence_batch_id`, {
                  valueAsNumber: true,
                })}
              />
            </Label>
            <Label>
              {t('Delivery evidence reference')}
              <Input {...form.register(`products.${i}.delivery_evidence`)} />
            </Label>
            <Label>
              {t('Delivery binding')}
              <Input {...form.register(`products.${i}.delivery_binding`)} />
            </Label>
            {props.catalog.products[i] ? (
              <ProductOperations
                product={props.catalog.products[i]}
                version={props.catalog.version}
                disabled={form.formState.isDirty}
              />
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
        ))}
        <Button
          type='button'
          variant='outline'
          disabled={false}
          onClick={() =>
            fields.append({
              id: `credit-new-${fields.fields.length + 1}`,
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
          {t('Save products')}
        </Button>
      </fieldset>
      {form.formState.errors.products?.root?.message ? (
        <p role='alert'>{t(form.formState.errors.products.root.message)}</p>
      ) : null}
      {save.isSuccess ? <p role='status'>{t('Saved successfully')}</p> : null}
      {save.isError ? <ErrorState /> : null}
    </form>
  )
}

export function CatalogEditor() {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['credit-catalog', 'admin'],
    queryFn: getAdminCatalog,
  })
  return (
    <SettingsSection title={t('API credit products')}>
      <p>
        {t(
          'Preparation mode preserves frozen legacy entries. Unified cutover requires separate release acceptance.'
        )}
      </p>
      {query.data?.legacy_products?.map((p) => (
        <p key={p.id}>
          {t('Legacy store product ID')}: {p.id} · {p.name} ·{' '}
          {t('Explicit mapping and review required')}
        </p>
      ))}
      {query.isPending ? <p>{t('Loading...')}</p> : null}
      {query.isError ? <ErrorState onRetry={() => query.refetch()} /> : null}
      {query.isSuccess ? (
        <CatalogForm key={query.data.version} catalog={query.data} />
      ) : null}
    </SettingsSection>
  )
}
