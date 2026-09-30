import { apiClient } from './client'
import type { UsageQueryParams } from '@/types/user'

export type PendingSource = 'receipt' | 'upstream_job'
export interface PendingSettlement {
  source: PendingSource
  receipt_id: number
  request_id: string
  api_key_id: number
  requested_model: string
  upstream_model: string | null
  upstream_response_model: string | null
  completion_status: string
  input_tokens: number | null
  output_tokens: number | null
  pricing_state: 'unpriced' | 'error'
  state: 'needs_review'
  amount: null
  amount_state: 'pending_pricing'
  created_at: string
}
export interface PendingSettlementPage {
  items: PendingSettlement[]
  total: number
  next_cursor: number
}
export async function listPendingSettlement(filters: UsageQueryParams, source: PendingSource, cursor = 0) {
  const { data } = await apiClient.get<PendingSettlementPage>('/usage/pending-settlement', {
    params: { source, cursor, page_size: 20, start_date: filters.start_date, end_date: filters.end_date,
      timezone: filters.timezone, model: filters.model, api_key_id: filters.api_key_id },
  })
  return data
}
