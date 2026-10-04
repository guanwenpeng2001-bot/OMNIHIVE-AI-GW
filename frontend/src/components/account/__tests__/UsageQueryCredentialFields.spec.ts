import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import UsageQueryCredentialFields from '../UsageQueryCredentialFields.vue'
import { usageQueryDraft, applyUsageQueryCredentials } from '../credentialsBuilder'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('management query credentials', () => {
  it.each(['aihubmix', 'dmxapi', 'crazyrouter', 'crazyrouter_intl', 'therouter'])('shows only the fields required by %s', platform => {
    const draft = usageQueryDraft({ usage_query_token: 'legacy-secret-must-not-render', usage_query_user_id: '42' })
    const wrapper = mount(UsageQueryCredentialFields, { props: { platform, modelValue: draft, editing: true, hasToken: true } })
    expect(wrapper.get('[data-test="usage-query-token"]').attributes('type')).toBe('password')
    expect(wrapper.get('[data-test="usage-query-token"]').element).toHaveProperty('value', '')
    expect(wrapper.html()).not.toContain('legacy-secret')
    expect(wrapper.find('[data-test="usage-query-user-id"]').exists()).toBe(['dmxapi', 'crazyrouter', 'crazyrouter_intl'].includes(platform))
    expect(wrapper.find('[data-test="usage-query-saved"]').exists()).toBe(true)
  })
  it('retains a blank token and clears only with explicit null', async () => {
    let draft = usageQueryDraft()
    const wrapper = mount(UsageQueryCredentialFields, { props: { platform: 'dmxapi', modelValue: draft, editing: true } })
    const credentials: Record<string, unknown> = { api_key: 'inference', usage_query_token: 'legacy' }
    applyUsageQueryCredentials(credentials, 'dmxapi', draft)
    expect(credentials).not.toHaveProperty('usage_query_token')
    expect(credentials.api_key).toBe('inference')
    await wrapper.get('[data-test="usage-query-token"]').setValue('new-management')
    draft = wrapper.emitted('update:modelValue')![0][0] as typeof draft
    await wrapper.setProps({ modelValue: draft })
    applyUsageQueryCredentials(credentials, 'dmxapi', draft)
    expect(credentials.usage_query_token).toBe('new-management')
    await wrapper.get('[data-test="usage-query-clear"]').setValue(true)
    draft = wrapper.emitted('update:modelValue')![1][0] as typeof draft
    await wrapper.setProps({ modelValue: draft })
    expect(wrapper.get('[data-test="usage-query-token"]').attributes('disabled')).toBeDefined()
    applyUsageQueryCredentials(credentials, 'dmxapi', draft)
    expect(credentials.usage_query_token).toBeNull()
  })
})
