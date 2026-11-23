import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createI18n } from 'vue-i18n'
import { ref } from 'vue'
import PublicCompareView from '../PublicCompareView.vue'
import PublicModelsView from '../PublicModelsView.vue'
import PublicModelDetailView from '../PublicModelDetailView.vue'
import PublicPricingView from '../PublicPricingView.vue'
import PublicHomeView from '../PublicHomeView.vue'
import { matchesModalityFilter, modalityLabel } from '@/features/public/format'

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

  it('does not override backend unpriced quote with local estimate in PublicCompareView', async () => {
    apiGetMock.mockResolvedValueOnce({
      data: { items: sampleCatalogItems, next_cursor: null }
    })
    apiPostMock.mockResolvedValueOnce({
      data: {
        catalog_version: 'cat_v1',
        as_of: '2026-11-20T12:00:00Z',
        rows: [
          {
            model_id: MODEL_A_ID,
            capabilities: { tools: 'supported', stream: 'supported' },
            quote: {
              state: 'unpriced',
              estimated_charge: null,
              components: []
            }
          },
          {
            model_id: MODEL_B_ID,
            capabilities: { tools: 'supported', stream: 'supported' },
            quote: {
              state: 'estimated',
              estimated_charge: { currency: 'USD', amount: '0.036' },
              components: [{ kind: 'total', quantity: '1', raw_amount: '0.036' }]
            }
          }
        ]
      }
    })

    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/model-plaza/compare', component: PublicCompareView }]
    })
    await router.push(`/model-plaza/compare?ids=${MODEL_A_ID},${MODEL_B_ID}`)
    const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: {} } })
    const wrapper = mount(PublicCompareView, {
      global: {
        plugins: [router, i18n],
        stubs: { SiteICPFooter: true, LocaleSwitcher: true }
      }
    })
    wrappers.push(wrapper)
    await flushPromises()

    const quoteRow = wrapper.findAll('tr').find((tr) => tr.text().includes('Scenario quote'))
    expect(quoteRow).toBeTruthy()
    expect(quoteRow!.text()).toContain('Unpriced')
    expect(quoteRow!.text()).toContain('$0.036')
    expect(quoteRow!.text()).not.toContain('$0.0135')
  })

  it('does not render $0.00 breakdown in PublicPricingView when output rate is missing', async () => {
    apiGetMock.mockResolvedValueOnce({
      data: {
        items: [
          {
            ...sampleCatalogItems[0],
            output_rate: null
          }
        ],
        next_cursor: null
      }
    })

    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/pricing', component: PublicPricingView }]
    })
    await router.push('/pricing')
    const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: {} } })
    const wrapper = mount(PublicPricingView, {
      global: {
        plugins: [router, i18n],
        stubs: { SiteICPFooter: true, LocaleSwitcher: true }
      }
    })
    wrappers.push(wrapper)
    await flushPromises()

    const estimatorSection = wrapper.get('[data-testid="pricing-estimator"]')
    expect(estimatorSection.text()).not.toContain('$0.00')
    expect(estimatorSection.text()).toContain('—')
  })

  it('filters multimodal models via Modality pills and distinguishes live vs catalog_only models', async () => {
    const multimodalCatalog = [
      {
        id: 'm_11111111111111111111111111111111',
        provider: 'openai',
        model: 'gpt-4o',
        display_name: 'GPT-4o',
        context_tokens: '128000',
        modalities: ['text'],
        actions: ['text.chat'],
        protocols: ['openai_chat'],
        route_state: 'eligible',
        price_state: 'priced',
        availability_state: 'live',
        input_rate: { currency: 'USD', amount: '2.5', per: '1000000', unit: 'input_token' },
        output_rate: { currency: 'USD', amount: '10', per: '1000000', unit: 'output_token' }
      },
      {
        id: 'm_22222222222222222222222222222222',
        provider: 'openai',
        model: 'gpt-image-1',
        display_name: 'GPT Image 1',
        context_tokens: null,
        modalities: ['image'],
        actions: ['image.generate'],
        protocols: ['openai_images'],
        route_state: 'ineligible',
        price_state: 'unpriced',
        availability_state: 'catalog_only',
        billing: {
          state: 'unpriced',
          reference_price: {
            source: 'pi-ai',
            input_rate: { currency: 'USD', amount: '5', per: '1000000', unit: 'input_token' },
            output_rate: { currency: 'USD', amount: '40', per: '1000000', unit: 'output_token' }
          }
        }
      },
      {
        id: 'm_33333333333333333333333333333333',
        provider: 'google',
        model: 'veo-2.0',
        display_name: 'Veo 2.0',
        modalities: ['video'],
        actions: ['video.generate'],
        route_state: 'ineligible',
        price_state: 'unpriced',
        availability_state: 'catalog_only'
      },
      {
        id: 'm_44444444444444444444444444444444',
        provider: 'openai',
        model: 'text-embedding-3-large',
        display_name: 'Text Embedding 3 Large',
        modalities: ['text'],
        actions: ['text.embeddings'],
        route_state: 'eligible',
        price_state: 'priced',
        input_rate: { currency: 'USD', amount: '0.13', per: '1000000', unit: 'input_token' }
      }
    ]

    apiGetMock.mockImplementation(async (_url: string, config?: { params?: Record<string, unknown> }) => {
      const params = config?.params ?? {}
      let filtered = [...multimodalCatalog]
      if (params.availability_state) {
        filtered = filtered.filter((item) => {
          const state = item.availability_state ?? (item.route_state === 'eligible' ? 'live' : 'catalog_only')
          return state === params.availability_state
        })
      }
      if (Array.isArray(params.modality) && params.modality.length > 0) {
        filtered = filtered.filter((item) =>
          (params.modality as string[]).some((mod) => matchesModalityFilter(item, mod as any))
        )
      }
      return { data: { items: filtered, next_cursor: 'cursor_page_2' } }
    })

    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/model-plaza', component: PublicModelsView },
        { path: '/model-plaza/:id', component: PublicModelDetailView }
      ]
    })
    await router.push('/model-plaza')
    await router.isReady()

    const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: {}, zh: {} } })
    const wrapper = mount(PublicModelsView, {
      global: {
        plugins: [router, i18n],
        stubs: { SiteICPFooter: true, LocaleSwitcher: true }
      }
    })
    wrappers.push(wrapper)
    await flushPromises()

    expect(apiGetMock).toHaveBeenCalledWith(
      '/api/v2/public/models',
      expect.objectContaining({
        params: expect.objectContaining({ availability_state: 'live', sort: 'availability', limit: 100 })
      })
    )
    // Default filter is availability_state=live (no catalog_only flood on first load)
    expect(wrapper.text()).toContain('Available')
    expect(wrapper.text()).toContain('GPT-4o')
    expect(wrapper.text()).not.toContain('GPT Image 1')
    expect(wrapper.text()).not.toContain('On demand')
    // When next_cursor is present, pill numeric counts must NOT show misleading unloaded counts
    expect(wrapper.get('[data-testid="modality-pill-audio"]').text()).toBe('Audio & Speech')

    // Switching to All reveals Catalog Only + secondary pi reference price without raw pi-ai
    await wrapper.get('[data-testid="availability-pill-all"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Catalog Only')
    expect(wrapper.text()).toContain('Ref $5.00')
    expect(wrapper.text()).toContain('Community Catalog (pi)')
    expect(wrapper.text()).toContain('Non-settlement')
    expect(wrapper.text()).not.toContain('pi-ai')
    expect(wrapper.text()).not.toContain('On demand')
    expect(wrapper.find('[data-testid="modality-badge-image"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="modality-badge-video"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="modality-badge-embedding"]').exists()).toBe(true)
    expect(modalityLabel('3d')).toBe('3D')

    await wrapper.get('[data-testid="availability-pill-live"]').trigger('click')
    await flushPromises()
    expect(apiGetMock).toHaveBeenLastCalledWith(
      '/api/v2/public/models',
      expect.objectContaining({
        params: expect.objectContaining({ availability_state: 'live', sort: 'availability' })
      })
    )
    expect(wrapper.text()).toContain('GPT-4o')
    expect(wrapper.text()).not.toContain('GPT Image 1')

    await wrapper.get('[data-testid="availability-pill-all"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="modality-pill-image"]').trigger('click')
    await flushPromises()
    expect(apiGetMock).toHaveBeenLastCalledWith(
      '/api/v2/public/models',
      expect.objectContaining({
        params: expect.objectContaining({ modality: ['image'] })
      })
    )
    expect(wrapper.text()).toContain('GPT Image 1')
    expect(wrapper.text()).not.toContain('Veo 2.0')
    expect(wrapper.text()).not.toContain('GPT-4o')

    await wrapper.get('[data-testid="modality-pill-video"]').trigger('click')
    await flushPromises()
    expect(apiGetMock).toHaveBeenLastCalledWith(
      '/api/v2/public/models',
      expect.objectContaining({
        params: expect.objectContaining({ modality: ['video'] })
      })
    )
    expect(wrapper.text()).toContain('Veo 2.0')
    expect(wrapper.text()).not.toContain('GPT Image 1')

    await wrapper.get('[data-testid="modality-pill-embedding"]').trigger('click')
    await flushPromises()
    expect(apiGetMock).toHaveBeenLastCalledWith(
      '/api/v2/public/models',
      expect.objectContaining({
        params: expect.objectContaining({ modality: ['embedding'] })
      })
    )
    expect(wrapper.text()).toContain('Text Embedding 3 Large')
    expect(wrapper.text()).not.toContain('Veo 2.0')

    await wrapper.get('[data-testid="modality-pill-all"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('GPT-4o')
    expect(wrapper.text()).toContain('Veo 2.0')

    // Also verify PublicModelDetailView renders catalog_only badge & modality badges
    apiGetMock.mockReset()
    apiGetMock.mockResolvedValueOnce({
      data: {
        model: multimodalCatalog[1],
        reference_price: multimodalCatalog[1].billing?.reference_price
      }
    })
    await router.push('/model-plaza/m_22222222222222222222222222222222')
    const detailWrapper = mount(PublicModelDetailView, {
      global: {
        plugins: [router, i18n],
        stubs: { SiteICPFooter: true, LocaleSwitcher: true }
      }
    })
    wrappers.push(detailWrapper)
    await flushPromises()

    expect(detailWrapper.get('[data-testid="detail-availability-badge-catalog_only"]').text()).toContain(
      'Catalog Only'
    )
    expect(detailWrapper.get('[data-testid="detail-modality-badge-image"]').text()).toContain('Image')
    expect(detailWrapper.text()).toContain('Ref $5.00')
    expect(detailWrapper.text()).toContain('Community Catalog (pi)')
    expect(detailWrapper.text()).toContain('Non-settlement')
    expect(detailWrapper.text()).not.toContain('pi-ai')
    expect(detailWrapper.text()).not.toContain('On demand')
    // Strict 6-section IA markers & collapsed quickstart for non-live models
    expect(detailWrapper.find('[data-testid="detail-section-pricing"]').exists()).toBe(true)
    expect(detailWrapper.find('[data-testid="detail-section-capabilities"]').exists()).toBe(true)
    expect(detailWrapper.find('[data-testid="detail-section-quickstart"]').exists()).toBe(true)
    expect(detailWrapper.find('[data-testid="detail-section-lifecycle"]').exists()).toBe(true)
    expect(detailWrapper.find('[data-testid="detail-section-telemetry"]').exists()).toBe(true)
    expect(detailWrapper.get('[data-testid="detail-quickstart-toggle"]').attributes('aria-expanded')).toBe('false')
  })

  it('renders availability badges, reference_price hints, and availability toggle in PublicPricingView (FE-05)', async () => {
    const pricingItems = [
      {
        ...sampleCatalogItems[0],
        availability_state: 'live'
      },
      {
        id: 'm_catalog_ref_1',
        provider: 'openai',
        model: 'gpt-image-1',
        display_name: 'GPT Image 1',
        context_tokens: '32000',
        modalities: ['image'],
        route_state: 'ineligible',
        price_state: 'unpriced',
        availability_state: 'catalog_only',
        billing: {
          state: 'unpriced',
          reference_price: {
            source: 'pi-ai',
            input_rate: { currency: 'USD', amount: '5', per: '1000000', unit: 'input_token' },
            output_rate: { currency: 'USD', amount: '40', per: '1000000', unit: 'output_token' }
          }
        }
      }
    ]
    apiGetMock.mockImplementation(async (_url: string, config?: { params?: Record<string, unknown> }) => {
      const avail = config?.params?.availability_state
      const items = avail
        ? pricingItems.filter((item) => item.availability_state === avail)
        : pricingItems
      return { data: { items, next_cursor: null } }
    })

    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/pricing', component: PublicPricingView },
        { path: '/model-plaza/:id', component: PublicModelDetailView }
      ]
    })
    await router.push('/pricing')
    const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: {} } })
    const wrapper = mount(PublicPricingView, {
      global: {
        plugins: [router, i18n],
        stubs: { SiteICPFooter: true, LocaleSwitcher: true }
      }
    })
    wrappers.push(wrapper)
    await flushPromises()

    expect(wrapper.get('[data-testid="pricing-availability-badge-live"]').text()).toContain('Available')
    expect(wrapper.text()).not.toContain('GPT Image 1')

    await wrapper.get('[data-testid="pricing-availability-pill-all"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="pricing-availability-badge-catalog_only"]').text()).toContain(
      'Catalog Only'
    )
    expect(wrapper.get('[data-testid="pricing-reference-rate-input"]').text()).toContain('Ref $5.00')
    expect(wrapper.get('[data-testid="pricing-reference-rate-output"]').text()).toContain('Ref $40.00')
    expect(wrapper.get('[data-testid="pricing-reference-rate-input"]').text()).toContain('Community Catalog (pi)')
    expect(wrapper.get('[data-testid="pricing-reference-rate-input"]').text()).toContain('Non-settlement')
    expect(wrapper.text()).not.toContain('pi-ai')

    await wrapper.get('[data-testid="pricing-availability-pill-live"]').trigger('click')
    await flushPromises()
    expect(apiGetMock).toHaveBeenLastCalledWith(
      '/api/v2/public/models',
      expect.objectContaining({
        params: expect.objectContaining({ availability_state: 'live' })
      })
    )
    expect(wrapper.text()).toContain('Gemini 3.8 Flash')
    expect(wrapper.text()).not.toContain('GPT Image 1')
  })

  it('omits public requires_scope badge, provides 3-action empty state on live filter, and separates varies vs error in Compare/Plaza', async () => {
    apiGetMock.mockImplementation(async (_url: string, config?: { params?: Record<string, unknown> }) => {
      if (config?.params?.availability_state === 'live') {
        return { data: { items: [], next_cursor: null } }
      }
      return {
        data: {
          items: [
            {
              id: 'm_scoped_pub',
              provider: 'openai',
              model: 'gpt-4.1',
              display_name: 'GPT-4.1',
              availability: { state: 'requires_scope', publication: 'published', access: 'requires_scope' },
              billing: {
                state: 'conflict'
              }
            },
            {
              id: 'm_err_pub',
              provider: 'anthropic',
              model: 'claude-4-opus',
              display_name: 'Claude 4 Opus',
              availability: { state: 'live', publication: 'published', access: 'allowed' },
              billing: { state: 'error', reason: 'price_read_failed' }
            }
          ],
          next_cursor: null
        }
      }
    })

    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/model-plaza', component: PublicModelsView },
        { path: '/model-plaza/:id', component: PublicModelDetailView },
        { path: '/model-plaza/compare', component: PublicCompareView },
        { path: '/docs', component: { template: '<div />' } }
      ]
    })
    await router.push('/model-plaza')
    const i18n = createI18n({ legacy: false, locale: 'zh', messages: { zh: {} } })
    const wrapper = mount(PublicModelsView, {
      global: {
        plugins: [router, i18n],
        stubs: { SiteICPFooter: true, LocaleSwitcher: true }
      }
    })
    wrappers.push(wrapper)
    await flushPromises()

    // 3-action empty state when live returns 0 models
    expect(wrapper.text()).toContain('当前筛选下没有可直接调用的模型')
    expect(wrapper.find('[data-testid="empty-state-view-catalog-only"]').exists()).toBe(true)
    expect(wrapper.find('a[href="/docs#support"]').exists()).toBe(true)

    await wrapper.get('[data-testid="availability-pill-all"]').trigger('click')
    await flushPromises()

    // Public plaza MUST NOT show "需项目授权" badge
    expect(wrapper.text()).not.toContain('需项目授权')
    expect(wrapper.find('[data-testid="availability-badge-requires_scope"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('按分组定价')
    expect(wrapper.text()).toContain('价格加载失败')

    // PublicCompareView also distinguishes error vs varies vs unpriced without collapsing error to unpriced
    apiGetMock.mockReset()
    apiGetMock.mockResolvedValueOnce({
      data: {
        items: [
          {
            id: MODEL_A_ID,
            provider: 'openai',
            model: 'gpt-4.1',
            availability: { state: 'live', publication: 'published', access: 'allowed' },
            billing: { state: 'conflict' }
          },
          {
            id: MODEL_B_ID,
            provider: 'anthropic',
            model: 'claude-4-opus',
            availability: { state: 'live', publication: 'published', access: 'allowed' },
            billing: { state: 'error', reason: 'price_read_failed' }
          }
        ],
        next_cursor: null
      }
    })
    apiPostMock.mockResolvedValueOnce({
      data: {
        catalog_version: 'cat_v2',
        as_of: '2026-11-23T00:00:00Z',
        rows: [
          {
            model_id: MODEL_A_ID,
            capabilities: { tools: 'supported', stream: 'supported' },
            quote: { state: 'unpriced', billing_state: 'conflict', estimated_charge: null, components: [] }
          },
          {
            model_id: MODEL_B_ID,
            capabilities: { tools: 'supported', stream: 'supported' },
            quote: { state: 'unpriced', billing_state: 'error', estimated_charge: null, components: [] }
          }
        ]
      }
    })
    await router.push(`/model-plaza/compare?ids=${MODEL_A_ID},${MODEL_B_ID}`)
    const compareWrapper = mount(PublicCompareView, {
      global: {
        plugins: [router, i18n],
        stubs: { SiteICPFooter: true, LocaleSwitcher: true }
      }
    })
    wrappers.push(compareWrapper)
    await flushPromises()

    const quoteRow = compareWrapper.get('tr[data-metric-key="scenario-quote"]')
    expect(quoteRow.text()).toContain('按分组定价')
    expect(quoteRow.text()).toContain('价格加载失败')
    expect(quoteRow.find('[data-testid="compare-quote-retry"]').exists()).toBe(true)
  })

  it('renders BE-3 anonymous published requires_scope offers as normal 可调用 on default live filter without any 需项目授权 badge', async () => {
    apiGetMock.mockResolvedValueOnce({
      data: {
        items: [
          {
            id: 'm_be3_pub_1',
            provider: 'anthropic',
            model: 'claude-sonnet-4-5',
            display_name: 'Claude Sonnet 4.5',
            availability_state: 'live',
            availability: {
              state: 'requires_scope',
              publication: 'published',
              access: 'requires_scope'
            },
            billing: {
              state: 'priced',
              sale_price: {
                input_rate: { currency: 'USD', amount: '3', per: '1000000', unit: 'input_token' },
                output_rate: { currency: 'USD', amount: '15', per: '1000000', unit: 'output_token' }
              }
            }
          },
          {
            id: 'm_be3_cat_1',
            provider: 'meta',
            model: 'llama-4-scout',
            display_name: 'Llama 4 Scout',
            availability_state: 'catalog_only',
            availability: {
              state: 'requires_scope',
              publication: 'unpublished',
              access: 'requires_scope',
              reason: 'platform_catalog'
            }
          }
        ],
        next_cursor: null
      }
    })

    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/model-plaza', component: PublicModelsView },
        { path: '/model-plaza/:id', component: PublicModelDetailView }
      ]
    })
    await router.push('/model-plaza')
    const i18n = createI18n({ legacy: false, locale: 'zh', messages: { zh: {} } })
    const wrapper = mount(PublicModelsView, {
      global: {
        plugins: [router, i18n],
        stubs: { SiteICPFooter: true, LocaleSwitcher: true }
      }
    })
    wrappers.push(wrapper)
    await flushPromises()

    expect(wrapper.text()).toContain('Claude Sonnet 4.5')
    expect(wrapper.text()).not.toContain('Llama 4 Scout')
    expect(wrapper.get('[data-testid="availability-badge-live"]').text()).toContain('可调用')
    expect(wrapper.find('[data-testid="availability-badge-requires_scope"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('需项目授权')
  })

  it('prefers live chat-capable model for hero snippet in PublicHomeView (FE-04)', async () => {
    apiGetMock.mockImplementation(async (url: string, config?: { params?: Record<string, unknown> }) => {
      if (url === '/api/v2/public/status') {
        return { data: { overall: 'operational', components: [] } }
      }
      if (config?.params?.availability_state === 'live') {
        return {
          data: {
            items: [
              {
                id: 'm_live_img',
                provider: 'openai',
                model: 'openai/dall-e-3',
                modalities: ['image'],
                actions: ['image.generate'],
                availability_state: 'live',
                route_state: 'eligible',
                price_state: 'priced'
              },
              {
                id: 'm_live_chat',
                provider: 'anthropic',
                model: 'anthropic/claude-sonnet-4-5',
                modalities: ['text'],
                actions: ['text.chat'],
                availability_state: 'live',
                route_state: 'eligible',
                price_state: 'priced'
              }
            ],
            next_cursor: null
          }
        }
      }
      return {
        data: {
          items: [
            {
              id: 'm_catalog_first',
              provider: 'amazon',
              model: 'amazon/nova-2-lite-v1',
              modalities: ['text'],
              actions: ['text.chat'],
              availability_state: 'catalog_only',
              route_state: 'ineligible',
              price_state: 'unpriced'
            },
            {
              id: 'm_live_chat',
              provider: 'anthropic',
              model: 'anthropic/claude-sonnet-4-5',
              modalities: ['text'],
              actions: ['text.chat'],
              availability_state: 'live',
              route_state: 'eligible',
              price_state: 'priced'
            }
          ],
          next_cursor: null
        }
      }
    })

    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', component: PublicHomeView },
        { path: '/model-plaza', component: PublicModelsView },
        { path: '/status', component: PublicModelsView },
        { path: '/login', component: PublicModelsView },
        { path: '/dashboard', component: PublicModelsView }
      ]
    })
    await router.push('/')
    await router.isReady()
    const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: {} } })
    const wrapper = mount(PublicHomeView, {
      global: {
        plugins: [router, i18n],
        stubs: { SiteICPFooter: true, LocaleSwitcher: true }
      }
    })
    wrappers.push(wrapper)
    await flushPromises()

    expect(apiGetMock).toHaveBeenCalledWith(
      '/api/v2/public/models',
      expect.objectContaining({
        params: expect.objectContaining({ availability_state: 'live', limit: 10, sort: 'name' })
      })
    )
    expect(wrapper.text()).toContain('anthropic/claude-sonnet-4-5')
    expect(wrapper.text()).not.toContain('amazon/nova-2-lite-v1')
    expect(wrapper.text()).not.toContain('openai/dall-e-3')
  })

  it('formats 3d modality as 3D and hides partial pill counts when next_cursor is non-null (FE-03, FE-10)', async () => {
    expect(modalityLabel('3d', false)).toBe('3D')
    expect(modalityLabel('3d', true)).toBe('3D')

    apiGetMock.mockResolvedValueOnce({
      data: {
        items: [
          {
            id: 'm_page1',
            provider: 'openai',
            model: 'gpt-4o',
            display_name: 'GPT-4o',
            modalities: ['text'],
            actions: ['text.chat'],
            availability_state: 'live',
            route_state: 'eligible',
            price_state: 'priced'
          }
        ],
        next_cursor: 'cursor_page_2'
      }
    })

    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/model-plaza', component: PublicModelsView },
        { path: '/model-plaza/:id', component: PublicModelDetailView },
        { path: '/model-plaza/compare', component: PublicCompareView }
      ]
    })
    await router.push('/model-plaza')
    const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: {}, zh: {} } })
    const wrapper = mount(PublicModelsView, {
      global: {
        plugins: [router, i18n],
        stubs: { SiteICPFooter: true, LocaleSwitcher: true }
      }
    })
    wrappers.push(wrapper)
    await flushPromises()

    expect(wrapper.get('[data-testid="modality-pill-video"]').text()).toBe('Video Generation')
    expect(wrapper.get('[data-testid="modality-pill-video"]').text()).not.toContain('0')
  })
})
