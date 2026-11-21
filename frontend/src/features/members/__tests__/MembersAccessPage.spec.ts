import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import MembersAccessPage from '../MembersAccessPage.vue'

let currentLocale: 'en' | 'zh' = 'en'

function resolveTranslation(key: string, params?: Record<string, unknown>): string {
  const dict: unknown = currentLocale === 'zh' ? zh : en
  let value: unknown = dict
  for (const part of key.split('.')) {
    if (value && typeof value === 'object' && part in (value as Record<string, unknown>)) {
      value = (value as Record<string, unknown>)[part]
    } else {
      return key
    }
  }
  if (typeof value !== 'string') return key
  if (!params) return value
  return value.replace(/\{(\w+)\}/g, (_, token: string) =>
    params[token] !== undefined ? String(params[token]) : `{${token}}`
  )
}

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: resolveTranslation,
    locale: { value: currentLocale },
  }),
}))

const mocks = vi.hoisted(() => ({
  apiGet: vi.fn(),
  workspacesList: vi.fn(),
  workspacesMembers: vi.fn(),
  workspacesSetMember: vi.fn(),
  workspacesRemoveMember: vi.fn(),
  listScopes: vi.fn(),
  listProjects: vi.fn(),
  getProjectAccess: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get: mocks.apiGet,
  },
}))

vi.mock('@/api/workspaces', () => ({
  workspacesAPI: {
    list: mocks.workspacesList,
    members: mocks.workspacesMembers,
    setMember: mocks.workspacesSetMember,
    removeMember: mocks.workspacesRemoveMember,
  },
}))

vi.mock('../api', () => ({
  membersV2API: {
    listScopes: mocks.listScopes,
    listProjects: mocks.listProjects,
    getProjectAccess: mocks.getProjectAccess,
    createProject: vi.fn(),
    updateProject: vi.fn(),
    deleteProject: vi.fn(),
    setMemberGrants: vi.fn(),
  },
}))

describe('MembersAccessPage remote user search & i18n', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
    currentLocale = 'en'

    mocks.workspacesList.mockResolvedValue([
      { id: 1, name: 'Core Workspace', role: 'admin' },
    ])
    mocks.listScopes.mockResolvedValue({
      items: [{ workspace_id: '1', name: 'Core Workspace', projects: [] }],
    })
    mocks.listProjects.mockResolvedValue({
      items: [{ id: 'p1', name: 'Gateway', version: '2' }],
      next_cursor: null,
    })
    mocks.getProjectAccess.mockResolvedValue({
      actor_id: '1',
      scope_version: '5',
      actions: ['members.grants.write', 'projects.write'],
    })
    mocks.workspacesMembers.mockResolvedValue([])
    mocks.workspacesSetMember.mockResolvedValue(undefined)

    mocks.apiGet.mockImplementation(async (url: string, config?: { params?: Record<string, unknown> }) => {
      if (url === '/admin/users') {
        if (config?.params?.search === 'user9981@corp.io') {
          return {
            data: {
              items: [{ id: 9981, username: 'alice9981', email: 'user9981@corp.io' }],
              total: 1,
            },
          }
        }
        return {
          data: {
            items: [
              { id: 1, username: 'admin', email: 'admin@corp.io' },
              { id: 2, username: 'bob', email: 'bob@corp.io' },
            ],
            total: 10000,
          },
        }
      }
      if (url === '/workspaces/1/members') {
        return { data: { items: [] } }
      }
      return { data: {} }
    })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('loads initial directory with page_size=20, supports debounced remote search, and selects a user beyond page 1', async () => {
    const wrapper = mount(MembersAccessPage, {
      props: { initialWorkspaceId: '1' },
      global: { stubs: { RouterLink: { template: '<a><slot /></a>' }, Teleport: true } },
    })
    await flushPromises()

    expect(mocks.apiGet).toHaveBeenCalledWith('/admin/users', {
      params: { page: 1, page_size: 20 },
    })

    const searchInput = wrapper.get('[data-testid="platform-user-search-input"] input')
    await searchInput.trigger('focus')
    await flushPromises()

    expect(wrapper.get('[data-testid="platform-user-truncated-hint"]').text()).toContain('10000')

    await searchInput.setValue('user9981@corp.io')
    vi.advanceTimersByTime(260)
    await flushPromises()

    expect(mocks.apiGet).toHaveBeenCalledWith('/admin/users', {
      params: { page: 1, page_size: 20, search: 'user9981@corp.io' },
    })

    const optionBtn = wrapper.get('[data-testid="platform-user-option-9981"]')
    expect(optionBtn.text()).toContain('alice9981')
    await optionBtn.trigger('mousedown')
    await flushPromises()

    const idInput = wrapper.get('[data-testid="new-member-user-id-input"] input')
    expect((idInput.element as HTMLInputElement).value).toBe('9981')

    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(mocks.workspacesSetMember).toHaveBeenCalledWith(1, 9981, 'member')
  })

  it('clears selected user ID and disables submit when search text changes after selecting user A (E2-1)', async () => {
    const wrapper = mount(MembersAccessPage, {
      props: { initialWorkspaceId: '1' },
      global: { stubs: { RouterLink: { template: '<a><slot /></a>' }, Teleport: true } },
    })
    await flushPromises()

    const searchInput = wrapper.get('[data-testid="platform-user-search-input"] input')
    await searchInput.trigger('focus')
    await searchInput.setValue('user9981@corp.io')
    vi.advanceTimersByTime(260)
    await flushPromises()

    await wrapper.get('[data-testid="platform-user-option-9981"]').trigger('mousedown')
    await flushPromises()

    expect(wrapper.get('[data-testid="selected-platform-user-summary"]').text()).toContain('user9981@corp.io')
    expect(wrapper.get('[data-testid="selected-platform-user-summary"]').text()).toContain('9981')
    const submitBtn = wrapper.get('[data-testid="add-member-submit-btn"]')
    expect(submitBtn.attributes('disabled')).toBeUndefined()

    await searchInput.setValue('different-user')
    await flushPromises()

    const idInput = wrapper.get('[data-testid="new-member-user-id-input"] input')
    expect((idInput.element as HTMLInputElement).value).toBe('')
    expect(wrapper.find('[data-testid="selected-platform-user-summary"]').exists()).toBe(false)
    expect(submitBtn.attributes('disabled')).toBeDefined()

    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(mocks.workspacesSetMember).not.toHaveBeenCalled()
  })

  it('distinguishes user search failure from zero matches (E2-1)', async () => {
    mocks.apiGet.mockImplementation(async (url: string, config?: { params?: Record<string, unknown> }) => {
      if (url === '/admin/users') {
        if (config?.params?.search === 'boom') {
          throw { response: { status: 403, data: { message: 'Forbidden' } } }
        }
        if (config?.params?.search === 'nobody') {
          return { data: { items: [], total: 0 } }
        }
        return { data: { items: [{ id: 1, username: 'admin', email: 'admin@corp.io' }], total: 1 } }
      }
      if (url === '/workspaces/1/members') {
        return { data: { items: [] } }
      }
      return { data: {} }
    })

    const wrapper = mount(MembersAccessPage, {
      props: { initialWorkspaceId: '1' },
      global: { stubs: { RouterLink: { template: '<a><slot /></a>' }, Teleport: true } },
    })
    await flushPromises()

    const searchInput = wrapper.get('[data-testid="platform-user-search-input"] input')
    await searchInput.trigger('focus')
    await searchInput.setValue('boom')
    vi.advanceTimersByTime(260)
    await flushPromises()

    expect(wrapper.find('[data-testid="platform-user-search-error"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="platform-user-search-empty"]').exists()).toBe(false)

    await searchInput.setValue('nobody')
    vi.advanceTimersByTime(260)
    await flushPromises()

    expect(wrapper.find('[data-testid="platform-user-search-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="platform-user-search-empty"]').exists()).toBe(true)
  })

  it('renders grantable actions strictly from server authorization scope including tasks.* and excluding workspace/platform actions (E2-1)', async () => {
    mocks.getProjectAccess.mockResolvedValue({
      actor_id: '1',
      scope_version: '5',
      actions: [
        'members.grants.write',
        'ops.export.configure',
        'catalog.publish',
        'projects.read',
        'projects.edit',
        'tasks.read',
        'tasks.create',
        'tasks.cancel',
        'tasks.retry',
        'tasks.artifacts.read',
      ],
    })
    mocks.apiGet.mockImplementation(async (url: string) => {
      if (url === '/admin/users') {
        return { data: { items: [], total: 0 } }
      }
      if (url === '/workspaces/1/members') {
        return {
          data: {
            items: [
              {
                id: '7',
                user_id: '7',
                username: 'alice',
                email: 'alice@corp.io',
                role: 'member',
                status: 'active',
                version: '3',
              },
            ],
          },
        }
      }
      if (url === '/workspaces/1/members/7/grants') {
        return {
          data: {
            member_id: '7',
            version: '3',
            project_ids: ['p1'],
            actions: ['tasks.read'],
          },
        }
      }
      return { data: {} }
    })

    const wrapper = mount(MembersAccessPage, {
      props: { initialWorkspaceId: '1' },
      global: { stubs: { RouterLink: { template: '<a><slot /></a>' }, Teleport: true } },
    })
    await flushPromises()

    const editGrantsBtn = wrapper
      .findAll('button')
      .find((btn) => btn.text().includes('Edit Grants'))
    expect(editGrantsBtn).toBeDefined()
    await editGrantsBtn!.trigger('click')
    await flushPromises()

    const modalText = wrapper.get('[data-testid="grant-actions-grid"]').text()
    expect(modalText).toContain('tasks.read')
    expect(modalText).toContain('tasks.create')
    expect(modalText).toContain('tasks.cancel')
    expect(modalText).toContain('tasks.retry')
    expect(modalText).toContain('tasks.artifacts.read')
    expect(modalText).toContain('projects.read')
    expect(modalText).toContain('projects.edit')

    expect(modalText).not.toContain('keys.create')
    expect(modalText).not.toContain('billing.read')
    expect(modalText).not.toContain('members.grants.write')
    expect(modalText).not.toContain('ops.export.configure')
    expect(modalText).not.toContain('catalog.publish')
  })
})
