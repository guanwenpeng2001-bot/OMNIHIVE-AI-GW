import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createI18n } from 'vue-i18n'
import { ref } from 'vue'
import PublicCompareView from '../PublicCompareView.vue'
import PublicModelsView from '../PublicModelsView.vue'
import PublicModelDetailView from '../PublicModelDetailView.vue'

const { apiGetMock, apiPostMock } = vi.hoisted(() => ({
  apiGetMock: vi.fn(),
  apiPostMock: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get: apiGetMock,
    post: apiPostMock
  }
}))

const storeMock = {
  siteName: 'OmniHive',
  siteLogo: '',
  docUrl: '',
  cachedPublicSettings: { registration_enabled: true, backend_mode_enabled: false, doc_url: '' },
  isAuthenticated: false,
  isAdmin: false,
  fetchPublicSettings: vi.fn(),
  openCommandPalette: vi.fn()
}

vi.mock('@/stores', () => ({
  useAppStore: () => storeMock,
  useAuthStore: () => storeMock
}))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => storeMock
}))
vi.mock('@/composables/useThemeDirection', () => ({
  useThemeDirection: () => ({ isDark: ref(true), toggleDarkMode: vi.fn() })
}))
vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard: vi.fn().mockResolvedValue(true) })
}))

const MODEL_A_ID = 'm_0123456789abcdef0123456789abcdef'
const MODEL_B_ID = 'm_fedcba9876543210fedcba9876543210'

const sampleCatalogItems = [
  {
    id: MODEL_A_ID,
    provider: 'openai',
    model: 'auto/gemini-3.8-flash',
    display_name: 'Gemini 3.8 Flash',
    context_tokens: '1000000',
    modalities: ['text', 'image'],
    protocols: ['openai_chat'],
    actions: ['text.chat'],
    regions: ['eu', 'us'],
    maturity: 'stable',
    route_state: 'eligible',
    price_state: 'priced',
    input_rate: { currency: 'USD', amount: '0.075', per: '1000000', unit: 'input_token' },
    output_rate: { currency: 'USD', amount: '0.3', per: '1000000', unit: 'output_token' }
  },
  {
    id: MODEL_B_ID,
    provider: 'anthropic',
    model: 'claude-sonnet-5-5',
    display_name: 'Claude Sonnet 5.5',
    context_tokens: '200000',
    modalities: ['text'],
    protocols: ['anthropic_messages'],
    actions: ['text.chat'],
    regions: ['us'],
    maturity: 'stable',
    route_state: 'eligible',
    price_state: 'priced',
    input_rate: { currency: 'USD', amount: '3', per: '1000000', unit: 'input_token' },
    output_rate: { currency: 'USD', amount: '15', per: '1000000', unit: 'output_token' }
  }
]

const wrappers: Array<ReturnType<typeof mount>> = []

beforeEach(() => {
  apiGetMock.mockReset()
  apiPostMock.mockReset()
  window.localStorage.clear()
})

afterEach(() => {
  wrappers.splice(0).forEach((w) => w.unmount())
})

describe('PublicCompareView, PublicModelsView & PublicModelDetailView (FE-10-16, FE-F05-01)', () => {
  it('recalculates interactive scenario cost and renders metrics.window + metrics.source in PublicCompareView', async () => {
    apiGetMock.mockResolvedValueOnce({
      data: { items: sampleCatalogItems, next_cursor: null }
    })
    apiPostMock.mockImplementation(async (_url: string, body: { scenario?: { requests?: string } }) => {
      const isUpdatedScenario = body?.scenario?.requests === '250'
      return {
        data: {
          catalog_version: 'cat_v1',
          as_of: '2026-11-20T12:00:00Z',
          rows: [
            {
              model_id: MODEL_A_ID,
              capabilities: { tools: 'supported', stream: 'supported' },
              metrics: {
                window: '24h (2026-11-19 → 2026-11-20)',
                source: 'gateway_observations',
                requests: '1420',
                success_rate: 0.9985,
                ttft_p50_ms: 210,
                output_tokens_per_second: 148.4
              },
              quote: {
                state: 'estimated',
                price_version: 'pv_20261120',
                estimated_charge: {
                  currency: 'USD',
                  amount: isUpdatedScenario ? '1.125' : '0.135'
                },
                components: isUpdatedScenario
                  ? []
                  : [
                      { kind: 'input', quantity: '800000', raw_amount: '0.06' },
                      { kind: 'cache_read', quantity: '200000', raw_amount: '0.015' },
                      { kind: 'output', quantity: '200000', raw_amount: '0.06' }
                    ]
              }
            },
            {
              model_id: MODEL_B_ID,
              capabilities: { tools: 'supported', stream: 'supported' },
              metrics: {
                window_start: '2026-11-19T12:00:00Z',
                window_end: '2026-11-20T12:00:00Z',
                source: 'unavailable',
                requests: null,
                state: 'insufficient_samples'
              },
              quote: {
                state: 'estimated',
                estimated_charge: {
                  currency: 'USD',
                  amount: isUpdatedScenario ? '45.00' : '5.46'
                },
                components: []
              }
            }
          ]
        }
      }
    })

    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/model-plaza/compare', component: PublicCompareView },
        { path: '/:pathMatch(.*)*', component: { template: '<div />' } }
      ]
    })
    await router.push(`/model-plaza/compare?ids=${encodeURIComponent('auto/gemini-3.8-flash')},${MODEL_B_ID}`)
    await router.isReady()

    const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: {}, zh: {} } })
    const wrapper = mount(PublicCompareView, {
      global: {
        plugins: [router, i18n],
        stubs: { SiteICPFooter: true, LocaleSwitcher: true }
      }
    })
    wrappers.push(wrapper)
    await flushPromises()

    // Initial compare resolves canonical slug to stable ID and sends default scenario
    expect(apiPostMock).toHaveBeenNthCalledWith(1, '/api/v2/public/models/compare', {
      model_ids: [MODEL_A_ID, MODEL_B_ID],
      scenario: {
        input_tokens: '10000',
        cached_input_tokens: '2000',
        output_tokens: '2000',
        requests: '100'
      }
    })

    // Renders metrics.window, metrics.source, and sample count (requests)
    const windowRow = wrapper.get('tr[data-metric-key="metrics-window"]')
    expect(windowRow.text()).toContain('24h (2026-11-19 → 2026-11-20)')
    const sourceRow = wrapper.get('tr[data-metric-key="metrics-source"]')
    expect(sourceRow.text()).toContain('Gateway live traffic')
    expect(sourceRow.text()).toContain('Unobserved')
    const requestsRow = wrapper.get('tr[data-metric-key="metrics-requests"]')
    expect(requestsRow.text()).toContain('1,420')

    // Update scenario calculator inputs and recalculate
    await wrapper.get('[data-testid="scenario-input-tokens"]').setValue('50000')
    await wrapper.get('[data-testid="scenario-cached-tokens"]').setValue('10000')
    await wrapper.get('[data-testid="scenario-output-tokens"]').setValue('5000')
    await wrapper.get('[data-testid="scenario-request-count"]').setValue('250')
    await wrapper.get('[data-testid="scenario-recalculate"]').trigger('click')
    await flushPromises()

    expect(apiPostMock).toHaveBeenLastCalledWith('/api/v2/public/models/compare', {
      model_ids: [MODEL_A_ID, MODEL_B_ID],
      scenario: {
        input_tokens: '50000',
        cached_input_tokens: '10000',
        output_tokens: '5000',
        requests: '250'
      }
    })
    const quoteRow = wrapper.get('tr[data-metric-key="scenario-quote"]')
    expect(quoteRow.text()).toContain('$1.125')
    expect(quoteRow.text()).toContain('$45.00')
  })

  it('wires region & retention filters in PublicModelsView and renders identical adaptive rates on /model-plaza/auto%2Fgemini-3.8-flash', async () => {
    apiGetMock.mockResolvedValue({
      data: { items: [sampleCatalogItems[0]], next_cursor: null, as_of: '2026-11-20T12:00:00Z' }
    })

    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/model-plaza', component: PublicModelsView },
        { path: '/model-plaza/:id(.+)', component: PublicModelDetailView },
        { path: '/:pathMatch(.*)*', component: { template: '<div />' } }
      ]
    })
    await router.push('/model-plaza')
    await router.isReady()

    const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: {}, zh: {} } })
    const listWrapper = mount(PublicModelsView, {
      global: {
        plugins: [router, i18n],
        stubs: { SiteICPFooter: true, LocaleSwitcher: true }
      }
    })
    wrappers.push(listWrapper)
    await flushPromises()

    // List displays $0.075 and $0.30
    expect(listWrapper.text()).toContain('$0.075')
    expect(listWrapper.text()).toContain('$0.30')

    // Click region=eu and retention=zero filters
    await listWrapper.get('[data-testid="filter-region-eu"]').trigger('click')
    await flushPromises()
    expect(apiGetMock).toHaveBeenLastCalledWith(
      '/api/v2/public/models',
      expect.objectContaining({
        params: expect.objectContaining({ region: ['eu'] })
      })
    )

    await listWrapper.get('[data-testid="filter-retention-zero"]').trigger('click')
    await flushPromises()
    expect(apiGetMock).toHaveBeenLastCalledWith(
      '/api/v2/public/models',
      expect.objectContaining({
        params: expect.objectContaining({ region: ['eu'], retention: ['zero'] })
      })
    )

    // Mount PublicModelDetailView on encoded slug URL /model-plaza/auto%2Fgemini-3.8-flash with 404 fallback
    apiGetMock.mockReset()
    apiGetMock
      .mockRejectedValueOnce({ status: 404, message: 'Not found' })
      .mockResolvedValueOnce({ data: { items: sampleCatalogItems } })
      .mockResolvedValueOnce({
        data: {
          model: sampleCatalogItems[0],
          price_version: 'pv_20261120',
          price_source: 'published'
        }
      })

    await router.push('/model-plaza/auto%2Fgemini-3.8-flash')
    const detailWrapper = mount(PublicModelDetailView, {
      global: {
        plugins: [router, i18n],
        stubs: { SiteICPFooter: true, LocaleSwitcher: true }
      }
    })
    wrappers.push(detailWrapper)
    await flushPromises()

    expect(detailWrapper.text()).toContain('Gemini 3.8 Flash')
    expect(detailWrapper.text()).toContain('$0.075')
    expect(detailWrapper.text()).toContain('$0.30')
  })
})
