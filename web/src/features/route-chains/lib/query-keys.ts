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

export const ROUTE_CHAINS_QUERY_KEY = ['route-chains'] as const

export const routeChainListKey = (includeArchived: boolean) => [
  ...ROUTE_CHAINS_QUERY_KEY,
  'list',
  { includeArchived },
]

export const routeChainDetailKey = (id: number) => [
  ...ROUTE_CHAINS_QUERY_KEY,
  'detail',
  { id },
]

export const routeChainAttemptsKey = (id: number, candidateId?: number) => [
  ...ROUTE_CHAINS_QUERY_KEY,
  'attempts',
  { id, candidateId },
]

export const routeChainDecisionsKey = (id: number, candidateId?: number) => [
  ...ROUTE_CHAINS_QUERY_KEY,
  'decisions',
  { id, candidateId },
]

export const ROUTE_CHAIN_TARGETS_QUERY_KEY = [
  ...ROUTE_CHAINS_QUERY_KEY,
  'targets',
] as const

export const ROUTE_CHAIN_GATES_QUERY_KEY = [
  ...ROUTE_CHAINS_QUERY_KEY,
  'gates',
] as const
