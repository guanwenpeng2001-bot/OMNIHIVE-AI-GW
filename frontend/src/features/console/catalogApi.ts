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
}

interface RawCatalogModel {
  id?: string
  slug?: string
  canonical_slug?: string
  name?: string
  display_name?: string
  provider?: string
  protocols?: string[]
  supported_protocols?: string[]
  facts?: {
    context_tokens?: number | null
    max_output_tokens?: number | null
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
    raw.pricing?.input ??
    rates.find((r) => r.unit === 'input_token' || r.unit === 'prompt_token') ??
    null
  const outputRate =
    raw.pricing?.output ??
    rates.find((r) => r.unit === 'output_token' || r.unit === 'completion_token') ??
    null
  return { inputRate, outputRate }
}

export function normalizeCatalogModel(raw: RawCatalogModel): CatalogModelItem | null {
  const slug = (raw.slug || raw.canonical_slug || raw.id || '').trim()
  const id = (raw.id || slug).trim()
  if (!id) return null
  const displayName = (raw.display_name || raw.name || slug || id).trim()
  const protocols = raw.protocols ?? raw.supported_protocols ?? []
  const modalities = raw.facts?.modalities ?? []
  const { inputRate, outputRate } = normalizeRateList(raw)

  return {
    id,
    slug: slug || id,
    displayName,
    provider: (raw.provider || 'gateway').trim(),
    protocols: Array.isArray(protocols) ? protocols : [],
    modalities: Array.isArray(modalities) ? modalities : [],
    maxOutputTokens:
      typeof raw.facts?.max_output_tokens === 'number' ? raw.facts.max_output_tokens : null,
    contextTokens:
      typeof raw.facts?.context_tokens === 'number' ? raw.facts.context_tokens : null,
    toolsSupported: Boolean(raw.facts?.tools && raw.facts.tools !== 'unsupported'),
    reasoningSupported: Boolean(raw.facts?.reasoning && raw.facts.reasoning !== 'unsupported'),
    inputRate,
    outputRate
  }
}

export const catalogV2Api = {
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
  }
}
