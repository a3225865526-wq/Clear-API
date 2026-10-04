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
import { useTranslation } from 'react-i18next'

import type { LogOtherData } from '../types'

const qualityLabels: Record<string, string> = {
  missing: 'Metering is missing',
  upstream_partial: 'Metering is partial',
  inconsistent: 'Metering is inconsistent',
  local_estimate: 'Metering is locally estimated',
  upstream_complete: 'Complete upstream usage',
}
const sourceLabels: Record<string, string> = {
  upstream_response: 'Upstream response',
  mixed_upstream_local: 'Upstream and local estimate',
  local_estimate: 'Local estimate',
  unknown: 'Unknown',
}

export function TokenObservationDisplay(props: {
  observation: NonNullable<LogOtherData['admin_info']>['token_observation']
}) {
  const { t } = useTranslation()
  const observation = props.observation
  if (!observation) return null
  const metrics: [string, number | null][] = [
    ['Input', observation.input],
    ['Output', observation.output],
    ['Cache read tokens', observation.cache_read],
    ['Cache write tokens', observation.cache_write],
    ['Reasoning tokens', observation.reasoning],
    ['Total tokens', observation.total],
  ]
  return (
    <div
      className='min-w-0 space-y-1 text-xs break-words'
      aria-label={t('Token measurement')}
    >
      <p>
        {t(qualityLabels[observation.quality] || 'Unknown')} ·{' '}
        {t(sourceLabels[observation.source] || 'Unknown')}
      </p>
      <div className='flex flex-wrap gap-x-3 gap-y-1'>
        {metrics.map(([label, value]) => (
          <span key={label}>
            {t(label)}: {value == null ? t('Unknown') : value.toLocaleString()}
          </span>
        ))}
      </div>
      {observation.interrupted ? <p>{t('Interrupted')}</p> : null}
      {observation.internal_model ? (
        <p>
          {t('Internal model')}: {observation.internal_model}
        </p>
      ) : null}
      {observation.model_alias ? (
        <p>
          {t('External model alias')}: {observation.model_alias}
        </p>
      ) : null}
    </div>
  )
}
