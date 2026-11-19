import { apiClient } from '@/api/client'

export interface PublicRate {
  amount: string
  currency: string
  per: string
  unit: string
}

export interface PublicModelSummary {
  id: string
  model: string
  provider?: string | null
  context_tokens?: string | null
  modalities?: string[] | null
  input_rate?: PublicRate | null
  output_rate?: PublicRate | null
  route_state?: string | null
}

export interface PublicModelPage {
  items: PublicModelSummary[]
  next_cursor?: string | null
  as_of?: string | null
  catalog_version?: string | null
}

export interface PublicModelDetail {
  model: PublicModelSummary
  model_facts?: Array<{
    state?: string
    tools?: boolean | null
    reasoning?: boolean | null
  }>
  capabilities?: Array<{ action?: string; protocol?: string; state?: string }> | null
  metrics?: { ttft_p50_ms?: number | null }
}

export interface PublicCompareResponse {
  catalog_version?: string | null
  as_of?: string | null
  rows: Array<{
    model_id: string
    metrics?: {
      success_rate?: number | null
      ttft_p50_ms?: number | null
      output_tokens_per_second?: number | null
      requests?: string | null
    } | null
  }>
}

export interface PublicStatusComponent {
  id: string
  name?: string
  observed_state?: string
  last_success?: string | null
  success_rate?: number | null
  sample_count?: string | null
}

export interface PublicStatusResponse {
  components?: PublicStatusComponent[]
  as_of?: string | null
  overall?: string
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
