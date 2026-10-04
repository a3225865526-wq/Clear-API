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
import { it, expect } from 'vitest'

import { TokenObservationDisplay } from '../token-observation'

it('does not display missing usage as real zero and preserves explicit upstream zero', () => {
  const missing = {
    source: 'unknown',
    quality: 'missing',
    input: null,
    output: null,
    cache_read: null,
    cache_write: null,
    reasoning: null,
    total: null,
    interrupted: false,
  }
  const view = render(<TokenObservationDisplay observation={missing} />)
  expect(screen.getByText(/Metering is missing/)).toBeInTheDocument()
  expect(screen.getByText('Input: Unknown')).toBeInTheDocument()
  expect(screen.queryByText('Input: 0')).not.toBeInTheDocument()
  view.rerender(
    <TokenObservationDisplay
      observation={{
        ...missing,
        source: 'upstream_response',
        quality: 'upstream_partial',
        input: 0,
        internal_model: 'internal',
        model_alias: 'alias',
      }}
    />
  )
  expect(screen.getByText('Input: 0')).toBeInTheDocument()
  expect(screen.getByText('Output: Unknown')).toBeInTheDocument()
  expect(screen.getByText('Internal model: internal')).toBeInTheDocument()
})
