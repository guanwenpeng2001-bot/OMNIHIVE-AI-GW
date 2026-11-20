import type { components } from '@/api/generated/openapi'
import { createIdempotencyKey, controlDelete, controlGet, controlPatch, controlPost, controlPut } from '@/features/shared/controlClient'

export type ByokAuthType = components['schemas']['ByokAuthType']
export type ByokClass = components['schemas']['ByokClass']
export type ByokClassList = components['schemas']['ByokClassList']
export type ByokCredential = components['schemas']['ByokCredential']
export type ByokPage = components['schemas']['ByokPage']
export type ByokEstimate = components['schemas']['ByokEstimate']
export type ByokGrantPatch = components['schemas']['ByokGrantPatch']
export type ByokRevokeMode = components['schemas']['ByokRevoke']['mode']
export type ByokServiceAccount = components['schemas']['ByokServiceAccount']
export type ByokEvent = components['schemas']['ConnectionEvent']
export type ByokEventPage = components['schemas']['ConnectionEventPage']
export type ByokOperation = components['schemas']['IdentityOperation']

export interface ByokPolicyPayload {
  allowed_project_ids: string[]
  allowed_member_ids?: string[]
  allowed_service_account_ids?: string[]
  allowed_key_ids: string[]
  allowed_model_ids: string[]
  priority: number
  allow_platform_fallback: boolean
  include_external_in_budget: boolean
}

export interface ByokCreatePayload extends ByokPolicyPayload {
  provider: string
  auth_type: ByokAuthType
  secret: string
}

export interface ByokPatchPayload {
  allowed_project_ids?: string[]
  allowed_member_ids?: string[]
  allowed_service_account_ids?: string[]
  allowed_key_ids?: string[]
  allowed_model_ids?: string[]
  priority?: number
  allow_platform_fallback?: boolean
  include_external_in_budget?: boolean
}

export interface ByokWorkspaceEstimateQuery {
  project_id: string
  member_id: string
  key_id: string
  model_id: string
  input_token_bound: string
  output_token_bound: string
}

export interface ByokProjectEstimateQuery {
  member_id: string
  key_id: string
  model_id: string
  input_token_bound: string
  output_token_bound: string
}

export function formatIfMatchHeader(version: string): Record<string, string> {
  const clean = String(version).replace(/^"+|"+$/g, '').trim()
  return { 'If-Match': `"${clean}"` }
}

function sanitizePolicyBody<T extends ByokPolicyPayload>(input: T): Record<string, unknown> {
  const body: Record<string, unknown> = {
    allowed_project_ids: input.allowed_project_ids,
    allowed_key_ids: input.allowed_key_ids,
    allowed_model_ids: input.allowed_model_ids,
    priority: input.priority,
    allow_platform_fallback: input.allow_platform_fallback,
    include_external_in_budget: input.include_external_in_budget
  }
  if (input.allowed_member_ids && input.allowed_member_ids.length > 0) {
    body.allowed_member_ids = input.allowed_member_ids
  } else {
    body.allowed_member_ids = []
  }
  if (input.allowed_service_account_ids && input.allowed_service_account_ids.length > 0) {
    body.allowed_service_account_ids = input.allowed_service_account_ids
  }
  return body
}

function sanitizePatchBody(patch: ByokPatchPayload): Record<string, unknown> {
  const body: Record<string, unknown> = {}
  if (patch.allowed_project_ids !== undefined) body.allowed_project_ids = patch.allowed_project_ids
  if (patch.allowed_member_ids !== undefined) body.allowed_member_ids = patch.allowed_member_ids
  if (patch.allowed_service_account_ids !== undefined) {
    body.allowed_service_account_ids = patch.allowed_service_account_ids
  }
  if (patch.allowed_key_ids !== undefined) body.allowed_key_ids = patch.allowed_key_ids
  if (patch.allowed_model_ids !== undefined) body.allowed_model_ids = patch.allowed_model_ids
  if (patch.priority !== undefined) body.priority = patch.priority
  if (patch.allow_platform_fallback !== undefined) {
    body.allow_platform_fallback = patch.allow_platform_fallback
  }
  if (patch.include_external_in_budget !== undefined) {
    body.include_external_in_budget = patch.include_external_in_budget
  }
  return body
}

function wsBase(workspaceId: string): string {
  return `/workspaces/${encodeURIComponent(workspaceId)}/byok`
}

function projBase(workspaceId: string, projectId: string): string {
  return `/workspaces/${encodeURIComponent(workspaceId)}/projects/${encodeURIComponent(projectId)}/byok/credentials`
}

export const byokApi = {
  // ==================== Workspace BYOK (12 operations) ====================

  listWorkspaceCredentials(
    workspaceId: string,
    params?: { cursor?: string; limit?: number }
  ): Promise<ByokPage> {
    return controlGet<ByokPage>(wsBase(workspaceId), {
      cursor: params?.cursor || undefined,
      limit: params?.limit ?? 25
    })
  },

  listWorkspaceClasses(workspaceId: string): Promise<ByokClassList> {
    return controlGet<ByokClassList>(`${wsBase(workspaceId)}/provider-classes`)
  },

  createWorkspaceCredential(
    workspaceId: string,
    input: ByokCreatePayload,
    idempotencyKey = createIdempotencyKey()
  ): Promise<ByokCredential> {
    return controlPost<ByokCredential>(
      wsBase(workspaceId),
      {
        provider: input.provider,
        auth_type: input.auth_type,
        secret: input.secret,
        ...sanitizePolicyBody(input)
      },
      { 'Idempotency-Key': idempotencyKey }
    )
  },

  getWorkspaceCredential(workspaceId: string, id: string): Promise<ByokCredential> {
    return controlGet<ByokCredential>(`${wsBase(workspaceId)}/${encodeURIComponent(id)}`)
  },

  patchWorkspaceCredential(
    workspaceId: string,
    id: string,
    version: string,
    patch: ByokPatchPayload
  ): Promise<ByokCredential> {
    return controlPatch<ByokCredential>(
      `${wsBase(workspaceId)}/${encodeURIComponent(id)}`,
      sanitizePatchBody(patch),
      formatIfMatchHeader(version)
    )
  },

  putWorkspacePolicy(
    workspaceId: string,
    id: string,
    version: string,
    policy: ByokPolicyPayload
  ): Promise<ByokCredential> {
    return controlPut<ByokCredential>(
      `${wsBase(workspaceId)}/${encodeURIComponent(id)}/policy`,
      sanitizePolicyBody(policy),
      formatIfMatchHeader(version)
    )
  },

  verifyWorkspaceCredential(
    workspaceId: string,
    id: string,
    version: string,
    idempotencyKey = createIdempotencyKey()
  ): Promise<ByokOperation> {
    return controlPost<ByokOperation>(
      `${wsBase(workspaceId)}/${encodeURIComponent(id)}/verify`,
      {},
      {
        ...formatIfMatchHeader(version),
        'Idempotency-Key': idempotencyKey
      }
    )
  },

  rotateWorkspaceCredential(
    workspaceId: string,
    id: string,
    version: string,
    replacementSecret: string,
    idempotencyKey = createIdempotencyKey()
  ): Promise<ByokOperation> {
    return controlPost<ByokOperation>(
      `${wsBase(workspaceId)}/${encodeURIComponent(id)}/rotate`,
      { replacement: { secret: replacementSecret } },
      {
        ...formatIfMatchHeader(version),
        'Idempotency-Key': idempotencyKey
      }
    )
  },

  revokeWorkspaceCredential(
    workspaceId: string,
    id: string,
    version: string,
    mode: ByokRevokeMode,
    idempotencyKey = createIdempotencyKey()
  ): Promise<ByokOperation> {
    return controlPost<ByokOperation>(
      `${wsBase(workspaceId)}/${encodeURIComponent(id)}/revoke`,
      { mode },
      {
        ...formatIfMatchHeader(version),
        'Idempotency-Key': idempotencyKey
      }
    )
  },

  deleteWorkspaceCredential(
    workspaceId: string,
    id: string,
    version: string,
    idempotencyKey = createIdempotencyKey()
  ): Promise<ByokOperation> {
    return controlDelete<ByokOperation>(`${wsBase(workspaceId)}/${encodeURIComponent(id)}`, {
      ...formatIfMatchHeader(version),
      'Idempotency-Key': idempotencyKey
    })
  },

  estimateWorkspaceCredential(
    workspaceId: string,
    id: string,
    query: ByokWorkspaceEstimateQuery
  ): Promise<ByokEstimate> {
    return controlGet<ByokEstimate>(
      `${wsBase(workspaceId)}/${encodeURIComponent(id)}/estimate`,
      { ...query }
    )
  },

  listWorkspaceEvents(
    workspaceId: string,
    id: string,
    params?: { cursor?: string; limit?: number }
  ): Promise<ByokEventPage> {
    return controlGet<ByokEventPage>(`${wsBase(workspaceId)}/${encodeURIComponent(id)}/events`, {
      cursor: params?.cursor || undefined,
      limit: params?.limit ?? 25
    })
  },

  // ==================== Project BYOK (15 operations) ====================

  listProjectCredentials(
    workspaceId: string,
    projectId: string,
    params?: { cursor?: string; limit?: number }
  ): Promise<ByokPage> {
    return controlGet<ByokPage>(projBase(workspaceId, projectId), {
      cursor: params?.cursor || undefined,
      limit: params?.limit ?? 25
    })
  },

  listProjectClasses(workspaceId: string, projectId: string): Promise<ByokClassList> {
    return controlGet<ByokClassList>(`${projBase(workspaceId, projectId)}/provider-classes`)
  },

  createProjectCredential(
    workspaceId: string,
    projectId: string,
    input: ByokCreatePayload,
    idempotencyKey = createIdempotencyKey()
  ): Promise<ByokCredential> {
    return controlPost<ByokCredential>(
      projBase(workspaceId, projectId),
      {
        provider: input.provider,
        auth_type: input.auth_type,
        secret: input.secret,
        ...sanitizePolicyBody({
          ...input,
          allowed_project_ids: [projectId]
        })
      },
      { 'Idempotency-Key': idempotencyKey }
    )
  },

  getProjectCredential(workspaceId: string, projectId: string, id: string): Promise<ByokCredential> {
    return controlGet<ByokCredential>(`${projBase(workspaceId, projectId)}/${encodeURIComponent(id)}`)
  },

  patchProjectCredential(
    workspaceId: string,
    projectId: string,
    id: string,
    version: string,
    patch: ByokPatchPayload
  ): Promise<ByokCredential> {
    return controlPatch<ByokCredential>(
      `${projBase(workspaceId, projectId)}/${encodeURIComponent(id)}`,
      sanitizePatchBody(patch),
      formatIfMatchHeader(version)
    )
  },

  putProjectPolicy(
    workspaceId: string,
    projectId: string,
    id: string,
    version: string,
    policy: ByokPolicyPayload
  ): Promise<ByokCredential> {
    return controlPut<ByokCredential>(
      `${projBase(workspaceId, projectId)}/${encodeURIComponent(id)}/policy`,
      sanitizePolicyBody({
        ...policy,
        allowed_project_ids: [projectId]
      }),
      formatIfMatchHeader(version)
    )
  },

  verifyProjectCredential(
    workspaceId: string,
    projectId: string,
    id: string,
    version: string,
    idempotencyKey = createIdempotencyKey()
  ): Promise<ByokOperation> {
    return controlPost<ByokOperation>(
      `${projBase(workspaceId, projectId)}/${encodeURIComponent(id)}/verify`,
      {},
      {
        ...formatIfMatchHeader(version),
        'Idempotency-Key': idempotencyKey
      }
    )
  },

  rotateProjectCredential(
    workspaceId: string,
    projectId: string,
    id: string,
    version: string,
    replacementSecret: string,
    idempotencyKey = createIdempotencyKey()
  ): Promise<ByokOperation> {
    return controlPost<ByokOperation>(
      `${projBase(workspaceId, projectId)}/${encodeURIComponent(id)}/rotate`,
      { replacement: { secret: replacementSecret } },
      {
        ...formatIfMatchHeader(version),
        'Idempotency-Key': idempotencyKey
      }
    )
  },

  revokeProjectCredential(
    workspaceId: string,
    projectId: string,
    id: string,
    version: string,
    mode: ByokRevokeMode,
    idempotencyKey = createIdempotencyKey()
  ): Promise<ByokOperation> {
    return controlPost<ByokOperation>(
      `${projBase(workspaceId, projectId)}/${encodeURIComponent(id)}/revoke`,
      { mode },
      {
        ...formatIfMatchHeader(version),
        'Idempotency-Key': idempotencyKey
      }
    )
  },

  deleteProjectCredential(
    workspaceId: string,
    projectId: string,
    id: string,
    version: string,
    idempotencyKey = createIdempotencyKey()
  ): Promise<ByokOperation> {
    return controlDelete<ByokOperation>(`${projBase(workspaceId, projectId)}/${encodeURIComponent(id)}`, {
      ...formatIfMatchHeader(version),
      'Idempotency-Key': idempotencyKey
    })
  },

  estimateProjectCredential(
    workspaceId: string,
    projectId: string,
    id: string,
    query: ByokProjectEstimateQuery
  ): Promise<ByokEstimate> {
    return controlGet<ByokEstimate>(
      `${projBase(workspaceId, projectId)}/${encodeURIComponent(id)}/estimate`,
      { ...query }
    )
  },

  listProjectEvents(
    workspaceId: string,
    projectId: string,
    id: string,
    params?: { cursor?: string; limit?: number }
  ): Promise<ByokEventPage> {
    return controlGet<ByokEventPage>(
      `${projBase(workspaceId, projectId)}/${encodeURIComponent(id)}/events`,
      {
        cursor: params?.cursor || undefined,
        limit: params?.limit ?? 25
      }
    )
  },

  getProjectServiceAccount(
    workspaceId: string,
    projectId: string,
    keyId: string
  ): Promise<ByokServiceAccount> {
    return controlGet<ByokServiceAccount>(
      `${projBase(workspaceId, projectId)}/service-accounts/${encodeURIComponent(keyId)}`
    )
  },

  registerProjectServiceAccount(
    workspaceId: string,
    projectId: string,
    keyId: string
  ): Promise<ByokServiceAccount> {
    return controlPut<ByokServiceAccount>(
      `${projBase(workspaceId, projectId)}/service-accounts/${encodeURIComponent(keyId)}`,
      {}
    )
  },

  revokeProjectServiceAccount(
    workspaceId: string,
    projectId: string,
    keyId: string
  ): Promise<ByokServiceAccount> {
    return controlDelete<ByokServiceAccount>(
      `${projBase(workspaceId, projectId)}/service-accounts/${encodeURIComponent(keyId)}`
    )
  }
}
