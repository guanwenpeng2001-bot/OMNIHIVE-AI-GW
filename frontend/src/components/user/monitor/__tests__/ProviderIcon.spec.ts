import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { CONCRETE_PLATFORM_OPTIONS } from '@/constants/platforms'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import ProviderIcon from '../ProviderIcon.vue'

describe('ProviderIcon', () => {
  it.each(CONCRETE_PLATFORM_OPTIONS)('delegates $value to the real PlatformIcon', ({ value }) => {
    const wrapper = mount(ProviderIcon, { props: { provider: value } })
    expect(wrapper.getComponent(PlatformIcon).props('platform')).toBe(value)
    expect(wrapper.get('svg').element.innerHTML).toBe(mount(PlatformIcon, { props: { platform: value } }).get('svg').element.innerHTML)
    expect((wrapper.get('svg').element as SVGElement).style.width).toBe('20px')
    expect((wrapper.get('svg').element as SVGElement).style.height).toBe('20px')
    expect(wrapper.get('svg, span').attributes('aria-hidden')).toBe('true')
    expect(wrapper.get('svg').attributes('focusable')).toBe('false')
  })

  it.each([7, 19, 27.5, 64])('passes numeric size %s through without quantizing', size => {
    const wrapper = mount(ProviderIcon, { props: { provider: 'anthropic', size } })
    expect((wrapper.get('svg').element as SVGElement).style.width).toBe(`${size}px`)
    expect((wrapper.get('svg').element as SVGElement).style.height).toBe(`${size}px`)
    expect(wrapper.get('svg').attributes('viewBox')).toBe('0 0 16 16')
  })

  it.each([['custom', 'C'], ['', '?'], ['OpenAI', 'O']])('preserves unknown provider %s fallback without aliases', (provider, text) => {
    const wrapper = mount(ProviderIcon, { props: { provider, size: 31 } })
    expect(wrapper.text()).toBe(text)
    expect(wrapper.findComponent(PlatformIcon).exists()).toBe(false)
    expect((wrapper.element as HTMLElement).style.width).toBe('31px')
    expect((wrapper.element as HTMLElement).style.height).toBe('31px')
    expect(wrapper.get('svg, span').attributes('aria-hidden')).toBe('true')
  })

  it('delegates composite to the shared functional icon', () => {
    const wrapper = mount(ProviderIcon, { props: { provider: 'composite' } })
    expect(wrapper.getComponent(PlatformIcon).props('platform')).toBe('composite')
    expect(wrapper.findAll('circle')).toHaveLength(3)
  })
})
