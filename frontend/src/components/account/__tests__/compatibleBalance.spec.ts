import { describe, expect, it } from 'vitest'
import { compatibleBalanceVisible } from '../compatibleBalance'
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
  it.each(['atlascloud', 'dmxapi', 'together', 'openrouter'])('hides unknown or privileged %s', platform => {
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
