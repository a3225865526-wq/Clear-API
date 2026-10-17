import { useQueryClient } from '@tanstack/react-query'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'

import { importPoolCredentials } from '../api'
import { POOL_QUERY_KEY } from '../constants'
import type { PoolImportResult } from '../types'

const MAX_FILES = 10
const MAX_FILE_BYTES = 256 * 1024
const MAX_TOTAL_BYTES = 1024 * 1024

type PendingFile = {
  file: File
  error?: string
}

function preflight(file: File): string | undefined {
  if (!file.name.toLowerCase().endsWith('.json')) {
    return 'Only .json files are accepted.'
  }
  if (file.size <= 0) return 'The file is empty.'
  if (file.size > MAX_FILE_BYTES) return 'Each file must be 256 KiB or smaller.'
  return undefined
}

function resultVariant(status: PoolImportResult['status']) {
  if (status === 'imported') return 'default'
  if (status === 'duplicate') return 'secondary'
  return 'destructive'
}

function statusLabel(status: PoolImportResult['status']) {
  switch (status) {
    case 'imported':
      return 'Imported'
    case 'duplicate':
      return 'Duplicate'
    case 'rejected':
      return 'Rejected'
    case 'failed':
      return 'Failed'
  }
}

export function CredentialImportPanel() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const inputRef = useRef<HTMLInputElement>(null)
  const [files, setFiles] = useState<PendingFile[]>([])
  const [results, setResults] = useState<PoolImportResult[]>([])
  const [busy, setBusy] = useState(false)
  const [dragging, setDragging] = useState(false)
  const [generalError, setGeneralError] = useState('')

  const choose = (incoming: File[]) => {
    setGeneralError('')
    setResults([])
    const next: PendingFile[] = []
    let total = 0
    for (const file of incoming.slice(0, MAX_FILES)) {
      const error = preflight(file)
      total += file.size
      next.push({
        file,
        error:
          error ??
          (total > MAX_TOTAL_BYTES
            ? 'Batch must be 1 MiB or smaller.'
            : undefined),
      })
    }
    if (incoming.length > MAX_FILES) {
      setGeneralError(t('Up to 10 files can be imported at once.'))
    }
    setFiles(next)
  }

  const retryFiles = results
    .filter((r) => r.status === 'failed' || r.status === 'rejected')
    .map((r) => files.find((f) => f.file.name === r.file_name))
    .filter((f): f is PendingFile => Boolean(f) && !f?.error)

  const upload = async (batch: PendingFile[]) => {
    const valid = batch.filter((item) => !item.error)
    if (!valid.length) {
      setGeneralError(t('No valid files are ready to import.'))
      return
    }
    setBusy(true)
    setGeneralError('')
    try {
      const response = await importPoolCredentials(
        valid.map((item) => item.file)
      )
      setResults(response.results)
      if (response.imported > 0) {
        toast.success(
          t('Imported {{count}} credential file(s).', {
            count: response.imported,
          })
        )
      }
      if (response.duplicate > 0 || response.rejected > 0) {
        toast.warning(
          t('Some files were not imported. Review the results below.')
        )
      }
      void queryClient.invalidateQueries({ queryKey: [...POOL_QUERY_KEY] })
    } catch {
      setGeneralError(
        t('The import request failed. No credential status was changed.')
      )
    } finally {
      setBusy(false)
    }
  }

  return (
    <section
      className='space-y-4 rounded-lg border p-4'
      aria-labelledby='pool-import-title'
    >
      <div>
        <h2 id='pool-import-title' className='font-semibold'>
          {t('Import JSON credentials')}
        </h2>
        <p className='text-muted-foreground mt-1 text-sm'>
          {t(
            'Upload one or more Codex OAuth JSON files. Credentials are processed server-side and are never stored in browser storage or returned in responses.'
          )}
        </p>
      </div>
      <div
        role='button'
        tabIndex={0}
        aria-label={t('Drop JSON credential files here or choose files')}
        className={`rounded-lg border border-dashed p-6 text-center transition-colors ${
          dragging ? 'border-primary bg-muted/50' : 'border-border'
        }`}
        onClick={() => inputRef.current?.click()}
        onKeyDown={(event) => {
          if (event.key === 'Enter' || event.key === ' ') {
            inputRef.current?.click()
          }
        }}
        onDragOver={(event) => {
          event.preventDefault()
          setDragging(true)
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={(event) => {
          event.preventDefault()
          setDragging(false)
          choose([...event.dataTransfer.files])
        }}
      >
        <p className='font-medium'>{t('Drop JSON files here')}</p>
        <p className='text-muted-foreground mt-1 text-sm'>
          {t('Up to 10 files, 256 KiB each, 1 MiB total')}
        </p>
        <input
          ref={inputRef}
          type='file'
          accept='application/json,.json'
          multiple
          className='sr-only'
          onChange={(event) => {
            choose([...(event.target.files ?? [])])
            event.target.value = ''
          }}
        />
      </div>
      {files.length > 0 ? (
        <ul className='space-y-2'>
          {files.map((item) => (
            <li
              key={`${item.file.name}:${item.file.size}:${item.file.lastModified}`}
              className='flex flex-wrap items-center justify-between gap-2 rounded-md border p-2 text-sm'
            >
              <span className='min-w-0 truncate'>{item.file.name}</span>
              {item.error ? (
                <Badge variant='destructive'>{item.error}</Badge>
              ) : (
                <Badge variant='secondary'>{t('Ready')}</Badge>
              )}
            </li>
          ))}
        </ul>
      ) : null}
      {generalError ? (
        <p role='alert' className='text-destructive text-sm'>
          {generalError}
        </p>
      ) : null}
      <div className='flex flex-wrap gap-2'>
        <Button
          disabled={busy || files.every((item) => item.error)}
          onClick={() => upload(files)}
        >
          {busy ? t('Importing…') : t('Import files')}
        </Button>
        <Button
          variant='outline'
          disabled={busy || retryFiles.length === 0}
          onClick={() => upload(retryFiles)}
        >
          {t('Retry failed files')}
        </Button>
        <Button
          variant='ghost'
          disabled={busy || files.length === 0}
          onClick={() => {
            setFiles([])
            setResults([])
            setGeneralError('')
          }}
        >
          {t('Clear')}
        </Button>
      </div>
      {results.length > 0 ? (
        <div className='space-y-2'>
          <h3 className='text-sm font-medium'>{t('Import results')}</h3>
          <ul className='space-y-2'>
            {results.map((result) => (
              <li
                key={`${result.file_name}:${result.account_id ?? result.code ?? result.status}`}
                className='rounded-md border p-3 text-sm'
              >
                <div className='flex flex-wrap items-center justify-between gap-2'>
                  <span className='font-mono text-xs break-all'>
                    {result.file_name}
                  </span>
                  <Badge variant={resultVariant(result.status)}>
                    {t(statusLabel(result.status))}
                  </Badge>
                </div>
                <dl className='text-muted-foreground mt-2 grid gap-1 sm:grid-cols-2'>
                  {result.identity_masked ? (
                    <div>
                      <dt className='inline'>{t('Account')}: </dt>
                      <dd className='inline'>{result.identity_masked}</dd>
                    </div>
                  ) : null}
                  {result.account_id ? (
                    <div>
                      <dt className='inline'>{t('Pool ID')}: </dt>
                      <dd className='inline'>{result.account_id}</dd>
                    </div>
                  ) : null}
                  {result.run_status ? (
                    <div>
                      <dt className='inline'>{t('Registration')}: </dt>
                      <dd className='inline'>{result.run_status}</dd>
                    </div>
                  ) : null}
                  {result.admission ? (
                    <div>
                      <dt className='inline'>{t('Admission')}: </dt>
                      <dd className='inline'>{result.admission}</dd>
                    </div>
                  ) : null}
                  {result.capability ? (
                    <div>
                      <dt className='inline'>{t('Model capability')}: </dt>
                      <dd className='inline'>{result.capability}</dd>
                    </div>
                  ) : null}
                </dl>
                {result.code ? (
                  <p className='text-destructive mt-2 text-xs'>
                    {t(result.code)}
                  </p>
                ) : null}
              </li>
            ))}
          </ul>
        </div>
      ) : null}
      <Alert>
        <AlertTitle>{t('Admission is separate from upload')}</AlertTitle>
        <AlertDescription>
          {t(
            'A successfully imported account is registered as not admitted until trust and model capability are verified by the executor. Upload success alone never enables scheduling.'
          )}
        </AlertDescription>
      </Alert>
    </section>
  )
}
