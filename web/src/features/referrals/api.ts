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
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

export type ReferralRule = {
  invites_enabled: boolean
  rewards_enabled: boolean
  mode: 'percentage' | 'fixed'
  basis_points: number
  fixed_quota: number
  per_reward_cap: number
  lifetime_cap: number
  version: number
  epoch: number
}
export type Reward = {
  id: number
  reward_quota: number
  status: string
  created_time: number
  paid_time: number
}
export type ReferralSelf = {
  code: string
  link: string
  count: number
  paid_quota: number
  rule: ReferralRule
  rewards: Reward[]
  total: number
}
export type ReferralAdmin = {
  items: { reward: Reward; inviter_id: number; invitee_id: number }[]
  total: number
  summary: { status: string; count: number; quota: number }[]
  audits: {
    id: number
    actor_id: number
    action: string
    details: string
    created_time: number
  }[]
}
export async function getReferralSelf(page: number): Promise<ReferralSelf> {
  const r = await api.get(`/api/clearapi/referral/self?page=${page}`)
  return requireServerSuccess(r.data).data
}
export async function getReferralRule(): Promise<ReferralRule> {
  const r = await api.get('/api/clearapi/referral/settings')
  return requireServerSuccess(r.data).data
}
export async function saveReferralRule(rule: ReferralRule): Promise<void> {
  const r = await api.put('/api/clearapi/referral/settings', rule)
  requireServerSuccess(r.data)
}
export async function getReferralAdmin(page: number): Promise<ReferralAdmin> {
  const r = await api.get(`/api/clearapi/referral/rewards?page=${page}`)
  return requireServerSuccess(r.data).data
}
export async function qualifyCodes(input: {
  ids: number[]
  kind: string
  reason: string
}): Promise<void> {
  const r = await api.put('/api/clearapi/referral/qualification', input)
  requireServerSuccess(r.data)
}
export type ReferralStockPreview = {
  count: number
  digest: string
  batches: {
    id: number
    name: string
    product_id: string
    product_revision: number
    count: number
  }[]
}
export async function getReferralStockPreview(): Promise<ReferralStockPreview> {
  const r = await api.get('/api/clearapi/referral/qualification/store')
  return requireServerSuccess(r.data).data
}
export async function qualifyReferralStock(digest: string): Promise<{
  updated_count: number
}> {
  const r = await api.post('/api/clearapi/referral/qualification/store', {
    digest,
  })
  return requireServerSuccess(r.data).data
}
export async function resumeReward(input: {
  id: number
  reason: string
}): Promise<void> {
  const r = await api.post(
    `/api/clearapi/referral/rewards/${input.id}/resume`,
    { reason: input.reason }
  )
  requireServerSuccess(r.data)
}
