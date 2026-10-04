import { apiClient } from '../client'
import type { CompatibleUsageReason } from './cnProviders'

export const atlasTokenFields = ['input', 'output', 'total', 'cache_creation', 'cache_creation_1h', 'cache_read', 'input_image', 'output_image'] as const
export interface AtlasUsageWindow { start_date: string; end_date: string }
export interface AtlasUsageBucket {
  object: 'usage.bucket'
  date: string
  start_at: string
  end_at: string
  covered_until: string
  partial: boolean
  results: {
    object: 'usage.result'
    usage: {
      requests: number
      tokens: Record<typeof atlasTokenFields[number], number> | null
      images: { count: number } | null
      video: { seconds: number } | null
    }
  }[]
}
export interface AtlasUsageResult {
  provider: 'atlascloud'
  kind: 'consumed_usage' | 'unsupported'
  success: boolean
  unsupported?: boolean
  reason?: CompatibleUsageReason
  status_code?: number
  fetched_at: number
  persisted: boolean
  usage?: AtlasUsageWindow & {
    scope: 'self'
    data: AtlasUsageBucket[]
    has_more: boolean
    next_page: string | null
    pages_fetched: number
    partial: boolean
  }
}

export async function queryAtlasUsage(id: number, params: AtlasUsageWindow): Promise<AtlasUsageResult> {
  const { data } = await apiClient.get<AtlasUsageResult>(`/admin/cn-providers/accounts/${id}/usage`, {
    // Atlas accepts UTC dates only. Skip the shared GET timezone default; Axios omits undefined.
    params: { ...params, timezone: undefined }
  })
  return data
}
