import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createI18n, type LocaleMessages, type VueMessageType } from 'vue-i18n'
import { baseCompile } from '@intlify/message-compiler'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import McpAuthorizeView from '../McpAuthorizeView.vue'

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
  en: prepareMessages({ publicPortal: en.publicPortal }),
  zh: prepareMessages({ publicPortal: zh.publicPortal })
}

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    user: { id: 1 },
    isAdmin: false
  })
}))

vi.mock('@/api/mcp', () => ({
  mcpAPI: {
    authorizeGrant: vi.fn(),
    listGrants: vi.fn()
  }
}))

describe('McpAuthorizeView i18n [E1-9]', () => {
  it('renders localized English copy without hardcoded Chinese when locale is en', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/oauth/authorize', component: McpAuthorizeView }]
    })
    await router.push(
      `/oauth/authorize?client_id=test-client&client_name=Test+App&redirect_uri=https://app.example.com/cb&resource=${encodeURIComponent(`${window.location.origin}/mcp`)}`
    )
    const i18n = createI18n({
      legacy: false,
      locale: 'en',
      messages: compiledMessages
    })

    const wrapper = mount(McpAuthorizeView, {
      global: {
        plugins: [router, i18n],
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' }
        }
      }
    })
    await flushPromises()

    const text = wrapper.text()
    expect(/[\u4e00-\u9fff]/.test(text)).toBe(false)
    expect(text).toContain('Authorize')
    expect(text).toContain('Deny')
  })
})

describe('McpAuthorizeView missing client context guard (FL-04)', () => {
  it('shows an explicit error and never renders demo client details when query context is missing', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/oauth/authorize', component: McpAuthorizeView }]
    })
    await router.push('/oauth/authorize')
    const i18n = createI18n({
      legacy: false,
      locale: 'zh',
      messages: compiledMessages
    })

    const wrapper = mount(McpAuthorizeView, {
      global: {
        plugins: [router, i18n],
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' }
        }
      }
    })
    await flushPromises()

    const alert = wrapper.get('[role="alert"]')
    expect(alert.text()).toContain('缺少必要参数')
    expect(wrapper.text()).not.toContain('Claude Desktop MCP')
    expect(wrapper.text()).not.toContain('claude-desktop-mcp')
    expect(wrapper.text()).not.toContain('127.0.0.1:43110')
    expect(wrapper.find('form').exists()).toBe(false)
  })
})
