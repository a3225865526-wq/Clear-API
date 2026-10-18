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
import { RefreshCcw } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { handleServerError } from '@/lib/handle-server-error'

import {
  applyModelPricingCleanup,
  getModelPricingCleanup,
  invalidateModelPricing,
  type ModelPricingCleanup,
  type ModelPricingCleanupRecord,
} from './api'

const ACTION_LABELS: Record<ModelPricingCleanupRecord['action'], string> = {
  keep: 'No cleanup needed',
  reset_to_defaults: 'Restore built-in price',
  use_builtin: 'Use built-in upstream price',
}

export function ModelPricingCleanupDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [selected, setSelected] = useState<string[]>([])
  const [plan, setPlan] = useState<ModelPricingCleanup | null>(null)
  const query = useQuery({
    queryKey: ['model-pricing-cleanup'],
    queryFn: getModelPricingCleanup,
    enabled: props.open,
    refetchOnWindowFocus: false,
    meta: { errorToast: false },
  })

  useEffect(() => {
    if (!query.data) return
    setPlan(query.data)
    setSelected([])
  }, [query.data])

  const apply = useMutation({
    mutationFn: applyModelPricingCleanup,
    meta: { errorToast: false },
    onSuccess: async (report) => {
      setPlan(report)
      setSelected([])
      toast.success(
        t('Cleaned up pricing for {{count}} models', {
          count: report.cleared?.length ?? 0,
        })
      )
      await invalidateModelPricing(queryClient)
      await queryClient.invalidateQueries({ queryKey: ['model-pricing-cleanup'] })
    },
    onError: (error: Error) =>
      handleServerError(error, t('Failed to clean up model pricing')),
  })

  const selectable = (record: ModelPricingCleanupRecord) =>
    record.action !== 'keep'
  const records = plan?.records ?? []
  const candidates = records.filter(selectable)
  const allSelected =
    candidates.length > 0 &&
    candidates.every((record) => selected.includes(record.model_name))

  const columns: StaticDataTableColumn<ModelPricingCleanupRecord>[] = [
    {
      id: 'select',
      header: (
        <Checkbox
          checked={allSelected}
          aria-label={t('Select all models with pricing to clean up')}
          onCheckedChange={(checked) =>
            setSelected(checked === true ? candidates.map((r) => r.model_name) : [])
          }
        />
      ),
      cell: (record) => (
        <Checkbox
          checked={selected.includes(record.model_name)}
          disabled={!selectable(record)}
          aria-label={t('Select {{model}}', { model: record.model_name })}
          onCheckedChange={(checked) =>
            setSelected((previous) =>
              checked === true
                ? [...previous, record.model_name]
                : previous.filter((name) => name !== record.model_name)
            )
          }
        />
      ),
    },
    {
      id: 'model',
      header: t('Model'),
      cell: (record) => (
        <div className='min-w-0'>
          <p className='truncate font-medium'>{record.model_name}</p>
          <p className='text-muted-foreground text-xs'>
            {record.configured.length
              ? t('Site pricing: {{fields}}', {
                  fields: record.configured.join(', '),
                })
              : t('No site pricing stored')}
          </p>
        </div>
      ),
    },
    {
      id: 'action',
      header: t('Cleanup'),
      cell: (record) => (
        <div className='space-y-1 text-xs'>
          <p>{t(ACTION_LABELS[record.action])}</p>
          {record.unpriced_after_reset && (
            <p role='status' className='text-amber-600 dark:text-amber-400'>
              {t('No built-in price. This model becomes unpriced after reset.')}
            </p>
          )}
        </div>
      ),
    },
  ]

  const footer = (
    <>
      <Button
        variant='outline'
        onClick={() => void query.refetch()}
        disabled={query.isFetching || apply.isPending}
      >
        <RefreshCcw className='size-4' aria-hidden />
        {t('Reload pricing')}
      </Button>
      <Button
        onClick={() => {
          if (!plan) return
          const changes = plan.plans
            .filter((entry) => selected.includes(entry.model_name))
            .map((entry) => ({
              model_name: entry.model_name,
              expected_version: entry.version,
              pricing: entry.pricing,
            }))
          apply.mutate({ snapshot: plan.snapshot, changes })
        }}
        disabled={!selected.length || apply.isPending}
      >
        {apply.isPending
          ? t('Cleaning up...')
          : t('Clean up {{count}} models', { count: selected.length })}
      </Button>
    </>
  )

  if (query.isError) {
    return (
      <Dialog
        open={props.open}
        onOpenChange={props.onOpenChange}
        title={t('Clean up model pricing')}
      >
        <ErrorState
          description={query.error.message}
          onRetry={() => void query.refetch()}
        />
      </Dialog>
    )
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Clean up model pricing')}
      description={t(
        'Remove site pricing records so the built-in price applies. Model definitions, channels, groups, API keys, users and usage history are not affected.'
      )}
      contentClassName='sm:max-w-4xl'
      contentHeight='min(70vh, 720px)'
      bodyClassName='h-full'
      footer={footer}
    >
      {!plan || query.isLoading ? (
        <LoadingState />
      ) : (
        <div className='space-y-3'>
          {apply.isError && (
            <div role='alert' className='text-destructive text-sm'>
              <p>{apply.error.message}</p>
              <p className='mt-1'>
                {t(
                  'Pricing changed since this plan was prepared. Reload before cleaning up.'
                )}
              </p>
            </div>
          )}
          <p className='text-muted-foreground text-sm' role='status'>
            {t('{{count}} models have site pricing to clean up', {
              count: candidates.length,
            })}
          </p>
          <StaticDataTable
            columns={columns}
            data={records}
            getRowKey={(record) => record.model_name}
            tableProps={{ 'aria-label': t('Model pricing cleanup') }}
            emptyContent={t('No enabled models were found')}
          />
        </div>
      )}
    </Dialog>
  )
}
