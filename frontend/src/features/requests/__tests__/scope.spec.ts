import { createPinia, setActivePinia } from 'pinia'
import { describe, expect, it, vi } from 'vitest'
import { firstAvailableScope, useProjectScope } from '../useProjectScope'

vi.mock('vue-router', () => ({
  useRoute: () => ({ query: { workspace_id: '2', project_id: '9' } }),
  useRouter: () => ({ replace: vi.fn() })
}))

describe('project scope bootstrap', () => {
  it('selects the first workspace that has a project', () => {
    expect(firstAvailableScope([
      { workspace_id: '1', projects: [] },
      { workspace_id: '2', projects: [{ id: '9' }] },
      { workspace_id: '3', projects: [{ id: '10' }] }
    ])).toEqual({ workspaceId: '2', projectId: '9' })
  })

  it('does not invent a scope when no project is available', () => {
    expect(firstAvailableScope([{ workspace_id: '1', projects: [] }])).toBeNull()
  })

  it('treats wildcard * grant in IdentityAccess as matching any action', () => {
    setActivePinia(createPinia())
    const { access, can } = useProjectScope()
    access.data.value = {
      actor_id: '1',
      workspace_id: '2',
      project_id: '9',
      actions: ['*'],
      scope_version: '1',
      expires_at: new Date(Date.now() + 60_000).toISOString()
    }
    access.error.value = null
    expect(can('requests.read')).toBe(true)
    expect(can('credentials.byok.manage')).toBe(true)
  })
})
