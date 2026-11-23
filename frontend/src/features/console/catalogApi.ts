import {
  compareProjectModels as compareProjectModelsClient,
  getProjectModel as getProjectModelClient,
  listProjectModels as listProjectModelsClient,
  listPublicModels as listPublicModelsClient
} from '@/api/catalog'
import type {
  CatalogCompareInput,
  CatalogCompareResult,
  CatalogModelDetail,
  CatalogModelSummary,
  CatalogRate
} from '@/features/catalog/contract'
import { toCatalogModelVM } from '@/features/catalog/viewModel'

export type CatalogModelRate = CatalogRate

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
  priceState?: string | null
  accessState?: string | null
  routeState?: string | null
  availabilityState?: string | null
  catalogVersion?: string | null
  billingState?: string | null
  billingReason?: string | null
  sourceKinds?: string[]
}

export type { CatalogCompareInput, CatalogCompareResult, CatalogModelDetail }

function parseOptionalPositiveInt(val: number | string | null | undefined): number | null {
  if (typeof val === 'number' && Number.isFinite(val)) return val
  if (typeof val === 'string' && val.trim() !== '') {
    const parsed = Number(val)
    if (Number.isFinite(parsed)) return parsed
  }
  return null
}

export function normalizeCatalogModel(raw: CatalogModelSummary): CatalogModelItem | null {
  const slug = (raw.model || raw.id || '').trim()
  const id = (raw.id || slug).trim()
  if (!id) return null
  const vm = toCatalogModelVM(raw, null, { perspective: 'scoped' })

  return {
    id,
    slug: slug || id,
    displayName: (raw.display_name || slug || id).trim(),
    provider: (raw.provider || 'gateway').trim(),
    actions: Array.isArray(raw.actions) ? raw.actions : [],
    protocols: Array.isArray(raw.protocols) ? raw.protocols : [],
    modalities: Array.isArray(raw.modalities) ? raw.modalities : [],
    maxOutputTokens: null,
    contextTokens: parseOptionalPositiveInt(raw.context_tokens),
    toolsSupported: false,
    reasoningSupported: false,
    inputRate: vm.billing.inputRate,
    outputRate: vm.billing.outputRate,
    priceState: raw.billing?.state ?? raw.price_state ?? null,
    accessState: raw.availability?.access ?? raw.access_state ?? null,
    routeState: raw.route_state ?? null,
    availabilityState: raw.availability?.state ?? raw.availability_state ?? null,
    catalogVersion: raw.catalog_version ?? null,
    billingState: vm.billing.state,
    billingReason: vm.billing.reason,
    sourceKinds: vm.origin.kinds
  }
}

export const catalogApi = {
  async listScopedModels(
    workspaceId: string,
    projectId: string,
    params?: {
      q?: string
      action?: string[]
      modality?: string[]
      api_key_id?: string
      limit?: number
    },
    signal?: AbortSignal
  ): Promise<CatalogModelItem[]> {
    if (!workspaceId || !projectId) {
      return []
    }
    const page = await listProjectModelsClient(
      workspaceId,
      projectId,
      {
        limit: params?.limit ?? 100,
        accessible: true,
        q: params?.q,
        action: params?.action,
        modality: params?.modality,
        api_key_id: params?.api_key_id
      },
      { signal }
    )
    const list = page?.items ?? []
    return list.map(normalizeCatalogModel).filter((item): item is CatalogModelItem => item !== null)
  },

  async listPublicModels(
    params?: { q?: string; modality?: string[]; limit?: number },
    signal?: AbortSignal
  ): Promise<CatalogModelItem[]> {
    const page = await listPublicModelsClient(
      {
        limit: params?.limit ?? 100,
        q: params?.q,
        modality: params?.modality
      },
      { signal }
    )
    const list = page?.items ?? []
    return list.map(normalizeCatalogModel).filter((item): item is CatalogModelItem => item !== null)
  },

  async getProjectModel(
    workspaceId: string,
    projectId: string,
    modelId: string,
    params?: { catalog_version?: string; api_key_id?: string },
    signal?: AbortSignal
  ): Promise<CatalogModelDetail> {
    return getProjectModelClient(workspaceId, projectId, modelId, params, { signal })
  },

  async compareProjectModels(
    workspaceId: string,
    projectId: string,
    input: CatalogCompareInput,
    signal?: AbortSignal
  ): Promise<CatalogCompareResult> {
    return compareProjectModelsClient(workspaceId, projectId, input, { signal })
  }
}
