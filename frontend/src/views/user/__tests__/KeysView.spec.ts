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
  showSuccess,
  copyToClipboard,
  isCurrentStep,
  nextStep,
} = vi.hoisted(() => ({
  listKeys: vi.fn(),
  updateKey: vi.fn(),
  getPublicSettings: vi.fn(),
  getDashboardApiKeysUsage: vi.fn(),
  getAvailableGroups: vi.fn(),
  getUserGroupRates: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  copyToClipboard: vi.fn(),
  isCurrentStep: vi.fn(),
  nextStep: vi.fn(),
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
  'keys.status.expired': 'Expired',
  'keys.status.inactive': 'Inactive',
  'keys.status.quota_exhausted': 'Quota exhausted',
  'keys.usage': 'Usage',
}

vi.mock('@/api', () => ({
  keysAPI: {
    list: listKeys,
    create: vi.fn(),
    update: updateKey,
    delete: vi.fn(),
    toggleStatus: vi.fn(),
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
    showSuccess,
  }),
}))

vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({
    isCurrentStep,
    nextStep,
  }),
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
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
        : messages[key] ?? key,
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
      </div>
      <slot name="empty" />
    </div>
  `,
}

const SelectStub = {
  name: 'Select',
  props: ['modelValue', 'options'],
  emits: ['update:modelValue'],
  template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"></select>',
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

describe('user KeysView column settings', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.restoreAllMocks()
    credentialLocale = 'en'

    listKeys.mockReset()
    updateKey.mockReset()
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
    expect(credentials.findAll('code').map((address) => address.text())).toEqual([
      `${window.location.origin}/v1`,
      `${window.location.origin}/v1/messages`
    ])
    const copyButtons = credentials.findAll('button')
    for (const [index, protocol] of [translations.openai, translations.anthropic].entries()) {
      expect(copyButtons[index].attributes('aria-label')).toBe(
        translations.copyAddress.replace('{protocol}', protocol)
      )
      await copyButtons[index].trigger('click')
    }
    expect(copyToClipboard).toHaveBeenNthCalledWith(1, `${window.location.origin}/v1`, translations.copied)
    expect(copyToClipboard).toHaveBeenNthCalledWith(2, `${window.location.origin}/v1/messages`, translations.copied)
    expect(wrapper.findComponent({ name: 'DataTable' }).exists()).toBe(true)
    wrapper.unmount()
  })

  it('preserves the gateway origin returned by the environment URL helper', async () => {
    const buildGatewayUrl = vi.spyOn(gatewayUrl, 'buildGatewayUrl')
      .mockImplementation((path) => `https://gateway.example:8443${path}`)
    const wrapper = await mountView()
    expect(wrapper.findAll('section code').map((address) => address.text())).toEqual([
      'https://gateway.example:8443/v1',
      'https://gateway.example:8443/v1/messages'
    ])
    expect(buildGatewayUrl).toHaveBeenCalledWith('/v1')
    expect(buildGatewayUrl).toHaveBeenCalledWith('/v1/messages')
    wrapper.unmount()
  })

  it('resolves relative gateway subpaths against the current site origin', async () => {
    vi.spyOn(gatewayUrl, 'buildGatewayUrl').mockImplementation((path) => `/proxy/sub2api${path}`)
    const wrapper = await mountView()
    expect(wrapper.get('section code').text()).toBe(`${window.location.origin}/proxy/sub2api/v1`)
    await wrapper.get('section button').trigger('click')
    expect(copyToClipboard).toHaveBeenCalledWith(`${window.location.origin}/proxy/sub2api/v1`, englishDashboard.keys.payAsYouGo.copied)
    wrapper.unmount()
  })

  it.each([
    { initialStatus: 'quota_exhausted', status: 'active', formStatus: 'active' },
    { initialStatus: 'inactive', status: 'inactive', formStatus: 'inactive' },
    { initialStatus: 'active', status: 'active', formStatus: 'inactive' },
  ] as const)('syncs quota reset from $initialStatus to $status with form status $formStatus', async ({ initialStatus, status, formStatus }) => {
    const key: ApiKey = {
      ...createApiKey(), group_id: 1, quota: 10, quota_used: 10,
      status: initialStatus,
    }
    listKeys.mockResolvedValueOnce({ items: [key], total: 1, page: 1, page_size: 20, pages: 1 })
    updateKey.mockResolvedValue({ ...key, status, quota_used: 0 })
    const wrapper = await mountView()
    await getButtonByText(wrapper, 'common.edit').trigger('click')
    await wrapper.get('[data-tour="key-form-name"]').setValue('Unsaved name')
    const statusSelect = wrapper.findAllComponents({ name: 'Select' })
      .find((select) => select.props('options').length === 2 &&
        select.props('options')[0].value === 'active')!
    statusSelect.vm.$emit('update:modelValue', 'inactive')
    await wrapper.get('button[title="keys.resetQuotaUsed"]').trigger('click')
    const confirmation = wrapper.findAllComponents({ name: 'ConfirmDialog' })
      .find((dialog) => dialog.props('title') === 'keys.resetQuotaTitle')!
    confirmation.vm.$emit('confirm')
    await flushPromises()

    expect(updateKey).toHaveBeenNthCalledWith(1, key.id, { reset_quota: true })
    expect(wrapper.findComponent({ name: 'DataTable' }).props('data')[0])
      .toMatchObject({ status, quota_used: 0 })
    expect(statusSelect.props('modelValue')).toBe(formStatus)
    expect((wrapper.get('[data-tour="key-form-name"]').element as HTMLInputElement).value)
      .toBe('Unsaved name')

    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    expect(updateKey).toHaveBeenNthCalledWith(2, key.id, expect.objectContaining({ name: 'Unsaved name', status: formStatus }))
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
})
