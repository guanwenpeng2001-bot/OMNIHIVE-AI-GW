import { describe, expect, it } from 'vitest'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { baseCompile } from '@intlify/message-compiler'
import en from '@/i18n/locales/en/admin/accounts'
import zh from '@/i18n/locales/zh/admin/accounts'
import { mount as mountComponent } from '@vue/test-utils'
import CompatibleProviderFields from '../CompatibleProviderFields.vue'
import CompatibleSpecialAuthFields from '../CompatibleSpecialAuthFields.vue'
import { compatibleProvider, buildCompatibleCredentials } from '@/constants/compatibleProviders'
import { changeSpecialCompatibleMode, specialCompatibleKeyOptional, validateSpecialCompatibleDraft } from '../compatibleSpecialAuth'

function localizedI18n(locale: 'en' | 'zh' = 'en') {
  return createI18n({
    legacy: false,
    locale,
    messageCompiler: message => new Function('return ' + baseCompile(message as string).code)(),
    messages: { en: { admin: en }, zh: { admin: zh } }
  })
}

const mount: typeof mountComponent = (component, options) => mountComponent(component, {
  ...options, global: { plugins: [localizedI18n()], ...options?.global }
})

describe('Copilot and Nous authentication fields', () => {
  it('uses independent authentication and routing modes without presenting vendor billing claims', () => {
    const provider = compatibleProvider('github_copilot')!
    const wrapper = mount(CompatibleProviderFields, { props: { provider, modelValue: {} } })
    expect(wrapper.text()).toContain('fine-grained PAT')
    expect(wrapper.text()).toContain('OmniHive converts Messages, Responses and Gemini')
    expect(wrapper.find('[data-testid="compatible-mode"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="compatible-protocol"]').exists()).toBe(false)
    expect(buildCompatibleCredentials(provider, {}).account_mode).toBe('payg')
  })

  it('preserves untouched reference metadata and clears mode-specific secrets and endpoints on mode changes', async () => {
    const saved = { auth_mode: 'invocation_token', account_mode: 'payg', base_url: 'https://inference-api.nousresearch.com/v1', api_key: 'old-synthetic-secret', credential_secret_reference: 'account-ref', credential_secret_version: 4, model_mapping: { alias: 'model' } }
    const wrapper = mount(CompatibleSpecialAuthFields, { props: { platform: 'nous_research', modelValue: saved } })
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.get('[data-testid="special-auth-mode"]').setValue('official_proxy')
    const changed = wrapper.emitted('update:modelValue')![0][0] as Record<string, unknown>
    expect(changed).toMatchObject({ auth_mode: 'official_proxy', account_mode: 'payg', api_key: '', base_url: '', credential_secret_reference: 'account-ref', credential_secret_version: 4, model_mapping: { alias: 'model' } })
    expect(wrapper.emitted('reset-secret')).toHaveLength(1)
    expect(saved.api_key).toBe('old-synthetic-secret')
    await wrapper.setProps({ modelValue: changed })
    expect(wrapper.get('[data-testid="special-auth-base"]').attributes('required')).toBeDefined()
    expect(wrapper.text()).toContain('otherwise, leave it empty')
    expect(specialCompatibleKeyOptional('nous_research', changed)).toBe(true)
    expect(() => validateSpecialCompatibleDraft('nous_research', changed)).toThrow()
    expect(() => validateSpecialCompatibleDraft('nous_research', { ...changed, base_url: 'http://127.0.0.1:8645/v1' })).not.toThrow()
    expect(changeSpecialCompatibleMode(changed, 'invocation_token').api_key).toBe('')
  })

  it('preserves stored secrets for ordinary edits and rejects refresh and missing mode', async () => {
    const saved = { auth_mode: 'official_proxy', credential_secret_reference: 'account-ref', credential_secret_version: 3, base_url: 'https://proxy.test/v1' }
    const wrapper = mount(CompatibleSpecialAuthFields, { props: { platform: 'nous_research', modelValue: saved } })
    await wrapper.get('[data-testid="special-auth-base"]').setValue('https://proxy.test/prefix')
    const edited = wrapper.emitted('update:modelValue')![0][0] as Record<string, unknown>
    expect(edited).toMatchObject({ credential_secret_reference: 'account-ref', credential_secret_version: 3 })
    expect(edited).not.toHaveProperty('api_key')
    expect(wrapper.emitted('reset-secret')).toBeUndefined()
    expect(() => validateSpecialCompatibleDraft('nous_research', {})).toThrow()
    expect(() => validateSpecialCompatibleDraft('github_copilot', { refresh_token: '' })).toThrow()
    expect(() => validateSpecialCompatibleDraft('github_copilot', { auth_mode: 'fine_grained_pat' })).not.toThrow()
    await wrapper.get('[data-testid="special-auth-clear-key"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')!.at(-1)![0]).toMatchObject({ api_key: '', credential_secret_reference: 'account-ref', credential_secret_version: 3 })
    expect(wrapper.emitted('reset-secret')).toHaveLength(1)
  })
})


describe('localized provider guidance', () => {
  it.each(['en', 'zh'] as const)('renders Copilot guidance in %s', locale => {
    const wrapper = mount(CompatibleProviderFields, {
      props: { provider: compatibleProvider('github_copilot')!, modelValue: {} },
      global: { plugins: [localizedI18n(locale)] }
    })
    expect(wrapper.text()).toContain(locale === 'zh'
      ? '具有 Copilot Requests 权限的 GitHub 细粒度个人访问令牌（PAT）'
      : 'GitHub fine-grained PAT with Copilot Requests permission')
    expect(wrapper.text()).toContain(locale === 'zh' ? '账号和组织设置' : 'account and organization settings')
    expect(wrapper.text()).toContain(locale === 'zh' ? '基础地址（可选）' : 'Base URL (optional)')
    expect(wrapper.text()).toContain(locale === 'zh' ? 'OmniHive 会将 Messages、Responses 和 Gemini' : 'OmniHive converts Messages, Responses and Gemini')
    expect(wrapper.text()).not.toMatch(/Routing mode|payg|identifies itself|frontdoor|admin\.accounts/)
    expect(wrapper.get('[data-testid="special-auth-base"]').attributes('required')).toBeUndefined()
    expect(wrapper.get('[data-testid="special-auth-base"]').attributes('placeholder')).toBe('https://api.githubcopilot.com/v1')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it.each(['en', 'zh'] as const)('renders Nous modes and preserves mode/clear behavior across a %s language change', async locale => {
    const i18n = localizedI18n(locale)
    const saved = { auth_mode: 'invocation_token', api_key: 'synthetic-token', base_url: 'https://inference-api.nousresearch.com/v1' }
    const wrapper = mount(CompatibleSpecialAuthFields, {
      props: { platform: 'nous_research', modelValue: saved }, global: { plugins: [i18n] }
    })
    expect(wrapper.text()).toContain(locale === 'zh' ? '认证方式' : 'Authentication mode')
    expect(wrapper.text()).toContain(locale === 'zh' ? '请选择认证方式' : 'Select authentication mode')
    expect(wrapper.text()).toContain(locale === 'zh' ? '手动调用令牌' : 'Manual invocation token')
    expect(wrapper.text()).toContain(locale === 'zh' ? '手动续期并替换' : 'Renew and replace it manually')
    expect(wrapper.get('[data-testid="special-auth-base"]').attributes('required')).toBeUndefined()
    expect(wrapper.findAll('option').map(option => option.attributes('value'))).toEqual(['', 'invocation_token', 'official_proxy'])
    await wrapper.get('[data-testid="special-auth-mode"]').setValue('official_proxy')
    const changed = wrapper.emitted('update:modelValue')![0][0] as Record<string, unknown>
    expect(changed).toMatchObject({ auth_mode: 'official_proxy', api_key: '', base_url: '' })
    await wrapper.setProps({ modelValue: changed })
    expect(wrapper.text()).toContain(locale === 'zh' ? '由运营方部署的官方 Hermes 代理' : 'official Hermes proxy hosted by your operator')
    expect(wrapper.text()).toContain(locale === 'zh' ? '否则留空' : 'otherwise, leave it empty')
    expect(wrapper.text()).toContain(locale === 'zh' ? '代理基础地址（必填）' : 'Proxy base URL (required)')
    expect(wrapper.get('[data-testid="special-auth-base"]').attributes('placeholder')).toBe('https://hermes.example.com/v1')
    i18n.global.locale.value = locale === 'zh' ? 'en' : 'zh'
    await nextTick()
    expect(wrapper.text()).toContain(locale === 'zh' ? 'Proxy base URL (required)' : '代理基础地址（必填）')
    expect(wrapper.get('[data-testid="special-auth-clear-key"]').text()).toBe(locale === 'zh' ? 'Clear proxy credential' : '清除代理凭据')
    expect(wrapper.get('[data-testid="special-auth-base"]').attributes('required')).toBeDefined()
    expect(wrapper.emitted('update:modelValue')).toHaveLength(1)
    expect(wrapper.emitted('reset-secret')).toHaveLength(1)
    await wrapper.get('[data-testid="special-auth-clear-key"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')![1][0]).toEqual({ ...changed, api_key: '' })
    expect(wrapper.emitted('reset-secret')).toHaveLength(2)
    await wrapper.get('[data-testid="special-auth-mode"]').setValue('invocation_token')
    await wrapper.setProps({ modelValue: wrapper.emitted('update:modelValue')![2][0] as Record<string, unknown> })
    expect(wrapper.text()).toContain(locale === 'zh' ? 'Renew and replace it manually' : '手动续期并替换')
    expect(wrapper.get('[data-testid="special-auth-base"]').attributes('required')).toBeUndefined()
    expect(wrapper.find('[data-testid="special-auth-clear-key"]').exists()).toBe(false)
    expect(saved.api_key).toBe('synthetic-token')
  })
})
