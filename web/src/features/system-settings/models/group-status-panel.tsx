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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

type GroupStatus = { name: string; enabled: boolean }
const queryKey = ['group-statuses']

export function GroupStatusPanel() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [search, setSearch] = useState('')
  const [statusFilter, setStatusFilter] = useState<
    'all' | 'enabled' | 'disabled'
  >('all')
  const [confirm, setConfirm] = useState<GroupStatus | null>(null)
  const groups = useQuery({
    queryKey,
    queryFn: async () => {
      const response = await api.get('/api/group/statuses')
      requireServerSuccess(response.data)
      return response.data.data as GroupStatus[]
    },
  })
  const mutation = useMutation({
    mutationFn: async (group: GroupStatus) => {
      const response = await api.put('/api/group/status', {
        name: group.name,
        enabled: !group.enabled,
      })
      requireServerSuccess(response.data)
    },
    onSuccess: async () => {
      setConfirm(null)
      await client.invalidateQueries({ queryKey })
    },
  })

  return (
    <section
      className='space-y-3 rounded-lg border p-4'
      aria-label={t('Group availability')}
    >
      <h3 className='font-medium'>{t('Group availability')}</h3>
      <div className='flex flex-wrap gap-2'>
        <Input
          aria-label={t('Search groups')}
          placeholder={t('Search groups')}
          value={search}
          onChange={(event) => setSearch(event.target.value)}
        />
        <Button
          type='button'
          variant='outline'
          aria-pressed={statusFilter === 'disabled'}
          onClick={() =>
            setStatusFilter(statusFilter === 'disabled' ? 'all' : 'disabled')
          }
        >
          {t('Disabled only')}
        </Button>
        <Button
          type='button'
          variant='outline'
          aria-pressed={statusFilter === 'enabled'}
          onClick={() =>
            setStatusFilter(statusFilter === 'enabled' ? 'all' : 'enabled')
          }
        >
          {t('Enabled')}
        </Button>
      </div>
      {groups.isError && <ErrorState />}
      {groups.isPending && <p role='status'>{t('Loading...')}</p>}
      {groups.data
        ?.filter(
          (group) =>
            group.name.toLowerCase().includes(search.toLowerCase()) &&
            (statusFilter === 'all' ||
              group.enabled === (statusFilter === 'enabled'))
        )
        .map((group) => (
          <div
            key={group.name}
            className='flex items-center justify-between gap-3 border-b py-2'
          >
            <span className='min-w-0 break-all'>{group.name}</span>
            <span>{t(group.enabled ? 'Enabled' : 'Disabled')}</span>
            <Switch
              aria-label={group.name}
              checked={group.enabled}
              disabled={mutation.isPending}
              onCheckedChange={() =>
                group.enabled ? setConfirm(group) : mutation.mutate(group)
              }
            />
          </div>
        ))}
      <ConfirmDialog
        open={confirm !== null}
        onOpenChange={(open) => {
          if (!open && !mutation.isPending) setConfirm(null)
        }}
        title={t('Disable group')}
        desc={t(
          'Disabling blocks all users, including existing API keys. Active requests finish normally.'
        )}
        destructive
        isLoading={mutation.isPending}
        handleConfirm={() => {
          if (confirm) mutation.mutate(confirm)
        }}
      />
    </section>
  )
}
