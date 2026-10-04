import { COMPATIBLE_PROVIDERS } from '@/constants/compatibleProviders'
import { describe, expect, it } from 'vitest'
import { compatibleBalanceVisible, compatibleUsageCapability } from '../compatibleBalance'
import type { Account } from '@/types'

const account = (platform: string, credentials: Record<string, unknown> = {}) => ({
  id: 7, platform, type: 'apikey', credentials
}) as Account

describe('compatible balance eligibility', () => {
  it.each(['novita', 'ppio', 'siliconflow_cn', 'siliconflow_global', 'shengsuanyun', 'soleapi'])('shows documented payg %s', platform => {
    expect(compatibleBalanceVisible(account(platform))).toBe(true)
    expect(compatibleBalanceVisible(account(platform, { account_mode: 'payg' }))).toBe(true)
    expect(compatibleBalanceVisible(account(platform, { account_mode: 'coding' }))).toBe(false)
    expect(compatibleBalanceVisible(account(platform, { account_mode: 'invalid' }))).toBe(false)
  })
  it.each(['dmxapi', 'together', 'openrouter'])('hides unknown or privileged %s', platform => {
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
  const wallets = ['novita', 'ppio', 'siliconflow_cn', 'siliconflow_global', 'shengsuanyun', 'soleapi', 'atlascloud']
  it('covers all 75 registered provider IDs and every declared mode', () => {
    expect(COMPATIBLE_PROVIDERS).toHaveLength(75)
    for (const provider of COMPATIBLE_PROVIDERS) {
      for (const mode of provider.modes) {
        const candidate = account(provider.id, { account_mode: mode.mode })
        const expected = mode.mode !== 'payg' ? 'unsupported' : wallets.includes(provider.id) ? 'wallet_balance' : provider.id === 'qiniu' ? 'consumed_usage' : 'unsupported'
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
