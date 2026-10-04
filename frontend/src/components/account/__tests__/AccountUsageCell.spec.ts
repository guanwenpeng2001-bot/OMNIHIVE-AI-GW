import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AccountUsageCell from '../AccountUsageCell.vue'
import type { Account } from '@/types'

const { getUsage, queryQuota } = vi.hoisted(() => ({
  queryQuota: vi.fn(),
  getUsage: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    cnProviders: { queryQuota },
    accounts: {
      getUsage
    }
  }
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
	  locale: { value: 'en' },
      t: (key: string) => key
    })
  }
})

function makeAccount(overrides: Partial<Account>): Account {
  return {
    id: 1,
    name: 'account',
    platform: 'antigravity',
    type: 'oauth',
    proxy_id: null,
    concurrency: 1,
    priority: 1,
    status: 'active',
    error_message: null,
    last_used_at: null,
    expires_at: null,
    auto_pause_on_expired: true,
    created_at: '2026-03-15T00:00:00Z',
    updated_at: '2026-03-15T00:00:00Z',
    schedulable: true,
    rate_limited_at: null,
    rate_limit_reset_at: null,
    overload_until: null,
    temp_unschedulable_until: null,
    temp_unschedulable_reason: null,
    session_window_start: null,
    session_window_end: null,
    session_window_status: null,
    ...overrides,
  }
}

function makeOllamaUsage(accountId: number, overrides: Partial<NonNullable<Account['ollama_cloud_usage']>> = {}) {
  return {
    account_id: accountId,
    eligible: true,
    configured: true,
    auto_refresh_enabled: true,
    encryption_key_configured: true,
    snapshot: {
      status: 'ok' as const,
      last_attempt_at: '2026-07-23T00:00:00Z',
      next_refresh_at: '2026-07-23T01:00:00Z',
      data: {
        five_hour: { used_percent: 12 },
        seven_day: { used_percent: 34 }
      }
    },
    ...overrides,
  }
}

// CN 平台 Ollama Cloud 用例共用的子组件 stub：按 data-test 断言渲染与否
const cnUsageCellStubs = {
  OllamaCloudUsageCell: {
    props: ['account'],
    template: '<div data-test="embedded-ollama">ollama</div>'
  },
  CNProviderQuotaCell: {
    template: '<div data-test="cn-quota-cell" />'
  },
  CNProviderBalanceCell: {
    template: '<div data-test="cn-balance-cell" />'
  }
}

const localOnlyAccounts = [
  ['qianwen', undefined], ['qianfan', undefined], ['hunyuan', undefined],
  ['mistral', undefined], ['volcengine', undefined],
  ['zhipu', 'payg'], ['zhipu', ''], ['zhipu', undefined],
  ['minimax', 'payg'], ['minimax', ''], ['minimax', undefined], ['opencode_go', 'zen'],
] as const

describe('AccountUsageCell', () => {
  it.each(['novita', 'ppio', 'siliconflow_cn', 'siliconflow_global', 'shengsuanyun', 'soleapi'])('shows supported %s balance without automatic upstream requests', async platform => {
    const account = makeAccount({ platform, type: 'apikey', credentials: { account_mode: 'payg' } })
    const wrapper = mount(AccountUsageCell, { props: { account }, global: { stubs: cnUsageCellStubs } })
    await flushPromises()
    expect(wrapper.find('[data-test="cn-balance-cell"]').exists()).toBe(true)
    expect(getUsage).not.toHaveBeenCalled()
    await wrapper.setProps({ account: { ...account, credentials: { account_mode: 'coding' } } })
    expect(wrapper.find('[data-test="cn-balance-cell"]').exists()).toBe(false)
    await wrapper.setProps({ account: { ...account, credentials: { base_url: 'https://relay.example/v1' } } })
    expect(wrapper.find('[data-test="cn-balance-cell"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it.each(localOnlyAccounts)('renders missing/loading/zero/nonzero local statistics for %s %s without probing upstream', async (platform, accountMode) => {
    const wrapper = mount(AccountUsageCell, {
      props: { account: makeAccount({ platform, type: 'apikey', credentials: { account_mode: accountMode } }) }
    })
    const explanation = () => wrapper.find('[data-testid="local-usage-explanation"]')
    const placeholder = () => wrapper.findAll('div').some(element => element.text() === '-')
    await flushPromises()
    expect(explanation().text()).toBe('admin.accounts.usageWindow.noUpstreamUsageApi')
    expect(placeholder()).toBe(true)
    expect(wrapper.text()).not.toContain('A $0.00')
    await wrapper.setProps({ todayStatsLoading: true })
    expect(explanation().exists()).toBe(true)
    expect(wrapper.find('.animate-pulse').exists()).toBe(true)
    expect(placeholder()).toBe(false)
    await wrapper.setProps({ todayStatsLoading: false, todayStats: { requests: 12, tokens: 1200, cost: 2.5, user_cost: 3 } })
    expect(explanation().exists()).toBe(true)
    expect(wrapper.find('.animate-pulse').exists()).toBe(false)
    expect(placeholder()).toBe(false)
    for (const value of ['12 req', '1.2K tokens', 'A $2.50', 'U $3.00']) expect(wrapper.text()).toContain(value)
    await wrapper.setProps({ todayStats: { requests: 0, tokens: 0, cost: 0 } })
    for (const value of ['0 req', '0 tokens', 'A $0.00']) expect(wrapper.text()).toContain(value)
    expect(wrapper.text()).not.toContain('U $')
    await wrapper.setProps({ todayStats: { requests: 0, tokens: 0, cost: 0, user_cost: 0 } })
    expect(wrapper.text()).toContain('U $0.00')
    await wrapper.setProps({ todayStats: null })
    expect(explanation().exists()).toBe(true)
    expect(placeholder()).toBe(true)
    expect(wrapper.findComponent({ name: 'UsageProgressBar' }).exists()).toBe(false)
    expect(wrapper.find('[data-test="cn-provider-quota"]').exists()).toBe(false)
    expect(wrapper.findComponent({ name: 'CNProviderBalanceCell' }).exists()).toBe(false)
    expect(getUsage).not.toHaveBeenCalled()
    expect(queryQuota).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it.each([
    ['zhipu', 'payg', 'coding'], ['minimax', 'payg', 'coding'], ['opencode_go', 'zen', 'go'],
  ] as const)('preserves local-only fallback across %s mode and account switches', async (platform, localMode, quotaMode) => {
    const localAccount = makeAccount({ platform, type: 'apikey', credentials: { account_mode: localMode } })
    const wrapper = mount(AccountUsageCell, {
      props: { account: localAccount, todayStats: { requests: 12, tokens: 1200, cost: 2.5 } },
      global: { stubs: cnUsageCellStubs }
    })
    await wrapper.setProps({ account: { ...localAccount, credentials: { account_mode: quotaMode } } })
    expect(wrapper.find('[data-testid="local-usage-explanation"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="cn-quota-cell"]').exists()).toBe(true)
    await wrapper.setProps({ account: localAccount })
    expect(wrapper.find('[data-testid="local-usage-explanation"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('12 req')
    await wrapper.setProps({ account: makeAccount({ id: 9008, platform: 'qianwen', type: 'apikey' }), todayStats: null, todayStatsLoading: true })
    expect(wrapper.text()).not.toContain('12 req')
    expect(wrapper.find('.animate-pulse').exists()).toBe(true)
    await wrapper.setProps({ todayStatsLoading: false, todayStats: { requests: 7, tokens: 70, cost: 1 } })
    expect(wrapper.text()).toContain('7 req')
    expect(wrapper.find('[data-test="cn-quota-cell"]').exists()).toBe(false)
    await wrapper.setProps({ account: makeAccount({ id: 9009, platform: 'openai', type: 'apikey' }), todayStats: null })
    expect(wrapper.text()).toBe('-')
    expect(getUsage).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it.each([...localOnlyAccounts, ['commandcode', undefined]] as const)('keeps Ollama and configured local quota separate for %s %s', async (platform, accountMode) => {
    const localAccount = makeAccount({ platform, type: 'apikey', credentials: { account_mode: accountMode }, quota_daily_limit: 10, quota_daily_used: 2 })
    const wrapper = mount(AccountUsageCell, {
      props: { account: localAccount, todayStats: { requests: 3, tokens: 30, cost: 0.5 } },
      global: { stubs: { ...cnUsageCellStubs, UsageProgressBar: { props: ['label', 'utilization'], template: '<div data-test="quota-bar">{{ label }}|{{ utilization }}</div>' } } }
    })
    expect(wrapper.get('[data-test="quota-bar"]').text()).toBe('1d|20')
    expect(wrapper.find('[data-testid="local-usage-explanation"]').exists()).toBe(platform !== 'commandcode')
    await wrapper.setProps({ account: { ...localAccount, ollama_cloud_usage: makeOllamaUsage(localAccount.id) } })
    expect(wrapper.find('[data-test="embedded-ollama"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="local-usage-explanation"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="cn-quota-cell"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="cn-balance-cell"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('3 req')
    expect(wrapper.get('[data-test="quota-bar"]').text()).toBe('1d|20')
    expect(getUsage).not.toHaveBeenCalled()
    expect(queryQuota).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it.each([
    ['kimi', 'coding', 'cn-quota-cell'], ['kimi', 'payg', 'cn-balance-cell'],
    ['deepseek', 'payg', 'cn-balance-cell'], ['opencode_go', '', 'cn-quota-cell'],
  ] as const)('preserves supported %s %s quota or balance display', async (platform, accountMode, cell) => {
    const wrapper = mount(AccountUsageCell, {
      props: { account: makeAccount({ platform, type: 'apikey', credentials: { account_mode: accountMode } }) },
      global: { stubs: cnUsageCellStubs }
    })
    await flushPromises()
    expect(wrapper.find(`[data-test="${cell}"]`).exists()).toBe(true)
    expect(wrapper.find('[data-testid="local-usage-explanation"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('keeps local-only content across viewport changes and late CommandCode probe results', async () => {
    let finishProbe!: (result: { success: boolean; tiers: { window: string; used_percent: number }[] }) => void
    queryQuota.mockImplementationOnce(() => new Promise(resolve => { finishProbe = resolve }))
    let changeViewport!: (event: { matches: boolean }) => void
    vi.mocked(window.matchMedia).mockReturnValue({
      matches: true,
      addEventListener: (_event: string, listener: typeof changeViewport) => { changeViewport = listener },
      removeEventListener: vi.fn(),
    } as unknown as MediaQueryList)
    const wrapper = mount(AccountUsageCell, {
      props: { account: makeAccount({ id: 98001, platform: 'commandcode', type: 'apikey' }) }
    })
    await flushPromises()
    expect(queryQuota).toHaveBeenCalledWith(98001)
    await wrapper.setProps({ account: makeAccount({ id: 98002, platform: 'qianwen', type: 'apikey' }), todayStats: { requests: 7, tokens: 70, cost: 1 } })
    changeViewport({ matches: false })
    finishProbe({ success: true, tiers: [{ window: '5h', used_percent: 42 }] })
    await flushPromises()
    changeViewport({ matches: true })
    await flushPromises()
    expect(wrapper.find('[data-testid="local-usage-explanation"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="commandcode-local-fallback"]').exists()).toBe(false)
    expect(wrapper.findComponent({ name: 'UsageProgressBar' }).exists()).toBe(false)
    expect(wrapper.text()).toContain('7 req')
    expect(wrapper.text()).not.toContain('42%')
    expect(queryQuota).toHaveBeenCalledTimes(1)
    expect(getUsage).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it.each(['commandcode', 'qianwen'] as const)('explains local-only usage for %s with and without traffic', async (platform) => {
    const wrapper = mount(AccountUsageCell, {
      props: { account: makeAccount({ platform, type: 'apikey' }) }
    })
    await flushPromises()
    const explanation = platform === 'commandcode' ? '[data-test="commandcode-local-fallback"]' : '[data-testid="local-usage-explanation"]'
    expect(wrapper.get(explanation).text()).toBe(platform === 'commandcode' ? 'admin.accounts.cnProviders.commandCodeLocal' : 'admin.accounts.usageWindow.noUpstreamUsageApi')
    expect(wrapper.text()).not.toBe('-')
    expect(wrapper.findComponent({ name: 'UsageProgressBar' }).exists()).toBe(false)
    expect(getUsage).not.toHaveBeenCalled()
    await wrapper.setProps({ todayStats: { requests: 12, tokens: 1200, cost: 2.5, user_cost: 3 } })
    expect(wrapper.text()).toContain('12 req')
    expect(wrapper.text()).toContain('A $2.50')
    expect(wrapper.text()).toContain('U $3.00')
    expect(wrapper.find(explanation).exists()).toBe(true)
    await wrapper.setProps({ todayStats: null, todayStatsLoading: true })
    expect(wrapper.find(explanation).exists()).toBe(true)
    await wrapper.setProps({ account: makeAccount({ platform: 'openai', type: 'apikey' }), todayStatsLoading: false })
    expect(wrapper.find('[data-testid="local-usage-explanation"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it.each(['oauth', 'apikey'] as const)('queries Cursor monthly usage for %s accounts', async (type) => {
    getUsage.mockResolvedValue({ cursor_monthly: { start_of_month: null, resets_at: null, models: { 'gpt-4': { numRequests: 2, numRequestsTotal: 3, numTokens: 70, maxTokenUsage: null, maxRequestUsage: null } } } })
    const account = makeAccount({ id: type === 'oauth' ? 97001 : 97002, platform: 'cursor', type })
    const wrapper = mount(AccountUsageCell, { props: { account } })
    await flushPromises()
    expect(getUsage).toHaveBeenCalledWith(account.id)
    expect(wrapper.find('[data-test="cursor-monthly"]').text()).toContain('gpt-4')
    expect(wrapper.text()).toContain('admin.accounts.cursor.quotaUnknown')
    expect(wrapper.text()).not.toContain('admin.accounts.cursor.unlimited')
    await wrapper.find('[data-test="cursor-monthly"] button').trigger('click')
    await flushPromises()
    expect(getUsage).toHaveBeenLastCalledWith(account.id, 'active', true)
    wrapper.unmount()
  })

  it('shows Cursor query failure and recovers through refresh', async () => {
    getUsage.mockResolvedValueOnce({ error_code: 'unauthenticated', error: 'HTTP 401' }).mockResolvedValueOnce({ cursor_monthly: { start_of_month: null, resets_at: null, models: {} } })
    const wrapper = mount(AccountUsageCell, { props: { account: makeAccount({ id: 97003, platform: 'cursor', type: 'apikey' }) } })
    await flushPromises()
    expect(wrapper.text()).toContain('admin.accounts.cursor.usageFailed')
    await wrapper.find('[data-test="cursor-monthly"] button').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('admin.accounts.cursor.noUsage')
    wrapper.unmount()
  })
  beforeEach(() => {
    getUsage.mockReset()
    queryQuota.mockReset().mockResolvedValue({ success: false, unsupported: true })
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: vi.fn().mockImplementation(() => ({
        matches: true,
        media: '(min-width: 768px)',
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      }))
    })
  })

  it('renders eligible Ollama Cloud state and forwards query updates', async () => {
    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({
          id: 9001,
          platform: 'openai',
          type: 'apikey',
          ollama_cloud_usage: {
            account_id: 9001,
            eligible: true,
            configured: true,
            auto_refresh_enabled: true,
            encryption_key_configured: true,
            snapshot: {
              status: 'ok',
              last_attempt_at: '2026-07-23T00:00:00Z',
              next_refresh_at: '2026-07-23T01:00:00Z',
              data: {
                five_hour: { used_percent: 12 },
                seven_day: { used_percent: 34 }
              }
            }
          }
        })
      },
      global: {
        stubs: {
          OllamaCloudUsageCell: {
            props: ['account'],
            emits: ['updated'],
            template: '<button data-test="embedded-ollama" @click="$emit(\'updated\', { ...account.ollama_cloud_usage, auto_refresh_enabled: false })">{{ account.ollama_cloud_usage.snapshot.data.five_hour.used_percent }}</button>'
          },
          UsageProgressBar: true,
          AccountQuotaInfo: true
        }
      }
    })

    expect(wrapper.get('[data-test="embedded-ollama"]').text()).toBe('12')
    expect(getUsage).not.toHaveBeenCalled()

    await wrapper.get('[data-test="embedded-ollama"]').trigger('click')

    const updatedAccount = wrapper.emitted<Account[]>('account-updated')?.[0]?.[0]
    expect(updatedAccount?.id).toBe(9001)
    expect(updatedAccount?.ollama_cloud_usage?.auto_refresh_enabled).toBe(false)
  })

  it.each(['kimi', 'zhipu', 'deepseek', 'minimax'] as const)(
    '%s apikey 账号 Ollama Cloud eligible 时渲染 Ollama 用量单元格并跳过 CN 子单元格',
    async (platform) => {
      const wrapper = mount(AccountUsageCell, {
        props: {
          account: makeAccount({
            id: 9002,
            platform,
            type: 'apikey',
            credentials: { account_mode: 'coding' },
            quota_daily_limit: 10,
            quota_daily_used: 2,
            ollama_cloud_usage: makeOllamaUsage(9002)
          }),
          todayStats: { requests: 3, tokens: 30, cost: 0.5 }
        },
        global: {
          stubs: { ...cnUsageCellStubs, UsageProgressBar: true, AccountQuotaInfo: true }
        }
      })

      await flushPromises()

      expect(wrapper.find('[data-test="embedded-ollama"]').exists()).toBe(true)
      expect(wrapper.text()).toContain('3 req')
      expect(wrapper.findComponent({ name: 'UsageProgressBar' }).props('utilization')).toBe(20)
      expect(wrapper.find('[data-test="cn-quota-cell"]').exists()).toBe(false)
      expect(wrapper.find('[data-test="cn-balance-cell"]').exists()).toBe(false)
      expect(wrapper.find('div[title="admin.accounts.cnProviders.noBalanceEndpoint"]').exists()).toBe(false)
      expect(getUsage).not.toHaveBeenCalled()
    }
  )

  it('CN 平台 Ollama Cloud eligible 账号的用量更新经 account-updated 透传', async () => {
    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({
          id: 9003,
          platform: 'kimi',
          type: 'apikey',
          credentials: { account_mode: 'coding' },
          ollama_cloud_usage: makeOllamaUsage(9003)
        })
      },
      global: {
        stubs: {
          ...cnUsageCellStubs,
          OllamaCloudUsageCell: {
            props: ['account'],
            emits: ['updated'],
            template: '<button data-test="embedded-ollama" @click="$emit(\'updated\', { ...account.ollama_cloud_usage, auto_refresh_enabled: false })" />'
          },
          UsageProgressBar: true,
          AccountQuotaInfo: true
        }
      }
    })

    await wrapper.get('[data-test="embedded-ollama"]').trigger('click')

    const updatedAccount = wrapper.emitted<Account[]>('account-updated')?.[0]?.[0]
    expect(updatedAccount?.id).toBe(9003)
    expect(updatedAccount?.ollama_cloud_usage?.auto_refresh_enabled).toBe(false)
  })

  it.each([
    { name: '无 ollama_cloud_usage', usage: undefined },
    { name: 'eligible=false', usage: makeOllamaUsage(9004, { eligible: false }) }
  ])('普通 kimi apikey 账号（$name）仍渲染 CN 子单元格', async ({ usage }) => {
    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({
          id: 9004,
          platform: 'kimi',
          type: 'apikey',
          credentials: { account_mode: 'coding' },
          ollama_cloud_usage: usage
        })
      },
      global: {
        stubs: { ...cnUsageCellStubs, UsageProgressBar: true, AccountQuotaInfo: true }
      }
    })

    await flushPromises()

    expect(wrapper.find('[data-test="cn-quota-cell"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="cn-balance-cell"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="embedded-ollama"]').exists()).toBe(false)
  })

  it('Antigravity 图片用量会聚合新旧 image 模型', async () => {
    getUsage.mockResolvedValue({
      antigravity_quota: {
        'gemini-2.5-flash-image': {
          utilization: 45,
          reset_time: '2026-03-01T11:00:00Z'
        },
        'gemini-3.1-flash-image': {
          utilization: 20,
          reset_time: '2026-03-01T10:00:00Z'
        },
        'gemini-3-pro-image': {
          utilization: 70,
          reset_time: '2026-03-01T09:00:00Z'
        }
      }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({
          id: 1001,
          platform: 'antigravity',
          type: 'oauth',
          extra: {}
        })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization', 'resetsAt', 'color'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}|{{ resetsAt }}</div>'
          },
          AccountQuotaInfo: true
        }
      }
    })

    await flushPromises()

    expect(wrapper.text()).toContain('admin.accounts.usageWindow.gemini3Image|70|2026-03-01T09:00:00Z')
  })

  it('Antigravity 会显示 AI Credits 余额信息', async () => {
    getUsage.mockResolvedValue({
      ai_credits: [
        {
          credit_type: 'GOOGLE_ONE_AI',
          amount: 25,
          minimum_balance: 5
        }
      ]
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({
          id: 1002,
          platform: 'antigravity',
          type: 'oauth',
          extra: {}
        })
      },
      global: {
        stubs: {
          UsageProgressBar: true,
          AccountQuotaInfo: true
        }
      }
    })

    await flushPromises()

    expect(wrapper.text()).toContain('admin.accounts.aiCreditsBalance')
    expect(wrapper.text()).toContain('25')
  })


  it('OpenAI OAuth 快照已过期时首屏会重新请求 usage', async () => {
    getUsage.mockResolvedValue({
      five_hour: {
        utilization: 15,
        resets_at: '2026-03-08T12:00:00Z',
        remaining_seconds: 3600,
        window_stats: {
          requests: 3,
          tokens: 300,
          cost: 0.03,
          standard_cost: 0.03,
          user_cost: 0.03
        }
      },
      seven_day: {
        utilization: 77,
        resets_at: '2026-03-13T12:00:00Z',
        remaining_seconds: 3600,
        window_stats: {
          requests: 3,
          tokens: 300,
          cost: 0.03,
          standard_cost: 0.03,
          user_cost: 0.03
        }
      }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({
          id: 2000,
          platform: 'openai',
          type: 'oauth',
          extra: {
            codex_usage_updated_at: '2026-03-07T00:00:00Z',
            codex_5h_used_percent: 12,
            codex_5h_reset_at: '2026-03-08T12:00:00Z',
            codex_7d_used_percent: 34,
            codex_7d_reset_at: '2026-03-13T12:00:00Z'
          }
        })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization', 'resetsAt', 'windowStats', 'color'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}|{{ windowStats?.tokens }}</div>'
          },
          AccountQuotaInfo: true
        }
      }
    })

    await flushPromises()

    expect(getUsage).toHaveBeenCalledWith(2000)
    expect(wrapper.text()).toContain('5h|15|300')
    expect(wrapper.text()).toContain('7d|77|300')
  })

  it('OpenAI OAuth 有 codex 快照时仍然使用 /usage API 数据渲染', async () => {
    getUsage.mockResolvedValue({
      five_hour: {
        utilization: 18,
        resets_at: '2099-03-07T12:00:00Z',
        remaining_seconds: 3600,
        window_stats: {
          requests: 9,
          tokens: 900,
          cost: 0.09,
          standard_cost: 0.09,
          user_cost: 0.09
        }
      },
      seven_day: {
        utilization: 36,
        resets_at: '2099-03-13T12:00:00Z',
        remaining_seconds: 3600,
        window_stats: {
          requests: 9,
          tokens: 900,
          cost: 0.09,
          standard_cost: 0.09,
          user_cost: 0.09
        }
      }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({
          id: 2001,
          platform: 'openai',
          type: 'oauth',
          extra: {
            codex_usage_updated_at: '2099-03-07T10:00:00Z',
            codex_5h_used_percent: 12,
            codex_5h_reset_at: '2099-03-07T12:00:00Z',
            codex_7d_used_percent: 34,
            codex_7d_reset_at: '2099-03-13T12:00:00Z'
          }
        })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization', 'resetsAt', 'windowStats', 'color'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}|{{ windowStats?.tokens }}</div>'
          },
          AccountQuotaInfo: true
        }
      }
    })

    await flushPromises()

    expect(getUsage).toHaveBeenCalledWith(2001)
    // 单一数据源：始终使用 /usage API 返回值，忽略 codex 快照
    expect(wrapper.text()).toContain('5h|18|900')
    expect(wrapper.text()).toContain('7d|36|900')
  })

  it('仅为 OpenAI OAuth 7d 窗口计算预计总费用', async () => {
    getUsage.mockResolvedValue({
      five_hour: {
        utilization: 25,
        resets_at: null,
        remaining_seconds: 0,
        window_stats: { requests: 1, tokens: 100, cost: 2 }
      },
      seven_day: {
        utilization: 40,
        resets_at: null,
        remaining_seconds: 0,
        window_stats: { requests: 2, tokens: 200, cost: 12 }
      }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({ id: 6752, platform: 'openai', type: 'oauth' })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'estimatedTotalCost'],
            template: '<div class="usage-bar">{{ label }}|{{ estimatedTotalCost ?? "none" }}</div>'
          },
          AccountQuotaInfo: true
        }
      }
    })

    await flushPromises()

    expect(wrapper.text()).toContain('5h|none')
    expect(wrapper.text()).toContain('7d|30')
  })

  it.each([
    { id: 6801, utilization: 0, cost: 12 },
    { id: 6802, utilization: -1, cost: 12 },
    { id: 6803, utilization: Number.NaN, cost: 12 },
    { id: 6804, utilization: 40, cost: 0 },
    { id: 6805, utilization: 40, cost: Number.POSITIVE_INFINITY }
  ])('OpenAI OAuth 7d 输入无效时不显示预计总费用 (%o)', async ({ id, utilization, cost }) => {
    getUsage.mockResolvedValue({
      seven_day: {
        utilization,
        resets_at: null,
        remaining_seconds: 0,
        window_stats: { requests: 1, tokens: 100, cost }
      }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({
          id,
          platform: 'openai',
          type: 'oauth'
        })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'estimatedTotalCost'],
            template: '<div class="usage-bar">{{ label }}|{{ estimatedTotalCost ?? "none" }}</div>'
          },
          AccountQuotaInfo: true
        }
      }
    })

    await flushPromises()

    expect(wrapper.text()).toContain('7d|none')
    expect(wrapper.text()).not.toMatch(/Infinity|NaN/)
  })

  it('OpenAI OAuth 有现成快照时，手动刷新信号会触发 usage 重拉', async () => {
    getUsage.mockResolvedValue({
      five_hour: {
        utilization: 18,
        resets_at: '2099-03-07T12:00:00Z',
        remaining_seconds: 3600,
        window_stats: {
          requests: 9,
          tokens: 900,
          cost: 0.09,
          standard_cost: 0.09,
          user_cost: 0.09
        }
      },
      seven_day: {
        utilization: 36,
        resets_at: '2099-03-13T12:00:00Z',
        remaining_seconds: 3600,
        window_stats: {
          requests: 9,
          tokens: 900,
          cost: 0.09,
          standard_cost: 0.09,
          user_cost: 0.09
        }
      }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({
          id: 2010,
          platform: 'openai',
          type: 'oauth',
          extra: {
            codex_usage_updated_at: '2099-03-07T10:00:00Z',
            codex_5h_used_percent: 12,
            codex_5h_reset_at: '2099-03-07T12:00:00Z',
            codex_7d_used_percent: 34,
            codex_7d_reset_at: '2099-03-13T12:00:00Z'
          },
          rate_limit_reset_at: null
        }),
        manualRefreshToken: 0
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization', 'resetsAt', 'windowStats', 'color'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}|{{ windowStats?.tokens }}</div>'
          },
          AccountQuotaInfo: true
        }
      }
    })

    await flushPromises()
    // mount 时已经拉取一次
    expect(getUsage).toHaveBeenCalledTimes(1)

    await wrapper.setProps({ manualRefreshToken: 1 })
    await flushPromises()

    // 手动刷新再拉一次
    expect(getUsage).toHaveBeenCalledTimes(2)
    expect(getUsage).toHaveBeenCalledWith(2010)
    // 单一数据源：始终使用 /usage API 值
    expect(wrapper.text()).toContain('5h|18|900')
  })

  it('OpenAI OAuth 在无 codex 快照时会回退显示 usage 接口窗口', async () => {
	getUsage.mockResolvedValue({
	  five_hour: {
	    utilization: 0,
	    resets_at: null,
	    remaining_seconds: 0,
	    window_stats: {
	      requests: 2,
	      tokens: 27700,
	      cost: 0.06,
	      standard_cost: 0.06,
	      user_cost: 0.06
	    }
	  },
	  seven_day: {
	    utilization: 0,
	    resets_at: null,
	    remaining_seconds: 0,
	    window_stats: {
	      requests: 2,
	      tokens: 27700,
	      cost: 0.06,
	      standard_cost: 0.06,
	      user_cost: 0.06
	    }
	  }
	})

		const wrapper = mount(AccountUsageCell, {
		  props: {
		    account: makeAccount({
		      id: 2002,
		      platform: 'openai',
		      type: 'oauth',
		      extra: {}
		    })
		  },
	  global: {
	    stubs: {
	      UsageProgressBar: {
	        props: ['label', 'utilization', 'resetsAt', 'windowStats', 'color'],
	        template: '<div class="usage-bar">{{ label }}|{{ utilization }}|{{ windowStats?.tokens }}</div>'
	      },
	      AccountQuotaInfo: true
	    }
	  }
	})

	await flushPromises()

	expect(getUsage).toHaveBeenCalledWith(2002)
	expect(wrapper.text()).toContain('5h|0|27700')
	expect(wrapper.text()).toContain('7d|0|27700')
  })

  it('OpenAI OAuth 在行数据刷新但仍无 codex 快照时会重新拉取 usage', async () => {
	getUsage
	  .mockResolvedValueOnce({
	    five_hour: {
	      utilization: 0,
	      resets_at: null,
	      remaining_seconds: 0,
	      window_stats: {
	        requests: 1,
	        tokens: 100,
	        cost: 0.01,
	        standard_cost: 0.01,
	        user_cost: 0.01
	      }
	    },
	    seven_day: null
	  })
	  .mockResolvedValueOnce({
	    five_hour: {
	      utilization: 0,
	      resets_at: null,
	      remaining_seconds: 0,
	      window_stats: {
	        requests: 2,
	        tokens: 200,
	        cost: 0.02,
	        standard_cost: 0.02,
	        user_cost: 0.02
	      }
	    },
	    seven_day: null
	  })

		const wrapper = mount(AccountUsageCell, {
		  props: {
		    account: makeAccount({
		      id: 2003,
		      platform: 'openai',
		      type: 'oauth',
		      updated_at: '2026-03-07T10:00:00Z',
		      extra: {}
		    })
		  },
	  global: {
	    stubs: {
	      UsageProgressBar: {
	        props: ['label', 'utilization', 'resetsAt', 'windowStats', 'color'],
	        template: '<div class="usage-bar">{{ label }}|{{ utilization }}|{{ windowStats?.tokens }}</div>'
	      },
	      AccountQuotaInfo: true
	    }
	  }
	})

	await flushPromises()
	expect(wrapper.text()).toContain('5h|0|100')
	expect(getUsage).toHaveBeenCalledTimes(1)

	await wrapper.setProps({
	  account: {
	    id: 2003,
	    platform: 'openai',
	    type: 'oauth',
	    updated_at: '2026-03-07T10:01:00Z',
	    extra: {}
	  }
	})

	await flushPromises()
	expect(getUsage).toHaveBeenCalledTimes(2)
	expect(wrapper.text()).toContain('5h|0|200')
  })

  it('OpenAI 重置响应更新账号行后重新拉取 usage', async () => {
    getUsage.mockResolvedValue({
      five_hour: {
        utilization: 0,
        resets_at: null,
        remaining_seconds: 0
      },
      seven_day: null
    })
    const account = makeAccount({
      id: 2004,
      platform: 'openai',
      type: 'oauth',
      updated_at: '2026-03-07T10:00:00Z',
      extra: {}
    })
    const wrapper = mount(AccountUsageCell, {
      props: { account },
      global: {
        stubs: {
          UsageProgressBar: true,
          AccountQuotaInfo: true,
          OpenAIQuotaResetCell: {
            props: ['account'],
            emits: ['account-updated'],
            template: '<button data-test="quota-reset-result" @click="$emit(\'account-updated\', { ...account, updated_at: \'2026-03-07T10:01:00Z\' })" />'
          }
        }
      }
    })

    await flushPromises()
    expect(getUsage).toHaveBeenCalledTimes(1)

    await wrapper.get('[data-test="quota-reset-result"]').trigger('click')
    const updatedAccount = wrapper.emitted<Account[]>('account-updated')?.[0]?.[0]
    expect(updatedAccount?.updated_at).toBe('2026-03-07T10:01:00Z')

    await wrapper.setProps({ account: updatedAccount as Account })
    await flushPromises()

    expect(getUsage).toHaveBeenCalledTimes(2)
  })

  it('OpenAI OAuth 已限额时显示 /usage API 返回的限额数据', async () => {
	getUsage.mockResolvedValue({
	  five_hour: {
	    utilization: 100,
	    resets_at: '2026-03-07T12:00:00Z',
	    remaining_seconds: 3600,
	    window_stats: {
	      requests: 211,
	      tokens: 106540000,
	      cost: 38.13,
	      standard_cost: 38.13,
	      user_cost: 38.13
	    }
	  },
	  seven_day: {
	    utilization: 100,
	    resets_at: '2026-03-13T12:00:00Z',
	    remaining_seconds: 3600,
	    window_stats: {
	      requests: 211,
	      tokens: 106540000,
	      cost: 38.13,
	      standard_cost: 38.13,
	      user_cost: 38.13
	    }
	  }
	})

		const wrapper = mount(AccountUsageCell, {
		  props: {
		    account: makeAccount({
		      id: 2004,
		      platform: 'openai',
		      type: 'oauth',
		      rate_limit_reset_at: '2099-03-07T12:00:00Z',
		      extra: {
		        codex_5h_used_percent: 0,
		        codex_7d_used_percent: 0
		      }
		    })
		  },
	  global: {
	    stubs: {
	      UsageProgressBar: {
	        props: ['label', 'utilization', 'resetsAt', 'windowStats', 'color'],
	        template: '<div class="usage-bar">{{ label }}|{{ utilization }}|{{ windowStats?.tokens }}</div>'
	      },
	      AccountQuotaInfo: true
	    }
	  }
	})

	await flushPromises()

  expect(getUsage).toHaveBeenCalledWith(2004)
  expect(wrapper.text()).toContain('5h|100|106540000')
  expect(wrapper.text()).toContain('7d|100|106540000')
  })

  it('Key 账号会展示 today stats 徽章并带 A/U 提示', async () => {
		const wrapper = mount(AccountUsageCell, {
		  props: {
		    account: makeAccount({
		      id: 3001,
		      platform: 'anthropic',
		      type: 'apikey'
		    }),
		    todayStats: {
		      requests: 1_000_000,
		      tokens: 1_000_000_000,
		      cost: 12.345,
		      standard_cost: 12.345,
		      user_cost: 6.789
		    }
		  },
		  global: {
		    stubs: {
		      UsageProgressBar: true,
		      AccountQuotaInfo: true
		    }
		  }
		})

		await flushPromises()

		expect(wrapper.text()).toContain('1.0M req')
		expect(wrapper.text()).toContain('1.0B')
		expect(wrapper.text()).toContain('A $12.35')
		expect(wrapper.text()).toContain('U $6.79')

		const badges = wrapper.findAll('span[title]')
		expect(badges.some(node => node.attributes('title') === 'usage.accountBilled')).toBe(true)
		expect(badges.some(node => node.attributes('title') === 'usage.userBilled')).toBe(true)
  })

  it('Grok OAuth compact UI drops local chips and header quota bars', async () => {
    getUsage.mockResolvedValue({
      grok_local_usage: {
        requests: 4,
        tokens: 1200,
        cost: 0.12,
        standard_cost: 0.12,
        user_cost: 0.34
      },
      grok_request_quota: {
        limit: 10,
        remaining: -2,
        reset_at: '2026-07-09T16:00:00Z'
      },
      grok_quota_snapshot_state: 'observed'
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({ id: 3861, platform: 'grok', type: 'oauth', extra: {} })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization', 'resetsAt', 'color'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}|{{ resetsAt }}</div>'
          },
          AccountQuotaInfo: true
        }
      }
    })

    await flushPromises()
    expect(getUsage).toHaveBeenCalledWith(3861)
    expect(wrapper.text()).not.toContain('4 req')
    expect(wrapper.text()).not.toContain('admin.accounts.usageWindow.grokRequests|')
  })

  it('Grok paid monthly limits show 30d bar without free 24h', async () => {
    getUsage.mockResolvedValue({
      grok_billing: {
        period_type: 'weekly',
        usage_percent: null,
        used_percent: 12,
        monthly_limit_cents: 25_000,
        used_cents: 3_000,
        plan: ''
      },
      grok_entitlement_status: 'free',
      grok_token_quota: { limit: 1_000, remaining: 250 }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({ id: 4402, platform: 'grok', type: 'oauth', extra: {} })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}</div>'
          },
          AccountQuotaInfo: true
        }
      }
    })

    await flushPromises()
    expect(wrapper.text()).toContain('30d|')
    expect(wrapper.text()).not.toContain('24h|')
  })

  it('Grok OAuth uses the official weekly billing percentage when available', async () => {
    getUsage.mockResolvedValue({
      grok_billing: {
        period_type: 'weekly',
        usage_percent: 37,
        period_end: '2026-07-16T03:25:00Z',
        plan: 'SuperGrok'
      },
      grok_local_usage: {
        requests: 5,
        tokens: 2_200_000,
        cost: 4.42,
        standard_cost: 4.42,
        user_cost: 0.44
      },
      grok_request_quota: { limit: 100, remaining: 100 },
      grok_token_quota: { limit: 2_000_000, remaining: 2_000_000 }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({ id: 4201, platform: 'grok', type: 'oauth', extra: {} })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization', 'resetsAt', 'remainingCapacity'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}|{{ resetsAt }}|{{ remainingCapacity }}</div>'
          },
          AccountQuotaInfo: true,
        }
      }
    })

    await flushPromises()

    expect(wrapper.text()).toContain('7d|37|2026-07-16T03:25:00Z')
    expect(wrapper.text()).not.toContain('admin.accounts.usageWindow.grokRequests|')
    expect(wrapper.text()).not.toContain('admin.accounts.usageWindow.grokTokens|')
    expect(wrapper.text()).not.toContain('2M|')
  })

  it.each([
    { tokens: 0, expected: 0 },
    { tokens: 500_000, expected: 50 },
    { tokens: 1_000_000, expected: 100 },
    { tokens: 1_100_000, expected: 100 }
  ])('Grok Free derives its 1M quota from local tokens: $tokens -> $expected%', async ({ tokens, expected }) => {
    getUsage.mockResolvedValue({
      grok_free_token_limit: 1_000_000,
      grok_billing: {
        period_type: 'weekly',
        usage_percent: null,
        plan: ''
      },
      grok_local_usage_24h: {
        requests: 5,
        tokens,
        cost: 0,
        standard_cost: 0,
        user_cost: 0
      },
      grok_request_quota: { limit: 100, remaining: 100 },
      grok_token_quota: { limit: 1_000_000, remaining: 1_000_000 }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({ id: 4300 + expected, platform: 'grok', type: 'oauth', extra: {} })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}</div>'
          },
          AccountQuotaInfo: true,
        }
      }
    })

    await flushPromises()

    expect(wrapper.text()).toContain(`24h|${expected}`)
    expect(wrapper.findAll('.usage-bar')).toHaveLength(1)
    expect(wrapper.text()).not.toContain('admin.accounts.usageWindow.grokRequests|')
    expect(wrapper.text()).not.toContain('admin.accounts.usageWindow.grokTokens|')
    expect(wrapper.text()).not.toContain('7d|')
  })

  it('Grok Free uses rolling 24h usage instead of today-only usage', async () => {
    getUsage.mockResolvedValue({
      grok_free_token_limit: 1_000_000,
      grok_billing: { period_type: 'weekly', usage_percent: null, plan: '' },
      grok_local_usage: {
        requests: 2,
        tokens: 250_000,
        cost: 0,
        standard_cost: 0
      },
      grok_local_usage_24h: {
        requests: 12,
        tokens: 750_000,
        cost: 0,
        standard_cost: 0
      }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({ id: 4398, platform: 'grok', type: 'oauth', extra: {} }),
        todayStats: {
          requests: 2,
          tokens: 200_000,
          cost: 0,
          standard_cost: 0
        }
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization', 'title'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}|{{ title }}</div>'
          },
          AccountQuotaInfo: true,
        }
      }
    })

    await flushPromises()

    expect(wrapper.text()).toContain('24h|75|admin.accounts.usageWindow.grokFreeQuota24hHint')
    expect(wrapper.text()).not.toContain('7d|')
    expect(wrapper.text()).not.toContain('200.0K')
    expect(wrapper.text()).not.toContain('250.0K')
  })

  it('Grok Free does not substitute today stats when rolling 24h usage is unavailable', async () => {
    getUsage.mockResolvedValue({
      grok_free_token_limit: 1_000_000,
      grok_billing: { period_type: 'weekly', usage_percent: null, plan: '' },
      grok_local_usage: {
        requests: 1,
        tokens: 250_000,
        cost: 0,
        standard_cost: 0,
        user_cost: 0
      }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({ id: 4399, platform: 'grok', type: 'oauth', extra: {} }),
        todayStats: {
          requests: 4,
          tokens: 1_000_000,
          cost: 0,
          standard_cost: 0,
          user_cost: 0
        }
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}</div>'
          },
          AccountQuotaInfo: true,
        }
      }
    })

    await flushPromises()

    expect(wrapper.findAll('.usage-bar')).toHaveLength(0)
    expect(wrapper.text()).not.toContain('24h|')
    expect(wrapper.text()).not.toContain('1.0M')
    expect(wrapper.text()).not.toContain('250.0K')
  })

  it('Grok JWT free tier shows 24h bar even when leftover Heavy billing metrics remain', async () => {
    getUsage.mockResolvedValue({
      grok_free_token_limit: 500_000,
      subscription_tier: 'free',
      grok_billing: {
        plan: 'SuperGrok Heavy',
        monthly_limit_cents: 150_000,
        usage_percent: 10,
        used_percent: 5
      },
      grok_local_usage_24h: {
        requests: 2,
        tokens: 250_000,
        cost: 0,
        standard_cost: 0
      }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({ id: 4404, platform: 'grok', type: 'oauth', extra: {} })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}</div>'
          },
          AccountQuotaInfo: true
        }
      }
    })

    await flushPromises()
    expect(wrapper.text()).toContain('24h|')
    expect(wrapper.text()).not.toContain('7d|')
    expect(wrapper.text()).not.toContain('30d|')
  })

  it('Grok SuperGrok Lite stays on paid 7d bar, not free 24h', async () => {
    getUsage.mockResolvedValue({
      subscription_tier: 'supergrok_lite',
      grok_billing: {
        period_type: 'weekly',
        plan: 'SuperGrok',
        usage_percent: 20
      },
      grok_local_usage_24h: {
        requests: 1,
        tokens: 100,
        cost: 0,
        standard_cost: 0
      }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({ id: 4405, platform: 'grok', type: 'oauth', extra: {} })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}</div>'
          },
          AccountQuotaInfo: true
        }
      }
    })

    await flushPromises()
    expect(wrapper.text()).toContain('7d|')
    expect(wrapper.text()).not.toContain('24h|')
  })

  it('Grok credential Free tier keeps the 1M fallback when billing is unavailable', async () => {
    getUsage.mockResolvedValue({
      grok_free_token_limit: 1_000_000,
      subscription_tier: 'FREE',
      grok_local_usage_24h: {
        requests: 3,
        tokens: 1_000_000,
        cost: 0,
        standard_cost: 0
      }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({ id: 4403, platform: 'grok', type: 'oauth', extra: {} })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}</div>'
          },
          AccountQuotaInfo: true,
        }
      }
    })

    await flushPromises()

    expect(wrapper.text()).toContain('24h|100')
  })

  it('Grok Free 24h bar shows rolling local usage chips', async () => {
    getUsage.mockResolvedValue({
      grok_free_token_limit: 1_000_000,
      grok_billing: { period_type: 'weekly', usage_percent: null, plan: '' },
      grok_local_usage: {
        requests: 2,
        tokens: 250_000,
        cost: 0,
        standard_cost: 0
      },
      grok_local_usage_24h: {
        requests: 12,
        tokens: 750_000,
        cost: 0.12,
        standard_cost: 0.12,
        user_cost: 0.04
      }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({ id: 4410, platform: 'grok', type: 'oauth', extra: {} })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization', 'windowStats'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}|{{ windowStats?.tokens }}</div>'
          },
          AccountQuotaInfo: true
        }
      }
    })

    await flushPromises()
    expect(wrapper.text()).toContain('24h|75|750000')
    expect(wrapper.text()).not.toContain('|250000')
    expect(wrapper.text()).not.toContain('7d|')
  })

  it('Grok SuperGrok and Heavy bars show period-aligned local 7d and 30d usage', async () => {
    getUsage.mockResolvedValue({
      subscription_tier: 'SuperGrok Heavy',
      grok_billing: {
        period_type: 'weekly',
        usage_percent: 37,
        used_percent: 12,
        monthly_limit_cents: 150_000,
        period_end: '2026-07-16T03:25:00Z',
        billing_period_end: '2026-08-01T00:00:00Z',
        plan: 'SuperGrok Heavy'
      },
      grok_local_usage: {
        requests: 1,
        tokens: 99,
        cost: 0,
        standard_cost: 0
      },
      grok_local_usage_24h: {
        requests: 2,
        tokens: 100,
        cost: 0,
        standard_cost: 0
      },
      grok_local_usage_7d: {
        requests: 8,
        tokens: 2_200_000,
        cost: 4.42,
        standard_cost: 4.42
      },
      grok_local_usage_monthly: {
        requests: 20,
        tokens: 8_000_000,
        cost: 18.5,
        standard_cost: 18.5
      }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({ id: 4411, platform: 'grok', type: 'oauth', extra: {} })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization', 'windowStats'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}|{{ windowStats?.tokens }}</div>'
          },
          AccountQuotaInfo: true
        }
      }
    })

    await flushPromises()
    expect(wrapper.text()).toContain('7d|37|2200000')
    expect(wrapper.text()).toContain('30d|12|8000000')
    expect(wrapper.text()).not.toContain('|99')
    expect(wrapper.text()).not.toContain('|100')
    expect(wrapper.text()).not.toContain('24h|')
  })

  it('Grok paid bars fall back to official seven_day and thirty_day window_stats', async () => {
    getUsage.mockResolvedValue({
      subscription_tier: 'SuperGrok',
      grok_billing: {
        period_type: 'weekly',
        usage_percent: 20,
        used_percent: 8,
        monthly_limit_cents: 25_000,
        plan: 'SuperGrok'
      },
      seven_day: {
        utilization: 20,
        window_stats: {
          requests: 6,
          tokens: 1_500_000,
          cost: 3.1,
          standard_cost: 3.1
        }
      },
      thirty_day: {
        utilization: 8,
        window_stats: {
          requests: 14,
          tokens: 4_400_000,
          cost: 9.2,
          standard_cost: 9.2
        }
      }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({ id: 4412, platform: 'grok', type: 'oauth', extra: {} })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization', 'windowStats'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}|{{ windowStats?.tokens }}</div>'
          },
          AccountQuotaInfo: true
        }
      }
    })

    await flushPromises()
    expect(wrapper.text()).toContain('7d|20|1500000')
    expect(wrapper.text()).toContain('30d|8|4400000')
  })

  it('Grok paid hides zero prepaid and hides used/limit when monthly limit is 0', async () => {
    getUsage.mockResolvedValue({
      subscription_tier: 'SuperGrok',
      grok_billing: {
        period_type: 'weekly',
        usage_percent: 20,
        prepaid_balance: 0,
        monthly_limit: 0,
        monthly_used: 3.5,
        plan: 'SuperGrok'
      }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({ id: 4413, platform: 'grok', type: 'oauth', extra: {} })
      },
      global: {
        stubs: {
          UsageProgressBar: true,
          AccountQuotaInfo: true
        }
      }
    })

    await flushPromises()
    expect(wrapper.text()).not.toContain('admin.accounts.usageWindow.grokPrepaid')
    expect(wrapper.text()).not.toContain('admin.accounts.usageWindow.grokUsed')
    expect(wrapper.text()).not.toContain('3.5/0')
  })

  it('Grok paid shows used/limit without prepaid, and prepaid without a zero monthly limit', async () => {
    getUsage.mockResolvedValueOnce({
      subscription_tier: 'SuperGrok',
      grok_billing: {
        period_type: 'weekly',
        usage_percent: 20,
        monthly_limit: 25,
        monthly_used: 3.5,
        plan: 'SuperGrok'
      }
    })

    const usedOnly = mount(AccountUsageCell, {
      props: {
        account: makeAccount({ id: 4414, platform: 'grok', type: 'oauth', extra: {} })
      },
      global: {
        stubs: {
          UsageProgressBar: true,
          AccountQuotaInfo: true
        }
      }
    })
    await flushPromises()
    expect(usedOnly.text()).not.toContain('admin.accounts.usageWindow.grokPrepaid')
    expect(usedOnly.text()).toContain('admin.accounts.usageWindow.grokUsed')
    expect(usedOnly.text()).toContain('3.50/25.0')

    getUsage.mockResolvedValueOnce({
      subscription_tier: 'SuperGrok Heavy',
      grok_billing: {
        period_type: 'weekly',
        usage_percent: 20,
        prepaid_balance: 12.5,
        monthly_limit: 0,
        monthly_used: 8,
        plan: 'SuperGrok Heavy'
      }
    })
    const prepaidOnly = mount(AccountUsageCell, {
      props: {
        account: makeAccount({ id: 4415, platform: 'grok', type: 'oauth', extra: {} })
      },
      global: {
        stubs: {
          UsageProgressBar: true,
          AccountQuotaInfo: true
        }
      }
    })
    await flushPromises()
    expect(prepaidOnly.text()).toContain('admin.accounts.usageWindow.grokPrepaid')
    expect(prepaidOnly.text()).toContain('$12.5')
    expect(prepaidOnly.text()).not.toContain('admin.accounts.usageWindow.grokUsed')
    expect(prepaidOnly.text()).not.toContain('8.00/0')
  })

  it('Key 账号在 today stats loading 时显示骨架屏', async () => {
		const wrapper = mount(AccountUsageCell, {
		  props: {
		    account: makeAccount({
		      id: 3002,
		      platform: 'anthropic',
		      type: 'apikey'
		    }),
		    todayStats: null,
		    todayStatsLoading: true
		  },
		  global: {
		    stubs: {
		      UsageProgressBar: true,
		      AccountQuotaInfo: true
		    }
		  }
		})

		await flushPromises()

		expect(wrapper.findAll('.animate-pulse').length).toBeGreaterThan(0)
  })

  it('Key 账号在无 today stats 且无配额时显示兜底短横线', async () => {
		const wrapper = mount(AccountUsageCell, {
		  props: {
		    account: makeAccount({
		      id: 3003,
		      platform: 'anthropic',
		      type: 'apikey',
		      quota_limit: 0,
		      quota_daily_limit: 0,
		      quota_weekly_limit: 0
		    }),
		    todayStats: null,
		    todayStatsLoading: false
		  },
		  global: {
		    stubs: {
		      UsageProgressBar: true,
		      AccountQuotaInfo: true
		    }
		  }
		})

		await flushPromises()

		expect(wrapper.text().trim()).toBe('-')
  })

  it('Vertex 账号会在 Gemini 用量窗口里展示 today stats 徽章', async () => {
		const wrapper = mount(AccountUsageCell, {
		  props: {
		    account: makeAccount({
		      id: 4001,
		      platform: 'gemini',
		      type: 'service_account',
          credentials: {
            tier_id: 'vertex',
            project_id: 'vertex-proj',
            client_email: 'svc@vertex-proj.iam.gserviceaccount.com',
            location: 'global'
          },
		      extra: {}
		    }),
		    todayStats: {
		      requests: 0,
		      tokens: 0,
		      cost: 0,
		      standard_cost: 0,
		      user_cost: 0
		    }
		  },
		  global: {
		    stubs: {
		      UsageProgressBar: true,
		      AccountQuotaInfo: true
		    }
		  }
		})

		await flushPromises()

		expect(wrapper.text()).toContain('0 req')
		expect(wrapper.text()).toContain('0')
		expect(wrapper.text()).toContain('A $0.00')
		expect(wrapper.text()).toContain('U $0.00')
  })

  it('Anthropic OAuth 会渲染 7d F (Fable) 进度条，且 7d S 逻辑保留', async () => {
    getUsage.mockResolvedValue({
      source: 'passive',
      five_hour: {
        utilization: 41,
        resets_at: '2026-07-03T10:00:00Z',
        remaining_seconds: 3600
      },
      seven_day: {
        utilization: 56,
        resets_at: '2026-07-06T22:00:00Z',
        remaining_seconds: 300000
      },
      seven_day_sonnet: {
        utilization: 30,
        resets_at: '2026-07-06T22:00:00Z',
        remaining_seconds: 300000
      },
      seven_day_fable: {
        utilization: 100,
        resets_at: '2026-07-06T22:00:00Z',
        remaining_seconds: 300000
      }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({
          id: 3001,
          platform: 'anthropic',
          type: 'oauth',
          extra: {}
        })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization', 'resetsAt', 'color'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}</div>'
          },
          AccountQuotaInfo: true,
        }
      }
    })

    await flushPromises()

    expect(wrapper.text()).toContain('5h|41')
    expect(wrapper.text()).toContain('7d|56')
    expect(wrapper.text()).toContain('7d S|30')
    expect(wrapper.text()).toContain('7d F|100')
  })

  it('Anthropic OAuth 无 Fable 数据时不渲染 7d F 进度条', async () => {
    getUsage.mockResolvedValue({
      source: 'passive',
      five_hour: {
        utilization: 41,
        resets_at: '2026-07-03T10:00:00Z',
        remaining_seconds: 3600
      },
      seven_day: {
        utilization: 56,
        resets_at: '2026-07-06T22:00:00Z',
        remaining_seconds: 300000
      }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({
          id: 3002,
          platform: 'anthropic',
          type: 'oauth',
          extra: {}
        })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization', 'resetsAt', 'color'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}</div>'
          },
          AccountQuotaInfo: true,
        }
      }
    })

    await flushPromises()

    expect(wrapper.text()).toContain('5h|41')
    expect(wrapper.text()).toContain('7d|56')
    expect(wrapper.text()).not.toContain('7d S')
    expect(wrapper.text()).not.toContain('7d F')
  })
})

describe('OpenRouter key usage', () => {
  it('labels key spending and unlimited cap without inventing a wallet', async () => {
    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({ platform: 'openrouter', type: 'apikey' }),
        requestBatchedUsage: vi.fn(),
        batchedUsage: { five_hour: null, openrouter_key: { limit: null, remaining: null, unlimited: true, remaining_unlimited: true, limit_reset: null, usage: 12, daily: 1, weekly: 2, monthly: 3 } }
      }
    })
    await flushPromises()
    expect(wrapper.text()).toContain('admin.accounts.openrouterUsage.spending')
    expect(wrapper.text()).toContain('$12.0000')
    expect(wrapper.text()).toContain('admin.accounts.openrouterUsage.unlimited')
    expect(wrapper.text()).toContain('$1.0000 / $2.0000 / $3.0000')
    expect(wrapper.text()).not.toContain('noUpstreamUsageApi')
    await wrapper.setProps({ batchedUsage: { five_hour: null, openrouter_key: { limit: 20, remaining: 8, unlimited: false, remaining_unlimited: false, limit_reset: 'monthly', usage: 12, daily: null, weekly: null, monthly: null } } })
    await flushPromises()
    expect(wrapper.text()).toContain('$20.0000')
    expect(wrapper.text()).toContain('$8.0000')
    expect(wrapper.text()).not.toContain('admin.accounts.openrouterUsage.unlimited')
    await wrapper.setProps({ batchedUsage: { five_hour: null, openrouter_key: { limit: 10, remaining: null, unlimited: false, remaining_unlimited: true, limit_reset: null, usage: 1, daily: null, weekly: null, monthly: null } } })
    expect(wrapper.text()).toContain('$10.0000')
    expect(wrapper.text()).toContain('admin.accounts.openrouterUsage.remaining: admin.accounts.openrouterUsage.unlimited')
    await wrapper.setProps({ batchedUsage: { five_hour: null, openrouter_key: { limit: 10, remaining: null, unlimited: false, remaining_unlimited: false, limit_reset: null, usage: 1, daily: null, weekly: null, monthly: null } } })
    expect(wrapper.text()).toContain('admin.accounts.openrouterUsage.remaining: —')
    wrapper.unmount()
  })
})


describe('compatible usage routing', () => {
  it.each([
    ['astron_token', 'coding'], ['mimo_token_sg', 'coding'], ['atlascloud', 'coding'], ['qiniu', 'subscription'], ['together', 'payg']
  ])('explains unsupported %s/%s without a query', async (platform, account_mode) => {
    getUsage.mockClear()
    const wrapper = mount(AccountUsageCell, { props: { account: makeAccount({ platform: platform as Account['platform'], type: 'apikey', credentials: { account_mode } }) } })
    await flushPromises()
    expect(wrapper.find('[data-test="compatible-usage-unsupported"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('compatibleUsage.localLedger')
    expect(wrapper.find('button').exists()).toBe(false)
    expect(getUsage).not.toHaveBeenCalled()
  })
})

it.each([
  ['novita', 'coding_plan'], ['ppio', 'token_plan'], ['ppio', 'addon_pack'],
  ['shengsuanyun', 'lite'], ['shengsuanyun', 'pro'], ['shengsuanyun', 'enterprise']
])('hides wallet controls for saved %s/%s and restores them on clear', async (platform, billing_plan) => {
  getUsage.mockClear()
  const candidate = makeAccount({ platform: platform as Account['platform'], type: 'apikey', credentials: { billing_plan }, credentials_status: { has_api_key: true } })
  const wrapper = mount(AccountUsageCell, { props: { account: candidate } })
  await flushPromises()
  expect(wrapper.find('[data-test="compatible-usage-unsupported"]').exists()).toBe(true)
  expect(wrapper.find('button').exists()).toBe(false)
  expect(wrapper.text()).not.toContain('$0')
  expect(getUsage).not.toHaveBeenCalled()
  await wrapper.setProps({ account: { ...candidate, credentials: { billing_plan: null } } })
  await flushPromises()
  expect(wrapper.find('[data-test="compatible-usage-unsupported"]').exists()).toBe(false)
  expect(wrapper.find('button').exists()).toBe(true)
  expect(getUsage).not.toHaveBeenCalled()
  wrapper.unmount()
})

describe('Volcengine saved product quota routing', () => {
  it('routes Ark Coding into the quota cell and keeps Ark Agent and relays explicit', async () => {
    const account = makeAccount({ platform: 'volcengine', type: 'apikey', credentials: { account_mode: 'coding', api_protocol: 'chat_completions', base_url: 'https://ark.cn-beijing.volces.com/api/coding/v3' } })
    const wrapper = mount(AccountUsageCell, { props: { account }, global: { stubs: cnUsageCellStubs } })
    await flushPromises()
    expect(wrapper.find('[data-test="cn-quota-cell"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="local-usage-explanation"]').exists()).toBe(false)
    await wrapper.setProps({ account: { ...account, credentials: { ...account.credentials, base_url: 'https://ark.cn-beijing.volces.com/api/plan/v3' } } })
    expect(wrapper.find('[data-test="cn-quota-cell"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="local-usage-explanation"]').text()).toContain('volcengineQuota.agentUnverified')
    await wrapper.setProps({ account: { ...account, credentials: { ...account.credentials, base_url: 'https://relay.example/v1' } } })
    expect(wrapper.get('[data-testid="local-usage-explanation"]').text()).toContain('custom_origin')
    wrapper.unmount()
  })
})
