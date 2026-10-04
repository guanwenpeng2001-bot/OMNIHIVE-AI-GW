import { COMPATIBLE_PROVIDERS } from '@/constants/compatibleProviders'
import { describe, expect, it } from 'vitest'
import { compatibleBalanceVisible, compatibleUsageCapability } from '../compatibleBalance'
import type { Account } from '@/types'

const account = (platform: string, credentials: Record<string, unknown> = {}) => ({
  id: 7, platform, type: 'apikey', credentials
}) as Account

describe('compatible balance eligibility', () => {
  it.each([
    ['teamorouter', 'https://api.teamorouter.cn/v1', 'https://api.teamorouter.com/v1', 'https://teamorouter.cn/v1'],
    ['teamorouter_com', 'https://api.teamorouter.com/v1', 'https://api.teamorouter.cn/v1', 'https://teamorouter.com/v1'],
    ['sudocode_chat', 'https://api.sudocode.chat/v1', 'https://api.sudocode.us/v1', 'https://api.sudocode.chat/api/account/balance']
  ])('checks the inference host separately from billing for %s', (platform, base, otherRegion, queryBase) => {
    const protocol = platform === 'teamorouter_com' ? 'chat_completions' : 'anthropic'
    expect(compatibleBalanceVisible(account(platform))).toBe(true)
    expect(compatibleBalanceVisible(account(platform, { base_url: base }))).toBe(true)
    expect(compatibleBalanceVisible(account(platform, { api_base_urls: { [protocol]: base } }))).toBe(true)
    expect(compatibleBalanceVisible(account(platform, { base_url: otherRegion }))).toBe(false)
    expect(compatibleBalanceVisible(account(platform, { base_url: queryBase }))).toBe(false)
    expect(compatibleBalanceVisible(account(platform, { api_base_urls: { [protocol]: otherRegion } }))).toBe(false)
  })
  it('does not reuse the chat balance contract for sudocode_us', () => {
    expect(compatibleBalanceVisible(account('sudocode_us'))).toBe(false)
  })
  it.each([
    ['stepfun_api', 'https://api.stepfun.com/v1', 'https://api.stepfun.ai/v1'],
    ['stepfun_api_intl', 'https://api.stepfun.ai/v1', 'https://api.stepfun.com/v1']
  ])('only shows the verified PAYG region for %s', (platform, base, otherRegion) => {
    expect(compatibleBalanceVisible(account(platform))).toBe(true)
    expect(compatibleBalanceVisible({ ...account(platform, { account_mode: 'payg', base_url: base }), credentials_status: { has_api_key: true } })).toBe(true)
    expect(compatibleBalanceVisible(account(platform, { base_url: otherRegion }))).toBe(false)
    expect(compatibleBalanceVisible(account(platform, { api_base_urls: { chat_completions: otherRegion } }))).toBe(false)
    expect(compatibleBalanceVisible(account(platform, { base_url: base.replace('/v1', '/step_plan/v1') }))).toBe(false)
    expect(compatibleBalanceVisible(account(platform, { account_mode: 'coding' }))).toBe(false)
    expect(compatibleBalanceVisible(account(platform, { billing_plan: 'step_plan' }))).toBe(false)
    expect(compatibleBalanceVisible({ ...account(platform), credentials_status: { has_api_key: false } })).toBe(false)
  })
  it.each(['stepfun_plan', 'stepfun_plan_intl'])('never presents an account balance query for %s', platform => {
    expect(compatibleBalanceVisible(account(platform))).toBe(false)
    expect(compatibleBalanceVisible(account(platform, { account_mode: 'payg', billing_plan: 'payg' }))).toBe(false)
  })
  it.each(['novita', 'ppio', 'siliconflow_cn', 'siliconflow_global', 'shengsuanyun', 'soleapi'])('shows documented payg %s', platform => {
    expect(compatibleBalanceVisible(account(platform))).toBe(true)
    expect(compatibleBalanceVisible(account(platform, { account_mode: 'payg' }))).toBe(true)
    expect(compatibleBalanceVisible(account(platform, { account_mode: 'coding' }))).toBe(false)
    expect(compatibleBalanceVisible(account(platform, { account_mode: 'invalid' }))).toBe(false)
  })
  it.each(['dmxapi_com', 'together', 'openrouter'])('hides unknown or privileged %s', platform => {
    expect(compatibleBalanceVisible(account(platform))).toBe(false)
  })
  it.each([
    { base_url: 'https://relay.example/v1' },
    { base_url: 'https://api.ppio.com/openai/v1' },
    { base_url: 'https://api.novita.ai/coding/v1' },
    { base_url: 'https://user@api.novita.ai/openai/v1' },
    { base_url: 'https://api.novita.ai/openai/v1?key=secret' },
    { api_base_urls: { anthropic: 'https://relay.example/v1' } },
    { api_protocol: 'responses' }
  ])('hides custom bases and invalid protocols %j', credentials => {
    expect(compatibleBalanceVisible(account('novita', credentials))).toBe(false)
  })
  it('accepts documented protocol bases and trailing slashes', () => {
    expect(compatibleBalanceVisible(account('ppio', { api_protocol: 'anthropic', base_url: 'https://api.ppio.com/anthropic/v1/' }))).toBe(true)
  })
})


describe('compatible usage registry matrix', () => {
  const wallets = ['novita', 'ppio', 'siliconflow_cn', 'siliconflow_global', 'shengsuanyun', 'soleapi', 'atlascloud', 'stepfun_api', 'stepfun_api_intl', 'teamorouter', 'teamorouter_com', 'sudocode_chat', 'aihubmix', 'dmxapi', 'crazyrouter', 'crazyrouter_intl']
  it('covers all 75 registered provider IDs and every declared mode', () => {
    expect(COMPATIBLE_PROVIDERS).toHaveLength(75)
    for (const provider of COMPATIBLE_PROVIDERS) {
      for (const mode of provider.modes) {
        const candidate = account(provider.id, { account_mode: mode.mode })
        const expected = mode.mode !== 'payg' ? 'unsupported' : wallets.includes(provider.id) ? 'wallet_balance' : provider.id === 'qiniu' ? 'consumed_usage' : provider.id === 'therouter' ? 'account_credits' : 'unsupported'
        expect(compatibleUsageCapability(candidate).kind, `${provider.id}/${mode.mode}`).toBe(expected)
      }
    }
  })
  it('supports redacted DTOs without accessing a secret', () => {
    expect(compatibleUsageCapability({ ...account('qiniu'), credentials_status: { has_api_key: true } }).kind).toBe('consumed_usage')
    expect(compatibleBalanceVisible({ ...account('atlascloud'), credentials_status: { has_api_key: true } })).toBe(true)
    expect(compatibleUsageCapability({ ...account('qiniu'), credentials_status: { has_api_key: false } }).reason).toBe('invalid_credentials')
  })
  it.each([
    { account_mode: 7 }, { api_key: {} }, { api_protocol: true }, { base_url: false },
    { api_base_urls: { anthropic: null } }, { api_base_urls: [] }, { api_base_urls: null }, { api_base_urls: { unknown: '' } },
    { base_url: 'https://api.siliconflow.cn:443/v1' }, { base_url: 'https://api.siliconflow.cn/other/../v1' },
    { base_url: 'https://api.siliconflow.com/v1' }
  ])('rejects unsafe inputs %j', credentials => {
    expect(compatibleUsageCapability(account('siliconflow_cn', credentials)).kind).toBe('unsupported')
  })
  it('trims mode and protocol and rejects a custom relay', () => {
    expect(compatibleUsageCapability(account('qiniu', { account_mode: ' payg ', api_protocol: ' adaptive ' })).kind).toBe('consumed_usage')
    expect(compatibleUsageCapability({ ...account('qiniu'), extra: { custom_base_url_enabled: true } }).reason).toBe('custom_origin')
  })
})

describe('persisted plan query eligibility', () => {
  it('checks every saved plan against legacy eligibility across all providers and modes', () => {
    expect(COMPATIBLE_PROVIDERS).toHaveLength(75)
    for (const provider of COMPATIBLE_PROVIDERS) {
      for (const mode of provider.modes) {
        const legacy = compatibleUsageCapability(account(provider.id, { account_mode: mode.mode }))
        for (const plan of provider.plans ?? []) {
          const candidate = { ...account(provider.id, { account_mode: mode.mode, billing_plan: plan.id }), credentials_status: { has_api_key: true } }
          expect(compatibleUsageCapability(candidate).kind, `${provider.id}/${mode.mode}/${plan.id}`).toBe(plan.query === 'payg' ? legacy.kind : 'unsupported')
          expect(candidate.credentials).not.toHaveProperty('api_key')
        }
        for (const cleared of [null, '']) {
          expect(compatibleUsageCapability(account(provider.id, { account_mode: mode.mode, billing_plan: cleared }))).toEqual(legacy)
        }
        for (const invalid of ['unknown', 'PAYG', ' payg ', 1, false, {}, []]) {
          expect(compatibleUsageCapability(account(provider.id, { account_mode: mode.mode, billing_plan: invalid })).kind).toBe('unsupported')
        }
      }
    }
  })
  it.each(['novita', 'ppio', 'shengsuanyun'])('keeps explicit PAYG subject to existing guards for %s', platform => {
    expect(compatibleBalanceVisible(account(platform, { billing_plan: 'payg' }))).toBe(true)
    expect(compatibleBalanceVisible(account(platform, { billing_plan: 'payg', base_url: 'https://relay.example/v1' }))).toBe(false)
    expect(compatibleBalanceVisible(account(platform, { billing_plan: 'payg', account_mode: 'coding' }))).toBe(false)
    expect(compatibleBalanceVisible({ ...account(platform, { billing_plan: 'payg' }), credentials_status: { has_api_key: false } })).toBe(false)
  })
  it('restores legacy eligibility when a selected plan is cleared', () => {
    const candidate = account('novita', { billing_plan: 'coding_plan' })
    expect(compatibleBalanceVisible(candidate)).toBe(false)
    candidate.credentials.billing_plan = null
    expect(compatibleBalanceVisible(candidate)).toBe(true)
    delete candidate.credentials.billing_plan
    expect(compatibleBalanceVisible(candidate)).toBe(true)
  })
})


describe('management query eligibility', () => {
  it.each(['aihubmix', 'dmxapi', 'crazyrouter', 'crazyrouter_intl', 'therouter'])('keeps %s configurable with missing management credentials', platform => {
    const kind = platform === 'therouter' ? 'account_credits' : 'wallet_balance'
    const missing = { ...account(platform), credentials_status: { has_api_key: false, has_usage_query_token: false } }
    expect(compatibleBalanceVisible(missing)).toBe(true)
    expect(compatibleUsageCapability(missing)).toEqual({ kind, reason: 'missing_query_credentials' })
    const configured = { ...missing, credentials: { usage_query_user_id: '42' }, credentials_status: { has_api_key: false, has_usage_query_token: true } }
    expect(compatibleUsageCapability(configured)).toEqual({ kind })
    expect(compatibleUsageCapability({ ...configured, credentials: { ...configured.credentials, base_url: 'https://relay.example/v1' } }).reason).toBe('custom_origin')
    expect(compatibleUsageCapability({ ...configured, credentials: { ...configured.credentials, account_mode: 'coding' } }).reason).toBe('mode_unsupported')
  })
  it.each(['dmxapi', 'crazyrouter', 'crazyrouter_intl'])('requires management user ID for %s', platform => {
    const saved = { ...account(platform), credentials_status: { has_usage_query_token: true } }
    expect(compatibleUsageCapability(saved).reason).toBe('missing_query_credentials')
    expect(compatibleUsageCapability({ ...saved, credentials: { usage_query_user_id: 'bad id' } }).reason).toBe('invalid_credentials')
  })
  it.each(['dmxapi_com', 'dmxapi_ssvip'])('never applies the domestic wallet contract to %s', platform => {
    expect(compatibleUsageCapability(account(platform)).reason).toBe('no_verified_query')
  })
})
