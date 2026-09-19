import { baseCompile } from '@intlify/message-compiler'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
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
describe('user dashboard states', () => {
  beforeEach(() => {
    api.getDashboardStats.mockReset().mockResolvedValue({ total_requests: 1 })
    api.getDashboardTrend.mockReset().mockResolvedValue({ trend: [{ date: '2026-03-01', requests: 1 }] })
    api.getDashboardModels.mockReset().mockResolvedValue({ models: [{ model: 'example' }] })
    api.query.mockReset().mockResolvedValue({ items: [] })
  })
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
  it.each(['en', 'zh'])('shows incomplete coverage, reason and retention in %s', async locale => {
    api.getDashboardStats.mockResolvedValue({ coverage: { complete: false, reason: 'raw_history_unavailable', raw_retained_from: '2026-03-05T00:00:00Z' } })
    const wrapper = mountPage(locale); await flushPromises()
    const banner = wrapper.get('[data-testid="coverage-incomplete"]')
    expect(banner.text()).toContain('raw_history_unavailable')
    expect(banner.text()).toContain('2026-03-05T00:00:00Z')
    expect(banner.text()).toContain(locale === 'en' ? 'Missing history does not mean zero usage' : '缺失历史不代表零用量')
    wrapper.unmount()
  })
  it('invalidates old charts on 409 and recovers on a successful range', async () => {
    const wrapper = mountPage(); await flushPromises()
    api.getDashboardTrend.mockRejectedValueOnce({ status: 409, reason: 'USAGE_HISTORY_INCOMPLETE', metadata: { raw_retained_from: '2026-03-05T00:00:00Z' } })
    wrapper.getComponent({ name: 'UserDashboardCharts' }).vm.$emit('dateRangeChange'); await flushPromises()
    expect(wrapper.get('[data-testid="history-gap"]').text()).toContain('No statistics were returned')
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
