import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { baseCompile } from '@intlify/message-compiler'
import { createI18n } from 'vue-i18n'
import en from '@/i18n/locales/en/adminPlatform'
import zh from '@/i18n/locales/zh/adminPlatform'
import RestoreWorkflow from '../RestoreWorkflow.vue'

const { mockControlGet, mockControlPost } = vi.hoisted(() => ({
  mockControlGet: vi.fn(),
  mockControlPost: vi.fn()
}))

vi.mock('@/features/shared/controlClient', () => ({
  createIdempotencyKey: () => 'idem-restore-1',
  controlGet: mockControlGet,
  controlPost: mockControlPost
}))

vi.mock('@/composables/useStepUp', () => ({
  isStepUpCancelled: () => false,
  useStepUp: () => ({
    open: false,
    code: '',
    error: '',
    submitting: false,
    run: async <T>(fn: () => Promise<T>) => fn(),
    onSubmit: vi.fn(),
    onCancel: vi.fn()
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    user: { id: 7 },
    sessionRevision: 1
  })
}))

vi.mock('@/components/auth/TotpStepUpDialog.vue', () => ({
  default: { name: 'TotpStepUpDialog', template: '<div />' }
}))

function mountRestore(locale: 'en' | 'zh', backupId = 'bkp-1') {
  const i18n = createI18n({
    legacy: false,
    locale,
    messageCompiler: message => new Function('return ' + baseCompile(message as string).code)(),
    messages: { en: { adminPlatform: en }, zh: { adminPlatform: zh } }
  })
  return mount(RestoreWorkflow, { props: { backupId }, global: { plugins: [i18n] } })
}

describe('RestoreWorkflow resilience', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    sessionStorage.clear()
    mockControlGet.mockReset()
    mockControlPost.mockReset()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders the restore form and validation feedback in English', async () => {
    const wrapper = mountRestore('en')
    expect(wrapper.get('h3').text()).toBe('Restore backup')
    await wrapper.get('input').setValue('invalid')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.text()).toContain('64 lowercase hexadecimal characters')
    expect(wrapper.text()).not.toMatch(/\p{Script=Han}/u)
    wrapper.unmount()
  })

  it('keeps polling with friendly status during 503 RESTORE_MAINTENANCE until confirmed', async () => {
    sessionStorage.setItem('omnihive:restore:7', JSON.stringify({ operationId: 'op-maint-1' }))
    mockControlGet
      .mockResolvedValueOnce({
        id: 'op-maint-1',
        target: 'production',
        state: 'running',
        stage: 'restoring'
      })
      .mockRejectedValueOnce({
        response: {
          status: 503,
          data: {
            code: 503,
            reason: 'RESTORE_MAINTENANCE',
            message: 'Production restore maintenance. Retry after recovery.'
          }
        }
      })
      .mockResolvedValueOnce({
        id: 'op-maint-1',
        target: 'production',
        state: 'succeeded',
        stage: 'verified',
        report: { source_digest: 'sha256:abc', counts: { users: 1 } }
      })

    const wrapper = mountRestore('zh')
    await flushPromises()
    expect(mockControlGet).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(2500)
    await flushPromises()
    expect(mockControlGet).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('生产数据恢复维护模式')
    expect(wrapper.find('[data-testid="restore-retry-status-btn"]').exists()).toBe(true)

    await vi.advanceTimersByTimeAsync(2500)
    await flushPromises()
    expect(mockControlGet).toHaveBeenCalledTimes(3)
    expect(wrapper.text()).toContain('恢复已完成并通过校验')
    wrapper.unmount()
  })

  it('retries transient network errors with backoff and supports manual retry', async () => {
    sessionStorage.setItem('omnihive:restore:7', JSON.stringify({ operationId: 'op-net-1' }))
    mockControlGet
      .mockResolvedValueOnce({
        id: 'op-net-1',
        target: 'isolated',
        state: 'running',
        stage: 'extracting'
      })
      .mockRejectedValueOnce({ status: 0, message: 'Network Error' })
      .mockResolvedValueOnce({
        id: 'op-net-1',
        target: 'isolated',
        state: 'succeeded',
        stage: 'verified',
        report: { source_digest: 'sha256:def', counts: { users: 2 } }
      })

    const wrapper = mountRestore('zh', 'bkp-2')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2500)
    await flushPromises()
    expect(wrapper.text()).toContain('网络波动导致状态查询暂时失败')
    expect(wrapper.find('[data-testid="restore-retry-status-btn"]').exists()).toBe(true)

    await wrapper.get('[data-testid="restore-retry-status-btn"]').trigger('click')
    await flushPromises()
    expect(mockControlGet).toHaveBeenCalledTimes(3)
    expect(wrapper.text()).toContain('恢复已完成并通过校验')
    wrapper.unmount()
  })
})
