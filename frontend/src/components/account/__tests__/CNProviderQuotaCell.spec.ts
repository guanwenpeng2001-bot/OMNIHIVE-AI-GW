import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import CNProviderQuotaCell from '../CNProviderQuotaCell.vue'
import UsageProgressBar from '../UsageProgressBar.vue'
import type { Account } from '@/types'
import englishAccounts from '@/i18n/locales/en/admin/accounts'
import chineseAccounts from '@/i18n/locales/zh/admin/accounts'

const { queryQuota, translate } = vi.hoisted(() => ({
  translate: vi.fn((key: string) => key),
  queryQuota: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    cnProviders: { queryQuota }
  }
}))

// 保留 vue-i18n 真实导出：UsageProgressBar 依赖 @/utils/format → @/i18n，
// 其模块级 createI18n 需要真实 createI18n 存在。
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: translate
    })
  }
})

const account = {
  id: 7,
  platform: 'zhipu',
  type: 'apikey',
  credentials: { account_mode: 'coding' },
  extra: {
    zhipu_5h_used_percent: 0,
    zhipu_weekly_used_percent: 27,
    zhipu_5h_reset_at: '2026-08-18T12:30:00+08:00',
    zhipu_weekly_reset_at: '2026-08-22T00:00:00+08:00',
    zhipu_usage_updated_at: new Date().toISOString()
  }
} as Account

describe('CNProviderQuotaCell', () => {
  beforeEach(() => {
    queryQuota.mockReset()
  })

  it('renders tier rows through the shared UsageProgressBar inside the account table cell', async () => {
    queryQuota.mockResolvedValue({
      success: true,
      tiers: [
        { window: '5h', used_percent: 0, reset_at: '2026-08-18T12:30:00+08:00' },
        { window: 'weekly', used_percent: 27, reset_at: '2026-08-22T00:00:00+08:00' }
      ]
    })
    const wrapper = mount(CNProviderQuotaCell, { props: { account } })

    const root = wrapper.get('[data-test="cn-provider-quota"]')
    expect(root.classes()).toContain('min-w-[220px]')

    // 新鲜快照：挂载即渲染条形图，不触发探测
    await flushPromises()
    expect(queryQuota).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('27%')

    // probe 按钮文案是动词 key（i18n mock 返回 key 本身），点击触发查询
    const probeButton = root.get('[data-test="cn-provider-quota-probe"]')
    expect(probeButton.text()).toBe('admin.accounts.cnProviders.probe')
    await probeButton.trigger('click')
    await flushPromises()
    expect(queryQuota).toHaveBeenCalledWith(account.id)

    // tier 行由 UsageProgressBar 渲染：数量、label/color/utilization/reset 逐行对齐
    expect(root.findAll('[data-test="cn-provider-quota-tier"]')).toHaveLength(2)
    const bars = root.findAllComponents(UsageProgressBar)
    expect(bars).toHaveLength(2)
    expect(bars[0].props('label')).toBe('admin.accounts.cnProviders.window5h')
    expect(bars[0].props('utilization')).toBe(0)
    expect(bars[0].props('color')).toBe('indigo')
    expect(bars[0].props('resetsAt')).toBe('2026-08-18T12:30:00+08:00')
    expect(bars[1].props('label')).toBe('admin.accounts.cnProviders.windowWeekly')
    expect(bars[1].props('utilization')).toBe(27)
    expect(bars[1].props('color')).toBe('emerald')
    expect(bars[1].props('resetsAt')).toBe('2026-08-22T00:00:00+08:00')
  })

  it('labels the refresh control with an explicit action verb, not a data caption', async () => {
    const wrapper = mount(CNProviderQuotaCell, { props: { account } })
    await flushPromises()

    // The snapshot is fresh (usage_updated_at = now): bars render without probing.
    expect(queryQuota).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('27%')

    // The control reads as an action ("query"), unlike the old noun label
    // ("5-hour window/weekly window") which looked like a passive caption.
    // The i18n mock returns the key itself.
    const probeButton = wrapper.get('[data-test="cn-provider-quota-probe"]')
    expect(probeButton.text()).toBe('admin.accounts.cnProviders.probe')

    await probeButton.trigger('click')
    await flushPromises()
    expect(queryQuota).toHaveBeenCalledWith(account.id)
  })
})

it('shows Command Code windows, credits and source from persisted data', async () => {
 const wrapper = mount(CNProviderQuotaCell, { props: { account: { ...account, id: 2020, platform: 'commandcode', credentials: {}, extra: { commandcode_5h_used_percent: 16.18, commandcode_weekly_used_percent: 37.1, commandcode_monthlyCredits: 57.01, commandcode_plan_level: 'individual-goat', commandcode_usage_updated_at: new Date().toISOString() } } as Account } })
 await flushPromises()
 expect(wrapper.findAllComponents(UsageProgressBar)).toHaveLength(2)
 expect(wrapper.text()).toContain('GOAT')
 expect(wrapper.text()).toContain('57.01')
 expect(wrapper.text()).toContain('admin.accounts.cnProviders.commandCodeAlpha')
 expect(wrapper.find('[data-test="commandcode-local-fallback"]').exists()).toBe(false)
})

it('removes stale Command Code bars and shows local fallback on unsupported response', async () => {
 queryQuota.mockResolvedValue({ success: false, unsupported: true })
 const wrapper = mount(CNProviderQuotaCell, { props: { account: { ...account, id: 2021, platform: 'commandcode', credentials: {}, extra: { commandcode_5h_used_percent: 42, commandcode_usage_updated_at: new Date().toISOString() } } as Account } })
 await wrapper.get('[data-test="cn-provider-quota-probe"]').trigger('click')
 await flushPromises()
 expect(wrapper.findAllComponents(UsageProgressBar)).toHaveLength(0)
 expect(wrapper.text()).toContain('admin.accounts.cnProviders.commandCodeUnsupported')
 expect(wrapper.find('[data-test="commandcode-local-fallback"]').exists()).toBe(true)
})

it('never renders numeric bars for persisted unsupported Command Code snapshots', async () => {
 const wrapper = mount(CNProviderQuotaCell, { props: { account: { ...account, id: 2022, platform: 'commandcode', credentials: {}, extra: { commandcode_usage_supported: false, commandcode_5h_used_percent: 42, commandcode_usage_updated_at: new Date().toISOString() } } as Account } })
 await flushPromises()
 expect(wrapper.findAllComponents(UsageProgressBar)).toHaveLength(0)
 expect(wrapper.find('[data-test="commandcode-local-fallback"]').exists()).toBe(true)
})


describe('CommandCode local usage wording and recovery', () => {
  beforeEach(() => {
    queryQuota.mockReset()
    translate.mockReset().mockImplementation((key: string) => key)
  })

  it.each([
    ['en', englishAccounts, /today/i, /temporarily unavailable/i, /cumulative/i],
    ['zh', chineseAccounts, /今日/, /暂不可用/, /累计/],
  ] as const)('describes CommandCode fallback as today using the real %s locale', async (locale, messages, today, unavailable, cumulative) => {
    translate.mockImplementation((key: string) => key === 'admin.accounts.cnProviders.commandCodeLocal'
      ? messages.accounts.cnProviders.commandCodeLocal
      : key)
    queryQuota.mockResolvedValue({ success: false, unsupported: true })
    const wrapper = mount(CNProviderQuotaCell, {
      props: { account: { ...account, id: locale === 'en' ? 2101 : 2102, platform: 'commandcode', credentials: {}, extra: {} } as Account }
    })
    await flushPromises()
    const fallback = wrapper.get('[data-test="commandcode-local-fallback"]').text()
    expect(fallback).toMatch(today)
    expect(fallback).toMatch(unavailable)
    expect(fallback).not.toMatch(cumulative)
    wrapper.unmount()
  })

  it('removes the fallback and restores real windows and credits after a successful manual probe', async () => {
    queryQuota.mockResolvedValueOnce({ success: false, unsupported: true })
    const wrapper = mount(CNProviderQuotaCell, {
      props: { account: { ...account, id: 2103, platform: 'commandcode', credentials: {}, extra: {} } as Account }
    })
    await flushPromises()
    expect(wrapper.find('[data-test="commandcode-local-fallback"]').exists()).toBe(true)
    queryQuota.mockResolvedValueOnce({ success: true, tiers: [{ window: '5h', used_percent: 18 }], credits: { monthlyCredits: 57.01 } })
    await wrapper.get('[data-test="cn-provider-quota-probe"]').trigger('click')
    await flushPromises()
    expect(queryQuota).toHaveBeenLastCalledWith(2103)
    expect(wrapper.find('[data-test="commandcode-local-fallback"]').exists()).toBe(false)
    expect(wrapper.findAllComponents(UsageProgressBar)).toHaveLength(1)
    expect(wrapper.text()).toContain('18%')
    expect(wrapper.text()).toContain('57.01')
    wrapper.unmount()
  })
})


describe('CommandCode independent plan and credit observations', () => {
  beforeEach(() => {
    queryQuota.mockReset()
    translate.mockReset().mockImplementation((key: string) => key)
  })
  it('renders a plan and genuine zero credits with no windows and leaves missing buckets absent', async () => {
    const wrapper = mount(CNProviderQuotaCell, { props: { account: {
      ...account, id: 2301, platform: 'commandcode', credentials: { api_key: 'fixture' },
      extra: { commandcode_plan_level: 'individual-goat', commandcode_monthlyCredits: 0,
        commandcode_usage_updated_at: new Date().toISOString() }
    } as Account } })
    await flushPromises()
    expect(queryQuota).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('GOAT')
    expect(wrapper.text()).toContain('monthlyCredits: 0.00')
    expect(wrapper.text()).not.toContain('purchasedCredits')
    expect(wrapper.findAllComponents(UsageProgressBar)).toHaveLength(0)
    expect(wrapper.find('[data-test="commandcode-local-fallback"]').exists()).toBe(false)
  })
  it('replaces missing plan and windows individually while retaining newly observed credits', async () => {
    queryQuota.mockResolvedValue({ success: true, credits: { purchasedCredits: 3 } })
    const wrapper = mount(CNProviderQuotaCell, { props: { account: {
      ...account, id: 2302, platform: 'commandcode', credentials: {}, extra: {
        commandcode_plan_level: 'individual-goat', commandcode_5h_used_percent: 42,
        commandcode_usage_updated_at: new Date().toISOString()
      }
    } as Account } })
    await wrapper.get('[data-test="cn-provider-quota-probe"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('GOAT')
    expect(wrapper.findAllComponents(UsageProgressBar)).toHaveLength(0)
    expect(wrapper.text()).toContain('purchasedCredits: 3.00')
  })
  it('ignores an in-flight observation after the key changes', async () => {
    let resolveProbe!: (value: unknown) => void
    queryQuota.mockReturnValue(new Promise(resolve => { resolveProbe = resolve }))
    const initial = { ...account, id: 2303, platform: 'commandcode', credentials: { api_key: 'old-fixture' }, extra: {} } as Account
    const wrapper = mount(CNProviderQuotaCell, { props: { account: initial } })
    await flushPromises()
    await wrapper.setProps({ account: { ...initial, credentials: { api_key: 'new-fixture' } } })
    resolveProbe({ success: true, plan_level: 'individual-goat', credits: { monthlyCredits: 57 } })
    await flushPromises()
    expect(wrapper.text()).not.toContain('GOAT')
    expect(wrapper.text()).not.toContain('57.00')
    expect(wrapper.find('[data-test="commandcode-local-fallback"]').exists()).toBe(true)
  })
})


it('clears the rendered plan when the server invalidates the credential snapshot', async () => {
  const initial = { ...account, id: 2304, platform: 'commandcode', credentials: {}, extra: {
    commandcode_plan_level: 'individual-goat', commandcode_monthlyCredits: 57,
    commandcode_usage_updated_at: new Date().toISOString()
  } } as Account
  const wrapper = mount(CNProviderQuotaCell, { props: { account: initial } })
  await flushPromises()
  expect(wrapper.text()).toContain('GOAT')
  await wrapper.setProps({ account: { ...initial, extra: {} } })
  expect(wrapper.text()).not.toContain('GOAT')
  expect(wrapper.text()).not.toContain('57.00')
})

// Synthetic observations exercise the frozen contract; these are not upstream captures.
describe('Volcengine upstream usage observations', () => {
  const codingAccount = (id: number, extra: Record<string, unknown> = {}) => ({
    ...account, id, platform: 'volcengine', credentials: { account_mode: 'coding', base_url: 'https://ark.cn-beijing.volces.com/api/coding/v3' }, extra
  } as Account)
  beforeEach(() => {
    queryQuota.mockReset()
    translate.mockReset().mockImplementation((key: string) => key)
  })
  it('shows genuine zero, overage, and each window reset with query source and time', async () => {
    const tiers = [
      { window: '5h', used_percent: 0, reset_at: '2099-10-04T15:00:00Z' },
      { window: 'weekly', used_percent: 123, reset_at: '2099-10-11T00:00:00Z' },
      { window: 'monthly', used_percent: 44, reset_at: '2099-11-01T00:00:00Z' }
    ]
    queryQuota.mockResolvedValue({ success: true, status: 'ok', source: 'coding_plan', fetched_at: 1791120000, tiers })
    const wrapper = mount(CNProviderQuotaCell, { props: { account: codingAccount(5101) } })
    await flushPromises()
    const bars = wrapper.findAllComponents(UsageProgressBar)
    expect(bars).toHaveLength(3)
    tiers.forEach((tier, index) => {
      expect(bars[index].props('utilization')).toBe(tier.used_percent)
      expect(bars[index].props('resetsAt')).toBe(tier.reset_at)
    })
    expect(wrapper.text()).toContain('123%')
    expect(wrapper.get('[data-test="quota-source"]').text()).toContain('volcengineQuota.source')
    expect(wrapper.get('[data-test="quota-observed-at"]').text()).not.toContain('Invalid Date')
    wrapper.unmount()
  })
  it.each(['missing_credentials', 'invalid_credentials', 'permission_denied', 'not_subscribed', 'no_seat', 'invalid_response', 'url_rejected'])('shows %s as unknown without manufacturing zero', async status => {
    queryQuota.mockResolvedValue({ success: false, status })
    const wrapper = mount(CNProviderQuotaCell, { props: { account: codingAccount(5200 + status.length) } })
    await flushPromises()
    expect(wrapper.findAllComponents(UsageProgressBar)).toHaveLength(0)
    expect(wrapper.text()).toContain('usageUnknown')
    expect(wrapper.text()).toContain(`volcengineQuota.status.${status}`)
    expect(wrapper.text()).not.toContain('0%')
    wrapper.unmount()
  })
  it('keeps the successful snapshot and its timestamp on failure, then replaces missing windows on recovery', async () => {
    const wrapper = mount(CNProviderQuotaCell, { props: { account: codingAccount(5301, {
      volcengine_5h_used_percent: 18, volcengine_weekly_used_percent: 0,
      volcengine_usage_updated_at: new Date().toISOString()
    }) } })
    await flushPromises()
    const previousTime = wrapper.get('[data-test="quota-observed-at"]').text()
    queryQuota.mockResolvedValueOnce({ success: false, status: 'permission_denied' })
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.findAllComponents(UsageProgressBar)).toHaveLength(2)
    expect(wrapper.get('[data-test="quota-observed-at"]').text()).toBe(previousTime)
    expect(wrapper.text()).toContain('permission_denied')
    queryQuota.mockResolvedValueOnce({ success: true, fetched_at: 1791120000, tiers: [{ window: 'monthly', used_percent: 0 }] })
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.findAllComponents(UsageProgressBar)).toHaveLength(1)
    expect(wrapper.text()).not.toContain('permission_denied')
    wrapper.unmount()
  })
  it('does not render malformed percentages as zero or invent missing windows', async () => {
    queryQuota.mockResolvedValue({ success: true, tiers: [{ window: '5h', used_percent: null }, { window: 'weekly', used_percent: NaN }, { window: 'monthly', used_percent: 0 }] })
    const wrapper = mount(CNProviderQuotaCell, { props: { account: codingAccount(5401) } })
    await flushPromises()
    expect(wrapper.findAllComponents(UsageProgressBar)).toHaveLength(1)
    expect(wrapper.findComponent(UsageProgressBar).props('label')).toContain('windowMonthly')
    wrapper.unmount()
  })
  it.each(['version', 'edition', 'seat'])('discards in-flight results after the %s changes', async change => {
    let finish!: (value: unknown) => void
    queryQuota.mockReturnValue(new Promise(resolve => { finish = resolve }))
    const initial = codingAccount(5450 + change.length, { volcengine_5h_used_percent: 18, volcengine_usage_updated_at: new Date().toISOString() })
    initial.updated_at = '2026-10-04T10:00:00Z'
    const wrapper = mount(CNProviderQuotaCell, { props: { account: initial } })
    await flushPromises()
    expect(wrapper.findAllComponents(UsageProgressBar)).toHaveLength(1)
    await wrapper.get('button').trigger('click')
    const updated = { ...initial, ...(change === 'version' ? { updated_at: '2026-10-04T10:01:00Z' } : {
      credentials: { ...initial.credentials, ...(change === 'edition' ? { volcengine_plan_edition: 'team' } : { volcengine_seat_id: 'another-synthetic-seat' }) }
    }) }
    await wrapper.setProps({ account: updated })
    finish({ success: true, tiers: [{ window: '5h', used_percent: 88 }] })
    await flushPromises()
    expect(wrapper.findAllComponents(UsageProgressBar)).toHaveLength(0)
    expect(wrapper.text()).not.toContain('88%')
    expect(wrapper.text()).toContain('usageUnknown')
    wrapper.unmount()
  })
  it.each(['https://ark.cn-beijing.volces.com/api/plan/v3', 'https://relay.example/api/coding/v3'])('never probes an unverified profile %s', async base_url => {
    const original = codingAccount(5501)
    const wrapper = mount(CNProviderQuotaCell, { props: { account: { ...original, credentials: { ...original.credentials, base_url } } } })
    await flushPromises()
    expect(queryQuota).not.toHaveBeenCalled()
    expect(wrapper.find('button').exists()).toBe(false)
    wrapper.unmount()
  })
})
