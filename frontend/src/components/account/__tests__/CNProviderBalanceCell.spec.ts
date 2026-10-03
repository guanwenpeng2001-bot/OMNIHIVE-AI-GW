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
    expect(wrapper.text()).toContain('Unsupported balance scope')
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
