import { reactive } from 'vue'
import { createI18n, type LocaleMessages, type VueMessageType } from 'vue-i18n'
import { baseCompile } from '@intlify/message-compiler'
import { createMemoryHistory, createRouter } from 'vue-router'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import enMessages from '@/i18n/locales/en'
import zhMessages from '@/i18n/locales/zh'
import LegalDocumentView from '@/views/public/LegalDocumentView.vue'
import PublicShell from '@/components/layout/PublicShell.vue'

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
  en: prepareMessages(enMessages as unknown as Record<string, unknown>),
  zh: prepareMessages(zhMessages as unknown as Record<string, unknown>)
}

const { fetchPublicSettingsMock, appStoreState } = vi.hoisted(() => ({
  fetchPublicSettingsMock: vi.fn(),
  appStoreState: {
    cachedPublicSettings: null as Record<string, unknown> | null,
    siteName: 'OmniHive',
    siteLogo: '',
    docUrl: '',
    contactInfo: '',
    apiBaseUrl: '',
    brandConfig: {
      siteName: 'OmniHive',
      siteLogo: '',
      subtitle: 'Unified AI Inference Gateway'
    }
  }
}))

const routeState = reactive({
  params: { documentId: 'privacy' } as Record<string, string>
})

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return {
    ...actual,
    useRoute: () => routeState
  }
})

vi.mock('@/stores/app', async () => {
  const { reactive } = await import('vue')
  const store = reactive({
    ...appStoreState,
    fetchPublicSettings: (...args: unknown[]) => fetchPublicSettingsMock(...args)
  })
  return { useAppStore: () => store }
})

vi.mock('@/stores', async () => {
  const { useAppStore } = await import('@/stores/app')
  return { useAppStore }
})

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/home', name: 'Home', component: { template: '<div />' } },
      { path: '/login', name: 'Login', component: { template: '<div />' } },
      { path: '/model-plaza', name: 'ModelPlaza', component: { template: '<div />' } },
      { path: '/pricing', name: 'Pricing', component: { template: '<div />' } },
      { path: '/status', name: 'Status', component: { template: '<div />' } },
      { path: '/docs', name: 'Docs', component: { template: '<div />' } },
      { path: '/compatibility', name: 'Compatibility', component: { template: '<div />' } },
      { path: '/support', name: 'Support', component: { template: '<div />' } },
      { path: '/legal/:documentId', name: 'LegalDocument', component: LegalDocumentView }
    ]
  })
}

describe('E1-1 LegalDocumentView & PublicShell footer links', () => {
  beforeEach(() => {
    routeState.params.documentId = 'privacy'
    appStoreState.cachedPublicSettings = null
    fetchPublicSettingsMock.mockReset()
  })

  it('includes Terms and Privacy links in the default PublicShell footer', async () => {
    const router = createTestRouter()
    await router.push('/pricing')
    await router.isReady()

    const i18n = createI18n({
      legacy: false,
      locale: 'en',
      messages: compiledMessages
    })
    const wrapper = mount(PublicShell, {
      global: {
        plugins: [router, i18n],
        stubs: {
          PublicHeader: true,
          SiteICPFooter: true
        }
      }
    })
    await flushPromises()

    const footerLinks = wrapper.findAll('[data-testid="public-shell-footer"] a')
    const hrefs = footerLinks.map((node) => node.attributes('href'))
    expect(hrefs).toContain('/legal/terms')
    expect(hrefs).toContain('/legal/privacy')
  })

  it('resolves operator-published privacy-policy content when visiting /legal/privacy', async () => {
    const router = createTestRouter()
    await router.push('/legal/privacy')
    await router.isReady()

    const settings = {
      site_name: 'OmniHive',
      login_agreement_enabled: true,
      login_agreement_updated_at: '2026-11-20',
      login_agreement_revision: 'rev-privacy-1',
      login_agreement_documents: [
        { id: 'terms', title: 'Custom Terms', content_md: 'Operator terms body.' },
        { id: 'privacy-policy', title: 'Operator Privacy Policy', content_md: 'Operator custom privacy retention policy.' }
      ]
    }
    fetchPublicSettingsMock.mockImplementation(async () => {
      appStoreState.cachedPublicSettings = settings
      return settings
    })

    const i18n = createI18n({
      legacy: false,
      locale: 'en',
      messages: compiledMessages
    })
    const wrapper = mount(LegalDocumentView, {
      global: {
        plugins: [router, i18n],
        stubs: { Icon: true }
      }
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Operator Privacy Policy')
    expect(wrapper.text()).toContain('Operator custom privacy retention policy.')
    expect(wrapper.find('[data-testid="legal-template-notice"]').exists()).toBe(false)
  })

  it('shows an explicit template placeholder notice when configured legal content is empty', async () => {
    const router = createTestRouter()
    await router.push('/legal/privacy-policy')
    await router.isReady()

    const settings = {
      site_name: 'OmniHive',
      login_agreement_enabled: false,
      login_agreement_documents: [
        { id: 'terms', title: 'Terms', content_md: '' },
        { id: 'privacy-policy', title: 'Privacy Policy', content_md: '' }
      ]
    }
    fetchPublicSettingsMock.mockImplementation(async () => {
      appStoreState.cachedPublicSettings = settings
      return settings
    })

    const i18n = createI18n({
      legacy: false,
      locale: 'en',
      messages: compiledMessages
    })
    const wrapper = mount(LegalDocumentView, {
      global: {
        plugins: [router, i18n],
        stubs: { Icon: true }
      }
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="legal-template-notice"]').exists()).toBe(true)
    expect(wrapper.find('.legal-document-content').exists()).toBe(true)
  })

  it('surfaces load error instead of masking public settings failure on /legal/terms', async () => {
    routeState.params.documentId = 'terms'
    const router = createTestRouter()
    await router.push('/legal/terms')
    await router.isReady()

    fetchPublicSettingsMock.mockResolvedValue(null)

    const i18n = createI18n({
      legacy: false,
      locale: 'en',
      messages: compiledMessages
    })
    const wrapper = mount(LegalDocumentView, {
      global: {
        plugins: [router, i18n],
        stubs: { Icon: true }
      }
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="legal-load-error"]').exists()).toBe(true)
    expect(wrapper.text()).toContain(enMessages.legal.loadFailed)
  })
})
