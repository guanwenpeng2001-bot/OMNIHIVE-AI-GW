import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import TogetherBillingUsage from '../TogetherBillingUsage.vue'
import type { Account } from '@/types'
import type { TogetherBillingResult } from '@/api/admin/togetherUsage'

const { queryTogetherUsage } = vi.hoisted(() => ({ queryTogetherUsage: vi.fn() }))
enableAutoUnmount(afterEach)
vi.mock('@/api/admin/togetherUsage', async () => ({ ...await vi.importActual<typeof import('@/api/admin/togetherUsage')>('@/api/admin/togetherUsage'), queryTogetherUsage }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
const account = { id: 12, type: 'apikey', platform: 'together', credentials: { account_mode: 'payg' }, credentials_status: { has_api_key: true } } as Account
function response(): TogetherBillingResult {
  return { provider: 'together', kind: 'billing_usage', success: true, persisted: false, fetched_at: 1, usage: {
    object: 'list', scope: 'organization', organization_id: 'org-fixture', billing_period: '2026-06', granularity: 'day', currency: 'USD', earliest_window_start: '2026-06-01T00:00:00Z', latest_window_end: '2026-06-02T00:00:00Z', next_cursor: null, pages_fetched: 1, partial: false,
    data: [{ date: '2026-06-01', start_time: '2026-06-01T00:00:00Z', end_time: '2026-06-02T00:00:00Z', line_items: [{ product_name: 'Inference', quantity: '9007199254740993.123', unit_price: '0.00000001', cost: '0.1234567890123456789', pricing_dimensions: {}, attributes: {} }] }]
  } }
}
async function query(wrapper: ReturnType<typeof mount>) { await wrapper.get('[data-test="together-query"]').trigger('click'); await flushPromises() }
describe('TogetherBillingUsage', () => {
  beforeEach(() => { queryTogetherUsage.mockReset(); vi.useFakeTimers({ toFake: ['Date'] }); vi.setSystemTime(new Date('2026-06-30T23:30:00Z')) })
  afterEach(() => { vi.useRealTimers() })
  it('queries manually with month, organization scope and exact decimal strings', async () => {
    queryTogetherUsage.mockResolvedValue(response())
    const wrapper = mount(TogetherBillingUsage, { props: { account } })
    expect(queryTogetherUsage).not.toHaveBeenCalled()
    await query(wrapper)
    expect(queryTogetherUsage).toHaveBeenCalledWith(12, '2026-06')
    expect(wrapper.text()).toContain('togetherUsage.scope')
    expect(wrapper.text()).toContain('9007199254740993.123')
    expect(wrapper.text()).toContain('0.1234567890123456789 USD')
    expect(wrapper.text()).toContain('togetherUsage.unknown')
    expect(wrapper.findAll('th')).toHaveLength(6)
  })
  it('shows actual unit, partial and empty distinctly', async () => {
    const data = response(); data.usage!.partial = true; data.usage!.next_cursor = 'opaque'; data.usage!.data[0].line_items[0].pricing_dimensions.unit = 'tokens'
    queryTogetherUsage.mockResolvedValueOnce(data)
    const wrapper = mount(TogetherBillingUsage, { props: { account } }); await query(wrapper)
    expect(wrapper.text()).toContain('tokens'); expect(wrapper.text()).toContain('togetherUsage.partial')
    const empty = response(); empty.usage!.data = []; queryTogetherUsage.mockResolvedValueOnce(empty); await query(wrapper)
    expect(wrapper.text()).toContain('togetherUsage.empty'); expect(wrapper.find('table').exists()).toBe(false)
  })
  it.each([[401, 'invalid_credentials'], [403, 'permission_denied'], [404, 'not_enabled'], [429, 'rate_limited']])('shows status %i without zero usage', async (status, reason) => {
    queryTogetherUsage.mockResolvedValue({ success: false, status_code: status })
    const wrapper = mount(TogetherBillingUsage, { props: { account } }); await query(wrapper)
    expect(wrapper.get('[role="alert"]').text()).toContain(reason)
    expect(wrapper.find('table').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('togetherUsage.empty')
  })
  it('rejects missing amounts and does not expose raw errors', async () => {
    const data = response(); Object.assign(data.usage!.data[0].line_items[0], { cost: undefined })
    queryTogetherUsage.mockResolvedValueOnce(data).mockRejectedValueOnce(new Error('private-key'))
    const wrapper = mount(TogetherBillingUsage, { props: { account } }); await query(wrapper)
    expect(wrapper.get('[role="alert"]').text()).toContain('invalid_response')
    await query(wrapper); expect(wrapper.get('[role="alert"]').text()).toContain('upstream_error'); expect(wrapper.text()).not.toContain('private-key')
  })
  it.each(['account', 'month', 'runtime'])('ignores late responses after %s changes', async change => {
    let resolve!: (value: TogetherBillingResult) => void
    queryTogetherUsage.mockImplementationOnce(() => new Promise<TogetherBillingResult>(done => { resolve = done }))
    const wrapper = mount(TogetherBillingUsage, { props: { account } })
    await wrapper.get('[data-test="together-query"]').trigger('click')
    expect(wrapper.attributes('aria-busy')).toBe('true')
    if (change === 'runtime') await wrapper.setProps({ account: { ...account, runtime_identity: 'rotated' } })
    if (change === 'account') await wrapper.setProps({ account: { ...account, id: 13 } })
    if (change === 'month') await wrapper.get('[data-test="together-month"]').setValue('2026-05')
    resolve(response()); await flushPromises()
    expect(wrapper.find('table').exists()).toBe(false); expect(wrapper.attributes('aria-busy')).toBe('false')
  })
  it('preserves data on unrelated financial update', async () => {
    queryTogetherUsage.mockResolvedValue(response())
    const wrapper = mount(TogetherBillingUsage, { props: { account } }); await query(wrapper)
    await wrapper.setProps({ account: { ...account, updated_at: '2026-07-01T00:00:00Z' } })
    expect(wrapper.find('table').exists()).toBe(true)
  })
  it('does not label a missing local account as supplier beta access', async () => {
    queryTogetherUsage.mockRejectedValue({ response: { status: 404 } })
    const wrapper = mount(TogetherBillingUsage, { props: { account } }); await query(wrapper)
    expect(wrapper.get('[role="alert"]').text()).toContain('upstream_error')
    expect(wrapper.text()).not.toContain('not_enabled')
  })
  it('rejects invalid month and hides custom-origin accounts', async () => {
    const wrapper = mount(TogetherBillingUsage, { props: { account } })
    await wrapper.get('[data-test="together-month"]').setValue('')
    expect(wrapper.get('[data-test="together-query"]').attributes('disabled')).toBeDefined()
    await wrapper.setProps({ account: { ...account, credentials: { ...account.credentials, base_url: 'https://relay.example/v1' } } })
    expect(wrapper.find('[data-test="together-query"]').exists()).toBe(false)
  })
  it('retains a successful report when a same-identity refresh fails', async () => {
    queryTogetherUsage.mockResolvedValueOnce(response()).mockResolvedValueOnce({ success: false, reason: 'permission_denied' })
    const wrapper = mount(TogetherBillingUsage, { props: { account } }); await query(wrapper); await query(wrapper)
    expect(wrapper.text()).toContain('9007199254740993.123')
    expect(wrapper.text()).toContain('permission_denied')
    expect(wrapper.text()).toContain('previousReading')
  })

})
