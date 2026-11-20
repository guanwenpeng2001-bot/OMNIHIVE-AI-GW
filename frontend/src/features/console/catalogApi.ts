import type { components } from '@/api/generated/openapi'
import { apiClient, buildGatewayUrl } from '@/api/client'

export interface CatalogModelRate {
  currency: string
  amount: string
  unit: string
  per: string
}

export interface CatalogModelItem {
  id: string
  slug: string
  displayName: string
  provider: string
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
  catalogVersion?: string
}

export type CatalogModelDetail = components['schemas']['ModelDetail']
export type CatalogCompareInput = components['schemas']['CatalogCompareInput']
export type CatalogCompareResult = components['schemas']['CatalogCompareResult']

interface RawCatalogModel {
  id?: string
  model?: string
  slug?: string
  canonical_slug?: string
  name?: string
  display_name?: string
  provider?: string
  catalog_version?: string
  protocols?: string[]
  supported_protocols?: string[]
  modalities?: string[]
  actions?: string[]
  context_tokens?: number | string | null
  price_state?: 'priced' | 'free' | 'unpriced' | 'error'
  access_state?: 'allowed' | 'denied' | 'unknown' | 'requires_scope'
  route_state?: 'eligible' | 'ineligible' | 'unknown' | 'requires_scope'
  input_rate?: CatalogModelRate | null
  output_rate?: CatalogModelRate | null
  facts?: {
    context_tokens?: number | string | null
    max_output_tokens?: number | string | null
    tools?: boolean | string | null
    reasoning?: boolean | string | null
    modalities?: string[]
  }
  pricing?: {
    rates?: CatalogModelRate[]
    input?: CatalogModelRate | null
    output?: CatalogModelRate | null
  }
  rates?: CatalogModelRate[]
}

interface RawCatalogPage {
  items?: RawCatalogModel[]
  models?: RawCatalogModel[]
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
  if (typeof val === 'number' && Number.isFinite(val)) return val
  if (typeof val === 'string' && val.trim() !== '') {
    const parsed = Number(val)
    if (Number.isFinite(parsed)) return parsed
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
    protocols: Array.isArray(protocols) ? protocols : [],
    modalities: Array.isArray(modalities) ? modalities : [],
    maxOutputTokens,
    contextTokens,
    toolsSupported: Boolean(raw.facts?.tools && raw.facts.tools !== 'unsupported'),
    reasoningSupported: Boolean(raw.facts?.reasoning && raw.facts.reasoning !== 'unsupported'),
    inputRate,
    outputRate,
    priceState: raw.price_state,
    accessState: raw.access_state,
    routeState: raw.route_state,
    catalogVersion: raw.catalog_version
  }
}

export const catalogApi = {
  async listScopedModels(
    workspaceId: string,
    projectId: string,
    params?: { q?: string; action?: string[]; modality?: string[]; limit?: number },
    signal?: AbortSignal
  ): Promise<CatalogModelItem[]> {
    if (workspaceId && projectId) {
      const { data } = await apiClient.get<RawCatalogPage>(
        v2Url(`/workspaces/${workspaceId}/projects/${projectId}/models`),
        {
          baseURL: '',
          params: { limit: params?.limit ?? 100, accessible: true, ...params },
          signal
        }
      )
      const list = data?.items ?? data?.models ?? []
      return list.map(normalizeCatalogModel).filter((item): item is CatalogModelItem => item !== null)
    }
    return this.listPublicModels(params, signal)
  },

  async listPublicModels(
    params?: { q?: string; modality?: string[]; limit?: number },
    signal?: AbortSignal
  ): Promise<CatalogModelItem[]> {
    const { data } = await apiClient.get<RawCatalogPage>(v2Url('/public/models'), {
      baseURL: '',
      params: { limit: params?.limit ?? 100, ...params },
      signal
    })
    const list = data?.items ?? data?.models ?? []
    return list.map(normalizeCatalogModel).filter((item): item is CatalogModelItem => item !== null)
  },

  async getProjectModel(
    workspaceId: string,
    projectId: string,
    modelId: string,
    params?: { catalog_version?: string },
    signal?: AbortSignal
  ): Promise<CatalogModelDetail> {
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
    const { data } = await apiClient.post<CatalogCompareResult>(
      v2Url(`/workspaces/${workspaceId}/projects/${projectId}/models/compare`),
      input,
      { baseURL: '', signal }
    )
    return data
  }
}
