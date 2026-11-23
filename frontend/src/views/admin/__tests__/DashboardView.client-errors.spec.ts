import { baseCompile } from '@intlify/message-compiler'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { AxiosError, type AxiosAdapter } from 'axios'
import { apiClient } from '@/api/client'
import DashboardView from '../DashboardView.vue'
import en from '@/i18n/locales/en/dashboard'
import zh from '@/i18n/locales/zh/dashboard'

const { showError } = vi.hoisted(() => ({ showError: vi.fn() }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError }) }))
vi.mock('vue-router', async () => ({ RouterLink: (await import('@vue/test-utils')).RouterLinkStub, useRouter: () => ({ push: vi.fn() }) }))
vi.mock('@/composables/useBatchImageAccess', () => ({ useBatchImageAccess: () => ({ canUseBatchImage: false, refreshBatchImageAccess: vi.fn() }) }))
// No mock of client.ts, Axios interceptors, or dashboard API functions.
const originalAdapter = apiClient.defaults.adapter
let status = 409
let reason = 'USAGE_HISTORY_INCOMPLETE'
const adapter: AxiosAdapter = async config => {
  const snapshot = config.url?.includes('snapshot-v2')
  if (snapshot) {
    const response = { status, statusText: 'Failure', headers: {}, config, data: { code: status, reason, message: 'History unavailable', metadata: { raw_retained_from: '2026-03-05T00:00:00Z' } } }
    throw new AxiosError('Request failed', 'ERR_BAD_RESPONSE', config, {}, response)
  }
  const data = config.url?.endsWith('/models') ? { items: [] } : config.url?.endsWith('/stats') ? { coverage: { complete: false, reason: 'raw_history_unavailable', raw_retained_from: '2026-03-05T00:00:00Z' } } : { trend: [], ranking: [] }
  return { status: 200, statusText: 'OK', headers: {}, config, data: { code: 0, data } }
}
describe('real client to dashboard error chain', () => {
  beforeEach(() => { window.history.replaceState({}, '', '/login'); apiClient.defaults.adapter = adapter; status = 409; reason = 'USAGE_HISTORY_INCOMPLETE'; showError.mockReset() })
  afterEach(() => { apiClient.defaults.adapter = originalAdapter })
  it.each(['en', 'zh'])('renders neutral unknown without coverage details in %s', async locale => {
    const wrapper = mount(DashboardView, { global: { plugins: [createI18n({ messageCompiler: message => new Function('return ' + baseCompile(message as string).code)(), legacy: false, locale, missingWarn: false, fallbackWarn: false, messages: { en, zh } })], stubs: { AppLayout: { template: '<div><slot /></div>' }, LoadingSpinner: true, Icon: true, DateRangePicker: true, Select: true, ModelDistributionChart: true, TokenUsageTrend: true, Line: true } } })
    await flushPromises()
    expect(wrapper.find('[data-testid="history-gap"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="coverage-incomplete"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('—')
    expect(wrapper.html()).not.toContain('raw_history_unavailable')
    expect(wrapper.text()).not.toContain('2026-03-05T00:00:00Z')
    expect(showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it.each([[409, 'OTHER_CONFLICT'], [500, 'USAGE_HISTORY_INCOMPLETE'], [401, 'UNAUTHORIZED']])('keeps %i / %s on the ordinary error path', async (failureStatus, failureReason) => {
    status = failureStatus as number; reason = failureReason as string
    const wrapper = mount(DashboardView, { global: { plugins: [createI18n({ messageCompiler: message => new Function('return ' + baseCompile(message as string).code)(), legacy: false, locale: 'en', missingWarn: false, messages: { en } })], stubs: { AppLayout: { template: '<div><slot /></div>' }, LoadingSpinner: true } } })
    await flushPromises()
    expect(wrapper.find('[data-testid="history-gap"]').exists()).toBe(false)
    expect(showError).toHaveBeenCalled()
    wrapper.unmount()
  })
})
