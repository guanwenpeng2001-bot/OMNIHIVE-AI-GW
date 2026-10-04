import { afterEach, describe, expect, it } from 'vitest'
import { apiClient } from '../client'
import { queryTogetherUsage, togetherBillingVisible } from '../admin/togetherUsage'
import type { Account } from '@/types'

const originalAdapter = apiClient.defaults.adapter
afterEach(() => { apiClient.defaults.adapter = originalAdapter })
describe('Together API contract', () => {
  it('uses real request interceptors and serializes only month and day', async () => {
    let serialized = ''
    apiClient.defaults.adapter = async config => {
      serialized = apiClient.getUri(config)
      return { data: { code: 0, data: { provider: 'together', success: false, reason: 'not_enabled' } }, status: 200, statusText: 'OK', headers: {}, config }
    }
    const result = await queryTogetherUsage(12, '2026-06')
    const url = new URL(serialized, 'https://local.example')
    expect(url.pathname).toContain('/admin/cn-providers/accounts/12/usage')
    expect([...url.searchParams.entries()].sort()).toEqual([['granularity', 'day'], ['month', '2026-06']])
    expect(result.reason).toBe('not_enabled')
  })
  it.each([
    { account_mode: 'coding' }, { api_protocol: 'anthropic' }, { base_url: 'https://api.together.ai:443/v1' },
    { base_url: 'https://api.together.ai/v1?key=secret' }, { api_base_urls: { chat_completions: 'https://relay.example/v1' } }, { billing_plan: 'unknown' }
  ])('rejects unsupported configuration %j', credentials => {
    expect(togetherBillingVisible({ platform: 'together', type: 'apikey', credentials } as Account)).toBe(false)
  })
})
