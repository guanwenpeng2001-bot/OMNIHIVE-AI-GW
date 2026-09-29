import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'
import { nextTick, reactive } from 'vue'
import App from '@/App.vue'

const mocks = vi.hoisted(() => ({
  auth: null as any,
  fetchSubscriptions: vi.fn().mockResolvedValue([]),
  startPolling: vi.fn(),
  stopPolling: vi.fn(),
  fetchAnnouncements: vi.fn(),
  clear: vi.fn(),
  removeRouteHook: vi.fn(),
}))
vi.mock('vue-router', () => ({
  RouterView: { template: '<div />' },
  useRouter: () => ({
    afterEach: () => mocks.removeRouteHook,
    replace: vi.fn(),
    currentRoute: { value: { path: '/dashboard', fullPath: '/dashboard', meta: {} } },
  }),
  useRoute: () => ({ path: '/dashboard', fullPath: '/dashboard', meta: {}, params: {} }),
}))
vi.mock('@/stores', () => ({
  useAuthStore: () => mocks.auth,
  useAppStore: () => ({ cachedPublicSettings: {}, fetchPublicSettings: vi.fn().mockResolvedValue({}) }),
  useSubscriptionStore: () => ({ fetchActiveSubscriptions: mocks.fetchSubscriptions, startPolling: mocks.startPolling, stopPolling: mocks.stopPolling, clear: mocks.clear }),
  useAnnouncementStore: () => ({ fetchAnnouncements: mocks.fetchAnnouncements, reset: vi.fn() }),
  useAdminComplianceStore: () => ({ reset: vi.fn(), fetchStatus: vi.fn().mockResolvedValue({}) }),
  useAdminSettingsStore: () => ({ reset: vi.fn(), customMenuItems: [] }),
}))
vi.mock('@/router/title', () => ({ resolveRouteDocumentTitle: () => 'Dashboard' }))
vi.mock('@/api/setup', () => ({ getSetupStatus: vi.fn().mockResolvedValue({ needs_setup: false }) }))
vi.mock('@/utils/featureFlags', () => ({ FeatureFlags: { subscription: 'subscription' }, isFeatureFlagEnabled: () => true, resolveFeatureFlag: () => true }))
vi.mock('@/components/common/Toast.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/common/NavigationProgress.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/common/AnnouncementPopup.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/admin/AdminComplianceDialog.vue', () => ({ default: { template: '<div />' } }))

describe('application identity synchronization', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
    mocks.auth = reactive({ user: { id: 1, role: 'user' }, isAuthenticated: true, isAdmin: false })
  })
  afterEach(() => { vi.clearAllTimers(); vi.useRealTimers() })

  it('preloads and restarts polling for a direct user switch but not a profile refresh', async () => {
    const wrapper = shallowMount(App)
    await flushPromises()
    expect(mocks.fetchSubscriptions).toHaveBeenCalledTimes(1)
    mocks.auth.user = { id: 1, role: 'user', balance: 20 }
    await nextTick()
    expect(mocks.fetchSubscriptions).toHaveBeenCalledTimes(1)
    mocks.auth.user = { id: 2, role: 'user' }
    await nextTick()
    expect(mocks.fetchSubscriptions).toHaveBeenCalledTimes(2)
    expect(mocks.startPolling).toHaveBeenCalledTimes(2)
    expect(mocks.fetchAnnouncements).toHaveBeenCalledTimes(2)
    wrapper.unmount()
    expect(mocks.stopPolling).toHaveBeenCalledOnce()
    expect(mocks.removeRouteHook).toHaveBeenCalledOnce()
  })

  it('cancels delayed login announcements on logout and unmount', async () => {
    mocks.auth.isAuthenticated = false
    const wrapper = shallowMount(App)
    await flushPromises()
    mocks.auth.isAuthenticated = true
    await nextTick()
    mocks.auth.isAuthenticated = false
    await nextTick()
    await vi.advanceTimersByTimeAsync(3000)
    expect(mocks.fetchAnnouncements).not.toHaveBeenCalled()
    mocks.auth.isAuthenticated = true
    await nextTick()
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(3000)
    expect(mocks.fetchAnnouncements).not.toHaveBeenCalled()
  })
})
