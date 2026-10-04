import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import AccountChoiceCards from '../AccountChoiceCards.vue'

const options = [
  { value: 'payg', label: 'Pay as you go', description: 'Usage-based API service.', icon: 'creditCard' as const },
  { value: 'coding', label: 'Coding Plan', description: 'Use credentials for your plan.', icon: 'bolt' as const }
]

describe('native account choice cards', () => {
  it('keeps the icon beside the complete text block without an extra radio decoration', () => {
    const wrapper = mount(AccountChoiceCards, { props: { label: 'Plan type', modelValue: 'payg', options } })
    const selected = wrapper.get('[aria-checked="true"]')
    expect(selected.classes()).toEqual(expect.arrayContaining(['flex', 'items-center', 'gap-3', 'rounded-lg', 'border-primary-500']))
    expect(selected.element.children).toHaveLength(2)
    expect(selected.element.children[0].getAttribute('aria-hidden')).toBe('true')
    expect(selected.element.children[0].querySelector('svg')).not.toBeNull()
    expect(selected.element.children[1].textContent).toContain(options[0].label)
    expect(selected.element.children[1].textContent).toContain(options[0].description)
    expect(wrapper.find('.rounded-full').exists()).toBe(false)
    expect(wrapper.get('[role="radiogroup"]').attributes('aria-label')).toBe('Plan type')
    wrapper.unmount()
  })

  it('preserves selection, roving focus, wrapping and locked behavior without a visible radio dot', async () => {
    const wrapper = mount(AccountChoiceCards, { attachTo: document.body, props: {
      label: 'Plan type', modelValue: 'payg', options,
      'onUpdate:modelValue': value => { void wrapper.setProps({ modelValue: value }) }
    } })
    await wrapper.get('[data-choice="payg"]').trigger('keydown', { key: 'ArrowLeft' })
    expect(wrapper.props('modelValue')).toBe('coding')
    expect(wrapper.findAll('[tabindex="0"]')).toHaveLength(1)
    expect(document.activeElement).toBe(wrapper.get('[data-choice="coding"]').element)
    expect(wrapper.get('[data-choice="coding"]').attributes('aria-checked')).toBe('true')
    await wrapper.get('[data-choice="coding"]').trigger('keydown', { key: 'Home' })
    expect(wrapper.props('modelValue')).toBe('payg')
    await wrapper.setProps({ locked: true })
    expect(wrapper.findAll('[tabindex="0"]')).toHaveLength(0)
    expect(wrapper.findAll('button:disabled')).toHaveLength(2)
    await wrapper.get('[data-choice="payg"]').trigger('keydown', { key: 'End' })
    expect(wrapper.props('modelValue')).toBe('payg')
    wrapper.unmount()
  })
})
