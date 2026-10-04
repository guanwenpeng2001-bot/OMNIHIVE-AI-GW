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
  it.each(['en', 'zh'] as const)('separates public client setup from supplier contracts in %s', locale => {
    const { wrapper } = mountGuide(locale)
    const supplierSection = wrapper.get('section#suppliers')
    expect(supplierSection.findAll('tbody tr')).toHaveLength(5)
    expect(supplierSection.text()).not.toMatch(/72 (?:个|distinct)/)
    expect(supplierSection.text()).toContain('https://omnihive2.com/v1')
    for (const endpoint of ['/v1/models', '/v1/models/catalog', '/v1beta/models/']) {
      expect(supplierSection.text()).toContain(endpoint)
    }
    for (const supplierName of ['Copilot', 'Nous', 'RelayCode', 'RelaxyCode', 'ETok.ai']) {
      expect(supplierSection.text()).toContain(supplierName)
    }
    for (const contract of ['billing_plan', 'AICodeMirror', 'CCSub', 'AtlasCloud', 'kToken', 'project_budget', 'consumed_usage', 'Asia/Shanghai', 'fine_grained_pat', 'invocation_token', 'official_proxy',
      'Copilot-Integration-Id: omnihive', '/models', '/v1/chat/completions',
      'Tencent Intl', 'Novita', 'PPIO', 'SiliconFlow', 'SoleAPI', '10000', 'Credits']) {
      expect(supplierSection.text()).toContain(contract)
    }
    const supplierCopy = supplierSection.text()
    for (const contract of ['Ark Coding', 'AK/SK', 'BytePlus', 'Ark Agent Plan',
      '5h / weekly / monthly', 'missing_credentials', 'invalid_credentials', 'permission_denied',
      'unsupported_profile', 'no_verified_query', 'not_subscribed', 'no_seat',
      'invalid_response', 'upstream_error', 'url_rejected', 'discovered_models', 'candidate_models',
      'upstream_model_sync_', 'discovery_unconfirmed', 'endpoint_not_found', 'method_not_allowed']) {
      expect(supplierCopy).toContain(contract)
    }
    for (const boundary of locale === 'en'
      ? ['explicit clearing removes them', 'Failures never become zero or erase history',
        'Selecting a candidate manually is not discovery success']
      : ['明确清除才删除', '失败不当零、不清历史', '手工选择不等于发现成功']) {
      expect(supplierCopy).toContain(boundary)
    }
    expect(supplierCopy).not.toMatch(/pending integration|待集成验证|70 distinct|70 个独立/)
    for (const boundary of locale === 'en'
      ? ['offline contract tests, not live vendor certification', 'no automatic OAuth or refresh',
        'same-mode edits preserve secret references', 'Use single-account editing', 'Unsupported or failed is not zero',
        'personal cannot inherit enterprise Responses', 'optional frontdoor key']
      : ['离线合同测试，不是供应商在线认证', '无自动 OAuth 或 refresh',
        '同模式编辑保留密钥引用', '通过单账号编辑', '不支持或失败不是零余额',
        '个人套餐不能从共享主机继承企业 Responses', '入口 key 可选']) {
      expect(supplierCopy).toContain(boundary)
    }
    expect(supplierSection.text()).toContain('793e67d9b3210eb527e7d36f2c866a56fae9dec7')
    expect(supplierSection.text()).not.toContain('https://api.openai.com')
    const conversionSection = wrapper.get('section#cc-switch').text()
    expect(conversionSection).toContain('wire_api = responses')
    for (const endpoint of ['/v1/messages', '/v1/chat/completions', '/v1/responses']) {
      expect(conversionSection).toContain(endpoint)
    }
    expect(wrapper.find('a[href="https://openrouter.ai/docs/api_reference/overview"]').exists()).toBe(true)
  })

  it('translates the resource, conflict and upload error guidance in Chinese', () => {
    const errors = zh.helpGuide.faq.tables.errors.rows
    for (const row of [errors.missing, errors.conflict, errors.large]) {
      expect(row.code).toBe(errors.model.code)
      expect(row.code).not.toBe('Read the response body')
    }
  })

  it.each([
    { locale: 'en', guide: en.helpGuide, contractNote: 'separate platform contracts' },
    { locale: 'zh', guide: zh.helpGuide, contractNote: '独立平台契约' }
  ])('keeps catalog and audio guidance consistent in $locale', ({ guide, contractNote }) => {
    const catalog = guide.models.tables.endpoints.rows.openai.notes
    expect(catalog).toContain('GET /backend-api/codex/models')
    expect(JSON.stringify(guide.models)).not.toMatch(/2027-03-21|until|截至/)
    const media = guide.clients.notes.mediaEndpoints
    for (const endpoint of ['/v1/audio/speech', '/v1/audio/transcriptions', '/v1/audio/translations']) {
      expect(media).toContain(endpoint)
      expect(guide.audio.endpoint).toContain(endpoint)
    }
    expect(media).toContain(contractNote)
    expect(media).toContain('/v1/tts')
    expect(media).toContain('/v1/stt')
  })

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
