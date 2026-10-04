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
import type {
  PoolAdminStatus,
  PoolFailReason,
  PoolRunStatus,
  PoolTaskKind,
  PoolTaskStatus,
} from './types'

/**
 * Every value is an i18n key (identical to the English fallback). Render via
 * `t(config[key])`; the static keys are registered in `src/i18n/static-keys.ts`.
 */
export const POOL_ADMIN_STATUS_LABELS: Record<PoolAdminStatus, string> = {
  enabled: 'Enabled',
  paused: 'Paused',
}

export const POOL_RUN_STATUS_LABELS: Record<PoolRunStatus, string> = {
  awaiting_auth: 'Awaiting authorization',
  authorizing: 'Authorizing',
  available: 'Available',
  reauth_required: 'Re-authorization required',
  cooldown: 'Cooling down',
  executor_offline: 'Executor offline',
  not_admitted: 'Not admitted',
}

export const POOL_TASK_STATUS_LABELS: Record<PoolTaskStatus, string> = {
  pending: 'Pending',
  completed: 'Completed',
  expired: 'Expired',
  cancelled: 'Cancelled',
  failed: 'Failed',
}

export const POOL_TASK_KIND_LABELS: Record<PoolTaskKind, string> = {
  initial: 'Initial authorization',
  reauth: 'Re-authorization',
}

export const POOL_FAIL_REASON_LABELS: Record<PoolFailReason, string> = {
  remote_start_uncertain:
    'Remote authorization start is uncertain; reservation remains held',
  remote_cancel_unconfirmed:
    'Remote authorization termination is unconfirmed; reservation remains held',
  correlation_failed:
    'Credential correlation failed; manual reconciliation required',
  post_auth_disable_failed:
    'New credential could not be disabled; reservation remains held',
  orphan_cleanup_failed:
    'Remote credential cleanup failed; reservation remains held',
  expired: 'Authorization expired',
  identity_mismatch: 'Identity mismatch',
  credential_ref_taken: 'Credential reference already taken',
  executor_error: 'Executor error',
  platform_mismatch: 'Platform mismatch',
  concurrent_update: 'Concurrent update',
}

export const POOL_CLAIM_STATUS_LABELS: Record<
  'unverified' | 'verified' | 'refuted',
  string
> = {
  unverified: 'Unverified',
  verified: 'Verified',
  refuted: 'Refuted',
}

export const POOL_EVIDENCE_LABELS: Record<
  'documentation' | 'source' | 'test' | 'live' | 'none',
  string
> = {
  documentation: 'Documentation',
  source: 'Confirmed in source code',
  test: 'Verified by test',
  live: 'Live verification',
  none: 'No evidence',
}

/**
 * Server `code` values map to i18n keys (English fallback text). poolRequest
 * attaches them to errors with the safe-server-message symbol so toast and
 * inline handlers translate them consistently.
 */
export const POOL_ERROR_MESSAGE_KEYS: Record<string, string> = {
  POOL_INVALID: 'The pool request is invalid.',
  POOL_NOT_FOUND: 'The pool account or task does not exist.',
  POOL_CONFLICT:
    'This account changed while you were working. Please review the latest state and retry.',
  POOL_TASK_EXPIRED: 'This authorization task has expired. Remote session reconciliation is required before another authorization.',
  POOL_UNSUPPORTED: 'This operation is not supported.',
  POOL_EXECUTOR_OFFLINE: 'The executor is offline. Please try again later.',
  POOL_EXECUTOR_FAILED: 'The executor failed to complete this operation.',
  POOL_NOT_ADMITTED:
    'This account has not passed capability admission and cannot be resumed yet.',
  POOL_ISOLATION_REQUIRED:
    'Executor isolation is not attested. Complete the required deployment actions first.',
  POOL_CORRELATION_FAILED:
    'The new credential could not be reliably identified. Nothing was changed; please resolve manually.',
  POOL_AUTH_TIMEOUT:
    'The remote authorization has not been confirmed. Reconcile the previous session before starting another authorization.',
  POOL_UNAVAILABLE:
    'Pool storage is not initialized. Please run the storage migration first.',
  POOL_INTERNAL: 'Upstream account pool error. Please try again later.',
}

export const POOL_UNAVAILABLE_MESSAGE_KEY =
  'Upstream account pool is unavailable.'

export const POOL_QUERY_KEY = ['upstream-pool'] as const

export const POOL_VIEW_MODE_STORAGE_KEY = 'upstream-pool:view-mode'
