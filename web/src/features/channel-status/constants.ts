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
export const statusColors: Record<string, string> = {
  normal: 'bg-emerald-600 text-white',
  degraded: 'bg-amber-500 text-black',
  error: 'bg-red-600 text-white',
  unknown: 'bg-muted text-muted-foreground',
  skipped: 'bg-muted text-muted-foreground',
  blocked: 'bg-amber-500 text-black',
}

export const probeStatusKeys: Record<string, string> = {
  normal: 'monitor.normal',
  degraded: 'monitor.degraded',
  error: 'Failed',
  unknown: 'monitor.unknown',
  skipped: 'Not executed',
  blocked: 'Blocked',
}

export const probeReasonKeys: Record<string, string> = {
  MONITOR_PAUSED: 'Monitoring is paused or this target is not enabled',
  MONITOR_BUDGET_OR_CONCURRENCY:
    'Budget, concurrency or minimum interval limit reached',
  MONITOR_COST_UNVERIFIED: 'Supplier cost evidence is missing or expired',
  MONITOR_COST_EXCEEDED: 'Supplier cost exceeds the configured ceiling',
  MONITOR_METERING_FAILED: 'Upstream usage could not be verified',
  MONITOR_TARGET_DENIED: 'Target authorization denied',
  MONITOR_INTEGRATION_PENDING: 'Target transport is not configured',
  MONITOR_LEASE_UNRESOLVED: 'Previous upstream execution remains unresolved',
  probe_transport_unresolved:
    'Transport failed; upstream outcome remains unconfirmed',
  probe_failed: 'monitor.probeFailed',
  probe_timeout: 'Probe timed out',
  probe_incomplete: 'Upstream response was incomplete',
  authorization_denied: 'Target authorization denied',
  invalid_result: 'Invalid upstream result',
}
