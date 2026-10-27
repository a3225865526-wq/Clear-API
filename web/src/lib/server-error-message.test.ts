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
import i18next from 'i18next'
import { describe, expect, test } from 'vitest'

import zh from '@/i18n/locales/zh.json'

import {
  getServerErrorMessage,
  getServerErrorMessageKey,
} from './server-error-message'

describe('server error message mapping', () => {
  test('maps the active-session limit to recovery instructions', () => {
    const message = getServerErrorMessageKey({ code: 'AUTH_SESSION_LIMIT' })

    expect(message ?? '').toMatch(/Sign out other sessions/)
    expect(message ?? '').toMatch(/reset your password/)
  })

  test('maps an Axios-shaped issuance limit to rolling-window guidance', () => {
    const message = getServerErrorMessageKey({
      response: { data: { code: 'AUTH_SESSION_ISSUANCE_LIMIT' } },
    })

    expect(message ?? '').toMatch(/rolling window/)
    expect(getServerErrorMessageKey({ code: 'UNKNOWN_CODE' })).toBe(null)
  })

  test('maps stable Telegram bind errors without exposing server text', () => {
    const expected = {
      TELEGRAM_BIND_DISABLED: 'Telegram binding is disabled.',
      TELEGRAM_BIND_INVALID_REQUEST:
        'The Telegram authorization request is invalid or expired.',
      TELEGRAM_BIND_FLOW_INVALID:
        'This Telegram binding request has expired or has already been used.',
      TELEGRAM_BIND_SESSION_INVALID:
        'The login session that started this Telegram binding is no longer valid.',
      TELEGRAM_BIND_ALREADY_BOUND: 'This Telegram account is already bound.',
      TELEGRAM_BIND_USER_DELETED: 'This user account no longer exists.',
      TELEGRAM_BIND_USER_DISABLED: 'This user account is disabled.',
      TELEGRAM_BIND_INTERNAL_ERROR:
        'Telegram binding failed. Please try again.',
    }

    for (const [code, message] of Object.entries(expected)) {
      expect(getServerErrorMessageKey({ code })).toBe(message)
    }

    expect(
      getServerErrorMessageKey({
        response: {
          data: { code: 'TELEGRAM_BIND_INTERNAL_ERROR', message: 'raw detail' },
        },
      })
    ).toBe(expected.TELEGRAM_BIND_INTERNAL_ERROR)
  })
})

test.each([
  [
    'STORE_CATALOG_CHANGED',
    'The product catalog changed. Reload the page and try again.',
  ],
  [
    'STORE_AUTOMATIC_VALIDATION_FAILED',
    'Automatic validation failed. Check the product configuration and delivery evidence, then reload before retrying.',
  ],
])(
  'maps %s to a safe localized product error instead of internal text',
  (code, message) => {
    expect(
      getServerErrorMessage({
        response: {
          data: { code, message: 'private SQL detail must not be shown' },
        },
      })
    ).toBe(message)
  }
)

test('shows the catalog conflict in Chinese instead of the raw English toast', async () => {
  const previousLanguage = i18next.language
  const previousResources = structuredClone(
    i18next.getResourceBundle('zh', 'translation')
  )
  try {
    i18next.addResourceBundle('zh', 'translation', zh.translation, true, true)
    await i18next.changeLanguage('zh')
    expect(
      getServerErrorMessage({
        code: 'STORE_CATALOG_CHANGED',
        message: 'The product catalog changed. Reload the page and try again.',
      })
    ).toBe('商品配置已更新，请刷新页面后重试。')
  } finally {
    i18next.removeResourceBundle('zh', 'translation')
    if (previousResources) {
      i18next.addResourceBundle('zh', 'translation', previousResources)
    }
    await i18next.changeLanguage(previousLanguage)
  }
})
