import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ProviderManagementUsage from '../ProviderManagementUsage.vue'
import type { Account } from '@/types'
import type { ManagementUsageResult } from '@/api/admin/managementUsage'

const { queryManagementUsage } = vi.hoisted(() => ({ queryManagementUsage: vi.fn() }))
vi.mock('@/api/admin/managementUsage', async () => ({ ...await vi.importActual<typeof import('@/api/admin/managementUsage')>('@/api/admin/managementUsage'), queryManagementUsage }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
const azure = { id: 12, platform: 'azure_openai', type: 'apikey', credentials: { account_mode: 'payg' } } as Account
const github = { ...azure, platform: 'github_copilot' } as Account
function quota(): ManagementUsageResult {
  return { provider: 'azure_openai', kind: 'subscription_regional_quota', scope: 'subscription_region', success: true, partial: false, fetched_at: 1, persisted: false,
    quota: { subscription_id: 'subscription', region: 'eastus', pages_fetched: 1, has_more: false, value: [{ name: { value: 'model-quota', localizedValue: 'Tokens Per Minute (thousands)' }, currentValue: 3, limit: 200, unit: 'Count' }] } }
}
function billing(): ManagementUsageResult {
  return { provider: 'github_copilot', kind: 'personal_billing_usage', scope: 'personal_account', success: true, partial: false, fetched_at: 1, persisted: false,
    billing: { username: 'octocat', year: 2026, month: 10, reports: ['premium_request', 'ai_credit'].map(report => ({
      report: report as 'premium_request' | 'ai_credit', success: true, timePeriod: { year: 2026, month: 10 }, user: 'octocat', usageItems: [{ product: 'Copilot', sku: report, model: 'model-name', unitType: report === 'ai_credit' ? 'ai-credits' : 'requests', pricePerUnit: 0.04, grossQuantity: 100, grossAmount: 4, discountQuantity: 25, discountAmount: 1, netQuantity: 75, netAmount: 3 }]
    })) } }
}
async function query(wrapper: ReturnType<typeof mount>) { await wrapper.get('[data-test="management-query"]').trigger('click'); await flushPromises() }

describe('ProviderManagementUsage', () => {
  beforeEach(() => { queryManagementUsage.mockReset(); vi.useFakeTimers({ toFake: ['Date'] }); vi.setSystemTime(new Date('2026-10-04T12:00:00Z')) })
  afterEach(() => { vi.useRealTimers() })
  it('only queries manually and preserves raw regional quota units', async () => {
    queryManagementUsage.mockResolvedValue(quota())
    const wrapper = mount(ProviderManagementUsage, { props: { account: azure } })
    expect(queryManagementUsage).not.toHaveBeenCalled()
    await query(wrapper)
    expect(queryManagementUsage).toHaveBeenCalledWith(12, undefined)
    expect(wrapper.text()).toContain('azureScope')
    expect(wrapper.text()).toContain('subscription')
    expect(wrapper.text()).toContain('Count')
    expect(wrapper.text()).toContain('200')
    expect(wrapper.text()).not.toMatch(/USD|3000|197/)
    expect(wrapper.findAll('table')).toHaveLength(1)
  })
  it('shows unknown missing/null numbers rather than zero', async () => {
    const result = quota(); result.quota!.value[0].currentValue = null; result.quota!.value[0].limit = null
    queryManagementUsage.mockResolvedValue(result)
    const wrapper = mount(ProviderManagementUsage, { props: { account: azure } }); await query(wrapper)
    expect(wrapper.findAll('td').map(cell => cell.text())).toEqual(['admin.accounts.managementUsage.unknown', 'admin.accounts.managementUsage.unknown', 'Count'])
  })
  it('renders independent billing reports and all original quantities and amounts', async () => {
    queryManagementUsage.mockResolvedValue(billing())
    const wrapper = mount(ProviderManagementUsage, { props: { account: github } }); await query(wrapper)
    expect(queryManagementUsage).toHaveBeenCalledWith(12, { year: 2026, month: 10 })
    expect(wrapper.findAll('table')).toHaveLength(2)
    expect(wrapper.text()).toContain('githubScope')
    expect(wrapper.text()).toContain('githubUnits')
    expect(wrapper.text()).toContain('pricePerUnit')
    expect(wrapper.text()).toContain('discountQuantity')
    expect(wrapper.text()).not.toContain('USD')
    expect(wrapper.findAll('tbody tr')[0].findAll('td').map(cell => cell.text())).toEqual(['Copilot', 'premium_request', 'model-name', 'requests', '0.04', '100', '4', '25', '1', '75', '3'])
  })
  it('keeps successful report visible when the other report fails', async () => {
    const result = billing(); result.success = false; result.partial = true; result.reason = 'report_failed'
    result.billing!.reports[1] = { report: 'ai_credit', success: false, reason: 'permission_denied', status_code: 403, usageItems: null }
    queryManagementUsage.mockResolvedValue(result)
    const wrapper = mount(ProviderManagementUsage, { props: { account: github } }); await query(wrapper)
    expect(wrapper.findAll('table')).toHaveLength(1)
    expect(wrapper.text()).toContain('managementUsage.partial')
    expect(wrapper.text()).toContain('reasons.permission_denied')
    expect(wrapper.text()).toContain('report_failed')
  })
  it('marks bounded Azure pages partial', async () => {
    const result = quota(); result.success = false; result.partial = true; result.reason = 'pagination_limit'; result.quota!.has_more = true
    queryManagementUsage.mockResolvedValue(result)
    const wrapper = mount(ProviderManagementUsage, { props: { account: azure } }); await query(wrapper)
    expect(wrapper.text()).toContain('pagination_limit')
    expect(wrapper.text()).toContain('managementUsage.partial')
    expect(wrapper.find('table').exists()).toBe(true)
  })
  it('displays missing query credentials without needing an inference key', async () => {
    queryManagementUsage.mockResolvedValue({ ...quota(), success: false, quota: undefined, reason: 'missing_query_credentials' })
    const wrapper = mount(ProviderManagementUsage, { props: { account: azure } }); await query(wrapper)
    expect(wrapper.text()).toContain('missing_query_credentials')
    expect(wrapper.find('table').exists()).toBe(false)
  })
  it.each(['2024-10', '2026-11', ''])('rejects outside 24-month window %s', async month => {
    const wrapper = mount(ProviderManagementUsage, { props: { account: github } })
    await wrapper.get('input').setValue(month)
    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('invalidMonth')
    expect(queryManagementUsage).not.toHaveBeenCalled()
  })
  it('discards responses after month changes and permits a new query', async () => {
    let finish!: (result: ManagementUsageResult) => void
    queryManagementUsage.mockReturnValueOnce(new Promise(done => { finish = done }))
    const wrapper = mount(ProviderManagementUsage, { props: { account: github } }); await query(wrapper)
    await wrapper.get('input').setValue('2026-09')
    finish(billing()); await flushPromises()
    expect(wrapper.find('table').exists()).toBe(false)
    const next = billing(); next.billing!.month = 9; next.billing!.reports.forEach(report => { report.timePeriod!.month = 9 })
    queryManagementUsage.mockResolvedValue(next); await query(wrapper)
    expect(wrapper.text()).toContain('2026-09')
    expect(wrapper.findAll('table')).toHaveLength(2)
  })
  it('discards responses after account switches', async () => {
    let finish!: (result: ManagementUsageResult) => void
    queryManagementUsage.mockReturnValue(new Promise(done => { finish = done }))
    const wrapper = mount(ProviderManagementUsage, { props: { account: azure } }); await query(wrapper)
    await wrapper.setProps({ account: { ...azure, id: 13 } }); finish(quota()); await flushPromises()
    expect(wrapper.find('table').exists()).toBe(false)
  })
  it('retains reports through unrelated account health timestamp updates', async () => {
    queryManagementUsage.mockResolvedValue(quota())
    const wrapper = mount(ProviderManagementUsage, { props: { account: azure } }); await query(wrapper)
    await wrapper.setProps({ account: { ...azure, updated_at: '2026-10-04T13:00:00Z' } })
    expect(wrapper.find('table').exists()).toBe(true)
  })
  it('invalidates a report when query metadata changes', async () => {
    queryManagementUsage.mockResolvedValue(quota())
    const wrapper = mount(ProviderManagementUsage, { props: { account: azure } }); await query(wrapper)
    await wrapper.setProps({ account: { ...azure, credentials: { usage_query_region: 'westus' } } })
    expect(wrapper.find('table').exists()).toBe(false)
  })
  it('distinguishes an empty report from failure and sanitizes errors', async () => {
    const result = quota(); result.quota!.value = []
    queryManagementUsage.mockResolvedValueOnce(result).mockRejectedValueOnce(new Error('secret-token'))
    const wrapper = mount(ProviderManagementUsage, { props: { account: azure } }); await query(wrapper)
    expect(wrapper.text()).toContain('noItems'); await query(wrapper)
    expect(wrapper.text()).toContain('upstream_error'); expect(wrapper.text()).not.toContain('secret-token')
    expect(wrapper.text()).not.toContain('noItems')
  })
  it.each(['number', 'scope', 'month', 'false-success'])('rejects malformed report %s', async change => {
    const result = billing()
    if (change === 'number') Object.assign(result.billing!.reports[0].usageItems![0], { netAmount: '3' })
    if (change === 'scope') Object.assign(result, { scope: 'organization' })
    if (change === 'month') result.billing!.month = 9
    if (change === 'false-success') result.billing!.reports[0] = { report: 'premium_request', success: false, usageItems: null }
    queryManagementUsage.mockResolvedValue(result)
    const wrapper = mount(ProviderManagementUsage, { props: { account: github } }); await query(wrapper)
    expect(wrapper.text()).toContain('invalid_response'); expect(wrapper.find('table').exists()).toBe(false)
  })
  it.each([{ ...azure, platform: 'other' }, { ...azure, type: 'oauth' }, { ...azure, credentials: { account_mode: 'coding' } }])('hides unsupported provider/type/mode', account => {
    const wrapper = mount(ProviderManagementUsage, { props: { account: account as Account } })
    expect(wrapper.find('button').exists()).toBe(false)
  })
})
