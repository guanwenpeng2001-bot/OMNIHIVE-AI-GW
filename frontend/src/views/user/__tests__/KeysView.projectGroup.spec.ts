import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { reactive } from 'vue'
import { useConsoleScopeStore } from '@/stores/consoleScope'
import { resetScopeAccessCache } from '@/features/console/scopeAccess'
import KeysView from '../KeysView.vue'

const mocks = vi.hoisted(() => ({
  listKeys: vi.fn(),
  createKey: vi.fn(),
  getPublicSettings: vi.fn(),
  getDashboardApiKeysUsage: vi.fn(),
  getAvailableGroups: vi.fn(),
  getUserGroupRates: vi.fn(),
  showError: vi.fn(),
  showInfo: vi.fn(),
  showSuccess: vi.fn(),
  listProjectKeys: vi.fn(),
  listProjectKeyGroups: vi.fn(),
  createProjectKey: vi.fn(),
  listScopes: vi.fn(),
  getProjectAccess: vi.fn(),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () =>
    reactive({
      user: { id: 1, email: 'dev@example.com', role: 'user', balance: 100 },
      isSimpleMode: false,
      refreshUser: vi.fn(),
    }),
}))

vi.mock('@/stores/app', () => ({
  SCOPE_CHANGED_EVENT: 'omnihive:scope-changed',
  useAppStore: () => ({
    showError: mocks.showError,
    showInfo: mocks.showInfo,
    showSuccess: mocks.showSuccess,
    publicSettingsLoaded: true,
    fetchPublicSettings: vi.fn(),
  }),
}))

vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({
    isCurrentStep: vi.fn(() => false),
    nextStep: vi.fn(),
  }),
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copied: { value: false },
    copyToClipboard: vi.fn(),
  }),
}))

vi.mock('@/api/modelAccess', () => ({
  keyVisibleModels: vi.fn().mockResolvedValue([]),
}))

vi.mock('@/api', () => ({
  keysAPI: {
    list: mocks.listKeys,
    create: mocks.createKey,
    update: vi.fn(),
    delete: vi.fn(),
    toggleStatus: vi.fn(),
    rotate: vi.fn(),
    revokeCredentials: vi.fn(),
  },
  authAPI: {
    getPublicSettings: mocks.getPublicSettings,
  },
  usageAPI: {
    getDashboardApiKeysUsage: mocks.getDashboardApiKeysUsage,
  },
  userGroupsAPI: {
    getAvailable: mocks.getAvailableGroups,
    getUserGroupRates: mocks.getUserGroupRates,
  },
}))

vi.mock('@/api/identity', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api/identity')>()
  return {
    ...actual,
    identityApi: {
      ...actual.identityApi,
      listScopes: mocks.listScopes,
      listProjectKeys: mocks.listProjectKeys,
      listProjectKeyGroups: mocks.listProjectKeyGroups,
      createProjectKey: mocks.createProjectKey,
      getProjectAccess: mocks.getProjectAccess,
    },
  }
})

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
      locale: { value: 'en' },
    }),
  }
})

const stubs = {
  AppLayout: { template: '<div><slot /></div>' },
  DataTable: {
    props: ['data'],
    template:
      '<div data-testid="keys-data-table"><span v-for="item in data" :key="item.id" data-testid="table-key-row">{{ item.name }}:{{ item.key }}<slot name="cell-actions" :row="item" /></span></div>',
  },
  Pagination: { template: '<div />' },
  BaseDialog: {
    props: ['show', 'title'],
    template: '<div v-if="show" data-testid="base-dialog"><slot /><slot name="footer" /></div>',
  },
  ConfirmDialog: { template: '<div />' },
  UseKeyModal: { template: '<div />' },
  Select: {
    props: ['modelValue', 'options'],
    template:
      '<select :value="modelValue" @change="$emit(\'update:modelValue\', Number($event.target.value) || null)"><option v-for="o in options" :key="o.value" :value="o.value">{{ o.label }}</option></select>',
  },
  Icon: { template: '<span />' },
  GroupBadge: { template: '<span />' },
  GroupOptionItem: { template: '<span />' },
  PlatformIcon: { template: '<span />' },
  ModelAccessRuleEditor: { template: '<div />' },
  DatePicker: { template: '<input />' },
  Teleport: true,
}

describe('KeysView project-scoped API key group binding guard', () => {
  const mountedWrappers: VueWrapper[] = []

  function mountKeysView() {
    const wrapper = mount(KeysView, { global: { stubs } })
    mountedWrappers.push(wrapper)
    return wrapper
  }

  afterEach(() => {
    while (mountedWrappers.length > 0) {
      mountedWrappers.pop()?.unmount()
    }
  })

  function seedProjectScope(workspaceId: string, projectId: string | null) {
    const store = useConsoleScopeStore()
    store.selectWorkspaceScope(workspaceId, projectId)
    return store
  }

  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    setActivePinia(createPinia())
    resetScopeAccessCache()
    vi.clearAllMocks()
    mocks.listKeys.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
    mocks.getPublicSettings.mockResolvedValue({ api_base_url: 'https://api.example.com', site_name: 'OmniHive' })
    mocks.getDashboardApiKeysUsage.mockResolvedValue({ stats: {} })
    mocks.getUserGroupRates.mockResolvedValue({})
    mocks.listProjectKeys.mockResolvedValue({ items: [] })
    mocks.listProjectKeyGroups.mockResolvedValue({ items: [] })
    mocks.listScopes.mockResolvedValue({
      items: [
        {
          workspace_id: '10',
          name: 'Workspace 10',
          projects: [{ id: '20', name: 'Project 20' }],
          projects_next_cursor: null,
        },
        {
          workspace_id: '4',
          name: 'Workspace 4',
          projects: [{ id: '5', name: 'Default Project 5' }],
          projects_next_cursor: null,
        },
      ],
      next_cursor: null,
    })
    mocks.getProjectAccess.mockResolvedValue({
      actor_id: '1',
      workspace_id: '10',
      project_id: '20',
      actions: [
        'keys.read',
        'keys.create',
        'text.chat',
        'text.embeddings',
        'image.generate',
        'usage.read',
        'tasks.read',
        'models.read',
      ],
      issuable_key_actions: [
        'text.chat',
        'text.embeddings',
        'image.generate',
        'usage.read',
        'tasks.read',
        'models.read',
      ],
      scope_version: '1',
      expires_at: new Date(Date.now() + 3600_000).toISOString(),
    })
  })

  it('omits legacy row mutation controls even for an actor-owned project key', async () => {
    seedProjectScope('10', '20')
    mocks.listProjectKeys.mockResolvedValue({ items: [{
      id: '59', owner_id: '1', project_id: '20', name: 'Project key', state: 'active', version: '1'
    }] })
    const wrapper = mountKeysView()
    await flushPromises()
    const actions = wrapper.get('[data-testid="table-key-row"]').text()
    expect(actions).toContain('keys.useKey')
    for (const label of ['keys.disable', 'keys.rotateKey', 'common.edit', 'common.delete']) {
      expect(actions).not.toContain(label)
    }
  })

  it('switches from a project to Personal before opening the requested personal key form', async () => {
    const store = seedProjectScope('10', '20')
    mocks.getAvailableGroups.mockResolvedValue([])
    const wrapper = mountKeysView()
    await flushPromises()
    await wrapper.setProps({ createPersonal: true })
    await flushPromises()
    expect(store.activeScope.mode).toBe('personal')
    expect(wrapper.get('[data-tour="key-form-name"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="keys-link-requests"]').attributes('href')).toBe('/usage')
    expect(mocks.listKeys).toHaveBeenCalled()
  })

  it('blocks project-scoped key creation and shows explicit validation error when no project group exists', async () => {
    seedProjectScope('10', '20')
    mocks.listProjectKeyGroups.mockResolvedValue({ items: [] })

    const wrapper = mountKeysView()
    await flushPromises()

    await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-tour="key-form-name"]').setValue('unbound-project-key')

    expect(wrapper.get('[data-testid="project-key-group-required-alert"]').text()).toContain(
      'keys.projectGroupRequired'
    )
    const submitBtn = wrapper.get('[data-tour="key-form-submit"]')
    expect(submitBtn.attributes('disabled')).toBeDefined()

    await wrapper.get('#key-form').trigger('submit.prevent')
    await flushPromises()

    expect(mocks.showError).toHaveBeenCalledWith('keys.projectGroupRequired')
    expect(mocks.createProjectKey).not.toHaveBeenCalled()
    expect(mocks.createKey).not.toHaveBeenCalled()
    expect(mocks.getAvailableGroups).not.toHaveBeenCalled()
  })

  it('creates project-scoped key directly with explicit group_id and idempotencyKey when project group exists', async () => {
    seedProjectScope('10', '20')
    mocks.listProjectKeyGroups.mockResolvedValue({
      items: [
        {
          id: '7',
          name: 'Primary OpenAI Pool',
          platform: 'openai',
          status: 'active',
          subscription_type: 'standard',
          rate_multiplier: 1,
        },
      ],
    })
    mocks.createProjectKey.mockResolvedValue({
      id: 'pk_1',
      name: 'bound-project-key',
      credential: 'sk-proj-secret-1',
    })

    const wrapper = mountKeysView()
    await flushPromises()

    await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-tour="key-form-name"]').setValue('bound-project-key')
    expect(wrapper.find('[data-testid="project-key-group-required-alert"]').exists()).toBe(false)

    await wrapper.get('#key-form').trigger('submit.prevent')
    await flushPromises()

    expect(mocks.createProjectKey).toHaveBeenCalledWith(
      '10',
      '20',
      expect.objectContaining({
        group_id: '7',
        name: 'bound-project-key',
        model_access: { mode: 'inherit', ids: [] },
      }),
      expect.any(String)
    )
    expect(mocks.showSuccess).toHaveBeenCalledWith('keys.keyCreatedSuccess')
    expect(mocks.getAvailableGroups).not.toHaveBeenCalled()
  })

  it('loads workspace/project-scoped groups instead of account-level group #1 for new workspace default project keys (E2-5)', async () => {
    seedProjectScope('4', '5')
    mocks.getProjectAccess.mockResolvedValue({
      actor_id: '1',
      workspace_id: '4',
      project_id: '5',
      actions: ['keys.read', 'keys.create', 'text.chat'],
      issuable_key_actions: ['text.chat'],
      scope_version: '1',
      expires_at: new Date(Date.now() + 3600_000).toISOString(),
    })
    mocks.listProjectKeyGroups.mockResolvedValue({
      items: [
        {
          id: '42',
          name: 'ws4-openai',
          platform: 'openai',
          status: 'active',
          subscription_type: 'standard',
          rate_multiplier: 1,
        },
      ],
    })
    mocks.createProjectKey.mockResolvedValue({
      id: 'pk_42',
      name: 'fe-e2e-restricted',
      credential: 'sk-proj-secret-42',
    })

    const wrapper = mountKeysView()
    await flushPromises()

    await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
    await flushPromises()

    expect(mocks.listProjectKeyGroups).toHaveBeenCalledWith(
      '4',
      '5',
      expect.objectContaining({ limit: 100 })
    )
    expect(mocks.getAvailableGroups).not.toHaveBeenCalled()
    expect(wrapper.get<HTMLInputElement>('input[name="key-provider"][value="anthropic"]').element.disabled).toBe(true)
    expect(wrapper.get<HTMLInputElement>('input[name="key-provider"][value="openai"]').element.checked).toBe(true)

    await wrapper.get('[data-tour="key-form-name"]').setValue('fe-e2e-restricted')
    await wrapper.get('#key-form').trigger('submit.prevent')
    await flushPromises()

    expect(mocks.createProjectKey).toHaveBeenCalledWith(
      '4',
      '5',
      expect.objectContaining({
        group_id: '42',
        name: 'fe-e2e-restricted',
        actions: ['text.chat'],
      }),
      expect.any(String)
    )
  })

  it('keeps models.read unchecked by default and allows opting in on project keys (E2-2)', async () => {
    seedProjectScope('10', '20')
    mocks.listProjectKeyGroups.mockResolvedValue({
      items: [
        {
          id: '12',
          name: 'openai-main',
          platform: 'openai',
          status: 'active',
          subscription_type: 'standard',
          rate_multiplier: 1,
        },
      ],
    })
    mocks.createProjectKey
      .mockResolvedValueOnce({
        id: 'pk_12',
        name: 'default-scoped-key',
        credential: 'sk-proj-default',
        actions: ['text.chat', 'text.embeddings', 'image.generate', 'usage.read', 'tasks.read'],
      })
      .mockResolvedValueOnce({
        id: 'pk_13',
        name: 'models-read-key',
        credential: 'sk-proj-models',
        actions: ['text.chat', 'text.embeddings', 'image.generate', 'usage.read', 'tasks.read', 'models.read'],
      })

    const wrapper = mountKeysView()
    await flushPromises()

    await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
    await flushPromises()

    const modelsReadCheckbox = wrapper.get('[data-testid="project-key-models-read-checkbox"]')
    expect((modelsReadCheckbox.element as HTMLInputElement).checked).toBe(false)

    const preview = wrapper.get('[data-testid="project-key-scope-preview"]')
    expect(preview.text()).toContain('text.chat')
    expect(preview.text()).toContain('keys.projectScopeModelsReadExcluded')
    expect(preview.findAll('code').map((c) => c.text())).not.toContain('models.read')

    await wrapper.get('[data-tour="key-form-name"]').setValue('default-scoped-key')
    await wrapper.get('#key-form').trigger('submit.prevent')
    await flushPromises()

    expect(mocks.createProjectKey).toHaveBeenNthCalledWith(
      1,
      '10',
      '20',
      expect.objectContaining({
        actions: ['text.chat', 'text.embeddings', 'image.generate', 'usage.read', 'tasks.read'],
      }),
      expect.any(String)
    )

    await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="project-key-models-read-checkbox"]').setValue(true)
    await flushPromises()

    const previewWithModels = wrapper.get('[data-testid="project-key-scope-preview"]')
    expect(previewWithModels.findAll('code').map((c) => c.text())).toContain('models.read')
    expect(previewWithModels.text()).toContain('keys.projectScopeModelsReadIncluded')

    await wrapper.get('[data-tour="key-form-name"]').setValue('models-read-key')
    await wrapper.get('#key-form').trigger('submit.prevent')
    await flushPromises()

    expect(mocks.createProjectKey).toHaveBeenNthCalledWith(
      2,
      '10',
      '20',
      expect.objectContaining({
        actions: ['text.chat', 'text.embeddings', 'image.generate', 'usage.read', 'tasks.read', 'models.read'],
      }),
      expect.any(String)
    )
  })

  it('separates keys.create and keys.read, renders project keys in main table, and fails closed on empty issuable actions or 503', async () => {
    seedProjectScope('10', '20')
    mocks.listProjectKeys.mockResolvedValue({
      items: [
        {
          id: '91',
          prefix: 'sk-proj-91',
          name: 'Team Key 91',
          owner_id: '1',
          group_id: '7',
          state: 'active',
          actions: ['text.chat'],
          model_access: { mode: 'inherit', ids: [] },
        },
      ],
      next_cursor: null,
    })
    const withCreate = mountKeysView()
    await flushPromises()
    expect(mocks.listKeys).not.toHaveBeenCalled()
    expect(mocks.listProjectKeys).toHaveBeenCalled()
    expect(withCreate.get('[data-testid="table-key-row"]').text()).toContain('Team Key 91:sk-proj-91')
    expect(withCreate.find('[data-testid="keys-create-btn"]').exists()).toBe(true)
    withCreate.unmount()

    resetScopeAccessCache()
    mocks.listProjectKeys.mockClear()
    mocks.getProjectAccess.mockResolvedValueOnce({
      actor_id: '1',
      workspace_id: '10',
      project_id: '20',
      actions: ['keys.read', 'projects.read', 'models.read'],
      issuable_key_actions: [],
      scope_version: '1',
      expires_at: new Date(Date.now() + 3600_000).toISOString(),
    })
    const readOnly = mountKeysView()
    await flushPromises()
    expect(mocks.listProjectKeys).toHaveBeenCalled()
    expect(readOnly.find('[data-testid="keys-create-btn"]').exists()).toBe(false)
    readOnly.unmount()

    resetScopeAccessCache()
    mocks.listProjectKeys.mockClear()
    mocks.getProjectAccess.mockResolvedValueOnce({
      actor_id: '1',
      workspace_id: '10',
      project_id: '20',
      actions: ['projects.read', 'models.read'],
      issuable_key_actions: [],
      scope_version: '1',
      expires_at: new Date(Date.now() + 3600_000).toISOString(),
    })
    const noKeysGrant = mountKeysView()
    await flushPromises()
    expect(mocks.listProjectKeys).not.toHaveBeenCalled()
    expect(noKeysGrant.find('[data-testid="keys-create-btn"]').exists()).toBe(false)
    expect(noKeysGrant.get('[data-testid="scope-gate-empty-state"]').attributes('data-state')).toBe('denied')
    noKeysGrant.unmount()

    resetScopeAccessCache()
    mocks.getProjectAccess.mockResolvedValueOnce({
      actor_id: '1',
      workspace_id: '10',
      project_id: '20',
      actions: ['keys.read', 'keys.create'],
      issuable_key_actions: [],
      scope_version: '1',
      expires_at: new Date(Date.now() + 3600_000).toISOString(),
    })
    const emptyActions = mountKeysView()
    await flushPromises()
    await emptyActions.get('[data-tour="keys-create-btn"]').trigger('click')
    await flushPromises()
    expect(emptyActions.find('[data-testid="project-key-empty-actions-alert"]').exists()).toBe(true)
    expect(emptyActions.get('[data-tour="key-form-submit"]').attributes('disabled')).toBeDefined()
    emptyActions.unmount()

    resetScopeAccessCache()
    mocks.getProjectAccess.mockRejectedValueOnce(
      Object.assign(new Error('Service Unavailable'), { status: 503, metadata: { request_id: 'req-k503' } })
    )
    const unavailableWrapper = mountKeysView()
    await flushPromises()
    expect(unavailableWrapper.get('[data-testid="scope-gate-empty-state"]').attributes('data-state')).toBe('unavailable')
    expect(unavailableWrapper.find('[data-tour="keys-create-btn"]').exists()).toBe(false)
    unavailableWrapper.unmount()

    resetScopeAccessCache()
    mocks.listProjectKeys.mockClear()
    mocks.listProjectKeyGroups.mockClear()
    mocks.getProjectAccess.mockClear()
    seedProjectScope('10', null)
    const overviewWrapper = mountKeysView()
    await flushPromises()
    expect(overviewWrapper.get('[data-testid="scope-gate-empty-state"]').attributes('data-state')).toBe('empty')
    expect(mocks.listKeys).not.toHaveBeenCalled()
    expect(mocks.listProjectKeys).not.toHaveBeenCalled()
    expect(mocks.getProjectAccess).not.toHaveBeenCalled()
  })
})
