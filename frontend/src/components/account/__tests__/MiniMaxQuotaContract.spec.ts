import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import CNProviderQuotaCell from '../CNProviderQuotaCell.vue'
import UsageProgressBar from '../UsageProgressBar.vue'
import type { Account } from '@/types'

const { queryQuota } = vi.hoisted(() => ({ queryQuota: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { cnProviders: { queryQuota } } }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))

let accountId = 8300
function codingAccount(base: string, extra: Record<string, unknown> = {}): Account {
  return {
    id: ++accountId,
    platform: 'minimax',
    type: 'apikey',
    credentials: { account_mode: 'coding', api_key: 'fixture-plan-key', base_url: base },
    extra
  } as Account
}

const resetShort = '2030-10-04T18:00:00Z'
const resetWeekly = '2030-10-10T00:00:00Z'

describe('MiniMax CN token plan quota contract', () => {
  beforeEach(() => queryQuota.mockReset())

  it.each(['https://api.minimax.cn/v1', 'https://api.minimax.cn/anthropic'])(
    'queries a coding account at %s and renders standard tiers including true zero', async (base) => {
      // Mocked service response, not a live MiniMax query.
      queryQuota.mockResolvedValue({
        provider: 'minimax', source: 'coding_plan', success: true, credential_valid: true,
        persisted: true, fetched_at: Date.now() / 1000,
        tiers: [
          { window: '5h', used_percent: 0, reset_at: resetShort },
          { window: 'weekly', used_percent: 60, reset_at: resetWeekly }
        ]
      })
      const account = codingAccount(base)
      const wrapper = mount(CNProviderQuotaCell, { props: { account } })
      await flushPromises()
      expect(queryQuota).toHaveBeenCalledWith(account.id)
      expect(wrapper.find('[data-test="cn-provider-quota"]').exists()).toBe(true)
      const bars = wrapper.findAllComponents(UsageProgressBar)
      expect(bars).toHaveLength(2)
      expect(bars[0].props('label')).toBe('admin.accounts.cnProviders.window5h')
      expect(bars[0].props('utilization')).toBe(0)
      expect(bars[0].props('resetsAt')).toBe(resetShort)
      expect(bars[1].props('label')).toBe('admin.accounts.cnProviders.windowWeekly')
      expect(bars[1].props('utilization')).toBe(60)
      expect(bars[1].props('resetsAt')).toBe(resetWeekly)
      expect(wrapper.text()).toContain('0%')
      expect(wrapper.text()).toContain('60%')
      await wrapper.get('[data-test="cn-provider-quota-probe"]').trigger('click')
      await flushPromises()
      expect(queryQuota).toHaveBeenCalledTimes(2)
      wrapper.unmount()
    }
  )

  it('restores a fresh real-zero snapshot while leaving the missing weekly window unknown', async () => {
    const account = codingAccount('https://api.minimax.cn/v1', {
      minimax_5h_used_percent: 0,
      minimax_5h_reset_at: resetShort,
      minimax_weekly_used_percent: null,
      minimax_usage_updated_at: new Date().toISOString()
    })
    const wrapper = mount(CNProviderQuotaCell, { props: { account } })
    await flushPromises()
    expect(queryQuota).not.toHaveBeenCalled()
    const bars = wrapper.findAllComponents(UsageProgressBar)
    expect(bars).toHaveLength(1)
    expect(bars[0].props('utilization')).toBe(0)
    expect(bars[0].props('resetsAt')).toBe(resetShort)
    queryQuota.mockResolvedValue({ success: false, error: 'MiniMax quota is unknown' })
    await wrapper.get('[data-test="cn-provider-quota-probe"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAllComponents(UsageProgressBar)).toHaveLength(1)
    expect(wrapper.text()).toContain('MiniMax quota is unknown')
    wrapper.unmount()
  })

  it('does not expose coding quota for a pay-as-you-go account', async () => {
    const account = codingAccount('https://api.minimax.cn/v1')
    account.credentials.account_mode = 'payg'
    account.credentials.api_key = 'sk-api-fixture'
    const wrapper = mount(CNProviderQuotaCell, { props: { account } })
    await flushPromises()
    expect(queryQuota).not.toHaveBeenCalled()
    expect(wrapper.find('[data-test="cn-provider-quota-probe"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
