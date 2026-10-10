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

export const referralRuleSchema = z.object({
  invites_enabled: z.boolean(),
  rewards_enabled: z.boolean(),
  mode: z.enum(['percentage', 'fixed']),
  basis_points: z.number().int().min(0).max(10000),
  fixed_quota: z.number().int().min(0).max(Number.MAX_SAFE_INTEGER),
  per_reward_cap: z.number().int().positive().max(Number.MAX_SAFE_INTEGER),
  lifetime_cap: z.number().int().positive().max(Number.MAX_SAFE_INTEGER),
  version: z.number().int(),
  epoch: z.number().int(),
})
