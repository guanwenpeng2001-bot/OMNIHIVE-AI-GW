import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'
import ProxiesView from '../ProxiesView.vue'
import type { ProxyQualityCheckResult } from '@/types'

const { list, getAllWithCount, checkProxyQuality, testProxy, showSuccess, showInfo } = vi.hoisted(() => ({
  list: vi.fn(), getAllWithCount: vi.fn(), checkProxyQuality: vi.fn(), testProxy: vi.fn(), showSuccess: vi.fn(), showInfo: vi.fn()
}))
vi.mock('@/api/admin', () => ({ adminAPI: { proxies: { list, getAllWithCount, checkProxyQuality, testProxy } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess, showInfo }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string, values?: Record<string, unknown>) => values ? `${key} ${JSON.stringify(values)}` : key })
}))
const mountView = () => shallowMount(ProxiesView, {
  global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' },
    TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /></div>' },
    DataTable: { props: ['data'], template: '<div><div v-for="row in data" :key="row.id"><div data-test="latency"><slot name="cell-latency" :row="row" /></div><slot name="cell-actions" :row="row" /></div></div>' },
    BaseDialog: { props: ['show'], template: '<div v-if="show" data-test="dialog"><slot /><slot name="footer" /></div>' }
  } }
})
let wrapper: ReturnType<typeof mountView>
const checkedAt = 1700000000
const result = (overrides: Partial<ProxyQualityCheckResult> = {}): ProxyQualityCheckResult => ({
  proxy_id: 9, score: 78, grade: 'B', summary: 'synthetic summary', checked_at: checkedAt,
  passed_count: 4, warn_count: 0, failed_count: 1, challenge_count: 0,
  items: [{ target: 'base_connectivity', status: 'fail', message: 'IP echo unavailable' },
    ...['openai', 'anthropic', 'gemini', 'grok'].map(target => ({ target, status: 'pass' as const }))],
  ...overrides
})
beforeEach(() => {
  vi.clearAllMocks()
  list.mockResolvedValue({ items: [{ id: 9, name: 'proxy', status: 'active', protocol: 'http', host: 'example.invalid', port: 8080, latency_ms: 20, latency_status: 'success' }], total: 1, pages: 1 })
  getAllWithCount.mockResolvedValue([{ id: 9 }])
})
afterEach(() => wrapper?.unmount())
async function check(report: ProxyQualityCheckResult) {
  checkProxyQuality.mockResolvedValue(report)
  wrapper = mountView()
  await flushPromises()
  await wrapper.findAll('button').find(button => button.text() === 'admin.proxies.qualityCheck')!.trigger('click')
  await flushPromises()
}

describe('proxy quality observations', () => {
  it.each([['providers reachable', 4, 'warn'], ['all failed', 0, 'failed']])('uses the same status in batch summaries: %s', async (_name, passed, status) => {
    checkProxyQuality.mockResolvedValue(result({ passed_count: passed as number, failed_count: passed ? 1 : 5 }))
    wrapper = mountView()
    await flushPromises()
    await wrapper.get('button[title="admin.proxies.batchQualityCheck"]').trigger('click')
    await flushPromises()
    expect(checkProxyQuality).toHaveBeenCalledWith(9)
    expect(showSuccess).toHaveBeenCalledWith(`admin.proxies.batchQualityDone ${JSON.stringify({ count: 1, healthy: 0, warn: status === 'warn' ? 1 : 0, challenge: 0, failed: status === 'failed' ? 1 : 0 })}`)
  })

  it('preserves the assessed healthy baseline', async () => {
    await check(result({ score: 100, grade: 'A', passed_count: 5, failed_count: 0, base_latency_ms: 23,
      items: [{ target: 'base_connectivity', status: 'pass' }] }))
    const cell = wrapper.get('[data-test="latency"]')
    expect(cell.text()).toContain('23ms')
    expect(cell.text()).toContain('admin.proxies.qualityStatusHealthy')
    expect(cell.text()).toContain('"grade":"A","score":100')
    expect(wrapper.get('[data-test="dialog"]').text()).toContain('admin.proxies.qualityGrade')
  })

  it('shows provider reachability as warn while clearing stale successful base latency', async () => {
    await check(result())
    const cell = wrapper.get('[data-test="latency"]')
    expect(cell.text()).toContain('admin.proxies.qualityStatusWarn')
    expect(cell.text()).toContain('admin.proxies.latencyFailed')
    expect(cell.text()).not.toContain('20ms')
    expect(cell.text()).toContain('"grade":"B","score":78')
    expect(cell.text()).toContain('admin.proxies.qualityCheckedAt')
    expect(cell.text()).toContain(new Date(checkedAt * 1000).toLocaleString())
    expect(wrapper.get('[data-test="dialog"]').text()).toContain('admin.proxies.httpProbeHint')
  })

  it.each([[78, 'B'], [0, 'F'], [0, '']])('hides unassessed score %s/%s in immediate list, modal and notification', async (score, grade) => {
    await check(result({ score: score as number, grade: grade as string, passed_count: 0, failed_count: 5,
      items: [{ target: 'base_connectivity', status: 'fail' }] }))
    const cell = wrapper.get('[data-test="latency"]')
    expect(cell.text()).toContain('admin.proxies.qualityUnassessed')
    expect(cell.text()).toContain('admin.proxies.qualityStatusFail')
    expect(cell.text()).not.toContain('admin.proxies.qualityInline')
    expect(wrapper.get('[data-test="dialog"]').text()).toContain('admin.proxies.qualityUnassessed')
    expect(wrapper.get('[data-test="dialog"]').text()).not.toContain('admin.proxies.qualityGrade')
    expect(showSuccess).not.toHaveBeenCalled()
    expect(showInfo).toHaveBeenCalledWith('admin.proxies.qualityUnassessed')
  })

  it('renders cached nil score with its check timestamp', async () => {
    list.mockResolvedValue({ items: [{ id: 9, name: 'proxy', quality_checked: checkedAt, quality_score: null, quality_grade: '', quality_status: 'failed' }], total: 1, pages: 1 })
    wrapper = mountView()
    await flushPromises()
    const cell = wrapper.get('[data-test="latency"]')
    expect(cell.text()).toContain('admin.proxies.qualityUnassessed')
    expect(cell.text()).toContain('admin.proxies.qualityCheckedAt')
    expect(cell.text()).toContain(new Date(checkedAt * 1000).toLocaleString())
  })

  it('labels historical quality alongside a newer failed latency test', async () => {
    list.mockResolvedValue({ items: [{ id: 9, name: 'proxy', quality_checked: checkedAt, quality_score: 100, quality_grade: 'A', quality_status: 'healthy' }], total: 1, pages: 1 })
    testProxy.mockResolvedValue({ success: false, message: 'new failure' })
    wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').filter(button => button.text() === 'admin.proxies.testConnection').at(-1)!.trigger('click')
    await flushPromises()
    const cell = wrapper.get('[data-test="latency"]')
    expect(cell.text()).toContain('admin.proxies.latencyFailed')
    expect(cell.text()).toContain('"grade":"A","score":100')
    expect(cell.text()).toContain('admin.proxies.qualityCheckedAt')
    expect(cell.text()).toContain(new Date(checkedAt * 1000).toLocaleString())
  })

  it('keeps Cloudflare challenge distinct from healthy', async () => {
    await check(result({ passed_count: 0, failed_count: 1, challenge_count: 4 }))
    expect(wrapper.get('[data-test="latency"]').text()).toContain('admin.proxies.qualityStatusChallenge')
    expect(wrapper.get('[data-test="latency"]').text()).toContain('admin.proxies.qualityUnassessed')
  })
})
