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
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatCnyCents, parseCnyToCents } from '@/features/wallet/lib/money'
import { handleServerError } from '@/lib/handle-server-error'
import { isValidStoreUrl } from '@/lib/store-url'

import { useUpdateOption } from '../hooks/use-update-option'
import {
  createStoreProduct,
  deleteStoreProduct,
  getStoreProducts,
  updateStoreProduct,
  type StoreProduct,
} from '../store-admin-api'

interface StoreSettingsSectionProps {
  defaultValues: { storeRechargeEnabled: boolean }
}

interface ProductFormState {
  id: number
  name: string
  faceValue: string
  storeUrl: string
  enabled: boolean
  sort: string
  description: string
}

const EMPTY_PRODUCT: ProductFormState = {
  id: 0,
  name: '',
  faceValue: '',
  storeUrl: '',
  enabled: true,
  sort: '0',
  description: '',
}

/**
 * Store recharge settings: the global feature flag plus the product list.
 * Managing products/batches stays available while the flag is off so
 * admins can prepare everything before enabling the feature.
 */
export function StoreSettingsSection(props: StoreSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [flagEnabled, setFlagEnabled] = useState(
    props.defaultValues.storeRechargeEnabled
  )
  const [products, setProducts] = useState<StoreProduct[]>([])
  const [loading, setLoading] = useState(true)
  const [formOpen, setFormOpen] = useState(false)
  const [form, setForm] = useState<ProductFormState>(EMPTY_PRODUCT)
  const [deleteTarget, setDeleteTarget] = useState<StoreProduct | null>(null)
  const [saving, setSaving] = useState(false)

  const loadProducts = useCallback(async () => {
    try {
      setLoading(true)
      setProducts(await getStoreProducts())
    } catch (error) {
      handleServerError(error)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadProducts()
  }, [loadProducts])

  const handleFlagChange = (checked: boolean) => {
    setFlagEnabled(checked)
    updateOption.mutate({
      key: 'StoreRechargeEnabled',
      value: checked ? 'true' : 'false',
    })
  }

  const openCreate = () => {
    setForm(EMPTY_PRODUCT)
    setFormOpen(true)
  }

  const openEdit = (product: StoreProduct) => {
    setForm({
      id: product.id,
      name: product.name,
      faceValue: (product.face_value_cents / 100).toFixed(2),
      storeUrl: product.store_url,
      enabled: product.enabled,
      sort: String(product.sort),
      description: product.description,
    })
    setFormOpen(true)
  }

  const handleSave = async () => {
    const faceValueCents = parseCnyToCents(form.faceValue)
    if (!form.name.trim()) {
      toast.error(t('Product name is required'))
      return
    }
    if (faceValueCents === null) {
      toast.error(t('Enter a valid CNY amount with up to 2 decimals'))
      return
    }
    if (!isValidStoreUrl(form.storeUrl)) {
      toast.error(t('Store URL must be a valid https link'))
      return
    }
    const payload = {
      name: form.name.trim(),
      face_value_cents: faceValueCents,
      store_url: form.storeUrl.trim(),
      enabled: form.enabled,
      sort: Number.parseInt(form.sort, 10) || 0,
      description: form.description.trim(),
    }
    try {
      setSaving(true)
      const response = form.id
        ? await updateStoreProduct({ ...payload, id: form.id })
        : await createStoreProduct(payload)
      if (response.success) {
        toast.success(t('Product saved'))
        setFormOpen(false)
        await loadProducts()
      } else {
        handleServerError(response, t('Failed to save product'))
      }
    } catch (error) {
      handleServerError(error, t('Failed to save product'))
    } finally {
      setSaving(false)
    }
  }

  const handleDelete = async () => {
    if (!deleteTarget) return
    try {
      const response = await deleteStoreProduct(deleteTarget.id)
      if (response.success) {
        toast.success(t('Product deleted'))
        setDeleteTarget(null)
        await loadProducts()
      } else {
        handleServerError(response, t('Failed to delete product'))
      }
    } catch (error) {
      handleServerError(error, t('Failed to delete product'))
    }
  }

  return (
    <div className='space-y-6'>
      <p>
        {t(
          'Legacy products are frozen. Manage new products through API credit products.'
        )}
      </p>
      <div className='rounded-lg border p-4'>
        <div className='flex items-start justify-between gap-4'>
          <div className='space-y-1'>
            <Label className='text-sm font-medium'>{t('Store Recharge')}</Label>
            <p className='text-muted-foreground text-xs'>
              {t(
                'When off, users cannot redeem store codes and the store products are hidden. Admin configuration stays available.'
              )}
            </p>
          </div>
          <Switch
            checked={flagEnabled}
            onCheckedChange={handleFlagChange}
            disabled={updateOption.isPending}
          />
        </div>
      </div>

      <div className='space-y-3'>
        <div className='flex items-center justify-between'>
          <h4 className='text-sm font-medium'>{t('Store Products')}</h4>
          <Button disabled size='sm' variant='outline' onClick={openCreate}>
            {t('Add Product')}
          </Button>
        </div>
        <div className='overflow-x-auto rounded-lg border'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Name')}</TableHead>
                <TableHead>{t('Face Value')}</TableHead>
                <TableHead>{t('Store URL')}</TableHead>
                <TableHead>{t('Enabled')}</TableHead>
                <TableHead>{t('Sort')}</TableHead>
                <TableHead className='text-right'>{t('Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {!loading && products.length === 0 && (
                <TableRow>
                  <TableCell
                    colSpan={6}
                    className='text-muted-foreground py-8 text-center text-sm'
                  >
                    {t('No store products configured yet')}
                  </TableCell>
                </TableRow>
              )}
              {products.map((product) => (
                <TableRow key={product.id}>
                  <TableCell className='font-medium'>{product.name}</TableCell>
                  <TableCell className='tabular-nums'>
                    {formatCnyCents(product.face_value_cents)}
                  </TableCell>
                  <TableCell className='max-w-[240px] truncate font-mono text-xs'>
                    {product.store_url}
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant={product.enabled ? 'secondary' : 'destructive'}
                    >
                      {product.enabled ? t('Enabled') : t('Disabled')}
                    </Badge>
                  </TableCell>
                  <TableCell className='tabular-nums'>{product.sort}</TableCell>
                  <TableCell className='text-right'>
                    <div className='flex justify-end gap-1'>
                      <Button
                        variant='ghost'
                        size='sm'
                        className='h-8 px-2'
                        disabled
                        onClick={() => openEdit(product)}
                      >
                        {t('Edit')}
                      </Button>
                      <Button
                        variant='ghost'
                        size='sm'
                        className='text-destructive h-8 px-2'
                        disabled
                        onClick={() => setDeleteTarget(product)}
                      >
                        {t('Delete')}
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
        <p className='text-muted-foreground text-xs'>
          {t(
            'Products are jump links only; the credited value of a code is decided by the backend when the batch is created.'
          )}
        </p>
      </div>

      <Dialog open={formOpen} onOpenChange={setFormOpen}>
        <DialogContent className='sm:max-w-md'>
          <DialogHeader>
            <DialogTitle>
              {form.id ? t('Edit Product') : t('Add Product')}
            </DialogTitle>
            <DialogDescription>
              {t(
                'Configure a store product jump link shown on the wallet page.'
              )}
            </DialogDescription>
          </DialogHeader>
          <div className='space-y-4'>
            <div className='space-y-1.5'>
              <Label htmlFor='product-name'>{t('Name')}</Label>
              <Input
                id='product-name'
                value={form.name}
                onChange={(event) =>
                  setForm({ ...form, name: event.target.value })
                }
                maxLength={64}
              />
            </div>
            <div className='grid grid-cols-2 gap-3'>
              <div className='space-y-1.5'>
                <Label htmlFor='product-face'>{t('Face Value (CNY)')}</Label>
                <Input
                  id='product-face'
                  value={form.faceValue}
                  onChange={(event) =>
                    setForm({ ...form, faceValue: event.target.value })
                  }
                  placeholder='100.00'
                  inputMode='decimal'
                />
              </div>
              <div className='space-y-1.5'>
                <Label htmlFor='product-sort'>{t('Sort')}</Label>
                <Input
                  id='product-sort'
                  value={form.sort}
                  onChange={(event) =>
                    setForm({ ...form, sort: event.target.value })
                  }
                  inputMode='numeric'
                />
              </div>
            </div>
            <div className='space-y-1.5'>
              <Label htmlFor='product-url'>{t('Store URL')}</Label>
              <Input
                id='product-url'
                value={form.storeUrl}
                onChange={(event) =>
                  setForm({ ...form, storeUrl: event.target.value })
                }
                placeholder='https://example.com/product/xxx'
              />
            </div>
            <div className='space-y-1.5'>
              <Label htmlFor='product-description'>{t('Description')}</Label>
              <Input
                id='product-description'
                value={form.description}
                onChange={(event) =>
                  setForm({ ...form, description: event.target.value })
                }
                maxLength={255}
              />
            </div>
            <div className='flex items-center gap-2'>
              <Switch
                id='product-enabled'
                checked={form.enabled}
                onCheckedChange={(checked) =>
                  setForm({ ...form, enabled: checked })
                }
              />
              <Label htmlFor='product-enabled'>{t('Enabled')}</Label>
            </div>
          </div>
          <DialogFooter>
            <Button
              variant='outline'
              onClick={() => setFormOpen(false)}
              disabled={saving}
            >
              {t('Cancel')}
            </Button>
            <Button onClick={() => void handleSave()} disabled={saving}>
              {t('Save')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
      >
        <DialogContent className='sm:max-w-md'>
          <DialogHeader>
            <DialogTitle>{t('Delete Product?')}</DialogTitle>
            <DialogDescription>
              {t(
                'This only removes the store display configuration. Existing redemption codes and purchased cards are not affected.'
              )}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant='outline'
              onClick={() => setDeleteTarget(null)}
              disabled={saving}
            >
              {t('Cancel')}
            </Button>
            <Button variant='destructive' onClick={() => void handleDelete()}>
              {t('Delete')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
