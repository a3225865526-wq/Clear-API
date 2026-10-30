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
import { useQuery } from '@tanstack/react-query'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { ModelSelector } from '@/components/model-group-selector'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { getUserModels } from '@/features/playground/api'
import { getPricing } from '@/features/pricing/api'
import { useStatus } from '@/hooks/use-status'
import { requireServerSuccess } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import {
  apiRoot,
  buildClientConfig,
  guideClients,
  type GuideClient,
} from './client-config'

const configPaths = {
  codex: '~/.codex/config.toml',
  claude: '~/.claude/settings.json',
  opencode: '~/.config/opencode/opencode.json',
  cherry: 'Cherry Studio → OpenAI-compatible',
}

export function ClientSetup() {
  const { t } = useTranslation()
  const { status } = useStatus()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const id = useId()
  const [client, setClient] = useState<GuideClient>('codex')
  const [selectedModel, setSelectedModel] = useState('')
  const query = useQuery({
    queryKey: ['guide-client-models', userId],
    enabled: Boolean(userId),
    queryFn: async () => {
      const [models, pricing] = await Promise.all([
        getUserModels(''),
        getPricing(),
      ])
      requireServerSuccess(pricing)
      return { models, pricing }
    },
  })
  const models = (query.data?.models ?? []).filter((model) =>
    query.data?.pricing.data.some(
      (entry) =>
        entry.model_name === model.value &&
        entry.supported_endpoint_types?.includes(guideClients[client].endpoint)
    )
  )
  const model =
    models.find((item) => item.value === selectedModel)?.value ??
    models[0]?.value ??
    ''
  let root = ''
  try {
    root = apiRoot(
      (typeof status?.server_address === 'string' &&
        status.server_address.trim()) ||
        window.location.origin
    )
  } catch {
    // Do not produce a configuration with a malformed or credential-bearing URL.
  }
  const config = root && model ? buildClientConfig(client, root, model) : ''
  return (
    <section
      aria-labelledby={`${id}-title`}
      className='bg-card min-w-0 space-y-4 rounded-2xl border p-5'
    >
      <h2 id={`${id}-title`} className='font-semibold'>
        {t('Client configuration')}
      </h2>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Choose a client and an available model. Your API key may have additional group and model restrictions.'
        )}
      </p>
      <div className='flex flex-wrap items-end gap-4'>
        <div className='space-y-2'>
          <label htmlFor={`${id}-client`} className='text-sm'>
            {t('Client')}
          </label>
          <NativeSelect
            id={`${id}-client`}
            value={client}
            onChange={(event) => setClient(event.target.value as GuideClient)}
          >
            {Object.entries(guideClients).map(([value, item]) => (
              <NativeSelectOption key={value} value={value}>
                {item.name}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </div>
        <fieldset className='min-w-0 space-y-2'>
          <legend className='mb-2 text-sm'>{t('Model')}</legend>
          <ModelSelector
            selectedModel={model}
            models={models}
            onModelChange={setSelectedModel}
            disabled={!models.length || query.isFetching}
          />
        </fieldset>
        <span className='text-muted-foreground text-xs'>
          {guideClients[client].endpoint}
        </span>
      </div>
      {query.isPending && <LoadingState />}
      {query.isError && <ErrorState onRetry={() => void query.refetch()} />}
      {query.isSuccess && !models.length && (
        <EmptyState title={t('No models available for this protocol')} />
      )}
      {!root && <p role='alert'>{t('Invalid API address')}</p>}
      {config && !query.isError && (
        <>
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <code className='text-sm break-all'>{configPaths[client]}</code>
            <CopyButton value={config} size='sm' variant='outline'>
              {t('Copy configuration')}
            </CopyButton>
          </div>
          <pre className='bg-muted max-h-96 overflow-auto rounded-lg p-4 text-xs'>
            <code>{config}</code>
          </pre>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Merge with your existing local configuration. Replace the key placeholder locally, or set CLEARAPI_API_KEY for Codex and OpenCode. Never share your key or commit it to a repository.'
            )}
          </p>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Start a new client session, select this model, send a short message, and check the usage log. Protocol availability does not guarantee every client feature.'
            )}
          </p>
        </>
      )}
    </section>
  )
}
