import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, RouterLinkStub } from '@vue/test-utils'
import { createI18n, type LocaleMessages, type VueMessageType } from 'vue-i18n'
import HelpGuide from '../HelpGuide.vue'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import AppSidebar from '@/components/layout/AppSidebar.vue'
import { ref } from 'vue'
import type { NavigationGuard, RouterOptions } from 'vue-router'
import { helpGroups, helpSectionIds, helpScrollOffset } from '@/components/intro/helpSections'
import DocSidebar from '@/components/intro/DocSidebar.vue'
import { baseCompile } from '@intlify/message-compiler'

const { authStore, enabledFlags, copyToClipboard } = vi.hoisted(() => ({
  authStore: { isSimpleMode: false, isAdmin: false, isAuthenticated: true, checkAuth: vi.fn() },
  enabledFlags: { payment: true, subscription: true },
  copyToClipboard: vi.fn().mockResolvedValue(true)
}))
const appStore = vi.hoisted(() => ({
  siteName: 'Sub2API', siteLogo: '', publicSettingsLoaded: true, cachedPublicSettings: { custom_menu_items: [] },
  sidebarCollapsed: false, sidebarScrollTop: 0, backendModeEnabled: false, mobileOpen: false,
  apiBaseUrl: 'https://gateway.example/v1'
}))
const adminSettingsStore = vi.hoisted(() => ({ customMenuItems: [], fetch: vi.fn() }))
const routerHarness = vi.hoisted(() => ({ options: null as RouterOptions | null, guard: null as NavigationGuard | null }))
vi.mock('vue-router', async original => ({
  ...await original<typeof import('vue-router')>(),
  useRoute: () => ({ path: '/help' }),
  useRouter: () => ({ push: vi.fn(), resolve: (path: string) => ({ path, matched: [], params: {}, query: {}, hash: '' }) }),
  createRouter: (options: RouterOptions) => {
    routerHarness.options = options
    return { beforeEach: (guard: NavigationGuard) => { routerHarness.guard = guard }, afterEach: vi.fn(), onError: vi.fn() }
  }
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => appStore }))
vi.mock('@/stores/adminSettings', () => ({ useAdminSettingsStore: () => adminSettingsStore }))
vi.mock('@/router/title', () => ({ resolveRouteDocumentTitle: () => 'Guide' }))
vi.mock('@/stores', () => ({
  useAppStore: () => appStore, useAuthStore: () => authStore,
  useAdminSettingsStore: () => adminSettingsStore, useOnboardingStore: () => ({})
}))
vi.mock('@/composables/useBatchImageAccess', () => ({ useBatchImageAccess: () => ({ canUseBatchImage: ref(false), refreshBatchImageAccess: vi.fn() }) }))

vi.mock('@/stores/auth', () => ({ useAuthStore: () => authStore }))
vi.mock('@/utils/featureFlags', () => ({
  FeatureFlags: { payment: 'payment', subscription: 'subscription' },
  isFeatureFlagEnabled: (flag: keyof typeof enabledFlags) => enabledFlags[flag],
  makeSidebarFlag: () => () => false,
  resolveFeatureFlag: () => false
}))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard }) }))

const mountedGuides: ReturnType<typeof mount>[] = []
// Match runtime compilation, including literal braces in documented API paths.
function prepareMessages(messages: Record<string, unknown>): LocaleMessages<VueMessageType> {
  return Object.fromEntries(Object.entries(messages).map(([key, value]) => [
    key,
    typeof value === 'string' ? new Function(`return ${baseCompile(value).code}`)() : prepareMessages(value as Record<string, unknown>)
  ]))
}
const messages = {
  en: prepareMessages({ helpGuide: en.helpGuide, nav: en.nav, common: en.common, keyUsage: en.keyUsage }),
  zh: prepareMessages({ helpGuide: zh.helpGuide, nav: zh.nav, common: zh.common, keyUsage: zh.keyUsage })
}
function mountGuide(locale: 'en' | 'zh' = 'zh') {
  const i18n = createI18n({ legacy: false, locale, messages })
  const wrapper = mount(HelpGuide, {
    global: {
      plugins: [i18n],
      stubs: { AppLayout: { template: '<main><slot /></main>' }, RouterLink: RouterLinkStub }
    }
  })
  mountedGuides.push(wrapper)
  return { wrapper, i18n }
}

beforeEach(() => {
  authStore.isSimpleMode = false
  authStore.isAdmin = false
  authStore.isAuthenticated = true
  enabledFlags.payment = true
  enabledFlags.subscription = true
  copyToClipboard.mockClear()
})
afterEach(() => {
  mountedGuides.splice(0).forEach(wrapper => wrapper.unmount())
})

describe('configuration manual', () => {
  it('uses one registry for grouped contents, translated sections and every hash destination', async () => {
    const { wrapper } = mountGuide('en')
    await import('@/router')
    await flushPromises()
    expect(wrapper.findAll('#guide-contents details')).toHaveLength(helpGroups.length)
    expect(wrapper.findAll('#guide-contents a').map(link => link.attributes('href'))).toEqual(helpSectionIds.map(id => `#${id}`))
    const scroll = routerHarness.options!.scrollBehavior!
    const route = { path: '/help', fullPath: '/help', name: 'HelpGuide', meta: {}, params: {}, query: {}, hash: '', matched: [], redirectedFrom: undefined }
    for (const id of helpSectionIds) {
      expect(wrapper.find(`section#${id}`).exists()).toBe(true)
      expect(await scroll({ ...route, hash: `#${id}` }, route, null)).toEqual({ el: `#${id}`, top: helpScrollOffset })
    }
    expect(await scroll({ ...route, hash: '#unknown' }, route, null)).toEqual({ top: 0 })
    expect(await scroll(route, route, { left: 10, top: 300 })).toEqual({ left: 10, top: 300 })
  })

  it('exposes an accessible mobile contents toggle and expands the active group', async () => {
    const sidebar = mount(DocSidebar, {
      props: { active: 'quick', sections: helpSectionIds.map(id => ({ id, title: id })) },
      global: { plugins: [createI18n({ legacy: false, locale: 'en', messages })] }
    })
    mountedGuides.push(sidebar)
    const toggle = sidebar.get('button[aria-controls="guide-contents"]')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    await toggle.trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('true')
    expect(sidebar.get('a[href="#quick"]').attributes('aria-current')).toBe('location')
    await sidebar.get('a[href="#chat"]').trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    await sidebar.setProps({ active: 'audio' })
    expect(sidebar.get('a[href="#audio"]').element.closest('details')?.open).toBe(true)
    expect(sidebar.get('a[href="#audio"]').attributes('aria-current')).toBe('location')
  })

  it('renders prefixed Bash and PowerShell quickstarts and formal protocol/media examples', async () => {
    const configured = appStore.apiBaseUrl
    appStore.apiBaseUrl = 'https://gateway.example/proxy/v1beta'
    try {
      const { wrapper } = mountGuide('en')
      await flushPromises()
      const quick = wrapper.get('#quick').text()
      expect(quick).toContain('https://gateway.example/proxy/v1/chat/completions')
      const powershell = wrapper.findAll('#quick pre').find(block => block.text().includes('Invoke-RestMethod'))!
      expect(powershell.text()).toContain("$env:SUB2API_BASE_URL = 'https://gateway.example/proxy/v1'")
      expect(powershell.text()).not.toMatch(/\\\r?\n/)
      expect(wrapper.get('#gemini').text()).toContain('https://gateway.example/proxy/v1beta/models/GOOGLE_MODEL_ID_FROM_LIST:generateContent')
      expect(wrapper.get('#gemini').text()).toContain('/v1beta/models/{model}:generateContent')
      expect(wrapper.get('#resources').text()).toContain('/v1/files/{id}/content')
      expect(wrapper.get('#clients').text()).toContain('@ai-sdk/openai-compatible')
      for (const id of ['chat', 'messages', 'responses', 'gemini', 'retrieval', 'streaming', 'tools', 'reasoning', 'cache', 'images', 'async-images', 'media-jobs', 'audio']) {
        expect(wrapper.findAll(`#${id} table`)).toHaveLength(2)
        expect(wrapper.findAll(`#${id} pre`).length).toBeGreaterThan(0)
      }
      expect(wrapper.text()).not.toContain('Sub2API')
      expect(wrapper.get('#cc-switch').text()).toContain('optional')
      expect(wrapper.get('#clients').text()).toContain('Windsurf')
      for (const client of ['Claude Code', 'Codex', 'OpenCode', 'DeepSeek Harness', 'Pi']) expect(wrapper.get('#clients').text()).toContain(client)
    } finally {
      appStore.apiBaseUrl = configured
    }
  })
  it('renders complete translated structures and updates them when the locale changes', async () => {
    const { wrapper, i18n } = mountGuide()
    await flushPromises()
    expect(wrapper.get('h1').text()).toBe('配置与使用手册')
    expect(wrapper.findAll('#quick ol > li')).toHaveLength(5)
    expect(wrapper.findAll('#keys tbody tr')).toHaveLength(6)
    expect(wrapper.findAll('#models table')).toHaveLength(2)
    expect(wrapper.findAll('#clients table')).toHaveLength(2)
    expect(wrapper.findAll('#usage table')).toHaveLength(2)
    expect(wrapper.findAll('#billing tbody tr')).toHaveLength(3)
    expect(wrapper.get('#faq').text()).toContain('insufficient_quota')
    expect(wrapper.get('#faq').text()).toContain('API_KEY_EXPIRED')
    expect(wrapper.text()).not.toContain('helpGuide.')
    for (const region of wrapper.findAll('[role="region"]')) {
      expect(region.attributes('tabindex')).toBe('0')
      expect(region.classes()).toContain('overflow-x-auto')
      expect(wrapper.find(`#${region.attributes('aria-labelledby')}`).exists()).toBe(true)
    }
    i18n.global.locale.value = 'en'
    await flushPromises()
    expect(wrapper.get('h1').text()).toBe('Configuration and usage manual')
    expect(wrapper.get('#keys').text()).toContain('Personal laptop')
    expect(wrapper.get('#billing').text()).toContain('Redemption code')
    expect(wrapper.text()).not.toContain('helpGuide.')
  })

  it('keeps anchor destinations and copies the actual client example', async () => {
    const { wrapper } = mountGuide()
    await flushPromises()
    for (const anchor of wrapper.findAll('#guide-contents a')) {
      expect(wrapper.find(anchor.attributes('href')!).exists()).toBe(true)
    }
    await wrapper.get('#clients button[aria-label="复制 Text · Cursor"]').trigger('click')
    await flushPromises()
    expect(copyToClipboard).toHaveBeenCalledWith(expect.stringContaining('Model: MODEL_ID_FROM_LIST'), '已复制到剪贴板')
    expect(copyToClipboard).toHaveBeenCalledWith(expect.stringContaining('https://gateway.example/v1'), '已复制到剪贴板')
    expect(wrapper.get('#clients [role="status"]').text()).toBe('已复制到剪贴板')
    // 新增客户端使用第一方文档里的字段名，且示例统一指向本站配置的地址。
    const clients = wrapper.get('#clients').text()
    expect(clients).toContain('OpenAI Compatible')
    expect(clients).toContain('apiBase')
    expect(clients).toContain('Providers & API keys')
  })

  it('preserves feature-gated links and simple-mode restrictions', async () => {
    enabledFlags.payment = false
    enabledFlags.subscription = false
    const { wrapper } = mountGuide()
    await flushPromises()
    const destinations = wrapper.findAllComponents(RouterLinkStub).map(link => link.props('to'))
    expect(destinations).toContain('/keys')
    expect(destinations).toContain('/usage')
    expect(destinations).toContain('/redeem')
    expect(destinations).not.toContain('/purchase')
    expect(destinations).not.toContain('/orders')
    expect(destinations).not.toContain('/subscriptions')

    expect(destinations).toContain('/key-usage')

    authStore.isSimpleMode = true
    const simpleGuide = mountGuide().wrapper
    await flushPromises()
    expect(simpleGuide.findAllComponents(RouterLinkStub).map(link => link.props('to'))).toEqual(['/keys', '/keys', '/key-usage', '/key-usage'])
  })

  it.each([
    { admin: false, simple: false }, { admin: true, simple: false },
    { admin: false, simple: true }, { admin: true, simple: true }
  ])('keeps the guide in the personal navigation for admin=$admin simple=$simple', async ({ admin, simple }) => {
    authStore.isAdmin = admin
    authStore.isSimpleMode = simple
    const i18n = createI18n({ legacy: false, locale: 'zh', messages })
    const wrapper = mount(AppSidebar, { global: {
      plugins: [i18n], stubs: { RouterLink: RouterLinkStub, VersionBadge: true, Icon: true }
    } })
    mountedGuides.push(wrapper)
    const guide = () => wrapper.findAllComponents(RouterLinkStub).find(link => link.props('to') === '/help')!
    expect(guide().text()).toBe('使用指南')
    if (admin) expect(guide().element.closest('.sidebar-section')?.textContent).toContain(zh.nav.myAccount)
    if (simple) {
      const paths = wrapper.findAllComponents(RouterLinkStub).map(link => link.props('to'))
      expect(paths).not.toContain('/purchase')
      expect(paths).not.toContain('/subscriptions')
    }
    i18n.global.locale.value = 'en'
    await flushPromises()
    expect(guide().text()).toBe('Guide')
  })

  it('registers a protected non-admin route and applies the real authentication guard', async () => {
    await import('@/router')
    const help = routerHarness.options!.routes.find(route => route.path === '/help')!
    expect(help.meta).toMatchObject({ requiresAuth: true, requiresAdmin: false, titleKey: 'helpGuide.title' })
    const to = { path: '/help', fullPath: '/help', name: 'HelpGuide', meta: help.meta, params: {}, query: {}, hash: '', matched: [], redirectedFrom: undefined }
    const next = vi.fn()
    authStore.isAuthenticated = false
    await routerHarness.guard!(to, to, next)
    expect(next).toHaveBeenCalledWith({ path: '/login', query: { redirect: '/help' } })
    authStore.isAuthenticated = true
    authStore.isSimpleMode = true
    next.mockClear()
    await routerHarness.guard!(to, to, next)
    expect(next).toHaveBeenCalledWith()
  })

  it('prompts explicitly and renders no example when no public API address is configured', async () => {
    const configured = appStore.apiBaseUrl
    appStore.apiBaseUrl = ''
    try {
      const { wrapper } = mountGuide()
      await flushPromises()
      expect(wrapper.text()).toContain('api_base_url')
      expect(wrapper.findAll('pre')).toHaveLength(0)
      expect(wrapper.text()).not.toContain('your-gateway.example')
    } finally {
      appStore.apiBaseUrl = configured
    }
  })

  it('explains actual user key windows and Antigravity limitations in both languages', () => {
    for (const locale of [zh, en]) {
      const windows = locale.helpGuide.usage.tables.fields.rows.windows.meaning
      for (const field of ['rate_limit_5h', 'rate_limit_1d', 'rate_limit_7d']) expect(windows).toContain(field)
      expect(windows).not.toContain('30d')
      expect(locale.helpGuide.clients.notes.antigravity).toContain('Antigravity')
    }
  })
})
