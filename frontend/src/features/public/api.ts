import { apiClient } from '@/api/client'
import {
  comparePublicModels,
  getPublicModel,
  isCatalogAbortError,
  isSnapshotExpiredError,
  listPublicModels,
  type CatalogRequestOptions
} from '@/api/catalog'
import type {
  CatalogCompareInput,
  CatalogCompareResult,
  CatalogCompareRow,
  CatalogListParams,
  CatalogMetrics,
  CatalogModelDetail,
  CatalogModelPage,
  CatalogModelSummary,
  CatalogRate,
  StatusComponent,
  StatusDaily,
  StatusIncident,
  StatusIncidentPage,
  StatusIncidentSummary,
  StatusSummary,
  StatusWindow
} from '@/features/catalog/contract'

export type PublicRequestOptions = CatalogRequestOptions
export type ListParams = CatalogListParams
export type PublicRate = CatalogRate
export type PublicCatalogMetrics = CatalogMetrics
export type PublicModelSummary = CatalogModelSummary
export type PublicModelPage = CatalogModelPage
export type PublicModelDetail = CatalogModelDetail
export type PublicCompareInput = CatalogCompareInput
export type PublicCompareRow = CatalogCompareRow
export type PublicCompareResponse = CatalogCompareResult
export type PublicStatusWindow = StatusWindow
export type PublicStatusDaily = StatusDaily
export type PublicStatusComponent = StatusComponent
export type PublicStatusIncidentSummary = StatusIncidentSummary
export type PublicStatusIncident = StatusIncident
export type PublicIncidentPage = StatusIncidentPage
export type PublicStatusResponse = StatusSummary

export {
  comparePublicModels,
  getPublicModel,
  isCatalogAbortError,
  isSnapshotExpiredError,
  listPublicModels
}

function withoutImplicitTimezone(params: Record<string, unknown> = {}): Record<string, unknown> {
  return { ...params, timezone: undefined }
}

export async function getPublicStatus(
  window?: StatusWindow,
  requestOptions?: CatalogRequestOptions
): Promise<StatusSummary> {
  const { data } = await apiClient.get('/api/v2/public/status', {
    params: withoutImplicitTimezone(window ? { window } : {}),
    ...(requestOptions?.signal ? { signal: requestOptions.signal } : {})
  })
  return (data?.data ?? data) as StatusSummary
}

export async function listPublicIncidents(
  params?: {
    from?: string
    to?: string
    cursor?: string
    limit?: number
  },
  requestOptions?: CatalogRequestOptions
): Promise<StatusIncidentPage> {
  const { data } = await apiClient.get('/api/v2/public/status/incidents', {
    params: withoutImplicitTimezone({
      from: params?.from || undefined,
      to: params?.to || undefined,
      cursor: params?.cursor || undefined,
      limit: params?.limit ?? 20
    }),
    ...(requestOptions?.signal ? { signal: requestOptions.signal } : {})
  })
  return (data?.data ?? data) as StatusIncidentPage
}

export async function getPublicIncident(
  id: string,
  requestOptions?: CatalogRequestOptions
): Promise<StatusIncident> {
  const { data } = await apiClient.get(`/api/v2/public/status/incidents/${encodeURIComponent(id)}`, {
    params: withoutImplicitTimezone(),
    ...(requestOptions?.signal ? { signal: requestOptions.signal } : {})
  })
  return (data?.data ?? data) as StatusIncident
}
