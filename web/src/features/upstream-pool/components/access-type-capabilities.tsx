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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import type { PoolClaim, PoolRunStatus } from '../types'
import {
  PoolClaimStatusBadge,
  PoolEvidenceBadge,
  PoolRunStatusBadge,
} from './pool-badges'

function OpBadges(props: { ops: string[] }) {
  if (props.ops.length === 0) {
    return <span className='text-muted-foreground text-xs'>-</span>
  }
  return (
    <div className='flex flex-wrap gap-1'>
      {props.ops.map((op) => (
        <Badge key={op} variant='outline' className='font-mono font-normal'>
          {op}
        </Badge>
      ))}
    </div>
  )
}

function ClaimsTable(props: { claims: PoolClaim[] }) {
  const { t } = useTranslation()
  if (props.claims.length === 0) {
    return (
      <p className='text-muted-foreground text-sm'>
        {t('No claims recorded.')}
      </p>
    )
  }
  return (
    <div className='overflow-x-auto'>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('Claim')}</TableHead>
            <TableHead>{t('Status')}</TableHead>
            <TableHead>{t('Evidence')}</TableHead>
            <TableHead>{t('Note')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {props.claims.map((claim) => (
            <TableRow key={claim.key}>
              <TableCell className='font-mono text-xs break-all'>
                {claim.key}
              </TableCell>
              <TableCell>
                <PoolClaimStatusBadge claim={claim} />
              </TableCell>
              <TableCell>
                <PoolEvidenceBadge claim={claim} />
              </TableCell>
              <TableCell className='text-muted-foreground max-w-72 text-xs break-all whitespace-normal'>
                {claim.note ?? '-'}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

/**
 * Capabilities of one platform + access type: operations, auth methods and the
 * claim/evidence table. The disabled reason is server-authored Chinese and is
 * rendered verbatim.
 */
export function AccessTypeCapabilities(props: {
  platformLabel: string
  platform: string
  accessType: {
    type: string
    label: string
    executor_id: string
    auth_methods: string[]
    supported_ops: string[]
    unsupported_ops: string[]
    disabled: boolean
    disabled_reason?: string
    claims: PoolClaim[]
  } | null
  accountStatus?: PoolRunStatus
}) {
  const { t } = useTranslation()
  const at = props.accessType
  return (
    <div className='space-y-3'>
      <div className='flex flex-wrap items-center gap-2'>
        <span className='text-sm font-semibold'>
          {props.platformLabel || props.platform}
        </span>
        <Badge variant='secondary' className='font-mono font-normal'>
          {props.platform}
        </Badge>
        {at ? (
          <>
            <Badge variant='outline' className='font-mono font-normal'>
              {at.type}
            </Badge>
            <Badge variant='outline'>{at.label}</Badge>
          </>
        ) : null}
      </div>
      {at ? (
        <>
          <dl className='grid gap-2 text-sm sm:grid-cols-2'>
            <div>
              <dt className='text-muted-foreground'>{t('Executor')}</dt>
              <dd className='font-mono text-xs'>{at.executor_id || '-'}</dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>{t('Auth methods')}</dt>
              <dd>
                <OpBadges ops={at.auth_methods} />
              </dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>
                {t('Supported operations')}
              </dt>
              <dd>
                <OpBadges ops={at.supported_ops} />
              </dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>
                {t('Unsupported operations')}
              </dt>
              <dd>
                <OpBadges ops={at.unsupported_ops} />
              </dd>
            </div>
          </dl>
          {at.disabled ? (
            <div className='border-warning/40 bg-warning/10 text-warning rounded-md border px-3 py-2 text-sm'>
              {t('Disabled')}
              {at.disabled_reason ? `: ${at.disabled_reason}` : ''}
            </div>
          ) : null}
          <h4 className='text-sm font-semibold'>{t('Claims')}</h4>
          <ClaimsTable claims={at.claims} />
        </>
      ) : (
        <p className='text-muted-foreground text-sm'>
          {t(
            'This access type is not part of the current catalog for this platform.'
          )}
        </p>
      )}
      {props.accountStatus ? (
        <p className='text-muted-foreground flex items-center gap-2 text-xs'>
          {t('Account status')}:
          <PoolRunStatusBadge status={props.accountStatus} />
        </p>
      ) : null}
    </div>
  )
}

export { ClaimsTable }
