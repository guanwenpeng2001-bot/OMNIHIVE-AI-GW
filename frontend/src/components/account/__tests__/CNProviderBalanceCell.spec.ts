import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import CNProviderBalanceCell from '../CNProviderBalanceCell.vue'
import type { Account } from '@/types'

const { queryBalance } = vi.hoisted(() => ({
  queryBalance: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    cnProviders: { queryBalance }
  }
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

const account = {
  id: 7,
  platform: 'kimi',
  type: 'apikey',
  credentials: { account_mode: 'payg' },
  extra: {
    kimi_balance: 12.5,
    kimi_balance_currency: 'CNY'
  }
} as Account

describe('CNProviderBalanceCell', () => {
  beforeEach(() => {
    queryBalance.mockReset()
  })

  it('renders the persisted balance as static text with an explicit query action', async () => {
    const wrapper = mount(CNProviderBalanceCell, { props: { account } })
    await flushPromises()

    // Snapshot value renders without any probe.
    expect(queryBalance).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="cn-provider-balance-value"]').text()).toContain('CNY 12.50')

    // The control reads as an action; the i18n mock returns the key itself.
    const probeButton = wrapper.get('[data-test="cn-provider-balance-probe"]')
    expect(probeButton.text()).toBe('admin.accounts.cnProviders.probe')

    await probeButton.trigger('click')
    await flushPromises()
    expect(queryBalance).toHaveBeenCalledWith(account.id)
  })

  it('shows the low-balance badge from the snapshot marker', () => {
    const lowAccount = {
      ...account,
      extra: { kimi_balance: 0.4, kimi_balance_low: true }
    } as Account

    const wrapper = mount(CNProviderBalanceCell, { props: { account: lowAccount } })

    expect(wrapper.text()).toContain('admin.accounts.cnProviders.balanceLow')
  })

  it('keeps the snapshot balance visible when a query fails', async () => {
    queryBalance.mockResolvedValue({ success: false, error: 'HTTP 401' })
    const wrapper = mount(CNProviderBalanceCell, { props: { account } })

    await wrapper.get('[data-test="cn-provider-balance-probe"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('CNY 12.50')
    expect(wrapper.text()).toContain('HTTP 401')
  })

  it('queries a compatible account and renders legitimate zero', async () => {
    queryBalance.mockResolvedValue({ success: true, balance: 0, currency: 'USD', balances: [{ currency: 'USD', balance: 0 }] })
    const wrapper = mount(CNProviderBalanceCell, { props: { account: { ...account, platform: 'novita', extra: {} } as Account } })
    await wrapper.get('[data-test="cn-provider-balance-probe"]').trigger('click')
    await flushPromises()
    expect(queryBalance).toHaveBeenCalledWith(account.id)
    expect(wrapper.text()).toContain('USD 0.00')
  })

  it('preserves every currency in the existing balance response contract', async () => {
    queryBalance.mockResolvedValue({ success: true, balances: [{ currency: 'CNY', balance: 2 }, { currency: 'USD', balance: 3 }] })
    const wrapper = mount(CNProviderBalanceCell, { props: { account } })
    await wrapper.get('[data-test="cn-provider-balance-probe"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('CNY 2.00 · USD 3.00')
  })

  it('does not turn unsupported results into zero', async () => {
    queryBalance.mockResolvedValue({ success: false, unsupported: true, error: 'Unsupported balance scope' })
    const wrapper = mount(CNProviderBalanceCell, { props: { account: { ...account, platform: 'shengsuanyun', extra: {} } as Account } })
    await wrapper.get('[data-test="cn-provider-balance-probe"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('admin.accounts.compatibleUsage.reasons.no_verified_query')
    expect(wrapper.text()).not.toContain('0.00')
  })

  it('ignores a balance reply after the account credentials change', async () => {
    let resolve!: (value: unknown) => void
    queryBalance.mockReturnValue(new Promise(done => { resolve = done }))
    const compatible = { ...account, platform: 'novita', extra: {} } as Account
    const wrapper = mount(CNProviderBalanceCell, { props: { account: compatible } })
    await wrapper.get('[data-test="cn-provider-balance-probe"]').trigger('click')
    await wrapper.setProps({ account: { ...compatible, credentials: { api_key: 'new-fixture-key' } } as Account })
    resolve({ success: true, balance: 900, currency: 'USD' })
    await flushPromises()
    expect(wrapper.text()).not.toContain('USD 900')
  })
})


describe('compatible wallet and budget semantics', () => {
  it.each([
    ['teamorouter', 'USD'], ['teamorouter_com', 'EUR'], ['sudocode_chat', 'Credits']
  ])('queries %s and displays the response unit %s', async (platform, currency) => {
    queryBalance.mockResolvedValue({ success: true, balance: 0, currency, balances: [{ currency, balance: 0 }] })
    const wrapper = mount(CNProviderBalanceCell, { props: { account: { ...account, platform, extra: {} } as Account } })
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(queryBalance).toHaveBeenLastCalledWith(account.id)
    expect(wrapper.get('[data-test="cn-provider-balance-value"]').text()).toBe(`${currency} 0.00`)
  })
  it.each(['stepfun_api', 'stepfun_api_intl'])('renders %s live zero without inventing currency', async platform => {
    queryBalance.mockResolvedValue({ success: true, kind: 'wallet_balance', scope: 'account', balance: 0, balances: [{ currency: '', balance: 0 }] })
    const wrapper = mount(CNProviderBalanceCell, { props: { account: { ...account, platform, credentials_status: { has_api_key: true }, extra: {} } as Account } })
    await wrapper.get('[data-test="cn-provider-balance-probe"]').trigger('click')
    await flushPromises()
    expect(queryBalance).toHaveBeenLastCalledWith(account.id)
    expect(wrapper.get('[data-test="cn-provider-balance-value"]').text()).toBe('0.00')
    expect(wrapper.text()).toContain('compatibleUsage.accountBalance')
    expect(wrapper.text()).not.toMatch(/USD|CNY|¥|\$/)
  })
  it.each(['stepfun_api', 'stepfun_api_intl'])('preserves %s unknown-currency snapshots on failure', async platform => {
    for (const extra of [
      { [`${platform}_balance`]: 12.5 },
      { [`${platform}_balances`]: [{ currency: '', balance: 12.5 }] }
    ]) {
      queryBalance.mockResolvedValue({ success: false, reason: 'invalid_response' })
      const wrapper = mount(CNProviderBalanceCell, { props: { account: { ...account, platform, extra } as Account } })
      expect(wrapper.get('[data-test="cn-provider-balance-value"]').text()).toBe('12.50')
      await wrapper.get('button').trigger('click')
      await flushPromises()
      expect(wrapper.get('[data-test="cn-provider-balance-value"]').text()).toBe('12.50')
      expect(wrapper.get('[role="alert"]').text()).toContain('invalid_response')
      expect(wrapper.text()).toContain('compatibleUsage.stale')
      expect(wrapper.text()).not.toMatch(/USD|CNY|¥|\$/)
      wrapper.unmount()
    }
  })
  it.each(['stepfun_api', 'stepfun_api_intl'])('does not display %s failure as zero', async platform => {
    queryBalance.mockResolvedValue({ success: false, status_code: 401 })
    const wrapper = mount(CNProviderBalanceCell, { props: { account: { ...account, platform, extra: {} } as Account } })
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="cn-provider-balance-value"]').text()).toBe('admin.accounts.cnProviders.balance')
    expect(wrapper.get('[role="alert"]').text()).toContain('invalid_credentials')
    expect(wrapper.text()).not.toContain('0.00')
  })
  it.each(['stepfun_plan', 'stepfun_plan_intl'])('hides the query control for %s', platform => {
    const wrapper = mount(CNProviderBalanceCell, { props: { account: { ...account, platform, extra: {} } as Account } })
    expect(wrapper.find('button').exists()).toBe(false)
  })
  it.each([false, true])('renders a project budget, unlimited=%s, without an old wallet amount', async unlimited => {
    queryBalance.mockResolvedValue({ success: true, kind: 'project_budget', project_budget: {
      consumed: 0, limit: null, unlimited, currency: 'CNY', period_kind: 'monthly',
      period_start: '2026-10-01T00:00:00+08:00', period_end: '2026-11-01T00:00:00+08:00'
    } })
    const wrapper = mount(CNProviderBalanceCell, { props: { account: { ...account, platform: 'shengsuanyun', extra: { shengsuanyun_balance: 888 } } as Account } })
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="project-budget"]').text()).toContain('consumed: 0 CNY')
    expect(wrapper.text()).toContain(unlimited ? 'compatibleUsage.unlimited' : 'limit: —')
    expect(wrapper.text()).not.toContain('888')
    expect(wrapper.find('[data-test="cn-provider-balance-value"]').exists()).toBe(false)
  })
  it.each([[403, 'permission_denied'], [429, 'rate_limited'], [500, 'upstream_error']])('distinguishes status %s and retains a labeled snapshot', async (status_code, reason) => {
    queryBalance.mockResolvedValue({ success: false, status_code })
    const wrapper = mount(CNProviderBalanceCell, { props: { account: { ...account, platform: 'atlascloud', extra: { atlascloud_balance: 4, atlascloud_balance_currency: 'USD' } } as Account } })
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('USD 4.00')
    expect(wrapper.text()).toContain('compatibleUsage.stale')
    expect(wrapper.get('[role="alert"]').text()).toContain(reason)
    expect(wrapper.text()).toContain('compatibleUsage.accountBalance')
  })
})


describe('management balance presentation', () => {
  beforeEach(() => queryBalance.mockReset())
  const managed = (platform: string) => ({ ...account, platform, credentials: { usage_query_user_id: '42', credential_secret_version: 1 }, credentials_status: { has_api_key: false, has_usage_query_token: true }, extra: {} } as Account)
  it('shows missing configuration while retaining the query action', async () => {
    const candidate = { ...managed('dmxapi'), credentials_status: { has_usage_query_token: false } }
    const wrapper = mount(CNProviderBalanceCell, { props: { account: candidate } })
    expect(wrapper.text()).toContain('missing_query_credentials')
    queryBalance.mockResolvedValue({ success: false, kind: 'wallet_balance', reason: 'missing_query_credentials' })
    await wrapper.get('[data-test="cn-provider-balance-probe"]').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('missing_query_credentials')
    expect(wrapper.text()).not.toContain('0.00')
  })
  it('renders both raw credits values without a wallet or currency', async () => {
    queryBalance.mockResolvedValue({ success: true, kind: 'account_credits', credits: { total_credits: 950.5, total_usage: 49.5, unit: 'credits' } })
    const candidate = { ...managed('therouter'), extra: { therouter_balance: 901, therouter_balance_currency: 'USD' } }
    const wrapper = mount(CNProviderBalanceCell, { props: { account: candidate } })
    expect(wrapper.text()).not.toContain('901')
    await wrapper.get('[data-test="cn-provider-balance-probe"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-test="account-credits"]').text()).toContain('950.5 credits')
    expect(wrapper.get('[data-test="account-credits"]').text()).toContain('49.5 credits')
    expect(wrapper.text()).not.toContain('USD'); expect(wrapper.text()).not.toContain('901')
  })
  it('displays reported used amounts including zero', async () => {
    queryBalance.mockResolvedValue({ success: true, kind: 'wallet_balance', balance: 0, currency: 'USD', used: 0 })
    const wrapper = mount(CNProviderBalanceCell, { props: { account: managed('aihubmix') } })
    await wrapper.get('[data-test="cn-provider-balance-probe"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-test="management-used"]').text()).toContain('0 USD')
    expect(wrapper.get('[data-test="cn-provider-balance-value"]').text()).toBe('USD 0.00')
    await wrapper.setProps({ account: { ...managed('aihubmix'), updated_at: '2099-01-01', extra: { total_spend: 50 } } })
    expect(wrapper.get('[data-test="cn-provider-balance-value"]').text()).toBe('USD 0.00')
  })
  it.each(['user', 'tokenVersion', 'hasToken', 'runtime'])('suppresses old snapshots and an in-flight result after %s changes', async change => {
    let resolve!: (value: unknown) => void
    queryBalance.mockReturnValue(new Promise(r => { resolve = r }))
    const candidate = { ...managed('crazyrouter'), extra: { crazyrouter_balances: [{ currency: 'USD', balance: 18 }, { currency: 'CNY', balance: 20 }] } }
    const wrapper = mount(CNProviderBalanceCell, { props: { account: candidate } })
    expect(wrapper.text()).toContain('USD 18.00'); expect(wrapper.text()).toContain('CNY 20.00')
    await wrapper.get('[data-test="cn-provider-balance-probe"]').trigger('click')
    const next = { ...candidate, credentials: { ...candidate.credentials }, credentials_status: { ...candidate.credentials_status } }
    if (change === 'runtime') Object.assign(next, { runtime_identity: 'rotated' })
    if (change === 'user') next.credentials.usage_query_user_id = '43'
    if (change === 'tokenVersion') next.credentials.credential_secret_version = 2
    if (change === 'hasToken') next.credentials_status.has_usage_query_token = false
    await wrapper.setProps({ account: next })
    resolve({ success: true, balance: 999, currency: 'USD' }); await flushPromises()
    expect(wrapper.text()).not.toContain('18.00'); expect(wrapper.text()).not.toContain('20.00'); expect(wrapper.text()).not.toContain('999')
  })
})


it('clears old identity history and snapshot when the server reports rotation before the new DTO arrives', async () => {
  const candidate = { ...account, platform: 'aihubmix', credentials: { account_mode: 'payg' }, credentials_status: { has_usage_query_token: true }, extra: { aihubmix_balance: 18, aihubmix_balance_currency: 'USD' } } as Account
  const wrapper = mount(CNProviderBalanceCell, { props: { account: candidate } })
  queryBalance.mockResolvedValueOnce({ success: true, balance: 25, currency: 'USD' }).mockResolvedValueOnce({ success: false, reason: 'identity_changed', balance: 999, currency: 'USD' })
  await wrapper.get('[data-test="cn-provider-balance-probe"]').trigger('click'); await flushPromises()
  expect(wrapper.text()).toContain('25.00')
  await wrapper.get('[data-test="cn-provider-balance-probe"]').trigger('click'); await flushPromises()
  expect(wrapper.text()).toContain('identity_changed')
  for (const amount of ['18.00', '25.00', '999']) expect(wrapper.text()).not.toContain(amount)
  wrapper.unmount()
})
