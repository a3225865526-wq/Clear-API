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
import { z } from 'zod'

import { hasMixedCandidateTypes } from './chain-policy'

// Editor form schema (§六–§十一). Field bounds mirror the backend policy
// validation; the backend re-validates authoritatively on save. The
// cross-source rule is enforced client-side as UX and authoritatively server-side.

export const chainEditorFormSchema = z
  .object({
    model: z.string().trim().min(1, 'Model is required.'),
    group: z.string().trim().min(1, 'Group is required.'),
    enabled: z.boolean(),
    allow_cross_source: z.boolean(),
    candidates: z
      .array(
        z.object({
          id: z.number().optional(),
          candidate_type: z.enum(['pool', 'api_channel']),
          target_id: z
            .number({ message: 'Pick a target channel.' })
            .int()
            .positive('Pick a target channel.'),
          enabled: z.boolean(),
          failure_threshold: z
            .union([z.literal(''), z.number().int().min(1).max(20)])
            .optional(),
          cooldown_seconds: z
            .union([z.literal(''), z.number().int().min(1).max(3600)])
            .optional(),
          failover_on_429: z.boolean().optional(),
          failover_on_404: z.boolean().optional(),
          connect_timeout_ms: z
            .union([z.literal(''), z.number().int().min(0).max(120000)])
            .optional(),
        })
      )
      .min(1, 'A route chain needs at least one candidate.'),
  })
  .superRefine((data, ctx) => {
    const mixed = hasMixedCandidateTypes(data.candidates)
    if (mixed && !data.allow_cross_source) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['allow_cross_source'],
        message:
          'This configuration mixes pool and API candidates, but cross-source failover is disabled. Enable cross-source failover explicitly or use a single source.',
      })
    }
    const seenTargets = new Set<number>()
    for (const [index, candidate] of data.candidates.entries()) {
      if (candidate.target_id <= 0) continue
      if (seenTargets.has(candidate.target_id)) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ['candidates', index, 'target_id'],
          message: 'The same target channel appears more than once.',
        })
      }
      seenTargets.add(candidate.target_id)
    }
  })

export type ChainEditorFormValues = z.input<typeof chainEditorFormSchema>
