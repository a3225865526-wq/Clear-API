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
export type PoolAdminStatus = 'enabled' | 'paused'

export type PoolRunStatus =
  | 'awaiting_auth'
  | 'authorizing'
  | 'available'
  | 'reauth_required'
  | 'cooldown'
  | 'executor_offline'
  | 'not_admitted'

export type PoolTaskKind = 'initial' | 'reauth'

export type PoolTaskStatus =
  | 'pending'
  | 'completed'
  | 'expired'
  | 'cancelled'
  | 'failed'

export type PoolFailReason =
  | 'remote_start_uncertain'
  | 'remote_cancel_unconfirmed'
  | 'correlation_failed'
  | 'post_auth_disable_failed'
  | 'orphan_cleanup_failed'
  | 'expired'
  | 'identity_mismatch'
  | 'credential_ref_taken'
  | 'executor_error'
  | 'platform_mismatch'
  | 'concurrent_update'

export interface PoolCapability {
  can_access: boolean
  schema_ready: boolean
  adapter_version: number
}

export interface PoolExecutor {
  id: string
  type: string
  connected: boolean
  verified: boolean
  endpoint_configured: boolean
  note: string
}

export interface PoolClaim {
  key: string
  status: 'unverified' | 'verified' | 'refuted'
  evidence: 'documentation' | 'source' | 'test' | 'live' | 'none'
  note?: string
}

export interface PoolAccessTypeSpec {
  type: string
  label: string
  executor_id: string
  auth_methods: string[]
  supported_ops: string[]
  unsupported_ops: string[]
  disabled: boolean
  disabled_reason?: string
  claims: PoolClaim[]
}

export interface PoolPlatformSpec {
  platform: string
  label: string
  access_types: PoolAccessTypeSpec[]
  /** Optional platform-level switch advertised by newer adapters. */
  disabled?: boolean
  disabled_reason?: string
}

export interface PoolCatalog {
  adapter_version: number
  platforms: PoolPlatformSpec[]
}

export interface PoolAccount {
  quota?: PoolQuota
  id: number
  platform: string
  access_type: string
  identity_masked: string
  note: string
  admin_status: PoolAdminStatus
  run_status: PoolRunStatus
  executor_id: string
  credential_ref: string
  /** Unix seconds. */
  last_refresh_at: number | null
  last_error: string
  version: number
  /** Unix seconds. */
  created_at: number
  /** Unix seconds. */
  updated_at: number
}

export interface PoolQuotaMetric {
  query_result: PoolQuota['result']
  kind: string
  value: number
  unit: string
  scope: string
  pool: string
  model?: string
  window_seconds?: number
  reset_at?: number
  source:
    | 'upstream'
    | 'upstream_derived'
    | 'local_statistics'
    | 'local_prediction'
    | 'unknown'
  updated_at: number
  stale: boolean
}

export interface PoolQuota {
  query_capability: 'unsupported' | 'observations_only'
  result: 'unknown' | 'unsupported' | 'failed' | 'success'
  metrics: PoolQuotaMetric[]
  checked_at: number
  next_refresh_at: number
  stale: boolean
  reason?: string
}

export interface ModelAdmission {
  platform: string
  access_type: string
  model: string
  adapter_version: number
  open: boolean
  reasons: string[]
  metering_source: string
  quota_capability: string
  capabilities: {
    capability: string
    status: 'supported' | 'unsupported' | 'unverified'
    native: boolean
    evidence: {
      kind: string
      reference: string
      revision: string
      adapter_version: number
    }[]
  }[]
}

export interface PoolTask {
  id: string
  platform: string
  access_type: string
  kind: PoolTaskKind
  status: PoolTaskStatus
  fail_reason?: PoolFailReason
  /** Unix seconds. */
  expires_at: number | null
  /** Unix seconds. */
  created_at: number
  /** Unix seconds. */
  updated_at: number
}

export interface PoolAccountDetail {
  account: PoolAccount
  tasks: PoolTask[]
}

export interface PoolAccountCapabilities {
  model_admission: ModelAdmission
  platform: PoolPlatformSpec
  access_type: PoolAccessTypeSpec | null
  account_status: PoolRunStatus
}

export type PoolAuthMethod = 'oauth_browser' | 'oauth_device'

/**
 * One-time view of an authorization task. The auth URL / user code are shown
 * once in page state only and can never be fetched again.
 */
export interface PoolAuthStart {
  task_id: string
  method: PoolAuthMethod
  auth_url?: string
  verification_uri?: string
  user_code?: string
  /** Unix seconds. */
  expires_at: number
  account_version: number
  one_time: true
}

export interface CreatePoolAccountInput {
  platform: string
  access_type: string
  executor_id?: string
  note?: string
}

export interface CompletePoolAuthInput {
  callback_url?: string
  code?: string
  state?: string
}
