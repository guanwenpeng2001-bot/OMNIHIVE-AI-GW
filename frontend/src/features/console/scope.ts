import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import type { components } from '@/api/generated/v2'
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
export interface IdentityMember {
  id: string
  user_id?: string
  email?: string
  role?: string
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
export type IdentityKey = components['schemas']['IdentityKey'] & {
  state?: string
}
export type IdentityKeyPage = Omit<components['schemas']['IdentityKeyPage'], 'items'> & {
  items: IdentityKey[]
}
export type IdentityKeyInput = components['schemas']['IdentityKeyInput']
export type IdentityAccess = components['schemas']['IdentityAccess']

export const SCOPE_STORAGE_KEY = 'omnihive.scope.selection.v1'
export const WORKSPACE_ID_STORAGE_KEY = 'omnihive.workspace_id.v1'
export const PROJECT_ID_STORAGE_KEY = 'omnihive.project_id.v1'
export const SCOPE_CHANGED_EVENT = 'omnihive:scope-changed'

export interface ConsoleScopeSelection {
  mode: 'personal' | 'workspace'
  workspaceId: string | null
  projectId: string | null
}

export function generateIdempotencyKey(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  return `${Date.now()}-${Math.random().toString(36).slice(2, 12)}`
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
      const ws = pathWs || (typeof parsed.workspaceId === 'string' && /^[1-9]\d*$/.test(parsed.workspaceId) ? parsed.workspaceId : null)
      const proj = typeof parsed.projectId === 'string' && /^[1-9]\d*$/.test(parsed.projectId) ? parsed.projectId : null
      if (ws) {
        return { mode: 'workspace', workspaceId: ws, projectId: proj }
      }
    }
    const directWs = pathWs || window.localStorage.getItem(WORKSPACE_ID_STORAGE_KEY)
    const directProj = window.localStorage.getItem(PROJECT_ID_STORAGE_KEY)
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
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(SCOPE_STORAGE_KEY, JSON.stringify(selection))
    if (selection.mode === 'workspace' && selection.workspaceId) {
      window.localStorage.setItem(WORKSPACE_ID_STORAGE_KEY, selection.workspaceId)
      if (selection.projectId) {
        window.localStorage.setItem(PROJECT_ID_STORAGE_KEY, selection.projectId)
      } else {
        window.localStorage.removeItem(PROJECT_ID_STORAGE_KEY)
      }
    } else {
      window.localStorage.removeItem(WORKSPACE_ID_STORAGE_KEY)
      window.localStorage.removeItem(PROJECT_ID_STORAGE_KEY)
    }
    window.dispatchEvent(new CustomEvent(SCOPE_CHANGED_EVENT, { detail: selection }))
  } catch {
    // Ignore storage write failures
  }
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

export const identityV2Api = {
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

  async getProjectAccess(workspaceId: string, projectId: string, signal?: AbortSignal): Promise<IdentityAccess> {
    const apiClient = await getApiClient()
    const { data } = await apiClient.get<IdentityAccess>(
      v2Url(`/workspaces/${workspaceId}/projects/${projectId}/access`),
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
    const { data } = await apiClient.get<IdentityMemberPage>(
      v2Url(`/workspaces/${workspaceId}/members`),
      { baseURL: '', params, signal }
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
  },

  async rotateProjectKey(
    workspaceId: string,
    projectId: string,
    keyId: string,
    version: string,
    idempotencyKey = generateIdempotencyKey()
  ): Promise<IdentityKey> {
    const apiClient = await getApiClient()
    const { data } = await apiClient.post<IdentityKey>(
      v2Url(`/workspaces/${workspaceId}/projects/${projectId}/keys/${keyId}/rotate`),
      {},
      {
        baseURL: '',
        headers: {
          ...ifMatch(version),
          'Idempotency-Key': idempotencyKey
        }
      }
    )
    return data
  }
}

export function useConsoleScope() {
  const scopes = ref<IdentityWorkspace[]>([])
  const loading = ref(false)
  const refreshing = ref(false)
  const loaded = ref(false)
  const lastSyncedAt = ref<string>('')
  const scopeError = ref<unknown>(null)
  const selection = ref<ConsoleScopeSelection>(readPersistedConsoleScope())

  const activeWorkspace = computed(
    () => scopes.value.find((w) => w.workspace_id === selection.value.workspaceId) ?? scopes.value[0] ?? null
  )

  const activeWorkspaceId = computed(
    () => selection.value.workspaceId || activeWorkspace.value?.workspace_id || ''
  )

  const availableProjects = computed(() => activeWorkspace.value?.projects ?? [])

  const activeProject = computed(() => {
    const list = availableProjects.value
    if (selection.value.projectId) {
      const match = list.find((p) => p.id === selection.value.projectId)
      if (match) return match
    }
    return list[0] ?? null
  })

  const activeProjectId = computed(() => activeProject.value?.id || selection.value.projectId || '')

  async function refreshScopes(force = false): Promise<void> {
    if (loading.value || refreshing.value) return
    if (loaded.value && !force) return
    if (loaded.value) {
      refreshing.value = true
    } else {
      loading.value = true
    }
    scopeError.value = null
    try {
      const page = await identityV2Api.listScopes({ limit: 50 })
      scopes.value = page.items ?? []
      lastSyncedAt.value = page.as_of || new Date().toISOString()
      loaded.value = true

      if (!selection.value.workspaceId && scopes.value.length > 0) {
        const firstWs = scopes.value[0]
        const firstProj = firstWs.projects?.[0]?.id ?? null
        selectWorkspaceProject(firstWs.workspace_id, firstProj)
      } else if (selection.value.workspaceId) {
        const ws = scopes.value.find((item) => item.workspace_id === selection.value.workspaceId)
        if (ws && !selection.value.projectId && ws.projects.length > 0) {
          selectWorkspaceProject(ws.workspace_id, ws.projects[0].id)
        }
      }
    } catch (err) {
      scopeError.value = err
    } finally {
      loading.value = false
      refreshing.value = false
    }
  }

  function selectWorkspaceProject(workspaceId: string, projectId: string | null): void {
    const next: ConsoleScopeSelection = {
      mode: 'workspace',
      workspaceId,
      projectId
    }
    selection.value = next
    persistConsoleScope(next)
  }

  function handleExternalScopeEvent(): void {
    selection.value = readPersistedConsoleScope()
  }

  onMounted(() => {
    if (typeof window !== 'undefined') {
      window.addEventListener(SCOPE_CHANGED_EVENT, handleExternalScopeEvent)
    }
  })

  onBeforeUnmount(() => {
    if (typeof window !== 'undefined') {
      window.removeEventListener(SCOPE_CHANGED_EVENT, handleExternalScopeEvent)
    }
  })

  return {
    scopes,
    loading,
    refreshing,
    loaded,
    lastSyncedAt,
    scopeError,
    selection,
    activeWorkspace,
    activeWorkspaceId,
    availableProjects,
    activeProject,
    activeProjectId,
    refreshScopes,
    selectWorkspaceProject
  }
}
