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
import { readFileSync, readdirSync } from 'node:fs'
import { resolve } from 'node:path'

import { render, screen } from '@testing-library/react'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { describe, expect, it } from 'vitest'

import zh from '@/i18n/locales/zh.json'

import { Code } from '../components'
import { codeLabels, metricLabels } from '../labels'
import { fieldLabels, operationLabels, options } from '../operation-form'

const directory = resolve(process.cwd(), 'src/features/owner-finance')
const source = readdirSync(directory)
  .filter((name) => /\.tsx?$/.test(name))
  .map((name) => readFileSync(`${directory}/${name}`, 'utf8'))
  .join('\n')
const literalKeys = [...source.matchAll(/\bt\(\s*'([^']+)'/g)].map(
  (match) => match[1]
)
const keys = new Set([
  ...literalKeys,
  ...Object.values(codeLabels),
  ...Object.values(metricLabels),
  ...Object.values(fieldLabels),
  ...Object.values(operationLabels),
  ...Object.values(options).flatMap((values) => Object.values(values)),
  'FX record key (reuse for next version)',
  'Current FX head ID (0 for first version)',
  'Frozen CNY per original currency unit',
  'Enter a valid value for this field.',
])

async function chinese() {
  const instance = createInstance()
  await instance.init({
    lng: 'zhCN',
    fallbackLng: 'en',
    nsSeparator: false,
    resources: { zhCN: zh },
    interpolation: { escapeValue: false },
  })
  return instance
}

describe('Chinese owner finance translations', () => {
  it('loads every finance heading, field, status and explanation in Chinese', async () => {
    const instance = await chinese()
    for (const key of keys) {
      expect(instance.exists(key), key).toBe(true)
      expect(instance.t(key), key).toMatch(/[\u4e00-\u9fff]/)
    }
  })

  it('renders translated evidence statuses and substitutes overview counts', async () => {
    const instance = await chinese()
    render(
      <I18nextProvider i18n={instance}>
        <Code code='unknown_fx' />
      </I18nextProvider>
    )
    expect(screen.getByText(instance.t('Missing frozen FX'))).toBeInTheDocument()
    expect(instance.t('ops.title')).toBe('全站经营概览')
    expect(instance.t('ops.unknownRegistration', { count: 3 })).toContain('3 个用户')
    expect(instance.t('ops.unknownRegistration', { count: 3 })).not.toContain('{{')
    expect(instance.t('Owner finance')).toBe('站长财务')
  })
})
