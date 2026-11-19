import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import WorkspacesView from '../WorkspacesView.vue'

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  members: vi.fn(),
  listProjects: vi.fn(),
  createProject: vi.fn(),
  renameProject: vi.fn(),
  archiveProject: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  auth: { isAdmin: false, user: { id: 9 } }
}))

vi.mock('@/api/workspaces', () => ({ workspacesAPI: mocks }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => mocks.auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks }))
vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (k: string) => k, locale: { value: 'en' } })
  }
})
vi.mock('@/components/layout/AppLayout.vue', () => ({
  default: { template: '<main><slot /></main>' }
}))
vi.mock('@/api/mcp', () => ({
  mcpAPI: {
    getRoutingPolicies: vi.fn().mockResolvedValue({
      zdr_enabled: false,
      allow_fallback: true,
      auto_prompt_cache: true,
      strategy_mode: 'smart',
      region_preference: 'global',
      alert_threshold_percent: 80
    })
  }
}))

vi.mock('@/features/console/scope', async () => {
  const actual = await vi.importActual<typeof import('@/features/console/scope')>(
    '@/features/console/scope'
  )
  return {
    ...actual,
    identityV2Api: {
      ...actual.identityV2Api,
      listProjects: mocks.listProjects,
      createProject: mocks.createProject,
      renameProject: mocks.renameProject,
      archiveProject: mocks.archiveProject
    }
  }
})

describe('WorkspacesView BE-01 projects and scope switching', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.resetAllMocks()
    mocks.list.mockResolvedValue([{ id: 7, name: 'Platform Team', role: 'admin', is_default: false }])
    mocks.members.mockResolvedValue([{ user_id: 9, role: 'admin' }])
    mocks.listProjects.mockResolvedValue({
      items: [{ id: '12', workspace_id: '7', name: 'Core API', state: 'active', version: '3' }],
      next_cursor: null,
      as_of: '2026-11-19T10:00:00Z'
    })
  })

  it('lists BE-01 projects, switches persisted console scope, creates/renames/archives projects, and surfaces 409 conflicts', async () => {
    mocks.createProject.mockResolvedValue({
      id: '19',
      workspace_id: '7',
      name: 'Eval Sandbox',
      state: 'active',
      version: '1'
    })
    mocks.renameProject.mockRejectedValueOnce({
      status: 409,
      reason: 'VERSION_CONFLICT',
      message: 'ETag mismatch'
    })

    const wrapper = mount(WorkspacesView, {
      global: {
        stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } }
      }
    })
    await flushPromises()

    expect(mocks.listProjects).toHaveBeenCalledWith('7', { limit: 100 })
    expect(wrapper.get('[data-testid="project-row-12"]').text()).toContain('Core API')

    // Switch active scope to Workspace #7 / Project #12
    await wrapper.get('[data-testid="switch-project-12"]').trigger('click')
    await flushPromises()
    expect(localStorage.getItem('omnihive.workspace_id.v1')).toBe('7')
    expect(localStorage.getItem('omnihive.project_id.v1')).toBe('12')
    expect(wrapper.find('[data-testid="console-state-confirmed"]').exists()).toBe(true)

    // Create new BE-01 project
    await wrapper.get('[data-testid="new-project-name-input"]').setValue('Eval Sandbox')
    await wrapper.get('[data-testid="create-project-btn"]').trigger('click')
    await flushPromises()
    expect(mocks.createProject).toHaveBeenCalledWith('7', { name: 'Eval Sandbox' })
    expect(wrapper.find('[data-testid="project-row-19"]').exists()).toBe(true)

    // Rename triggers 409 conflict banner
    await wrapper.get('[data-testid="start-rename-project-12"]').trigger('click')
    await wrapper.get('[data-testid="rename-project-input"]').setValue('Renamed API')
    await wrapper.get('[data-testid="confirm-rename-project-btn"]').trigger('click')
    await flushPromises()
    expect(mocks.renameProject).toHaveBeenCalledWith('7', '12', { name: 'Renamed API' }, '3')
    expect(wrapper.find('[data-testid="console-state-conflict"]').exists()).toBe(true)

    wrapper.unmount()
  })
})
