import { Line } from 'vue-chartjs'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'

import type { DashboardStats } from '@/types'
import DashboardView from '../DashboardView.vue'

const { getSnapshotV2, getUserUsageTrend, getUserSpendingRanking, getStats } = vi.hoisted(() => ({
  getSnapshotV2: vi.fn(),
  getUserUsageTrend: vi.fn(),
  getUserSpendingRanking: vi.fn(),
  getStats: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    dashboard: {
      getSnapshotV2,
      getUserUsageTrend,
      getUserSpendingRanking,
      getStats
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn()
  })
}))

vi.mock('@/api/catalog', () => ({ listPublicModels: vi.fn().mockResolvedValue({ items: [] }) }))

vi.mock('vue-router', async () => ({
  RouterLink: (await import('@vue/test-utils')).RouterLinkStub,
  useRouter: () => ({
    push: vi.fn()
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => key + (params ? ':' + JSON.stringify(params) : '')
    })
  }
})

const formatLocalDate = (date: Date): string => {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

const createDashboardStats = (): DashboardStats => ({
  total_users: 0,
  coverage: { complete: true, raw_retained_from: '2026-01-01T00:00:00Z', published_before: '', timezone: 'UTC', projection_version: 2 },
  today_new_users: 0,
  active_users: 0,
  hourly_active_users: 0,
  stats_updated_at: '',
  stats_stale: false,
  total_api_keys: 0,
  active_api_keys: 0,
  total_accounts: 0,
  normal_accounts: 0,
  error_accounts: 0,
  ratelimit_accounts: 0,
  overload_accounts: 0,
  total_requests: 0,
  total_input_tokens: 0,
  total_output_tokens: 0,
  total_cache_creation_tokens: 0,
  total_cache_read_tokens: 0,
  total_tokens: 0,
  total_cost: 0,
  total_actual_cost: 0,
  today_requests: 0,
  today_input_tokens: 0,
  today_output_tokens: 0,
  today_cache_creation_tokens: 0,
  today_cache_read_tokens: 0,
  today_tokens: 0,
  today_cost: 0,
  today_actual_cost: 0,
  average_duration_ms: 0,
  uptime: 0,
  rpm: 0,
  tpm: 0
})

describe('admin DashboardView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())

    getSnapshotV2.mockReset()
    getUserUsageTrend.mockReset()
    getUserSpendingRanking.mockReset()
    getStats.mockReset()

    getSnapshotV2.mockResolvedValue({
      stats: createDashboardStats(),
      trend: [],
      models: []
    })
    getUserUsageTrend.mockResolvedValue({
      trend: [],
      start_date: '',
      end_date: '',
      granularity: 'hour'
    })
    getUserSpendingRanking.mockResolvedValue({
      ranking: [],
      total_actual_cost: 0,
      total_requests: 0,
      total_tokens: 0,
      start_date: '',
      end_date: ''
    })
  })

  it.each(['all_time', 'retained', 'range', 'unknown', 'legacy'] as const)('labels %s counters and their average', async scope => {
    const coverage = { complete: scope === 'all_time', retained_complete: scope === 'retained', range_complete: scope === 'range',
      window_from: '2026-09-11T16:00:00Z', window_until: '2026-10-02T05:00:00Z', timezone: 'Asia/Shanghai',
      raw_retained_from: '2026-09-11T16:00:00Z', published_before: '', projection_version: 2, reason: 'history_before_coverage_floor_unverified' }
    getSnapshotV2.mockResolvedValueOnce({ stats: { ...createDashboardStats(), total_requests: 987654, coverage: scope === 'legacy' ? undefined : coverage }, trend: [], models: [] })
    const wrapper = mount(DashboardView, { props: { timeZone: 'Asia/Shanghai' }, global: { stubs: {
      AppLayout: { template: '<div><slot /></div>' }, LoadingSpinner: true, Icon: true, DateRangePicker: true,
      Select: true, ModelDistributionChart: true, TokenUsageTrend: true, Line: true,
    } } })
    await flushPromises()
    const text = wrapper.text()
    expect(text).not.toContain('history_before_coverage_floor_unverified')
    if (scope === 'unknown' || scope === 'legacy') {
      expect(text).toContain('dashboard.scopeUnknown'); expect(text).not.toContain('987,654')
    } else {
      expect(text).toContain('987,654'); expect(text).toContain('0ms')
      if (scope !== 'all_time') {
        expect(text).toContain(scope === 'range' ? 'dashboard.scopeRange' : 'dashboard.scopeRetained')
        expect(text).toContain('2026-09-12 00:00')
        expect(text).toContain('2026-10-02 13:00')
        expect(text).not.toContain('T16:00:00Z')
      }
    }
    wrapper.unmount()
  })

  it('keeps loading and failed overview requests separate from unknown coverage', async () => {
    let rejectQuery!: (error: Error) => void
    getSnapshotV2.mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectQuery = reject }))
    const wrapper = mount(DashboardView, { global: { stubs: {
      AppLayout: { template: '<div><slot /></div>' }, LoadingSpinner: true, Icon: true, DateRangePicker: true,
      Select: true, ModelDistributionChart: true, TokenUsageTrend: true, Line: true,
    } } })
    await flushPromises()
    expect(wrapper.findComponent({ name: 'LoadingSpinner' }).exists()).toBe(true)
    expect(wrapper.text()).not.toContain('dashboard.scopeUnknown')
    rejectQuery(new Error('Synthetic overview failure'))
    await flushPromises()
    expect(wrapper.findComponent({ name: 'LoadingSpinner' }).exists()).toBe(false)
    expect(wrapper.text()).not.toContain('dashboard.scopeUnknown')
    expect(wrapper.getComponent({ name: 'UsageStatsState' }).props('state')).toBe('error')
    wrapper.unmount()
  })

  it('hides unverified historical totals and keeps independently returned entities and today values', async () => {
    getSnapshotV2.mockResolvedValueOnce({ stats: { ...createDashboardStats(), total_users: 345, total_api_keys: 678,
      total_requests: 987654, total_tokens: 765432, total_cost: 456.78, total_actual_cost: 456.78,
      today_requests: 23, today_actual_cost: 1.25,
      coverage: { complete: false, reason: 'history_after_origin_unavailable', raw_retained_from: '2026-09-01T00:00:00Z' } }, trend: [], models: [] })
    const wrapper = mount(DashboardView, { global: { stubs: {
      AppLayout: { template: '<div><slot /></div>' }, LoadingSpinner: true,
      Icon: true, DateRangePicker: true, Select: true, ModelDistributionChart: true,
      TokenUsageTrend: true, Line: true,
    } } })
    await flushPromises()
    expect(wrapper.text()).toContain('345')
    expect(wrapper.text()).toContain('678')
    expect(wrapper.text()).toContain('23')
    expect(wrapper.text()).toContain('USD 1.25')
    expect(wrapper.text()).not.toContain('987,654')
    expect(wrapper.text()).not.toContain('456.78')
    expect(wrapper.html()).not.toContain('history_after_origin_unavailable')
    expect(wrapper.html()).not.toContain('2026-09-01')
    expect(wrapper.text()).toContain('—')
    wrapper.unmount()
  })

  it('keeps model and ranking request states independent', async () => {
    getUserSpendingRanking.mockRejectedValueOnce({ status: 409, reason: 'USAGE_HISTORY_INCOMPLETE' })
    const wrapper = mount(DashboardView, { global: { stubs: {
      AppLayout: { template: '<div><slot /></div>' }, LoadingSpinner: true,
      Icon: true, DateRangePicker: true, Select: true, ModelDistributionChart: true,
      TokenUsageTrend: true, Line: true,
    } } })
    await flushPromises()
    const chart = wrapper.getComponent({ name: 'ModelDistributionChart' })
    expect(chart.props('rankingUnknown')).toBe(true)
    expect(chart.props('rankingError')).toBe(false)
    expect(chart.props('modelUnknown')).toBe(false)
    const page = wrapper.vm as any
    await page.loadUserSpendingRanking()
    getSnapshotV2.mockRejectedValueOnce({ status: 409, reason: 'USAGE_HISTORY_INCOMPLETE' })
    await page.loadDashboardSnapshot(false)
    expect(chart.props('modelUnknown')).toBe(true)
    expect(chart.props('rankingUnknown')).toBe(false)
    wrapper.unmount()
  })

  it.each(['Asia/Shanghai', 'UTC', 'America/Los_Angeles'])('shares explicit %s and projects user chart gaps without another read', async timeZone => {
    getUserUsageTrend.mockResolvedValue({ trend: [
      { date: '2026-11-01 01:00', user_id: 1, username: 'first', tokens: 7 },
      { date: '2026-11-01 02:00', user_id: 2, username: 'second', tokens: 9 },
    ] })
    const wrapper = mount(DashboardView, { props: { timeZone }, global: { stubs: {
      AppLayout: { template: '<div><slot /></div>' }, LoadingSpinner: true,
      Icon: true, DateRangePicker: true, Select: true, ModelDistributionChart: true,
      TokenUsageTrend: true, Line: true,
    } } })
    await flushPromises()
    for (const request of [getSnapshotV2, getUserUsageTrend, getUserSpendingRanking]) {
      expect(request).toHaveBeenCalledTimes(1)
      expect(request).toHaveBeenCalledWith(expect.objectContaining({ timezone: timeZone }))
    }
    expect(wrapper.getComponent({ name: 'TokenUsageTrend' }).props('timeZone')).toBe(timeZone)
    const chart = wrapper.getComponent(Line).props('data')
    const rows = wrapper.get('[data-testid="user-trend-table"]').findAll('tbody tr')
    expect(rows.map(row => row.get('th').text())).toEqual(chart.labels)
    expect(rows.map(row => row.findAll('td').map(cell => cell.text()))).toEqual([['7', '-'], ['-', '9']])
    expect(Number.isNaN(chart.datasets[0].data[1])).toBe(true)
    expect(wrapper.get('[data-testid="user-trend-table"]').element.parentElement?.parentElement?.classList.contains('h-64')).toBe(false)
    wrapper.unmount()
  })

  it('clears obsolete snapshot charts on a history gap and recovers', async () => {
    const wrapper = mount(DashboardView, { global: { stubs: {
      AppLayout: { template: '<div><slot /></div>' }, LoadingSpinner: true,
      Icon: true, DateRangePicker: true, Select: true, ModelDistributionChart: true,
      TokenUsageTrend: true, Line: true,
    } } })
    await flushPromises()
    const page = wrapper.vm as any
    getSnapshotV2.mockResolvedValueOnce({ trend: [{ date: '2026-03-01', requests: 7 }], models: [{ model: 'old' }] })
    await page.loadDashboardSnapshot(false)
    expect(page.trendData).toHaveLength(1)
    getSnapshotV2.mockRejectedValueOnce({ status: 409, reason: 'USAGE_HISTORY_INCOMPLETE' })
    await page.loadDashboardSnapshot(false)
    expect(page.trendData).toEqual([])
    expect(page.modelStats).toEqual([])
    expect(wrapper.find('[data-testid="history-gap"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('—')
    await page.loadDashboardSnapshot(false)
    expect(wrapper.find('[data-testid="history-gap"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('does not let an obsolete snapshot failure replace newer charts', async () => {
    const wrapper = mount(DashboardView, { global: { stubs: {
      AppLayout: { template: '<div><slot /></div>' }, LoadingSpinner: true,
      Icon: true, DateRangePicker: true, Select: true, ModelDistributionChart: true,
      TokenUsageTrend: true, Line: true,
    } } })
    await flushPromises()
    const page = wrapper.vm as any
    let rejectOld!: (reason: unknown) => void
    getSnapshotV2.mockReturnValueOnce(new Promise((_resolve, reject) => { rejectOld = reject }))
    const oldRequest = page.loadDashboardSnapshot(false)
    getSnapshotV2.mockResolvedValueOnce({ trend: [{ date: '2026-03-06', requests: 9 }], models: [] })
    await page.loadDashboardSnapshot(false)
    rejectOld({ status: 409, reason: 'USAGE_HISTORY_INCOMPLETE' })
    await oldRequest
    expect(page.trendData[0].requests).toBe(9)
    expect(wrapper.find('[data-testid="history-gap"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows the account cost as unavailable instead of a fake zero when the overview fallback cannot prove it', async () => {
    // 降级概览(/admin/dashboard/stats 旧响应)不含账号成本时，卡片必须明确显示不可用，
    // 直接走 formatCost 会把未知值渲染成 0.0000，等于低估账号成本。
    const wrapper = mount(DashboardView, { global: { stubs: {
      AppLayout: { template: '<div><slot /></div>' }, LoadingSpinner: true,
      Icon: true, DateRangePicker: true, Select: true, ModelDistributionChart: true,
      TokenUsageTrend: true, Line: true,
    } } })
    await flushPromises()
    const page = wrapper.vm as any

    getSnapshotV2.mockRejectedValueOnce({ status: 409, reason: 'USAGE_HISTORY_INCOMPLETE' })
    getStats.mockResolvedValueOnce({
      ...createDashboardStats(),
      total_cost: 12,
      total_actual_cost: 9,
      today_cost: 1,
      today_actual_cost: 0.5,
    })
    await page.loadDashboardSnapshot(true)
    await flushPromises()

    expect(getStats).toHaveBeenCalledTimes(1)
    const text = wrapper.text()
    expect(text).toContain('admin.dashboard.accountCost')
    expect(text).toContain('common.notAvailable')
    expect(text).not.toContain('USD 0.0000')
    wrapper.unmount()
  })

  it('renders the real account cost when the payload carries it', async () => {
    const wrapper = mount(DashboardView, { global: { stubs: {
      AppLayout: { template: '<div><slot /></div>' }, LoadingSpinner: true,
      Icon: true, DateRangePicker: true, Select: true, ModelDistributionChart: true,
      TokenUsageTrend: true, Line: true,
    } } })
    await flushPromises()
    const page = wrapper.vm as any

    getSnapshotV2.mockResolvedValueOnce({
      stats: { ...createDashboardStats(), total_account_cost: 4.5, today_account_cost: 0.45 },
      trend: [],
      models: [],
    })
    await page.loadDashboardSnapshot(true)
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('USD 4.50')
    expect(text).toContain('USD 0.450')
    expect(text).not.toContain('common.notAvailable')
    wrapper.unmount()
  })

  it('uses last 24 hours as default dashboard range', async () => {
    mount(DashboardView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          LoadingSpinner: true,
          Icon: true,
          DateRangePicker: true,
          Select: true,
          ModelDistributionChart: true,
          TokenUsageTrend: true,
          Line: true
        }
      }
    })

    await flushPromises()

    const now = new Date()
    const yesterday = new Date(now.getTime() - 24 * 60 * 60 * 1000)

    expect(getSnapshotV2).toHaveBeenCalledTimes(1)
    expect(getSnapshotV2).toHaveBeenCalledWith(expect.objectContaining({
      start_date: formatLocalDate(yesterday),
      end_date: formatLocalDate(now),
      granularity: 'hour'
    }))
  })
})
