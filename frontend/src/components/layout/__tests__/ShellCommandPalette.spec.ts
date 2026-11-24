import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { reactive, ref } from 'vue'
import ShellCommandPalette from '../ShellCommandPalette.vue'

const { apiGetMock, routerPushMock } = vi.hoisted(() => ({
  apiGetMock: vi.fn(),
  routerPushMock: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get: apiGetMock,
    post: vi.fn()
  }
}))

const appState = reactive({
  commandPaletteOpen: false,
  backendModeEnabled: false,
  publicSettingsLoaded: true,
  cachedPublicSettings: {
    model_plaza_enabled: true,
    model_plaza_require_auth: false
  },
  activeScope: null as null | { mode: string; workspaceId?: string; projectId?: string }
})

const authState = reactive({
  isAuthenticated: false,
  isAdmin: false,
  isSimpleMode: false,
  hasPendingAuthSession: false
})

vi.mock('@/stores', () => ({
  useAppStore: () => appState,
  useAuthStore: () => authState
}))
vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: () => ({
    opsMonitoringEnabled: false,
    paymentEnabled: true
  })
}))
vi.mock('@/composables/useThemeDirection', () => ({
  useThemeDirection: () => ({
    cmdkOpen: ref(false),
    themeMode: ref('dark'),
    densityMode: ref('comfort'),
    setThemeMode: vi.fn(),
    setDensityMode: vi.fn(),
    closeCommandPalette: vi.fn()
  })
}))
vi.mock('@/router/navigationRegistry', async (importOriginal) => {
  return await importOriginal<typeof import('@/router/navigationRegistry')>()
})
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: routerPushMock }),
  useRoute: () => ({ path: '/model-plaza', query: {}, meta: {} })
}))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key, locale: ref('en') })
}))

const wrappers: Array<ReturnType<typeof mount>> = []

beforeEach(() => {
  apiGetMock.mockReset()
  routerPushMock.mockReset()
  appState.commandPaletteOpen = false
  appState.activeScope = null
  authState.isAuthenticated = false
})

afterEach(() => {
  wrappers.splice(0).forEach((w) => w.unmount())
})

describe('ShellCommandPalette v2 catalog integration', () => {
  it('queries v2 /api/v2/public/models (never v1 /model-plaza), omits non-live models, and navigates to detail', async () => {
    apiGetMock.mockResolvedValueOnce({
      data: {
        items: [
          {
            id: 'm_live_1',
            provider: 'anthropic',
            model: 'claude-sonnet-4-5',
            display_name: 'Claude Sonnet 4.5',
            availability: { state: 'live', publication: 'published', access: 'allowed' },
            protocols: ['anthropic_messages']
          },
          {
            id: 'm_cat_1',
            provider: 'meta',
            model: 'llama-4-scout',
            display_name: 'Llama 4 Scout',
            availability: { state: 'unavailable', publication: 'unpublished', access: 'denied', reason: 'platform_catalog' }
          }
        ]
      }
    })

    const wrapper = mount(ShellCommandPalette, {
      global: {
        stubs: {
          CommandPalette: {
            props: ['open', 'groups', 'loading'],
            template: `
              <div>
                <div v-for="group in groups" :key="group.id">
                  <button
                    v-for="item in group.items"
                    :key="item.id"
                    :data-item-id="item.id"
                    @click="item.onSelect()"
                  >
                    {{ item.label }} · {{ item.badge }}
                  </button>
                </div>
              </div>
            `
          }
        }
      }
    })
    wrappers.push(wrapper)

    appState.commandPaletteOpen = true
    await flushPromises()

    expect(apiGetMock).toHaveBeenCalledWith(
      '/api/v2/public/models',
      expect.objectContaining({
        params: expect.objectContaining({ availability_state: 'live', limit: 24, sort: 'availability' })
      })
    )
    expect(wrapper.find('[data-item-id="mdl-m_live_1"]').text()).toContain('Claude Sonnet 4.5')
    expect(wrapper.find('[data-item-id="mdl-m_live_1"]').text()).toContain('Available')
    expect(wrapper.find('[data-item-id="mdl-m_cat_1"]').exists()).toBe(false)

    await wrapper.get('[data-item-id="mdl-m_live_1"]').trigger('click')
    expect(routerPushMock).toHaveBeenCalledWith('/model-plaza/m_live_1')
  })

  it('includes /docs, /compatibility, and /model-plaza/compare in PUBLIC palette items with search keywords [P1-2]', async () => {
    apiGetMock.mockResolvedValueOnce({ data: { items: [] } })

    let capturedGroups: Array<{
      id: string
      items: Array<{ id: string; description?: string; keywords?: string[]; onSelect?: () => void }>
    }> = []

    const wrapper = mount(ShellCommandPalette, {
      global: {
        stubs: {
          CommandPalette: {
            props: ['open', 'groups', 'loading'],
            setup(props: { groups: typeof capturedGroups }) {
              capturedGroups = props.groups
              return () => null
            }
          }
        }
      }
    })
    wrappers.push(wrapper)

    appState.commandPaletteOpen = true
    await flushPromises()

    const publicGroup = capturedGroups.find((g) => g.id === 'nav-public')
    expect(publicGroup).toBeDefined()

    const docsItem = publicGroup!.items.find((item) => item.description === '/docs')
    const compatItem = publicGroup!.items.find((item) => item.description === '/compatibility')
    const compareItem = publicGroup!.items.find((item) => item.description === '/model-plaza/compare')

    expect(docsItem?.keywords).toEqual(expect.arrayContaining(['docs', 'documentation', '文档']))
    expect(compatItem?.keywords).toEqual(expect.arrayContaining(['compatibility', '兼容']))
    expect(compareItem?.keywords).toEqual(expect.arrayContaining(['compare', '对比']))

    docsItem?.onSelect?.()
    compatItem?.onSelect?.()
    compareItem?.onSelect?.()

    expect(routerPushMock).toHaveBeenCalledWith('/docs')
    expect(routerPushMock).toHaveBeenCalledWith('/compatibility')
    expect(routerPushMock).toHaveBeenCalledWith('/model-plaza/compare')
  })
})
