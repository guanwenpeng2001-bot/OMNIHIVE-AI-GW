import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import WorkspacesView from '../WorkspacesView.vue'
import { getConsoleScopeStore } from '@/stores/consoleScope'
import { identityApi } from '@/api/identity'

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  members: vi.fn(),
  listProjects: vi.fn(),
  createProject: vi.fn(),
  renameProject: vi.fn(),
  archiveProject: vi.fn(),
  getProjectAccess: vi.fn(),
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
    identityApi: {
      ...actual.identityApi,
      listProjects: mocks.listProjects,
      createProject: mocks.createProject,
      renameProject: mocks.renameProject,
      archiveProject: mocks.archiveProject,
      getProjectAccess: mocks.getProjectAccess
    }
  }
})

describe('WorkspacesView workspace projects and scope switching', () => {
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

  it('lists workspace projects, switches persisted console scope, creates/renames/archives projects, and surfaces 409 conflicts', async () => {
    const store = getConsoleScopeStore()
    const refreshScopes = vi.spyOn(store, 'fetchScopes')
    vi.spyOn(identityApi, 'listScopes').mockResolvedValue({ items: [], next_cursor: null })
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

    // Create a project
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

    mocks.archiveProject.mockResolvedValue({ id: '12', workspace_id: '7', name: 'Core API', version: '4' })
    await wrapper.get('[data-testid="archive-project-12"]').trigger('click')
    await flushPromises()
    expect(mocks.archiveProject).toHaveBeenCalledWith('7', '12', '3')
    expect(localStorage.getItem('omnihive.workspace_id.v1')).toBe('7')
    expect(localStorage.getItem('omnihive.project_id.v1')).toBeNull()
    expect(refreshScopes).toHaveBeenCalledWith({ force: true })
    const archived = wrapper.get('[data-testid="project-row-12"]')
    expect(archived.text()).toContain('Archived')
    expect(archived.findAll('button')).toHaveLength(0)
    expect(wrapper.get('[data-testid="console-state-confirmed"]').text()).toContain('Archived project')

    wrapper.unmount()
  })

  it('surfaces active scope banner, default workspace/role badges, and all 4 project quick actions on default onboarding', async () => {
    mocks.list.mockResolvedValue([
      { id: 1, name: 'Default', role: 'member', is_default: true }
    ])
    mocks.listProjects.mockResolvedValue({
      items: [{ id: '101', workspace_id: '1', name: 'Default Project', state: 'active', version: '1' }],
      next_cursor: null,
      as_of: '2026-11-22T04:00:00Z'
    })

    const wrapper = mount(WorkspacesView, {
      global: {
        stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } }
      }
    })
    await flushPromises()

    expect(wrapper.get('[data-testid="default-workspace-badge"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="workspace-role-badge"]').text()).toContain('workspaces.member')
    const banner = wrapper.get('[data-testid="workspace-active-scope-banner"]')
    expect(banner.text()).toContain('Default (#1)')
    expect(banner.text()).toContain('Default Project (#101)')

    expect(wrapper.get('[data-testid="quick-action-keys"]').attributes('href')).toBe('/workspaces/1/keys')
    expect(wrapper.get('[data-testid="quick-action-playground"]').attributes('href')).toBe('/playground')
    expect(wrapper.get('[data-testid="quick-action-usage"]').attributes('href')).toBe('/usage')
    expect(wrapper.get('[data-testid="quick-action-tasks"]').attributes('href')).toBe('/tasks')

    expect(localStorage.getItem('omnihive.workspace_id.v1')).toBe('1')
    expect(localStorage.getItem('omnihive.project_id.v1')).toBe('101')
    wrapper.unmount()
  })

  it('gates create, rename, and archive buttons for non-admin members using exact projects.create/edit/delete grants', async () => {
    mocks.list.mockResolvedValue([
      { id: 7, name: 'Platform Team', role: 'member', is_default: false }
    ])
    mocks.listProjects.mockResolvedValue({
      items: [{ id: '12', workspace_id: '7', name: 'Core API', state: 'active', version: '3' }],
      next_cursor: null,
      as_of: '2026-11-23T00:00:00Z'
    })
    mocks.getProjectAccess.mockResolvedValue({
      actor_id: '9',
      workspace_id: '7',
      project_id: '12',
      actions: ['projects.read', 'projects.edit', 'requests.read'],
      scope_version: '1',
      expires_at: '2026-11-24T00:00:00Z'
    })

    const wrapper = mount(WorkspacesView, {
      global: {
        stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } }
      }
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="create-project-btn"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="start-rename-project-12"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="archive-project-12"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quick-action-usage"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('locks rename and archive on the protected shared Default project while allowing member filter and grant matrix in Workspaces', async () => {
    mocks.list.mockResolvedValue([
      { id: 1, name: 'Default', role: 'admin', is_default: true }
    ])
    mocks.members.mockResolvedValue([
      { user_id: 9, role: 'admin' },
      { user_id: 42, role: 'member' }
    ])
    mocks.listProjects.mockResolvedValue({
      items: [
        { id: '1', workspace_id: '1', name: 'Default', state: 'active', version: '1', is_default: true },
        { id: '12', workspace_id: '1', name: 'Team Project', state: 'active', version: '2', is_default: false }
      ],
      next_cursor: null,
      as_of: '2026-11-23T00:00:00Z'
    })

    const wrapper = mount(WorkspacesView, {
      global: {
        stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' }, Teleport: true }
      }
    })
    await flushPromises()

    // Shared Default project row shows browse-only badge and locks rename/archive
    expect(wrapper.find('[data-testid="shared-default-project-badge"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="start-rename-project-1"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="archive-project-1"]').exists()).toBe(false)
    // Team project (#12) remains editable
    expect(wrapper.find('[data-testid="start-rename-project-12"]').exists()).toBe(true)

    // Filter returned workspace members
    const filterInput = wrapper.get('[data-testid="workspace-member-filter-input"]')
    await filterInput.setValue('42')
    await flushPromises()
    expect(wrapper.findAll('li')).toHaveLength(1)
    expect(wrapper.find('li').text()).toContain('42')
    expect(wrapper.find('[data-testid="open-member-grants-42"]').exists()).toBe(true)

    wrapper.unmount()
  })
})
