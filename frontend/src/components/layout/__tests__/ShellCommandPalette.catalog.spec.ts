import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, reactive, ref } from 'vue'
import ShellCommandPalette from '../ShellCommandPalette.vue'

const mocks = vi.hoisted(() => ({ list: vi.fn(), push: vi.fn() }))
const app = reactive({
  commandPaletteOpen: false,
  cachedPublicSettings: { model_plaza_enabled: true, model_plaza_require_auth: false },
  scopes: [], activeScope: { mode: 'personal' }, publicSettingsLoaded: true,
})
const auth = reactive({ isAuthenticated: false, isAdmin: false, isSimpleMode: false, hasPendingAuthSession: false })
vi.mock('@/stores', () => ({ useAppStore: () => app, useAuthStore: () => auth }))
vi.mock('@/stores/adminSettings', () => ({ useAdminSettingsStore: () => ({}) }))
vi.mock('vue-router', () => ({ useRoute: () => ({ path: '/home' }), useRouter: () => ({ push: mocks.push }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ locale: ref('en'), t: (key: string) => key }) }))
vi.mock('@/composables/useThemeDirection', () => ({ useThemeDirection: () => ({ cmdkOpen: ref(false), themeMode: ref('system'), densityMode: ref('comfort'), setThemeMode: vi.fn(), setDensityMode: vi.fn(), closeCommandPalette: vi.fn() }) }))
vi.mock('@/router/navigationRegistry', () => ({ resolveVisibleTierNavItems: () => [] }))
vi.mock('@/features/public/api', () => ({ listPublicModels: mocks.list }))
vi.mock('@/api/client', () => ({ isSilentRequestError: () => false }))

const Palette = defineComponent({ name: 'CommandPalette', props: ['groups', 'open'], emits: ['queryChange'], template: '<div />' })
function render() { return mount(ShellCommandPalette, { global: { stubs: { CommandPalette: Palette } } }) }
function rows(wrapper: ReturnType<typeof render>) {
  return wrapper.findComponent(Palette).props('groups').find((group: { id: string }) => group.id === 'live-models')?.items ?? []
}

describe('Command palette canonical catalog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    app.commandPaletteOpen = false
    app.cachedPublicSettings = { model_plaza_enabled: true, model_plaza_require_auth: false }
    auth.isAuthenticated = false
  })

  it('retains distinct provider identities and navigates by the returned model ID', async () => {
    mocks.list.mockResolvedValue({ items: [{ id: 'm_a', display_name: 'Shared', provider: 'one' }, { id: 'm_b', display_name: 'Shared', provider: 'two' }] })
    const wrapper = render()
    app.commandPaletteOpen = true
    await flushPromises()
    expect(rows(wrapper)).toHaveLength(2)
    expect(rows(wrapper).map((item: { badge: string }) => item.badge)).toEqual(['ONE', 'TWO'])
    rows(wrapper)[1].onSelect()
    expect(mocks.push).toHaveBeenCalledWith({ path: '/model-plaza/m_b' })
    wrapper.unmount()
  })

  it('searches the canonical API rather than only the first preloaded page', async () => {
    vi.useFakeTimers()
    try {
      mocks.list.mockResolvedValue({ items: [] })
      const wrapper = render()
      app.commandPaletteOpen = true
      await flushPromises()
      wrapper.findComponent(Palette).vm.$emit('queryChange', 'model-on-page-90')
      await vi.advanceTimersByTimeAsync(181)
      await flushPromises()
      expect(mocks.list).toHaveBeenLastCalledWith(expect.objectContaining({ q: 'model-on-page-90', limit: 24 }), expect.objectContaining({ signal: expect.any(AbortSignal) }))
      wrapper.unmount()
    } finally { vi.useRealTimers() }
  })

  it('discards late catalog data after authentication is revoked', async () => {
    let resolve!: (value: unknown) => void
    mocks.list.mockImplementation(() => new Promise(done => { resolve = done }))
    auth.isAuthenticated = true
    app.cachedPublicSettings.model_plaza_require_auth = true
    const wrapper = render()
    app.commandPaletteOpen = true
    await flushPromises()
    const signal = mocks.list.mock.calls[0][1].signal as AbortSignal
    auth.isAuthenticated = false
    await flushPromises()
    resolve({ items: [{ id: 'm_private', display_name: 'Private', provider: 'one' }] })
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(rows(wrapper)).toEqual([])
    wrapper.unmount()
  })
})
