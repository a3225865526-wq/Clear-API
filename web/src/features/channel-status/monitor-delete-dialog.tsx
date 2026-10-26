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
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { api } from '@/lib/api'
import {
  getServerErrorMessage,
  requireServerSuccess,
} from '@/lib/server-error-message'

import type { Monitor } from './lib/monitor-admin-schema'

export function MonitorDeleteDialog(props: {
  monitor: Monitor
  onClose: () => void
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const remove = useMutation({
    mutationFn: async () =>
      requireServerSuccess(
        (
          await api.delete(`/api/console/monitor-admin/${props.monitor.id}`, {
            data: { confirm: true },
            headers: { 'X-ClearAPI-Monitor': '1' },
          })
        ).data
      ),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ['monitor-admin-catalog'] })
      void client.invalidateQueries({ queryKey: ['monitor-allowlist-catalog'] })
      void client.invalidateQueries({ queryKey: ['console-monitoring'] })
      toast.success(t('Deleted successfully'))
      props.onClose()
    },
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(open) => {
        if (!open && !remove.isPending) props.onClose()
      }}
      title={t('Delete monitoring entry')}
      desc={t(
        'Delete archived monitoring entry "{{name}}" (ID {{id}})? The upstream channel and historical records will be retained.',
        { name: props.monitor.display_name, id: props.monitor.id }
      )}
      confirmText={t('Delete')}
      destructive
      isLoading={remove.isPending}
      handleConfirm={() => remove.mutate()}
    >
      {remove.isError && (
        <p role='alert'>{getServerErrorMessage(remove.error)}</p>
      )}
    </ConfirmDialog>
  )
}
