import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AccountUsageCell from '../AccountUsageCell.vue'
import { apiClient } from '@/api/client'
import type { Account } from '@/types'

vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }) }))
const originalAdapter = apiClient.defaults.adapter
const urls: string[] = []
function account(platform: string, credentials: Record<string, unknown> = {}): Account {
  return { id: 456, name: 'fixture', platform, type: 'apikey', credentials: { account_mode: 'payg', ...credentials }, credentials_status: { has_api_key: true }, runtime_identity: 'initial', extra: {} } as Account
}
function respond(body: unknown) {
  apiClient.defaults.adapter = async config => {
    urls.push(apiClient.getUri(config))
    return { status: 200, statusText: 'OK', headers: {}, config, data: { code: 0, data: body } }
  }
}
beforeEach(() => { urls.length = 0; localStorage.clear(); sessionStorage.clear() })
afterEach(() => { apiClient.defaults.adapter = originalAdapter })

describe('account list queries through real API boundary', () => {
  it.each(['azure_openai', 'github_copilot'])('mounts %s and reports missing credentials without inventing a balance', async platform => {
    const wrapper = mount(AccountUsageCell, { props: { account: account(platform) } })
    await flushPromises(); expect(urls).toEqual([])
    respond({ provider: platform, kind: platform === 'azure_openai' ? 'subscription_regional_quota' : 'personal_billing_usage', scope: platform === 'azure_openai' ? 'subscription_region' : 'personal_account', success: false, partial: false, persisted: false, reason: 'missing_query_credentials' })
    await wrapper.get('[data-test="management-query"]').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('missing_query_credentials')
    const query = new URL(urls[0], 'https://local.example').searchParams
    expect(query.has('timezone')).toBe(false)
    expect([...query.keys()].sort()).toEqual(platform === 'azure_openai' ? [] : ['month', 'year'])
    wrapper.unmount()
  })
  it('keeps Atlas balance and model usage independently visible', async () => {
    const wrapper = mount(AccountUsageCell, { props: { account: account('atlascloud') } })
    expect(wrapper.find('[data-test="cn-provider-balance-probe"]').exists()).toBe(true)
    respond({ provider: 'atlascloud', kind: 'consumed_usage', success: false, reason: 'permission_denied' })
    await wrapper.get('[data-test="atlas-query"]').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('permission_denied')
    expect([...new URL(urls[0], 'https://local.example').searchParams.keys()].sort()).toEqual(['end_date', 'start_date'])
    wrapper.unmount()
  })
  it('mounts TheRouter account credits with a missing-management-token explanation', async () => {
    const wrapper = mount(AccountUsageCell, { props: { account: account('therouter') } })
    expect(wrapper.text()).toContain('missing_query_credentials')
    respond({ provider: 'therouter', kind: 'account_credits', success: true, credits: { total_credits: 25, total_usage: 8, unit: 'credits' } })
    await wrapper.get('[data-test="cn-provider-balance-probe"]').trigger('click'); await flushPromises()
    expect(urls[0]).toContain('/balance'); expect(wrapper.text()).toContain('25')
    expect(wrapper.text()).toContain('totalUsage'); expect(wrapper.text()).not.toContain('17.00')
    wrapper.unmount()
  })
  it('mounts Together and keeps only its month/granularity parameters', async () => {
    const wrapper = mount(AccountUsageCell, { props: { account: account('together') } })
    respond({ provider: 'together', kind: 'billing_usage', success: false, reason: 'not_enabled' })
    await wrapper.get('[data-test="together-query"]').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('not_enabled')
    expect([...new URL(urls[0], 'https://local.example').searchParams.keys()].sort()).toEqual(['granularity', 'month'])
    wrapper.unmount()
  })
  it('preserves a management report across finance updates and errors, rejects hidden-rotation races', async () => {
    const initial = account('azure_openai')
    const wrapper = mount(AccountUsageCell, { props: { account: initial } })
    const result = { provider: 'azure_openai', kind: 'subscription_regional_quota', scope: 'subscription_region', success: true, partial: false, persisted: false, quota: { subscription_id: 'fixture-subscription', region: 'eastus', pages_fetched: 1, has_more: false, value: [] } }
    respond(result); await wrapper.get('[data-test="management-query"]').trigger('click'); await flushPromises()
    await wrapper.setProps({ account: { ...initial, updated_at: '2099-01-01', extra: { total_spend: 99 } } })
    expect(wrapper.text()).toContain('fixture-subscription')
    respond({ ...result, success: false, quota: undefined, reason: 'permission_denied' })
    await wrapper.get('[data-test="management-query"]').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('fixture-subscription'); expect(wrapper.text()).toContain('permission_denied')
    let resolve!: () => void
    apiClient.defaults.adapter = config => new Promise(done => { resolve = () => done({ status: 200, statusText: 'OK', headers: {}, config, data: { code: 0, data: result } }) })
    await wrapper.get('[data-test="management-query"]').trigger('click'); await flushPromises()
    await wrapper.setProps({ account: { ...initial, runtime_identity: 'rotated' } })
    resolve(); await flushPromises(); expect(wrapper.text()).not.toContain('fixture-subscription')
    wrapper.unmount()
  })
  it('renders Tencent exact package and call-count strings without wallet or window labels', async () => {
    respond({ provider: 'hunyuan', success: true, source: 'token_plan', token_plan: { product_type: 'enterprise', unit: 'credits', package_info: { total_quota: '123456789012345.00100', total_used: '0' }, token_summary: { billing_items: [{ billing_item: 'call_count', total_qty: '9007199254740993' }] } } })
    const wrapper = mount(AccountUsageCell, { props: { account: account('hunyuan', { account_mode: 'coding', base_url: 'https://tokenhub.tencentmaas.com/plan/v3' }) } })
    await flushPromises()
    expect(wrapper.get('[data-test="token-plan-package"]').text()).toContain('123456789012345.00100')
    expect(wrapper.get('[data-test="token-plan-billing"]').text()).toContain('call_count: 9007199254740993')
    expect(wrapper.find('[data-test="cn-provider-balance-probe"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="cn-provider-quota-tier"]').exists()).toBe(false)
    wrapper.unmount()
  })
})


it.each([
  ['volcengine', { base_url: 'https://ark.cn-beijing.volces.com/api/plan/v3', volcengine_plan_edition: 'personal' }],
  ['hunyuan', { base_url: 'https://api.hunyuan.cloud.tencent.com/v1' }],
  ['hunyuan', { base_url: 'https://tokenhub.tencentmaas.com/plan/v3', api_base_urls: { anthropic: 'https://relay.example/messages' } }]
])('does not expose a cloud query for unsupported %s profile %j', async (platform, credentials) => {
  const wrapper = mount(AccountUsageCell, { props: { account: account(platform as string, { ...credentials as Record<string, unknown>, account_mode: 'coding' }) } })
  await flushPromises()
  expect(wrapper.find('[data-test="cn-provider-quota-probe"]').exists()).toBe(false)
  expect(urls).toEqual([])
  wrapper.unmount()
})


it.each(['azure_openai', 'github_copilot'])('shows an unsupported state for non-API-key %s accounts without requests', async platform => {
  const wrapper = mount(AccountUsageCell, { props: { account: { ...account(platform), type: 'oauth' } } })
  await flushPromises()
  expect(wrapper.find('[data-test="management-query"]').exists()).toBe(false)
  expect(wrapper.find('[data-test="compatible-usage-unsupported"]').exists()).toBe(true)
  expect(urls).toEqual([])
  wrapper.unmount()
})


it('mounts BytePlus team quota with exact profile, and does not infer a wallet or personal entitlement', async () => {
  respond({ provider: 'byteplus', source: 'coding_plan', success: true, tiers: [{ window: '5h', used_percent: 12 }, { window: 'weekly', used_percent: 34 }] })
  const team = { ...account('byteplus', { account_mode: 'coding', billing_plan: 'team', byteplus_seat_id: 'seat-fixture' }), id: 9881 }
  const wrapper = mount(AccountUsageCell, { props: { account: team } }); await flushPromises()
  expect(wrapper.findAll('[data-test="cn-provider-quota-tier"]')).toHaveLength(2)
  expect(urls[0]).toContain('/9881/quota')
  expect(wrapper.find('[data-test="cn-provider-balance-probe"]').exists()).toBe(false)
  await wrapper.setProps({ account: { ...team, credentials: { ...team.credentials, billing_plan: 'personal' } } })
  expect(wrapper.find('[data-test="cn-provider-quota-probe"]').exists()).toBe(false)
  await wrapper.setProps({ account: { ...team, credentials: { ...team.credentials, base_url: 'https://ark.cn-beijing.volces.com/api/coding/v3' } } })
  expect(wrapper.find('[data-test="cn-provider-quota-probe"]').exists()).toBe(false)
  expect(urls).toHaveLength(1)
  wrapper.unmount()
})


it('clears CN quota history on a server identity rejection before the refreshed row arrives', async () => {
  const quotaAccount = { ...account('minimax', { account_mode: 'coding', base_url: 'https://api.minimax.cn/v1' }), id: 99881, extra: { minimax_5h_used_percent: 18, minimax_usage_updated_at: new Date().toISOString() } }
  const wrapper = mount(AccountUsageCell, { props: { account: quotaAccount } }); await flushPromises()
  expect(wrapper.findAll('[data-test="cn-provider-quota-tier"]')).toHaveLength(1)
  respond({ provider: 'minimax', success: false, status: 'identity_changed', tiers: [{ window: '5h', used_percent: 99 }] })
  await wrapper.get('[data-test="cn-provider-quota-probe"]').trigger('click'); await flushPromises()
  expect(wrapper.findAll('[data-test="cn-provider-quota-tier"]')).toHaveLength(0)
  expect(wrapper.text()).toContain('identity_changed')
  expect(wrapper.text()).not.toContain('99%')
  wrapper.unmount()
})


it('shows a configurable team Agent query without a seat, with no automatic request', async () => {
  const wrapper = mount(AccountUsageCell, { props: { account: { ...account('volcengine', { account_mode: 'coding', base_url: 'https://ark.cn-beijing.volces.com/api/plan/v3', volcengine_plan_edition: 'team' }), id: 99882 } } })
  await flushPromises()
  expect(wrapper.find('[data-test="cn-provider-quota-probe"]').exists()).toBe(true)
  expect(wrapper.text()).toContain('no_seat')
  expect(wrapper.text()).not.toContain('agentUnverified')
  expect(urls).toEqual([])
  respond({ provider: 'volcengine', success: false, status: 'missing_configuration' })
  await wrapper.get('[data-test="cn-provider-quota-probe"]').trigger('click'); await flushPromises()
  expect(urls).toHaveLength(1)
  expect(wrapper.text()).toContain('no_seat')
  expect(wrapper.find('[data-test="cn-provider-quota-tier"]').exists()).toBe(false)
  wrapper.unmount()
})
