import { Line } from 'vue-chartjs'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'

import type { DashboardStats } from '@/types'
import DashboardView from '../DashboardView.vue'

const { getSnapshotV2, getUserUsageTrend, getUserSpendingRanking } = vi.hoisted(() => ({
  getSnapshotV2: vi.fn(),
  getUserUsageTrend: vi.fn(),
  getUserSpendingRanking: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    dashboard: {
      getSnapshotV2,
      getUserUsageTrend,
      getUserSpendingRanking
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn()
  })
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({
    push: vi.fn()
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
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
    expect(wrapper.find('[data-testid="history-gap"]').exists()).toBe(true)
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
