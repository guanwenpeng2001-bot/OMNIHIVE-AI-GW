import type { RouteLocationRaw } from 'vue-router'
import { apiClient } from '@/api/client'
import type { components } from '@/api/generated/openapi'

export type Schema = components['schemas']
export type ProjectScope = { workspaceId: string; projectId: string }

export function opaqueNumericId(value: unknown): value is string {
  return typeof value === 'string' && /^[1-9]\d{0,18}$/.test(value) && BigInt(value) <= 9223372036854775807n
}

export function projectScope(workspaceId: unknown, projectId: unknown): ProjectScope | null {
  return opaqueNumericId(workspaceId) && opaqueNumericId(projectId) ? { workspaceId, projectId } : null
}

export function scopeValue(scope: ProjectScope | null): string {
  return scope ? `${scope.workspaceId}/${scope.projectId}` : ''
}

export function scopeQuery(scope: ProjectScope) {
  return { workspace_id: scope.workspaceId, project_id: scope.projectId }
}

export function workspaceBase(workspaceId: string): string {
  return `/api/v2/workspaces/${encodeURIComponent(workspaceId)}`
}

export function projectBase(scope: ProjectScope): string {
  return `${workspaceBase(scope.workspaceId)}/projects/${encodeURIComponent(scope.projectId)}`
}

export function requestLocation(scope: ProjectScope, id: string | null, admin = false): RouteLocationRaw {
  const base = admin ? '/admin/requests' : '/requests'
  return {
    path: id ? `${base}/${encodeURIComponent(id)}` : base,
    query: scopeQuery(scope),
  }
}

export function queryParams(values: Record<string, string | number | null | undefined>): URLSearchParams {
  const params = new URLSearchParams()
  for (const [name, value] of Object.entries(values)) {
    if (value != null && value !== '') params.set(name, String(value))
  }
  return params
}

// Several v2 endpoints reject unrecognized parameters. Preserve the exact
// contract query while retaining the shared session and response interceptors.
export async function controlGet<T>(path: string, params = new URLSearchParams(), signal?: AbortSignal): Promise<T> {
  return (await apiClient.get<T>(path, {
    params,
    paramsSerializer: { serialize: () => params.toString() },
    signal,
  })).data
}

export const requestsApi = {
  scopes: (cursor: string | null, signal: AbortSignal) =>
    controlGet<Schema['IdentityWorkspacePage']>('/api/v2/me/scopes', queryParams({ limit: 50, cursor }), signal),
  projects: (workspaceId: string, cursor: string, signal: AbortSignal) =>
    controlGet<Schema['IdentityProjectPage']>(`${workspaceBase(workspaceId)}/projects`, queryParams({ limit: 50, cursor }), signal),
  access: (scope: ProjectScope, signal: AbortSignal) =>
    controlGet<Schema['IdentityAccess']>(`${projectBase(scope)}/access`, undefined, signal),
  keys: (scope: ProjectScope, signal: AbortSignal) =>
    controlGet<Schema['IdentityKeyPage']>(`${projectBase(scope)}/keys`, queryParams({ limit: 100 }), signal),
  list: (scope: ProjectScope, params: URLSearchParams, signal: AbortSignal) =>
    controlGet<Schema['RequestsPage']>(`${projectBase(scope)}/requests`, params, signal),
  detail: (scope: ProjectScope, id: string, signal: AbortSignal) =>
    controlGet<Schema['RequestsDetail']>(`${projectBase(scope)}/requests/${encodeURIComponent(id)}`, undefined, signal),
  explanation: (scope: ProjectScope, id: string, signal: AbortSignal) =>
    controlGet<Schema['RequestsRouteExplanation']>(`${projectBase(scope)}/requests/${encodeURIComponent(id)}/route-explain`, undefined, signal),
  createExport: async (
    scope: ProjectScope,
    id: string,
    payload: Schema['RequestsExportInput'],
    idempotencyKey: string
  ): Promise<Schema['RequestsExportOperation']> => {
    return (
      await apiClient.post<Schema['RequestsExportOperation']>(
        `${projectBase(scope)}/requests/${encodeURIComponent(id)}/exports`,
        payload,
        { headers: { 'Idempotency-Key': idempotencyKey } }
      )
    ).data
  },
  downloadExport: async (scope: ProjectScope, id: string, exportId: string): Promise<unknown> => {
    return controlGet<unknown>(
      `${projectBase(scope)}/requests/${encodeURIComponent(id)}/exports/${encodeURIComponent(exportId)}/download`
    )
  },
}
