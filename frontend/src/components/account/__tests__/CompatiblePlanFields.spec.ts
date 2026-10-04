import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import CompatibleProviderFields from '../CompatibleProviderFields.vue'
import { buildCompatibleCredentials, compatibleProvider, COMPATIBLE_PROVIDERS, paygBalanceAllowed } from '@/constants/compatibleProviders'
import en from '@/i18n/locales/en/admin/accounts'
import zh from '@/i18n/locales/zh/admin/accounts'

const { locale } = vi.hoisted(() => ({ locale: { value: 'en' } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ locale, t: (key: string) => {
  const messages = { admin: locale.value === 'zh' ? zh : en }
  return key.split('.').reduce<unknown>((value, part) => value && typeof value === 'object' ? (value as Record<string, unknown>)[part] : undefined, messages) ?? key
} }) }))

function render(platform: string, modelValue: Record<string, unknown> = {}, language = 'en') {
  locale.value = language
  const wrapper = mount(CompatibleProviderFields, {
    props: { provider: compatibleProvider(platform)!, modelValue, 'onUpdate:modelValue': value => { void wrapper.setProps({ modelValue: value }) } }
  })
  return wrapper
}

describe('saved compatible plan choices', () => {
  it.each([['aicodemirror', 'pro'], ['ccsub', 'subscription'], ['shengsuanyun', 'enterprise'], ['astron_token', 'premium']])('selects, serializes and restores %s/%s without changing transport', async (platform, plan) => {
    const provider = compatibleProvider(platform)!
    const endpoints = JSON.stringify(provider.modes)
    const draft = { api_key: 'fixture-key', account_mode: provider.default_mode, api_protocol: 'adaptive', base_url: 'https://relay.example/v1', api_base_urls: { anthropic: 'https://private.example/native' }, model_mapping: { alias: 'model' }, credential_secret_reference: 'fixture-reference', credential_secret_version: 3 }
    const wrapper = render(platform, draft)
    expect(wrapper.findAll('[data-testid="compatible-plan"] [aria-checked="true"]')).toHaveLength(0)
    await wrapper.get(`[data-testid="compatible-plan"] [data-choice="${plan}"]`).trigger('click')
    const request = buildCompatibleCredentials(provider, wrapper.props('modelValue'))
    expect(request).toEqual({ ...draft, billing_plan: plan })
    expect(draft).not.toHaveProperty('billing_plan')
    // The corresponding backend handler/DTO contract tests exercise this projection.
    const safeDTO = JSON.parse(JSON.stringify(request))
    delete safeDTO.api_key
    const edit = render(platform, safeDTO)
    expect(edit.get(`[data-choice="${plan}"]`).attributes('aria-checked')).toBe('true')
    await edit.get('[data-testid="compatible-plan-clear"]').trigger('click')
    const clearRequest = buildCompatibleCredentials(provider, edit.props('modelValue'))
    expect(clearRequest.billing_plan).toBeNull()
    expect(JSON.parse(JSON.stringify(clearRequest))).toHaveProperty('billing_plan', null)
    expect(edit.findAll('[data-testid="compatible-plan"] [aria-checked="true"]')).toHaveLength(0)
    expect(JSON.stringify(provider.modes)).toBe(endpoints)
    expect(wrapper.find('[data-testid="compatible-billing"]').exists()).toBe(false)
    if (provider.billing) {
      expect(wrapper.text()).not.toContain(provider.billing.summary)
      expect(wrapper.html()).not.toContain(provider.billing.evidence_url)
      expect(wrapper.text()).not.toContain(provider.billing.evidence_observed)
    }
  })

  it('keeps native cards, bilingual labels, special authentication and dedicated modes', () => {
    const chinese = render('ccsub', { billing_plan: 'subscription' }, 'zh')
    expect(chinese.get('[data-choice="subscription"]').text()).toContain('月度订阅')
    expect(chinese.get('[data-choice="subscription"]').classes()).toContain('border-primary-500')
    for (const platform of ['atlascloud', 'qiniu', 'kat_coder']) {
      const wrapper = render(platform)
      expect(wrapper.find('[data-testid="compatible-mode"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="compatible-plan"]').exists()).toBe(false)
    }
    expect(render('github_copilot').text()).toContain(en.accounts.specialCompatibleAuth.copilotHint)
    expect(render('nous_research').find('[data-testid="compatible-plan"]').exists()).toBe(true)
    for (const platform of ['astron_coding', 'amux', 'packycode']) expect(render(platform).find('[data-testid="compatible-plan"]').exists()).toBe(false)
    for (const platform of ['github_copilot', 'nous_research', 'astron_token', 'byteplus', 'mimo_token_sg']) {
      expect(compatibleProvider(platform)!.plans!.some(plan => plan.query === 'payg')).toBe(false)
    }
    for (const platform of ['novita', 'ppio', 'shengsuanyun']) expect(render(platform).get('[data-testid="compatible-plan"] [data-choice="payg"]').exists()).toBe(true)
  })

  it('preserves absent plans, validates exact IDs and serializes explicit clear markers', () => {
    const provider = compatibleProvider('aicodemirror')!
    expect(buildCompatibleCredentials(provider, { api_key: 'fixture-key' })).not.toHaveProperty('billing_plan')
    for (const value of ['unknown', 'PRO', 1, {}, [], false, ' pro']) {
      expect(() => buildCompatibleCredentials(provider, { billing_plan: value })).toThrow('billing plan')
      expect(paygBalanceAllowed('aicodemirror', { billing_plan: value })).toBe(false)
    }
    for (const platform of ['atlascloud', 'qiniu', 'kat_coder', 'amux']) expect(() => buildCompatibleCredentials(compatibleProvider(platform)!, { billing_plan: 'payg' })).toThrow('billing plan')
    const draft = { billing_plan: '' }
    expect(buildCompatibleCredentials(provider, draft).billing_plan).toBeNull()
    expect(draft.billing_plan).toBe('')
    for (const provider of COMPATIBLE_PROVIDERS) {
      for (const plan of provider.plans ?? []) expect(paygBalanceAllowed(provider.id, { billing_plan: plan.id })).toBe(plan.query === 'payg')
      expect(paygBalanceAllowed(provider.id, {})).toBe(true)
      expect(paygBalanceAllowed(provider.id, { billing_plan: null })).toBe(true)
      if (provider.plans?.length) expect(provider.modes).toHaveLength(1)
    }
  })
})
