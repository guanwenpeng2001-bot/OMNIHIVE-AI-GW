import { apiClient } from '../client'
export interface QianwenPricingOptions {
  region: string
  usd_per_cny: number
  fx_source: string
  fx_effective_at: string
  group_id?: number
}
export interface QianwenPricingResponse {
  written: number
  group_id: number | null
  result: {
    status: string
    reason?: string
    imported: number
    matched: number
    fetched: number
    source_url?: string
    fetched_at: string
    models: Array<{ model: string; status: string; reason?: string; pricing?: unknown; prices: unknown }>
    [key: string]: unknown
  }
}
export async function syncQianwenPricing(id: number, options: QianwenPricingOptions): Promise<QianwenPricingResponse> {
  const { data } = await apiClient.post<QianwenPricingResponse>(`/admin/accounts/${id}/pricing/sync-qianwen`, options)
  return data
}
