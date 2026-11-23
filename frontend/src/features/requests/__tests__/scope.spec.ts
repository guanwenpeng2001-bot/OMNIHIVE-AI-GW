import { createPinia, setActivePinia } from 'pinia'
import { effectScope, nextTick } from 'vue'
import { useAppStore } from '@/stores/app'
import { describe, expect, it, vi } from 'vitest'
import { useProjectScope } from '../useProjectScope'

vi.mock('vue-router', () => ({
  useRoute: () => ({ query: { workspace_id: '2', project_id: '9' } }),
  useRouter: () => ({ replace: vi.fn() })
}))

describe('project scope bootstrap', () => {
  it('preserves Personal Space and workspace overview when projects become available', async () => {
    setActivePinia(createPinia())
    const app = useAppStore()
    app.selectPersonalScope()
    const effects = effectScope()
    const project = effects.run(useProjectScope)!
    try {
      app.scopes = [{ workspace_id: '42', name: 'Default', projects: [{ id: '55', name: 'Default' }] }]
      app.scopesLoaded = true
      await nextTick()
      expect(app.activeScope.mode).toBe('personal')
      expect(project.scope.value).toBeNull()
      app.selectWorkspaceScope('42', null)
      await nextTick()
      expect(app.activeScope.projectId).toBeNull()
      expect(project.scopeKey.value).toBe('')
    } finally {
      effects.stop()
    }
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
