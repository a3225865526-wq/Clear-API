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

import {
  StatusBadge,
  type StatusBadgeProps,
} from '@/components/status-badge'

import type { StateLabel } from '../constants'

// Every state renders as text + color — color alone never carries the state
// (§二十一).

interface MappedStatusBadgeProps extends Omit<StatusBadgeProps, 'label' | 'variant'> {
  stateLabel: StateLabel | undefined
  fallbackKey: string
}

export function MappedStatusBadge(props: MappedStatusBadgeProps) {
  const { stateLabel, fallbackKey, ...badgeProps } = props
  const { t } = useTranslation()
  return (
    <StatusBadge
      {...badgeProps}
      label={t(stateLabel?.labelKey ?? fallbackKey)}
      variant={stateLabel?.variant ?? 'neutral'}
      copyable={false}
    />
  )
}
