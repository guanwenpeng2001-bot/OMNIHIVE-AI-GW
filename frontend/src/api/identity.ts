import type { components } from '@/api/generated/openapi'
import { buildGatewayUrl } from '@/api/url'
import { extractApiErrorStatus } from '@/utils/apiError'

export type IdentityProjectSummary = components['schemas']['IdentityWorkspace']['projects'][number] & {
  is_default?: boolean
  state?: string
}

export type IdentityWorkspace = Omit<components['schemas']['IdentityWorkspace'], 'projects'> & {
  workspace_name?: string
  workspace_state?: string
  is_default?: boolean
  projects: IdentityProjectSummary[]
}

export type IdentityWorkspacePage = Omit<components['schemas']['IdentityWorkspacePage'], 'items'> & {
  items: IdentityWorkspace[]
}

export type IdentityProject = components['schemas']['IdentityProject'] & {
  is_default?: boolean
  state?: string
}

export type IdentityProjectPage = Omit<components['schemas']['IdentityProjectPage'], 'items'> & {
  items: IdentityProject[]
}

export interface IdentityMember {
  id: string
  user_id?: string
  email?: string
  username?: string
  role?: string
  status?: string
  actions?: string[]
  project_ids?: string[]
  version: string
}

export interface IdentityMemberPage {
  items: IdentityMember[]
  next_cursor: string | null
  as_of?: string
}

export type IdentityGrantInput = components['schemas']['IdentityGrantInput']
export type IdentityGrantVersion = components['schemas']['IdentityGrantVersion']

export interface IdentityMemberGrants {
  member_id: string
  project_ids: string[]
  actions: string[]
  project_grants?: Array<{ project_id: string; actions: string[] }>
  version: string
  effective_at?: string
}

export type IdentityKeyDerivedState =
  | 'active'
  | 'disabled'
  | 'expired'
  | 'quota_exhausted'
  | 'no_credential'
  | 'unknown'

export type IdentityKeyKind = 'project_policy' | 'legacy_group'

// Extend generated IdentityKey with optional P2 §2 fields until openapi.d.ts is regenerated.
export type IdentityKey = components['schemas']['IdentityKey'] & {
  status?: string
  expires_at?: string | null
  state?: IdentityKeyDerivedState | string
  group_id?: string | null
  kind?: IdentityKeyKind | string
  project_id?: string
  created_at?: string
}

export type IdentityKeyPage = Omit<components['schemas']['IdentityKeyPage'], 'items'> & {
  items: IdentityKey[]
}

export type IdentityKeyGroup = components['schemas']['IdentityKeyGroup'] & {
  subscription_type?: string | null
  rate_multiplier?: number
  workspace_id?: string | number | null
  project_id?: string | number | null
  status?: string
  description?: string | null
}
export type IdentityKeyGroupPage = Omit<components['schemas']['IdentityKeyGroupPage'], 'items'> & {
  items: IdentityKeyGroup[]
}
export type IdentityKeyInput = components['schemas']['IdentityKeyInput']

export type IdentityAccess = components['schemas']['IdentityAccess'] & {
  issuable_key_actions?: string[]
}

export interface IdentityWorkspaceAccess {
  actor_id: string
  workspace_id: string
  project_id?: string | null
  actions: string[]
  scope_version: string
  expires_at: string
  membership_role?: string | null
  role_source?: string | null
  membership_role_source?: string | null
  effective_admin?: boolean
  workspace_admin?: boolean
  grantable_actions?: string[]
}

export function generateIdempotencyKey(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  return `${Date.now()}-${Math.random().toString(36).slice(2, 12)}`
}

export function normalizeIdentityKeyState(rawState: unknown): IdentityKeyDerivedState {
  switch (rawState) {
    case 'active':
    case 'disabled':
    case 'expired':
    case 'quota_exhausted':
    case 'no_credential':
    case 'unknown':
      return rawState
    default:
      // Treat missing or unrecognized state as unknown (fail closed).
      return 'unknown'
  }
}

export function resolveDefaultRoutingGroupId(
  groups: ReadonlyArray<{ id: number | string; subscription_type?: string | null }> | null | undefined
): number | null {
  if (!Array.isArray(groups) || groups.length === 0) return null
  const standardGroup = groups.find(
    (g) => !g.subscription_type || g.subscription_type === 'standard'
  )
  const target = standardGroup ?? groups[0]
  const parsed = Number(target?.id)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : null
}

function v2Url(path: string): string {
  return buildGatewayUrl(path.startsWith('/api/v2') ? path : `/api/v2${path}`)
}

function ifMatch(version: string): Record<string, string> {
  const clean = String(version).replace(/^"+|"+$/g, '')
  return { 'If-Match': `"${clean}"` }
}

async function getApiClient() {
  const { apiClient } = await import('@/api/client')
  return apiClient
}

// Retain idempotency tokens only for in-flight or ambiguously failed create payloads so
// retries reuse the same token while edited payloads automatically receive a new token.
const pendingProjectKeyCreates = new Map<string, string>()

export function clearPendingProjectKeyIdempotency(): void {
  pendingProjectKeyCreates.clear()
}

export const identityApi = {
  async listScopes(
    params?: { cursor?: string; limit?: number },
    signal?: AbortSignal
  ): Promise<IdentityWorkspacePage> {
    const apiClient = await getApiClient()
    const { data } = await apiClient.get<IdentityWorkspacePage>(v2Url('/me/scopes'), {
      baseURL: '',
      params,
      signal
    })
    return data
  },

  async listProjects(
    workspaceId: string,
    params?: { cursor?: string; limit?: number },
    signal?: AbortSignal
  ): Promise<IdentityProjectPage> {
    const apiClient = await getApiClient()
    const { data } = await apiClient.get<IdentityProjectPage>(
      v2Url(`/workspaces/${encodeURIComponent(workspaceId)}/projects`),
      { baseURL: '', params, signal }
    )
    return data
  },

  async getProject(workspaceId: string, projectId: string, signal?: AbortSignal): Promise<IdentityProject> {
    const apiClient = await getApiClient()
    const { data } = await apiClient.get<IdentityProject>(
      v2Url(`/workspaces/${encodeURIComponent(workspaceId)}/projects/${encodeURIComponent(projectId)}`),
      { baseURL: '', signal }
    )
    return data
  },

  async createProject(
    workspaceId: string,
    nameOrInput: string | { name: string },
    idempotencyKey = generateIdempotencyKey()
  ): Promise<IdentityProject> {
    const apiClient = await getApiClient()
    const name = typeof nameOrInput === 'string' ? nameOrInput : nameOrInput.name
    const { data } = await apiClient.post<IdentityProject>(
      v2Url(`/workspaces/${encodeURIComponent(workspaceId)}/projects`),
      { name },
      { baseURL: '', headers: { 'Idempotency-Key': idempotencyKey } }
    )
    return data
  },

  async renameProject(
    workspaceId: string,
    projectId: string,
    nameOrInput: string | { name: string },
    version: string
  ): Promise<IdentityProject> {
    const apiClient = await getApiClient()
    const name = typeof nameOrInput === 'string' ? nameOrInput : nameOrInput.name
    const { data } = await apiClient.patch<IdentityProject>(
      v2Url(`/workspaces/${encodeURIComponent(workspaceId)}/projects/${encodeURIComponent(projectId)}`),
      { name },
      { baseURL: '', headers: ifMatch(version) }
    )
    return data
  },

  async archiveProject(workspaceId: string, projectId: string, version: string): Promise<IdentityProject> {
    const apiClient = await getApiClient()
    const { data } = await apiClient.delete<IdentityProject>(
      v2Url(`/workspaces/${encodeURIComponent(workspaceId)}/projects/${encodeURIComponent(projectId)}`),
      { baseURL: '', headers: ifMatch(version) }
    )
    return data
  },

  async getWorkspaceAccess(workspaceId: string, signal?: AbortSignal): Promise<IdentityWorkspaceAccess> {
    const apiClient = await getApiClient()
    const { data } = await apiClient.get<IdentityWorkspaceAccess>(
      v2Url(`/workspaces/${encodeURIComponent(workspaceId)}/access`),
      { baseURL: '', signal }
    )
    return data
  },

  async getProjectAccess(
    workspaceId: string,
    projectId: string,
    signal?: AbortSignal
  ): Promise<IdentityAccess> {
    const apiClient = await getApiClient()
    const { data } = await apiClient.get<IdentityAccess>(
      v2Url(`/workspaces/${encodeURIComponent(workspaceId)}/projects/${encodeURIComponent(projectId)}/access`),
      { baseURL: '', signal }
    )
    return data
  },

  async listMembers(
    workspaceId: string,
    params?: { cursor?: string; limit?: number },
    signal?: AbortSignal
  ): Promise<IdentityMemberPage> {
    const apiClient = await getApiClient()
    const { data } = await apiClient.get<
      Array<{ user_id: number | string; role?: string; email?: string; version?: string }> | IdentityMemberPage
    >(v2Url(`/workspaces/${encodeURIComponent(workspaceId)}/members`), { baseURL: '', params, signal })
    if (Array.isArray(data)) {
      return {
        items: data.map((m) => ({
          id: String(m.user_id),
          user_id: String(m.user_id),
          email: m.email,
          role: m.role,
          version: m.version ?? '0'
        })),
        next_cursor: null
      }
    }
    return data
  },

  async getMemberGrants(
    workspaceId: string,
    memberId: string,
    signal?: AbortSignal
  ): Promise<IdentityMemberGrants> {
    const apiClient = await getApiClient()
    const { data } = await apiClient.get<IdentityMemberGrants>(
      v2Url(`/workspaces/${encodeURIComponent(workspaceId)}/members/${encodeURIComponent(memberId)}/grants`),
      { baseURL: '', signal }
    )
    return data
  },

  async updateMemberGrants(
    workspaceId: string,
    memberId: string,
    input: IdentityGrantInput,
    version: string
  ): Promise<IdentityGrantVersion> {
    const apiClient = await getApiClient()
    const { data } = await apiClient.put<IdentityGrantVersion>(
      v2Url(`/workspaces/${encodeURIComponent(workspaceId)}/members/${encodeURIComponent(memberId)}/grants`),
      input,
      { baseURL: '', headers: ifMatch(version) }
    )
    return data
  },

  async listProjectKeyGroups(
    workspaceId: string,
    projectId: string,
    params?: { cursor?: string; limit?: number },
    signal?: AbortSignal
  ): Promise<IdentityKeyGroupPage> {
    const apiClient = await getApiClient()
    const { data } = await apiClient.get<IdentityKeyGroupPage>(
      v2Url(`/workspaces/${encodeURIComponent(workspaceId)}/projects/${encodeURIComponent(projectId)}/key-groups`),
      { baseURL: '', params, signal }
    )
    return data
  },

  async listProjectKeys(
    workspaceId: string,
    projectId: string,
    params?: { cursor?: string; limit?: number },
    signal?: AbortSignal
  ): Promise<IdentityKeyPage> {
    const apiClient = await getApiClient()
    const { data } = await apiClient.get<IdentityKeyPage>(
      v2Url(`/workspaces/${encodeURIComponent(workspaceId)}/projects/${encodeURIComponent(projectId)}/keys`),
      { baseURL: '', params, signal }
    )
    return data
  },

  async createProjectKey(
    workspaceId: string,
    projectId: string,
    input: IdentityKeyInput,
    explicitIdempotencyKey?: string
  ): Promise<IdentityKey> {
    const apiClient = await getApiClient()
    const targetUrl = v2Url(`/workspaces/${encodeURIComponent(workspaceId)}/projects/${encodeURIComponent(projectId)}/keys`)
    const submissionSignature = JSON.stringify([targetUrl, input])
    let token = explicitIdempotencyKey
    if (!token) {
      token = pendingProjectKeyCreates.get(submissionSignature)
      if (!token) {
        token = generateIdempotencyKey()
        pendingProjectKeyCreates.set(submissionSignature, token)
      }
    }
    try {
      const { data } = await apiClient.post<IdentityKey>(
        targetUrl,
        input,
        { baseURL: '', headers: { 'Idempotency-Key': token } }
      )
      if (!explicitIdempotencyKey && pendingProjectKeyCreates.get(submissionSignature) === token) {
        pendingProjectKeyCreates.delete(submissionSignature)
      }
      return data
    } catch (error) {
      const status = extractApiErrorStatus(error)
      if (
        !explicitIdempotencyKey &&
        status !== undefined &&
        status >= 400 &&
        status < 500 &&
        pendingProjectKeyCreates.get(submissionSignature) === token
      ) {
        pendingProjectKeyCreates.delete(submissionSignature)
      }
      throw error
    }
  }
}
