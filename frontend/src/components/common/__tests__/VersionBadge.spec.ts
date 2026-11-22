import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import VersionBadge from '../VersionBadge.vue'

const { mockPerformUpdate, mockRestartService, mockFetchVersion, mockClearVersionCache, mockShowError } =
  vi.hoisted(() => ({
    mockPerformUpdate: vi.fn(),
    mockRestartService: vi.fn(),
    mockFetchVersion: vi.fn(),
    mockClearVersionCache: vi.fn(),
    mockShowError: vi.fn()
  }))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
    locale: { value: 'zh-CN' }
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    isAdmin: true
  })
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    versionLoading: false,
    currentVersion: '0.3.1',
    latestVersion: '0.3.2',
    hasUpdate: true,
    releaseInfo: {
      name: 'v0.3.2',
      body: 'Release notes',
      published_at: '2026-11-22T00:00:00Z',
      html_url: 'https://github.com/example/omnihive/releases/tag/v0.3.2'
    },
    buildType: 'release',
    fetchVersion: mockFetchVersion,
    clearVersionCache: mockClearVersionCache,
    showError: mockShowError
  })
}))

vi.mock('@/api/admin/system', () => ({
  default: {},
  performUpdate: mockPerformUpdate,
  restartService: mockRestartService,
  getRollbackVersions: vi.fn().mockResolvedValue({ versions: [], max_backups: 3 }),
  performRollback: vi.fn()
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copied: { value: false },
    copyToClipboard: vi.fn()
  })
}))

describe('VersionBadge safety and lifecycle', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    mockPerformUpdate.mockReset()
    mockRestartService.mockReset()
    mockFetchVersion.mockReset().mockResolvedValue(undefined)
    mockClearVersionCache.mockReset()
    mockShowError.mockReset()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('requires confirmation before update, preserves needRestart on refresh, surfaces 403 restart errors, and cleans timers on unmount', async () => {
    mockPerformUpdate.mockResolvedValueOnce({ need_restart: true })
    mockRestartService.mockRejectedValueOnce({
      response: { status: 403, data: { message: 'Forbidden restart' } }
    })

    const wrapper = mount(VersionBadge, {
      props: { version: '0.3.1' },
      global: {
        stubs: {
          Icon: { template: '<span />' },
          ConfirmDialog: {
            name: 'ConfirmDialog',
            props: ['show', 'title', 'message'],
            emits: ['confirm', 'cancel'],
            template:
              '<div v-if="show" data-testid="confirm-dialog"><button data-testid="confirm-ok" @click="$emit(\'confirm\')">ok</button></div>'
          }
        }
      }
    })
    await flushPromises()

    // Open dropdown
    await wrapper.find('button').trigger('click')
    await flushPromises()

    // Clicking Update opens ConfirmDialog without calling performUpdate immediately
    await wrapper.get('[data-testid="version-update-now-btn"]').trigger('click')
    expect(mockPerformUpdate).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="confirm-dialog"]').exists()).toBe(true)

    // Confirm triggers performUpdate -> sets needRestart
    await wrapper.get('[data-testid="confirm-ok"]').trigger('click')
    await flushPromises()
    expect(mockPerformUpdate).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('version.restartNow')

    // Refreshing version preserves pending needRestart banner
    const refreshBtn = wrapper.find('button[title="version.refresh"]')
    await refreshBtn.trigger('click')
    await flushPromises()
    expect(mockFetchVersion).toHaveBeenCalledWith(true)
    expect(wrapper.text()).toContain('version.restartNow')

    // Restart returning HTTP 403 surfaces error and resets restarting state instead of starting countdown
    const restartBtn = wrapper.findAll('button').find((b) => b.text().includes('version.restartNow'))!
    await restartBtn.trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="version-restart-error"]').text()).toContain('Forbidden restart')
    expect(mockShowError).toHaveBeenCalledWith('Forbidden restart')

    // Network disconnect restart starts countdown, and unmounting cleans up interval cleanly
    mockRestartService.mockRejectedValueOnce(new Error('Network Error'))
    await restartBtn.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('version.restarting')

    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: false } as Response)
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(15000)
    expect(fetchSpy).not.toHaveBeenCalled()
    fetchSpy.mockRestore()
  })
})
