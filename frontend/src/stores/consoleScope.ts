import { computed } from 'vue'
import { defineStore } from 'pinia'
import { StorageKeys, scopeSelectionStorage } from '@/api/tokenRefresh'
import { workspaceIDFromPath } from '@/api/workspaceScope'
import { SCOPE_CHANGED_EVENT, useAppStore, type ScopeSelection } from './app'

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

/**
 * Reads persisted scope selection written by either `appStore` or feature-level
 * `persistConsoleScope` so top-bar and page-level scope pickers stay synchronized.
 */
export function readPersistedGlobalScope(): ScopeSelection {
  const versioned = scopeSelectionStorage.getJSON<Partial<ScopeSelection> | null>(null)
  if (
    versioned &&
    versioned.mode === 'workspace' &&
    typeof versioned.workspaceId === 'string' &&
    /^[1-9]\d*$/.test(versioned.workspaceId)
  ) {
    return {
      mode: 'workspace',
      workspaceId: versioned.workspaceId,
      projectId:
        typeof versioned.projectId === 'string' && /^[1-9]\d*$/.test(versioned.projectId)
          ? versioned.projectId
          : null
    }
  }

  try {
    if (typeof localStorage !== 'undefined') {
      const wsId = localStorage.getItem(StorageKeys.WORKSPACE_ID)
      if (wsId && /^[1-9]\d*$/.test(wsId)) {
        const projId = localStorage.getItem(StorageKeys.PROJECT_ID)
        return {
          mode: 'workspace',
          workspaceId: wsId,
          projectId: projId && /^[1-9]\d*$/.test(projId) ? projId : null
        }
      }
    }
  } catch {
    // Ignore storage read failures in restricted environments.
  }

  return { mode: 'personal', workspaceId: null, projectId: null }
}

export const useConsoleScopeStore = defineStore('consoleScope', () => {
  const appStore = useAppStore()

  const activeScope = computed<ScopeSelection>(
    () => appStore.activeScope ?? { mode: 'personal', workspaceId: null, projectId: null }
  )
  const scopes = computed(() => appStore.scopes ?? [])
  const activeWorkspace = computed(() => appStore.activeWorkspace ?? null)
  const activeProject = computed(() => appStore.activeProject ?? null)
  const scopesLoading = computed(() => Boolean(appStore.scopesLoading))
  const scopesRefreshing = computed(() => Boolean(appStore.scopesRefreshing))
  const scopesStale = computed(() => Boolean(appStore.scopesStale))
  const scopesError = computed(() => appStore.scopesError ?? null)

  function syncFromStorage(): void {
    const persisted = readPersistedGlobalScope()
    const current = activeScope.value
    if (
      current.mode === persisted.mode &&
      current.workspaceId === persisted.workspaceId &&
      current.projectId === persisted.projectId
    ) {
      return
    }
    if (persisted.mode === 'workspace' && persisted.workspaceId) {
      appStore.selectWorkspaceScope?.(persisted.workspaceId, persisted.projectId)
    } else {
      appStore.selectPersonalScope?.()
    }
  }

  return {
    SCOPE_CHANGED_EVENT,
    activeScope,
    scopes,
    activeWorkspace,
    activeProject,
    scopesLoading,
    scopesRefreshing,
    scopesStale,
    scopesError,
    fetchScopes: (options?: { force?: boolean; cursor?: string }) => appStore.fetchScopes?.(options),
    selectPersonalScope: () => appStore.selectPersonalScope?.(),
    selectWorkspaceScope: (workspaceId: string, projectId?: string | null) =>
      appStore.selectWorkspaceScope?.(workspaceId, projectId),
    syncFromStorage
  }
})
