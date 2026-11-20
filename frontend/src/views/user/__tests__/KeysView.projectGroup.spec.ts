import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
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
  createProjectKey: vi.fn(),
  listScopes: vi.fn(),
  consoleScope: { mode: 'workspace' as const, workspaceId: '10', projectId: '20' },
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

vi.mock('@/features/console/scope', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/features/console/scope')>()
  return {
    ...actual,
    readPersistedConsoleScope: () => ({ ...mocks.consoleScope }),
    identityApi: {
      ...actual.identityApi,
      listScopes: mocks.listScopes,
      listProjectKeys: mocks.listProjectKeys,
      createProjectKey: mocks.createProjectKey,
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

describe('KeysView project-scoped API key group binding guard (W1-E2E-P0-01)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.consoleScope = { mode: 'workspace', workspaceId: '10', projectId: '20' }
    mocks.listKeys.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
    mocks.getPublicSettings.mockResolvedValue({ api_base_url: 'https://api.example.com', site_name: 'OmniHive' })
    mocks.getDashboardApiKeysUsage.mockResolvedValue({ stats: {} })
    mocks.getUserGroupRates.mockResolvedValue({})
    mocks.listProjectKeys.mockResolvedValue({ items: [] })
    mocks.listScopes.mockResolvedValue({ items: [] })
  })

  it('blocks project-scoped key creation and shows explicit validation error when no available group exists', async () => {
    mocks.getAvailableGroups.mockResolvedValue([])

    const wrapper = mount(KeysView, { global: { stubs } })
    await flushPromises()

    await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
    await flushPromises()

    const checkbox = wrapper.get('#key-form input[type="checkbox"]')
    await checkbox.setValue(true)
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
    mocks.getAvailableGroups.mockResolvedValue([
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

    const checkbox = wrapper.get('#key-form input[type="checkbox"]')
    await checkbox.setValue(true)
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
    mocks.getAvailableGroups.mockImplementation(async (options?: { workspaceId?: string; projectId?: string }) => {
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

    const checkbox = wrapper.get('#key-form input[type="checkbox"]')
    await checkbox.setValue(true)
    await flushPromises()

    expect(mocks.getAvailableGroups).toHaveBeenCalledWith(
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
    mocks.getAvailableGroups.mockImplementation(async (options?: { workspaceId?: string; projectId?: string }) => {
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

    const checkbox = wrapper.get('#key-form input[type="checkbox"]')
    await checkbox.setValue(true)
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
    mocks.getAvailableGroups.mockImplementation(async (options?: { workspaceId?: string; projectId?: string }) => {
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

    await wrapper.get('#key-form input[type="checkbox"]').setValue(true)
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
})
