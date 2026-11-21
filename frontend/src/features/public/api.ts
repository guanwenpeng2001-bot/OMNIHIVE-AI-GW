import { apiClient } from '@/api/client'

export interface PublicRate {
  amount: string
  currency: string
  per: string
  unit: string
}

export interface PublicCatalogEvidence {
  source: string
  maturity: 'unknown' | 'stable' | 'experimental' | 'deprecated' | string
  observed_at: string | null
  expires_at: string | null
  state: 'fresh' | 'stale' | 'unknown' | string
}

export interface PublicModelFacts {
  source?: string
  maturity?: 'unknown' | 'stable' | 'experimental' | 'deprecated' | string
  observed_at?: string | null
  expires_at?: string | null
  state?: 'fresh' | 'stale' | 'unknown' | string
  context_tokens?: string | null
  max_output_tokens?: string | null
  embedding_dimensions?: string | null
  context_kind?: 'input' | 'total' | 'unspecified' | '' | string
  tools?: boolean | null
  vision?: boolean | null
  reasoning?: boolean | null
  prompt_caching?: boolean | null
  pdf?: boolean | null
  modalities?: string[] | null
}

export interface PublicCatalogCapability {
  verified_extension_actions?: string[]
  source?: string
  maturity?: 'unknown' | 'stable' | 'experimental' | 'deprecated' | string
  observed_at?: string | null
  expires_at?: string | null
  state?: 'fresh' | 'stale' | 'unknown' | string
  action?: string
  protocol?: string
  mode?: 'native' | 'bridge' | 'unknown' | string
  tools?: 'supported' | 'unsupported' | 'unknown' | string
  stream?: 'supported' | 'unsupported' | 'unknown' | string
  verified_at?: string | null
  adapter_version?: string | null
  limitations?: string[]
}

export interface PublicCatalogEndpoint {
  id: string
  provider: string
  training: string
  content_retention: string
  regions: string[]
  safety_retention_days: string | null
  region_evidence?: PublicCatalogEvidence | null
  privacy_evidence?: PublicCatalogEvidence | null
  capabilities?: PublicCatalogCapability[]
}

export interface PublicRateBand {
  rate: PublicRate
  kind: string
  min_tokens: string | null
  max_tokens: string | null
  label?: string
}

export interface PublicTimeBand {
  timezone: string
  start_time: string
  end_time: string
  weekdays_only: boolean
  multiplier: string
}

export interface PublicCatalogMetrics {
  window?: string | null
  window_start?: string
  window_end?: string
  source?: string
  requests?: string | null
  ttft_samples?: string | null
  speed_samples?: string | null
  success_rate?: number | null
  ttft_p50_ms?: number | null
  output_tokens_per_second?: number | null
  state?: 'insufficient_samples' | 'partial' | 'available' | 'unavailable' | 'withheld' | string
}

export interface PublicCatalogBenchmark {
  benchmark_id: string
  unit: string
  source: string
  value: string
  verified_at: string
}

export interface PublicModelCompatibility {
  action: string
  protocol: string
  account_mode: string
  adapter_version: string
  maturity: 'candidate' | 'experimental' | 'stable' | 'deprecated' | 'closed' | string
  publication_version: string
  published_at: string
}

export interface PublicModelSummary {
  id: string
  model: string
  display_name?: string
  provider?: string | null
  catalog_version?: string
  context_tokens?: string | null
  modalities?: string[] | null
  protocols?: string[] | null
  actions?: string[] | null
  regions?: string[] | null
  maturity?: 'unknown' | 'stable' | 'experimental' | 'deprecated' | string | null
  catalog_state?: string | null
  access_state?: 'allowed' | 'denied' | 'unknown' | 'requires_scope' | string | null
  input_rate?: PublicRate | null
  output_rate?: PublicRate | null
  route_state?: string | null
  price_state?: 'priced' | 'free' | 'unpriced' | 'error' | string | null
  observed_at?: string | null
  evidence_state?: 'fresh' | 'stale' | 'unknown' | string | null
  metrics?: PublicCatalogMetrics | null
}

export interface PublicModelPage {
  items: PublicModelSummary[]
  next_cursor?: string | null
  as_of?: string | null
  catalog_version?: string | null
}

export interface PublicModelDetail {
  model: PublicModelSummary
  aliases?: string[]
  model_facts?: PublicModelFacts[]
  capabilities?: PublicCatalogCapability[] | null
  endpoints?: PublicCatalogEndpoint[]
  price_version?: string | null
  price_source?: string | null
  reference_price?: { source: string; input_rate: PublicRate; output_rate: PublicRate }
  additional_rates?: PublicRateBand[]
  context_basis?: string
  time_bands?: PublicTimeBand[]
  metrics?: PublicCatalogMetrics | null
  benchmarks?: PublicCatalogBenchmark[]
  compatibility?: PublicModelCompatibility[]
  documentation_url?: string | null
  redactions?: string[]
}

export interface PublicCatalogMoney {
  currency: string
  amount: string
}

export interface PublicCatalogEstimate {
  state: 'estimated' | 'unpriced' | string
  price_version?: string | null
  estimated_charge?: PublicCatalogMoney | null
  components?: Array<{ kind: string; quantity: string; raw_amount: string }>
  rounding_adjustment?: string | null
}

export interface PublicCompareInput {
  model_ids: string[]
  protocol?: string
  action?: string
  scenario?: {
    input_tokens?: string
    cached_input_tokens?: string
    output_tokens?: string
    requests?: string
  }
}

export interface PublicCompareRow {
  model_id: string
  capabilities?: {
    tools?: 'supported' | 'unsupported' | 'unknown' | string
    stream?: 'supported' | 'unsupported' | 'unknown' | string
  } | null
  metrics?: PublicCatalogMetrics | null
  benchmarks?: PublicCatalogBenchmark[] | null
  quote?: PublicCatalogEstimate | null
}

export interface PublicCompareResponse {
  catalog_version?: string | null
  as_of?: string | null
  rows: PublicCompareRow[]
}

export type PublicStatusWindow = '1h' | '24h' | '7d' | '30d'

export interface PublicStatusDaily {
  date: string
  state: string
  reason?: string | null
  sample_count: string
  success_rate?: number | null
}

export interface PublicStatusComponent {
  id: string
  name?: string
  state?: string
  observed_state?: string
  reason?: string | null
  source?: 'gateway_observations' | 'unavailable' | string
  freshness?: 'fresh' | 'stale' | 'unknown' | string
  stale_after?: string | null
  last_success?: string | null
  success_rate?: number | null
  sample_count?: string | null
  minimum_sample_count?: string | null
  ttft_sample_count?: string | null
  ttft_p50_ms?: number | null
  ttft_p95_ms?: number | null
  stream_completion_rate?: number | null
  counts?: Record<string, string>
  history?: PublicStatusDaily[]
}

export interface PublicStatusIncidentSummary {
  id: string
  title: string
  state: 'investigating' | 'identified' | 'monitoring' | 'resolved' | string
  severity: 'degraded' | 'outage' | 'maintenance' | string
  component_ids: string[]
  started_at: string
  resolved_at: string | null
}

export interface PublicStatusIncident extends PublicStatusIncidentSummary {
  updates: Array<{
    at: string
    state: 'investigating' | 'identified' | 'monitoring' | 'resolved' | string
    message: string
  }>
}

export interface PublicIncidentPage {
  items: PublicStatusIncidentSummary[]
  next_cursor?: string | null
  as_of?: string | null
}

export interface PublicStatusResponse {
  components?: PublicStatusComponent[]
  as_of?: string | null
  window_start?: string | null
  window_end?: string | null
  data_lag_seconds?: number | null
  overall?: string
  incidents?: PublicStatusIncidentSummary[]
  active_incident_count?: string | null
  definitions?: {
    success_rate?: string
    stream_completion_rate?: string
    ttft?: string
    excluded_categories?: string[]
    reported_categories?: string[]
    aggregation_interval_seconds?: number
    publication_delay_seconds?: number
  }
}

export interface ListParams {
  q?: string
  provider?: string[]
  protocol?: string[]
  action?: string[]
  modality?: string[]
  region?: string[]
  retention?: string[]
  stability?: string[]
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
    region: params.region?.length ? params.region : undefined,
    retention: params.retention?.length ? params.retention : undefined,
    stability: params.stability?.length ? params.stability : undefined,
    sort: params.sort,
    cursor: params.cursor || undefined,
    limit: params.limit ?? 20
  }
}

function withoutImplicitTimezone(params: Record<string, unknown> = {}): Record<string, unknown> {
  return { ...params, timezone: undefined }
}

const STABLE_MODEL_ID_RE = /^m_[0-9a-f]{32}$/

function normalizeModelIdentifier(raw: string): string {
  const trimmed = String(raw ?? '').trim().replace(/^\/+|\/+$/g, '')
  if (!trimmed.includes('%')) return trimmed
  try {
    return decodeURIComponent(trimmed).trim().replace(/^\/+|\/+$/g, '')
  } catch {
    return trimmed
  }
}

function isNotFoundError(err: unknown): boolean {
  if (!err || typeof err !== 'object') return false
  const candidate = err as {
    status?: number
    code?: number | string
    reason?: string
    response?: { status?: number }
  }
  return (
    candidate.status === 404 ||
    candidate.code === 404 ||
    candidate.code === 'NOT_FOUND' ||
    candidate.reason === 'NOT_FOUND' ||
    candidate.response?.status === 404
  )
}

function matchesCanonicalModelSlug(item: PublicModelSummary, idOrSlug: string): boolean {
  if (item.id === idOrSlug || item.model === idOrSlug) return true
  if (item.provider && `${item.provider}/${item.model}` === idOrSlug) return true
  return false
}

async function fetchPublicModelDirect(
  idOrSlug: string,
  catalogVersion?: string
): Promise<PublicModelDetail> {
  const { data } = await apiClient.get(
    `/api/v2/public/models/${encodeURIComponent(idOrSlug)}`,
    {
      params: withoutImplicitTimezone(catalogVersion ? { catalog_version: catalogVersion } : {})
    }
  )
  return (data?.data ?? data) as PublicModelDetail
}

export async function listPublicModels(params: ListParams): Promise<PublicModelPage> {
  const { data } = await apiClient.get('/api/v2/public/models', {
    params: withoutImplicitTimezone(arrayParams(params)),
    paramsSerializer: { indexes: null }
  })
  return (data?.data ?? data) as PublicModelPage
}

export async function getPublicModel(
  idOrSlug: string,
  catalogVersion?: string
): Promise<PublicModelDetail> {
  const target = normalizeModelIdentifier(idOrSlug)
  try {
    return await fetchPublicModelDirect(target, catalogVersion)
  } catch (err) {
    if (STABLE_MODEL_ID_RE.test(target) || !isNotFoundError(err)) {
      throw err
    }

    const primaryPage = await listPublicModels({ q: target, limit: 50 })
    let matched = (primaryPage.items ?? []).find((item) =>
      matchesCanonicalModelSlug(item, target)
    )

    if (!matched && target.includes('/')) {
      const slashIndex = target.indexOf('/')
      const modelTail = target.slice(slashIndex + 1).trim()
      if (modelTail) {
        const tailPage = await listPublicModels({ q: modelTail, limit: 50 })
        matched = (tailPage.items ?? []).find((item) =>
          matchesCanonicalModelSlug(item, target)
        )
      }
    }

    if (matched?.id && matched.id !== target) {
      return fetchPublicModelDirect(matched.id, catalogVersion)
    }
    throw err
  }
}

export async function comparePublicModels(
  modelIds: string[],
  options?: Omit<PublicCompareInput, 'model_ids'>
): Promise<PublicCompareResponse> {
  const payload: PublicCompareInput = {
    model_ids: modelIds,
    ...(options?.protocol ? { protocol: options.protocol } : {}),
    ...(options?.action ? { action: options.action } : {}),
    ...(options?.scenario ? { scenario: options.scenario } : {})
  }
  const { data } = await apiClient.post('/api/v2/public/models/compare', payload)
  return (data?.data ?? data) as PublicCompareResponse
}

export async function getPublicStatus(window?: PublicStatusWindow): Promise<PublicStatusResponse> {
  const { data } = await apiClient.get('/api/v2/public/status', {
    params: withoutImplicitTimezone(window ? { window } : {})
  })
  return (data?.data ?? data) as PublicStatusResponse
}

export async function listPublicIncidents(params?: {
  from?: string
  to?: string
  cursor?: string
  limit?: number
}): Promise<PublicIncidentPage> {
  const { data } = await apiClient.get('/api/v2/public/status/incidents', {
    params: withoutImplicitTimezone({
      from: params?.from || undefined,
      to: params?.to || undefined,
      cursor: params?.cursor || undefined,
      limit: params?.limit ?? 20
    })
  })
  return (data?.data ?? data) as PublicIncidentPage
}

export async function getPublicIncident(id: string): Promise<PublicStatusIncident> {
  const { data } = await apiClient.get(`/api/v2/public/status/incidents/${encodeURIComponent(id)}`, {
    params: withoutImplicitTimezone()
  })
  return (data?.data ?? data) as PublicStatusIncident
}
