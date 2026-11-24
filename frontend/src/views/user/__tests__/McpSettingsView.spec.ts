import { baseCompile } from '@intlify/message-compiler'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it, vi } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import McpSettingsView from '../McpSettingsView.vue'

const mocks = vi.hoisted(() => ({ push: vi.fn(), post: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: mocks.push }) }))
vi.mock('@/api/client', () => ({ apiClient: { get: async () => ({ data: { grants: [] } }), post: mocks.post } }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { id: 1 }, isAdmin: false }) }))

describe('MCP settings', () => {
  it.each([['en', en], ['zh', zh]] as const)('validates both authorization actions inline in %s', async (locale, messages) => {
    const i18n = createI18n({
      legacy: false, locale, messages: { en, zh },
      messageCompiler: message => new Function('return ' + baseCompile(message as string).code)(),
    })
    const wrapper = mount(McpSettingsView, { attachTo: document.body, global: {
      plugins: [i18n], stubs: { AppLayout: { template: '<main><slot /></main>' }, BaseDialog: true },
    } })
    await flushPromises()
    expect(wrapper.get('h1').text()).toBe(messages.nav.mcpAuth)
    if (locale === 'en') expect(wrapper.text()).not.toMatch(/\p{Script=Han}/u)
    const form = wrapper.get('form')
    expect(form.element.noValidate).toBe(true)
    await wrapper.get('#mcp-name').setValue('')
    await form.get('button[type=submit]').trigger('click')
    expect(wrapper.get('[role=alert]').text()).toBe(messages.mcpSettings.missingName)
    await wrapper.get('#mcp-name').setValue('Example')
    await wrapper.get('#mcp-redirect').setValue('invalid')
    await form.get('button[type=button]').trigger('click')
    expect(wrapper.get('[role=alert]').text()).toBe(messages.publicPortal.mcpAuthorize.errInvalidRedirect)
    expect(mocks.post).not.toHaveBeenCalled()
    expect(mocks.push).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
