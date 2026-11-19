import { apiClient } from '@/api/client'

export interface PublicModelSummary {
  id: string
  model: string
  provider?: string | null
  canonical?: string | null
  context_window?: number | null
  modalities?: string[] | null
  input_price?: { amount: string; currency: string; per: string } | null
  output_price?: { amount: string; currency: string; per: string } | null
  ttft_ms?: number | null
  stability?: string | null
  route_state?: string | null
  updated_at?: string | null
}

export interface PublicModelPage {
  items: PublicModelSummary[]
  next_cursor?: string | null
  as_of?: string | null
  catalog_version?: string | null
}

export interface PublicModelDetail extends PublicModelSummary {
  facts?: Record<string, unknown> | null
  endpoints?: Array<Record<string, unknown>> | null
  capabilities?: Array<{ action?: string; protocol?: string; state?: string }> | null
  metrics?: Record<string, unknown> | null
  evidence?: Record<string, unknown> | null
}

export interface PublicCompareRow {
  model_id: string
  capabilities?: unknown
  quote?: unknown
  metrics?: Record<string, unknown> | null
  benchmarks?: unknown
}

export interface PublicCompareResponse {
  catalog_version?: string | null
  as_of?: string | null
  rows: PublicCompareRow[]
}

export interface PublicStatusComponent {
  id: string
  name?: string
  observed_state?: string
  last_success_at?: string | null
  success_rate?: number | null
  samples?: number | null
}

export interface PublicStatusResponse {
  components?: PublicStatusComponent[]
  incidents?: Array<Record<string, unknown>>
  as_of?: string | null
  stale?: boolean
}

export interface ListParams {
  q?: string
  provider?: string[]
  protocol?: string[]
  action?: string[]
  modality?: string[]
  sort?: 'name' | 'price_input' | 'context' | 'ttft'
  cursor?: string | null
  limit?: number
}

function arrayParams(params: ListParams): Record<string, unknown> {
  return {
    q: params.q || undefined,
    provider: params.provider?.length ? params.provider : undefined,
    protocol: params.protocol?.length ? params.protocol : undefined,
    action: params.action?.length ? params.action : undefined,
    modality: params.modality?.length ? params.modality : undefined,
    sort: params.sort,
    cursor: params.cursor || undefined,
    limit: params.limit ?? 20
  }
}

export async function listPublicModels(params: ListParams): Promise<PublicModelPage> {
  const { data } = await apiClient.get('/api/v2/public/models', {
    params: arrayParams(params),
    paramsSerializer: { indexes: null }
  })
  return (data?.data ?? data) as PublicModelPage
}

export async function getPublicModel(id: string): Promise<PublicModelDetail> {
  const { data } = await apiClient.get(`/api/v2/public/models/${encodeURIComponent(id)}`)
  return (data?.data ?? data) as PublicModelDetail
}

export async function comparePublicModels(modelIds: string[]): Promise<PublicCompareResponse> {
  const { data } = await apiClient.post('/api/v2/public/models/compare', { model_ids: modelIds })
  return (data?.data ?? data) as PublicCompareResponse
}

export async function getPublicStatus(): Promise<PublicStatusResponse> {
  const { data } = await apiClient.get('/api/v2/public/status')
  return (data?.data ?? data) as PublicStatusResponse
}
