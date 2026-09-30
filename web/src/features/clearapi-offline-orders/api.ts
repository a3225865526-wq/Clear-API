import { api } from '@/lib/api'

export type OfflineOrder = {
  id: number
  order_no: string
  currency: string
  money_cents: number
  quota_per_code: number
  code_count: number
  paid_at: number
  status: 'paid' | 'void'
  codes: { id: number; key: string; status: number }[]
}

export type OfflineOrderInput = {
  order_no: string
  money_cents: number
  quota_per_code: number
  count: number
  paid_at: number
  expired_time: number
}

export async function listOfflineOrders(
  page: number
): Promise<{ items: OfflineOrder[]; total: number }> {
  const response = await api.get(
    `/api/clearapi/offline-orders/?p=${page}&page_size=10`
  )
  if (!response.data.success) throw new Error(response.data.message)
  return {
    items: response.data.data.items ?? [],
    total: response.data.data.total ?? 0,
  }
}

export async function createOfflineOrder(
  input: OfflineOrderInput
): Promise<string[]> {
  const response = await api.post('/api/clearapi/offline-orders/', input)
  if (!response.data.success) throw new Error(response.data.message)
  return response.data.data.codes
}

export async function voidOfflineOrder(id: number): Promise<void> {
  const response = await api.post(`/api/clearapi/offline-orders/${id}/void`)
  if (!response.data.success) throw new Error(response.data.message)
}
