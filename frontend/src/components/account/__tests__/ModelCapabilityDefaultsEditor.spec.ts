import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ModelCapabilityDefaultsEditor from '../ModelCapabilityDefaultsEditor.vue'
import { parseModelCapabilityDefaults } from '../modelCapabilityDefaults'

vi.mock('vue-i18n', async () => ({ ...await vi.importActual('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
describe('capability specification editor', () => {
  it.each(['{', '[]', '{}', '{"schema_version":1,"reason":" ","models":{"private":{}}}'])('associates invalid JSON or envelope %s with its field', text => {
    const wrapper = mount(ModelCapabilityDefaultsEditor, { props: { modelValue: text } })
    const input = wrapper.get('textarea')
    expect(input.attributes('aria-invalid')).toBe('true')
    expect(input.attributes('aria-describedby')).toContain(wrapper.get('[role="alert"]').attributes('id'))
    expect(wrapper.get('[role="alert"]').text()).toContain('capabilityDefaults.invalid')
    wrapper.unmount()
  })
  it('preserves explicit false and empty arrays as specifications and emits only a local draft', async () => {
    const declaration = { schema_version: 1, reason: 'verified deployment', models: { private: { supports_function_calling: false, output_modalities: [] } } }
    const text = JSON.stringify(declaration)
    const wrapper = mount(ModelCapabilityDefaultsEditor, { props: { modelValue: text } })
    expect(wrapper.get('textarea').attributes('aria-invalid')).toBe('false')
    expect(parseModelCapabilityDefaults(text)).toEqual(declaration)
    await wrapper.get('textarea').setValue('')
    expect(wrapper.emitted('update:modelValue')).toEqual([['']])
    expect(parseModelCapabilityDefaults('')).toBeNull()
    wrapper.unmount()
  })
})
