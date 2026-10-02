/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful, but WITHOUT
ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or
FITNESS FOR A PARTICULAR PURPOSE. See the GNU Affero General Public License
for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { describe, expect, it } from 'vitest'

import {
  CLEARAPI_BRAND_NAME,
  getClearApiSystemName,
  getClearApiLogo,
  getClearApiDocsLink,
  isDefaultNewApiName,
} from '../clearapi-branding'

describe('ClearAPI system branding', () => {
  it('uses ClearAPI for a missing or default New API name', () => {
    expect(getClearApiSystemName()).toBe(CLEARAPI_BRAND_NAME)
    expect(getClearApiSystemName(' New API ')).toBe(CLEARAPI_BRAND_NAME)
    expect(isDefaultNewApiName('nEw aPi')).toBe(true)
  })

  it('keeps a custom system name and trims surrounding whitespace', () => {
    expect(getClearApiSystemName(' Example Gateway ')).toBe('Example Gateway')
    expect(isDefaultNewApiName('Example Gateway')).toBe(false)
  })
})


describe('ClearAPI assets and documentation', () => {
  it('maps legacy logo paths while retaining a configured logo', () => {
    expect(getClearApiLogo()).toBe('/clearapi.svg?v=2')
    expect(getClearApiLogo('/logo.png')).toBe('/clearapi.svg?v=2')
    expect(getClearApiLogo('/favicon.ico')).toBe('/clearapi.svg?v=2')
    expect(getClearApiLogo('/clearapi.svg')).toBe('/clearapi.svg?v=2')
    expect(getClearApiLogo('/custom.svg')).toBe('/custom.svg')
  })

  it('normalizes both ClearAPI brand aliases', () => {
    expect(getClearApiSystemName('ClearAPI')).toBe(CLEARAPI_BRAND_NAME)
    expect(getClearApiSystemName('澄光 API')).toBe(CLEARAPI_BRAND_NAME)
    expect(getClearApiSystemName('new-api')).toBe(CLEARAPI_BRAND_NAME)
  })

  it('hides default upstream documentation without inventing a replacement', () => {
    expect(getClearApiDocsLink()).toBeUndefined()
    expect(getClearApiDocsLink('https://docs.newapi.pro/wiki/')).toBeUndefined()
    expect(getClearApiDocsLink('https://newapi.pro')).toBeUndefined()
    expect(getClearApiDocsLink('/#quickstart')).toBe('/#quickstart')
    expect(getClearApiDocsLink('https://example.com/help')).toBe('https://example.com/help')
  })
})
