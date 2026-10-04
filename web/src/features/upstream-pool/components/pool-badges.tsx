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
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'

import {
  POOL_ADMIN_STATUS_LABELS,
  POOL_CLAIM_STATUS_LABELS,
  POOL_EVIDENCE_LABELS,
  POOL_RUN_STATUS_LABELS,
  POOL_TASK_STATUS_LABELS,
} from '../constants'
import type {
  PoolAdminStatus,
  PoolClaim,
  PoolRunStatus,
  PoolTaskStatus,
} from '../types'

export function PoolAdminStatusBadge(props: { status: PoolAdminStatus }) {
  const { t } = useTranslation()
  return (
    <Badge
      variant={props.status === 'enabled' ? 'outline' : 'warning'}
      aria-label={t(POOL_ADMIN_STATUS_LABELS[props.status])}
    >
      {t(POOL_ADMIN_STATUS_LABELS[props.status])}
    </Badge>
  )
}

const RUN_STATUS_VARIANTS: Record<
  PoolRunStatus,
  'default' | 'secondary' | 'warning' | 'destructive'
> = {
  available: 'default',
  awaiting_auth: 'secondary',
  authorizing: 'secondary',
  reauth_required: 'warning',
  cooldown: 'warning',
  executor_offline: 'destructive',
  not_admitted: 'secondary',
}

export function PoolRunStatusBadge(props: { status: PoolRunStatus }) {
  const { t } = useTranslation()
  return (
    <Badge
      variant={RUN_STATUS_VARIANTS[props.status] ?? 'outline'}
      aria-label={t(POOL_RUN_STATUS_LABELS[props.status])}
    >
      {t(POOL_RUN_STATUS_LABELS[props.status])}
    </Badge>
  )
}

const TASK_STATUS_VARIANTS: Record<
  PoolTaskStatus,
  'outline' | 'secondary' | 'warning' | 'destructive'
> = {
  pending: 'secondary',
  completed: 'outline',
  expired: 'warning',
  cancelled: 'outline',
  failed: 'destructive',
}

export function PoolTaskStatusBadge(props: { status: PoolTaskStatus }) {
  const { t } = useTranslation()
  return (
    <Badge
      variant={TASK_STATUS_VARIANTS[props.status] ?? 'outline'}
      aria-label={t(POOL_TASK_STATUS_LABELS[props.status])}
    >
      {t(POOL_TASK_STATUS_LABELS[props.status])}
    </Badge>
  )
}

function claimStatusVariant(
  status: PoolClaim['status']
): 'default' | 'warning' | 'destructive' {
  if (status === 'verified') return 'default'
  if (status === 'refuted') return 'destructive'
  return 'warning'
}

function evidenceVariant(
  evidence: PoolClaim['evidence']
): 'default' | 'secondary' | 'destructive' {
  if (evidence === 'source' || evidence === 'live') return 'default'
  if (evidence === 'none') return 'destructive'
  return 'secondary'
}

export function PoolClaimStatusBadge(props: { claim: PoolClaim }) {
  const { t } = useTranslation()
  const variant = claimStatusVariant(props.claim.status)
  return (
    <Badge
      variant={variant}
      aria-label={t(POOL_CLAIM_STATUS_LABELS[props.claim.status])}
    >
      {t(POOL_CLAIM_STATUS_LABELS[props.claim.status])}
    </Badge>
  )
}

export function PoolEvidenceBadge(props: { claim: PoolClaim }) {
  const { t } = useTranslation()
  const variant = evidenceVariant(props.claim.evidence)
  return (
    <Badge
      variant={variant}
      aria-label={t(POOL_EVIDENCE_LABELS[props.claim.evidence])}
    >
      {t(POOL_EVIDENCE_LABELS[props.claim.evidence])}
    </Badge>
  )
}
