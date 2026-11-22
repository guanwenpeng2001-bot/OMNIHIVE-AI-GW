import { computed } from 'vue'
import { storeToRefs } from 'pinia'
import { useAppStore, type ScopeSelection } from '@/stores/app'
import type { components } from '@/api/generated/openapi'
import { buildGatewayUrl } from '@/api/url'
import { workspaceIDFromPath } from '@/api/workspaceScope'

export type IdentityWorkspace = components['schemas']['IdentityWorkspace'] & {
  workspace_name?: string
  workspace_state?: string
}
export type IdentityProjectSummary = IdentityWorkspace['projects'][number]
export type IdentityWorkspacePage = components['schemas']['IdentityWorkspacePage']
export type IdentityProject = components['schemas']['IdentityProject'] & {
  state?: string
}
export type IdentityProjectPage = Omit<components['schemas']['IdentityProjectPage'], 'items'> & {
  items: IdentityProject[]
}
export type IdentityMember = components['schemas']['IdentityMember']
export type IdentityMemberPage = components['schemas']['IdentityMemberPage']
export type IdentityGrantInput = components['schemas']['IdentityGrantInput']
export type IdentityGrantVersion = components['schemas']['IdentityGrantVersion']
export type IdentityMemberGrants = components['schemas']['IdentityMemberGrants']
export type IdentityKey = components['schemas']['IdentityKey']
export type IdentityKeyPage = Omit<components['schemas']['IdentityKeyPage'], 'items'> & {
  items: IdentityKey[]
}
export type IdentityKeyInput = components['schemas']['IdentityKeyInput']
export type IdentityAccess = components['schemas']['IdentityAccess']

export const SCOPE_STORAGE_KEY = 'omnihive.scope.selection.v1'
export const WORKSPACE_ID_STORAGE_KEY = 'omnihive.workspace_id.v1'
export const PROJECT_ID_STORAGE_KEY = 'omnihive.project_id.v1'
export { SCOPE_CHANGED_EVENT } from '@/stores/app'

export type ConsoleScopeSelection = ScopeSelection

export function generateIdempotencyKey(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  return `${Date.now()}-${Math.random().toString(36).slice(2, 12)}`
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

export function readPersistedConsoleScope(): ConsoleScopeSelection {
  if (typeof window === 'undefined') {
    return { mode: 'personal', workspaceId: null, projectId: null }
  }
  const pathWs = workspaceIDFromPath(window.location.pathname)
  try {
    const raw = window.localStorage.getItem(SCOPE_STORAGE_KEY)
    if (raw) {
      const parsed = JSON.parse(raw) as Partial<ConsoleScopeSelection>
      if (!pathWs && parsed.mode === 'personal') return { mode: 'personal', workspaceId: null, projectId: null }
      const ws = pathWs || (typeof parsed.workspaceId === 'string' && /^[1-9]\d*$/.test(parsed.workspaceId) ? parsed.workspaceId : null)
      const proj = (!pathWs || pathWs === parsed.workspaceId) && typeof parsed.projectId === 'string' && /^[1-9]\d*$/.test(parsed.projectId) ? parsed.projectId : null
      if (ws) {
        return { mode: 'workspace', workspaceId: ws, projectId: proj }
      }
    }
    const directWs = pathWs || window.localStorage.getItem(WORKSPACE_ID_STORAGE_KEY)
    const directProj = pathWs && pathWs !== window.localStorage.getItem(WORKSPACE_ID_STORAGE_KEY) ? null : window.localStorage.getItem(PROJECT_ID_STORAGE_KEY)
    if (directWs && /^[1-9]\d*$/.test(directWs)) {
      return {
        mode: 'workspace',
        workspaceId: directWs,
        projectId: directProj && /^[1-9]\d*$/.test(directProj) ? directProj : null
      }
    }
  } catch {
    // Ignore storage read failures
  }
  return { mode: 'personal', workspaceId: null, projectId: null }
}

export function persistConsoleScope(selection: ConsoleScopeSelection): void {
  useAppStore().selectScope(selection)
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

export const identityApi = {
  async listScopes(params?: { cursor?: string; limit?: number }, signal?: AbortSignal): Promise<IdentityWorkspacePage> {
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
      v2Url(`/workspaces/${workspaceId}/projects`),
      { baseURL: '', params, signal }
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
      v2Url(`/workspaces/${workspaceId}/projects`),
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
      v2Url(`/workspaces/${workspaceId}/projects/${projectId}`),
      { name },
      { baseURL: '', headers: ifMatch(version) }
    )
    return data
  },

  async archiveProject(workspaceId: string, projectId: string, version: string): Promise<IdentityProject> {
    const apiClient = await getApiClient()
    const { data } = await apiClient.delete<IdentityProject>(
      v2Url(`/workspaces/${workspaceId}/projects/${projectId}`),
      { baseURL: '', headers: ifMatch(version) }
    )
    return data
  },

  async getWorkspaceAccess(workspaceId: string, signal?: AbortSignal): Promise<IdentityAccess> {
    const apiClient = await getApiClient()
    const { data } = await apiClient.get<IdentityAccess>(
      v2Url(`/workspaces/${workspaceId}/access`),
      { baseURL: '', signal }
    )
    return data
  },

  async getProjectAccess(workspaceId: string, projectId: string, signal?: AbortSignal): Promise<IdentityAccess> {
    const apiClient = await getApiClient()
    const { data } = await apiClient.get<IdentityAccess>(
      v2Url(`/workspaces/${workspaceId}/projects/${projectId}/access`),
      { baseURL: '', signal }
    )
    return data
  },

  async listMembers(workspaceId: string, params?: { cursor?: string; limit?: number }, signal?: AbortSignal): Promise<IdentityMemberPage> {
    const apiClient = await getApiClient()
    const { data } = await apiClient.get<IdentityMemberPage>(v2Url(`/workspaces/${workspaceId}/members`), { baseURL: '', params, signal })
    return data
  },

  async updateProjectGrants(workspaceId: string, memberId: string, projectId: string, actions: string[], version: string): Promise<IdentityGrantVersion> {
    const apiClient = await getApiClient()
    const { data } = await apiClient.put<IdentityGrantVersion>(v2Url(`/workspaces/${workspaceId}/members/${memberId}/grants/projects/${projectId}`), { actions }, { baseURL: '', headers: ifMatch(version) })
    return data
  },

  async listKeyGroups(workspaceId: string, projectId: string, params?: { cursor?: string; limit?: number }) {
    const apiClient = await getApiClient()
    const { data } = await apiClient.get<components['schemas']['IdentityKeyGroupPage']>(v2Url(`/workspaces/${workspaceId}/projects/${projectId}/key-groups`), { baseURL: '', params })
    return data
  },

  async getMemberGrants(
    workspaceId: string,
    memberId: string,
    signal?: AbortSignal
  ): Promise<IdentityMemberGrants> {
    const apiClient = await getApiClient()
    const { data } = await apiClient.get<IdentityMemberGrants>(
      v2Url(`/workspaces/${workspaceId}/members/${memberId}/grants`),
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
      v2Url(`/workspaces/${workspaceId}/members/${memberId}/grants`),
      input,
      { baseURL: '', headers: ifMatch(version) }
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
      v2Url(`/workspaces/${workspaceId}/projects/${projectId}/keys`),
      { baseURL: '', params, signal }
    )
    return data
  },

  async createProjectKey(
    workspaceId: string,
    projectId: string,
    input: IdentityKeyInput,
    idempotencyKey = generateIdempotencyKey()
  ): Promise<IdentityKey> {
    const apiClient = await getApiClient()
    const { data } = await apiClient.post<IdentityKey>(
      v2Url(`/workspaces/${workspaceId}/projects/${projectId}/keys`),
      input,
      { baseURL: '', headers: { 'Idempotency-Key': idempotencyKey } }
    )
    return data
  }
}

export function useConsoleScope() {
  const app = useAppStore()
  const state = storeToRefs(app)
  const selection = state.activeScope
  const activeWorkspace = state.activeWorkspace
  const activeProject = state.activeProject
  const activeWorkspaceId = computed(() => activeWorkspace.value?.workspace_id ?? '')
  const activeProjectId = computed(() => activeProject.value?.id ?? '')
  const availableProjects = computed(() => activeWorkspace.value?.projects ?? [])

  async function refreshScopes(force = false): Promise<void> {
    await app.fetchScopes({ force })
    const seen = new Set<string>()
    while (app.scopesNextCursor && !seen.has(app.scopesNextCursor)) {
      const cursor = app.scopesNextCursor
      seen.add(cursor)
      if (!await app.fetchScopes({ cursor })) break
    }
    // A selected project on a later page is not stale merely because it was not
    // present in the workspace summary. Preserve the explicit selection.
    const ws = app.activeWorkspace
    const projectCursors = new Set<string>()
    while (ws && app.activeScope.projectId && !app.activeProject) {
      const current = app.scopes.find(item => item.workspace_id === ws.workspace_id)
      if (!current?.projects_next_cursor || projectCursors.has(current.projects_next_cursor)) break
      projectCursors.add(current.projects_next_cursor)
      if (!await app.loadMoreWorkspaceProjects(ws.workspace_id)) break
    }
  }

  return {
    scopes: state.scopes,
    loading: state.scopesLoading,
    refreshing: state.scopesRefreshing,
    loaded: state.scopesLoaded,
    lastSyncedAt: computed(() => app.scopesAsOf ?? ''),
    scopeError: state.scopesError,
    selection, activeWorkspace, activeWorkspaceId, availableProjects, activeProject, activeProjectId,
    refreshScopes,
    selectWorkspaceProject: (workspaceId: string, projectId: string | null) => app.selectWorkspaceScope(workspaceId, projectId),
    selectPersonalScope: () => app.selectPersonalScope()
  }
}
