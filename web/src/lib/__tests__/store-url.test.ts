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
import { describe, expect, test, vi } from 'vitest'

import { isValidStoreUrl, openStoreUrl } from '../store-url'

describe('isValidStoreUrl', () => {
  test('accepts https URLs', () => {
    expect(isValidStoreUrl('https://example.com/product/100')).toBe(true)
  })

  test('accepts localhost http for development', () => {
    expect(isValidStoreUrl('http://localhost:3000/product')).toBe(true)
    expect(isValidStoreUrl('http://127.0.0.1/product')).toBe(true)
  })

  test('rejects unsafe or non-https URLs', () => {
    expect(isValidStoreUrl('')).toBe(false)
    expect(isValidStoreUrl('javascript:alert(1)')).toBe(false)
    expect(isValidStoreUrl('data:text/html,<script>x</script>')).toBe(false)
    expect(isValidStoreUrl('file:///etc/passwd')).toBe(false)
    expect(isValidStoreUrl('ftp://example.com/x')).toBe(false)
    expect(isValidStoreUrl('//example.com/x')).toBe(false)
    expect(isValidStoreUrl('not a url')).toBe(false)
    expect(isValidStoreUrl('http://example.com/x')).toBe(false)
  })
})

describe('openStoreUrl', () => {
  test('opens valid URLs with noopener/noreferrer', () => {
    const openSpy = vi
      .spyOn(window, 'open')
      .mockReturnValue(null as unknown as Window)
    const opened = openStoreUrl(' https://example.com/product/100 ')
    expect(opened).toBe(true)
    expect(openSpy).toHaveBeenCalledWith(
      'https://example.com/product/100',
      '_blank',
      'noopener,noreferrer'
    )
    openSpy.mockRestore()
  })

  test('refuses invalid URLs without opening a window', () => {
    const openSpy = vi.spyOn(window, 'open')
    const opened = openStoreUrl('javascript:alert(1)')
    expect(opened).toBe(false)
    expect(openSpy).not.toHaveBeenCalled()
    openSpy.mockRestore()
  })
})
