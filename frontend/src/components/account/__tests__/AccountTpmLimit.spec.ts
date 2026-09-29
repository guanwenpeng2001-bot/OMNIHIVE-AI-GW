import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import TpmLimitField from '@/components/common/TpmLimitField.vue'
import { bulkUpdate, update } from '@/api/admin/accounts'

const { put, post } = vi.hoisted(() => ({ put: vi.fn(), post: vi.fn() }))
vi.mock('@/api/client', () => ({ default: { put, post }, apiClient: { put, post } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('account token limits', () => {
  beforeEach(() => { put.mockReset().mockResolvedValue({ data: { id: 1, tpm_limit: 17 } }); post.mockReset().mockResolvedValue({ data: { success: 1 } }) })
  it('shows unlimited separately from RPM and USD', () => {
    const field = mount(TpmLimitField, { props: { modelValue: 0 } })
    expect(field.text()).toContain('admin.accounts.tpmUnlimited')
    expect(field.text()).toContain('admin.accounts.tpmLimitHint')
    expect(field.get('input').attributes('max')).toBe(String(Number.MAX_SAFE_INTEGER))
  })
  it('keeps omitted bulk values unchanged', async () => {
    await bulkUpdate([1, 2], { priority: 8 })
    expect(post.mock.calls[0][1]).not.toHaveProperty('tpm_limit')
    await bulkUpdate([1, 2], { tpm_limit: 0 })
    expect(post.mock.calls[1][1]).toHaveProperty('tpm_limit', 0)
  })
  it('saves and reads the server account limit', async () => {
    expect((await update(1, { tpm_limit: 17 })).tpm_limit).toBe(17)
    expect(put.mock.calls[0][1]).toEqual({ tpm_limit: 17 })
  })
  it.each([null, -1, 1.5, '7', Number.MAX_SAFE_INTEGER + 1, NaN])('rejects an unsafe value before sending: %s', async value => {
    await expect(bulkUpdate([1], { tpm_limit: value })).rejects.toThrow('tpm_limit')
    expect(post).not.toHaveBeenCalled()
  })
})
