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
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Skeleton } from '@/components/ui/skeleton'
import { handleServerError } from '@/lib/handle-server-error'

import { getRedemptionBatchCodes } from '../api'
import {
  buildBatchExportContent,
  buildBatchExportFilename,
  downloadTextFile,
  type BatchExportFormat,
} from '../lib/batch-export'

interface RedemptionsBatchExportDialogProps {
  batchId: number
  onClose: () => void
}

/**
 * Export UNUSED codes of a batch. Keys are always fetched live from the
 * server (never taken from in-memory table state). Codes are sensitive:
 * shown only to admins here, never logged or persisted.
 */
export function RedemptionsBatchExportDialog(
  props: RedemptionsBatchExportDialogProps
) {
  const { t } = useTranslation()
  const [format, setFormat] = useState<BatchExportFormat>('txt')
  const [loading, setLoading] = useState(false)
  const [keys, setKeys] = useState<string[] | null>(null)

  const loadKeys = async () => {
    try {
      setLoading(true)
      const response = await getRedemptionBatchCodes(props.batchId)
      if (response.success && response.data) {
        setKeys(response.data)
        return response.data
      }
      handleServerError(response, t('Failed to load batch codes'))
      return null
    } catch (error) {
      handleServerError(error, t('Failed to load batch codes'))
      return null
    } finally {
      setLoading(false)
    }
  }

  const handleExport = async () => {
    const fetched = keys ?? (await loadKeys())
    if (!fetched) return
    const mime =
      format === 'csv'
        ? 'text/csv;charset=utf-8'
        : 'text/plain;charset=utf-8'
    downloadTextFile(
      buildBatchExportFilename(props.batchId, format),
      buildBatchExportContent(fetched, format),
      mime
    )
  }

  const handleCopy = async () => {
    const fetched = keys ?? (await loadKeys())
    if (!fetched) return
    try {
      await navigator.clipboard.writeText(fetched.join('\n'))
      toast.success(
        t('Copied {{count}} codes to clipboard', { count: fetched.length })
      )
    } catch {
      toast.error(t('Copy failed'))
    }
  }

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
      title={t('Export Unused Codes')}
      description={t(
        'Codes are fetched live from the server (unused only). Admin only — do not share publicly.'
      )}
      contentClassName='sm:max-w-md'
      bodyClassName='space-y-4'
      footer={
        <div className='flex gap-2'>
          <Button
            variant='outline'
            onClick={() => void handleCopy()}
            disabled={loading}
          >
            {t('Copy All')}
          </Button>
          <Button onClick={() => void handleExport()} disabled={loading}>
            {loading ? t('Loading...') : t('Export')}
          </Button>
        </div>
      }
    >
      {keys && (
        <div className='text-muted-foreground text-xs'>
          {t('{{count}} unused codes', { count: keys.length })}
        </div>
      )}
      {loading && <Skeleton className='h-4 w-full' />}
      <RadioGroup
        value={format}
        onValueChange={(value) => {
          if (value === 'txt' || value === 'csv') setFormat(value)
        }}
        className='flex flex-wrap gap-x-5 gap-y-3'
      >
        {[
          { value: 'txt', label: t('Save as TXT') },
          { value: 'csv', label: t('Save as CSV') },
        ].map((option) => (
          <div key={option.value} className='flex items-center gap-2'>
            <RadioGroupItem value={option.value} id={`batch-${option.value}`} />
            <Label htmlFor={`batch-${option.value}`}>{option.label}</Label>
          </div>
        ))}
      </RadioGroup>
      <p className='text-muted-foreground text-xs'>
        {t(
          'The CSV contains a single "code" column for direct import into your store.'
        )}
      </p>
    </Dialog>
  )
}
