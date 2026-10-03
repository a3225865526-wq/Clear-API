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

// ============================================================================
// Store recharge admin API: StoreProduct + VIPLevel CRUD
// ============================================================================

export interface StoreProduct {
  id: number
  name: string
  face_value_cents: number
  store_url: string
  enabled: boolean
  sort: number
  description: string
  created_time?: number
  updated_time?: number
}

export interface VipLevel {
  id: number
  name: string
  threshold_cents: number
  bonus_basis_points: number
  enabled: boolean
  sort: number
  created_time?: number
  updated_time?: number
}

export interface AdminApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

async function list<T>(path: string): Promise<T[]> {
  const res = await api.get<AdminApiResponse<T[]>>(path)
  const payload = res.data
  if (payload && payload.success && Array.isArray(payload.data)) {
    return payload.data
  }
  return []
}

export function getStoreProducts(): Promise<StoreProduct[]> {
  return list<StoreProduct>('/api/store/products')
}

export function createStoreProduct(
  product: Omit<StoreProduct, 'id' | 'created_time' | 'updated_time'>
): Promise<AdminApiResponse<StoreProduct>> {
  return api.post<AdminApiResponse<StoreProduct>>('/api/store/products', product)
    .then((res) => res.data)
}

export function updateStoreProduct(
  product: StoreProduct
): Promise<AdminApiResponse<StoreProduct>> {
  return api.put<AdminApiResponse<StoreProduct>>('/api/store/products', product)
    .then((res) => res.data)
}

export function deleteStoreProduct(id: number): Promise<AdminApiResponse> {
  return api.delete<AdminApiResponse>(`/api/store/products/${id}`)
    .then((res) => res.data)
}

export function getVipLevels(): Promise<VipLevel[]> {
  return list<VipLevel>('/api/vip_levels/')
}

export function createVipLevel(
  level: Omit<VipLevel, 'id' | 'created_time' | 'updated_time'>
): Promise<AdminApiResponse<VipLevel>> {
  return api.post<AdminApiResponse<VipLevel>>('/api/vip_levels/', level)
    .then((res) => res.data)
}

export function updateVipLevel(
  level: VipLevel
): Promise<AdminApiResponse<VipLevel>> {
  return api.put<AdminApiResponse<VipLevel>>('/api/vip_levels/', level)
    .then((res) => res.data)
}

export function deleteVipLevel(id: number): Promise<AdminApiResponse> {
  return api.delete<AdminApiResponse>(`/api/vip_levels/${id}`)
    .then((res) => res.data)
}
