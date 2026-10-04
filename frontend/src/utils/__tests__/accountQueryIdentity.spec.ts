import { describe, expect, it } from 'vitest'
import type { Account } from '@/types'
import { accountQueryIdentity } from '../accountQueryIdentity'

const account = {
  id: 7, platform: 'volcengine', type: 'apikey', runtime_identity: 'opaque-digest',
  credentials: { account_mode: 'coding', base_url: 'https://ark.cn-beijing.volces.com/api/coding/v3' },
  credentials_status: { has_volcengine_access_key_id: true, has_volcengine_secret_access_key: true },
  extra: { volcengine_5h_used_percent: 18 },
} as Account

describe('accountQueryIdentity', () => {
  it('ignores row, financial and snapshot metadata', () => {
    expect(accountQueryIdentity({ ...account, updated_at: 'later', notes: 'note', rate_multiplier: 2,
      extra: { ...account.extra, volcengine_5h_used_percent: 42, volcengine_usage_updated_at: 'later' }
    })).toBe(accountQueryIdentity(account))
  })
  it('recognizes a hidden secret rotation despite unchanged redaction flags', () => {
    expect(accountQueryIdentity({ ...account, runtime_identity: 'rotated-digest' })).not.toBe(accountQueryIdentity(account))
  })
  it('is stable across object property order changes', () => {
    expect(accountQueryIdentity({ ...account,
      credentials: { base_url: account.credentials!.base_url, account_mode: 'coding' },
      credentials_status: { has_volcengine_secret_access_key: true, has_volcengine_access_key_id: true }
    })).toBe(accountQueryIdentity(account))
  })
  it('supports older DTOs without treating updated_at as a secret version', () => {
    const legacy = { ...account, runtime_identity: undefined }
    expect(accountQueryIdentity({ ...legacy, updated_at: 'later' })).toBe(accountQueryIdentity(legacy))
    expect(accountQueryIdentity({ ...legacy, credentials: { ...legacy.credentials, volcengine_seat_id: 'new-seat' } })).not.toBe(accountQueryIdentity(legacy))
    expect(accountQueryIdentity({ ...legacy, extra: { upstream_model_catalog_state: { identity_revision: 2 } } })).not.toBe(accountQueryIdentity(legacy))
  })
})
