import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter, type NavigationGuard, type RouterOptions } from 'vue-router'
import { createI18n, type LocaleMessages, type VueMessageType } from 'vue-i18n'
import { baseCompile } from '@intlify/message-compiler'
import { ref } from 'vue'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import HelpGuide from '@/views/HelpGuide.vue'
import PublicCompatibilityView from '../PublicCompatibilityView.vue'

const { authStore, appStore, adminSettingsStore, routerHarness } = vi.hoisted(() => ({
  authStore: {
    isSimpleMode: false,
    isAdmin: false,
    isAuthenticated: false,
    hasPendingAuthSession: false,
    user: null as { id: number; role: string } | null,
    checkAuth: vi.fn()
  },
  appStore: {
    siteName: 'OmniHive',
    siteLogo: '',
    docUrl: '',
    publicSettingsLoaded: true,
    cachedPublicSettings: {
      custom_menu_items: [],
      registration_enabled: true,
      backend_mode_enabled: false,
      doc_url: ''
    },
    sidebarCollapsed: false,
    sidebarScrollTop: 0,
    backendModeEnabled: false,
    mobileOpen: false,
    apiBaseUrl: 'https://gateway.omnihive.example/v1',
    fetchPublicSettings: vi.fn(),
    openCommandPalette: vi.fn()
  },
  adminSettingsStore: {
    customMenuItems: [],
    fetch: vi.fn()
  },
  routerHarness: {
    options: null as RouterOptions | null,
    guard: null as NavigationGuard | null
  }
}))

vi.mock('vue-router', async (original) => {
  const actual = await original<typeof import('vue-router')>()
  return {
    ...actual,
    createRouter: (options: RouterOptions) => {
      if (!routerHarness.options && options.routes.length > 10) {
        routerHarness.options = options
        const real = actual.createRouter({
          ...options,
          history: actual.createMemoryHistory()
        })
        const origBeforeEach = real.beforeEach.bind(real)
        real.beforeEach = (guard: NavigationGuard) => {
          routerHarness.guard = guard
          return origBeforeEach(guard)
        }
        return real
      }
      return actual.createRouter(options)
    }
  }
})

vi.mock('@/stores/app', () => ({ useAppStore: () => appStore }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => authStore }))
vi.mock('@/stores/adminSettings', () => ({ useAdminSettingsStore: () => adminSettingsStore }))
vi.mock('@/stores', () => ({
  useAppStore: () => appStore,
  useAuthStore: () => authStore,
  useAdminSettingsStore: () => adminSettingsStore,
  useOnboardingStore: () => ({ setReplayCallback: vi.fn() })
}))
vi.mock('@/composables/useBatchImageAccess', () => ({
  useBatchImageAccess: () => ({ canUseBatchImage: ref(false), refreshBatchImageAccess: vi.fn() })
}))
vi.mock('@/composables/useThemeDirection', () => ({
  useThemeDirection: () => ({ isDark: ref(true), toggleDarkMode: vi.fn() })
}))
vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard: vi.fn().mockResolvedValue(true) })
}))

function prepareMessages(raw: Record<string, unknown>): LocaleMessages<VueMessageType> {
  return Object.fromEntries(
    Object.entries(raw).map(([key, value]) => [
      key,
      typeof value === 'string'
        ? new Function(`return ${baseCompile(value).code}`)()
        : prepareMessages(value as Record<string, unknown>)
    ])
  )
}

const compiledMessages = {
  en: prepareMessages({
    helpGuide: en.helpGuide,
    nav: en.nav,
    common: en.common,
    keyUsage: en.keyUsage,
    home: en.home,
    publicPortal: en.publicPortal
  }),
  zh: prepareMessages({
    helpGuide: zh.helpGuide,
    nav: zh.nav,
    common: zh.common,
    keyUsage: zh.keyUsage,
    home: zh.home,
    publicPortal: zh.publicPortal
  })
}

const wrappers: Array<ReturnType<typeof mount>> = []

beforeEach(() => {
  window.scrollTo = vi.fn()
  authStore.isAuthenticated = false
  authStore.isAdmin = false
  authStore.isSimpleMode = false
  authStore.user = null
  appStore.docUrl = ''
  appStore.cachedPublicSettings.doc_url = ''
})

afterEach(() => {
  wrappers.splice(0).forEach((w) => w.unmount())
})

describe('Public documentation, compatibility page & /models redirect (FE-10-15, FE-RET-03)', () => {
  it('configures /docs, /help, and /compatibility as public routes in PublicShell and redirects /models -> /model-plaza', async () => {
    const appRouterModule = await import('@/router')
    const appRouter = appRouterModule.default

    const docsResolved = appRouter.resolve('/docs')
    expect(docsResolved.meta).toMatchObject({
      requiresAuth: false,
      shellTier: 'public',
      title: 'Documentation',
      titleZh: '接入文档'
    })

    const helpResolved = appRouter.resolve('/help')
    expect(helpResolved.meta).toMatchObject({
      requiresAuth: false,
      shellTier: 'public',
      title: 'Documentation',
      titleZh: '接入文档'
    })

    const compatResolved = appRouter.resolve('/compatibility')
    expect(compatResolved.meta).toMatchObject({
      requiresAuth: false,
      shellTier: 'public',
      title: 'Updates & Compatibility',
      titleZh: '更新与兼容性'
    })

    const modelsRouteRecord = routerHarness.options?.routes.find((r) => r.path === '/models')
    expect(modelsRouteRecord?.redirect).toBe('/model-plaza')

    await appRouter.push('/models')
    await appRouter.isReady()
    expect(appRouter.currentRoute.value.path).toBe('/model-plaza')

    // Anonymous guard allows /docs, /help, /compatibility without /login redirect
    for (const targetPath of ['/docs', '/help', '/compatibility']) {
      const resolved = appRouter.resolve(targetPath)
      const next = vi.fn()
      await routerHarness.guard!(resolved, resolved, next)
      expect(next).toHaveBeenCalledWith()
    }
  })

  it('renders HelpGuide inside PublicShell without private console sidebar when unauthenticated or authenticated', async () => {
    const testRouter = createRouter({
      history: createMemoryHistory(),
      routes: [
        {
          path: '/docs',
          component: HelpGuide,
          meta: { requiresAuth: false, shellTier: 'public' }
        },
        {
          path: '/compatibility',
          component: PublicCompatibilityView,
          meta: { requiresAuth: false, shellTier: 'public' }
        },
        { path: '/:pathMatch(.*)*', component: { template: '<div />' } }
      ]
    })
    await testRouter.push('/docs')
    await testRouter.isReady()

    const i18n = createI18n({ legacy: false, locale: 'zh', messages: compiledMessages })
    const wrapper = mount(HelpGuide, {
      global: {
        plugins: [testRouter, i18n],
        stubs: {
          SiteICPFooter: true,
          LocaleSwitcher: true
        }
      }
    })
    wrappers.push(wrapper)
    await flushPromises()

    // Renders PublicShell (header + 1560px main + footer) and NEVER private console shell
    expect(wrapper.find('[data-testid="public-header"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="public-shell-main"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="public-shell-footer"]').exists()).toBe(true)
    expect(wrapper.find('[data-shell-tier="user"]').exists()).toBe(false)
    expect(wrapper.find('.app-shell').exists()).toBe(false)

    // Includes compatibility link & 4-protocol quickstart tabs
    const compatLink = wrapper.get('[data-testid="docs-compatibility-link"]')
    expect(compatLink.attributes('href')).toBe('/compatibility')
    expect(wrapper.get('[data-testid="docs-compatibility-banner"]').text()).toContain('.ohplugin v2')

    const quickstartPanel = wrapper.get('[data-testid="docs-protocol-quickstart"]')
    expect(quickstartPanel.text()).toContain('/v1/chat/completions')

    const tabs = quickstartPanel.findAll('button[role="tab"]')
    expect(tabs).toHaveLength(4)
    await tabs[1].trigger('click')
    expect(quickstartPanel.text()).toContain('/v1/responses')
    await tabs[2].trigger('click')
    expect(quickstartPanel.text()).toContain('/v1/messages')
    await tabs[3].trigger('click')
    expect(quickstartPanel.text()).toContain('/v1beta/models/')

    // Even when authenticated, /docs stays in PublicShell
    authStore.isAuthenticated = true
    authStore.user = { id: 1, role: 'user' }
    await flushPromises()
    expect(wrapper.find('[data-testid="public-shell-main"]').exists()).toBe(true)
    expect(wrapper.find('.app-shell').exists()).toBe(false)
  })

  it('renders PublicCompatibilityView inside PublicShell and links /docs + /compatibility in header and footer', async () => {
    const testRouter = createRouter({
      history: createMemoryHistory(),
      routes: [
        {
          path: '/compatibility',
          component: PublicCompatibilityView,
          meta: { requiresAuth: false, shellTier: 'public' }
        },
        { path: '/:pathMatch(.*)*', component: { template: '<div />' } }
      ]
    })
    await testRouter.push('/compatibility')
    await testRouter.isReady()

    const i18n = createI18n({ legacy: false, locale: 'en', messages: compiledMessages })
    const wrapper = mount(PublicCompatibilityView, {
      global: {
        plugins: [testRouter, i18n],
        stubs: {
          SiteICPFooter: true,
          LocaleSwitcher: true
        }
      }
    })
    wrappers.push(wrapper)
    await flushPromises()

    expect(wrapper.find('[data-testid="public-compatibility-view"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="public-shell-main"]').exists()).toBe(true)
    expect(wrapper.find('.app-shell').exists()).toBe(false)

    const text = wrapper.text()
    expect(text).toContain('POST /v1/chat/completions')
    expect(text).toContain('POST /v1/responses')
    expect(text).toContain('POST /v1/messages')
    expect(text).toContain('POST /v1beta/models/{model}:generateContent')
    expect(text).toContain('.ohplugin')
    expect(text).toContain('.s2plugin')
    expect(text).toContain('SHA256SUMS')

    // Header & Footer contain /docs and /compatibility links
    const desktopLinks = wrapper
      .get('[data-testid="public-desktop-nav"]')
      .findAll('a')
      .map((a) => a.attributes('href'))
    expect(desktopLinks).toContain('/docs')
    expect(desktopLinks).toContain('/compatibility')

    const footerLinks = wrapper
      .get('[data-testid="public-shell-footer"]')
      .findAll('a')
      .map((a) => a.attributes('href'))
    expect(footerLinks).toContain('/docs')
    expect(footerLinks).toContain('/compatibility')

    // Switch to Chinese cleanly
    i18n.global.locale.value = 'zh'
    await flushPromises()
    expect(wrapper.text()).toContain('协议兼容矩阵')
  })

  it('exposes human support entry from public footer and renders contact, scope, and SLA in HelpGuide #support [E1-3]', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/docs', component: HelpGuide },
        { path: '/compatibility', component: PublicCompatibilityView }
      ]
    })
    await router.push('/compatibility')
    const i18n = createI18n({ legacy: false, locale: 'en', messages: compiledMessages })

    const compatWrapper = mount(PublicCompatibilityView, {
      global: {
        plugins: [router, i18n],
        stubs: { SiteICPFooter: true, LocaleSwitcher: true }
      }
    })
    wrappers.push(compatWrapper)
    await flushPromises()

    const footerLinks = compatWrapper
      .get('[data-testid="public-shell-footer"]')
      .findAll('a')
      .map((a) => a.attributes('href'))
    expect(footerLinks).toContain('/docs#support')

    await router.push('/docs#support')
    const docsWrapper = mount(HelpGuide, {
      global: {
        plugins: [router, i18n],
        stubs: { SiteICPFooter: true, LocaleSwitcher: true }
      }
    })
    wrappers.push(docsWrapper)
    await flushPromises()

    const supportSection = docsWrapper.find('#support')
    expect(supportSection.exists()).toBe(true)
    expect(supportSection.text()).toContain('Not configured')
    expect(supportSection.text()).toContain('1–2 business days')
  })

  it('corrects image endpoint, probe constraint, plugin host disclosure, and renders versioned changelog [E1-5]', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/compatibility', component: PublicCompatibilityView }]
    })
    await router.push('/compatibility')
    const i18n = createI18n({ legacy: false, locale: 'en', messages: compiledMessages })
    const wrapper = mount(PublicCompatibilityView, {
      global: {
        plugins: [router, i18n],
        stubs: { SiteICPFooter: true, LocaleSwitcher: true }
      }
    })
    wrappers.push(wrapper)
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('/v1/images/generations')
    expect(text).not.toContain('POST /v1/embeddings · /v1/rerank · /v1/images · /v1/audio/*')
    expect(text).toContain('omnihive-plugin-host')
    expect(text).not.toContain('sub2api')
    expect(text).toContain('EVIDENCE_UNAVAILABLE')
    expect(wrapper.find('[data-testid="compatibility-changelog"]').exists()).toBe(true)
  })

  it('includes SDK first-call examples for OpenAI, Anthropic, Gemini, and rate-limit retry in HelpGuide SDK tab [E1-5]', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/docs', component: HelpGuide }]
    })
    await router.push('/docs')
    const i18n = createI18n({ legacy: false, locale: 'en', messages: compiledMessages })
    const wrapper = mount(HelpGuide, {
      global: {
        plugins: [router, i18n],
        stubs: { SiteICPFooter: true, LocaleSwitcher: true }
      }
    })
    wrappers.push(wrapper)
    await flushPromises()

    const sdkTab = wrapper.findAll('button').find((b) => b.text().includes('SDK'))
    expect(sdkTab).toBeTruthy()
    await sdkTab!.trigger('click')
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('OpenAI(')
    expect(text).toContain('Anthropic(')
    expect(text).toContain('GoogleGenAI')
    expect(text).toContain('Retry-After')
  })

  it('marks console links in HelpGuide with login-required hint and avoids /models links in public navigation [E1-10]', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/docs', component: HelpGuide }]
    })
    await router.push('/docs')
    const i18n = createI18n({ legacy: false, locale: 'en', messages: compiledMessages })
    const wrapper = mount(HelpGuide, {
      global: {
        plugins: [router, i18n],
        stubs: { SiteICPFooter: true, LocaleSwitcher: true }
      }
    })
    wrappers.push(wrapper)
    await flushPromises()

    const keysLink = wrapper.findAll('a').find((a) => a.attributes('href') === '/keys')
    expect(keysLink).toBeTruthy()
    expect(keysLink!.text()).toContain('Login required')

    const keyUsageLink = wrapper.findAll('a').find((a) => a.attributes('href') === '/key-usage')
    expect(keyUsageLink).toBeTruthy()
    expect(keyUsageLink!.text()).not.toContain('Login required')

    const publicHrefs = wrapper.findAll('a').map((a) => a.attributes('href'))
    expect(publicHrefs).not.toContain('/models')
    expect(publicHrefs).toContain('/model-plaza')
  })
})
