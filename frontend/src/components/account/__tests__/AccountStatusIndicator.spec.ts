import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AccountStatusIndicator from '../AccountStatusIndicator.vue'
import type { Account } from '@/types'
import { formatDateTime } from '@/utils/format'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: { time?: string }) => params?.time ? `${key} ${params.time}` : key
    })
  }
})

vi.mock('@/utils/format', async () => {
  const actual = await vi.importActual<typeof import('@/utils/format')>('@/utils/format')
  return {
    ...actual,
    formatCountdown: () => '1h'
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

describe('AccountStatusIndicator', () => {
  const openCodeMonthlyAccount = () => makeAccount({
    platform: 'opencode_go', type: 'apikey',
    extra: {
      opencode_go_usage_updated_at: new Date().toISOString(),
      opencode_go_5h_used_percent: 0,
      opencode_go_5h_reset_at: '2099-01-01T00:00:00Z',
      opencode_go_weekly_used_percent: 0,
      opencode_go_monthly_used_percent: 100,
      opencode_go_monthly_reset_at: '2099-10-11T21:28:32Z'
    },
    rate_limit_reset_at: '2099-01-01T00:00:00Z'
  })

  it('shows monthly exhaustion and its actual reset despite rolling zero', () => {
    const wrapper = mount(AccountStatusIndicator, { props: { account: openCodeMonthlyAccount() } })
    expect(wrapper.text()).toContain('admin.accounts.status.monthlyQuotaExhausted')
    expect(wrapper.text()).toContain('admin.accounts.status.quotaResetsAt')
    expect(wrapper.text()).toContain(formatDateTime('2099-10-11T21:28:32Z'))
    expect(wrapper.text()).not.toContain(formatDateTime('2099-01-01T00:00:00Z'))
    expect(wrapper.text()).not.toContain('429')
    expect(wrapper.text()).not.toContain('admin.accounts.status.active')
  })

  it.each([
    { status: 'error', schedulable: true, error_message: 'HTTP 401 invalid key', label: 'error' },
    { status: 'error', schedulable: true, error_message: 'HTTP 403 forbidden', label: 'error' },
    { status: 'inactive', schedulable: false, label: 'inactive' },
    { status: 'active', schedulable: false, label: 'paused' }
  ] as const)('keeps $label ahead of old quota', ({ label, ...overrides }) => {
    const wrapper = mount(AccountStatusIndicator, {
      props: { account: { ...openCodeMonthlyAccount(), ...overrides } }
    })
    expect(wrapper.text()).toContain(`admin.accounts.status.${label}`)
    expect(wrapper.text()).not.toContain('admin.accounts.status.monthlyQuotaExhausted')
    expect(wrapper.text()).not.toContain('admin.accounts.status.rateLimited')
  })

  it('does not carry quota label across invalidation or elapsed reset', () => {
    const account = openCodeMonthlyAccount()
    account.rate_limit_reset_at = null
    account.extra = {}
    let wrapper = mount(AccountStatusIndicator, { props: { account } })
    expect(wrapper.text()).not.toContain('monthlyQuotaExhausted')
    account.extra = { ...openCodeMonthlyAccount().extra, opencode_go_monthly_reset_at: '2000-01-01T00:00:00Z' }
    wrapper = mount(AccountStatusIndicator, { props: { account } })
    expect(wrapper.text()).not.toContain('monthlyQuotaExhausted')
  })

  it('labels payment recheck without claiming a known quota reset or free capacity', () => {
    const wrapper = mount(AccountStatusIndicator, { props: { account: makeAccount({
      platform: 'opencode_go', type: 'apikey',
      temp_unschedulable_reason: 'opencode_payment_required: HTTP 402 Insufficient account funds',
      temp_unschedulable_until: '2099-01-01T00:00:00Z'
    }) } })
    expect(wrapper.text()).toContain('admin.accounts.status.paymentRequired')
    expect(wrapper.text()).toContain('admin.accounts.status.quotaRecheckAt')
    expect(wrapper.text()).not.toContain('admin.accounts.status.quotaResetsAt')
    expect(wrapper.text()).not.toContain('admin.accounts.status.active')
  })

  it('Claude 5 模型限流时显示 Opus 和 Sonnet 的短别名', () => {
    const wrapper = mount(AccountStatusIndicator, {
      props: {
        account: makeAccount({
          extra: {
            model_rate_limits: {
              'claude-opus-5': {
                rate_limited_at: '2026-07-28T00:00:00Z',
                rate_limit_reset_at: '2099-07-28T00:00:00Z'
              },
              'claude-sonnet-5': {
                rate_limited_at: '2026-07-28T00:00:00Z',
                rate_limit_reset_at: '2099-07-28T00:00:00Z'
              }
            }
          }
        })
      },
      global: {
        stubs: {
          Icon: true
        }
      }
    })

    expect(wrapper.text()).toContain('COpus5')
    expect(wrapper.text()).toContain('CSon5')
    expect(wrapper.text()).not.toContain('claude-sonnet-5')
  })

  it('Grok 账号额度限流时显示自动恢复时间而非临时不可调度', () => {
    const wrapper = mount(AccountStatusIndicator, {
      props: {
        account: makeAccount({
          id: 5,
          name: 'grok-free-1',
          platform: 'grok',
          rate_limited_at: '2026-07-11T12:00:00Z',
          rate_limit_reset_at: '2099-07-11T13:00:00Z',
          temp_unschedulable_until: '2099-07-11T12:30:00Z',
          temp_unschedulable_reason: 'legacy grok rate limited'
        })
      },
      global: {
        stubs: {
          Icon: true
        }
      }
    })

    expect(wrapper.find('.badge-warning').text()).toBe('admin.accounts.status.rateLimited')
    expect(wrapper.text()).toContain('admin.accounts.status.rateLimitedAutoResume')
    expect(wrapper.text()).not.toContain('admin.accounts.status.tempUnschedulable')
  })

  it('模型限流 + overages 启用 + 无 AICredits key → 显示 ⚡ (credits_active)', () => {
    const wrapper = mount(AccountStatusIndicator, {
      props: {
        account: makeAccount({
          id: 1,
          name: 'ag-1',
          extra: {
            allow_overages: true,
            model_rate_limits: {
              'claude-sonnet-4-5': {
                rate_limited_at: '2026-03-15T00:00:00Z',
                rate_limit_reset_at: '2099-03-15T00:00:00Z'
              }
            }
          }
        })
      },
      global: {
        stubs: {
          Icon: true
        }
      }
    })

    expect(wrapper.text()).toContain('⚡')
    expect(wrapper.text()).toContain('CSon45')
  })

  it('模型限流 + overages 未启用 → 普通限流样式（无 ⚡）', () => {
    const wrapper = mount(AccountStatusIndicator, {
      props: {
        account: makeAccount({
          id: 2,
          name: 'ag-2',
          extra: {
            model_rate_limits: {
              'claude-sonnet-4-5': {
                rate_limited_at: '2026-03-15T00:00:00Z',
                rate_limit_reset_at: '2099-03-15T00:00:00Z'
              }
            }
          }
        })
      },
      global: {
        stubs: {
          Icon: true
        }
      }
    })

    expect(wrapper.text()).toContain('CSon45')
    expect(wrapper.text()).not.toContain('⚡')
  })

  it('AICredits key 生效 → 显示积分已用尽 (credits_exhausted)', () => {
    const wrapper = mount(AccountStatusIndicator, {
      props: {
        account: makeAccount({
          id: 3,
          name: 'ag-3',
          extra: {
            allow_overages: true,
            model_rate_limits: {
              'AICredits': {
                rate_limited_at: '2026-03-15T00:00:00Z',
                rate_limit_reset_at: '2099-03-15T00:00:00Z'
              }
            }
          }
        })
      },
      global: {
        stubs: {
          Icon: true
        }
      }
    })

    expect(wrapper.text()).toContain('admin.accounts.status.creditsExhausted')
  })

  it('模型限流 + overages 启用 + AICredits key 生效 → 普通限流样式（积分耗尽，无 ⚡）', () => {
    const wrapper = mount(AccountStatusIndicator, {
      props: {
        account: makeAccount({
          id: 4,
          name: 'ag-4',
          extra: {
            allow_overages: true,
            model_rate_limits: {
              'claude-sonnet-4-5': {
                rate_limited_at: '2026-03-15T00:00:00Z',
                rate_limit_reset_at: '2099-03-15T00:00:00Z'
              },
              'AICredits': {
                rate_limited_at: '2026-03-15T00:00:00Z',
                rate_limit_reset_at: '2099-03-15T00:00:00Z'
              }
            }
          }
        })
      },
      global: {
        stubs: {
          Icon: true
        }
      }
    })

    // 模型限流 + 积分耗尽 → 不应显示 ⚡
    expect(wrapper.text()).toContain('CSon45')
    expect(wrapper.text()).not.toContain('⚡')
    // AICredits 积分耗尽状态应显示
    expect(wrapper.text()).toContain('admin.accounts.status.creditsExhausted')
  })
})
