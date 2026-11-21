import { createI18n, type LocaleMessages, type VueMessageType } from 'vue-i18n'
import { baseCompile } from '@intlify/message-compiler'
import { createMemoryHistory, createRouter } from 'vue-router'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import enMessages from '@/i18n/locales/en'
import zhMessages from '@/i18n/locales/zh'
import LoginAgreementPrompt from '@/components/auth/LoginAgreementPrompt.vue'

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

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/legal/:documentId', name: 'LegalDocument', component: { template: '<div />' } }
    ]
  })
}

describe('E1-1 LoginAgreementPrompt', () => {
  it('renders localized button labels without hardcoded Chinese in English locale', async () => {
    const router = createTestRouter()
    await router.push('/')
    await router.isReady()
    const i18n = createI18n({
      legacy: false,
      locale: 'en',
      messages: compiledMessages
    })

    const wrapper = mount(LoginAgreementPrompt, {
      props: {
        enabled: true,
        accepted: false,
        mode: 'checkbox',
        updatedAt: '2026-11-20',
        visible: true,
        documents: [
          { id: 'terms', title: 'Terms of Service', content_md: 'Terms text' },
          { id: 'privacy-policy', title: 'Privacy Policy', content_md: 'Privacy text' }
        ]
      },
      global: {
        plugins: [router, i18n],
        stubs: {
          Icon: true,
          Teleport: true
        }
      }
    })
    await flushPromises()

    expect(wrapper.text()).not.toContain('查看服务条款')
    expect(wrapper.text()).not.toContain('查看隐私政策')
    expect(wrapper.text()).not.toContain('新窗口打开完整文档')
    expect(wrapper.find('#login-agreement-consent').exists()).toBe(true)
  })

  it('omits the consent checkbox when agreement gate is disabled while keeping legal document links', async () => {
    const router = createTestRouter()
    await router.push('/')
    await router.isReady()
    const i18n = createI18n({
      legacy: false,
      locale: 'en',
      messages: compiledMessages
    })

    const wrapper = mount(LoginAgreementPrompt, {
      props: {
        enabled: false,
        accepted: true,
        mode: 'modal',
        visible: false,
        documents: []
      },
      global: {
        plugins: [router, i18n],
        stubs: {
          Icon: true,
          Teleport: true
        }
      }
    })
    await flushPromises()

    expect(wrapper.find('#login-agreement-consent').exists()).toBe(false)
    expect(wrapper.find('.auth-legal-actions').exists()).toBe(true)
  })

  it('localizes default Chinese backend legal document titles when locale is English [E1-9]', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/legal/:documentId', name: 'LegalDocument', component: { template: '<div />' } }]
    })
    await router.push('/')
    await router.isReady()
    const i18n = createI18n({
      legacy: false,
      locale: 'en',
      messages: compiledMessages
    })

    const wrapper = mount(LoginAgreementPrompt, {
      props: {
        enabled: true,
        accepted: false,
        mode: 'checkbox',
        visible: false,
        documents: [
          { id: 'terms', title: '服务条款', content_md: '' },
          { id: 'privacy-policy', title: '隐私政策', content_md: '' }
        ]
      },
      global: {
        plugins: [router, i18n],
        stubs: {
          Icon: true,
          Teleport: true
        }
      }
    })
    await flushPromises()

    expect(wrapper.text()).toContain('OmniHive Terms of Service')
    expect(wrapper.text()).toContain('OmniHive Privacy Policy')
    expect(wrapper.text()).not.toContain('服务条款')
    expect(wrapper.text()).not.toContain('隐私政策')
  })
})
