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
import { JsonCodeEditor } from '@/components/json-code-editor'
import { Button } from '@/components/ui/button'
import { api } from '@/lib/api'
import {
  getServerErrorMessage,
  requireServerSuccess,
} from '@/lib/server-error-message'

export function MonitorExecutionSettings(props: {
  monitorId?: number
  policy?: object
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [draft, setDraft] = useState<string | null>(null)
  const [confirm, setConfirm] = useState(false)
  const isContract = props.monitorId !== undefined
  const path = isContract
    ? `/api/console/monitor-admin/${props.monitorId}/cost-contract`
    : '/api/console/monitor-admin/policy'
  const contract = useQuery({
    queryKey: ['monitor-cost-contract', props.monitorId],
    enabled: isContract,
    retry: false,
    refetchOnWindowFocus: false,
    meta: { errorToast: false },
    queryFn: async () => requireServerSuccess((await api.get(path)).data).data,
  })
  const value =
    draft ??
    JSON.stringify(isContract ? (contract.data ?? {}) : props.policy, null, 2)
  const mutation = useMutation({
    mutationFn: async () => {
      const configuration: unknown = JSON.parse(value)
      if (
        !configuration ||
        typeof configuration !== 'object' ||
        Array.isArray(configuration)
      ) {
        throw new Error(t('Invalid value'))
      }
      return requireServerSuccess(
        (
          await api.put(
            path,
            {
              confirm: true,
              [isContract ? 'contract' : 'policy']: configuration,
            },
            { headers: { 'X-ClearAPI-Monitor': '1' } }
          )
        ).data
      )
    },
    onSuccess: async () => {
      setConfirm(false)
      setDraft(null)
      await Promise.all([
        client.invalidateQueries({ queryKey: ['monitor-admin-catalog'] }),
        client.invalidateQueries({ queryKey: ['monitor-allowlist-catalog'] }),
        client.invalidateQueries({
          queryKey: ['monitor-cost-contract', props.monitorId],
        }),
      ])
    },
  })
  const label = t(
    isContract ? 'Upstream cost contract' : 'Global monitoring budget policy'
  )
  return (
    <details className='space-y-3 rounded border p-3'>
      <summary className='cursor-pointer font-medium'>{label}</summary>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Use an authorized supplier contract and explicit budgets. Saving a contract does not enable monitoring.'
        )}
      </p>
      {contract.isError && (
        <p role='status'>
          {t(
            'No verified contract loaded. Supply an authorized contract before saving.'
          )}
        </p>
      )}
      <JsonCodeEditor
        value={value}
        onChange={setDraft}
        name={label}
        ariaLabel={label}
      />
      <Button
        type='button'
        disabled={mutation.isPending || contract.isFetching}
        onClick={() => setConfirm(true)}
      >
        {t('Save')}
      </Button>
      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        title={label}
        desc={t(
          isContract
            ? 'Review the authorization and budget before saving. This action does not run a probe.'
            : 'Enabling this policy permits scheduled probes for eligible monitors within the saved budgets.'
        )}
        isLoading={mutation.isPending}
        handleConfirm={() => mutation.mutate()}
      >
        {mutation.isError && (
          <p role='alert'>{t(getServerErrorMessage(mutation.error))}</p>
        )}
      </ConfirmDialog>
    </details>
  )
}
