import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { defineComponent, nextTick, reactive, ref } from 'vue'
import PublicHeader from '../PublicHeader.vue'

const routeState = reactive({
  path: '/home',
  fullPath: '/home'
})

const localeRef = ref('zh-CN')

const storeState = reactive({
  siteName: 'OmniHive',
  siteLogo: '',
  docUrl: '',
  cachedPublicSettings: {
    model_plaza_enabled: true,
    registration_enabled: true,
    backend_mode_enabled: false,
    doc_url: ''
  } as {
    model_plaza_enabled?: boolean
    registration_enabled?: boolean
    backend_mode_enabled?: boolean
    doc_url?: string
  } | null,
  isAuthenticated: false,
  isAdmin: false
})

const fetchPublicSettingsSpy = vi.fn()
const openCommandPaletteSpy = vi.fn()

vi.mock('vue-router', () => ({
  useRoute: () => routeState
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    locale: localeRef,
    t: (key: string) => key
  })
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    get siteName() {
      return storeState.siteName
    },
    get siteLogo() {
      return storeState.siteLogo
    },
    get docUrl() {
      return storeState.docUrl
    },
    get cachedPublicSettings() {
      return storeState.cachedPublicSettings
    },
    fetchPublicSettings: fetchPublicSettingsSpy,
    openCommandPalette: openCommandPaletteSpy
  }),
  useAuthStore: () => ({
    get isAuthenticated() {
      return storeState.isAuthenticated
    },
    get isAdmin() {
      return storeState.isAdmin
    }
  })
}))

const RouterLinkStub = defineComponent({
  name: 'RouterLink',
  props: {
    to: {
      type: [String, Object],
      required: true
    }
  },
  emits: ['click'],
  template:
    '<a :href="typeof to === \'string\' ? to : to.path" @click.prevent="$emit(\'click\', $event)"><slot /></a>'
})

enableAutoUnmount(afterEach)

function mountHeader() {
  return mount(PublicHeader, {
    attachTo: document.body,
    global: {
      stubs: {
        'router-link': RouterLinkStub,
        RouterLink: RouterLinkStub,
        LocaleSwitcher: { template: '<div data-testid="locale-switcher-stub" />' }
      }
    }
  })
}

describe('PublicHeader responsive navigation', () => {
  beforeEach(() => {
    routeState.path = '/home'
    routeState.fullPath = '/home'
    localeRef.value = 'zh-CN'
    storeState.docUrl = ''
    storeState.isAuthenticated = false
    storeState.isAdmin = false
    storeState.cachedPublicSettings = {
      model_plaza_enabled: true,
      registration_enabled: true,
      backend_mode_enabled: false,
      doc_url: ''
    }
    fetchPublicSettingsSpy.mockClear()
    openCommandPaletteSpy.mockClear()
  })

  it('exposes an accessible compact menu button and renders all 5 navigation links when expanded', async () => {
    const wrapper = mountHeader()
    const toggle = wrapper.get('[data-testid="public-nav-toggle"]')

    expect(toggle.attributes('aria-expanded')).toBe('false')
    expect(toggle.attributes('aria-controls')).toBe('public-mobile-nav')
    expect(wrapper.find('#public-mobile-nav').exists()).toBe(false)

    await toggle.trigger('click')

    expect(toggle.attributes('aria-expanded')).toBe('true')
    const mobileNav = wrapper.get('#public-mobile-nav [data-testid="public-mobile-nav-list"]')
    const links = mobileNav.findAll('a')

    expect(links.map((node) => ({ href: node.attributes('href'), text: node.text() }))).toEqual([
      { href: '/model-plaza', text: '模型广场' },
      { href: '/key-usage', text: '额度查询' },
      { href: '/pricing', text: '定价' },
      { href: '/status', text: '服务状态' },
      { href: '/help', text: '文档' }
    ])
  })

  it('always keeps /model-plaza visible and preserves sanitized docUrl, registrationEnabled, and English labels', async () => {
    localeRef.value = 'en'
    storeState.docUrl = 'https://docs.example.com/guide'
    storeState.cachedPublicSettings = {
      model_plaza_enabled: false,
      registration_enabled: false,
      backend_mode_enabled: false
    }

    const wrapper = mountHeader()
    await wrapper.get('[data-testid="public-nav-toggle"]').trigger('click')

    const mobileLinks = wrapper
      .get('[data-testid="public-mobile-nav-list"]')
      .findAll('a')

    expect(mobileLinks.map((node) => ({ href: node.attributes('href'), text: node.text() }))).toEqual([
      { href: '/model-plaza', text: 'MODELS' },
      { href: '/key-usage', text: 'QUOTA' },
      { href: '/pricing', text: 'PRICING' },
      { href: '/status', text: 'STATUS' },
      { href: 'https://docs.example.com/guide', text: 'DOCS ↗' }
    ])
    expect(mobileLinks[4].attributes('target')).toBe('_blank')
    expect(mobileLinks[4].attributes('rel')).toBe('noopener noreferrer')
    expect(wrapper.find('a[href="/register"]').exists()).toBe(false)

    storeState.docUrl = 'javascript:alert(1)'
    await nextTick()

    const sanitizedDocsLink = wrapper
      .get('[data-testid="public-mobile-nav-list"]')
      .findAll('a')[4]
    expect(sanitizedDocsLink.attributes('href')).toBe('/help')
  })

  it('closes the compact menu on Escape and restores focus to the toggle button', async () => {
    const wrapper = mountHeader()
    const toggle = wrapper.get('[data-testid="public-nav-toggle"]')

    await toggle.trigger('click')
    expect(wrapper.find('#public-mobile-nav').exists()).toBe(true)

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await nextTick()

    expect(wrapper.find('#public-mobile-nav').exists()).toBe(false)
    expect(toggle.attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(toggle.element)
  })

  it('closes the compact menu when clicking outside or when the route changes', async () => {
    const wrapper = mountHeader()
    const toggle = wrapper.get('[data-testid="public-nav-toggle"]')

    await toggle.trigger('click')
    expect(wrapper.find('#public-mobile-nav').exists()).toBe(true)

    document.body.click()
    await nextTick()
    expect(wrapper.find('#public-mobile-nav').exists()).toBe(false)
    expect(toggle.attributes('aria-expanded')).toBe('false')

    await toggle.trigger('click')
    expect(wrapper.find('#public-mobile-nav').exists()).toBe(true)

    routeState.path = '/pricing'
    routeState.fullPath = '/pricing'
    await nextTick()
    expect(wrapper.find('#public-mobile-nav').exists()).toBe(false)
    expect(toggle.attributes('aria-expanded')).toBe('false')
  })

  it('closes the compact menu when a navigation item is activated', async () => {
    const wrapper = mountHeader()
    const toggle = wrapper.get('[data-testid="public-nav-toggle"]')

    await toggle.trigger('click')
    const pricingLink = wrapper
      .get('[data-testid="public-mobile-nav-list"]')
      .find('a[href="/pricing"]')

    await pricingLink.trigger('click')
    expect(wrapper.find('#public-mobile-nav').exists()).toBe(false)
    expect(toggle.attributes('aria-expanded')).toBe('false')
  })
})
