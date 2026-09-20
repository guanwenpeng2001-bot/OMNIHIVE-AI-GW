import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CursorBrowserAuthorization from '../CursorBrowserAuthorization.vue'

const api = vi.hoisted(() => ({ post: vi.fn(), delete: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: api }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('Cursor browser authorization', () => {
  beforeEach(() => { vi.useFakeTimers(); vi.clearAllMocks(); api.delete.mockResolvedValue({ data: {} }) })
  afterEach(() => { vi.useRealTimers() })
  const startResponse = () => ({ data: { id: 'synthetic-session', status: 'waiting', url: 'https://cursor.com/loginDeepControl?challenge=synthetic', expires_at: new Date(Date.now() + 600000).toISOString() } })

  it('shows the URL and emits success only after the server reports persisted credentials', async () => {
    api.post.mockResolvedValueOnce(startResponse()).mockResolvedValueOnce({ data: { status: 'waiting' } }).mockResolvedValueOnce({ data: { status: 'success', account_id: 42 } })
    const wrapper = mount(CursorBrowserAuthorization, { props: { account: { name: 'Auto', platform: 'cursor', type: 'oauth', credentials: {} }, clientVersion: '' } })
    await wrapper.find('button').trigger('click'); await flushPromises()
    expect(wrapper.find('a').attributes('href')).toContain('cursor.com/loginDeepControl')
    expect(wrapper.emitted('success')).toBeUndefined()
    await vi.advanceTimersByTimeAsync(4000); await flushPromises()
    expect(wrapper.emitted('success')).toBeUndefined()
    await vi.advanceTimersByTimeAsync(4000); await flushPromises()
    expect(wrapper.emitted('success')).toEqual([[42]])
    expect(wrapper.text()).toContain('authStatus.success')
    wrapper.unmount()
  })

  it('cancels remote polling on unmount', async () => {
    api.post.mockResolvedValueOnce(startResponse())
    const wrapper = mount(CursorBrowserAuthorization, { props: { accountId: 42, clientVersion: '' } })
    await wrapper.find('button').trigger('click'); await flushPromises(); wrapper.unmount(); await flushPromises()
    expect(api.delete).toHaveBeenCalledWith('/admin/accounts/cursor/auth/synthetic-session')
    await vi.advanceTimersByTimeAsync(20000)
    expect(api.post).toHaveBeenCalledTimes(1)
  })

  it.each(['failed', 'timeout', 'cancelled'])('shows the %s terminal state without saving again', async (status) => {
    api.post.mockResolvedValueOnce(startResponse()).mockResolvedValueOnce({ data: { status } })
    const wrapper = mount(CursorBrowserAuthorization, { props: { accountId: 42, clientVersion: '' } })
    await wrapper.find('button').trigger('click'); await flushPromises()
    await vi.advanceTimersByTimeAsync(4000); await flushPromises()
    expect(wrapper.text()).toContain(`authStatus.${status}`)
    expect(wrapper.emitted('success')).toBeUndefined()
    await vi.advanceTimersByTimeAsync(20000)
    expect(api.post).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })
})
