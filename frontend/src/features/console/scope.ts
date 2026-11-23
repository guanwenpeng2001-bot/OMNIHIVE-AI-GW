import { computed, onBeforeUnmount, onMounted } from 'vue'
import {
  identityApi,
  generateIdempotencyKey,
  resolveDefaultRoutingGroupId,
  normalizeIdentityKeyState,
  clearPendingProjectKeyIdempotency,
  type IdentityWorkspace,
  type IdentityProjectSummary,
  type IdentityWorkspacePage,
  type IdentityProject,
  type IdentityProjectPage,
  type IdentityMember,
  type IdentityMemberPage,
  type IdentityGrantInput,
  type IdentityGrantVersion,
  type IdentityMemberGrants,
  type IdentityKey,
  type IdentityKeyDerivedState,
  type IdentityKeyKind,
  type IdentityKeyPage,
  type IdentityKeyGroup,
  type IdentityKeyGroupPage,
  type IdentityKeyInput,
  type IdentityAccess,
  type IdentityWorkspaceAccess
} from '@/api/identity'
import { StorageKeys } from '@/api/tokenRefresh'
import {
  SCOPE_CHANGED_EVENT,
  getConsoleScopeStore,
  readPersistedGlobalScope,
  isDefaultSharedScope,
  type ScopeSelection
} from '@/stores/consoleScope'

export type {
  IdentityWorkspace,
  IdentityProjectSummary,
  IdentityWorkspacePage,
  IdentityProject,
  IdentityProjectPage,
  IdentityMember,
  IdentityMemberPage,
  IdentityGrantInput,
  IdentityGrantVersion,
  IdentityMemberGrants,
  IdentityKey,
  IdentityKeyDerivedState,
  IdentityKeyKind,
  IdentityKeyPage,
  IdentityKeyGroup,
  IdentityKeyGroupPage,
  IdentityKeyInput,
  IdentityAccess,
  IdentityWorkspaceAccess
}

export {
  identityApi,
  generateIdempotencyKey,
  resolveDefaultRoutingGroupId,
  normalizeIdentityKeyState,
  clearPendingProjectKeyIdempotency,
  SCOPE_CHANGED_EVENT,
  isDefaultSharedScope
}

export const SCOPE_STORAGE_KEY = StorageKeys.SCOPE_SELECTION
export const WORKSPACE_ID_STORAGE_KEY = StorageKeys.WORKSPACE_ID
export const PROJECT_ID_STORAGE_KEY = StorageKeys.PROJECT_ID

export type ConsoleScopeSelection = ScopeSelection

export function readPersistedConsoleScope(): ConsoleScopeSelection {
  return readPersistedGlobalScope()
}

export function persistConsoleScope(selection: ConsoleScopeSelection): void {
  const store = getConsoleScopeStore()
  store.selectScope(selection)
}

/**
 * Stateless adapter over `useConsoleScopeStore()` for page components.
 * Never auto-rewrites Personal scope to Workspace/Default.
 */
export function useConsoleScope() {
  const store = getConsoleScopeStore()

  const scopes = computed(() => store.scopes)
  const loading = computed(() => store.scopesLoading)
  const refreshing = computed(() => store.scopesRefreshing)
  const loaded = computed(() => store.scopesLoaded)
  const lastSyncedAt = computed(() => store.scopesAsOf ?? '')
  const scopeError = computed(() => store.scopesError)
  const selection = computed(() => store.activeScope)
  const activeWorkspace = computed(() => store.activeWorkspace)
  const activeWorkspaceId = computed(() => store.activeWorkspaceId)
  const availableProjects = computed(() => store.availableProjects)
  const activeProject = computed(() => store.activeProject)
  const activeProjectId = computed(() => store.activeProjectId)
  const availableProjectOptions = computed(() => store.availableProjectOptions)
  const scopeValidationStatus = computed(() => store.scopeValidationStatus)
  const scopeUnavailableReason = computed(() => store.scopeUnavailableReason)
  const isActiveDefaultShared = computed(() => store.isActiveDefaultShared)

  function handleExternalScopeEvent(): void {
    store.syncFromStorage()
  }

  onMounted(() => {
    store.syncFromStorage()
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
    availableProjectOptions,
    scopeValidationStatus,
    scopeUnavailableReason,
    isActiveDefaultShared,
    refreshScopes: async (force = false) => {
      await store.fetchScopes({ force })
    },
    selectPersonalScope: () => {
      store.selectPersonalScope()
    },
    selectWorkspaceProject: (workspaceId: string, projectId: string | null) => {
      store.selectWorkspaceScope(workspaceId, projectId)
    },
    syncFromLocation: (pathname: string, query?: Record<string, unknown>) => {
      store.syncFromLocation(pathname, query)
    }
  }
}
