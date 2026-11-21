import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'
import AppLayout from '../AppLayout.vue'

const mocks = vi.hoisted(() => ({
  getAdminPulse: vi.fn(),
}))

vi.mock('@/api/mcp', () => ({
  mcpAPI: {
    getAdminPulse: mocks.getAdminPulse,
  },
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    sidebarCollapsed: false,
  }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    user: { id: 1, email: 'admin@example.test', role: 'admin' },
    isAdmin: true,
  }),
}))

vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({
    setReplayCallback: vi.fn(),
  }),
}))

vi.mock('@/composables/useOnboardingTour', () => ({
  useOnboardingTour: () => ({
    replayTour: vi.fn(),
  }),
}))

vi.mock('vue-router', () => ({
  RouterLink: { template: '<a><slot /></a>' },
  useRoute: () => ({ path: '/admin/dashboard', fullPath: '/admin/dashboard', meta: {}, query: {} }),
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params?.time ? `${key}:${String(params.time)}` : key,
    locale: { value: 'en' },
  }),
}))

describe('AppLayout admin telemetry honesty (E2-3)', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows failed/unknown when first read fails, disconnected with last observed time on drop, and recovers on success', async () => {
    mocks.getAdminPulse
      .mockResolvedValueOnce(null)
      .mockResolvedValueOnce({
        status: 'healthy',
        today_requests: 120,
        rpm: 5,
        tpm: 400,
        p95_latency_ms: 120,
        error_rate_percent: 0,
        active_accounts: 2,
        total_accounts: 2,
        updated_at: '2026-11-21T01:00:00Z',
      })
      .mockResolvedValueOnce(null)
      .mockResolvedValueOnce({
        status: 'healthy',
        today_requests: 150,
        rpm: 6,
        tpm: 500,
        p95_latency_ms: 110,
        error_rate_percent: 0,
        active_accounts: 2,
        total_accounts: 2,
        updated_at: '2026-11-21T01:01:00Z',
      })

    const wrapper = shallowMount(AppLayout, {
      props: { tier: 'admin' },
      global: { stubs: { AppSidebar: true, AppHeader: true, PublicShell: true, RouterLink: true } },
    })
    await flushPromises()

    // Phase 1: First read failed -> never green healthy
    const dot = wrapper.get('[data-testid="admin-telemetry-dot"]')
    expect(dot.classes()).not.toContain('bg-[var(--oh-ok-fg)]')
    expect(dot.attributes('data-telemetry-state')).toBe('failed')
    expect(wrapper.get('[data-testid="admin-telemetry-status"]').text()).toContain(
      'routing.telemetry.status.failed'
    )
    expect(wrapper.get('[data-testid="admin-telemetry-observed"]').text()).toContain(
      'routing.telemetry.lastObservedUnknown'
    )

    // Phase 2a: Succeeds
    await vi.advanceTimersByTimeAsync(30_000)
    await flushPromises()
    expect(dot.attributes('data-telemetry-state')).toBe('healthy')
    expect(dot.classes()).toContain('bg-[var(--oh-ok-fg)]')
    expect(wrapper.get('[data-testid="admin-telemetry-observed"]').text()).toContain('2026-11-21')

    // Phase 2b: Disconnects after success -> never green, preserves last observed timestamp
    await vi.advanceTimersByTimeAsync(30_000)
    await flushPromises()
    expect(dot.attributes('data-telemetry-state')).toBe('disconnected')
    expect(dot.classes()).not.toContain('bg-[var(--oh-ok-fg)]')
    expect(wrapper.get('[data-testid="admin-telemetry-status"]').text()).toContain(
      'routing.telemetry.status.disconnected'
    )
    expect(wrapper.get('[data-testid="admin-telemetry-observed"]').text()).toContain('2026-11-21')

    // Phase 3: Recovers
    await vi.advanceTimersByTimeAsync(30_000)
    await flushPromises()
    expect(dot.attributes('data-telemetry-state')).toBe('healthy')
    expect(dot.classes()).toContain('bg-[var(--oh-ok-fg)]')
  })
})
