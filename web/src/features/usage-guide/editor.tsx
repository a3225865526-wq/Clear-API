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
import { useMutation, useQuery } from '@tanstack/react-query'
import { isAxiosError } from 'axios'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { RichContent } from '@/components/rich-content'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { toIntlLocale } from '@/i18n/languages'

import {
  getGuideVersions,
  saveGuide,
  uploadGuideImage,
  type GuideDocument,
  type GuideSection,
} from './api'

export function GuideEditor(props: {
  article: string
  document: GuideDocument
  open: boolean
  onClose: () => void
  onSaved: (document: GuideDocument) => void
}) {
  const { t, i18n } = useTranslation()
  const page = props.document.pages[props.article]
  const [revision] = useState(props.document.revision)
  const [title, setTitle] = useState(page.title)
  const [html, setHtml] = useState(page.html)
  const [previous, setPrevious] = useState(page.previous ?? '')
  const [next, setNext] = useState(page.next ?? '')
  const [catalog, setCatalog] = useState(
    JSON.stringify(props.document.catalog, null, 2)
  )
  const [error, setError] = useState('')
  const [discard, setDiscard] = useState(false)
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const history = useQuery({
    queryKey: ['guide-versions', props.article, props.document.revision],
    queryFn: ({ signal }) => getGuideVersions(props.article, signal),
    enabled: props.open,
    retry: false,
  })
  const save = useMutation({
    mutationFn: saveGuide,
    onSuccess: (document) => {
      props.onSaved(document)
      props.onClose()
    },
    onError: (failure) => {
      const conflict = isAxiosError(failure) && failure.response?.status === 409
      setError(t(conflict ? 'guideEdit.conflict' : 'guideEdit.saveFailed'))
    },
  })
  const upload = useMutation({
    mutationFn: uploadGuideImage,
    onSuccess: (image) =>
      setHtml((value) => `${value}\n<img src="${image.url}" alt="">`),
    onError: () => setError(t('guideEdit.uploadFailed')),
  })
  const dirty =
    title !== page.title ||
    html !== page.html ||
    previous !== (page.previous ?? '') ||
    next !== (page.next ?? '') ||
    catalog !== JSON.stringify(props.document.catalog, null, 2)
  const busy = save.isPending || upload.isPending
  const close = () => {
    if (busy) return
    if (dirty) setDiscard(true)
    else props.onClose()
  }
  const submit = () => {
    setError('')
    let sections: GuideSection[]
    try {
      const parsed: unknown = JSON.parse(catalog)
      if (!Array.isArray(parsed)) throw new Error('invalid')
      sections = parsed as GuideSection[]
    } catch {
      setError(t('guideEdit.invalidCatalog'))
      return
    }
    save.mutate({
      path: props.article,
      revision,
      title,
      html,
      previous,
      next,
      catalog: sections,
    })
  }
  return (
    <>
      <Dialog
        open={props.open}
        onOpenChange={(open) => {
          if (!open) close()
        }}
        title={t('guideEdit.title')}
        description={t('guideEdit.hint')}
        contentClassName='sm:max-w-6xl'
        contentHeight='min(65vh, 750px)'
        footer={
          <>
            <Button variant='outline' onClick={close} disabled={busy}>
              {t('guideEdit.cancel')}
            </Button>
            <Button onClick={submit} disabled={busy || !dirty}>
              {t(save.isPending ? 'guideEdit.saving' : 'guideEdit.publish')}
            </Button>
          </>
        }
      >
        <div className='space-y-4'>
          {error && (
            <Alert variant='destructive'>
              <AlertDescription role='alert'>{error}</AlertDescription>
            </Alert>
          )}
          <div className='space-y-2'>
            <Label htmlFor='guide-title'>{t('guideEdit.articleTitle')}</Label>
            <Input
              id='guide-title'
              value={title}
              onChange={(event) => setTitle(event.target.value)}
              maxLength={200}
              disabled={busy}
            />
          </div>
          <div className='grid min-w-0 gap-4 lg:grid-cols-2'>
            <div className='min-w-0 space-y-2'>
              <Label htmlFor='guide-body'>{t('guideEdit.html')}</Label>
              <Textarea
                id='guide-body'
                value={html}
                onChange={(event) => setHtml(event.target.value)}
                className='h-96 max-h-none resize-y font-mono text-sm'
                disabled={busy}
              />
            </div>
            <section
              className='min-w-0 space-y-2'
              aria-label={t('guideEdit.preview')}
            >
              <h3 className='text-sm font-medium'>{t('guideEdit.preview')}</h3>
              <div className='h-96 overflow-auto rounded-lg border p-4'>
                <RichContent
                  mode='html'
                  content={html}
                  className='migrated-guide'
                />
              </div>
            </section>
          </div>
          <div className='space-y-2'>
            <Label htmlFor='guide-image'>{t('guideEdit.upload')}</Label>
            <Input
              id='guide-image'
              type='file'
              accept='image/png,image/jpeg,image/gif'
              disabled={busy}
              onChange={(event) => {
                const file = event.target.files?.[0]
                if (file) {
                  setError('')
                  upload.mutate(file)
                }
                event.target.value = ''
              }}
            />
            <p className='text-muted-foreground text-xs'>
              {t(
                upload.isPending ? 'guideEdit.uploading' : 'guideEdit.imageHint'
              )}
            </p>
          </div>
          <div className='grid gap-4 sm:grid-cols-2'>
            {[
              {
                label: 'guide.previous',
                id: 'guide-previous',
                value: previous,
                set: setPrevious,
              },
              {
                label: 'guide.next',
                id: 'guide-next',
                value: next,
                set: setNext,
              },
            ].map((field) => (
              <div key={field.id} className='space-y-2'>
                <Label htmlFor={field.id}>{t(field.label)}</Label>
                <select
                  id={field.id}
                  value={field.value}
                  onChange={(event) => field.set(event.target.value)}
                  disabled={busy}
                  className='bg-background h-9 w-full rounded-lg border px-2 text-sm'
                >
                  <option value=''>{t('guideEdit.none')}</option>
                  {Object.entries(props.document.pages)
                    .filter(([path]) => path !== props.article)
                    .map(([path, item]) => (
                      <option key={path} value={path}>
                        {item.title}
                      </option>
                    ))}
                </select>
              </div>
            ))}
          </div>
          <details>
            <summary className='cursor-pointer text-sm font-medium'>
              {t('guideEdit.catalog')}
            </summary>
            <p className='text-muted-foreground my-2 text-xs'>
              {t('guideEdit.catalogHint')}
            </p>
            <Label htmlFor='guide-catalog'>{t('guideEdit.catalogJson')}</Label>
            <Textarea
              id='guide-catalog'
              value={catalog}
              onChange={(event) => setCatalog(event.target.value)}
              className='mt-2 h-48 font-mono text-sm'
              disabled={busy}
            />
          </details>
          <div className='space-y-2'>
            <Label htmlFor='guide-history'>{t('guideEdit.history')}</Label>
            {history.isError && (
              <p role='alert' className='text-destructive text-sm'>
                {t('guideEdit.historyFailed')}{' '}
                <Button
                  variant='outline'
                  size='sm'
                  onClick={() => void history.refetch()}
                >
                  {t('console.refresh')}
                </Button>
              </p>
            )}
            <select
              id='guide-history'
              value=''
              className='bg-background h-9 w-full rounded-lg border px-2 text-sm'
              disabled={busy || history.isPending}
              onChange={(event) => {
                const version = history.data?.[Number(event.target.value)]
                if (!version) return
                setTitle(version.article.title)
                setHtml(version.article.html)
                setPrevious(version.article.previous ?? '')
                setNext(version.article.next ?? '')
                setCatalog(JSON.stringify(version.catalog, null, 2))
                setError('')
              }}
            >
              <option value=''>
                {t(
                  history.isPending
                    ? 'guideEdit.loadingHistory'
                    : 'guideEdit.chooseHistory'
                )}
              </option>
              {history.data?.map((version, index) => (
                <option key={`${version.revision}-${version.at}`} value={index}>
                  {version.at
                    ? `v${version.revision} · ${new Date(version.at * 1000).toLocaleString(locale)}`
                    : t('guideEdit.original')}
                </option>
              ))}
            </select>
            <p className='text-muted-foreground text-xs'>
              {t('guideEdit.restoreHint')}
            </p>
          </div>
        </div>
      </Dialog>
      <Dialog
        open={discard}
        onOpenChange={setDiscard}
        title={t('guideEdit.discardTitle')}
        description={t('guideEdit.discardHint')}
        footer={
          <>
            <Button variant='outline' onClick={() => setDiscard(false)}>
              {t('guideEdit.keepEditing')}
            </Button>
            <Button
              variant='destructive'
              onClick={() => {
                setDiscard(false)
                props.onClose()
              }}
            >
              {t('guideEdit.discard')}
            </Button>
          </>
        }
      >
        <span />
      </Dialog>
    </>
  )
}
