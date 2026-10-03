import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import CompatibleProviderFields from '../CompatibleProviderFields.vue'
import CompatibleSpecialAuthFields from '../CompatibleSpecialAuthFields.vue'
import { compatibleProvider, buildCompatibleCredentials } from '@/constants/compatibleProviders'
import { changeSpecialCompatibleMode, specialCompatibleKeyOptional, validateSpecialCompatibleDraft } from '../compatibleSpecialAuth'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('Copilot and Nous authentication fields', () => {
  it('uses independent authentication and routing modes without presenting vendor billing claims', () => {
    const provider = compatibleProvider('github_copilot')!
    const wrapper = mount(CompatibleProviderFields, { props: { provider, modelValue: {} } })
    expect(wrapper.text()).toContain('fine-grained PAT')
    expect(wrapper.text()).toContain('gateway bridges')
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
    expect(wrapper.text()).toContain('optional proxy frontdoor bearer')
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
