import { baseCompile } from '@intlify/message-compiler'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { useAppStore } from '@/stores/app'
import { createI18n } from 'vue-i18n'
import DashboardView from '../DashboardView.vue'
import en from '@/i18n/locales/en/dashboard'
import zh from '@/i18n/locales/zh/dashboard'

const api = vi.hoisted(() => ({ getDashboardStats: vi.fn(), getDashboardTrend: vi.fn(), getDashboardModels: vi.fn(), query: vi.fn() }))
vi.mock('@/api/usage', () => ({ usageAPI: api }))
vi.mock('@/api/user', () => ({ getMyPlatformQuotas: vi.fn(async () => ({})) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: {}, refreshUser: vi.fn(), isSimpleMode: false }) }))
const stubs = { AppLayout: { template: '<div><slot /></div>' }, LoadingSpinner: true, UserDashboardStats: true, UserDashboardCharts: true, UserDashboardRecentUsage: true, UserDashboardQuickActions: true }
const mountPage = (locale = 'en', timeZone?: string) => mount(DashboardView, { props: { timeZone }, global: { stubs, plugins: [createI18n({ messageCompiler: message => new Function('return ' + baseCompile(message as string).code)(), legacy: false, locale, messages: { en, zh } })] } })
beforeEach(() => {
  localStorage.clear()
  setActivePinia(createPinia())
  const app = useAppStore()
  app.scopesLoaded = true
  app.scopes = []
  api.getDashboardStats.mockReset().mockResolvedValue({ total_requests: 1 })
  api.getDashboardTrend.mockReset().mockResolvedValue({ trend: [{ date: '2026-03-01', requests: 1 }] })
  api.getDashboardModels.mockReset().mockResolvedValue({ models: [{ model: 'example' }] })
  api.query.mockReset().mockResolvedValue({ items: [] })
})

describe('user dashboard states', () => {

  it.each(['Asia/Shanghai', 'UTC', 'America/Los_Angeles'])('shares explicit %s between chart requests and recent rows', async timeZone => {
    const wrapper = mountPage('en', timeZone)
    await flushPromises()
    expect(api.getDashboardTrend).toHaveBeenCalledTimes(1)
    expect(api.getDashboardModels).toHaveBeenCalledTimes(1)
    expect(api.query).toHaveBeenCalledTimes(1)
    for (const request of [api.getDashboardTrend, api.getDashboardModels, api.query]) {
      expect(request).toHaveBeenCalledWith(expect.objectContaining({ timezone: timeZone }))
    }
    expect(wrapper.getComponent({ name: 'UserDashboardCharts' }).props('timeZone')).toBe(timeZone)
    expect(wrapper.getComponent({ name: 'UserDashboardRecentUsage' }).props('timeZone')).toBe(timeZone)
    wrapper.unmount()
  })
  it.each(['en', 'zh'])('omits coverage details in %s', async locale => {
    api.getDashboardStats.mockResolvedValue({ coverage: { complete: false, reason: 'raw_history_unavailable', raw_retained_from: '2026-03-05T00:00:00Z' } })
    const wrapper = mountPage(locale); await flushPromises()
    expect(wrapper.find('[data-testid="coverage-incomplete"]').exists()).toBe(false)
    expect(wrapper.html()).not.toContain('raw_history_unavailable')
    expect(wrapper.text()).not.toContain('2026-03-05T00:00:00Z')
    wrapper.unmount()
  })
  it('invalidates old charts on 409 and recovers on a successful range', async () => {
    const wrapper = mountPage(); await flushPromises()
    api.getDashboardTrend.mockRejectedValueOnce({ status: 409, reason: 'USAGE_HISTORY_INCOMPLETE', metadata: { raw_retained_from: '2026-03-05T00:00:00Z' } })
    wrapper.getComponent({ name: 'UserDashboardCharts' }).vm.$emit('dateRangeChange'); await flushPromises()
    expect(wrapper.get('[data-testid="chart-state"]').text()).toContain('—')
    expect(wrapper.getComponent({ name: 'UserDashboardCharts' }).isVisible()).toBe(false)
    expect(wrapper.getComponent({ name: 'UserDashboardCharts' }).props('trend')).toEqual([])
    wrapper.getComponent({ name: 'UserDashboardCharts' }).vm.$emit('dateRangeChange'); await flushPromises()
    expect(wrapper.find('[data-testid="history-gap"]').exists()).toBe(false)
    wrapper.unmount()
  })
  it('does not show a historical gap for unrelated conflicts', async () => {
    api.getDashboardTrend.mockRejectedValueOnce({ status: 409, reason: 'OTHER_CONFLICT' })
    const wrapper = mountPage(); await flushPromises()
    expect(wrapper.find('[data-testid="history-gap"]').exists()).toBe(false)
    wrapper.unmount()
  })
})


describe('user dashboard independent errors and late responses', () => {
  it.each([{ status: 409, reason: 'OTHER_CONFLICT' }, { status: 500 }, { status: 401 }, { status: 403 }, new Error('network')])('shows a chart failure for %j without losing recent results', async error => {
    api.getDashboardStats.mockResolvedValue({ total_requests: 0 })
    api.getDashboardTrend.mockRejectedValueOnce(error)
    api.query.mockResolvedValue({ items: [{ id: 42 }] })
    const wrapper = mountPage()
    await flushPromises()
    expect(wrapper.get('[data-testid="chart-state"]').text()).not.toContain('—')
    expect(wrapper.get('[data-testid="chart-state"]').text()).toContain('Failed')
    expect(wrapper.getComponent({ name: 'UserDashboardRecentUsage' }).props('data')).toEqual([{ id: 42 }])
    wrapper.unmount()
  })

  it('keeps a newer successful chart after an obsolete history failure', async () => {
    let rejectOld!: (error: unknown) => void
    api.getDashboardTrend.mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectOld = reject }))
    const wrapper = mountPage()
    await flushPromises()
    api.getDashboardTrend.mockResolvedValueOnce({ trend: [{ date: '2026-09-20', requests: 6 }] })
    wrapper.getComponent({ name: 'UserDashboardCharts' }).vm.$emit('dateRangeChange')
    await flushPromises()
    rejectOld({ status: 409, reason: 'USAGE_HISTORY_INCOMPLETE' })
    await flushPromises()
    expect(wrapper.find('[data-testid="chart-state"]').exists()).toBe(false)
    expect(wrapper.getComponent({ name: 'UserDashboardCharts' }).props('trend')[0].requests).toBe(6)
    wrapper.unmount()
  })
})
