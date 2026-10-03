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
import { handleServerError } from '@/lib/handle-server-error'

import {
  formatBasisPoints,
  formatCnyCents,
  parseCnyToCents,
  parsePercentToBasisPoints,
} from '@/features/wallet/lib/money'
import {
  createVipLevel,
  deleteVipLevel,
  getVipLevels,
  updateVipLevel,
  type VipLevel,
} from '../store-admin-api'

interface LevelFormState {
  id: number
  name: string
  threshold: string
  bonusPercent: string
  enabled: boolean
  sort: string
}

const EMPTY_LEVEL: LevelFormState = {
  id: 0,
  name: '',
  threshold: '',
  bonusPercent: '0',
  enabled: true,
  sort: '0',
}

/**
 * VIP level management. Thresholds (CNY) and bonus percentages are converted
 * to integer cents / basis points at the request boundary; the backend stays
 * the final validation authority (duplicate thresholds etc.).
 */
export function VipLevelsSettingsSection() {
  const { t } = useTranslation()
  const [levels, setLevels] = useState<VipLevel[]>([])
  const [loading, setLoading] = useState(true)
  const [formOpen, setFormOpen] = useState(false)
  const [form, setForm] = useState<LevelFormState>(EMPTY_LEVEL)
  const [deleteTarget, setDeleteTarget] = useState<VipLevel | null>(null)
  const [saving, setSaving] = useState(false)

  const loadLevels = useCallback(async () => {
    try {
      setLoading(true)
      setLevels(await getVipLevels())
    } catch (error) {
      handleServerError(error)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadLevels()
  }, [loadLevels])

  const openCreate = () => {
    setForm(EMPTY_LEVEL)
    setFormOpen(true)
  }

  const openEdit = (level: VipLevel) => {
    setForm({
      id: level.id,
      name: level.name,
      threshold: (level.threshold_cents / 100).toFixed(2),
      bonusPercent: (level.bonus_basis_points / 100).toFixed(2),
      enabled: level.enabled,
      sort: String(level.sort),
    })
    setFormOpen(true)
  }

  const handleSave = async () => {
    const thresholdCents = parseCnyToCents(form.threshold)
    if (!form.name.trim()) {
      toast.error(t('Level name is required'))
      return
    }
    if (thresholdCents === null) {
      toast.error(t('Enter a valid CNY amount with up to 2 decimals'))
      return
    }
    const bonusBasisPoints = parsePercentToBasisPoints(form.bonusPercent)
    if (bonusBasisPoints === null) {
      toast.error(t('Bonus must be between 0 and 100 with up to 2 decimals'))
      return
    }
    const payload = {
      name: form.name.trim(),
      threshold_cents: thresholdCents,
      bonus_basis_points: bonusBasisPoints,
      enabled: form.enabled,
      sort: Number.parseInt(form.sort, 10) || 0,
    }
    try {
      setSaving(true)
      const response = form.id
        ? await updateVipLevel({ ...payload, id: form.id })
        : await createVipLevel(payload)
      if (response.success) {
        toast.success(t('Level saved'))
        setFormOpen(false)
        await loadLevels()
      } else {
        handleServerError(response, t('Failed to save level'))
      }
    } catch (error) {
      handleServerError(error, t('Failed to save level'))
    } finally {
      setSaving(false)
    }
  }

  const handleDelete = async () => {
    if (!deleteTarget) return
    try {
      const response = await deleteVipLevel(deleteTarget.id)
      if (response.success) {
        toast.success(t('Level deleted'))
        setDeleteTarget(null)
        await loadLevels()
      } else {
        handleServerError(response, t('Failed to delete level'))
      }
    } catch (error) {
      handleServerError(error, t('Failed to delete level'))
    }
  }

  return (
    <div className='space-y-3'>
      <div className='flex items-center justify-between'>
        <p className='text-muted-foreground max-w-xl text-sm'>
          {t(
            'Levels are derived from the total genuinely recharged CNY amount; bonuses apply from the next recharge after a level-up.'
          )}
        </p>
        <Button size='sm' variant='outline' onClick={openCreate}>
          {t('Add Level')}
        </Button>
      </div>
      <div className='overflow-x-auto rounded-lg border'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Level Name')}</TableHead>
              <TableHead>{t('Threshold (CNY)')}</TableHead>
              <TableHead>{t('Recharge Bonus')}</TableHead>
              <TableHead>{t('Enabled')}</TableHead>
              <TableHead>{t('Sort')}</TableHead>
              <TableHead className='text-right'>{t('Actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {!loading && levels.length === 0 && (
              <TableRow>
                <TableCell
                  colSpan={6}
                  className='text-muted-foreground py-8 text-center text-sm'
                >
                  {t('No levels configured')}
                </TableCell>
              </TableRow>
            )}
            {levels.map((level) => (
              <TableRow key={level.id}>
                <TableCell className='font-medium'>{level.name}</TableCell>
                <TableCell className='tabular-nums'>
                  {formatCnyCents(level.threshold_cents)}
                </TableCell>
                <TableCell className='tabular-nums'>
                  {formatBasisPoints(level.bonus_basis_points)}
                </TableCell>
                <TableCell>
                  <Badge variant={level.enabled ? 'secondary' : 'destructive'}>
                    {level.enabled ? t('Enabled') : t('Disabled')}
                  </Badge>
                </TableCell>
                <TableCell className='tabular-nums'>{level.sort}</TableCell>
                <TableCell className='text-right'>
                  <div className='flex justify-end gap-1'>
                    <Button
                      variant='ghost'
                      size='sm'
                      className='h-8 px-2'
                      onClick={() => openEdit(level)}
                    >
                      {t('Edit')}
                    </Button>
                    <Button
                      variant='ghost'
                      size='sm'
                      className='text-destructive h-8 px-2'
                      onClick={() => setDeleteTarget(level)}
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

      <Dialog open={formOpen} onOpenChange={setFormOpen}>
        <DialogContent className='sm:max-w-md'>
          <DialogHeader>
            <DialogTitle>
              {form.id ? t('Edit Level') : t('Add Level')}
            </DialogTitle>
            <DialogDescription>
              {t(
                'Users reaching the threshold count as this level; the bonus applies to subsequent recharges.'
              )}
            </DialogDescription>
          </DialogHeader>
          <div className='space-y-4'>
            <div className='space-y-1.5'>
              <Label htmlFor='level-name'>{t('Level Name')}</Label>
              <Input
                id='level-name'
                value={form.name}
                onChange={(event) =>
                  setForm({ ...form, name: event.target.value })
                }
                maxLength={32}
              />
            </div>
            <div className='grid grid-cols-2 gap-3'>
              <div className='space-y-1.5'>
                <Label htmlFor='level-threshold'>{t('Threshold (CNY)')}</Label>
                <Input
                  id='level-threshold'
                  value={form.threshold}
                  onChange={(event) =>
                    setForm({ ...form, threshold: event.target.value })
                  }
                  placeholder='500.00'
                  inputMode='decimal'
                />
              </div>
              <div className='space-y-1.5'>
                <Label htmlFor='level-bonus'>{t('Recharge Bonus (%)')}</Label>
                <Input
                  id='level-bonus'
                  value={form.bonusPercent}
                  onChange={(event) =>
                    setForm({ ...form, bonusPercent: event.target.value })
                  }
                  placeholder='2.5'
                  inputMode='decimal'
                />
              </div>
            </div>
            <div className='flex items-center gap-2'>
              <Switch
                id='level-enabled'
                checked={form.enabled}
                onCheckedChange={(checked) =>
                  setForm({ ...form, enabled: checked })
                }
              />
              <Label htmlFor='level-enabled'>{t('Enabled')}</Label>
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
            <DialogTitle>{t('Delete Level?')}</DialogTitle>
            <DialogDescription>
              {t(
                'Users at this level will fall back to the next matching enabled level, or no bonus.'
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
