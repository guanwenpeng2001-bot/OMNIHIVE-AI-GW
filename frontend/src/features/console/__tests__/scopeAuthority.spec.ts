import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAppStore } from '@/stores/app'
import { useConsoleScope, persistConsoleScope, readPersistedConsoleScope, SCOPE_STORAGE_KEY, WORKSPACE_ID_STORAGE_KEY, PROJECT_ID_STORAGE_KEY } from '../scope'
import { apiClient } from '@/api/client'

const ws = (id: string, project = '9') => ({ workspace_id: id, name: `Workspace ${id}`, projects: [{ id: project, name: 'Project' }], projects_next_cursor: null })

describe('one scope authority', () => {
  beforeEach(() => { localStorage.clear(); setActivePinia(createPinia()); vi.restoreAllMocks() })
  it('keeps explicit Personal even when stale legacy keys exist', () => {
    localStorage.setItem(SCOPE_STORAGE_KEY, JSON.stringify({ mode: 'personal', workspaceId: null, projectId: null }))
    localStorage.setItem(WORKSPACE_ID_STORAGE_KEY, '1')
    localStorage.setItem(PROJECT_ID_STORAGE_KEY, '1')
    expect(readPersistedConsoleScope()).toEqual({ mode: 'personal', workspaceId: null, projectId: null })
    expect(useAppStore().activeScope.mode).toBe('personal')
  })
  it('does not activate Default while loading a personal dashboard', async () => {
    vi.spyOn(apiClient, 'get').mockResolvedValue({ data: { items: [ws('1')], next_cursor: null, as_of: '2026-01-01T00:00:00Z' } })
    const scope = useConsoleScope()
    await scope.refreshScopes()
    expect(scope.selection.value.mode).toBe('personal')
    expect(scope.activeWorkspaceId.value).toBe('')
    expect(scope.activeProjectId.value).toBe('')
    expect(useAppStore().activeScope.mode).toBe('personal')
  })
  it('loads later pages without replacing a selected project', async () => {
    vi.spyOn(apiClient, 'get').mockResolvedValueOnce({ data: { items: [ws('1')], next_cursor: 'later', as_of: '2026-01-01T00:00:00Z' } }).mockResolvedValueOnce({ data: { items: [ws('51', '99')], next_cursor: null, as_of: '2026-01-01T00:00:00Z' } })
    persistConsoleScope({ mode: 'workspace', workspaceId: '51', projectId: '99' })
    const scope = useConsoleScope()
    await scope.refreshScopes()
    expect(scope.activeWorkspaceId.value).toBe('51')
    expect(scope.activeProjectId.value).toBe('99')
  })
  it('keeps aggregate and invalid selections distinct from the first project', async () => {
    const app = useAppStore()
    app.scopes = [ws('1')]
    app.scopesLoaded = true
    persistConsoleScope({ mode: 'workspace', workspaceId: '1', projectId: null })
    const scope = useConsoleScope()
    expect(scope.activeProjectId.value).toBe('')
    persistConsoleScope({ mode: 'workspace', workspaceId: '7', projectId: '9' })
    expect(scope.activeWorkspaceId.value).toBe('')
    expect(scope.activeProjectId.value).toBe('')
    expect(app.activeScope.workspaceId).toBe('7')
  })
})
