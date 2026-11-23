import { computed, ref } from 'vue'
import { createPinia, defineStore, getActivePinia, setActivePinia } from 'pinia'
import { isSilentRequestError } from '@/api/client'
import {
  identityApi,
  type IdentityProjectPage,
  type IdentityProjectSummary,
  type IdentityWorkspace,
  type IdentityWorkspacePage
} from '@/api/identity'
import {
  StorageKeys,
  authStorage,
  clearPersistedScopeSelection,
  scopeSelectionStorage
} from '@/api/tokenRefresh'
import { workspaceIDFromPath } from '@/api/workspaceScope'
import {
  ensureProjectAccess,
  ensureWorkspaceAccess,
  getAccessActionState,
  getCachedProjectAccess,
  getCachedWorkspaceAccess,
  invalidateScopeAccess,
  resetScopeAccessCache,
  type AccessProbeEntry,
  type PermissionProbeState
} from '@/features/console/scopeAccess'
import { i18n } from '@/i18n'
import { normalizeApiError } from '@/utils/apiError'

export type ScopeMode = 'personal' | 'workspace'

export interface ScopeSelection {
  mode: ScopeMode
  workspaceId: string | null
  projectId: string | null
}

export interface ScopeErrorState {
  status: number
  reason?: string
  message: string
  requestId?: string
}

export type ScopeValidationStatus = 'idle' | 'validating' | 'ready' | 'unavailable'
export type ScopeUnavailableReason = 'not_found' | 'archived' | 'revoked' | 'malformed' | null

export const SCOPE_CHANGED_EVENT = 'omnihive:scope-changed'
export const OPEN_SCOPE_SWITCHER_EVENT = 'omnihive:open-scope-switcher'

export function requestScopeSwitcher(): void {
  window.dispatchEvent(new Event(OPEN_SCOPE_SWITCHER_EVENT))
}

interface PersistedScopeEnvelope {
  v: 2
  actorId: string | null
  mode: ScopeMode
  workspaceId: string | null
  projectId: string | null
}

const USER_SCOPED_PREFIXES = [
  '/dashboard',
  '/keys',
  '/byok',
  '/playground',
  '/requests',
  '/tasks',
  '/billing',
  '/usage',
  '/workspaces',
  '/monitor',
  '/available-channels',
  '/batch-image'
] as const

const ADMIN_SCOPED_PREFIXES = [
  '/admin/audit',
  '/admin/members',
  '/admin/requests',
  '/admin/billing',
  '/admin/ops'
] as const

function matchesPrefix(path: string, prefix: string): boolean {
  return path === prefix || path.startsWith(`${prefix}/`)
}

function isPositiveNumericId(value: unknown): value is string {
  return typeof value === 'string' && /^[1-9]\d*$/.test(value.trim())
}

/**
 * Determines whether a console route operates on Workspace / Project scope
 * and should display the compact ScopeSwitcher in AppHeader.
 */
export function routeUsesConsoleScope(rawPath: string | null | undefined): boolean {
  const cleanPath = String(rawPath || '').split(/[?#]/)[0] || ''
  if (!cleanPath) return false
  if (workspaceIDFromPath(cleanPath)) return true

  if (cleanPath.startsWith('/admin')) {
    if (cleanPath === '/admin/audit-logs') return false
    return ADMIN_SCOPED_PREFIXES.some((prefix) => matchesPrefix(cleanPath, prefix))
  }

  return USER_SCOPED_PREFIXES.some((prefix) => matchesPrefix(cleanPath, prefix))
}

export function resolveScopeActorId(): string {
  const raw = authStorage.user.get()
  if (!raw) return ''
  try {
    const parsed = JSON.parse(raw) as { id?: number | string } | null
    if (parsed && parsed.id !== undefined && parsed.id !== null) {
      return String(parsed.id).trim()
    }
  } catch {
    // Ignore malformed user storage payloads.
  }
  return ''
}

export function isDefaultSharedScope(
  workspace: Pick<IdentityWorkspace, 'is_default'> | null | undefined,
  project: Pick<IdentityProjectSummary, 'is_default'> | null | undefined
): boolean {
  return Boolean(workspace?.is_default === true && project?.is_default === true)
}

export function readPersistedScopeDetails(actorIdOverride?: string): {
  selection: ScopeSelection
  untrustedPending: boolean
} {
  const currentActorId = actorIdOverride ?? resolveScopeActorId()
  const stored = scopeSelectionStorage.getJSON<(Partial<ScopeSelection> & Partial<PersistedScopeEnvelope>) | null>(null)
  if (stored && typeof stored === 'object') {
    if (stored.v === 2) {
      const storedActor = typeof stored.actorId === 'string' ? stored.actorId.trim() : ''
      if (currentActorId && storedActor && storedActor !== currentActorId) {
        clearPersistedScopeSelection()
        return {
          selection: { mode: 'personal', workspaceId: null, projectId: null },
          untrustedPending: false
        }
      }
      if (stored.mode === 'personal') {
        return {
          selection: { mode: 'personal', workspaceId: null, projectId: null },
          untrustedPending: false
        }
      }
      if (stored.mode === 'workspace' && isPositiveNumericId(stored.workspaceId)) {
        return {
          selection: {
            mode: 'workspace',
            workspaceId: stored.workspaceId.trim(),
            projectId: isPositiveNumericId(stored.projectId) ? stored.projectId.trim() : null
          },
          untrustedPending: !storedActor || (Boolean(currentActorId) && storedActor !== currentActorId)
        }
      }
      return {
        selection: { mode: 'personal', workspaceId: null, projectId: null },
        untrustedPending: false
      }
    }

    // Legacy unbound envelope: migrate as untrusted pending intent that must be validated.
    if (stored.mode === 'workspace' && isPositiveNumericId(stored.workspaceId)) {
      return {
        selection: {
          mode: 'workspace',
          workspaceId: stored.workspaceId.trim(),
          projectId: isPositiveNumericId(stored.projectId) ? stored.projectId.trim() : null
        },
        untrustedPending: true
      }
    }
    if (stored.mode === 'personal') {
      return {
        selection: { mode: 'personal', workspaceId: null, projectId: null },
        untrustedPending: false
      }
    }
  }

  try {
    if (typeof localStorage !== 'undefined') {
      const wsId = localStorage.getItem(StorageKeys.WORKSPACE_ID)
      if (isPositiveNumericId(wsId)) {
        const projId = localStorage.getItem(StorageKeys.PROJECT_ID)
        return {
          selection: {
            mode: 'workspace',
            workspaceId: wsId.trim(),
            projectId: isPositiveNumericId(projId) ? projId.trim() : null
          },
          untrustedPending: true
        }
      }
    }
  } catch {
    // Ignore storage read failures in restricted environments.
  }

  return {
    selection: { mode: 'personal', workspaceId: null, projectId: null },
    untrustedPending: false
  }
}

/**
 * Reads persisted scope selection written by `consoleScope` or legacy storage adapters.
 */
export function readPersistedGlobalScope(actorIdOverride?: string): ScopeSelection {
  return readPersistedScopeDetails(actorIdOverride).selection
}

export function persistScopeSelectionToStorage(
  selection: ScopeSelection,
  actorIdOverride?: string | null
): void {
  const actorId = actorIdOverride !== undefined ? actorIdOverride : resolveScopeActorId() || null
  const envelope: PersistedScopeEnvelope = {
    v: 2,
    actorId,
    mode: selection.mode,
    workspaceId: selection.workspaceId,
    projectId: selection.projectId
  }
  scopeSelectionStorage.setJSON(envelope)
  try {
    if (typeof localStorage === 'undefined') return
    if (selection.mode === 'workspace' && selection.workspaceId) {
      localStorage.setItem(StorageKeys.WORKSPACE_ID, selection.workspaceId)
      if (selection.projectId) {
        localStorage.setItem(StorageKeys.PROJECT_ID, selection.projectId)
      } else {
        localStorage.removeItem(StorageKeys.PROJECT_ID)
      }
    } else {
      localStorage.removeItem(StorageKeys.WORKSPACE_ID)
      localStorage.removeItem(StorageKeys.PROJECT_ID)
    }
  } catch {
    // Ignore storage write errors.
  }
}

function normalizeWorkspaceRecord(raw: IdentityWorkspace): IdentityWorkspace {
  const seenProjects = new Set<string>()
  const projects: IdentityProjectSummary[] = []
  for (const proj of raw.projects ?? []) {
    if (!proj?.id || seenProjects.has(proj.id)) continue
    seenProjects.add(proj.id)
    projects.push({ ...proj })
  }
  return {
    ...raw,
    name: raw.name || raw.workspace_name || `#${raw.workspace_id}`,
    projects
  }
}

function extractHttpStatus(err: unknown): number {
  const raw = (err ?? {}) as { status?: number; response?: { status?: number } }
  return raw.status ?? raw.response?.status ?? 0
}

function extractRequestId(err: unknown, fallback?: string): string | undefined {
  const raw = (err ?? {}) as {
    requestId?: string
    metadata?: Record<string, unknown>
    response?: { data?: { metadata?: Record<string, unknown> }; headers?: Record<string, string> }
  }
  return (
    raw.requestId ||
    (typeof raw.metadata?.request_id === 'string' ? raw.metadata.request_id : undefined) ||
    (typeof raw.response?.data?.metadata?.request_id === 'string'
      ? raw.response.data.metadata.request_id
      : undefined) ||
    raw.response?.headers?.['x-request-id'] ||
    fallback
  )
}

export const useConsoleScopeStore = defineStore('consoleScope', () => {
  const initial = readPersistedScopeDetails()

  const scopes = ref<IdentityWorkspace[]>([])
  const scopesLoaded = ref<boolean>(false)
  const scopesLoading = ref<boolean>(false)
  const scopesRefreshing = ref<boolean>(false)
  const scopesStale = ref<boolean>(false)
  const scopesError = ref<ScopeErrorState | null>(null)
  const scopesNextCursor = ref<string | null>(null)
  const scopesAsOf = ref<string | null>(null)
  const activeScope = ref<ScopeSelection>(initial.selection)
  const scopeRevision = ref<number>(0)
  const untrustedPending = ref<boolean>(initial.untrustedPending)
  const scopeValidationStatus = ref<ScopeValidationStatus>(
    initial.selection.mode === 'personal' ? 'ready' : 'validating'
  )
  const scopeUnavailableReason = ref<ScopeUnavailableReason>(null)

  let scopesRequest: Promise<IdentityWorkspacePage | null> | null = null
  let scopesLifecycleGeneration = 0
  const seenOuterCursors = new Set<string>()
  const seenProjectCursorsByWs = new Map<string, Set<string>>()
  let validatingKey: string | null = null

  const activeWorkspace = computed<IdentityWorkspace | null>(() => {
    if (activeScope.value.mode !== 'workspace' || !activeScope.value.workspaceId) return null
    return scopes.value.find((ws) => ws.workspace_id === activeScope.value.workspaceId) ?? null
  })

  const availableProjects = computed<IdentityProjectSummary[]>(() => activeWorkspace.value?.projects ?? [])

  const activeProject = computed<IdentityProjectSummary | null>(() => {
    const ws = activeWorkspace.value
    if (!ws || !activeScope.value.projectId) return null
    return ws.projects.find((proj) => proj.id === activeScope.value.projectId) ?? null
  })

  const activeWorkspaceId = computed<string>(() =>
    activeScope.value.mode === 'workspace' ? (activeScope.value.workspaceId ?? '') : ''
  )

  const activeProjectId = computed<string>(() =>
    activeScope.value.mode === 'workspace' ? (activeScope.value.projectId ?? '') : ''
  )

  const isPersonalMode = computed(() => activeScope.value.mode === 'personal')
  const isWorkspaceOverview = computed(
    () => activeScope.value.mode === 'workspace' && !activeScope.value.projectId
  )
  const isProjectMode = computed(
    () => activeScope.value.mode === 'workspace' && Boolean(activeScope.value.projectId)
  )
  const isActiveDefaultShared = computed(() =>
    isDefaultSharedScope(activeWorkspace.value, activeProject.value)
  )

  const availableProjectOptions = computed(() =>
    scopes.value.flatMap((ws) =>
      ws.projects
        .filter((proj) => proj.state !== 'archived')
        .map((proj) => ({
          workspaceId: ws.workspace_id,
          workspaceName: ws.name || ws.workspace_name || `#${ws.workspace_id}`,
          projectId: proj.id,
          projectName: proj.name,
          isDefaultShared: isDefaultSharedScope(ws, proj)
        }))
    )
  )

  function notifyScopeChanged(selection: ScopeSelection): void {
    scopeRevision.value += 1
    if (typeof window !== 'undefined') {
      window.dispatchEvent(
        new CustomEvent(SCOPE_CHANGED_EVENT, {
          detail: { ...selection, revision: scopeRevision.value }
        })
      )
    }
  }

  function evaluateLoadedScope(): void {
    if (activeScope.value.mode === 'personal') {
      untrustedPending.value = false
      scopeValidationStatus.value = 'ready'
      scopeUnavailableReason.value = null
      return
    }
    const wsId = activeScope.value.workspaceId
    if (!wsId || !isPositiveNumericId(wsId)) {
      scopeValidationStatus.value = 'unavailable'
      scopeUnavailableReason.value = 'malformed'
      return
    }
    const ws = scopes.value.find((item) => item.workspace_id === wsId)
    if (!ws) {
      if (scopesLoaded.value && !scopesNextCursor.value && !scopesError.value) {
        scopeValidationStatus.value = 'unavailable'
        scopeUnavailableReason.value = 'not_found'
      } else {
        scopeValidationStatus.value = scopesError.value && !scopesLoaded.value ? 'unavailable' : 'validating'
      }
      return
    }
    if (ws.workspace_state === 'archived') {
      scopeValidationStatus.value = 'unavailable'
      scopeUnavailableReason.value = 'archived'
      return
    }
    const projId = activeScope.value.projectId
    if (!projId) {
      untrustedPending.value = false
      scopeValidationStatus.value = 'ready'
      scopeUnavailableReason.value = null
      persistScopeSelectionToStorage(activeScope.value)
      return
    }
    const proj = ws.projects.find((item) => item.id === projId)
    if (!proj) {
      if (!ws.projects_next_cursor && !scopesError.value) {
        scopeValidationStatus.value = 'unavailable'
        scopeUnavailableReason.value = 'not_found'
      } else {
        scopeValidationStatus.value = scopesError.value ? 'unavailable' : 'validating'
      }
      return
    }
    if (proj.state === 'archived') {
      scopeValidationStatus.value = 'unavailable'
      scopeUnavailableReason.value = 'archived'
      return
    }
    untrustedPending.value = false
    scopeValidationStatus.value = 'ready'
    scopeUnavailableReason.value = null
    persistScopeSelectionToStorage(activeScope.value)
  }

  async function validateActiveScopeAcrossPages(): Promise<void> {
    if (activeScope.value.mode !== 'workspace' || !activeScope.value.workspaceId) {
      evaluateLoadedScope()
      return
    }
    const targetWsId = activeScope.value.workspaceId
    const targetProjId = activeScope.value.projectId
    const key = `${scopesLifecycleGeneration}:${targetWsId}:${targetProjId || ''}`
    if (validatingKey === key) return
    validatingKey = key
    const generation = scopesLifecycleGeneration

    try {
      let safety = 0
      while (
        generation === scopesLifecycleGeneration &&
        !scopes.value.some((w) => w.workspace_id === targetWsId) &&
        scopesNextCursor.value &&
        safety < 50
      ) {
        safety += 1
        const prevCursor = scopesNextCursor.value
        const page = await fetchScopes({ cursor: prevCursor })
        if (!page || generation !== scopesLifecycleGeneration) break
        if (scopesNextCursor.value === prevCursor) break
      }
      if (generation !== scopesLifecycleGeneration) return

      const ws = scopes.value.find((w) => w.workspace_id === targetWsId)
      if (ws && targetProjId) {
        let projSafety = 0
        while (
          generation === scopesLifecycleGeneration &&
          !ws.projects.some((p) => p.id === targetProjId) &&
          ws.projects_next_cursor &&
          projSafety < 50
        ) {
          projSafety += 1
          const prevProjCursor = ws.projects_next_cursor
          const page = await loadMoreWorkspaceProjects(ws.workspace_id)
          if (!page || generation !== scopesLifecycleGeneration) break
          const refreshedWs = scopes.value.find((w) => w.workspace_id === targetWsId)
          if (!refreshedWs || refreshedWs.projects_next_cursor === prevProjCursor) break
        }
      }
      if (generation === scopesLifecycleGeneration) {
        evaluateLoadedScope()
      }
    } finally {
      if (validatingKey === key) {
        validatingKey = null
      }
    }
  }

  function selectScope(next: ScopeSelection, options?: { untrusted?: boolean }): void {
    const normalized: ScopeSelection =
      next.mode === 'workspace' && isPositiveNumericId(next.workspaceId)
        ? {
            mode: 'workspace',
            workspaceId: next.workspaceId.trim(),
            projectId: isPositiveNumericId(next.projectId) ? next.projectId.trim() : null
          }
        : { mode: 'personal', workspaceId: null, projectId: null }

    const unchanged =
      activeScope.value.mode === normalized.mode &&
      activeScope.value.workspaceId === normalized.workspaceId &&
      activeScope.value.projectId === normalized.projectId

    activeScope.value = normalized
    untrustedPending.value = Boolean(options?.untrusted && normalized.mode === 'workspace')
    scopeUnavailableReason.value = null
    if (normalized.mode === 'personal') {
      scopeValidationStatus.value = 'ready'
    } else if (scopesLoaded.value) {
      evaluateLoadedScope()
      if (scopeValidationStatus.value === 'validating') {
        void validateActiveScopeAcrossPages()
      }
    } else {
      scopeValidationStatus.value = 'validating'
    }
    persistScopeSelectionToStorage(normalized)
    if (!unchanged) {
      invalidateScopeAccess()
      notifyScopeChanged(normalized)
    }
  }

  function selectPersonalScope(): void {
    selectScope({ mode: 'personal', workspaceId: null, projectId: null })
  }

  function selectWorkspaceScope(workspaceId: string, projectId?: string | null): void {
    if (!workspaceId) {
      selectPersonalScope()
      return
    }
    if (!isPositiveNumericId(workspaceId)) {
      scopeValidationStatus.value = 'unavailable'
      scopeUnavailableReason.value = 'malformed'
      return
    }
    if (projectId !== undefined && projectId !== null && projectId !== '' && !isPositiveNumericId(projectId)) {
      scopeValidationStatus.value = 'unavailable'
      scopeUnavailableReason.value = 'malformed'
      return
    }
    selectScope({
      mode: 'workspace',
      workspaceId: workspaceId.trim(),
      projectId: isPositiveNumericId(projectId) ? projectId.trim() : null
    })
  }

  function syncFromLocation(
    pathname: string,
    query?: Record<string, unknown>
  ): void {
    const pathWs = workspaceIDFromPath(pathname || '')
    const rawQueryWs = typeof query?.workspace_id === 'string' ? query.workspace_id.trim() : ''
    const rawQueryProj = typeof query?.project_id === 'string' ? query.project_id.trim() : ''

    if (rawQueryWs || rawQueryProj) {
      if (
        (rawQueryWs && !isPositiveNumericId(rawQueryWs)) ||
        (rawQueryProj && (!isPositiveNumericId(rawQueryProj) || (!rawQueryWs && !pathWs)))
      ) {
        scopeValidationStatus.value = 'unavailable'
        scopeUnavailableReason.value = 'malformed'
        return
      }
      const targetWs = rawQueryWs || pathWs || ''
      if (targetWs) {
        selectWorkspaceScope(targetWs, rawQueryProj || null)
        return
      }
    }

    if (pathWs) {
      if (
        activeScope.value.mode !== 'workspace' ||
        activeScope.value.workspaceId !== pathWs
      ) {
        // Workspace-only deep link never carries over a saved project from another workspace.
        selectWorkspaceScope(pathWs, null)
      }
    }
  }

  async function fetchScopes(
    options: { force?: boolean; cursor?: string; _retried410?: boolean } = {}
  ): Promise<IdentityWorkspacePage | null> {
    const cursor = options.cursor
    if (!cursor && !options.force && scopesLoaded.value) {
      evaluateLoadedScope()
      if (
        activeScope.value.mode === 'workspace' &&
        scopeValidationStatus.value === 'validating'
      ) {
        await validateActiveScopeAcrossPages()
      }
      return {
        items: scopes.value,
        next_cursor: scopesNextCursor.value,
        as_of: scopesAsOf.value ?? ''
      }
    }
    if (!cursor && scopesRequest) {
      return scopesRequest
    }
    if (cursor) {
      if (seenOuterCursors.has(cursor)) {
        scopesNextCursor.value = null
        return null
      }
      seenOuterCursors.add(cursor)
    } else {
      seenOuterCursors.clear()
      seenProjectCursorsByWs.clear()
    }

    const generation = scopesLifecycleGeneration
    if (scopesLoaded.value || cursor) {
      scopesRefreshing.value = true
    } else {
      scopesLoading.value = true
    }

    const run = identityApi
      .listScopes({
        limit: 50,
        ...(cursor ? { cursor } : {})
      })
      .then(async (data) => {
        if (generation !== scopesLifecycleGeneration) return null
        const incoming = Array.isArray(data?.items) ? data.items.map(normalizeWorkspaceRecord) : []
        if (cursor) {
          const existingIds = new Set(scopes.value.map((w) => w.workspace_id))
          scopes.value = [
            ...scopes.value,
            ...incoming.filter((w) => !existingIds.has(w.workspace_id))
          ]
        } else {
          const deduped: IdentityWorkspace[] = []
          const seen = new Set<string>()
          for (const ws of incoming) {
            if (!ws.workspace_id || seen.has(ws.workspace_id)) continue
            seen.add(ws.workspace_id)
            deduped.push(ws)
          }
          scopes.value = deduped
        }
        const rawNext = data?.next_cursor ?? null
        scopesNextCursor.value =
          rawNext && (rawNext === cursor || seenOuterCursors.has(rawNext)) ? null : rawNext
        scopesAsOf.value = data?.as_of ?? null
        scopesLoaded.value = true
        scopesStale.value = false
        scopesError.value = null
        evaluateLoadedScope()
        if (
          !cursor &&
          activeScope.value.mode === 'workspace' &&
          scopeValidationStatus.value === 'validating'
        ) {
          await validateActiveScopeAcrossPages()
        }
        return data
      })
      .catch((err: unknown) => {
        if (generation !== scopesLifecycleGeneration || isSilentRequestError(err)) {
          return null
        }
        const status = extractHttpStatus(err)
        if (status === 410 && cursor && !options._retried410) {
          seenOuterCursors.clear()
          return fetchScopes({ force: true, _retried410: true })
        }
        const normalized = normalizeApiError(err)
        scopesError.value = {
          status: status || (normalized.status ?? 0),
          reason: normalized.reason,
          message: normalized.message || i18n.global.t('common.unknownError'),
          requestId: extractRequestId(
            err,
            typeof normalized.metadata?.request_id === 'string'
              ? normalized.metadata.request_id
              : undefined
          )
        }
        if (scopesLoaded.value) {
          scopesStale.value = true
        }
        if (activeScope.value.mode === 'workspace' && !scopesLoaded.value) {
          scopeValidationStatus.value = 'unavailable'
        }
        return null
      })
      .finally(() => {
        if (generation === scopesLifecycleGeneration) {
          scopesLoading.value = false
          scopesRefreshing.value = false
        }
        if (scopesRequest === run) {
          scopesRequest = null
        }
      })

    if (!cursor) {
      scopesRequest = run
    }
    return run
  }

  async function loadMoreWorkspaceProjects(
    workspaceId: string,
    options?: { _retried410?: boolean; restartFromFirstPage?: boolean }
  ): Promise<IdentityProjectPage | null> {
    const wsIndex = scopes.value.findIndex((item) => item.workspace_id === workspaceId)
    if (wsIndex === -1) return null
    const target = scopes.value[wsIndex]
    const cursor = options?.restartFromFirstPage ? undefined : target.projects_next_cursor
    if (!options?.restartFromFirstPage && !cursor) return null

    let seenCursors = seenProjectCursorsByWs.get(workspaceId)
    if (!seenCursors) {
      seenCursors = new Set<string>()
      seenProjectCursorsByWs.set(workspaceId, seenCursors)
    }
    if (cursor) {
      if (seenCursors.has(cursor)) {
        const nextScopes = [...scopes.value]
        nextScopes[wsIndex] = { ...target, projects_next_cursor: null }
        scopes.value = nextScopes
        return null
      }
      seenCursors.add(cursor)
    } else {
      seenCursors.clear()
    }

    const generation = scopesLifecycleGeneration
    try {
      const data = await identityApi.listProjects(workspaceId, {
        limit: 50,
        ...(cursor ? { cursor } : {})
      })
      if (generation !== scopesLifecycleGeneration) return null
      const currentIndex = scopes.value.findIndex((item) => item.workspace_id === workspaceId)
      if (currentIndex !== -1) {
        const current = scopes.value[currentIndex]
        const existingBase = options?.restartFromFirstPage ? [] : current.projects
        const existingIds = new Set(existingBase.map((p) => p.id))
        const mergedProjects: IdentityProjectSummary[] = [...existingBase]
        for (const p of data?.items ?? []) {
          if (!p?.id || existingIds.has(p.id)) continue
          existingIds.add(p.id)
          mergedProjects.push({
            id: p.id,
            name: p.name,
            is_default: p.is_default,
            ...(p.state !== undefined ? { state: p.state } : {})
          })
        }
        const rawNext = data?.next_cursor ?? null
        const nextCursor = rawNext && (rawNext === cursor || seenCursors.has(rawNext)) ? null : rawNext
        const nextScopes = [...scopes.value]
        nextScopes[currentIndex] = {
          ...current,
          projects: mergedProjects,
          projects_next_cursor: nextCursor
        }
        scopes.value = nextScopes
      }
      evaluateLoadedScope()
      return data
    } catch (err) {
      if (generation !== scopesLifecycleGeneration || isSilentRequestError(err)) {
        return null
      }
      const status = extractHttpStatus(err)
      if (status === 410 && cursor && !options?._retried410) {
        seenCursors.clear()
        return loadMoreWorkspaceProjects(workspaceId, {
          _retried410: true,
          restartFromFirstPage: true
        })
      }
      const normalized = normalizeApiError(err)
      scopesError.value = {
        status: status || (normalized.status ?? 0),
        reason: normalized.reason,
        message: normalized.message || i18n.global.t('common.unknownError'),
        requestId: extractRequestId(err)
      }
      return null
    }
  }

  function resetScopes(): void {
    scopesLifecycleGeneration += 1
    scopesRequest = null
    validatingKey = null
    seenOuterCursors.clear()
    seenProjectCursorsByWs.clear()
    scopes.value = []
    scopesLoaded.value = false
    scopesLoading.value = false
    scopesRefreshing.value = false
    scopesStale.value = false
    scopesError.value = null
    scopesNextCursor.value = null
    scopesAsOf.value = null
    untrustedPending.value = false
    scopeValidationStatus.value = 'ready'
    scopeUnavailableReason.value = null
    const prev = activeScope.value
    activeScope.value = { mode: 'personal', workspaceId: null, projectId: null }
    clearPersistedScopeSelection()
    resetScopeAccessCache()
    if (prev.mode !== 'personal' || prev.workspaceId !== null || prev.projectId !== null) {
      notifyScopeChanged(activeScope.value)
    }
  }

  function syncFromStorage(): void {
    const details = readPersistedScopeDetails()
    const persisted = details.selection
    const current = activeScope.value
    if (
      current.mode === persisted.mode &&
      current.workspaceId === persisted.workspaceId &&
      current.projectId === persisted.projectId
    ) {
      return
    }
    if (persisted.mode === 'workspace' && persisted.workspaceId) {
      selectScope(persisted, { untrusted: details.untrustedPending })
    } else {
      selectPersonalScope()
    }
  }

  async function ensureActiveProjectAccess(force = false): Promise<AccessProbeEntry | null> {
    if (activeScope.value.mode !== 'workspace' || !activeScope.value.workspaceId || !activeScope.value.projectId) {
      return null
    }
    if (scopeValidationStatus.value === 'unavailable') {
      return null
    }
    return ensureProjectAccess(activeScope.value.workspaceId, activeScope.value.projectId, { force })
  }

  async function ensureActiveWorkspaceAccess(force = false): Promise<AccessProbeEntry | null> {
    if (activeScope.value.mode !== 'workspace' || !activeScope.value.workspaceId) {
      return null
    }
    if (scopeValidationStatus.value === 'unavailable') {
      return null
    }
    return ensureWorkspaceAccess(activeScope.value.workspaceId, { force })
  }

  function getActiveProjectActionState(action: string): PermissionProbeState {
    if (activeScope.value.mode !== 'workspace' || !activeScope.value.workspaceId || !activeScope.value.projectId) {
      return 'denied'
    }
    if (scopeValidationStatus.value === 'unavailable') {
      return 'unavailable'
    }
    const entry = getCachedProjectAccess(activeScope.value.workspaceId, activeScope.value.projectId)
    return getAccessActionState(entry, action)
  }

  function getActiveWorkspaceActionState(action: string): PermissionProbeState {
    if (activeScope.value.mode !== 'workspace' || !activeScope.value.workspaceId) {
      return 'denied'
    }
    if (scopeValidationStatus.value === 'unavailable') {
      return 'unavailable'
    }
    const entry = getCachedWorkspaceAccess(activeScope.value.workspaceId)
    return getAccessActionState(entry, action)
  }

  return {
    SCOPE_CHANGED_EVENT,
    activeScope,
    scopes,
    scopesLoaded,
    activeWorkspace,
    activeWorkspaceId,
    availableProjects,
    activeProject,
    activeProjectId,
    availableProjectOptions,
    isPersonalMode,
    isWorkspaceOverview,
    isProjectMode,
    isActiveDefaultShared,
    scopesLoading,
    scopesRefreshing,
    scopesStale,
    scopesError,
    scopesNextCursor,
    scopesAsOf,
    scopeRevision,
    untrustedPending,
    scopeValidationStatus,
    scopeUnavailableReason,
    fetchScopes,
    loadMoreWorkspaceProjects,
    selectScope,
    selectPersonalScope,
    selectWorkspaceScope,
    syncFromLocation,
    resetScopes,
    syncFromStorage,
    ensureActiveProjectAccess,
    ensureActiveWorkspaceAccess,
    getActiveProjectActionState,
    getActiveWorkspaceActionState
  }
})

let storageListenerAttached = false

function attachGlobalScopeStorageListener(): void {
  if (storageListenerAttached || typeof window === 'undefined') return
  storageListenerAttached = true
  window.addEventListener('storage', (event: StorageEvent) => {
    if (event.storageArea && event.storageArea !== localStorage) return
    if (
      event.key !== null &&
      event.key !== StorageKeys.SCOPE_SELECTION &&
      event.key !== StorageKeys.WORKSPACE_ID &&
      event.key !== StorageKeys.PROJECT_ID
    ) {
      return
    }
    if (getActivePinia()) {
      useConsoleScopeStore().syncFromStorage()
    }
  })
}

export function getConsoleScopeStore(): ReturnType<typeof useConsoleScopeStore> {
  attachGlobalScopeStorageListener()
  const pinia = getActivePinia() ?? createPinia()
  if (!getActivePinia()) {
    setActivePinia(pinia)
  }
  return useConsoleScopeStore(pinia)
}
