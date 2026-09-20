import { beforeEach, describe, expect, it, vi } from 'vitest'
const { post } = vi.hoisted(() => ({ post: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { post } }))
import { create, batchCreate } from '@/api/admin/accounts'
import { create as createKey } from '@/api/keys'
import type { CreateAccountRequest } from '@/types'

beforeEach(() => post.mockReset())
describe('logical create idempotency', () => {
  it.each(['account', 'batch', 'key'])('reuses %s retry keys and rotates for a changed body and after success', async kind => {
    const submit = (name: string) => kind === 'key' ? createKey(name) : kind === 'batch'
      ? batchCreate([{ name, platform: 'openai', type: 'apikey', credentials: { api_key: 'test' } } as CreateAccountRequest])
      : create({ name, platform: 'openai', type: 'apikey', credentials: { api_key: 'test' } } as CreateAccountRequest)
    post.mockRejectedValueOnce(new Error('lost response')).mockResolvedValue({ data: {} })
    await expect(submit(`${kind}-same`)).rejects.toThrow('lost response')
    await submit(`${kind}-changed`)
    await submit(`${kind}-same`)
    await submit(`${kind}-same`)
    const keys = post.mock.calls.map(call => call[2].headers['Idempotency-Key'])
    expect(keys.every(Boolean)).toBe(true)
    expect(keys[0]).toBe(keys[2])
    expect(keys[1]).not.toBe(keys[0])
    expect(keys[3]).not.toBe(keys[2])
  })
})
