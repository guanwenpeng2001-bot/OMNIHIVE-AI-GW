import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { compatibleFixture, resourceFixture } from './compatibleProviderFixtures'
vi.mock('@/constants/compatibleProviders', async () => {
  const actual = await vi.importActual<typeof import('@/constants/compatibleProviders')>('@/constants/compatibleProviders')
  const { compatibleFixture, resourceFixture } = await import('./compatibleProviderFixtures')
  return { ...actual, COMPATIBLE_PROVIDERS: [...actual.COMPATIBLE_PROVIDERS, compatibleFixture, resourceFixture], compatibleProvider: (id: string) => [compatibleFixture, resourceFixture].find(provider => provider.id === id) ?? actual.compatibleProvider(id) }
})
const { locale } = vi.hoisted(() => ({ locale: { value: 'en' } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, locale }) }))
import CompatibleProviderFields from '../CompatibleProviderFields.vue'
import AccountPlatformSelector from '../AccountPlatformSelector.vue'
import { GROUP_PLATFORM_OPTIONS, PRODUCT_PLATFORM_OPTIONS, getPlatformLabel, platformSupportsOAuth, platformSupportsPricingSync, platformFamily } from '@/constants/platforms'
import { platformBadgeClass, platformSolidBadgeClass } from '@/utils/platformColors'
import { compatibleDiscoveryDisabled, buildCompatibleCredentials, compatibleProvider } from '@/constants/compatibleProviders'
import { isCNProviderPlatform, isMultiProtocolApiKeyPlatform, showsCodingPlanMode, isDeclaredProtocolPlatform } from '../credentialsBuilder'
import en from '@/i18n/locales/en/admin/accounts'
import zh from '@/i18n/locales/zh/admin/accounts'

function mountFields(provider = compatibleFixture, modelValue: Record<string, unknown> = {}) {
  const wrapper = mount(CompatibleProviderFields, { props: { provider, modelValue,
    'onUpdate:modelValue': value => { void wrapper.setProps({ modelValue: value }) }
  } })
  return wrapper
}

describe('shared compatible provider forms', () => {
  it.each(['ccsub', 'nous_research', 'github_copilot'])('discloses %s billing without emitting or mutating create/edit credentials', (id) => {
    const provider = compatibleProvider(id)!
    for (const draft of [{}, { account_mode: provider.default_mode, api_key_ref: 'masked-reference', base_url: 'https://relay.example/v1', model_mapping: { alias: 'manual-model' } }]) {
      const before = JSON.stringify(draft)
      const wrapper = mountFields(provider, draft)
      const disclosure = wrapper.get('[data-testid="compatible-billing"]')
      expect(disclosure.text()).toContain(provider.billing!.summary)
      expect(disclosure.find('input,select,button,[role="radio"]').exists()).toBe(false)
      expect(wrapper.emitted('update:modelValue')).toBeUndefined()
      expect(JSON.stringify(draft)).toBe(before)
      if (id !== 'ccsub') {
        expect(wrapper.find('[data-testid="compatible-mode"]').exists()).toBe(false)
        expect(wrapper.find('[data-testid="compatible-protocol"]').exists()).toBe(false)
      }
      wrapper.unmount()
    }
  })

  it('retains a provider-scoped billing slot override', () => {
    const provider = compatibleProvider('ccsub')!
    const wrapper = mount(CompatibleProviderFields, { props: { provider, modelValue: {} }, slots: { billing: '<p data-testid="billing-override">{{ params.provider.id }}</p>' } })
    expect(wrapper.get('[data-testid="billing-override"]').text()).toBe('ccsub')
    expect(wrapper.find('[data-testid="compatible-billing"]').exists()).toBe(false)
  })

  it.each(['en', 'zh'])('uses actual dedicated-key labels and mode-specific protocols in %s', async (language) => {
    locale.value = language
    for (const [id, selectedMode, protocols] of [
      ['atlascloud', 'coding', ['adaptive', 'chat_completions', 'anthropic']],
      ['qiniu', 'subscription', ['adaptive', 'chat_completions']]
    ] as const) {
      const provider = compatibleProvider(id)!
      const wrapper = mountFields(provider, { account_mode: 'payg', api_protocol: 'responses', api_key_ref: 'masked-reference' })
      const option = wrapper.get(`[data-testid="compatible-mode"] [data-choice="${selectedMode}"]`)
      const vendorMode = provider.modes.find(entry => entry.mode === selectedMode)!
      expect(option.text()).toContain(language === 'zh' ? vendorMode.label_zh! : vendorMode.label!)
      expect(wrapper.text()).toContain('admin.accounts.supplier.planKeyHint')
      expect(wrapper.get('[data-testid="compatible-billing"]').text()).toContain(language === 'zh' ? provider.billing!.summary_zh : provider.billing!.summary)
      await option.trigger('click')
      expect(wrapper.get('[data-testid="compatible-protocol"]').findAll('[role="radio"]').map(radio => radio.attributes('data-choice'))).toEqual(protocols)
      expect(wrapper.props('modelValue')).toEqual({ account_mode: selectedMode, api_protocol: 'adaptive', api_key_ref: 'masked-reference' })
      wrapper.unmount()
    }
    locale.value = 'en'
  })

  it('projects named entries into labels, filters, groups and a searchable bounded picker', async () => {
    expect(PRODUCT_PLATFORM_OPTIONS.find(option => option.value === compatibleFixture.id)?.label).toBe('Example Cloud')
    expect(GROUP_PLATFORM_OPTIONS.some(option => option.value === compatibleFixture.id)).toBe(true)
    expect(getPlatformLabel(compatibleFixture.id)).toBe('Example Cloud')
    for (const [labels, expected] of [[en.accounts.platforms, 'Example Cloud'], [zh.accounts.platforms, '示例云']] as const) {
      expect(getPlatformLabel(compatibleFixture.id, key => (labels as Record<string, string>)[key.split('.').at(-1)!] ?? key)).toBe(expected)
    }
    const SelectStub = defineComponent({ props: ['modelValue', 'options'], template: '<select />' })
    const wrapper = mount(AccountPlatformSelector, { props: { modelValue: compatibleFixture.id }, global: { stubs: { Select: SelectStub, PlatformIcon: true } } })
    expect(wrapper.get('[role="radiogroup"]').classes()).toContain('max-h-80')
    await wrapper.get('input[type="search"]').setValue('Example Cloud')
    expect(wrapper.findAll('[role="radio"]')).toHaveLength(1)
    await wrapper.get('[data-testid="example-cloud-platform"]').trigger('click')
    expect(wrapper.emitted('select')?.[0]).toEqual(['example-cloud'])
    expect(platformFamily('example-cloud')).toBe('openaiCompatible')
    expect(platformBadgeClass('example-cloud')).toBeTruthy()
    expect(platformBadgeClass('example-cloud')).not.toBe(platformBadgeClass('openai'))
    expect(platformSolidBadgeClass('example-cloud')).toBeTruthy()
    expect(platformSolidBadgeClass('example-cloud')).not.toBe(platformSolidBadgeClass('openai'))
  })
  it('does not inherit native-provider OAuth, pricing, quota or form features', () => {
    for (const predicate of [platformSupportsOAuth, platformSupportsPricingSync, isCNProviderPlatform, isMultiProtocolApiKeyPlatform, showsCodingPlanMode, isDeclaredProtocolPlatform]) expect(predicate('example-cloud')).toBe(false)
    expect(compatibleDiscoveryDisabled(compatibleFixture, { account_mode: 'coding' })).toBe(true)
    expect(compatibleDiscoveryDisabled(compatibleFixture, { account_mode: 'payg' })).toBe(false)
  })
  it('offers native choices only, shows auth without a secret, and isolates coding defaults', async () => {
    const wrapper = mountFields()
    expect(wrapper.get('[data-testid="compatible-mode"] [role="radiogroup"]').attributes('aria-label')).toBe('admin.accounts.supplier.planType')
    expect(wrapper.get('[data-testid="compatible-protocol"]').text()).toContain('admin.accounts.supplier.protocolDescriptions.adaptive')
    expect(wrapper.get('[data-testid="compatible-protocol"]').findAll('[role="radio"]').map(option => option.attributes('data-choice'))).toEqual(['adaptive', 'chat_completions', 'anthropic'])
    expect(wrapper.text()).toContain('Authorization: Bearer …')
    expect(wrapper.text()).toContain('x-api-key: …')
    expect(wrapper.find('[data-testid="compatible-base-responses"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="compatible-base-anthropic"]').attributes('placeholder')).toBe('https://api.example.com/anthropic')
    await wrapper.get('[data-testid=\"compatible-protocol\"] [data-choice=\"chat_completions\"]').trigger('click')
    expect(wrapper.find('[data-testid="compatible-base-anthropic"]').exists()).toBe(false)
    await wrapper.get('[data-testid=\"compatible-mode\"] [data-choice=\"coding\"]').trigger('click')
    expect(wrapper.get('[data-testid="compatible-protocol"] [data-choice="adaptive"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[data-testid="compatible-base-anthropic"]').attributes('placeholder')).toBe('https://coding.example.com')
    expect(wrapper.text()).toContain('admin.accounts.compatible.manualModels')
  })
  it('uses coding-only default and preserves overrides when changing mode or protocol', async () => {
    const provider = { ...compatibleFixture, default_mode: 'coding' as const, modes: [compatibleFixture.modes[1]] }
    const wrapper = mountFields(provider, { base_url: 'https://relay.example.com', api_base_urls: { anthropic: 'https://relay.example.com/messages' } })
    expect(wrapper.find('[data-testid="compatible-mode"]').exists()).toBe(false)
    await wrapper.get('[data-testid=\"compatible-protocol\"] [data-choice=\"anthropic\"]').trigger('click')
    expect(buildCompatibleCredentials(provider, wrapper.props('modelValue'))).toEqual({ account_mode: 'coding', api_protocol: 'anthropic', base_url: 'https://relay.example.com', api_base_urls: { anthropic: 'https://relay.example.com/messages' } })
  })
  it('shows Azure resource and per-model deployment guidance without a deployment_id field', async () => {
    const wrapper = mountFields(resourceFixture)
    expect(wrapper.text()).toContain('api-key: …')
    expect(wrapper.text()).toContain('/openai/v1')
    expect(wrapper.get('[data-testid="compatible-deployment-hint"]').exists()).toBe(true)
    expect(wrapper.html()).not.toContain('deployment_id')
    await wrapper.get('[data-testid="compatible-base"]').setValue('https://resource.example.com')
    expect(buildCompatibleCredentials(resourceFixture, wrapper.props('modelValue'))).toEqual({ account_mode: 'payg', api_protocol: 'adaptive', base_url: 'https://resource.example.com' })
  })
})


it.each(['en', 'zh'])('uses explicit vendor mode names in %s including future subscription modes', async (language) => {
  locale.value = language
  const provider = { ...compatibleFixture, modes: [compatibleFixture.modes[0], { ...compatibleFixture.modes[1], mode: 'subscription', label: 'Token subscription', label_zh: 'Token 订阅套餐' }] } as unknown as typeof compatibleFixture
  const wrapper = mountFields(provider)
  const subscription = wrapper.get('[data-testid="compatible-mode"] [data-choice="subscription"]')
  expect(subscription.text()).toContain(language === 'zh' ? 'Token 订阅套餐' : 'Token subscription')
  expect(subscription.text()).toContain('admin.accounts.supplier.planDescriptions.subscription')
  await subscription.trigger('click')
  expect(wrapper.props('modelValue').account_mode).toBe('subscription')
  expect(wrapper.get('[data-testid="compatible-protocol"] [data-choice="adaptive"]').attributes('aria-checked')).toBe('true')
  wrapper.unmount()
  locale.value = 'en'
})
