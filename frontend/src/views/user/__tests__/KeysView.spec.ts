import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick, reactive } from 'vue'
import { identityScoped } from '@/router/identityScope'
import * as gatewayUrl from '@/api/url'
import englishDashboard from '@/i18n/locales/en/dashboard'
import chineseDashboard from '@/i18n/locales/zh/dashboard'

import type { ApiKey } from '@/types'
import KeysView from '../KeysView.vue'

let credentialLocale: 'en' | 'zh' = 'en'
const { authState } = vi.hoisted(() => ({ authState: { current: null as any } }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => authState.current }))
const translateCredential = (key: string, values: Record<string, string> = {}) => {
  const translations = credentialLocale === 'en' ? englishDashboard.keys.payAsYouGo : chineseDashboard.keys.payAsYouGo
  const message = translations[key.slice('keys.payAsYouGo.'.length) as keyof typeof translations]
  return message?.replace(/\{(\w+)\}/g, (_, name: string) => values[name] ?? `{${name}}`) ?? key
}

const {
  listKeys,
  updateKey,
  getPublicSettings,
  getDashboardApiKeysUsage,
  getAvailableGroups,
  getUserGroupRates,
  showError,
  showInfo,
  showSuccess,
  copyToClipboard,
  isCurrentStep,
  nextStep,
  createKey,
} = vi.hoisted(() => ({
  listKeys: vi.fn(),
  updateKey: vi.fn(),
  getPublicSettings: vi.fn(),
  getDashboardApiKeysUsage: vi.fn(),
  getAvailableGroups: vi.fn(),
  getUserGroupRates: vi.fn(),
  showError: vi.fn(),
  showInfo: vi.fn(),
  showSuccess: vi.fn(),
  copyToClipboard: vi.fn(),
  isCurrentStep: vi.fn(),
  nextStep: vi.fn(),
  createKey: vi.fn(),
}))

const messages: Record<string, string> = {
  'common.actions': 'Actions',
  'common.name': 'Name',
  'common.refresh': 'Refresh',
  'common.status': 'Status',
  'keys.apiKey': 'API Key',
  'keys.allGroups': 'All Groups',
  'keys.allStatus': 'All Status',
  'keys.columnSettings': 'Column Settings',
  'keys.createKey': 'Create API Key',
  'keys.importToCcSwitch': 'Import to CC Switch',
  'keys.ccSwitchSecretUnavailable': 'secret only shown once; rotate and import right away',
  'keys.ccSwitchBaseUrlMissing': 'no public API base URL configured',
  'keys.ccSwitchNotInstalled': 'CC-Switch missing',
  'keys.created': 'Created',
  'keys.expiresAt': 'Expires',
  'keys.group': 'Group',
  'keys.id': 'ID',
  'keys.currentConcurrency': 'Current Concurrency',
  'keys.lastUsedAt': 'Last Used',
  'keys.lastUsedIP': 'Last Used IP',
  'keys.rateLimitColumn': 'Rate Limit',
  'keys.searchPlaceholder': 'Search name or key...',
  'keys.status.active': 'Active',
  'keys.status.disabled': 'Disabled by admin',
  'keys.status.expired': 'Expired',
  'keys.status.inactive': 'Inactive',
  'keys.status.quota_exhausted': 'Quota exhausted',
  'keys.statusDisabledHint': 'An administrator disabled this key; it cannot be re-enabled from the panel.',
  'keys.usage': 'Usage',
}

vi.mock('@/api', () => ({
  keysAPI: {
    list: listKeys,
    create: createKey,
    update: updateKey,
    delete: vi.fn(),
    toggleStatus: vi.fn(),
    rotate: vi.fn(),
    revokeCredentials: vi.fn(),
  },
  authAPI: {
    getPublicSettings,
  },
  usageAPI: {
    getDashboardApiKeysUsage,
  },
  userGroupsAPI: {
    getAvailable: getAvailableGroups,
    getUserGroupRates,
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showInfo,
    showSuccess,
    publicSettingsLoaded: true,
    fetchPublicSettings: vi.fn(),
  }),
}))
const visibleModels = vi.hoisted(() => vi.fn())
vi.mock('@/api/modelAccess', () => ({ keyVisibleModels: visibleModels }))

vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({
    isCurrentStep,
    nextStep,
  }),
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copied: { value: false },
    copyToClipboard,
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, values: Record<string, string> = {}) => key.startsWith('keys.payAsYouGo.')
        ? translateCredential(key, values)
        : values.time ? `${key}:${values.time}` : messages[key] ?? key,
    }),
  }
})

const createApiKey = (): ApiKey => ({
  id: 1,
  user_id: 1,
  key: 'sk-test-key',
  name: 'test-key',
  group_id: null,
  status: 'active',
  ip_whitelist: [],
  ip_blacklist: [],
  last_used_at: null,
  last_used_ip: null,
  quota: 0,
  quota_used: 0,
  expires_at: null,
  created_at: '2026-06-27T00:00:00Z',
  updated_at: '2026-06-27T00:00:00Z',
  current_concurrency: 3,
  rate_limit_5h: 0,
  rate_limit_1d: 0,
  rate_limit_7d: 0,
  usage_5h: 0,
  usage_1d: 0,
  usage_7d: 0,
  window_5h_start: null,
  window_1d_start: null,
  window_7d_start: null,
  reset_5h_at: null,
  reset_1d_at: null,
  reset_7d_at: null,
  budget_period_mode: 'off',
  budget_limit: 0,
  budget_used: 0,
  budget_reserved: 0,
  budget_remaining: 0,
  budget_overrun: 0,
  budget_period_start: null,
  budget_period_end: null,
  budget_period_display: '',
  budget_legacy_anchor: '',
  budget_duration_seconds: 0,
  budget_calendar_unit: '',
  budget_anchor: '',
  budget_timezone: 'UTC',
  budget_enforced: false,
})

const AppLayoutStub = {
  template: '<div><slot /></div>',
}

const TablePageLayoutStub = {
  template: `
    <div>
      <slot name="filters" />
      <slot name="actions" />
      <slot name="table" />
      <slot name="pagination" />
    </div>
  `,
}

const DataTableStub = {
  name: 'DataTable',
  props: ['columns', 'data'],
  emits: ['sort'],
  template: `
    <div>
      <div data-test="columns">{{ columns.map((col) => col.key).join(',') }}</div>
      <div data-test="columns-meta">{{ JSON.stringify(columns.map((col) => ({ key: col.key, sortable: !!col.sortable }))) }}</div>
      <button data-test="sort-current-concurrency" @click="$emit('sort', 'current_concurrency', 'asc')">
        Sort Current Concurrency
      </button>
      <div v-for="row in data" :key="row.id">
        <div
          v-if="columns.some((col) => col.key === 'id')"
          data-test="key-id"
        >
          <slot name="cell-id" :value="row.id" :row="row" />
        </div>
        <div data-test="usage"><slot name="cell-usage" :row="row" /></div>
        <slot name="cell-name" :value="row.name" :row="row" />
        <slot name="cell-actions" :row="row" />
        <div data-test="current-concurrency">
          <slot name="cell-current_concurrency" :value="row.current_concurrency" :row="row" />
        </div>
        <div
          v-if="columns.some((col) => col.key === 'last_used_ip')"
          data-test="last-used-ip"
        >
          <slot name="cell-last_used_ip" :value="row.last_used_ip" :row="row" />
        </div>
        <div
          v-if="columns.some((col) => col.key === 'key')"
          data-test="key-cell"
        >
          <slot name="cell-key" :value="row.key" :row="row" />
        </div>
        <div data-test="rate-limit-cell">
          <slot name="cell-rate_limit" :value="row" :row="row" />
        </div>
        <div
          v-if="columns.some((col) => col.key === 'status')"
          data-test="status-cell"
        >
          <slot name="cell-status" :value="row.status" :row="row" />
        </div>
      </div>
      <slot name="empty" />
    </div>
  `,
}

const SelectStub = {
  name: 'Select',
  props: ['modelValue', 'options', 'disabled'],
  emits: ['update:modelValue'],
  template: `
    <select
      :value="modelValue"
      :disabled="disabled"
      :data-test="disabled ? 'select-disabled' : 'select-enabled'"
      @change="$emit('update:modelValue', $event.target.value)"
    >
      <option
        v-for="option in options"
        :key="option.value"
        :value="option.value"
        :data-test="'select-option-' + option.value"
      >{{ option.label }}</option>
    </select>
  `,
}

const SearchInputStub = {
  name: 'SearchInput',
  props: ['modelValue'],
  emits: ['update:modelValue', 'search'],
  template: '<input :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />',
}

const PaginationStub = {
  name: 'Pagination',
  props: ['page', 'total', 'pageSize'],
  emits: ['update:page', 'update:pageSize'],
  template: `
    <div>
      <button data-test="page-size-50" @click="$emit('update:pageSize', 50)">50</button>
    </div>
  `,
}

const IconStub = {
  props: ['name'],
  template: '<span data-test="icon">{{ name }}</span>',
}

const mountView = async (scoped = false) => {
  const wrapper = mount(scoped ? identityScoped(KeysView, false) : KeysView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        Pagination: PaginationStub,
        BaseDialog: {
          props: ['show'],
          template: '<div v-if="show"><slot /><slot name="footer" /></div>',
        },
        ConfirmDialog: true,
        EmptyState: true,
        Select: SelectStub,
        SearchInput: SearchInputStub,
        Icon: IconStub,
        UseKeyModal: true,
        EndpointPopover: true,
        GroupBadge: true,
        GroupOptionItem: true,
        Teleport: true,
      },
    },
  })
  await flushPromises()
  await nextTick()
  return wrapper
}

const visibleColumnKeys = (wrapper: VueWrapper) =>
  wrapper.get('[data-test="columns"]').text().split(',').filter(Boolean)

const visibleColumnMeta = (wrapper: VueWrapper): Array<{ key: string; sortable: boolean }> =>
  JSON.parse(wrapper.get('[data-test="columns-meta"]').text())

const getButtonByText = (wrapper: VueWrapper, text: string) => {
  const button = wrapper.findAll('button').find((item) => item.text().includes(text))
  if (!button) {
    throw new Error(`Button not found: ${text}`)
  }
  return button
}

const retainedFrom = '2026-09-01T00:00:00Z'
const historyError = { status: 409, reason: 'USAGE_HISTORY_INCOMPLETE', metadata: { raw_retained_from: retainedFrom } }
const usageResponse = { stats: { 1: { today_actual_cost: 12.3456, total_actual_cost: 45.6789, by_platform: [{ platform: 'grok', today_actual_cost: 12.3456, total_actual_cost: 45.6789 }] } } }

describe('user KeysView column settings', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.restoreAllMocks()
    credentialLocale = 'en'

    listKeys.mockReset()
    updateKey.mockReset()
    createKey.mockReset()
    getPublicSettings.mockReset()
    getDashboardApiKeysUsage.mockReset()
    getAvailableGroups.mockReset()
    getUserGroupRates.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    copyToClipboard.mockReset()
    isCurrentStep.mockReset()
    nextStep.mockReset()

    listKeys.mockResolvedValue({
      items: [createApiKey()],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    getPublicSettings.mockResolvedValue({})
    getDashboardApiKeysUsage.mockResolvedValue({ stats: {} })
    getAvailableGroups.mockResolvedValue([])
    getUserGroupRates.mockResolvedValue({})
    isCurrentStep.mockReturnValue(false)
  })

  it('refreshes the complete available list and preselects the only group on every create', async () => {
    getAvailableGroups.mockResolvedValue([{ id: 42, name: 'Only group' }])
    const wrapper = await mountView()
    await getButtonByText(wrapper, 'Create API Key').trigger('click')
    await flushPromises()
    expect(getAvailableGroups).toHaveBeenCalledTimes(2)
    const select = wrapper.findAllComponents({ name: 'Select' }).find(item => item.attributes('data-tour') === 'key-form-group')!
    expect(select.props('modelValue')).toBe(42)
    expect(wrapper.get('button[form="key-form"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('keeps all available groups and requires a choice when several are available', async () => {
    getAvailableGroups.mockResolvedValue(Array.from({ length: 125 }, (_, index) => ({ id: index + 1, name: `Group ${index + 1}` })))
    const wrapper = await mountView()
    await getButtonByText(wrapper, 'Create API Key').trigger('click')
    await flushPromises()
    const select = wrapper.findAllComponents({ name: 'Select' }).find(item => item.attributes('data-tour') === 'key-form-group')!
    expect(select.props('options')).toHaveLength(125)
    expect(select.props('modelValue')).toBeNull()
    expect(wrapper.get('button[form="key-form"]').attributes('disabled')).toBeDefined()
    select.vm.$emit('update:modelValue', 125)
    await nextTick()
    expect(wrapper.get('button[form="key-form"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('blocks creation with an actionable explanation when no groups are available', async () => {
    const wrapper = await mountView()
    await getButtonByText(wrapper, 'Create API Key').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('keys.noAvailableGroups')
    expect(wrapper.get('button[form="key-form"]').attributes('disabled')).toBeDefined()
    await wrapper.get('#key-form').trigger('submit')
    expect(createKey).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('blocks stale group choices while refreshing and offers retry after a load failure', async () => {
    getAvailableGroups.mockResolvedValueOnce([{ id: 42, name: 'Previous group' }])
    const wrapper = await mountView()
    let rejectLoad!: (error: Error) => void
    getAvailableGroups.mockImplementationOnce(() => new Promise((_, reject) => { rejectLoad = reject }))
    await getButtonByText(wrapper, 'Create API Key').trigger('click')
    expect(wrapper.text()).toContain('keys.groupsLoading')
    expect(wrapper.get('button[form="key-form"]').attributes('disabled')).toBeDefined()
    rejectLoad(new Error('network failed'))
    await flushPromises()
    expect(wrapper.text()).toContain('keys.groupsLoadFailed')
    expect(wrapper.get('button[form="key-form"]').attributes('disabled')).toBeDefined()
    getAvailableGroups.mockResolvedValue([{ id: 7, name: 'New group' }])
    await getButtonByText(wrapper, 'keys.retryGroups').trigger('click')
    await flushPromises()
    const select = wrapper.findAllComponents({ name: 'Select' }).find(item => item.attributes('data-tour') === 'key-form-group')!
    expect(select.props('modelValue')).toBe(7)
    expect(wrapper.get('button[form="key-form"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('aborts all auxiliary requests and ignores late failures after a same-user session change', async () => {
    authState.current = reactive({ user: { id: 1, role: 'user' }, isAuthenticated: true, sessionRevision: 'first' })
    const rejectRequests: Array<(error: unknown) => void> = []
    const auxiliaryApis = [getAvailableGroups, getUserGroupRates, getPublicSettings]
    auxiliaryApis.forEach(request => request.mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectRequests.push(reject) })))
    const logger = vi.spyOn(console, 'error').mockImplementation(() => {})
    const wrapper = await mountView(true)
    const signals = auxiliaryApis.map(request => request.mock.calls[0][0].signal as AbortSignal)
    expect(signals.every(signal => !signal.aborted)).toBe(true)
    authState.current.sessionRevision = 'second'
    await flushPromises()
    expect(signals.every(signal => signal.aborted)).toBe(true)
    auxiliaryApis.forEach(request => expect(request).toHaveBeenCalledTimes(2))
    rejectRequests.forEach(reject => reject(new Error('late request failed')))
    await flushPromises()
    expect(logger).not.toHaveBeenCalled()
    expect(showError).not.toHaveBeenCalled()
    wrapper.unmount()
    logger.mockRestore()
  })

  it.each(['AUTH_SESSION_CHANGED', 'NETWORK_ERROR'])('ignores a delayed old-session %s after same-user relogin', async (code) => {
    authState.current = reactive({ user: { id: 1, role: 'user' }, isAuthenticated: true, sessionRevision: 'first' })
    let rejectOld!: (error: unknown) => void
    listKeys.mockImplementationOnce(() => new Promise((_, reject) => { rejectOld = reject }))
    const wrapper = await mountView(true)
    const oldSignal = listKeys.mock.calls[0][3].signal as AbortSignal
    authState.current.sessionRevision = 'second'
    await flushPromises()
    expect(oldSignal.aborted).toBe(true)
    expect(listKeys).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('test-key')
    rejectOld({ code })
    await flushPromises()
    expect(showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('shows neutral unknown without coverage details on the first usage request', async () => {

    getDashboardApiKeysUsage.mockRejectedValueOnce(historyError)
    const wrapper = await mountView()
    await flushPromises()
    expect(wrapper.get('[data-test="usage"]').text()).toContain('—')
    expect(wrapper.get('[data-test="usage"]').text()).not.toContain(retainedFrom)
    expect(wrapper.get('[data-test="usage"]').text()).not.toContain('$0.0000')

    wrapper.unmount()
  })

  it('clears known usage on reload and shows neutral unknown instead of old costs', async () => {

    getDashboardApiKeysUsage.mockResolvedValueOnce(usageResponse)
    const wrapper = await mountView()
    await flushPromises()
    expect(wrapper.get('[data-test="usage"]').text()).toContain('12.3456')
    expect(wrapper.get('[data-test="usage"]').text()).toContain('45.6789')
    let rejectUsage!: (error: unknown) => void
    getDashboardApiKeysUsage.mockImplementationOnce(() => new Promise((_, reject) => { rejectUsage = reject }))
    await wrapper.get('[data-test="sort-current-concurrency"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="usage"]').text()).toContain('common.loading')
    expect(wrapper.get('[data-test="usage"]').text()).not.toContain('12.3456')
    rejectUsage(historyError)
    await flushPromises()
    expect(wrapper.get('[data-test="usage"]').text()).toContain('—')
    expect(wrapper.get('[data-test="usage"]').text()).not.toContain(retainedFrom)
    expect(wrapper.get('[data-test="usage"]').text()).not.toContain('45.6789')
    expect((wrapper.vm as unknown as { usageStats: object }).usageStats).toEqual({})
    wrapper.unmount()
  })

  it.each(['success', 'failure'] as const)('ignores a late usage %s after a newer request settles', async (outcome) => {

    let resolveOld!: (value: typeof usageResponse) => void
    let rejectOld!: (error: unknown) => void
    getDashboardApiKeysUsage.mockImplementationOnce(() => new Promise((resolve, reject) => { resolveOld = resolve; rejectOld = reject }))
    const wrapper = await mountView()
    await flushPromises()
    expect(wrapper.get('[data-test="usage"]').text()).toContain('common.loading')
    if (outcome === 'success') getDashboardApiKeysUsage.mockRejectedValueOnce(historyError)
    else getDashboardApiKeysUsage.mockResolvedValueOnce(usageResponse)
    await wrapper.get('[data-test="sort-current-concurrency"]').trigger('click')
    await flushPromises()
    if (outcome === 'success') resolveOld(usageResponse)
    else rejectOld(historyError)
    await flushPromises()
    const usage = wrapper.get('[data-test="usage"]').text()
    if (outcome === 'success') {
      expect(usage).toContain('—')
      expect(usage).not.toContain(retainedFrom)
      expect(usage).not.toContain('12.3456')
    } else {
      expect(usage).toContain('12.3456')
      expect(usage).not.toContain('—')
    }
    wrapper.unmount()
  })

  it('ignores session-change rejection while preserving real current-session failures', async () => {
    listKeys.mockRejectedValueOnce({ code: 'AUTH_SESSION_CHANGED' })
    const stale = await mountView()
    expect(showError).not.toHaveBeenCalled()
    stale.unmount()
    listKeys.mockRejectedValueOnce({ code: 'NETWORK_ERROR' })
    const current = await mountView()
    expect(showError).toHaveBeenCalledTimes(1)
    expect(showError).toHaveBeenCalledWith('keys.failedToLoad')
    current.unmount()
  })

  it('does not surface an older request failure after a newer list succeeds', async () => {
    let rejectOld!: (error: unknown) => void
    listKeys.mockImplementationOnce(() => new Promise((_, reject) => { rejectOld = reject }))
    const wrapper = await mountView()
    await wrapper.get('[data-test="sort-current-concurrency"]').trigger('click')
    await flushPromises()
    rejectOld({ code: 'NETWORK_ERROR' })
    await flushPromises()
    expect(wrapper.text()).toContain('test-key')
    expect(showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it.each(['en', 'zh'] as const)('renders and copies gateway addresses in %s', async (locale) => {
    credentialLocale = locale
    copyToClipboard.mockResolvedValue(true)
    const wrapper = await mountView()
    const translations = locale === 'en' ? englishDashboard.keys.payAsYouGo : chineseDashboard.keys.payAsYouGo
    const credentials = wrapper.get('section[aria-labelledby="pay-as-you-go-title"]')

    expect(credentials.get('h2').text()).toBe(translations.title)
    expect(credentials.text()).toContain(translations.description)
    expect(credentials.text()).toContain(translations.mode)
    expect(credentials.text()).toContain(translations.addressHint)
    // 文本/视觉协议入口与生成类入口分开列出，都能复制到正确地址。
    expect(credentials.findAll('code').map((address) => address.text())).toEqual([
      `${window.location.origin}/v1`,
      `${window.location.origin}/v1/messages`,
      `${window.location.origin}/v1beta`,
      `${window.location.origin}/v1/images/generations`,
      `${window.location.origin}/v1/images/edits`,
      `${window.location.origin}/v1/videos/generations`,
      `${window.location.origin}/v1/tts`,
      `${window.location.origin}/v1/stt`,
      `${window.location.origin}/v1/embeddings`,
      `${window.location.origin}/v1/rerank`
    ])
    expect(credentials.text()).toContain(translations.generationTitle)
    expect(credentials.text()).toContain(translations.generationHint)
    const copyButtons = credentials.findAll('button')
    for (const [index, protocol] of [translations.openai, translations.anthropic, translations.gemini].entries()) {
      expect(copyButtons[index].attributes('aria-label')).toBe(
        translations.copyAddress.replace('{protocol}', protocol)
      )
      await copyButtons[index].trigger('click')
    }
    expect(copyToClipboard).toHaveBeenNthCalledWith(1, `${window.location.origin}/v1`, translations.copied)
    expect(copyToClipboard).toHaveBeenNthCalledWith(2, `${window.location.origin}/v1/messages`, translations.copied)
    expect(credentials.text()).toContain(translations.gemini)
    expect(copyToClipboard).toHaveBeenNthCalledWith(3, `${window.location.origin}/v1beta`, translations.copied)
    expect(copyButtons[3].attributes('aria-label')).toBe(
      translations.copyAddress.replace('{protocol}', translations.imageGeneration)
    )
    await copyButtons[3].trigger('click')
    expect(copyToClipboard).toHaveBeenNthCalledWith(4, `${window.location.origin}/v1/images/generations`, translations.copied)
    expect(wrapper.findComponent({ name: 'DataTable' }).exists()).toBe(true)
    wrapper.unmount()
  })

  it('preserves the gateway origin returned by the environment URL helper', async () => {
    const buildGatewayUrl = vi.spyOn(gatewayUrl, 'buildGatewayUrl')
      .mockImplementation((path) => `https://gateway.example:8443${path}`)
    const wrapper = await mountView()
    expect(wrapper.findAll('section code').map((address) => address.text())).toEqual([
      'https://gateway.example:8443/v1',
      'https://gateway.example:8443/v1/messages',
      'https://gateway.example:8443/v1beta',
      'https://gateway.example:8443/v1/images/generations',
      'https://gateway.example:8443/v1/images/edits',
      'https://gateway.example:8443/v1/videos/generations',
      'https://gateway.example:8443/v1/tts',
      'https://gateway.example:8443/v1/stt',
      'https://gateway.example:8443/v1/embeddings',
      'https://gateway.example:8443/v1/rerank'
    ])
    expect(buildGatewayUrl).toHaveBeenCalledWith('/v1')
    expect(buildGatewayUrl).toHaveBeenCalledWith('/v1/messages')
    expect(buildGatewayUrl).toHaveBeenCalledWith('/v1beta')
    expect(buildGatewayUrl).toHaveBeenCalledWith('/v1/images/generations')
    wrapper.unmount()
  })

  it.each(['/proxy/sub2api', 'https://gateway.example:8443/proxy/sub2api'])(
    'copies the Gemini base URL with gateway prefix %s', async (prefix) => {
      vi.spyOn(gatewayUrl, 'buildGatewayUrl').mockImplementation((path) => `${prefix}${path}`)
      const wrapper = await mountView()
      const gatewayRoot = new URL(prefix, window.location.origin).href
      expect(wrapper.get('section code').text()).toBe(`${gatewayRoot}/v1`)
      await wrapper.get('section button').trigger('click')
      expect(copyToClipboard).toHaveBeenCalledWith(`${gatewayRoot}/v1`, englishDashboard.keys.payAsYouGo.copied)
      const geminiRow = wrapper.findAll('section dl > div').find(row => row.text().includes(englishDashboard.keys.payAsYouGo.gemini))!
      expect(geminiRow.get('code').text()).toBe(`${gatewayRoot}/v1beta`)
      await geminiRow.get('button').trigger('click')
      expect(copyToClipboard).toHaveBeenLastCalledWith(`${gatewayRoot}/v1beta`, englishDashboard.keys.payAsYouGo.copied)
      wrapper.unmount()
    }
  )

  it('keeps quota editing without any panel-side usage reset', async () => {
    const key: ApiKey = {
      ...createApiKey(), group_id: 1, quota: 10, quota_used: 10,
      status: 'quota_exhausted',
    }
    listKeys.mockResolvedValueOnce({ items: [key], total: 1, page: 1, page_size: 20, pages: 1 })
    updateKey.mockResolvedValue({ ...key, status: 'active', quota: 20 })
    const wrapper = await mountView()
    await getButtonByText(wrapper, 'common.edit').trigger('click')
    await wrapper.get('[data-tour="key-form-name"]').setValue('Unsaved name')
    // Consumed usage cannot be cleared from the panel any more.
    expect(wrapper.find('button[title="keys.resetQuotaUsed"]').exists()).toBe(false)
    expect(wrapper.findAllComponents({ name: 'ConfirmDialog' })
      .find((dialog) => dialog.props('title') === 'keys.resetQuotaTitle')).toBeUndefined()

    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()

    expect(updateKey).toHaveBeenCalledTimes(1)
    const [, updates] = updateKey.mock.calls[0]
    expect(updates).not.toHaveProperty('reset_quota')
    expect(updates).not.toHaveProperty('reset_rate_limit_usage')
    expect(updates.name).toBe('Unsaved name')
    wrapper.unmount()
  })

  it('expresses expired, exhausted and administratively disabled keys apart', async () => {
    const disabled: ApiKey = { ...createApiKey(), id: 11, name: 'admin-disabled', status: 'disabled' }
    const expired: ApiKey = { ...createApiKey(), id: 12, name: 'expired', status: 'expired', expires_at: '2026-01-01T00:00:00Z' }
    const exhausted: ApiKey = { ...createApiKey(), id: 13, name: 'exhausted', status: 'quota_exhausted' }
    listKeys.mockResolvedValueOnce({ items: [disabled, expired, exhausted], total: 3, page: 1, page_size: 20, pages: 1 })

    const wrapper = await mountView()

    const statuses = wrapper.findAll('[data-test="status-cell"] .badge')
    expect(statuses.map((badge) => badge.text())).toEqual([
      'Disabled by admin',
      'Expired',
      'Quota exhausted',
    ])
    const classes = statuses.map((badge) => badge.classes().join(' '))
    expect(new Set(classes).size).toBe(3)
    wrapper.unmount()
  })

  it('offers an explicit disabled status filter', async () => {
    const wrapper = await mountView()
    expect(wrapper.find('[data-test="select-option-disabled"]').text()).toBe('Disabled by admin')
    wrapper.unmount()
  })

  it('never sends status when editing an administratively disabled key', async () => {
    const disabled: ApiKey = { ...createApiKey(), id: 11, name: 'admin-disabled', group_id: 1, status: 'disabled' }
    listKeys.mockResolvedValueOnce({ items: [disabled], total: 1, page: 1, page_size: 20, pages: 1 })
    updateKey.mockResolvedValue(disabled)

    const wrapper = await mountView()
    await getButtonByText(wrapper, 'common.edit').trigger('click')

    // The status control is locked and the reason is spelled out.
    expect(wrapper.find('[data-test="select-disabled"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('An administrator disabled this key; it cannot be re-enabled from the panel.')

    await wrapper.get('[data-tour="key-form-name"]').setValue('Renamed disabled key')
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()

    expect(updateKey).toHaveBeenCalledTimes(1)
    const [, updates] = updateKey.mock.calls[0]
    expect(updates).not.toHaveProperty('status')
    expect(updates.name).toBe('Renamed disabled key')
    // Editing keeps the key in its original group instead of re-attributing it.
    expect(updates.group_id).toBe(1)
    wrapper.unmount()
  })

  it.each([
    ['GROUP_NOT_ALLOWED', 'This group is not allowed'],
    ['KEY_EXISTS', 'This key already exists'],
    ['INVALID_IP', 'Invalid IP address'],
  ])('shows the actual %s save failure from a normalized envelope', async (reason, message) => {
    const key: ApiKey = { ...createApiKey(), group_id: 1 }
    listKeys.mockResolvedValueOnce({ items: [key], total: 1, page: 1, page_size: 20, pages: 1 })
    updateKey.mockRejectedValueOnce({ code: 400, status: 400, reason, message, metadata: {} })
    const wrapper = await mountView()
    await getButtonByText(wrapper, 'common.edit').trigger('click')
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    expect(showError).toHaveBeenCalledWith(message)
    expect(wrapper.find('#key-form').exists()).toBe(true)
    wrapper.unmount()
  })

  it('uses the default API key columns with low-frequency columns hidden', async () => {
    const wrapper = await mountView()

    expect(visibleColumnKeys(wrapper)).toEqual([
      'name',
      'key',
      'group',
      'current_concurrency',
      'usage',
      'expires_at',
      'status',
      'created_at',
      'actions',
    ])
    expect(visibleColumnKeys(wrapper)).not.toContain('rate_limit')
    expect(visibleColumnKeys(wrapper)).not.toContain('last_used_at')
    expect(visibleColumnKeys(wrapper)).not.toContain('last_used_ip')
    expect(visibleColumnKeys(wrapper)).not.toContain('id')
  })

  it('shows a hidden column when toggled and persists the preference', async () => {
    const wrapper = await mountView()

    await wrapper.get('button[title="Column Settings"]').trigger('click')
    await getButtonByText(wrapper, 'Rate Limit').trigger('click')
    await nextTick()

    expect(visibleColumnKeys(wrapper)).toContain('rate_limit')
    expect(localStorage.getItem('api-key-hidden-columns')).toBe(
      JSON.stringify(['id', 'last_used_at', 'last_used_ip'])
    )
    expect(localStorage.getItem('api-key-column-settings-version')).toBe('3')
  })

  it('shows the API key ID column when toggled', async () => {
    const wrapper = await mountView()

    await wrapper.get('button[title="Column Settings"]').trigger('click')
    await getButtonByText(wrapper, 'ID').trigger('click')
    await nextTick()

    expect(visibleColumnKeys(wrapper)).toContain('id')
    expect(wrapper.get('[data-test="key-id"]').text()).toBe('#1')
    expect(visibleColumnMeta(wrapper).find((column) => column.key === 'id')?.sortable).toBe(true)
  })

  it('shows the last used IP column when toggled', async () => {
    listKeys.mockResolvedValueOnce({
      items: [{ ...createApiKey(), last_used_ip: '203.0.113.10' }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    const wrapper = await mountView()

    await wrapper.get('button[title="Column Settings"]').trigger('click')
    await getButtonByText(wrapper, 'Last Used IP').trigger('click')
    await nextTick()

    expect(visibleColumnKeys(wrapper)).toContain('last_used_ip')
    expect(wrapper.get('[data-test="last-used-ip"]').text()).toBe('203.0.113.10')
  })

  it('restores column preferences from localStorage on mount', async () => {
    localStorage.setItem('api-key-hidden-columns', JSON.stringify(['group', 'created_at']))
    localStorage.setItem('api-key-column-settings-version', '1')

    const wrapper = await mountView()

    expect(visibleColumnKeys(wrapper)).toEqual([
      'name',
      'key',
      'current_concurrency',
      'usage',
      'rate_limit',
      'expires_at',
      'status',
      'last_used_at',
      'actions',
    ])
    expect(localStorage.getItem('api-key-hidden-columns')).toBe(
      JSON.stringify(['group', 'created_at', 'last_used_ip', 'id'])
    )
    expect(localStorage.getItem('api-key-column-settings-version')).toBe('3')
  })

  it('does not include always-visible columns in the toggleable menu', async () => {
    const wrapper = await mountView()

    await wrapper.get('button[title="Column Settings"]').trigger('click')
    await nextTick()

    const columnMenuText = wrapper.text()
    expect(columnMenuText).toContain('API Key')
    expect(columnMenuText).toContain('ID')
    expect(columnMenuText).toContain('Current Concurrency')
    expect(columnMenuText).toContain('Rate Limit')
    expect(columnMenuText).toContain('Last Used IP')
    expect(columnMenuText).not.toContain('Name')
    expect(columnMenuText).not.toContain('Actions')
  })

  it('renders the current concurrency value', async () => {
    const wrapper = await mountView()

    expect(wrapper.get('[data-test="current-concurrency"]').text()).toBe('3')
  })

  it('marks current concurrency as sortable', async () => {
    const wrapper = await mountView()

    const currentConcurrencyColumn = visibleColumnMeta(wrapper).find(
      (column) => column.key === 'current_concurrency'
    )
    expect(currentConcurrencyColumn?.sortable).toBe(true)
  })

  it('shows the created secret once through the one-time dialog', async () => {
    getAvailableGroups.mockResolvedValue([{ id: 42, name: 'OpenAI' }])
    createKey.mockResolvedValue({
      api_key: { ...createApiKey(), id: 9 },
      secret: 'sk-created-once',
      grace_until: null,
    })
    const wrapper = await mountView()

    await getButtonByText(wrapper, 'Create API Key').trigger('click')
    await flushPromises()
    await wrapper.get('[data-tour="key-form-name"]').setValue('new key')
    const groupSelect = wrapper
      .findAllComponents({ name: 'Select' })
      .find((select) => (select.element as Element).getAttribute('data-tour') === 'key-form-group')!
    groupSelect.vm.$emit('update:modelValue', 42)
    await flushPromises()

    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()

    expect(createKey).toHaveBeenCalledWith('new key', 42, undefined, [], [], 0, undefined, {
      rate_limit_5h: 0,
      rate_limit_1d: 0,
      rate_limit_7d: 0,
    }, undefined)
    expect(wrapper.text()).toContain('sk-created-once')
    expect(wrapper.text()).toContain('keys.secretOnce.warning')
    wrapper.unmount()
  })

  it('refuses the CC-Switch import once the one-time secret is no longer held', async () => {
    const key = { ...createApiKey(), id: 5, key: '****abcd', group: { id: 2, name: 'OpenAI', platform: 'openai' } }
    listKeys.mockResolvedValueOnce({ items: [key], total: 1, page: 1, page_size: 20, pages: 1 })
    const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null)
    const wrapper = await mountView()

    await getButtonByText(wrapper, 'Import to CC Switch').trigger('click')
    await flushPromises()

    expect(openSpy).not.toHaveBeenCalled()
    expect(showInfo).toHaveBeenCalledWith('secret only shown once; rotate and import right away')
    expect(visibleModels).not.toHaveBeenCalled()
    openSpy.mockRestore()
    wrapper.unmount()
  })

  it('imports a model from the key visible set instead of a hardcoded constant', async () => {
    getAvailableGroups.mockResolvedValue([{ id: 42, name: 'OpenAI' }])
    getPublicSettings.mockResolvedValue({ api_base_url: 'https://gw.example/v1' })
    createKey.mockResolvedValue({
      api_key: { ...createApiKey(), id: 9 },
      secret: 'sk-created-once',
      grace_until: null,
    })
    listKeys.mockResolvedValue({
      items: [{ ...createApiKey(), id: 9, key: '****abcd', group: { id: 42, name: 'OpenAI', platform: 'openai' } }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    visibleModels.mockResolvedValue({ models: ['text-embedding-3-large', 'gpt-5.5'], fullyEnumerable: true })
    const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null)
    const wrapper = await mountView()

    await getButtonByText(wrapper, 'Create API Key').trigger('click')
    await flushPromises()
    await wrapper.get('[data-tour="key-form-name"]').setValue('new key')
    const groupSelect = wrapper
      .findAllComponents({ name: 'Select' })
      .find((select) => (select.element as Element).getAttribute('data-tour') === 'key-form-group')!
    groupSelect.vm.$emit('update:modelValue', 42)
    await flushPromises()
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()

    await getButtonByText(wrapper, 'Import to CC Switch').trigger('click')
    await flushPromises()

    expect(visibleModels).toHaveBeenCalledWith(9)
    const deeplink = String(openSpy.mock.calls[0][0])
    expect(deeplink).toContain('ccswitch://v1/import')
    expect(deeplink).toContain('model=gpt-5.5')
    expect(deeplink).toContain('apiKey=sk-created-once')
    // The list only ever carries a mask, and the mask must never become a client credential.
    expect(deeplink).not.toContain('****abcd')
    openSpy.mockRestore()
    wrapper.unmount()
  })

  it('never renders a plaintext credential in the key column', async () => {
    listKeys.mockResolvedValueOnce({
      items: [{ ...createApiKey(), key: '****abcd' }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    const wrapper = await mountView()

    const table = wrapper.findComponent({ name: 'DataTable' })
    expect(table.props('data')[0].key).toBe('****abcd')
    expect(wrapper.html()).not.toContain('maskApiKey')
    wrapper.unmount()
  })

  it('keeps filters and selected page size when sorting by current concurrency', async () => {
    getAvailableGroups.mockResolvedValue([{ id: 42, name: 'OpenAI' }])
    const wrapper = await mountView()

    await wrapper.get('[data-test="page-size-50"]').trigger('click')
    await flushPromises()

    await wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('update:modelValue', 'target')
    await wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('search')
    await flushPromises()

    const selects = wrapper.findAllComponents({ name: 'Select' })
    await selects[0].vm.$emit('update:modelValue', 42)
    await flushPromises()
    await selects[1].vm.$emit('update:modelValue', 'active')
    await flushPromises()

    listKeys.mockClear()

    await wrapper.get('[data-test="sort-current-concurrency"]').trigger('click')
    await flushPromises()

    expect(listKeys).toHaveBeenLastCalledWith(
      1,
      50,
      {
        search: 'target',
        status: 'active',
        group_id: 42,
        sort_by: 'current_concurrency',
        sort_order: 'asc',
      },
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
  })

  it('names the period budget separately from RPM/TPM and keeps the legacy 5h/1d/7d anchors visible', async () => {
    listKeys.mockResolvedValueOnce({
      items: [
        {
          ...createApiKey(),
          budget_period_mode: 'duration',
          budget_limit: 25,
          budget_used: 5,
          budget_remaining: 20,
          budget_period_display: '7d rolling window',
          budget_legacy_anchor: '7d',
          budget_duration_seconds: 604800,
          budget_period_end: '2026-07-04T00:00:00Z',
        },
      ],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    const wrapper = await mountView()

    const budgetCell = wrapper.get('[data-test="rate-limit-cell"]').text()
    expect(budgetCell).toContain('keys.budgetColumn')
    expect(budgetCell).toContain('7d')
    expect(budgetCell).toContain('$5.00')
    expect(budgetCell).toContain('$25.00')
    // The USD period budget is not a rate limit and must not be labelled as one.
    expect(budgetCell).not.toContain('keys.rateLimit5h')
    wrapper.unmount()
  })

  it('submits a custom duration period budget and offers the fixed calendar periods', async () => {
    getAvailableGroups.mockResolvedValue([{ id: 42, name: 'OpenAI' }])
    createKey.mockResolvedValue({
      api_key: { ...createApiKey(), id: 11 },
      secret: 'sk-budget-once',
      grace_until: null,
    })
    const wrapper = await mountView()

    await getButtonByText(wrapper, 'Create API Key').trigger('click')
    await flushPromises()
    await wrapper.get('[data-tour="key-form-name"]').setValue('budgeted key')
    const groupSelect = wrapper
      .findAllComponents({ name: 'Select' })
      .find((select) => (select.element as Element).getAttribute('data-tour') === 'key-form-group')!
    groupSelect.vm.$emit('update:modelValue', 42)
    await flushPromises()

    await wrapper.get('[data-test="key-form-budget-toggle"]').trigger('click')
    await flushPromises()

    // The compatible 5h/1d/7d anchors stay selectable next to custom and fixed periods.
    const periodOptions = wrapper
      .get('[data-test="key-form-budget-period"]')
      .findAll('option')
      .map((option) => option.attributes('value'))
    expect(periodOptions).toEqual([
      'legacy_5h',
      'legacy_1d',
      'legacy_7d',
      'duration',
      'calendar_day',
      'calendar_week',
      'calendar_month',
    ])

    await wrapper.get('[data-test="key-form-budget-limit"]').setValue('12')
    await wrapper.get('[data-test="key-form-budget-period"]').setValue('duration')
    await wrapper.get('[data-test="key-form-budget-duration-value"]').setValue('90')
    await flushPromises()

    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    const createArgs = createKey.mock.calls.at(-1)!
    expect(createArgs[8]).toMatchObject({
      budget_period_mode: 'duration',
      budget_duration_seconds: 90 * 3600,
      budget_anchor: 'first_use',
      budget_limit: 12,
    })
    wrapper.unmount()
  })

  it('does not offer self-service reset of consumed usage', async () => {
    listKeys.mockResolvedValueOnce({
      items: [
        {
          ...createApiKey(),
          quota: 10,
          quota_used: 4,
          rate_limit_5h: 5,
          usage_5h: 1,
        },
      ],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    const wrapper = await mountView()
    await getButtonByText(wrapper, 'common.edit').trigger('click')
    await flushPromises()

    const resetButtons = wrapper
      .findAll('button')
      .filter((button) => /reset/i.test(button.text()) || button.text().includes('keys.resetUsage'))
    expect(resetButtons).toHaveLength(0)
    expect(wrapper.text()).toContain('keys.usageSelfServiceHint')
    wrapper.unmount()
  })

})
