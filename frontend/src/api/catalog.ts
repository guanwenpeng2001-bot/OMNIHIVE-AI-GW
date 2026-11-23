import { apiClient } from '@/api/client'
import { normalizeApiError } from '@/utils/apiError'
import type {
  CatalogCompareInput,
  CatalogCompareResult,
  CatalogListParams,
  CatalogModelDetail,
  CatalogModelPage,
  CatalogModelSummary,
  ExactQuote,
  ExactQuoteInput
} from '@/features/catalog/contract'

export interface CatalogRequestOptions {
  signal?: AbortSignal
}

const STABLE_MODEL_ID_RE = /^m_[0-9a-f]{32}$/

export function isSnapshotExpiredError(err: unknown): boolean {
  const normalized = normalizeApiError(err)
  if (normalized.status === 409 || normalized.status === 410) {
    return true
  }
  const reason = String(normalized.reason || '').toUpperCase()
  return reason === 'SNAPSHOT_EXPIRED'
}

export function isCatalogAbortError(err: unknown): boolean {
  if (!err || typeof err !== 'object') return false
  const candidate = err as { code?: string; name?: string }
  return (
    candidate.code === 'ERR_CANCELED' ||
    candidate.name === 'CanceledError' ||
    candidate.name === 'AbortError' ||
    candidate.code === 'AUTH_SESSION_CHANGED'
  )
}

function withoutImplicitTimezone(params: Record<string, unknown> = {}): Record<string, unknown> {
  return { ...params, timezone: undefined }
}

function unwrapEnvelope<T>(payload: unknown): T {
  if (
    payload &&
    typeof payload === 'object' &&
    'data' in payload &&
    'code' in payload &&
    (payload as { data?: unknown }).data !== undefined
  ) {
    return (payload as { data: T }).data
  }
  if (
    payload &&
    typeof payload === 'object' &&
    'data' in payload &&
    !('items' in payload) &&
    !('model' in payload) &&
    !('rows' in payload) &&
    (payload as { data?: unknown }).data !== undefined
  ) {
    return (payload as { data: T }).data
  }
  return payload as T
}

async function postCatalog<T>(
  url: string,
  payload: unknown,
  requestOptions?: CatalogRequestOptions
): Promise<T> {
  const { data } = requestOptions?.signal
    ? await apiClient.post(url, payload, { signal: requestOptions.signal })
    : await apiClient.post(url, payload)
  return unwrapEnvelope<T>(data)
}

function buildListQueryParams(params: CatalogListParams = {}): Record<string, unknown> {
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
    availability: params.availability || undefined,
    billing: params.billing || undefined,
    source: params.source?.length ? params.source : undefined,
    api_key_id: params.api_key_id || undefined,
    accessible: params.accessible !== undefined ? params.accessible : undefined,
    sort: params.sort || undefined,
    cursor: params.cursor || undefined,
    limit: params.limit ?? 20
  }
}

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

function matchesCanonicalModelSlug(item: CatalogModelSummary, idOrSlug: string): boolean {
  if (item.id === idOrSlug || item.model === idOrSlug) return true
  if (item.provider && `${item.provider}/${item.model}` === idOrSlug) return true
  return false
}

async function fetchPublicModelDirect(
  idOrSlug: string,
  catalogVersion?: string,
  requestOptions?: CatalogRequestOptions
): Promise<CatalogModelDetail> {
  const { data } = await apiClient.get(
    `/api/v2/public/models/${encodeURIComponent(idOrSlug)}`,
    {
      params: withoutImplicitTimezone(catalogVersion ? { catalog_version: catalogVersion } : {}),
      ...(requestOptions?.signal ? { signal: requestOptions.signal } : {})
    }
  )
  return unwrapEnvelope<CatalogModelDetail>(data)
}

export async function listPublicModels(
  params: CatalogListParams = {},
  requestOptions?: CatalogRequestOptions
): Promise<CatalogModelPage> {
  const { data } = await apiClient.get('/api/v2/public/models', {
    params: withoutImplicitTimezone(buildListQueryParams(params)),
    paramsSerializer: { indexes: null },
    ...(requestOptions?.signal ? { signal: requestOptions.signal } : {})
  })
  return unwrapEnvelope<CatalogModelPage>(data)
}

export async function getPublicModel(
  idOrSlug: string,
  catalogVersion?: string,
  requestOptions?: CatalogRequestOptions
): Promise<CatalogModelDetail> {
  const target = normalizeModelIdentifier(idOrSlug)
  try {
    return await fetchPublicModelDirect(target, catalogVersion, requestOptions)
  } catch (err) {
    if (STABLE_MODEL_ID_RE.test(target) || !isNotFoundError(err)) {
      throw err
    }

    const primaryPage = await listPublicModels({ q: target, limit: 50 }, requestOptions)
    let matched = (primaryPage.items ?? []).find((item) =>
      matchesCanonicalModelSlug(item, target)
    )

    if (!matched && target.includes('/')) {
      const slashIndex = target.indexOf('/')
      const modelTail = target.slice(slashIndex + 1).trim()
      if (modelTail) {
        const tailPage = await listPublicModels({ q: modelTail, limit: 50 }, requestOptions)
        matched = (tailPage.items ?? []).find((item) =>
          matchesCanonicalModelSlug(item, target)
        )
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
  options?: Omit<CatalogCompareInput, 'model_ids'>,
  requestOptions?: CatalogRequestOptions
): Promise<CatalogCompareResult> {
  const payload: CatalogCompareInput = {
    model_ids: modelIds,
    ...(options?.protocol ? { protocol: options.protocol } : {}),
    ...(options?.action ? { action: options.action } : {}),
    ...(options?.scenario ? { scenario: options.scenario } : {}),
    ...(options?.price_at ? { price_at: options.price_at } : {})
  }
  return postCatalog<CatalogCompareResult>('/api/v2/public/models/compare', payload, requestOptions)
}

export async function listProjectModels(
  workspaceId: string,
  projectId: string,
  params: CatalogListParams = {},
  requestOptions?: CatalogRequestOptions
): Promise<CatalogModelPage> {
  const { data } = await apiClient.get(
    `/api/v2/workspaces/${encodeURIComponent(workspaceId)}/projects/${encodeURIComponent(projectId)}/models`,
    {
      params: withoutImplicitTimezone(buildListQueryParams(params)),
      paramsSerializer: { indexes: null },
      ...(requestOptions?.signal ? { signal: requestOptions.signal } : {})
    }
  )
  return unwrapEnvelope<CatalogModelPage>(data)
}

export async function getProjectModel(
  workspaceId: string,
  projectId: string,
  idOrSlug: string,
  params?: { catalog_version?: string; api_key_id?: string },
  requestOptions?: CatalogRequestOptions
): Promise<CatalogModelDetail> {
  const target = normalizeModelIdentifier(idOrSlug)
  const { data } = await apiClient.get(
    `/api/v2/workspaces/${encodeURIComponent(workspaceId)}/projects/${encodeURIComponent(projectId)}/models/${encodeURIComponent(target)}`,
    {
      params: withoutImplicitTimezone({
        catalog_version: params?.catalog_version || undefined,
        api_key_id: params?.api_key_id || undefined
      }),
      ...(requestOptions?.signal ? { signal: requestOptions.signal } : {})
    }
  )
  return unwrapEnvelope<CatalogModelDetail>(data)
}

export async function compareProjectModels(
  workspaceId: string,
  projectId: string,
  input: CatalogCompareInput,
  requestOptions?: CatalogRequestOptions
): Promise<CatalogCompareResult> {
  return postCatalog<CatalogCompareResult>(
    `/api/v2/workspaces/${encodeURIComponent(workspaceId)}/projects/${encodeURIComponent(projectId)}/models/compare`,
    input,
    requestOptions
  )
}

export async function quoteProjectModel(
  workspaceId: string,
  projectId: string,
  input: ExactQuoteInput,
  requestOptions?: CatalogRequestOptions
): Promise<ExactQuote> {
  return postCatalog<ExactQuote>(
    `/api/v2/workspaces/${encodeURIComponent(workspaceId)}/projects/${encodeURIComponent(projectId)}/quotes`,
    input,
    requestOptions
  )
}

export const catalogClient = {
  listPublicModels,
  getPublicModel,
  comparePublicModels,
  listProjectModels,
  getProjectModel,
  compareProjectModels,
  quoteProjectModel,
  isSnapshotExpiredError,
  isCatalogAbortError
}
