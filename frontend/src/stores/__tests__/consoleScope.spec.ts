import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import {
  StorageKeys,
  authStorage,
  clearPersistedScopeSelection,
  scopeSelectionStorage
} from '@/api/tokenRefresh'
import { authAPI } from '@/api'
import { identityApi, type IdentityAccess, type IdentityWorkspacePage } from '@/api/identity'
import { useAuthStore } from '@/stores/auth'
import {
  isDefaultSharedScope,
  persistScopeSelectionToStorage,
  readPersistedScopeDetails,
  useConsoleScopeStore
} from '@/stores/consoleScope'
import {
  ensureProjectAccess,
  ensureWorkspaceAccess,
  getAccessActionState,
  getIssuableKeyActions,
  recheckProjectActionBeforeMutation,
  recheckWorkspaceActionBeforeMutation,
  resetScopeAccessCache
} from '@/features/console/scopeAccess'

function setActor(actorId: string) {
  authStorage.user.set(JSON.stringify({ id: Number(actorId), email: `user${actorId}@example.com`, role: 'user' }))
  authStorage.sessionRevision.set(`rev-${actorId}`)
}

function futureISO(offsetMs = 60_000): string {
  return new Date(Date.now() + offsetMs).toISOString()
}

function buildAccessPayload(overrides: Partial<IdentityAccess> = {}): IdentityAccess {
  return {
    workspace_id: '12',
    project_id: '24',
    actor_id: '101',
    scope_version: 'v1',
    expires_at: futureISO(),
    actions: ['keys.read', 'keys.create', 'playground.run', 'text.chat'],
    issuable_key_actions: ['text.chat', 'image.generate'],
    ...overrides
  }
}

describe('useConsoleScopeStore & scopeAccess', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    setActivePinia(createPinia())
    resetScopeAccessCache()
    vi.restoreAllMocks()
    setActor('101')
  })

  it('keeps personal mode preserved across fetchScopes and never auto-rewrites to Default workspace', async () => {
    vi.spyOn(identityApi, 'listScopes').mockResolvedValueOnce({
      items: [
        {
          workspace_id: '1',
          name: 'Default',
          is_default: true,
          projects: [{ id: '1', name: 'Default', is_default: true }],
          projects_next_cursor: null
        },
        {
          workspace_id: '12',
          name: 'Acme AI',
          is_default: false,
          projects: [{ id: '24', name: 'Core', is_default: false }],
          projects_next_cursor: null
        }
      ],
      next_cursor: null,
      as_of: '2026-11-23T00:00:00Z'
    } as IdentityWorkspacePage)

    const store = useConsoleScopeStore()
    expect(store.activeScope).toEqual({ mode: 'personal', workspaceId: null, projectId: null })
    expect(store.isPersonalMode).toBe(true)

    await store.fetchScopes({ force: true })

    expect(store.activeScope).toEqual({ mode: 'personal', workspaceId: null, projectId: null })
    expect(store.isPersonalMode).toBe(true)
    expect(store.scopeValidationStatus).toBe('ready')
  })

  it('preserves workspace overview mode with projectId: null', async () => {
    vi.spyOn(identityApi, 'listScopes').mockResolvedValueOnce({
      items: [
        {
          workspace_id: '12',
          name: 'Acme AI',
          projects: [{ id: '24', name: 'Core' }],
          projects_next_cursor: null
        }
      ],
      next_cursor: null
    } as IdentityWorkspacePage)

    const store = useConsoleScopeStore()
    await store.fetchScopes({ force: true })
    store.selectWorkspaceScope('12', null)

    expect(store.activeScope).toEqual({ mode: 'workspace', workspaceId: '12', projectId: null })
    expect(store.isWorkspaceOverview).toBe(true)
    expect(store.isProjectMode).toBe(false)
    expect(store.scopeValidationStatus).toBe('ready')
  })

  it('binds persisted scope to actorId and clears foreign-user or registration persistence', async () => {
    persistScopeSelectionToStorage({ mode: 'workspace', workspaceId: '12', projectId: '24' }, '101')

    const sameActor = readPersistedScopeDetails('101')
    expect(sameActor.selection).toEqual({ mode: 'workspace', workspaceId: '12', projectId: '24' })
    expect(sameActor.untrustedPending).toBe(false)

    const foreignActor = readPersistedScopeDetails('999')
    expect(foreignActor.selection).toEqual({ mode: 'personal', workspaceId: null, projectId: null })
    expect(localStorage.getItem(StorageKeys.SCOPE_SELECTION)).toBeNull()

    // Re-seed for authStore switch / register cleanup
    persistScopeSelectionToStorage({ mode: 'workspace', workspaceId: '12', projectId: '24' }, '101')
    setActivePinia(createPinia())
    vi.spyOn(authAPI, 'login').mockResolvedValueOnce({
      access_token: 'tok-999',
      token_type: 'Bearer',
      user: {
        id: 999,
        email: 'other@example.com',
        username: 'other',
        role: 'user',
        balance: 0,
        concurrency: 1,
        status: 'active',
        allowed_groups: [],
        created_at: '',
        updated_at: ''
      }
    })
    const authStore = useAuthStore()
    await authStore.login({ email: 'other@example.com', password: 'secret' })
    expect(localStorage.getItem(StorageKeys.SCOPE_SELECTION)).toBeNull()
    expect(useConsoleScopeStore().activeScope).toEqual({ mode: 'personal', workspaceId: null, projectId: null })
  })

  it('treats legacy unbound persisted scope as untrusted until validated across pages', async () => {
    localStorage.setItem(StorageKeys.WORKSPACE_ID, '22')
    localStorage.setItem(StorageKeys.PROJECT_ID, '202')
    scopeSelectionStorage.remove()

    setActivePinia(createPinia())
    const listScopes = vi
      .spyOn(identityApi, 'listScopes')
      .mockResolvedValueOnce({
        items: [{ workspace_id: '1', name: 'Default', projects: [], projects_next_cursor: null }],
        next_cursor: 'ws-p2'
      } as IdentityWorkspacePage)
      .mockResolvedValueOnce({
        items: [
          {
            workspace_id: '22',
            name: 'Team 22',
            projects: [{ id: '201', name: 'First' }],
            projects_next_cursor: 'proj-p2'
          }
        ],
        next_cursor: null
      } as IdentityWorkspacePage)
    const listProjects = vi.spyOn(identityApi, 'listProjects').mockResolvedValueOnce({
      items: [{ id: '202', workspace_id: '22', name: 'Deep Target' }],
      next_cursor: null
    })

    const store = useConsoleScopeStore()
    expect(store.untrustedPending).toBe(true)
    expect(store.scopeValidationStatus).toBe('validating')

    await store.fetchScopes({ force: true })
    // Validation walks cursor pages without pre-marking unavailable before pages finish
    await vi.waitFor(() => {
      expect(store.scopeValidationStatus).toBe('ready')
    })

    expect(listScopes).toHaveBeenCalledTimes(2)
    expect(listProjects).toHaveBeenCalledWith('22', { cursor: 'proj-p2', limit: 50 })
    expect(store.untrustedPending).toBe(false)
    expect(store.activeProject?.id).toBe('202')
  })

  it('marks invalid workspace+project pairing or archived scope as unavailable without auto-fallback', async () => {
    vi.spyOn(identityApi, 'listScopes').mockResolvedValueOnce({
      items: [
        {
          workspace_id: '12',
          name: 'Acme',
          workspace_state: 'active',
          projects: [{ id: '24', name: 'Active', state: 'active' }],
          projects_next_cursor: null
        },
        {
          workspace_id: '13',
          name: 'Beta',
          workspace_state: 'active',
          projects: [{ id: '99', name: 'Archived Proj', state: 'archived' }],
          projects_next_cursor: null
        }
      ],
      next_cursor: null
    } as IdentityWorkspacePage)

    const store = useConsoleScopeStore()
    await store.fetchScopes({ force: true })

    store.selectWorkspaceScope('12', '99')
    expect(store.scopeValidationStatus).toBe('unavailable')
    expect(store.scopeUnavailableReason).toBe('not_found')
    expect(store.activeScope).toEqual({ mode: 'workspace', workspaceId: '12', projectId: '99' })

    store.selectWorkspaceScope('13', '99')
    expect(store.scopeValidationStatus).toBe('unavailable')
    expect(store.scopeUnavailableReason).toBe('archived')
  })

  it('handles HTTP 410 cursor expiry with a single clean reset retry and stops on repeated cursors', async () => {
    const err410 = Object.assign(new Error('Cursor expired'), { status: 410 })
    const listScopes = vi
      .spyOn(identityApi, 'listScopes')
      .mockResolvedValueOnce({
        items: [{ workspace_id: '1', name: 'Page1', projects: [], projects_next_cursor: null }],
        next_cursor: 'expired-cursor'
      } as IdentityWorkspacePage)
      .mockRejectedValueOnce(err410)
      .mockResolvedValueOnce({
        items: [
          { workspace_id: '1', name: 'Page1', projects: [], projects_next_cursor: null },
          { workspace_id: '2', name: 'Page2', projects: [], projects_next_cursor: null }
        ],
        next_cursor: 'loop-cursor'
      } as IdentityWorkspacePage)
      .mockResolvedValueOnce({
        items: [{ workspace_id: '2', name: 'Page2', projects: [], projects_next_cursor: null }],
        next_cursor: 'loop-cursor'
      } as IdentityWorkspacePage)

    const store = useConsoleScopeStore()
    await store.fetchScopes({ force: true })
    expect(store.scopesNextCursor).toBe('expired-cursor')

    await store.fetchScopes({ cursor: 'expired-cursor' })
    expect(listScopes).toHaveBeenCalledTimes(3)
    expect(store.scopes.map((w) => w.workspace_id)).toEqual(['1', '2'])

    // Repeated cursor is dropped cleanly instead of looping infinitely
    await store.fetchScopes({ cursor: 'loop-cursor' })
    expect(store.scopesNextCursor).toBeNull()
  })

  it('validates location sync and rejects malformed query IDs fail-closed', () => {
    const store = useConsoleScopeStore()
    store.selectWorkspaceScope('99', '990')

    store.syncFromLocation('/workspaces/12/keys', {})
    expect(store.activeScope).toEqual({ mode: 'workspace', workspaceId: '12', projectId: null })

    store.syncFromLocation('/playground', { workspace_id: '12', project_id: 'abc' })
    expect(store.scopeValidationStatus).toBe('unavailable')
    expect(store.scopeUnavailableReason).toBe('malformed')
  })

  it('identifies Shared Default ONLY when both workspace and project carry is_default === true', () => {
    expect(isDefaultSharedScope({ is_default: true }, { is_default: true })).toBe(true)
    expect(isDefaultSharedScope({ is_default: false }, { is_default: true })).toBe(false)
    expect(isDefaultSharedScope({ is_default: true }, { is_default: false })).toBe(false)
    expect(isDefaultSharedScope(null, null)).toBe(false)
  })

  it('fails closed on 401/403/404/503/timeout/expired/malformed access probes and enforces issuable_key_actions', async () => {
    // 401
    vi.spyOn(identityApi, 'getProjectAccess').mockRejectedValueOnce(
      Object.assign(new Error('Unauthorized'), { status: 401, metadata: { request_id: 'req-401' } })
    )
    let entry = await ensureProjectAccess('12', '24', { force: true })
    expect(entry.status).toBe('unavailable')
    expect(entry.error?.kind).toBe('unauthenticated')
    expect(getAccessActionState(entry, 'keys.read')).toBe('unavailable')

    // 403
    vi.spyOn(identityApi, 'getProjectAccess').mockRejectedValueOnce(
      Object.assign(new Error('Forbidden'), { status: 403, metadata: { request_id: 'req-403' } })
    )
    entry = await ensureProjectAccess('12', '24', { force: true })
    expect(entry.status).toBe('denied')
    expect(getAccessActionState(entry, 'playground.run')).toBe('denied')

    // 404
    vi.spyOn(identityApi, 'getProjectAccess').mockRejectedValueOnce(
      Object.assign(new Error('Not Found'), { status: 404, metadata: { request_id: 'req-404' } })
    )
    entry = await ensureProjectAccess('12', '24', { force: true })
    expect(entry.status).toBe('unavailable')
    expect(entry.error?.kind).toBe('not_found')

    // 503 with requestId
    vi.spyOn(identityApi, 'getProjectAccess').mockRejectedValueOnce(
      Object.assign(new Error('Service Unavailable'), { status: 503, metadata: { request_id: 'req-503' } })
    )
    entry = await ensureProjectAccess('12', '24', { force: true })
    expect(entry.status).toBe('unavailable')
    expect(entry.error?.requestId).toBe('req-503')

    // Actor mismatch -> malformed fail-closed
    vi.spyOn(identityApi, 'getProjectAccess').mockResolvedValueOnce(
      buildAccessPayload({ actor_id: '999' })
    )
    entry = await ensureProjectAccess('12', '24', { force: true })
    expect(entry.status).toBe('unavailable')
    expect(entry.error?.reason).toBe('ACCESS_SCOPE_MISMATCH')

    // Empty issuable_key_actions locks create even with keys.create
    vi.spyOn(identityApi, 'getProjectAccess').mockResolvedValueOnce(
      buildAccessPayload({ actions: ['keys.create', 'keys.read'], issuable_key_actions: [] })
    )
    entry = await ensureProjectAccess('12', '24', { force: true })
    expect(getAccessActionState(entry, 'keys.create')).toBe('allowed')
    expect(getIssuableKeyActions(entry)).toEqual({ status: 'denied', actions: [] })

    // Expired entry forces fresh recheck on mutation
    const now = 1_700_000_100_000
    vi.spyOn(identityApi, 'getProjectAccess')
      .mockResolvedValueOnce(
        buildAccessPayload({ expires_at: new Date(now + 1_000).toISOString(), actions: ['playground.run'] })
      )
      .mockResolvedValueOnce(
        buildAccessPayload({ expires_at: new Date(now + 120_000).toISOString(), actions: ['playground.run'] })
      )
    await ensureProjectAccess('12', '24', { force: true, nowMs: () => now })
    const recheck = await recheckProjectActionBeforeMutation('12', '24', 'playground.run', {
      nowMs: () => now + 5_000
    })
    expect(recheck.state).toBe('allowed')

    // Workspace access reads effective_admin / role_source and rechecks mutations
    vi.spyOn(identityApi, 'getWorkspaceAccess').mockResolvedValueOnce({
      workspace_id: '12',
      actor_id: '101',
      scope_version: 'w1',
      expires_at: futureISO(),
      actions: ['credentials.byok.manage'],
      effective_admin: true,
      role_source: 'inherited',
      grantable_actions: ['keys.read']
    })
    const wsEntry = await ensureWorkspaceAccess('12', { force: true })
    expect(wsEntry.workspaceAdmin).toBe(true)
    expect(wsEntry.membershipRoleSource).toBe('inherited')
    const wsRecheck = await recheckWorkspaceActionBeforeMutation('12', 'credentials.byok.manage')
    expect(wsRecheck.state).toBe('allowed')
    clearPersistedScopeSelection()
  })
})
