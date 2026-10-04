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
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { handleServerError } from '@/lib/handle-server-error'

import { createPoolAccount } from '../api'
import { POOL_QUERY_KEY } from '../constants'
import type { PoolCatalog } from '../types'

type CreateAccountDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  catalog: PoolCatalog | undefined
}

/**
 * Create dialog. Only non-disabled platform + access type pairs from the
 * catalog are offered; the executor comes from the selected access type.
 */
export function CreateAccountDialog(props: CreateAccountDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [platform, setPlatform] = useState('')
  const [accessType, setAccessType] = useState('')
  const [note, setNote] = useState('')

  const availablePlatforms = useMemo(
    () => (props.catalog?.platforms ?? []).filter((p) => !p.disabled),
    [props.catalog]
  )
  const selectedPlatform = availablePlatforms.find(
    (p) => p.platform === platform
  )
  const accessTypeOptions = useMemo(
    () => (selectedPlatform?.access_types ?? []).filter((a) => !a.disabled),
    [selectedPlatform]
  )
  const selectedAccessType = accessTypeOptions.find(
    (a) => a.type === accessType
  )

  const createMutation = useMutation({
    mutationFn: () =>
      createPoolAccount({
        platform,
        access_type: accessType,
        executor_id: selectedAccessType?.executor_id || undefined,
        note: note.trim() || undefined,
      }),
    onSuccess: () => {
      toast.success(t('Account created'))
      void queryClient.invalidateQueries({ queryKey: [...POOL_QUERY_KEY] })
      props.onOpenChange(false)
    },
    onError: (error) => handleServerError(error),
  })

  const reset = () => {
    setPlatform('')
    setAccessType('')
    setNote('')
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={(open) => {
        props.onOpenChange(open)
        if (!open) reset()
      }}
      title={t('Create pool account')}
      description={t(
        'The account starts in authorization state until its first authorization completes.'
      )}
      footer={
        <>
          <Button
            variant='outline'
            onClick={() => {
              props.onOpenChange(false)
              reset()
            }}
          >
            {t('Cancel')}
          </Button>
          <Button
            disabled={!platform || !accessType || createMutation.isPending}
            onClick={() => createMutation.mutate()}
          >
            {t('Create')}
          </Button>
        </>
      }
    >
      <div className='space-y-4'>
        <div className='space-y-1.5'>
          <Label htmlFor='pool-create-platform'>{t('Platform')}</Label>
          <Select
            items={availablePlatforms.map((p) => ({
              value: p.platform,
              label: p.label,
            }))}
            value={platform}
            onValueChange={(value) => {
              setPlatform(String(value))
              setAccessType('')
            }}
          >
            <SelectTrigger id='pool-create-platform' className='w-full'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                {availablePlatforms.map((p) => (
                  <SelectItem key={p.platform} value={p.platform}>
                    {p.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </div>
        <div className='space-y-1.5'>
          <Label htmlFor='pool-create-access-type'>{t('Access type')}</Label>
          <Select
            items={accessTypeOptions.map((a) => ({
              value: a.type,
              label: a.label,
            }))}
            value={accessType}
            onValueChange={(value) => setAccessType(String(value))}
            disabled={!platform}
          >
            <SelectTrigger id='pool-create-access-type' className='w-full'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                {accessTypeOptions.map((a) => (
                  <SelectItem key={a.type} value={a.type}>
                    {a.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </div>
        <div className='space-y-1.5'>
          <Label htmlFor='pool-create-note'>{t('Note')}</Label>
          <Input
            id='pool-create-note'
            value={note}
            maxLength={200}
            onChange={(event) => setNote(event.target.value)}
            placeholder={t('Optional remark for this account')}
          />
        </div>
      </div>
    </Dialog>
  )
}
