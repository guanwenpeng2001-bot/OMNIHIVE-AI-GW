import type { components } from '@/api/generated/openapi'
import { apiClient, buildGatewayUrl } from '@/api/client'

export type CatalogModelRate = components['schemas']['CatalogRate']

export interface CatalogModelItem {
  id: string
  slug: string
  displayName: string
  provider: string
  actions: string[]
  protocols: string[]
  modalities: string[]
  maxOutputTokens: number | null
  contextTokens: number | null
  toolsSupported: boolean
  reasoningSupported: boolean
  inputRate: CatalogModelRate | null
  outputRate: CatalogModelRate | null
  priceState?: 'priced' | 'free' | 'unpriced' | 'error'
  accessState?: 'allowed' | 'denied' | 'unknown' | 'requires_scope'
  routeState?: 'eligible' | 'ineligible' | 'unknown' | 'requires_scope'
  availabilityState?: 'live' | 'catalog_only' | string
  catalogVersion?: string
  billingState?: components['schemas']['ModelSummary']['billing_state']
}

export type CatalogModelDetail = components['schemas']['ModelDetail']
export type CatalogCompareInput = components['schemas']['CatalogCompareInput']
export type CatalogCompareResult = components['schemas']['CatalogCompareResult']

// Wire fields come from OpenAPI. The named legacy projection only accepts
// historical spellings at this boundary; no alternate live data source exists.
type RawCatalogModel = Partial<components['schemas']['ModelSummary']> & {
  slug?: string
  canonical_slug?: string
  name?: string
  supported_protocols?: string[]
  facts?: {
    context_tokens?: number | string | null
    max_output_tokens?: number | string | null
    tools?: boolean | string | null
    reasoning?: boolean | string | null
    modalities?: string[]
  }
  pricing?: { rates?: CatalogModelRate[]; input?: CatalogModelRate | null; output?: CatalogModelRate | null }
  rates?: CatalogModelRate[]
}

interface RawCatalogPage {
  items?: RawCatalogModel[]
  next_cursor?: string | null
}

function v2Url(path: string): string {
  return buildGatewayUrl(path.startsWith('/api/v2') ? path : `/api/v2${path}`)
}

function normalizeRateList(raw: RawCatalogModel): {
  inputRate: CatalogModelRate | null
  outputRate: CatalogModelRate | null
} {
  const rates = raw.pricing?.rates ?? raw.rates ?? []
  const inputRate =
    raw.input_rate ??
    raw.pricing?.input ??
    rates.find((r) => r.unit === 'input_token' || r.unit === 'prompt_token') ??
    null
  const outputRate =
    raw.output_rate ??
    raw.pricing?.output ??
    rates.find((r) => r.unit === 'output_token' || r.unit === 'completion_token') ??
    null
  return { inputRate, outputRate }
}

function parseOptionalPositiveInt(val: number | string | null | undefined): number | null {
  if (typeof val === 'number' && Number.isSafeInteger(val) && val > 0) return val
  if (typeof val === 'string' && val.trim() !== '') {
    const parsed = Number(val)
    if (Number.isSafeInteger(parsed) && parsed > 0) return parsed
  }
  return null
}

export function normalizeCatalogModel(raw: RawCatalogModel): CatalogModelItem | null {
  const slug = (raw.slug || raw.canonical_slug || raw.model || raw.id || '').trim()
  const id = (raw.id || slug).trim()
  if (!id) return null
  const displayName = (raw.display_name || raw.name || slug || id).trim()
  const protocols = raw.protocols ?? raw.supported_protocols ?? []
  const modalities = raw.modalities ?? raw.facts?.modalities ?? []
  const actions = raw.actions ?? []
  const { inputRate, outputRate } = normalizeRateList(raw)
  const maxOutputTokens = parseOptionalPositiveInt(raw.facts?.max_output_tokens)
  const contextTokens =
    parseOptionalPositiveInt(raw.context_tokens) ??
    parseOptionalPositiveInt(raw.facts?.context_tokens)

  return {
    id,
    slug: slug || id,
    displayName,
    provider: (raw.provider || 'gateway').trim(),
    actions: Array.isArray(actions) ? actions : [],
    protocols: Array.isArray(protocols) ? protocols : [],
    modalities: Array.isArray(modalities) ? modalities : [],
    maxOutputTokens,
    contextTokens,
    toolsSupported: raw.facts?.tools === true || raw.facts?.tools === 'supported',
    reasoningSupported: raw.facts?.reasoning === true || raw.facts?.reasoning === 'supported',
    inputRate,
    outputRate,
    priceState: raw.price_state,
    accessState: raw.access_state,
    routeState: raw.route_state,
    availabilityState: raw.availability_state,
    catalogVersion: raw.catalog_version,
    billingState: raw.billing_state
  }
}

async function listPages(path: string, params: Record<string, unknown>, signal?: AbortSignal): Promise<CatalogModelItem[]> {
  const models = new Map<string, CatalogModelItem>()
  const seen = new Set<string>()
  let cursor: string | undefined
  do {
    const { data } = await apiClient.get<RawCatalogPage>(v2Url(path), {
      baseURL: '', params: { ...params, cursor }, paramsSerializer: { indexes: null }, signal
    })
    for (const raw of data?.items ?? []) {
      const model = normalizeCatalogModel(raw)
      if (model) models.set(model.id, model)
    }
    cursor = data?.next_cursor || undefined
    if (cursor) {
      if (seen.has(cursor) || seen.size >= 100) throw new Error('CATALOG_PAGINATION_INCOMPLETE')
      seen.add(cursor)
    }
  } while (cursor)
  return [...models.values()]
}

function requireProject(workspaceId: string, projectId: string) {
  if (!/^[1-9]\d*$/.test(workspaceId) || !/^[1-9]\d*$/.test(projectId)) {
    throw Object.assign(new Error('Select an authorized project to load its model catalog.'), { reason: 'PROJECT_SCOPE_REQUIRED' })
  }
}

export const catalogApi = {
  async listScopedModels(
    workspaceId: string,
    projectId: string,
    params?: { q?: string; action?: string[]; modality?: string[]; limit?: number; accessible?: boolean | null },
    signal?: AbortSignal
  ): Promise<CatalogModelItem[]> {
    requireProject(workspaceId, projectId)
    return listPages(`/workspaces/${workspaceId}/projects/${projectId}/models`, {
      ...params, limit: params?.limit ?? 100, accessible: params?.accessible === null ? undefined : (params?.accessible ?? true)
    }, signal)
  },

  async getModelDiagnostics(
    workspaceId: string,
    projectId: string,
    modelId: string,
    signal?: AbortSignal
  ): Promise<components['schemas']['AdminModelDiagnostics']> {
    requireProject(workspaceId, projectId)
    const { data } = await apiClient.get<components['schemas']['AdminModelDiagnostics']>(
      v2Url(`/workspaces/${workspaceId}/projects/${projectId}/model-diagnostics/${encodeURIComponent(modelId)}`),
      { baseURL: '', signal }
    )
    return data
  },

  async listPublicModels(
    params?: { q?: string; modality?: string[]; limit?: number },
    signal?: AbortSignal
  ): Promise<CatalogModelItem[]> {
    return listPages('/public/models', { ...params, limit: params?.limit ?? 100 }, signal)
  },

  async getProjectModel(
    workspaceId: string,
    projectId: string,
    modelId: string,
    params?: { catalog_version?: string },
    signal?: AbortSignal
  ): Promise<CatalogModelDetail> {
    requireProject(workspaceId, projectId)
    const { data } = await apiClient.get<CatalogModelDetail>(
      v2Url(`/workspaces/${workspaceId}/projects/${projectId}/models/${encodeURIComponent(modelId)}`),
      { baseURL: '', params, signal }
    )
    return data
  },

  async compareProjectModels(
    workspaceId: string,
    projectId: string,
    input: CatalogCompareInput,
    signal?: AbortSignal
  ): Promise<CatalogCompareResult> {
    requireProject(workspaceId, projectId)
    const { data } = await apiClient.post<CatalogCompareResult>(
      v2Url(`/workspaces/${workspaceId}/projects/${projectId}/models/compare`),
      input,
      { baseURL: '', signal }
    )
    return data
  }
}
