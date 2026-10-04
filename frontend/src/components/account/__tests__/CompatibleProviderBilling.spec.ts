import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import CompatibleProviderBilling from '../CompatibleProviderBilling.vue'
import { compatibleProvider, buildCompatibleCredentials, type CompatibleProvider } from '@/constants/compatibleProviders'

function render(provider: CompatibleProvider, locale = 'en') {
  return mount(CompatibleProviderBilling, { props: { provider }, global: { plugins: [createI18n({ legacy: false, locale, messages: {} })] } })
}
describe('supplier billing disclosures', () => {
  it('distinguishes same-key automatic billing and dedicated-key plans without inputs or mutation', () => {
    for (const id of ['ccsub', 'atlascloud', 'qiniu', 'nous_research', 'aicodemirror', 'aigocode', 'fennoai', 'github_copilot', 'jiekou', 'novita', 'ppio', 'relaxycode', 'shengsuanyun', 'astron_coding', 'byteplus', 'qwencloud_maas_token']) {
      const provider = compatibleProvider(id)!
      const snapshot = JSON.stringify(provider)
      const wrapper = render(provider)
      expect(wrapper.text()).toContain(provider.billing!.summary)
      expect(wrapper.get('a').attributes('href')).toBe(provider.billing!.evidence_url)
      expect(wrapper.get('a').attributes('rel')).toBe('noopener noreferrer')
      expect(wrapper.find('input,select,button,[role="radio"]').exists()).toBe(false)
      expect(wrapper.emitted()).toEqual({})
      expect(JSON.stringify(provider)).toBe(snapshot)
      expect(buildCompatibleCredentials(provider, {}).account_mode).toBe(provider.default_mode)
    }
    expect(compatibleProvider('ccsub')!.billing!.same_key).toBe(true)
    expect(compatibleProvider('atlascloud')!.billing!.same_key).toBe(false)
    expect(compatibleProvider('qiniu')!.billing!.same_key).toBe(false)
    expect(render(compatibleProvider('qiniu')!).text()).toContain('does not automatically fall back')
    for (const id of ['jiekou', 'novita', 'ppio', 'relaxycode']) {
      expect(render(compatibleProvider(id)!).text()).toContain('not established')
      expect(compatibleProvider(id)!.modes).toHaveLength(1)
      expect(compatibleProvider(id)!.billing!.same_key).toBeUndefined()
    }
  })
  it('renders Chinese and treats absent billing as unknown', () => {
    const provider = compatibleProvider('ccsub')!
    expect(render(provider, 'zh-CN').text()).toContain(provider.billing!.summary_zh)
    expect(render({ ...provider, billing: undefined }).find('[data-testid="compatible-billing"]').exists()).toBe(false)
  })
  it('preserves old credentials byte-for-byte and restricts explicit new key modes', () => {
    for (const id of ['ccsub', 'atlascloud', 'qiniu', 'mimo_token_cn']) {
      const provider = compatibleProvider(id)!
      const draft = { api_key: '  opaque-key\n', account_mode: provider.default_mode, api_protocol: 'adaptive', base_url: 'https://relay.example/prefix/', api_base_urls: { anthropic: 'https://private.example/v1' }, auth_mode: 'untouched', extra: { untouched: true } }
      const before = JSON.stringify(draft)
      expect(JSON.stringify(buildCompatibleCredentials(provider, draft))).toBe(before)
      expect(JSON.stringify(draft)).toBe(before)
    }
    expect(buildCompatibleCredentials(compatibleProvider('qiniu')!, { account_mode: 'subscription' }).account_mode).toBe('subscription')
    expect(() => buildCompatibleCredentials(compatibleProvider('qiniu')!, { account_mode: 'subscription', api_protocol: 'anthropic' })).toThrow()
    expect(() => buildCompatibleCredentials(compatibleProvider('atlascloud')!, { account_mode: 'coding', api_protocol: 'responses' })).toThrow()
    for (const account_mode of [false, {}, 'unknown', 'Coding']) expect(() => buildCompatibleCredentials(compatibleProvider('atlascloud')!, { account_mode })).toThrow()
  })
})
