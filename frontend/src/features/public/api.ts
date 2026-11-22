import { apiClient } from '@/api/client'
import type { components } from '@/api/generated/openapi'
import { normalizeApiError } from '@/utils/apiError'

export interface PublicRequestOptions {
  signal?: AbortSignal
}

export function isSnapshotExpiredError(err: unknown): boolean {
  const normalized = normalizeApiError(err)
  if (normalized.status === 410) return true
  const reason = String(normalized.reason || '').toUpperCase()
  return reason === 'SNAPSHOT_EXPIRED'
}

// Generated schemas define transport facts. The optional projections below
// tolerate older servers during a rolling upgrade; omitted values stay unknown.
type Schema = components['schemas']
export type PublicRate = Schema['CatalogRate']
export type PublicCatalogEvidence = Schema['CatalogEvidence']
export type PublicModelFacts = Partial<Schema['CatalogModelFacts']>
export type PublicCatalogCapability = Partial<Schema['CatalogCapability']>
export type PublicCatalogEndpoint = Schema['CatalogEndpoint']
export type PublicRateBand = Schema['CatalogRateBand']
export type PublicTimeBand = Schema['CatalogTimeBand']
export type PublicCatalogMetrics = Schema['CatalogMetrics']
export type PublicCatalogBenchmark = Schema['CatalogBenchmark']
export type PublicModelCompatibility = Omit<Schema['CatalogModelCompatibility'], 'evidence' | 'deprecation'> &
  Partial<Pick<Schema['CatalogModelCompatibility'], 'evidence' | 'deprecation'>>
export type PublicModelSummary = Pick<Schema['ModelSummary'], 'id' | 'model'> &
  Partial<Omit<Schema['ModelSummary'], 'id' | 'model' | 'reference_price'>> & {
    reference_price?: Partial<Schema['CatalogReferencePrice']> | null
  }
export type PublicModelPage = Partial<Omit<Schema['CatalogModelPage'], 'items'>> & {
  items: PublicModelSummary[]
}
export type PublicModelDetail = Partial<Omit<Schema['ModelDetail'], 'model' | 'model_facts' | 'capabilities' | 'reference_price'>> & {
  model: PublicModelSummary
  model_facts?: PublicModelFacts[]
  capabilities?: PublicCatalogCapability[] | null
  reference_price?: Partial<Schema['CatalogReferencePrice']> | null
}
export type PublicCatalogMoney = Schema['CatalogMoney']
export type PublicCatalogEstimate = Partial<Schema['CatalogEstimate']>
export type PublicCompareInput = Schema['CatalogCompareInput']
export type PublicCompareRow = Pick<Schema['CatalogCompareRow'], 'model_id'> &
  Partial<Omit<Schema['CatalogCompareRow'], 'model_id' | 'quote'>> & {
    quote?: PublicCatalogEstimate | null
  }
export type PublicCompareResponse = Partial<Omit<Schema['CatalogCompareResult'], 'rows'>> & {
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
  availability_state?: 'live' | 'catalog_only' | 'requires_scope' | 'unknown' | 'unavailable' | 'eligible'
  publication_state?: 'published' | 'draft' | 'withdrawn'
  sort?: 'name' | 'price_input' | 'context' | 'ttft' | 'availability'
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
    availability_state: params.availability_state || undefined,
    publication_state: params.publication_state || undefined,
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

// Legacy links may contain a name rather than an immutable model ID. Resolve
// every matching page before choosing: names are not globally unique.
async function resolveLegacyModelName(
  target: string,
  query: string,
  options?: PublicRequestOptions
): Promise<PublicModelSummary | undefined> {
  const matches = new Map<string, PublicModelSummary>()
  const seenCursors = new Set<string>()
  let cursor: string | undefined
  do {
    const page = await listPublicModels({ q: query, limit: 100, cursor }, options)
    for (const model of page.items ?? []) {
      if (matchesCanonicalModelSlug(model, target)) matches.set(model.id, model)
    }
    if (matches.size > 1) {
      throw Object.assign(new Error('Model name is ambiguous; select a provider-qualified model.'), {
        status: 409,
        reason: 'MODEL_IDENTITY_AMBIGUOUS'
      })
    }
    cursor = page.next_cursor || undefined
    if (cursor) {
      if (seenCursors.has(cursor) || seenCursors.size >= 100) {
        throw Object.assign(new Error('Model lookup could not complete; retry from the directory.'), {
          reason: 'CATALOG_PAGINATION_INCOMPLETE'
        })
      }
      seenCursors.add(cursor)
    }
  } while (cursor)
  return matches.values().next().value
}

async function fetchPublicModelDirect(
  idOrSlug: string,
  catalogVersion?: string,
  requestOptions?: PublicRequestOptions
): Promise<PublicModelDetail> {
  const { data } = await apiClient.get(
    `/api/v2/public/models/${encodeURIComponent(idOrSlug)}`,
    {
      params: withoutImplicitTimezone(catalogVersion ? { catalog_version: catalogVersion } : {}),
      ...(requestOptions?.signal ? { signal: requestOptions.signal } : {})
    }
  )
  return (data?.data ?? data) as PublicModelDetail
}

export async function listPublicModels(
  params: ListParams,
  requestOptions?: PublicRequestOptions
): Promise<PublicModelPage> {
  const { data } = await apiClient.get('/api/v2/public/models', {
    params: withoutImplicitTimezone(arrayParams(params)),
    paramsSerializer: { indexes: null },
    ...(requestOptions?.signal ? { signal: requestOptions.signal } : {})
  })
  return (data?.data ?? data) as PublicModelPage
}

export async function getPublicModel(
  idOrSlug: string,
  catalogVersion?: string,
  requestOptions?: PublicRequestOptions
): Promise<PublicModelDetail> {
  const target = normalizeModelIdentifier(idOrSlug)
  try {
    return await fetchPublicModelDirect(target, catalogVersion, requestOptions)
  } catch (err) {
    if (STABLE_MODEL_ID_RE.test(target) || !isNotFoundError(err)) {
      throw err
    }

    let matched = await resolveLegacyModelName(target, target, requestOptions)

    if (!matched && target.includes('/')) {
      const slashIndex = target.indexOf('/')
      const modelTail = target.slice(slashIndex + 1).trim()
      if (modelTail) {
        matched = await resolveLegacyModelName(target, modelTail, requestOptions)
      }
    }

    if (matched?.id && matched.id !== target) {
      return fetchPublicModelDirect(matched.id, catalogVersion, requestOptions)
    }
    throw err
  }
}

export async function comparePublicModels(
  modelIds: string[],
  options?: Omit<PublicCompareInput, 'model_ids'>,
  requestOptions?: PublicRequestOptions
): Promise<PublicCompareResponse> {
  const payload: PublicCompareInput = {
    model_ids: modelIds,
    ...(options?.protocol ? { protocol: options.protocol } : {}),
    ...(options?.action ? { action: options.action } : {}),
    ...(options?.scenario ? { scenario: options.scenario } : {}),
    ...(options?.price_at ? { price_at: options.price_at } : {})
  }
  const { data } = requestOptions?.signal
    ? await apiClient.post('/api/v2/public/models/compare', payload, {
        signal: requestOptions.signal
      })
    : await apiClient.post('/api/v2/public/models/compare', payload)
  return (data?.data ?? data) as PublicCompareResponse
}

export async function getPublicStatus(
  window?: PublicStatusWindow,
  requestOptions?: PublicRequestOptions
): Promise<PublicStatusResponse> {
  const { data } = await apiClient.get('/api/v2/public/status', {
    params: withoutImplicitTimezone(window ? { window } : {}),
    ...(requestOptions?.signal ? { signal: requestOptions.signal } : {})
  })
  return (data?.data ?? data) as PublicStatusResponse
}

export async function listPublicIncidents(
  params?: {
    from?: string
    to?: string
    cursor?: string
    limit?: number
  },
  requestOptions?: PublicRequestOptions
): Promise<PublicIncidentPage> {
  const { data } = await apiClient.get('/api/v2/public/status/incidents', {
    params: withoutImplicitTimezone({
      from: params?.from || undefined,
      to: params?.to || undefined,
      cursor: params?.cursor || undefined,
      limit: params?.limit ?? 20
    }),
    ...(requestOptions?.signal ? { signal: requestOptions.signal } : {})
  })
  return (data?.data ?? data) as PublicIncidentPage
}

export async function getPublicIncident(
  id: string,
  requestOptions?: PublicRequestOptions
): Promise<PublicStatusIncident> {
  const { data } = await apiClient.get(`/api/v2/public/status/incidents/${encodeURIComponent(id)}`, {
    params: withoutImplicitTimezone(),
    ...(requestOptions?.signal ? { signal: requestOptions.signal } : {})
  })
  return (data?.data ?? data) as PublicStatusIncident
}
