import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createI18n } from 'vue-i18n'
import { reactive, ref } from 'vue'
import PublicModelsView from '../PublicModelsView.vue'
import PublicPricingView from '../PublicPricingView.vue'
import PublicStatusView from '../PublicStatusView.vue'
import PublicHomeView from '../PublicHomeView.vue'
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

const storeMock = reactive({
  siteName: 'Acme AI',
  siteLogo: '',
  docUrl: '',
  brandConfig: {
    siteName: 'Acme AI',
    siteLogo: '',
    subtitle: 'Gateway'
  },
  cachedPublicSettings: {
    registration_enabled: true,
    backend_mode_enabled: false,
    model_plaza_enabled: true,
    doc_url: ''
  },
  isAuthenticated: false,
  isAdmin: false,
  fetchPublicSettings: vi.fn(),
  openCommandPalette: vi.fn()
})

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

const wrappers: Array<ReturnType<typeof mount>> = []

beforeEach(() => {
  apiGetMock.mockReset()
  apiPostMock.mockReset()
  window.localStorage.clear()
  storeMock.siteName = 'Acme AI'
  storeMock.brandConfig.siteName = 'Acme AI'
  storeMock.cachedPublicSettings.model_plaza_enabled = true
})

afterEach(() => {
  wrappers.splice(0).forEach((w) => w.unmount())
})

describe('W07 Public Portal & Plaza Fixes (P2-1..5, P3-1..4, FE4-P1-02)', () => {
  it('decouples modality pills from input modality chips, keeps load-more on client-filtered empty state, and recovers from SNAPSHOT_EXPIRED (P2-1, P3-1, P3-2)', async () => {
    const page1 = [
      {
        id: 'm_img_only',
        provider: 'openai',
        model: 'gpt-image-1',
        display_name: 'GPT Image 1',
        modalities: ['image'],
        actions: ['image.generate'],
        availability_state: 'live',
        route_state: 'eligible',
        price_state: 'priced'
      },
      {
        id: 'm_img_pdf',
        provider: 'google',
        model: 'gemini-vision-pdf',
        display_name: 'Gemini Vision PDF',
        modalities: ['image', 'pdf'],
        actions: ['image.generate'],
        availability_state: 'live',
        route_state: 'eligible',
        price_state: 'priced'
      }
    ]
    apiGetMock.mockResolvedValue({
      data: { items: page1, next_cursor: 'cursor_2', as_of: '2026-11-22T00:00:00Z' }
    })

    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/model-plaza', component: PublicModelsView },
        { path: '/model-plaza/:id(.+)', component: { template: '<div />' } },
        { path: '/model-plaza/compare', component: { template: '<div />' } },
        { path: '/pricing', component: { template: '<div />' } },
        { path: '/:pathMatch(.*)*', component: { template: '<div />' } }
      ]
    })
    await router.push('/model-plaza')
    await router.isReady()

    const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: {} } })
    const wrapper = mount(PublicModelsView, {
      global: { plugins: [router, i18n], stubs: { SiteICPFooter: true, LocaleSwitcher: true } }
    })
    wrappers.push(wrapper)
    await flushPromises()

    // Duplicate data-testid fixed
    expect(wrapper.findAll('[data-testid="modality-filter-pills-row"]')).toHaveLength(1)
    expect(wrapper.findAll('[data-testid="availability-filter-pills-row"]')).toHaveLength(1)

    // Select modality pill = image and input modality chip = PDF: backend gets ONLY ['image'], client filters by PDF
    await wrapper.get('[data-testid="modality-pill-image"]').trigger('click')
    await flushPromises()
    const pdfButton = wrapper.findAll('button').find((btn) => btn.text().trim() === 'PDF')
    expect(pdfButton).toBeTruthy()
    await pdfButton!.trigger('click')
    await flushPromises()

    expect(apiGetMock).toHaveBeenLastCalledWith(
      '/api/v2/public/models',
      expect.objectContaining({
        params: expect.objectContaining({ modality: ['image'] })
      })
    )
    expect(wrapper.text()).toContain('Gemini Vision PDF')
    expect(wrapper.text()).not.toContain('GPT Image 1')
    expect(wrapper.text()).toContain('Showing 1 matching of 2 loaded')

    // Only favorites with empty page 1 still exposes Load next page button
    const favToggle = wrapper.findAll('button').find((btn) => btn.text().includes('Favorites'))
    await favToggle!.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="empty-state-load-more"]').exists()).toBe(true)

    // Snapshot expired on pagination clears cursor and retry triggers fresh reset load(true)
    apiGetMock.mockRejectedValueOnce({
      response: { status: 410, data: { reason: 'SNAPSHOT_EXPIRED', message: 'Expired' } }
    })
    await wrapper.get('[data-testid="empty-state-load-more"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Catalog snapshot expired')

    apiGetMock.mockResolvedValueOnce({
      data: { items: page1, next_cursor: null }
    })
    const reloadBtn = wrapper
      .findAll('button')
      .find((btn) => btn.text().includes('Reload latest catalog'))
    expect(reloadBtn).toBeTruthy()
    await reloadBtn!.trigger('click')
    await flushPromises()
    const lastCallParams = apiGetMock.mock.calls[apiGetMock.mock.calls.length - 1][1]?.params
    expect(lastCallParams?.cursor).toBeUndefined()
  })

  it('preserves pricing table rows on pagination failure and makes See detail badge clickable (P2-2, P3-3)', async () => {
    const pricingRow = {
      id: 'm_unpriced_1',
      provider: 'openai',
      model: 'custom-vision',
      display_name: 'Custom Vision',
      modalities: ['image'],
      availability_state: 'live',
      route_state: 'eligible',
      price_state: 'unpriced'
    }
    apiGetMock.mockResolvedValueOnce({
      data: { items: [pricingRow], next_cursor: 'cursor_2' }
    })

    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/pricing', component: PublicPricingView },
        { path: '/model-plaza/:id(.+)', component: { template: '<div />' } },
        { path: '/:pathMatch(.*)*', component: { template: '<div />' } }
      ]
    })
    await router.push('/pricing')
    await router.isReady()

    const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: {} } })
    const wrapper = mount(PublicPricingView, {
      global: { plugins: [router, i18n], stubs: { SiteICPFooter: true, LocaleSwitcher: true } }
    })
    wrappers.push(wrapper)
    await flushPromises()

    // Modality chips include embedding and realtime
    expect(wrapper.text()).toContain('Embeddings')
    expect(wrapper.text()).toContain('Realtime')

    // Clickable RouterLink wrapping See detail badge
    const inputDetailLink = wrapper.get('[data-testid="pricing-see-detail-input"]')
    expect(inputDetailLink.attributes('href')).toBe('/model-plaza/m_unpriced_1')

    // Pagination failure keeps loaded table visible
    apiGetMock.mockRejectedValueOnce(new Error('network blip'))
    const loadMoreBtn = wrapper.findAll('button').find((b) => b.text().includes('Load more rates'))
    await loadMoreBtn!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Custom Vision')
    expect(wrapper.text()).toContain('Could not load more rates')
  })

  it('prevents stale window overwrite and retries failed incident drawer without closing it (P2-3)', async () => {
    let resolveFirstWindow!: (val: unknown) => void
    const firstWindowPromise = new Promise((resolve) => {
      resolveFirstWindow = resolve
    })
    const incidentSummary = {
      id: 'inc_1',
      title: 'Upstream Latency Spike',
      severity: 'degraded',
      status: 'investigating',
      started_at: '2026-11-22T08:00:00Z',
      affected_components: ['openai']
    }

    apiGetMock.mockImplementation((url: string, config?: { params?: Record<string, unknown> }) => {
      if (url === '/api/v2/public/status' && config?.params?.window === '24h') {
        return firstWindowPromise
      }
      if (url === '/api/v2/public/status' && config?.params?.window === '7d') {
        return Promise.resolve({
          data: {
            overall: 'degraded',
            window: '7d',
            components: [{ id: 'openai', name: 'OpenAI 7d', state: 'degraded' }],
            incidents: [incidentSummary]
          }
        })
      }
      if (url === '/api/v2/public/status/incidents') {
        return Promise.resolve({ data: { items: [incidentSummary], next_cursor: null } })
      }
      if (url === '/api/v2/public/status/incidents/inc_1') {
        return Promise.reject(new Error('incident timeout'))
      }
      return Promise.resolve({ data: {} })
    })

    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/status', component: PublicStatusView },
        { path: '/:pathMatch(.*)*', component: { template: '<div />' } }
      ]
    })
    await router.push('/status')
    await router.isReady()

    const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: {} } })
    const wrapper = mount(PublicStatusView, {
      global: {
        plugins: [router, i18n],
        stubs: { SiteICPFooter: true, LocaleSwitcher: true, Teleport: true }
      }
    })
    wrappers.push(wrapper)

    // Switch to 7D before 24H resolves
    const btn7d = wrapper.findAll('button').find((b) => b.text().trim() === '7D')
    await btn7d!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('OpenAI 7d')

    // Resolve stale 24H request late — must NOT overwrite 7D
    resolveFirstWindow({
      data: {
        overall: 'operational',
        window: '24h',
        components: [{ id: 'stale', name: 'Stale 24h Component', state: 'operational' }],
        incidents: []
      }
    })
    await flushPromises()
    expect(wrapper.text()).toContain('OpenAI 7d')
    expect(wrapper.text()).not.toContain('Stale 24h Component')

    // Open incident drawer (fails once), then click Retry — must re-fetch instead of closing drawer
    const viewTimelineBtn = wrapper.findAll('button').find((b) => b.text().trim() === 'Timeline')
    expect(viewTimelineBtn).toBeTruthy()
    await viewTimelineBtn!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Failed to load incident updates')

    apiGetMock.mockResolvedValueOnce({
      data: {
        ...incidentSummary,
        updates: [{ id: 'u1', status: 'identified', message: 'Root cause found', created_at: '2026-11-22T08:15:00Z' }]
      }
    })
    const retryBtn = wrapper.findAll('button').find((b) => b.text().trim() === 'Retry')
    expect(retryBtn).toBeTruthy()
    await retryBtn!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Root cause found')
  })

  it('uses true provider header for slashless live model in PublicHomeView (P2-4)', async () => {
    apiGetMock.mockImplementation(async (url: string) => {
      if (url === '/api/v2/public/status') {
        return { data: { overall: 'operational', components: [] } }
      }
      return {
        data: {
          items: [
            {
              id: 'm_gpt4o',
              provider: 'openai',
              model: 'gpt-4o',
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
        { path: '/:pathMatch(.*)*', component: { template: '<div />' } }
      ]
    })
    await router.push('/')
    await router.isReady()
    const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: {} } })
    const wrapper = mount(PublicHomeView, {
      global: { plugins: [router, i18n], stubs: { SiteICPFooter: true, LocaleSwitcher: true } }
    })
    wrappers.push(wrapper)
    await flushPromises()

    expect(wrapper.text()).toContain('x-provider: openai')
    expect(wrapper.text()).not.toContain('x-provider: gpt-4o')
  })

  it('sanitizes documentation_url, avoids duplicate reference_price, fixes training no-substring match, and uses dynamic siteName in PublicModelDetailView (P2-5, P3-4, FE4-P1-02)', async () => {
    apiGetMock.mockResolvedValueOnce({
      data: {
        model: {
          id: 'm_ref_img',
          provider: 'openai',
          model: 'gpt-image-1',
          display_name: 'GPT Image 1',
          modalities: ['image'],
          actions: ['image.generate'],
          availability_state: 'catalog_only',
          route_state: 'ineligible',
          price_state: 'unpriced'
        },
        documentation_url: 'javascript:alert(1)',
        reference_price: {
          source: 'pi-ai',
          input_rate: { currency: 'USD', amount: '5', per: '1000000', unit: 'input_token' }
        },
        endpoints: [
          {
            provider: 'openai',
            training: 'cannot_opt_out'
          }
        ]
      }
    })

    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/model-plaza/:id(.+)', name: 'PublicModelDetail', component: PublicModelDetailView },
        { path: '/:pathMatch(.*)*', component: { template: '<div />' } }
      ]
    })
    await router.push('/model-plaza/m_ref_img')
    await router.isReady()
    const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: {} } })
    const wrapper = mount(PublicModelDetailView, {
      global: { plugins: [router, i18n], stubs: { SiteICPFooter: true, LocaleSwitcher: true } }
    })
    wrappers.push(wrapper)
    await flushPromises()

    // Malicious documentation_url is stripped
    expect(wrapper.find('a[href^="javascript:"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Provider Docs')

    // Reference price shown once + source shown instead of duplicating price
    expect(wrapper.text().match(/\$5\.00/g)?.length ?? 0).toBe(1)
    expect(wrapper.text()).toContain('Reference source: pi-ai')

    // cannot_opt_out is NOT mislabeled as "Not trained"
    expect(wrapper.text()).toContain('cannot_opt_out')
    expect(wrapper.text()).not.toContain('Not trained (cannot_opt_out)')

    // Image model snippet uses /v1/images/generations and notes catalog_only
    expect(wrapper.text()).toContain('/v1/images/generations')
    expect(wrapper.text()).toContain('catalog-only')

    // Dynamic brand siteName reflected in document.title
    expect(document.title).toBe('GPT Image 1 - Acme AI')
  })
})
