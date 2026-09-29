import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import Toggle from '../Toggle.vue'

describe('Toggle switch semantics', () => {
  it('forwards the field name and exposes checked state without pressed semantics', async () => {
    const wrapper = mount(Toggle, { props: { modelValue: false }, attrs: { 'aria-label': 'Enable notifications' } })
    const control = wrapper.get<HTMLButtonElement>('button[role="switch"]')
    expect(control.attributes('aria-label')).toBe('Enable notifications')
    expect(control.attributes('aria-checked')).toBe('false')
    expect(control.attributes('aria-pressed')).toBeUndefined()
    expect(control.element.tabIndex).toBe(0)
    await control.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[true]])
    await wrapper.setProps({ modelValue: true, disabled: true })
    expect(control.attributes('aria-checked')).toBe('true')
    expect(control.element.disabled).toBe(true)
    await control.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toHaveLength(1)
    wrapper.unmount()
  })
})
