import UsageStatsCards from '@/components/admin/usage/UsageStatsCards.vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import UsageView from '../UsageView.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'

const {
  query,
  getStats,
  getDashboardModels,
  getDashboardSnapshotV2,
  listMyErrorRequests,
  list,
  getAvailable,
  showError,
  showWarning,
  showSuccess,
  showInfo,
  downloadUsageCsv,
  fetchUsageExportRows,
} = vi.hoisted(() => ({
  query: vi.fn(),
  getStats: vi.fn(),
  getDashboardModels: vi.fn(),
  getDashboardSnapshotV2: vi.fn(),
  listMyErrorRequests: vi.fn(),
  list: vi.fn(),
  getAvailable: vi.fn(),
  showError: vi.fn(),
  showWarning: vi.fn(),
  showSuccess: vi.fn(),
  showInfo: vi.fn(),
  downloadUsageCsv: vi.fn(),
  fetchUsageExportRows: vi.fn(),
}))

const messages: Record<string, string> = {
  'admin.dashboard.timeRange': 'Time range',
  'admin.dashboard.granularity': 'Granularity',
  'admin.dashboard.day': 'Day',
  'admin.dashboard.hour': 'Hour',
  'admin.users.columnSettings': 'Columns',
  'admin.usage.group': 'Group',
  'admin.usage.billingType': 'Billing type',
  'admin.usage.billingMode': 'Billing mode',
  'admin.usage.allTypes': 'All types',
  'admin.usage.allBillingTypes': 'All billing types',
  'admin.usage.billingTypeBalance': 'Balance',
  'admin.usage.billingTypeSubscription': 'Subscription',
  'admin.usage.allBillingModes': 'All billing modes',
  'admin.usage.billingModeToken': 'Token',
  'admin.usage.billingModePerRequest': 'Per request',
  'admin.usage.billingModeImage': 'Image',
  'admin.usage.allGroups': 'All groups',
  'admin.usage.allModels': 'All models',
  'usage.allApiKeys': 'All API Keys',
  'usage.errors.allKeys': 'All API Keys',
  'usage.tabs.usage': 'Usage records',
  'usage.tabs.errors': 'Error records',
  'usage.apiKeyFilter': 'API Key',
  'usage.model': 'Model',
  'usage.type': 'Type',
  'usage.ws': 'WS',
  'usage.stream': 'Stream',
  'usage.sync': 'Sync',
  'usage.compactionFilter': 'Request Kind',
  'usage.allCompactionTypes': 'All Requests',
  'usage.compactionOnly': 'Compaction Only',
  'usage.exporting': 'Exporting',
  'usage.exportCsv': 'Export CSV',
  'usage.failedToLoad': 'Failed to load',
  'usage.noDataToExport': 'No data',
  'usage.preparingExport': 'Preparing export',
  'usage.exportSuccess': 'Export success',
  'usage.exportFailed': 'Export failed',
  'usage.exportHistoryIncomplete': 'Usage history is incomplete',
  'dashboard.rawRetainedFrom': 'Raw records retained from: {time}',
  'common.refresh': 'Refresh',
  'common.reset': 'Reset',
}

// 导出统一走服务端 CSV 流：浏览器只发一次请求，不再按页循环拉取。
vi.mock('@/utils/usageExport', () => ({
  ADMIN_USAGE_EXPORT_ENDPOINT: '/admin/usage/export',
  USER_USAGE_EXPORT_ENDPOINT: '/usage/export',
  downloadUsageCsv,
  fetchUsageExportRows,
  usageExportFileName: (start: string, end: string, extension: string) =>
    `usage_${start}_to_${end}.${extension}`,
}))

vi.mock('@/api', () => ({
  usageAPI: {
    query,
    getStats,
    getDashboardModels,
    getDashboardSnapshotV2,
    listMyErrorRequests,
  },
  keysAPI: {
    list,
  },
  userGroupsAPI: {
    getAvailable,
  },
}))

const appStoreState = vi.hoisted(() => ({
  cachedPublicSettings: { allow_user_view_error_requests: true } as Record<string, unknown>,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError, showWarning, showSuccess, showInfo,
    get cachedPublicSettings() {
      return appStoreState.cachedPublicSettings
    },
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

const simpleStub = { template: '<div><slot /></div>' }
const chartStub = { template: '<div />' }

const usageLog = {
  id: 1,
  request_id: 'req-user-export',
  actual_cost: 0.092883,
  total_cost: 0.092883,
  rate_multiplier: 1,
  service_tier: 'priority',
  input_cost: 0.020285,
  output_cost: 0.00303,
  cache_creation_cost: 0.000001,
  cache_read_cost: 0.069568,
  input_tokens: 4057,
  output_tokens: 101,
  cache_creation_tokens: 4,
  cache_read_tokens: 278272,
  cache_creation_5m_tokens: 0,
  cache_creation_1h_tokens: 0,
  image_count: 0,
  image_size: null,
  first_token_ms: 12,
  duration_ms: 345,
  created_at: '2026-03-08T00:00:00Z',
  model: 'gpt-5.4',
  reasoning_effort: null,
  ip_address: '203.0.113.10',
  api_key: { name: 'demo-key' },
  billing_mode: 'token',
  request_type: 'sync',
  stream: false,
  native_compaction_v2: false,
}

function mountUsageView(timeZone?: string) {
  return mount(UsageView, {
    props: { timeZone },
    global: {
      stubs: {
        AppLayout: simpleStub,
        Pagination: true,
        Select: true,
        DateRangePicker: true,
        Icon: true,
        UsageStatsCards: false,
        UsageTable: chartStub,
        UserErrorRequestsTable: chartStub,
        ModelDistributionChart: chartStub,
        GroupDistributionChart: chartStub,
        EndpointDistributionChart: chartStub,
        TokenUsageTrend: chartStub,
      },
    },
  })
}

describe('user UsageView', () => {
  beforeEach(() => {
    query.mockReset()
    getStats.mockReset()
    getDashboardModels.mockReset()
    getDashboardSnapshotV2.mockReset()
    listMyErrorRequests.mockReset()
    list.mockReset()
    getAvailable.mockReset()
    showError.mockReset()
    showWarning.mockReset()
    showSuccess.mockReset()
    showInfo.mockReset()

    query.mockResolvedValue({ items: [usageLog], total: 1, pages: 1 })
    getStats.mockResolvedValue({
      total_requests: 1,
      total_input_tokens: 10,
      total_output_tokens: 20,
      total_cache_tokens: 0,
      total_tokens: 30,
      total_cost: 0.1,
      total_actual_cost: 0.08,
      average_duration_ms: 12,
      endpoints: [],
      upstream_endpoints: [],
      endpoint_paths: [],
    })
    getDashboardModels.mockResolvedValue({
      models: [{ model: 'gpt-5.4', requests: 1, input_tokens: 10, output_tokens: 20, cache_creation_tokens: 0, cache_read_tokens: 0, total_tokens: 30, cost: 0.1, actual_cost: 0.08 }],
      start_date: '2026-03-08',
      end_date: '2026-03-08',
    })
    getDashboardSnapshotV2.mockResolvedValue({
      generated_at: '2026-03-08T00:00:00Z',
      start_date: '2026-03-08',
      end_date: '2026-03-08',
      granularity: 'hour',
      trend: [],
      groups: [],
    })
    listMyErrorRequests.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 0 })
    list.mockResolvedValue({ items: [{ id: 1, name: 'demo-key' }], total: 1, page: 1, page_size: 100, pages: 1 })
    getAvailable.mockResolvedValue([{ id: 1, name: 'default' }])
  })

  it('renders unknown on first stats failure and recovers to known zero', async () => {
    getStats.mockRejectedValueOnce({ status: 409, reason: 'USAGE_HISTORY_INCOMPLETE' })
    const wrapper = mountUsageView()

    await flushPromises()
    const cards = wrapper.getComponent(UsageStatsCards)
    expect(cards.text()).toContain('usage.statsUnknown')
    expect(cards.text()).not.toContain('0ms')
    getStats.mockResolvedValueOnce({ total_requests: 0 })
    await (wrapper.vm as any).loadStats()
    expect(cards.text()).not.toContain('usage.statsUnknown')
    expect(cards.text()).toContain('0')
    wrapper.unmount()
  })

  it('clears prior range stats and ignores obsolete success and failure responses', async () => {
    getStats.mockResolvedValueOnce({ total_requests: 777 })
    const wrapper = mountUsageView()

    await flushPromises()
    const page = wrapper.vm as any
    const cards = wrapper.getComponent(UsageStatsCards)
    expect(cards.text()).toContain('777')
    let resolveOld!: (value: unknown) => void
    getStats.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
    const old = page.loadStats()
    await flushPromises()
    expect(cards.text()).not.toContain('777')
    expect(cards.text()).toContain('common.loading')
    getStats.mockRejectedValueOnce({ status: 409, reason: 'USAGE_HISTORY_INCOMPLETE' })
    await page.loadStats()
    expect(cards.text()).toContain('usage.statsUnknown')
    resolveOld({ total_requests: 777 })
    await old
    expect(cards.text()).not.toContain('777')
    let rejectOld!: (error: unknown) => void
    getStats.mockImplementationOnce(() => new Promise((_, reject) => { rejectOld = reject }))
    const obsolete = page.loadStats()
    getStats.mockResolvedValueOnce({ total_requests: 123 })
    await page.loadStats()
    rejectOld(new Error('old failure'))
    await obsolete
    expect(cards.text()).toContain('123')
    getStats.mockRejectedValueOnce(new Error('network'))
    await page.loadStats()
    expect(cards.text()).toContain('usage.statsUnavailable')
    expect(cards.text()).not.toContain('123')
    wrapper.unmount()
  })

  it('clears old chart data for a history gap and recovers on a valid range', async () => {
    getDashboardSnapshotV2.mockResolvedValueOnce({ trend: [{ date: '2026-03-01', requests: 7 }], groups: [{ name: 'old' }] })
    const wrapper = mountUsageView()
    await flushPromises()
    const page = wrapper.vm as any
    expect(page.trendData).toHaveLength(1)
    getDashboardSnapshotV2.mockRejectedValueOnce({ status: 409, reason: 'USAGE_HISTORY_INCOMPLETE', metadata: { raw_retained_from: '2026-03-05T00:00:00Z' } })
    await page.loadChartData()
    expect(page.trendData).toEqual([])
    expect(page.groupStats).toEqual([])
    expect(wrapper.find('[data-testid="history-gap"]').exists()).toBe(true)
    getDashboardSnapshotV2.mockResolvedValueOnce({ trend: [], groups: [] })
    await page.loadChartData()
    expect(wrapper.find('[data-testid="history-gap"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('ignores an obsolete chart failure after a newer success', async () => {
    const wrapper = mountUsageView()
    await flushPromises()
    const page = wrapper.vm as any
    let rejectOld!: (reason: unknown) => void
    getDashboardSnapshotV2.mockReturnValueOnce(new Promise((_resolve, reject) => { rejectOld = reject }))
    const oldRequest = page.loadChartData()
    getDashboardSnapshotV2.mockResolvedValueOnce({ trend: [{ date: '2026-03-06', requests: 9 }], groups: [] })
    await page.loadChartData()
    rejectOld({ status: 409, reason: 'USAGE_HISTORY_INCOMPLETE' })
    await oldRequest
    expect(page.trendData[0].requests).toBe(9)
    expect(wrapper.find('[data-testid="history-gap"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('loads logs, stats, model stats, and snapshot on first render', async () => {
    mountUsageView()
    await flushPromises()

    expect(query).toHaveBeenCalled()
    expect(getStats).toHaveBeenCalled()
    expect(getDashboardModels).toHaveBeenCalled()
    expect(getDashboardSnapshotV2).toHaveBeenCalledWith(expect.objectContaining({
      include_trend: true,
      include_model_stats: false,
      include_group_stats: true,
    }))
    expect(list).toHaveBeenCalledTimes(1)
    expect(list).toHaveBeenCalledWith(1, 100)
    expect(getAvailable).toHaveBeenCalled()
  })

  it('includes API keys after the first page in both record filters and queries by the selected key', async () => {
    const firstPageKeys = Array.from({ length: 100 }, (_, index) => ({
      id: index + 1,
      name: `key-${index + 1}`,
    }))
    const laterKey = { id: 101, name: 'key-from-second-page' }
    list
      .mockResolvedValueOnce({ items: firstPageKeys, total: 101, page: 1, page_size: 100, pages: 2 })
      .mockResolvedValueOnce({ items: [laterKey], total: 101, page: 2, page_size: 100, pages: 2 })

    const wrapper = mountUsageView()
    await flushPromises()

    expect(list.mock.calls).toEqual([[1, 100], [2, 100]])
    const usageKeySelect = wrapper.findAllComponents(Select).find((select) =>
      select.props('options').some((option: SelectOption) => option.label === 'All API Keys')
    )!
    expect(usageKeySelect.props('options')).toHaveLength(102)
    expect(usageKeySelect.props('options')).toContainEqual({ value: laterKey.id, label: laterKey.name })

    query.mockClear()
    usageKeySelect.vm.$emit('update:modelValue', laterKey.id)
    usageKeySelect.vm.$emit('change', laterKey.id)
    await flushPromises()

    expect(query).toHaveBeenCalledWith(
      expect.objectContaining({ api_key_id: laterKey.id, page: 1 }),
      expect.anything()
    )

    await wrapper.findAll('button').find((button) => button.text() === 'Error records')!.trigger('click')
    await flushPromises()

    const errorKeySelect = wrapper.findAllComponents(Select).find((select) =>
      select.props('options').some((option: SelectOption) => option.label === 'All API Keys')
    )!
    expect(errorKeySelect.props('options')).toHaveLength(102)
    expect(errorKeySelect.props('options')).toContainEqual({ value: laterKey.id, label: laterKey.name })

    listMyErrorRequests.mockClear()
    errorKeySelect.vm.$emit('update:modelValue', laterKey.id)
    errorKeySelect.vm.$emit('change', laterKey.id)
    await flushPromises()

    expect(listMyErrorRequests).toHaveBeenCalledWith(
      expect.objectContaining({ api_key_id: laterKey.id, page: 1 })
    )
    expect(list).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('does not request another API key page when the user has no keys', async () => {
    list.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 100, pages: 0 })

    const wrapper = mountUsageView()
    await flushPromises()

    expect(list.mock.calls).toEqual([[1, 100]])
    const keySelect = wrapper.findAllComponents(Select).find((select) =>
      select.props('options').some((option: SelectOption) => option.label === 'All API Keys')
    )!
    expect(keySelect.props('options')).toEqual([{ value: null, label: 'All API Keys' }])
    expect(getAvailable).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('stops loading API keys when a later page is empty despite an outdated page count', async () => {
    const firstPageKeys = Array.from({ length: 100 }, (_, index) => ({
      id: index + 1,
      name: `key-${index + 1}`,
    }))
    list
      .mockResolvedValueOnce({ items: firstPageKeys, total: 201, page: 1, page_size: 100, pages: 3 })
      .mockResolvedValueOnce({ items: [], total: 201, page: 2, page_size: 100, pages: 3 })

    const wrapper = mountUsageView()
    await flushPromises()

    expect(list.mock.calls).toEqual([[1, 100], [2, 100]])
    const keySelect = wrapper.findAllComponents(Select).find((select) =>
      select.props('options').some((option: SelectOption) => option.label === 'All API Keys')
    )!
    expect(keySelect.props('options')).toHaveLength(101)
    expect(keySelect.props('options')).toContainEqual({ value: 100, label: 'key-100' })
    wrapper.unmount()
  })

  it('propagates and resets the native compaction filter across page requests', async () => {
    const wrapper = mountUsageView()
    await flushPromises()

    // 筛选选项由共享筛选壳渲染（用户页与管理页同一份选项）。
    const compactionSelect = wrapper.findAllComponents(Select).find((select) =>
      select.props('options').some((option: SelectOption) => option.label === 'All Requests')
    )!
    expect(compactionSelect.props('options')).toEqual([
      { value: null, label: 'All Requests' },
      { value: true, label: 'Compaction Only' },
    ])

    query.mockClear()
    getStats.mockClear()
    getDashboardModels.mockClear()
    getDashboardSnapshotV2.mockClear()

    ;(wrapper.vm as any).filters.native_compaction_v2 = true
    ;(wrapper.vm as any).applyFilters()
    await flushPromises()

    expect(query).toHaveBeenCalledWith(
      expect.objectContaining({ native_compaction_v2: true }),
      expect.anything()
    )
    expect(getStats).toHaveBeenCalledWith(expect.objectContaining({ native_compaction_v2: true }))
    expect(getDashboardModels).toHaveBeenCalledWith(expect.objectContaining({ native_compaction_v2: true }))
    expect(getDashboardSnapshotV2).toHaveBeenCalledWith(expect.objectContaining({ native_compaction_v2: true }))

    query.mockClear()
    getStats.mockClear()
    getDashboardModels.mockClear()
    getDashboardSnapshotV2.mockClear()

    ;(wrapper.vm as any).resetFilters()
    await flushPromises()

    expect((wrapper.vm as any).filters.native_compaction_v2).toBeNull()
    expect(query).toHaveBeenCalledWith(
      expect.objectContaining({ native_compaction_v2: null }),
      expect.anything()
    )
    expect(getStats).toHaveBeenCalledWith(expect.objectContaining({ native_compaction_v2: null }))
    expect(getDashboardModels).toHaveBeenCalledWith(expect.objectContaining({ native_compaction_v2: null }))
    expect(getDashboardSnapshotV2).toHaveBeenCalledWith(expect.objectContaining({ native_compaction_v2: null }))
  })

  it('exports the current filters through one server-side CSV request without admin-only columns', async () => {
    downloadUsageCsv.mockReset().mockResolvedValue(undefined)
    const wrapper = mountUsageView()
    await flushPromises()
    ;(wrapper.vm as any).filters.native_compaction_v2 = true

    await (wrapper.vm as any).exportToCSV()

    expect(downloadUsageCsv).toHaveBeenCalledTimes(1)
    const request = downloadUsageCsv.mock.calls[0][0]
    expect(request.endpoint).toBe('/usage/export')
    expect(request.params).toMatchObject({
      page_size: 100,
      sort_by: 'created_at',
      sort_order: 'desc',
      native_compaction_v2: true,
    })
    expect(request.fileName).toBe(`usage_${(wrapper.vm as any).startDate}_to_${(wrapper.vm as any).endDate}.csv`)
    // 用户端表头不含管理端字段，权限字段差异在导出标签上保持。
    expect(request.labels).toHaveLength(32)
    expect(request.labels).toContain('API Key')
    expect(request.labels).toContain('Model')
    expect(request.labels.join(',')).not.toContain('account_billed_cost')
    expect(request.labels.join(',')).not.toContain('upstream_request_id')
    expect(showSuccess).toHaveBeenCalled()
    expect(showError).not.toHaveBeenCalled()
  })

  it('keeps the filters captured when the export starts and never loops per page', async () => {
    downloadUsageCsv.mockReset().mockResolvedValue(undefined)
    query.mockResolvedValue({ items: [usageLog], total: 101, pages: 2 })
    const wrapper = mountUsageView()
    await flushPromises()

    const datePicker = wrapper.findComponent(DateRangePicker)
    datePicker.vm.$emit('change', { startDate: '2026-03-01', endDate: '2026-03-08', preset: null })
    await flushPromises()

    const keySelect = wrapper.findAllComponents(Select).find((select) =>
      select.props('options').some((option: SelectOption) => option.label === 'All API Keys')
    )!
    keySelect.vm.$emit('update:modelValue', 1)
    keySelect.vm.$emit('change', 1)
    await flushPromises()

    query.mockClear()
    await wrapper.findAll('button').find((button) => button.text() === 'Export CSV')!.trigger('click')

    // 一次导出 = 一次请求：服务端游标负责分页，浏览器不再按页拉取。
    expect(query).not.toHaveBeenCalled()
    expect(downloadUsageCsv).toHaveBeenCalledTimes(1)
    const request = downloadUsageCsv.mock.calls[0][0]
    expect(request.params).toMatchObject({
      page_size: 100,
      start_date: '2026-03-01',
      end_date: '2026-03-08',
      api_key_id: 1,
      sort_by: 'created_at',
      sort_order: 'desc',
    })
    expect(request.fileName).toBe('usage_2026-03-01_to_2026-03-08.csv')
    expect(showSuccess).toHaveBeenCalledWith('Export success')
    expect(showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('sends the billing-mode filter to the server export instead of rewriting rows in the browser', async () => {
    // 图片计费模式的历史行推断由服务端导出契约负责（backend dto 用例覆盖）。
    downloadUsageCsv.mockReset().mockResolvedValue(undefined)
    query.mockResolvedValue({ items: [{ ...usageLog, image_count: 1, billing_mode: null }], total: 1, pages: 1 })

    const wrapper = mountUsageView()
    await flushPromises()
    ;(wrapper.vm as any).filters.billing_mode = 'image'

    await (wrapper.vm as any).exportToCSV()

    expect(downloadUsageCsv).toHaveBeenCalledTimes(1)
    expect(downloadUsageCsv.mock.calls[0][0].params).toMatchObject({ billing_mode: 'image' })
    expect(query).not.toHaveBeenCalledWith(expect.objectContaining({ page: 2 }), expect.anything())
    wrapper.unmount()
  })

  it('shows the precise history-incomplete reason instead of the generic export failure', async () => {
    downloadUsageCsv.mockReset().mockRejectedValue({
      status: 409,
      code: 409,
      reason: 'USAGE_HISTORY_INCOMPLETE',
      message: 'Usage history is incomplete: raw history or a verified archive is unavailable',
      metadata: { raw_retained_from: '2026-03-05T00:00:00Z' },
    })

    const wrapper = mountUsageView()
    await flushPromises()
    await (wrapper.vm as any).exportToCSV()
    await flushPromises()

    expect(showError).toHaveBeenCalledWith(expect.stringContaining('Usage history is incomplete'))
    expect(showError).toHaveBeenCalledWith(expect.stringContaining('Raw records retained from'))
    expect(showError).not.toHaveBeenCalledWith('Export failed')
    wrapper.unmount()
  })

  it('keeps the generic export failure for errors that are not a history gap', async () => {
    downloadUsageCsv.mockReset().mockRejectedValue({ status: 500, reason: 'INTERNAL_ERROR', message: 'boom', metadata: {} })

    const wrapper = mountUsageView()
    await flushPromises()
    await (wrapper.vm as any).exportToCSV()
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('Export failed')
    wrapper.unmount()
  })
})

describe('UsageView subscription feature flag', () => {
  afterEach(() => {
    appStoreState.cachedPublicSettings = { allow_user_view_error_requests: true }
  })

  function billingTypeSelect(wrapper: ReturnType<typeof mountUsageView>) {
    return wrapper.findAllComponents(Select).find((select) =>
      select.props('options').some((option: SelectOption) => option.label === 'Subscription')
    )
  }

  it.each(['Asia/Shanghai', 'UTC', 'America/Los_Angeles'])('passes explicit %s to every report request and projection', async timeZone => {
    const wrapper = mountUsageView(timeZone)
    await flushPromises()
    expect(query).toHaveBeenLastCalledWith(expect.objectContaining({ timezone: timeZone }), expect.anything())
    expect(getStats).toHaveBeenLastCalledWith(expect.objectContaining({ timezone: timeZone }))
    expect(getDashboardSnapshotV2).toHaveBeenLastCalledWith(expect.objectContaining({ timezone: timeZone }))
    expect(getDashboardModels).toHaveBeenLastCalledWith(expect.objectContaining({ timezone: timeZone }))
    expect(wrapper.findAll('[time-zone]').map(node => node.attributes('time-zone'))).toContain(timeZone)
    wrapper.unmount()
  })

  it('filters the table, the totals and every chart by the same billing mode', async () => {
    query.mockResolvedValue({ items: [usageLog], total: 1, pages: 1 })
    getStats.mockResolvedValue({ total_requests: 0, total_tokens: 0, total_cost: 0 })
    getDashboardSnapshotV2.mockResolvedValue({ trend: [], models: [], groups: [] })
    getDashboardModels.mockResolvedValue({ models: [] })
    const wrapper = mountUsageView()
    await flushPromises()

    ;(wrapper.vm as any).filters.billing_mode = 'video'
    ;(wrapper.vm as any).applyFilters()
    await flushPromises()

    expect(query).toHaveBeenLastCalledWith(expect.objectContaining({ billing_mode: 'video' }), expect.anything())
    expect(getStats).toHaveBeenLastCalledWith(expect.objectContaining({ billing_mode: 'video' }))
    expect(getDashboardSnapshotV2).toHaveBeenLastCalledWith(expect.objectContaining({ billing_mode: 'video' }))
    expect(getDashboardModels).toHaveBeenLastCalledWith(expect.objectContaining({ billing_mode: 'video' }))
    wrapper.unmount()
  })

  it('offers the balance / subscription billing-type filter by default', async () => {
    const wrapper = mountUsageView()
    await flushPromises()

    expect(billingTypeSelect(wrapper)).toBeDefined()
    expect(wrapper.text()).toContain('Billing type')
    wrapper.unmount()
  })

  it('hides the billing-type filter entirely when subscriptions are disabled', async () => {
    appStoreState.cachedPublicSettings = { allow_user_view_error_requests: true, subscription_enabled: false }

    const wrapper = mountUsageView()
    await flushPromises()

    expect(billingTypeSelect(wrapper)).toBeUndefined()
    expect(wrapper.text()).not.toContain('Billing type')
    wrapper.unmount()
  })
})
