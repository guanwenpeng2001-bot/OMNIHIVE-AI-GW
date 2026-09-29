import { describe, expect, it, vi } from 'vitest'
import { normalizeHostedQuotaResponse, type PlatformQuotaItem } from '../admin/users'

vi.mock('../client', () => ({ apiClient: {} }))

describe('hosted quota response compatibility', () => {
  it('keeps separate direct and hosting budgets with every amount and window intact', () => {
    const rows = [
      { platform: 'anthropic', daily_usage_usd: 3.25, daily_limit_usd: 10 },
      { platform: 'anthropic', account_type: 'bedrock', daily_usage_usd: 8.75, daily_limit_usd: 20 }
    ] as PlatformQuotaItem[]
    const response = normalizeHostedQuotaResponse({ platform_quotas: rows })
    expect(response.platform_quotas).toEqual([rows[0], { ...rows[1], platform: 'bedrock' }])
    expect(rows[1]?.platform).toBe('anthropic')
  })
})
