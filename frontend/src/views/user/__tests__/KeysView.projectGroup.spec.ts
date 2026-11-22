import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { identityScoped } from '@/router/identityScope'
import KeysView from '../KeysView.vue'

enableAutoUnmount(afterEach)

const mocks = vi.hoisted(() => ({
  listKeys: vi.fn(),
  createKey: vi.fn(),
  getPublicSettings: vi.fn(),
  getDashboardApiKeysUsage: vi.fn(),
  getAvailableGroups: vi.fn(),
  projectGroups: vi.fn(),
  listKeyGroups: vi.fn(),
  getUserGroupRates: vi.fn(),
  showError: vi.fn(),
  showInfo: vi.fn(),
  showSuccess: vi.fn(),
  listProjectKeys: vi.fn(),
  createProjectKey: vi.fn(),
  listScopes: vi.fn(),
  getProjectAccess: vi.fn(),
  authState: null as any,
  consoleScope: { mode: 'workspace' as const, workspaceId: '10', projectId: '20' },
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => mocks.authState,
}))

vi.mock('@/stores/app', () => ({
  SCOPE_CHANGED_EVENT: 'omnihive:scope-changed',
  useAppStore: () => ({
    get activeScope() { return mocks.consoleScope },
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

vi.mock('@/features/console/scope', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/features/console/scope')>()
  return {
    ...actual,
    readPersistedConsoleScope: () => ({ ...mocks.consoleScope }),
    identityApi: {
      ...actual.identityApi,
      listScopes: mocks.listScopes,
      listProjectKeys: mocks.listProjectKeys,
      listKeyGroups: mocks.listKeyGroups,
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
  DataTable: { template: '<div />' },
  Pagination: { template: '<div />' },
  BaseDialog: {
    props: ['show', 'title'],
    template: '<div v-if="show" data-testid="base-dialog"><slot /><slot name="footer" /></div>',
  },
  ConfirmDialog: { template: '<div />' },
  UseKeyModal: { template: '<div />' },
  Select: {
    props: ['modelValue', 'options'],
    template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', Number($event.target.value) || null)"><option v-for="o in options" :key="o.value" :value="o.value">{{ o.label }}</option></select>',
  },
  Icon: { template: '<span />' },
  GroupBadge: { template: '<span />' },
  GroupOptionItem: { template: '<span />' },
  PlatformIcon: { template: '<span />' },
  ModelAccessRuleEditor: { template: '<div />' },
  DatePicker: { template: '<input />' },
  Teleport: true,
}

async function mountRoutedKeys() {
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/keys', component: identityScoped(KeysView, false, false) }
  ] })
  await router.push('/keys')
  return mount(RouterView, { global: { plugins: [router], stubs } })
}

describe('KeysView project-scoped API key group binding guard (W1-E2E-P0-01)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.consoleScope = reactive({ mode: 'workspace', workspaceId: '10', projectId: '20' })
    mocks.authState = reactive({ user: { id: 1, email: 'dev@example.com', role: 'user', balance: 100 }, isAuthenticated: true, isSimpleMode: false, refreshUser: vi.fn(), sessionRevision: 'first' })
    mocks.listKeys.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
    mocks.getPublicSettings.mockResolvedValue({ api_base_url: 'https://api.example.com', site_name: 'OmniHive' })
    mocks.getDashboardApiKeysUsage.mockResolvedValue({ stats: {} })
    mocks.getUserGroupRates.mockResolvedValue({})
    mocks.listProjectKeys.mockResolvedValue({ items: [], next_cursor: null, as_of: '2026-11-22T00:00:00Z' })
    mocks.projectGroups.mockResolvedValue([])
    mocks.listKeyGroups.mockImplementation(async (workspaceId, projectId) => ({ items: (await mocks.projectGroups({ workspaceId, projectId })).map((group: { id: number | string }) => ({ ...group, id: String(group.id) })), next_cursor: null, as_of: '2026-11-22T00:00:00Z' }))
    mocks.listScopes.mockResolvedValue({ items: [] })
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
      scope_version: '1',
      expires_at: '2026-11-24T00:00:00Z',
    })
  })

  it('blocks project-scoped key creation and shows explicit validation error when no available group exists', async () => {
    mocks.projectGroups.mockResolvedValue([])

    const wrapper = mount(KeysView, { global: { stubs } })
    await flushPromises()

    await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
    await flushPromises()


    const nameInput = wrapper.get('[data-tour="key-form-name"]')
    await nameInput.setValue('unbound-project-key')

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
  })

  it('creates project-scoped key with explicit group_id when an available group exists', async () => {
    mocks.projectGroups.mockResolvedValue([
      {
        id: 7,
        name: 'Primary OpenAI Pool',
        platform: 'openai',
        status: 'active',
        subscription_type: 'standard',
        rate_multiplier: 1,
        is_exclusive: false,
      },
    ])
    mocks.createProjectKey.mockResolvedValue({
      id: 'pk_1',
      name: 'bound-project-key',
      credential: 'sk-proj-secret',
    })

    const wrapper = mount(KeysView, { global: { stubs } })
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
      })
    )
    expect(mocks.showSuccess).toHaveBeenCalledWith('keys.keyCreatedSuccess')
    expect(mocks.getAvailableGroups).not.toHaveBeenCalled()
    expect(mocks.createKey).not.toHaveBeenCalled()
  })

  it('loads workspace/project-scoped groups instead of account-level group #1 for new workspace default project keys (E2-5)', async () => {
    mocks.consoleScope = { mode: 'workspace', workspaceId: '4', projectId: '5' }
    const accountGroup = {
      id: 1,
      name: 'default',
      platform: 'anthropic',
      status: 'active',
      subscription_type: 'standard',
      rate_multiplier: 1,
      is_exclusive: false,
    }
    const workspace4OpenAIGroup = {
      id: 42,
      name: 'ws4-openai',
      platform: 'openai',
      status: 'active',
      subscription_type: 'standard',
      rate_multiplier: 1,
      is_exclusive: false,
    }
    mocks.projectGroups.mockImplementation(async (options?: { workspaceId?: string; projectId?: string }) => {
      if (options?.workspaceId === '4' && options?.projectId === '5') {
        return [workspace4OpenAIGroup]
      }
      return [accountGroup]
    })
    mocks.createProjectKey.mockResolvedValue({
      id: 'pk_42',
      name: 'fe-e2e-restricted',
      credential: 'sk-proj-secret',
    })

    const wrapper = mount(KeysView, { global: { stubs } })
    await flushPromises()

    await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
    await flushPromises()


    expect(mocks.projectGroups).toHaveBeenCalledWith(
      expect.objectContaining({ workspaceId: '4', projectId: '5' })
    )
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
      })
    )
  })

  it('clears workspace 1 default group and blocks submit when workspace 4 default project 5 has no scoped groups (E2-5)', async () => {
    mocks.consoleScope = { mode: 'workspace', workspaceId: '4', projectId: '5' }
    mocks.projectGroups.mockImplementation(async (options?: { workspaceId?: string; projectId?: string }) => {
      if (options?.workspaceId === '4' && options?.projectId === '5') {
        return []
      }
      return [
        {
          id: 1,
          name: 'default',
          platform: 'anthropic',
          status: 'active',
          subscription_type: 'standard',
          rate_multiplier: 1,
          is_exclusive: false,
        },
      ]
    })

    const wrapper = mount(KeysView, { global: { stubs } })
    await flushPromises()

    await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
    await flushPromises()


    await wrapper.get('[data-tour="key-form-name"]').setValue('fe-e2e-restricted')
    expect(wrapper.get('[data-testid="project-key-group-required-alert"]').text()).toContain(
      'keys.projectGroupRequired'
    )
    expect(wrapper.get('[data-tour="key-form-submit"]').attributes('disabled')).toBeDefined()

    await wrapper.get('#key-form').trigger('submit.prevent')
    await flushPromises()

    expect(mocks.createProjectKey).not.toHaveBeenCalled()
  })

  it('filters out any foreign-workspace group returned in a scoped project lookup (E2-5)', async () => {
    mocks.consoleScope = { mode: 'workspace', workspaceId: '4', projectId: '5' }
    // Provide an account-level group so unpatched code would still see group #1.
    mocks.projectGroups.mockImplementation(async (options?: { workspaceId?: string; projectId?: string }) => {
      if (options?.workspaceId === '4' && options?.projectId === '5') {
        return [
          {
            id: 1,
            workspace_id: 1,
            name: 'foreign-default',
            platform: 'anthropic',
            status: 'active',
            subscription_type: 'standard',
            rate_multiplier: 1,
            is_exclusive: false,
          },
          {
            id: 42,
            workspace_id: 4,
            name: 'ws4-openai',
            platform: 'openai',
            status: 'active',
            subscription_type: 'standard',
            rate_multiplier: 1,
            is_exclusive: false,
          },
        ]
      }
      return [
        {
          id: 1,
          name: 'default',
          platform: 'anthropic',
          status: 'active',
          subscription_type: 'standard',
          rate_multiplier: 1,
          is_exclusive: false,
        },
      ]
    })
    mocks.createProjectKey.mockResolvedValue({
      id: 'pk_42',
      name: 'ws4-only-key',
      credential: 'sk-proj-secret',
    })

    const wrapper = mount(KeysView, { global: { stubs } })
    await flushPromises()

    await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
    await flushPromises()


    await wrapper.get('[data-tour="key-form-name"]').setValue('ws4-only-key')
    await wrapper.get('#key-form').trigger('submit.prevent')
    await flushPromises()

    expect(mocks.createProjectKey).toHaveBeenCalledWith(
      '4',
      '5',
      expect.objectContaining({
        group_id: '42',
      })
    )
  })

  it('keeps models.read unchecked by default and allows opting in on project keys (E2-2)', async () => {
    mocks.projectGroups.mockResolvedValue([
      {
        id: '12',
        name: 'openai-main',
        platform: 'openai',
        status: 'active',
        subscription_type: 'standard',
        rate_multiplier: 1,
        is_exclusive: false,
      },
    ])
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

    const wrapper = mount(KeysView, { global: { stubs } })
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
      })
    )
    const issuedBanner = wrapper.get('[data-testid="issued-project-key-scope-banner"]')
    expect(issuedBanner.text()).toContain('keys.issuedProjectKeyScopeTitle')
    expect(issuedBanner.text()).toContain('text.chat')
    expect(issuedBanner.text()).toContain('keys.projectScopeModelsReadExcluded')

    // Open again and explicitly grant models.read
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
      })
    )
    const issuedWithModels = wrapper.get('[data-testid="issued-project-key-scope-banner"]')
    expect(issuedWithModels.findAll('code').map((c) => c.text())).toContain('models.read')
    expect(issuedWithModels.text()).toContain('keys.projectScopeModelsReadIncluded')
  })

  it('shows create button and row actions when project access grants keys.create (without keys.write) and hides them for keys.read-only', async () => {
    const withCreate = mount(KeysView, { global: { stubs } })
    await flushPromises()
    expect(withCreate.find('[data-testid="keys-create-btn"]').exists()).toBe(true)
    withCreate.unmount()

    mocks.getProjectAccess.mockResolvedValueOnce({
      actor_id: '1',
      workspace_id: '10',
      project_id: '20',
      actions: ['keys.read', 'projects.read', 'models.read'],
      scope_version: '1',
      expires_at: '2026-11-24T00:00:00Z',
    })
    const readOnly = mount(KeysView, { global: { stubs } })
    await flushPromises()
    expect(mocks.listProjectKeys).toHaveBeenCalled()
    expect(mocks.listKeys).not.toHaveBeenCalled()
    expect(readOnly.find('[data-testid="keys-create-btn"]').exists()).toBe(false)
    readOnly.unmount()

    mocks.listProjectKeys.mockClear()
    mocks.listKeys.mockClear()
    mocks.getProjectAccess.mockResolvedValueOnce({
      actor_id: '1',
      workspace_id: '10',
      project_id: '20',
      actions: ['projects.read', 'models.read'],
      scope_version: '1',
      expires_at: '2026-11-24T00:00:00Z',
    })
    const noKeysGrant = mount(KeysView, { global: { stubs } })
    await flushPromises()
    expect(mocks.listKeys).not.toHaveBeenCalled()
    expect(mocks.listProjectKeys).not.toHaveBeenCalled()
    expect(noKeysGrant.find('[data-testid="keys-create-btn"]').exists()).toBe(false)
    noKeysGrant.unmount()
  })
  it.each([403, 404, 503, 0])('does not restore create permission after access status %s', async status => {
    mocks.getProjectAccess.mockRejectedValue({ status, message: 'access unavailable' })
    const wrapper = mount(KeysView, { global: { stubs } })
    await flushPromises()
    expect(wrapper.find('[data-testid="keys-create-btn"]').exists()).toBe(false)
    expect(mocks.listKeys).not.toHaveBeenCalled()
    expect(mocks.createKey).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('keeps an empty grant/action intersection empty instead of restoring default actions', async () => {
    mocks.getProjectAccess.mockResolvedValue({ actor_id: '1', workspace_id: '10', project_id: '20', actions: ['keys.read', 'keys.create'], scope_version: '1', expires_at: '2099-01-01T00:00:00Z' })
    mocks.projectGroups.mockResolvedValue([{ id: 7, name: 'OpenAI', platform: 'openai' }])
    const wrapper = mount(KeysView, { global: { stubs } })
    await flushPromises()
    await wrapper.get('[data-testid="keys-create-btn"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-tour="key-form-name"]').setValue('empty-intersection')
    expect(wrapper.get('[data-tour="key-form-submit"]').attributes('disabled')).toBeDefined()
    await wrapper.get('#key-form').trigger('submit.prevent')
    await flushPromises()
    expect(mocks.createProjectKey).not.toHaveBeenCalled()
    expect(mocks.createKey).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('ignores the first delayed access response when scope changes before mounting finishes', async () => {
    let resolveOld!: (value: unknown) => void
    mocks.getProjectAccess.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
    const wrapper = await mountRoutedKeys()
    mocks.getProjectAccess.mockResolvedValue({ actor_id: '1', workspace_id: '11', project_id: '21', actions: [], scope_version: '2', expires_at: '2099-01-01T00:00:00Z' })
    mocks.consoleScope.workspaceId = '11'
    mocks.consoleScope.projectId = '21'
    window.dispatchEvent(new Event('omnihive:scope-changed'))
    await flushPromises()
    resolveOld({ actor_id: '1', workspace_id: '10', project_id: '20', actions: ['keys.read', 'keys.create', 'text.chat'], scope_version: '1', expires_at: '2099-01-01T00:00:00Z' })
    await flushPromises()
    expect(mocks.getProjectAccess).toHaveBeenCalledWith('11', '21')
    expect(wrapper.find('[data-testid="keys-create-btn"]').exists()).toBe(false)
    expect(mocks.listProjectKeys).not.toHaveBeenCalled()
    expect(mocks.listKeys).not.toHaveBeenCalled()
  })

  it('keeps a delayed one-time secret attributed to its original scope without closing the new form', async () => {
    mocks.projectGroups.mockResolvedValue([{ id: 7, name: 'OpenAI', platform: 'openai' }])
    let resolveCreated!: (value: unknown) => void
    mocks.createProjectKey.mockImplementationOnce(() => new Promise(resolve => { resolveCreated = resolve }))
    const wrapper = await mountRoutedKeys()
    await flushPromises()
    await wrapper.get('[data-testid="keys-create-btn"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-tour="key-form-name"]').setValue('original-key')
    await wrapper.get('#key-form').trigger('submit.prevent')
    await flushPromises()
    mocks.consoleScope.workspaceId = '11'
    mocks.consoleScope.projectId = '21'
    window.dispatchEvent(new Event('omnihive:scope-changed'))
    await flushPromises()
    await wrapper.get('[data-testid="keys-create-btn"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-tour="key-form-name"]').setValue('new-scope-draft')
    resolveCreated({ id: '901', name: 'original-key', actions: ['text.chat'], credential: 'one-time-original-secret' })
    await flushPromises()
    expect(mocks.createProjectKey).toHaveBeenCalledTimes(1)
    expect(mocks.createProjectKey).toHaveBeenCalledWith('10', '20', expect.objectContaining({ name: 'original-key' }))
    expect(wrapper.get('[data-testid="key-secret-scope"]').text()).toContain('Workspace #10 / Project #20')
    expect(wrapper.get('[data-testid="key-secret-value"]').text()).toBe('one-time-original-secret')
    expect(wrapper.get('[data-testid="issued-project-key-origin"]').text()).toContain('Workspace #10 / Project #20')
    expect(wrapper.get<HTMLInputElement>('[data-tour="key-form-name"]').element.value).toBe('new-scope-draft')
    expect(mocks.createKey).not.toHaveBeenCalled()
  })

  it('destroys a delayed secret when the login principal changes', async () => {
    mocks.projectGroups.mockResolvedValue([{ id: 7, name: 'OpenAI', platform: 'openai' }])
    let resolveCreated!: (value: unknown) => void
    mocks.createProjectKey.mockImplementationOnce(() => new Promise(resolve => { resolveCreated = resolve }))
    const wrapper = await mountRoutedKeys()
    await flushPromises()
    await wrapper.get('[data-testid="keys-create-btn"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-tour="key-form-name"]').setValue('original-key')
    await wrapper.get('#key-form').trigger('submit.prevent')
    await flushPromises()
    mocks.authState.user.id = 2
    await flushPromises()
    resolveCreated({ id: '901', name: 'original-key', actions: ['text.chat'], credential: 'must-never-cross-actor' })
    await flushPromises()
    expect(wrapper.find('[data-testid="key-secret-once-dialog"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('must-never-cross-actor')
    expect(wrapper.find('[data-testid="issued-project-key-scope-banner"]').exists()).toBe(false)
    expect(mocks.showInfo).toHaveBeenCalledWith(expect.stringContaining('original account'))
    expect(mocks.createProjectKey).toHaveBeenCalledTimes(1)
  })

  it('queues subsequent one-time results instead of overwriting the visible secret', async () => {
    mocks.projectGroups.mockResolvedValue([{ id: 7, name: 'OpenAI', platform: 'openai' }])
    mocks.createProjectKey
      .mockResolvedValueOnce({ id: '901', name: 'first', credential: 'first-once-only', actions: ['text.chat'] })
      .mockResolvedValueOnce({ id: '902', name: 'second', credential: 'second-once-only', actions: ['text.chat'] })
    const wrapper = await mountRoutedKeys()
    await flushPromises()
    await wrapper.get('[data-testid="keys-create-btn"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-tour="key-form-name"]').setValue('first')
    await wrapper.get('#key-form').trigger('submit.prevent')
    await flushPromises()
    mocks.consoleScope.workspaceId = '11'
    mocks.consoleScope.projectId = '21'
    window.dispatchEvent(new Event('omnihive:scope-changed'))
    await flushPromises()
    await wrapper.get('[data-testid="keys-create-btn"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-tour="key-form-name"]').setValue('second')
    await wrapper.get('#key-form').trigger('submit.prevent')
    await flushPromises()
    expect(wrapper.get('[data-testid="key-secret-value"]').text()).toBe('first-once-only')
    expect(wrapper.get('[data-testid="key-secret-scope"]').text()).toContain('Workspace #10 / Project #20')
    await wrapper.get('[data-testid="confirm-secret-saved-checkbox"]').setValue(true)
    await wrapper.get('[data-testid="confirm-secret-close-btn"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="key-secret-value"]').text()).toBe('second-once-only')
    expect(wrapper.get('[data-testid="key-secret-scope"]').text()).toContain('Workspace #11 / Project #21')
    expect(wrapper.get<HTMLInputElement>('[data-testid="confirm-secret-saved-checkbox"]').element.checked).toBe(false)
  })

})
