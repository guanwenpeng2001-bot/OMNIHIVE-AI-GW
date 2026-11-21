import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'

import KeyUsageView from '../KeyUsageView.vue'

const { showInfo, showSuccess, showError, fetchPublicSettings, authUser, listKeys, getKeyById, getDailyUsage, getBatchUsage } = vi.hoisted(() => ({
  showInfo: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn(),
  fetchPublicSettings: vi.fn(),
  // Signed-out by default so the existing paste-a-key flow stays the subject of these tests.
  authUser: { value: null as { id: number; role: string } | null },
  listKeys: vi.fn(),
  getKeyById: vi.fn(),
  getDailyUsage: vi.fn(),
  getBatchUsage: vi.fn(),
}))

const messages: Record<string, string> = {
  'keyUsage.title': 'API Key Usage',
  'keyUsage.subtitle': 'Usage status',
  'keyUsage.placeholder': 'sk-test',
  'keyUsage.query': 'Query',
  'keyUsage.querying': 'Querying...',
  'keyUsage.privacyNote': 'Privacy note',
  'keyUsage.dateRange': 'Date Range:',
  'keyUsage.dateRangeToday': 'Today',
  'keyUsage.dateRange7d': '7 Days',
  'keyUsage.dateRange30d': '30 Days',
  'keyUsage.dateRange90d': '90 Days',
  'keyUsage.dateRangeCustom': 'Custom',
  'keyUsage.apply': 'Apply',
  'keyUsage.used': 'Used',
  'keyUsage.detailInfo': 'Detail Information',
  'keyUsage.tokenStats': 'Token Statistics',
  'keyUsage.dailyDetail': 'Daily Detail',
  'keyUsage.date': 'Date',
  'keyUsage.requests': 'Requests',
  'keyUsage.inputTokens': 'Input Tokens',
  'keyUsage.outputTokens': 'Output Tokens',
  'keyUsage.cacheReadTokens': 'Cache Read',
  'keyUsage.cacheWriteTokens': 'Cache Write',
  'keyUsage.cost': 'Cost',
  'keyUsage.quotaMode': 'Key Quota Mode',
  'keyUsage.walletBalance': 'Wallet Balance',
  'keyUsage.totalQuota': 'Total Quota',
  'keyUsage.limit5h': '5-Hour Limit',
  'keyUsage.limitDaily': 'Daily Limit',
  'keyUsage.limit7d': '7-Day Limit',
  'keyUsage.limitWeekly': 'Weekly Limit',
  'keyUsage.limitMonthly': 'Monthly Limit',
  'keyUsage.remainingQuota': 'Remaining Quota',
  'keyUsage.usedQuota': 'Used Quota',
  'keyUsage.subscriptionType': 'Subscription Type',
  'keyUsage.billingType': 'Billing Type',
  'keyUsage.todayRequests': 'Today Requests',
  'keyUsage.todayInputTokens': 'Today Input',
  'keyUsage.todayOutputTokens': 'Today Output',
  'keyUsage.todayTokens': 'Today Tokens',
  'keyUsage.todayCacheCreation': 'Today Cache Creation',
  'keyUsage.todayCacheRead': 'Today Cache Read',
  'keyUsage.todayCost': 'Today Cost',
  'keyUsage.rpmTpm': 'RPM / TPM',
  'keyUsage.totalRequests': 'Total Requests',
  'keyUsage.totalInputTokens': 'Total Input',
  'keyUsage.totalOutputTokens': 'Total Output',
  'keyUsage.totalTokensLabel': 'Total Tokens',
  'keyUsage.totalCacheCreation': 'Total Cache Creation',
  'keyUsage.totalCacheRead': 'Total Cache Read',
  'keyUsage.totalCost': 'Total Cost',
  'keyUsage.avgDuration': 'Avg Duration',
  'keyUsage.querySuccess': 'Query successful',
  'keyUsage.queryFailed': 'Query failed',
  'keyUsage.queryFailedRetry': 'Query failed, please try again later',
  'keyUsage.secretQuery.title': 'External key query (paste the full key)',
  'keyUsage.sessionQuery.title': 'Query by key ID (signed-in session)',
  'keyUsage.sessionQuery.description': 'Pick one of your own keys to read its quota, rate limits and daily usage without pasting a secret.',
  'keyUsage.sessionQuery.keyLabel': 'My keys',
  'keyUsage.sessionQuery.placeholder': 'Select a key',
  'keyUsage.sessionQuery.noKeys': 'This account has no keys to query.',
  'keyUsage.sessionQuery.noGroup': 'No group',
  'keyUsage.sessionQuery.signedOutHint': 'Sign in to select your own key and query it by ID without pasting a secret.',
  'keyUsage.panelModeNotice': 'These results come from the panel session and show what the panel knows; per-request token detail is not displayed here.',
  'home.viewDocs': 'Docs',
  'home.switchToLight': 'Light',
  'home.switchToDark': 'Dark',
  'home.footer.allRightsReserved': 'All rights reserved.',
}

vi.mock('@/api/keys', () => ({
  keysAPI: {
    list: listKeys,
    getById: getKeyById,
  },
}))

vi.mock('@/api/usage', () => ({
  getMyApiKeyDailyUsage: getDailyUsage,
  getDashboardApiKeysUsage: getBatchUsage,
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => (messages[key] ?? key) + (params ? ':' + JSON.stringify(params) : ''),
      locale: { value: 'en' },
    }),
  }
})

const appStoreState = vi.hoisted(() => ({
  cachedPublicSettings: null as Record<string, unknown> | null,
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => ({
    get user() {
      return authUser.value
    },
  }),
  useAppStore: () => ({
    get cachedPublicSettings() {
      return appStoreState.cachedPublicSettings
    },
    siteName: 'Sub2API',
    siteLogo: '',
    docUrl: '',
    publicSettingsLoaded: true,
    fetchPublicSettings,
    showInfo,
    showSuccess,
    showError,
  }),
}))

describe('KeyUsageView daily detail', () => {
  beforeEach(() => {
    showInfo.mockReset()
    showSuccess.mockReset()
    showError.mockReset()
    fetchPublicSettings.mockReset()
    localStorage.clear()

    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      value: vi.fn().mockReturnValue({ matches: false }),
    })
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => window.setTimeout(() => cb(0), 0))
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        mode: 'quota_limited',
        isValid: true,
        status: 'active',
        quota: {
          limit: 10,
          used: 1,
          remaining: 9,
          unit: 'USD',
        },
        usage: {
          today: {
            requests: 1,
            input_tokens: 10,
            output_tokens: 20,
            cache_creation_tokens: 0,
            cache_read_tokens: 0,
            total_tokens: 30,
            actual_cost: 0.01,
          },
          total: {
            requests: 12,
            input_tokens: 100,
            output_tokens: 200,
            cache_creation_tokens: 10,
            cache_read_tokens: 30,
            total_tokens: 340,
            actual_cost: 0.12,
          },
          rpm: 0,
          tpm: 0,
        },
        daily_usage: [
          {
            date: '2026-05-19',
            requests: 12,
            input_tokens: 100,
            output_tokens: 200,
            cache_read_tokens: 30,
            cache_write_tokens: 10,
            total_tokens: 340,
            cost: 0.15,
            actual_cost: 0.12,
          },
        ],
      }),
    }))
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it.each(['all_time', 'retained', 'range', 'unknown', 'legacy'] as const)('shows the %s scope for key totals', async scope => {
    const response = await vi.mocked(fetch)('/fixture')
    const payload = await response.json()
    payload.usage.total.requests = 987654
    payload.usage.average_duration_ms = 0
    payload.usage.coverage = scope === 'legacy' ? undefined : {
      complete: scope === 'all_time', retained_complete: scope === 'retained', range_complete: scope === 'range',
      window_from: '2026-09-11T16:00:00Z', window_until: '2026-10-02T05:00:00Z', timezone: 'Asia/Shanghai',
      reason: 'history_before_coverage_floor_unverified'
    }
    vi.mocked(fetch).mockResolvedValue({ ok: true, json: async () => payload } as Response)
    const wrapper = mount(KeyUsageView, { global: { stubs: { RouterLink: { template: '<a><slot /></a>' }, LocaleSwitcher: true, Icon: true } } })
    await wrapper.find('input').setValue('sk-test-key')
    await wrapper.find('input').trigger('keydown.enter')
    await flushPromises()
    const text = wrapper.text()
    expect(text).not.toContain('history_before_coverage_floor_unverified')
    if (scope === 'unknown' || scope === 'legacy') {
      expect(text).toContain('keyUsage.scopeUnknown'); expect(text).not.toContain('987,654')
    } else {
      expect(text).toContain('987,654'); expect(text).toContain('0 ms')
      if (scope !== 'all_time') {
        expect(text).toContain(scope === 'range' ? 'keyUsage.scopeRange' : 'keyUsage.scopeRetained')
        expect(text).not.toContain('Total Requests')
        expect(text).toContain('2026-09-12 00:00')
        expect(text).toContain('2026-10-02 13:00')
        expect(text).not.toContain('T16:00:00Z')
      } else expect(text).toContain('Total Requests')
    }
    wrapper.unmount()
  })

  it('keeps pending and failed key queries separate from unknown coverage', async () => {
    let rejectQuery!: (error: Error) => void
    vi.mocked(fetch).mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectQuery = reject }))
    const wrapper = mount(KeyUsageView, { global: { stubs: { RouterLink: { template: '<a><slot /></a>' }, LocaleSwitcher: true, Icon: true } } })
    await wrapper.find('input').setValue('sk-test-key')
    await wrapper.find('input').trigger('keydown.enter')
    expect(wrapper.find('.skeleton').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('keyUsage.scopeUnknown')
    rejectQuery(new Error('Synthetic query failure'))
    await flushPromises()
    expect(wrapper.find('.skeleton').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('keyUsage.scopeUnknown')
    expect(showError).toHaveBeenCalledWith('Synthetic query failure')
    wrapper.unmount()
  })

  it('renders daily usage detail rows after a successful query', async () => {
    const wrapper = mount(KeyUsageView, {
      global: {
        stubs: {
          RouterLink: { template: '<a><slot /></a>' },
          LocaleSwitcher: true,
          Icon: true,
        },
      },
    })

    await wrapper.find('input').setValue('sk-test-key')
    await wrapper.find('input').trigger('keydown.enter')
    await flushPromises()
    await nextTick()

    const fetchMock = vi.mocked(fetch)
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/v1/usage?'),
      expect.objectContaining({
        headers: { Authorization: 'Bearer sk-test-key' },
      })
    )
    expect(String(fetchMock.mock.calls[0][0])).toContain('days=30')

    const text = wrapper.text()
    expect(text).toContain('Daily Detail')
    expect(text).toContain('Date')
    expect(text).toContain('Cache Read')
    expect(text).toContain('Cache Write')
    expect(text).toContain('2026-05-19')
    expect(text).toContain('12')
    expect(text).toContain('100')
    expect(text).toContain('200')
    expect(text).toContain('30')
    expect(text).toContain('10')
    expect(text).toContain('$0.12')

    wrapper.unmount()
  })

  it('queries the current local calendar date near midnight', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(2026, 6, 13, 0, 30))

    const wrapper = mount(KeyUsageView, {
      global: {
        stubs: {
          RouterLink: { template: '<a><slot /></a>' },
          LocaleSwitcher: true,
          Icon: true,
        },
      },
    })

    await wrapper.find('input').setValue('sk-test-key')
    await wrapper.find('input').trigger('keydown.enter')
    await flushPromises()

    const requestUrl = String(vi.mocked(fetch).mock.calls[0][0])
    expect(requestUrl).toContain('start_date=2026-07-13')
    expect(requestUrl).toContain('end_date=2026-07-13')

    wrapper.unmount()
  })
})

describe('KeyUsageView signed-in session query', () => {
  beforeEach(() => {
    showInfo.mockReset()
    showSuccess.mockReset()
    showError.mockReset()
    fetchPublicSettings.mockReset()
    listKeys.mockReset()
    getKeyById.mockReset()
    getDailyUsage.mockReset()
    getBatchUsage.mockReset()
    localStorage.clear()
    authUser.value = { id: 7, role: 'user' }

    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      value: vi.fn().mockReturnValue({ matches: false }),
    })
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => window.setTimeout(() => cb(0), 0))
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    authUser.value = null
    vi.unstubAllGlobals()
  })

  const mountView = () => mount(KeyUsageView, {
    global: {
      stubs: { RouterLink: { template: '<a><slot /></a>' }, LocaleSwitcher: true, Icon: true },
    },
  })

  it('queries the chosen key by ID through the panel without asking for a secret', async () => {
    listKeys.mockResolvedValue({ items: [{ id: 42, name: 'laptop', group: { name: 'Text' }, status: 'active', group_id: 2 }], total: 1 })
    getKeyById.mockResolvedValue({
      id: 42,
      name: 'laptop',
      status: 'active',
      quota: 10,
      quota_used: 2,
      expires_at: null,
      rate_limit_5h: 5,
      usage_5h: 1,
      window_5h_start: null,
      reset_5h_at: null,
      rate_limit_1d: 0,
      usage_1d: 0,
      window_1d_start: null,
      reset_1d_at: null,
      rate_limit_7d: 0,
      usage_7d: 0,
      window_7d_start: null,
      reset_7d_at: null,
    })
    getDailyUsage.mockResolvedValue({
      items: [{
        date: '2026-05-19', requests: 3, input_tokens: 10, output_tokens: 20,
        cache_read_tokens: 0, cache_write_tokens: 1, total_tokens: 31, cost: 0.05, actual_cost: 0.04,
      }],
      days: 30, start_date: '2026-04-20', end_date: '2026-05-19',
    })
    getBatchUsage.mockResolvedValue({ stats: { 42: { api_key_id: 42, today_actual_cost: 0.01, total_actual_cost: 0.25 } } })

    const wrapper = mountView()
    await flushPromises()

    // The key list only carries masked credentials and drives a selector, not a paste box.
    expect(listKeys).toHaveBeenCalledTimes(1)
    await wrapper.get('select').setValue(42)
    await wrapper.findAll('button').find(button => button.text() === 'Query')!.trigger('click')
    await flushPromises()

    expect(getKeyById).toHaveBeenCalledWith(42)
    expect(getDailyUsage).toHaveBeenCalledWith(42, 30)
    expect(getBatchUsage).toHaveBeenCalledWith([42])
    expect(vi.mocked(fetch)).not.toHaveBeenCalled()

    const text = wrapper.text()
    expect(text).toContain('panel session')
    expect(text).toContain('Today Cost')
    expect(text).toContain('$0.01')
    expect(text).toContain('Total Cost')
    expect(text).toContain('$0.25')
    expect(text).toContain('2026-05-19')

    wrapper.unmount()
  })

  it('tells signed-out visitors they can query by ID after signing in', async () => {
    authUser.value = null
    const wrapper = mountView()
    expect(wrapper.text()).toContain('Sign in to select your own key')
    expect(wrapper.find('select').exists()).toBe(false)
    wrapper.unmount()
  })
})

describe('KeyUsageView subscription feature flag', () => {
  beforeEach(() => {
    localStorage.clear()
    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      value: vi.fn().mockReturnValue({ matches: false }),
    })
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => window.setTimeout(() => cb(0), 0))
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        mode: 'wallet',
        isValid: true,
        status: 'active',
        balance: 5.5,
        usage: {
          today: { requests: 0, input_tokens: 0, output_tokens: 0, cache_creation_tokens: 0, cache_read_tokens: 0, total_tokens: 0, actual_cost: 0 },
          total: { requests: 0, input_tokens: 0, output_tokens: 0, cache_creation_tokens: 0, cache_read_tokens: 0, total_tokens: 0, actual_cost: 0 },
          rpm: 0,
          tpm: 0,
        },
        daily_usage: [],
      }),
    }))
  })

  afterEach(() => {
    appStoreState.cachedPublicSettings = null
    vi.unstubAllGlobals()
  })

  async function mountAndQuery() {
    const wrapper = mount(KeyUsageView, {
      global: {
        stubs: {
          RouterLink: { template: '<a><slot /></a>' },
          LocaleSwitcher: true,
          Icon: true,
        },
      },
    })
    await wrapper.find('input').setValue('sk-test-key')
    await wrapper.find('input').trigger('keydown.enter')
    await flushPromises()
    await nextTick()
    return wrapper
  }

  it('labels the wallet row "Subscription Type" while subscriptions are enabled', async () => {
    const wrapper = await mountAndQuery()

    expect(wrapper.text()).toContain('Subscription Type')
    expect(wrapper.text()).toContain('Wallet Balance')
    wrapper.unmount()
  })

  it('drops the "Subscription" wording from the wallet row when subscriptions are disabled', async () => {
    appStoreState.cachedPublicSettings = { subscription_enabled: false }
    const wrapper = await mountAndQuery()

    expect(wrapper.text()).toContain('Billing Type')
    expect(wrapper.text()).not.toContain('Subscription Type')
    expect(wrapper.text()).toContain('Wallet Balance')
    wrapper.unmount()
  })

  it('gives every button on the public key-usage page an accessible name [E1-10]', async () => {
    const wrapper = await mountAndQuery()
    const unnamedButtons = wrapper.findAll('button').filter((btn) => {
      const text = btn.text().trim()
      const ariaLabel = btn.attributes('aria-label')?.trim()
      const title = btn.attributes('title')?.trim()
      return !text && !ariaLabel && !title
    })
    expect(unnamedButtons).toHaveLength(0)
    wrapper.unmount()
  })
})
