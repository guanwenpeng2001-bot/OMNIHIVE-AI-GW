import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, shallowMount } from '@vue/test-utils'
import AppHeader from '../AppHeader.vue'

const state = vi.hoisted(() => ({
  user: { id: 1, email: 'user@example.test', role: 'user' } as object | null,
  plazaEnabled: true
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({ docUrl: '', customMenuItems: [] }),
  useAuthStore: () => ({ user: state.user, isSimpleMode: false }),
  useOnboardingStore: () => ({ replay: vi.fn() })
}))
vi.mock('@/stores/adminSettings', () => ({ useAdminSettingsStore: () => ({}) }))
vi.mock('@/utils/featureFlags', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/utils/featureFlags')>(),
  isFeatureFlagEnabled: () => state.plazaEnabled
}))
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
  useRoute: () => ({ meta: {}, matched: [], path: '/dashboard', query: {} })
}))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))

enableAutoUnmount(afterEach)
beforeEach(() => {
  state.user = { id: 1, email: 'user@example.test', role: 'user' }
  state.plazaEnabled = true
})

describe('AppHeader model plaza entry', () => {
  it('keeps the labeled icon visible below the small-screen breakpoint', () => {
    const wrapper = shallowMount(AppHeader, { global: { stubs: { RouterLink: true } } })
    const link = wrapper.get('[aria-label="nav.modelPlaza"]')
    expect(link.classes()).toContain('flex')
    expect(link.classes()).not.toContain('hidden')
    expect(link.classes()).not.toContain('sm:flex')
    expect(link.attributes('title')).toBe('nav.modelPlaza')
  })

  it.each(['signed-out', 'feature-disabled'])('keeps the entry hidden when %s', (condition) => {
    if (condition === 'signed-out') state.user = null
    else state.plazaEnabled = false
    expect(shallowMount(AppHeader, { global: { stubs: { RouterLink: true } } }).find('[aria-label="nav.modelPlaza"]').exists()).toBe(false)
  })
})
