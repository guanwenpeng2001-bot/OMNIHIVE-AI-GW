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

it('shows Command Code windows, credits and alpha notice from persisted data', async () => {
 const wrapper = mount(CNProviderQuotaCell, { props: { account: { ...account, id: 2020, platform: 'commandcode', credentials: {}, extra: { commandcode_5h_used_percent: 16.18, commandcode_weekly_used_percent: 37.1, commandcode_monthlyCredits: 57.01, commandcode_plan_level: 'individual-goat', commandcode_usage_updated_at: new Date().toISOString() } } as Account } })
 await flushPromises()
 expect(wrapper.findAllComponents(UsageProgressBar)).toHaveLength(2)
 expect(wrapper.text()).toContain('individual-goat')
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
