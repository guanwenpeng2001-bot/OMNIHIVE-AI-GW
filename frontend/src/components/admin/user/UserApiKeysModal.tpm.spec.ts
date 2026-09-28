import { mount, flushPromises } from '@vue/test-utils'
import UserApiKeysModal from './UserApiKeysModal.vue'
import type { AdminUser } from '@/types'
import { describe, expect, it, vi } from 'vitest'
import { setTpmLimit } from '@/api/admin/apiKeys'
const { put, readKeys, saved } = vi.hoisted(() => ({ put: vi.fn(), readKeys: vi.fn(), saved: { limit: 8 } }))
vi.mock('@/api/client', () => ({ default: { put }, apiClient: { put } }))

vi.mock('vue-i18n', async () => ({ ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('@/api/admin', async () => ({ adminAPI: { apiKeys: await import('@/api/admin/apiKeys'), users: { getUserApiKeys: readKeys }, groups: { getAll: vi.fn().mockResolvedValue([]) } } }))

describe('administrator key token limit', () => {
  it('saves from the modal and reloads the persisted key', async () => {
    readKeys.mockReset().mockImplementation(async () => ({ items: [{ id: 7, name: 'Key', key: '****key', tpm_limit: saved.limit, created_at: '2026-01-01T00:00:00Z', status: 'active' }] }))
    put.mockReset().mockImplementation(async (_url, payload) => { saved.limit = payload.tpm_limit; return { data: { id: 7, tpm_limit: saved.limit } } })
    const wrapper = mount(UserApiKeysModal, { props: { show: false, user: { id: 3, email: 'owner@example.test', username: 'owner' } as AdminUser }, global: { stubs: { BaseDialog: { template: '<div><slot /></div>' }, KeyRotationDialog: true, KeySecretDialog: true, ConfirmDialog: true, ModelAccessRuleEditor: true, GroupBadge: true } } })
    await wrapper.setProps({ show: true })
    await flushPromises()
    await wrapper.get('[data-testid="tpm-limit-field"] input').setValue('43')
    await wrapper.findAll('button').find(button => button.text() === 'common.save')!.trigger('click')
    await flushPromises()
    expect(saved.limit).toBe(43)
    expect(readKeys).toHaveBeenCalledTimes(2)
    expect((wrapper.get('[data-testid="tpm-limit-field"] input').element as HTMLInputElement).value).toBe('43')
    wrapper.unmount()
  })
  it('uses the admin-only route and reads the saved value', async () => {
    put.mockResolvedValueOnce({ data: { id: 7, tpm_limit: 42 } })
    const key = await setTpmLimit(7, 42)
    expect(put).toHaveBeenCalledWith('/admin/api-keys/7/tpm', { tpm_limit: 42 })
    expect(key.tpm_limit).toBe(42)
  })
  it('does not grant users admin writes through a user key endpoint', async () => {
    put.mockClear().mockResolvedValueOnce({ data: { id: 7, tpm_limit: 0 } })
    await setTpmLimit(7, 0)
    expect(put.mock.calls.every(([url]) => String(url).startsWith('/admin/api-keys/'))).toBe(true)
    await expect(setTpmLimit(7, -1)).rejects.toThrow('tpm_limit')
    expect(put).toHaveBeenCalledTimes(1)
  })
})
