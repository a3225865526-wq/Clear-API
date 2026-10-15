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
import { ShieldAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'

import type { GateStates } from '../types'

// §二十三/§二十四: the console must show "configuration exists" and
// "runtime active" as different facts, and never offer a one-click way to
// bypass the production release gate.

export function GateBanner(props: { gates: GateStates }) {
  const { gates } = props
  const { t } = useTranslation()
  return (
    <Alert className='border-warning/40 bg-warning/10'>
      <ShieldAlert className='size-4' />
      <AlertTitle>{t('Route Chain production release gate is not approved')}</AlertTitle>
      <AlertDescription>
        <p>
          {t(
            'Configuration below is for preparation only. Runtime routing follows the feature gate, not this page.'
          )}
        </p>
        <div className='mt-2 flex flex-wrap items-center gap-2'>
          <Badge variant={gates.route_chain_enabled ? 'default' : 'outline'}>
            {gates.route_chain_enabled
              ? t('Runtime Route Chain: ON')
              : t('Runtime Route Chain: OFF')}
          </Badge>
          <Badge variant={gates.circuit_breaker_enabled ? 'default' : 'outline'}>
            {gates.circuit_breaker_enabled
              ? t('Circuit Breaker: ON')
              : t('Circuit Breaker: OFF')}
          </Badge>
        </div>
      </AlertDescription>
    </Alert>
  )
}
