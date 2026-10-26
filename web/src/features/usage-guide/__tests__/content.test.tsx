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
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { RichContent } from '@/components/rich-content'

import guide from '../../../../public/guide/pages.json'

describe('published beginner guide content', () => {
  it('shows distinct redemption entries and links users to their wallet', () => {
    render(
      <RichContent mode='html' content={guide.pages['/start/redeem'].html} />
    )

    expect(screen.getAllByText(/原版兑换码（Legacy）/).length).toBeGreaterThan(
      0
    )
    expect(
      screen.getByRole('cell', { name: /商城兑换码（仅启用该功能时显示）/ })
    ).toBeVisible()
    expect(
      screen.getByRole('cell', { name: '钱包 → 其他兑换方式 → 线下订单兑换码' })
    ).toBeVisible()
    expect(screen.getByRole('link', { name: '钱包' })).toHaveAttribute(
      'href',
      '/wallet'
    )
  })

  it('renders selectable request examples without losing placeholders or budgets', () => {
    const { container } = render(
      <RichContent
        mode='html'
        content={guide.pages['/start/first-request'].html}
      />
    )

    const code = [...container.querySelectorAll('pre code')].map(
      (block) => block.textContent
    )
    expect(
      code.some((block) =>
        block?.includes("Read-Host 'API Key' -AsSecureString")
      )
    ).toBe(true)
    expect(
      code.some((block) =>
        block?.includes('"model":"YOUR_MODEL_ID","max_tokens":128')
      )
    ).toBe(true)
    expect(
      screen.getByRole('link', { name: '查看可复制配置示例和完整文档' })
    ).toHaveAttribute('href', '/docs/clearapi/quickstart?lang=zh')
  })

  it('keeps catalog targets and article navigation reachable after content updates', () => {
    for (const page of Object.values(guide.pages)) {
      for (const section of page.sections) {
        for (const item of section.items) {
          expect(guide.pages).toHaveProperty([item.path])
        }
      }
      const root = document.createElement('div')
      root.innerHTML = page.html
      for (const link of root.querySelectorAll('a[href^="/usage-guide?"]')) {
        const article = new URL(
          link.getAttribute('href') ?? '',
          'https://guide.invalid'
        ).searchParams.get('article')
        if (article) expect(guide.pages).toHaveProperty([article])
      }
    }
  })
})
