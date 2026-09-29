import { describe, expect, it, vi } from 'vitest'
import { create, update } from '@/api/admin/groups'
const { post, put } = vi.hoisted(() => ({ post: vi.fn(), put: vi.fn() }))
vi.mock('@/api/client', () => ({ default: { post, put }, apiClient: { post, put } }))

describe('group token limit API contract', () => {
  it('saves and reloads the shared group limit without changing RPM', async () => {
    post.mockResolvedValueOnce({ data: { id: 4, tpm_limit: 41, rpm_limit: 7 } })
    const group = await create({ name: 'Shared', platform: 'openai', tpm_limit: 41, rpm_limit: 7 })
    expect(group.tpm_limit).toBe(41)
    expect(group.rpm_limit).toBe(7)
    put.mockResolvedValueOnce({ data: { ...group, tpm_limit: 0 } })
    const cleared = await update(4, { tpm_limit: 0 })
    expect(cleared.tpm_limit).toBe(0)
    expect(put.mock.calls[0][1]).toEqual({ tpm_limit: 0 })
  })
  it('rejects fractional limits before a request', async () => {
    await expect(update(4, { tpm_limit: 0.5 })).rejects.toThrow('tpm_limit')
  })
})
