import { apiClient } from '@/api/client'
import { postIdempotentCreate } from '@/api/idempotency'
import { workspaceIDFromPath } from '@/api/workspaceScope'
import type {
  ConnectionActivate,
  ConnectionAssignments,
  ConnectionAuthorization,
  ConnectionBulkApply,
  ConnectionBulkIn,
  ConnectionCreate,
  ConnectionCredentialInput,
  ConnectionCredentialResult,
  ConnectionEventPage,
  ConnectionNewPool,
  ConnectionOperation,
  ConnectionPatch,
  ConnectionPool,
  ConnectionPoolPage,
  ConnectionPreview,
  ConnectionResource,
  ConnectionResourcePage,
  ConnectionRotation,
  ConnectionSchemaPage,
  ConnectionVerify,
  ConnectionWriteResult
} from './types'

function generateIdempotencyKey(): string {
  return globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`
}

export function resolveActiveWorkspaceId(explicitWorkspaceId?: string): string {
  if (explicitWorkspaceId && /^[1-9]\d*$/.test(explicitWorkspaceId)) {
    return explicitWorkspaceId
  }
  if (typeof window !== 'undefined') {
    const fromPath = workspaceIDFromPath(window.location.pathname)
    if (fromPath) return fromPath
    const stored = window.localStorage?.getItem('omnihive.workspace_id.v1')
    if (stored && /^[1-9]\d*$/.test(stored)) return stored
  }
  return '1'
}

function workspaceBase(workspaceId?: string): string {
  return `/v2/workspaces/${resolveActiveWorkspaceId(workspaceId)}`
}

function ifMatchHeader(version: string): Record<string, string> {
  return { 'If-Match': `"${version}"` }
}

export const connectionsApi = {
  async listProviderSchemas(
    params?: { provider?: string; auth_type?: string; cursor?: string; limit?: number },
    workspaceId?: string
  ): Promise<ConnectionSchemaPage> {
    const { data } = await apiClient.get<ConnectionSchemaPage>(`${workspaceBase(workspaceId)}/provider-schemas`, {
      params
    })
    return data
  },

  async listConnections(
    params?: { provider?: string; pool_id?: string; state?: string; cursor?: string; limit?: number },
    workspaceId?: string
  ): Promise<ConnectionResourcePage> {
    const { data } = await apiClient.get<ConnectionResourcePage>(`${workspaceBase(workspaceId)}/connections`, {
      params
    })
    return data
  },

  async getConnection(id: string, workspaceId?: string): Promise<ConnectionResource> {
    const { data } = await apiClient.get<ConnectionResource>(`${workspaceBase(workspaceId)}/connections/${id}`)
    return data
  },

  async createConnection(payload: ConnectionCreate, workspaceId?: string): Promise<ConnectionWriteResult> {
    return postIdempotentCreate<ConnectionWriteResult>(`${workspaceBase(workspaceId)}/connections`, payload)
  },

  async patchConnection(
    id: string,
    version: string,
    payload: ConnectionPatch,
    workspaceId?: string
  ): Promise<ConnectionWriteResult> {
    const { data } = await apiClient.patch<ConnectionWriteResult>(
      `${workspaceBase(workspaceId)}/connections/${id}`,
      payload,
      { headers: ifMatchHeader(version) }
    )
    return data
  },

  async changeCredential(
    id: string,
    version: string,
    payload: ConnectionCredentialInput,
    workspaceId?: string
  ): Promise<ConnectionCredentialResult> {
    const { data } = await apiClient.put<ConnectionCredentialResult>(
      `${workspaceBase(workspaceId)}/connections/${id}/credential`,
      payload,
      { headers: ifMatchHeader(version) }
    )
    return data
  },

  async attachAuthorization(
    id: string,
    version: string,
    payload: ConnectionAuthorization,
    workspaceId?: string,
    idempotencyKey = generateIdempotencyKey()
  ): Promise<ConnectionCredentialResult> {
    const { data } = await apiClient.put<ConnectionCredentialResult>(
      `${workspaceBase(workspaceId)}/connections/${id}/authorization`,
      payload,
      {
        headers: {
          ...ifMatchHeader(version),
          'Idempotency-Key': idempotencyKey
        }
      }
    )
    return data
  },

  async verifyConnection(
    id: string,
    version: string,
    payload: ConnectionVerify,
    workspaceId?: string,
    idempotencyKey = generateIdempotencyKey()
  ): Promise<ConnectionOperation> {
    const { data } = await apiClient.post<ConnectionOperation>(
      `${workspaceBase(workspaceId)}/connections/${id}/verify`,
      payload,
      {
        headers: {
          ...ifMatchHeader(version),
          'Idempotency-Key': idempotencyKey
        }
      }
    )
    return data
  },

  async previewConnection(
    id: string,
    version: string,
    payload: ConnectionAssignments,
    workspaceId?: string,
    idempotencyKey = generateIdempotencyKey()
  ): Promise<ConnectionPreview> {
    const { data } = await apiClient.post<ConnectionPreview>(
      `${workspaceBase(workspaceId)}/connections/${id}/preview`,
      payload,
      {
        headers: {
          ...ifMatchHeader(version),
          'Idempotency-Key': idempotencyKey
        }
      }
    )
    return data
  },

  async activateConnection(
    id: string,
    version: string,
    payload: ConnectionActivate,
    workspaceId?: string,
    idempotencyKey = generateIdempotencyKey()
  ): Promise<ConnectionWriteResult> {
    const { data } = await apiClient.post<ConnectionWriteResult>(
      `${workspaceBase(workspaceId)}/connections/${id}/activate`,
      payload,
      {
        headers: {
          ...ifMatchHeader(version),
          'Idempotency-Key': idempotencyKey
        }
      }
    )
    return data
  },

  async rotateCredential(
    id: string,
    version: string,
    payload: ConnectionRotation,
    workspaceId?: string,
    idempotencyKey = generateIdempotencyKey()
  ): Promise<ConnectionOperation> {
    const { data } = await apiClient.post<ConnectionOperation>(
      `${workspaceBase(workspaceId)}/connections/${id}/rotations`,
      payload,
      {
        headers: {
          ...ifMatchHeader(version),
          'Idempotency-Key': idempotencyKey
        }
      }
    )
    return data
  },

  async listEvents(
    id: string,
    params?: { cursor?: string; limit?: number },
    workspaceId?: string
  ): Promise<ConnectionEventPage> {
    const { data } = await apiClient.get<ConnectionEventPage>(
      `${workspaceBase(workspaceId)}/connections/${id}/events`,
      { params }
    )
    return data
  },

  async bulkPreview(
    payload: ConnectionBulkIn,
    workspaceId?: string,
    idempotencyKey = generateIdempotencyKey()
  ): Promise<ConnectionPreview> {
    const { data } = await apiClient.post<ConnectionPreview>(
      `${workspaceBase(workspaceId)}/connections/bulk-previews`,
      payload,
      { headers: { 'Idempotency-Key': idempotencyKey } }
    )
    return data
  },

  async bulkApply(
    payload: ConnectionBulkApply,
    workspaceId?: string,
    idempotencyKey = generateIdempotencyKey()
  ): Promise<ConnectionOperation> {
    const { data } = await apiClient.post<ConnectionOperation>(
      `${workspaceBase(workspaceId)}/connections/bulk-apply`,
      payload,
      { headers: { 'Idempotency-Key': idempotencyKey } }
    )
    return data
  },

  async listPools(
    params?: { cursor?: string; limit?: number },
    workspaceId?: string
  ): Promise<ConnectionPoolPage> {
    const { data } = await apiClient.get<ConnectionPoolPage>(`${workspaceBase(workspaceId)}/pools`, { params })
    return data
  },

  async createPool(payload: ConnectionNewPool, workspaceId?: string): Promise<ConnectionPool> {
    return postIdempotentCreate<ConnectionPool>(`${workspaceBase(workspaceId)}/pools`, payload)
  },

  async updatePool(
    id: string,
    payload: { name?: string; max_concurrency?: number },
    workspaceId?: string
  ): Promise<ConnectionPool> {
    const { data } = await apiClient.patch<ConnectionPool>(
      `${workspaceBase(workspaceId)}/pools/${encodeURIComponent(id)}`,
      payload
    )
    return data
  },

  async deletePool(id: string, workspaceId?: string): Promise<void> {
    await apiClient.delete(`${workspaceBase(workspaceId)}/pools/${encodeURIComponent(id)}`)
  },

  async getOperation(statusUrl: string): Promise<ConnectionOperation> {
    const normalizedUrl = statusUrl.startsWith('/api/') ? statusUrl.slice('/api'.length) : statusUrl
    const { data } = await apiClient.get<ConnectionOperation>(normalizedUrl)
    return data
  }
}

export async function pollConnectionOperation(
  initial: ConnectionOperation,
  opts: {
    maxAttempts?: number
    intervalMs?: number
    signal?: AbortSignal
    onUpdate?: (op: ConnectionOperation) => void
  } = {}
): Promise<ConnectionOperation> {
  const maxAttempts = opts.maxAttempts ?? 12
  const intervalMs = opts.intervalMs ?? 800
  let current = initial
  opts.onUpdate?.(current)

  let attempt = 0
  while ((current.state === 'queued' || current.state === 'running') && attempt < maxAttempts) {
    if (opts.signal?.aborted) {
      break
    }
    await new Promise((resolve) => setTimeout(resolve, intervalMs))
    if (opts.signal?.aborted) {
      break
    }
    current = await connectionsApi.getOperation(current.status_url)
    opts.onUpdate?.(current)
    attempt += 1
  }
  return current
}
