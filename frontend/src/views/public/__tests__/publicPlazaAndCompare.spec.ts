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
  it.each(['same page', 'later page'])('rejects a legacy name shared by providers on the %s without running a comparison', async (placement) => {
    const first = { ...sampleCatalogItems[0], provider: 'provider-a', model: 'same' }
    const second = { ...sampleCatalogItems[1], provider: 'provider-b', model: 'same' }
    const other = { ...sampleCatalogItems[0], id: 'm_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', model: 'other' }
    apiGetMock.mockImplementation(async (url: string, config?: { params?: { q?: string; cursor?: string } }) => {
      if (url !== '/api/v2/public/models') throw { status: 404, message: 'Not found' }
      if (config?.params?.q === 'same') {
        if (placement === 'same page') return { data: { items: [first, second], next_cursor: null } }
        return config.params.cursor
          ? { data: { items: [second], next_cursor: null } }
          : { data: { items: [first], next_cursor: 'identity-page-2' } }
      }
      return { data: { items: placement === 'same page' ? [first, second, other] : [first, other], next_cursor: placement === 'later page' ? 'browse-page-2' : null } }
    })
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/model-plaza/compare', component: PublicCompareView }] })
    await router.push(`/model-plaza/compare?ids=same,${other.id}`)
    const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: {} } })
    const wrapper = mount(PublicCompareView, { global: { plugins: [router, i18n], stubs: { SiteICPFooter: true, LocaleSwitcher: true } } })
    wrappers.push(wrapper)
    await flushPromises()

    expect(wrapper.text()).toContain('Model name is ambiguous; select a provider-qualified model.')
    expect(apiPostMock).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="scenario-recalculate"]').exists()).toBe(false)
    expect(apiGetMock).toHaveBeenCalledWith('/api/v2/public/models/same', expect.any(Object))
    if (placement === 'later page') {
      expect(apiGetMock).toHaveBeenCalledWith('/api/v2/public/models', expect.objectContaining({ params: expect.objectContaining({ q: 'same', cursor: 'identity-page-2' }) }))
    }
  })

  it('resolves a provider-qualified legacy selection independently of list order', async () => {
    const first = { ...sampleCatalogItems[0], provider: 'provider-a', model: 'same' }
    const second = { ...sampleCatalogItems[1], provider: 'provider-b', model: 'same' }
    apiGetMock.mockImplementation(async (url: string) => {
      if (url === '/api/v2/public/models') return { data: { items: [first, second], next_cursor: null } }
      if (url === `/api/v2/public/models/${MODEL_B_ID}`) return { data: { model: second } }
      throw { status: 404, message: 'Not found' }
    })
    apiPostMock.mockResolvedValue({ data: { rows: [] } })
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/model-plaza/compare', component: PublicCompareView }] })
    await router.push(`/model-plaza/compare?ids=provider-b%2Fsame,${MODEL_A_ID}`)
    const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: {} } })
    const wrapper = mount(PublicCompareView, { global: { plugins: [router, i18n], stubs: { SiteICPFooter: true, LocaleSwitcher: true } } })
    wrappers.push(wrapper)
    await flushPromises()

    expect(apiPostMock).toHaveBeenCalledWith('/api/v2/public/models/compare', expect.objectContaining({ model_ids: [MODEL_B_ID, MODEL_A_ID] }))
    expect(apiGetMock).toHaveBeenCalledWith('/api/v2/public/models/provider-b%2Fsame', expect.any(Object))
  })

  it('recalculates interactive scenario cost and renders metrics.window + metrics.source in PublicCompareView', async () => {
    apiGetMock.mockImplementation(async (url: string) => {
      if (url === '/api/v2/public/models') return { data: { items: sampleCatalogItems, next_cursor: null } }
      if (url === `/api/v2/public/models/${MODEL_A_ID}`) return { data: { model: sampleCatalogItems[0] } }
      throw { status: 404, message: 'Not found' }
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
    expect(document.querySelector('link[rel="canonical"]')?.getAttribute('href')).toContain(`/model-plaza/${MODEL_A_ID}`)
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
        publication_state: 'published',
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
        publication_state: 'draft',
        reference_price: {
          source: 'pi-ai',
          input_rate: { currency: 'USD', amount: '5', per: '1000000', unit: 'input_token' },
          output_rate: { currency: 'USD', amount: '40', per: '1000000', unit: 'output_token' }
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
        availability_state: 'catalog_only', publication_state: 'draft'
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
      if (params.publication_state) {
        filtered = filtered.filter(item => item.publication_state === params.publication_state)
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
        params: expect.objectContaining({ sort: 'availability', limit: 100 })
      })
    )
    expect(wrapper.text()).toContain('Route health verified')
    expect(wrapper.text()).toContain('Reference catalog · Integration unconfirmed')
    expect(wrapper.text()).toContain('Ref $5.00')
    expect(wrapper.text()).toContain('No sale price')
    expect(wrapper.find('[data-testid="modality-badge-image"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="modality-badge-video"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="modality-badge-embedding"]').exists()).toBe(true)
    expect(modalityLabel('3d')).toBe('3D')
    // When next_cursor is present, pill numeric counts must NOT show misleading unloaded counts
    expect(wrapper.get('[data-testid="modality-pill-audio"]').text()).toBe('Audio & Speech')

    await wrapper.get('[data-testid="publication-pill-published"]').trigger('click')
    await flushPromises()
    expect(apiGetMock).toHaveBeenLastCalledWith(
      '/api/v2/public/models',
      expect.objectContaining({
        params: expect.objectContaining({ publication_state: 'published', sort: 'availability' })
      })
    )
    expect(wrapper.text()).toContain('GPT-4o')
    expect(wrapper.text()).not.toContain('GPT Image 1')

    await wrapper.get('[data-testid="publication-pill-all"]').trigger('click')
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
        reference_price: multimodalCatalog[1].reference_price
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
      'Reference catalog · Integration unconfirmed'
    )
    expect(detailWrapper.get('[data-testid="detail-modality-badge-image"]').text()).toContain('Image')
    expect(detailWrapper.text()).toContain('Ref $5.00')
  })

  it('renders availability badges, reference_price hints, and availability toggle in PublicPricingView (FE-05)', async () => {
    const pricingItems = [
      {
        ...sampleCatalogItems[0],
        availability_state: 'live', publication_state: 'published'
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
        publication_state: 'draft',
        reference_price: {
          source: 'pi-ai',
          input_rate: { currency: 'USD', amount: '5', per: '1000000', unit: 'input_token' },
          output_rate: { currency: 'USD', amount: '40', per: '1000000', unit: 'output_token' }
        }
      }
    ]
    apiGetMock.mockImplementation(async (_url: string, config?: { params?: Record<string, unknown> }) => {
      const publication = config?.params?.publication_state
      const items = publication
        ? pricingItems.filter((item) => item.publication_state === publication)
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

    expect(wrapper.get('[data-testid="pricing-availability-badge-live"]').text()).toContain('Route health verified')
    expect(wrapper.get('[data-testid="pricing-availability-badge-catalog_only"]').text()).toContain(
      'Reference catalog · Integration unconfirmed'
    )
    expect(wrapper.get('[data-testid="pricing-reference-rate-input"]').text()).toContain('Ref $5.00')
    expect(wrapper.get('[data-testid="pricing-reference-rate-output"]').text()).toContain('Ref $40.00')

    await wrapper.get('[data-testid="pricing-publication-pill-published"]').trigger('click')
    await flushPromises()
    expect(apiGetMock).toHaveBeenLastCalledWith(
      '/api/v2/public/models',
      expect.objectContaining({
        params: expect.objectContaining({ publication_state: 'published' })
      })
    )
    expect(wrapper.text()).toContain('Gemini 3.8 Flash')
    expect(wrapper.text()).not.toContain('GPT Image 1')
  })

  it('prefers live chat-capable model for hero snippet in PublicHomeView (FE-04)', async () => {
    apiGetMock.mockImplementation(async (url: string, config?: { params?: Record<string, unknown> }) => {
      if (url === '/api/v2/public/status') {
        return { data: { overall: 'operational', components: [] } }
      }
      if (config?.params?.publication_state === 'published') {
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
        publication_state: 'published',
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
        publication_state: 'published',
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
        publication_state: 'draft',
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
        publication_state: 'published',
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
        params: expect.objectContaining({ publication_state: 'published', modality: ['text'], limit: 10, sort: 'name' })
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
        publication_state: 'published',
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
