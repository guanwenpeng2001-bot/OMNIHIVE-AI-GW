import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

import Select from '../Select.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const originalInnerWidth = window.innerWidth
let unmountWrapper: (() => void) | undefined

const setViewportWidth = (width: number) => {
  Object.defineProperty(window, 'innerWidth', {
    configurable: true,
    value: width,
  })
}

const mockTriggerRect = (left: number, width: number) => {
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
    x: left,
    y: 20,
    top: 20,
    right: left + width,
    bottom: 60,
    left,
    width,
    height: 40,
    toJSON: () => ({}),
  })
}

const openSelect = async () => {
  const wrapper = mount(Select, {
    props: {
      modelValue: null,
      options: [
        {
          value: 'example',
          label: 'very-long-unbroken-option-value-that-must-not-overflow',
        },
      ],
    },
  })
  unmountWrapper = () => wrapper.unmount()

  await wrapper.get('button').trigger('click')
  await nextTick()

  return document.body.querySelector<HTMLElement>('.select-dropdown-portal')
}

afterEach(() => {
  unmountWrapper?.()
  unmountWrapper = undefined
  document.body.innerHTML = ''
  setViewportWidth(originalInnerWidth)
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('Select dropdown viewport constraints', () => {
  it('preserves the existing 200px minimum width when space is available', async () => {
    setViewportWidth(1024)
    mockTriggerRect(20, 80)

    const dropdown = await openSelect()

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('20px')
    expect(dropdown?.style.minWidth).toBe('200px')
    expect(dropdown?.style.maxWidth).toBe('996px')
  })

  it('shrinks the minimum width to fit near the right viewport edge', async () => {
    setViewportWidth(320)
    mockTriggerRect(220, 80)

    const dropdown = await openSelect()

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('220px')
    expect(dropdown?.style.minWidth).toBe('92px')
    expect(dropdown?.style.maxWidth).toBe('92px')
  })

  it('clamps a trigger left of the viewport to the safe padding', async () => {
    setViewportWidth(320)
    mockTriggerRect(-20, 80)

    const dropdown = await openSelect()

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('8px')
    expect(dropdown?.style.minWidth).toBe('200px')
    expect(dropdown?.style.maxWidth).toBe('304px')
  })

  it('clamps an offscreen-right trigger position to the viewport boundary', async () => {
    setViewportWidth(320)
    mockTriggerRect(400, 80)

    const dropdown = await openSelect()

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('312px')
    expect(dropdown?.style.minWidth).toBe('0px')
    expect(dropdown?.style.maxWidth).toBe('0px')
  })
})

describe('Select remote search', () => {
  const mountRemoteSelect = (props: Record<string, unknown> = {}) => {
    const wrapper = mount(Select, {
      props: {
        modelValue: null,
        remote: true,
        options: [
          { value: 'alpha', label: 'Alpha account' },
          { value: 'beta', label: 'Beta account' },
        ],
        ...props,
      },
    })
    unmountWrapper = () => wrapper.unmount()
    return wrapper
  }

  const openDropdown = async () => {
    const dropdown = document.body.querySelector<HTMLElement>('.select-dropdown-portal')
    expect(dropdown).not.toBeNull()
    return dropdown as HTMLElement
  }

  const typeSearchQuery = async (query: string) => {
    const dropdown = await openDropdown()
    const input = dropdown.querySelector<HTMLInputElement>('.select-search-input')
    expect(input).not.toBeNull()
    input!.value = query
    input!.dispatchEvent(new Event('input'))
    await nextTick()
  }

  it('emits debounced search events and skips local filtering in remote mode', async () => {
    vi.useFakeTimers()
    const wrapper = mountRemoteSelect()
    await wrapper.get('button').trigger('click')
    await nextTick()

    await typeSearchQuery('zzz')

    // 防抖窗口内不触发。
    expect(wrapper.emitted('search')).toBeUndefined()
    await vi.advanceTimersByTimeAsync(300)

    expect(wrapper.emitted('search')).toEqual([['zzz']])
    // 远程模式不做本地过滤：无命中的 query 下选项仍完整展示（由父组件更新 options）。
    const dropdown = await openDropdown()
    const labels = [...dropdown.querySelectorAll('.select-option-label')].map((el) => el.textContent)
    expect(labels).toContain('Alpha account')
    expect(labels).toContain('Beta account')
  })

  it('does not emit search when the dropdown closes and the query resets', async () => {
    vi.useFakeTimers()
    const wrapper = mountRemoteSelect()
    await wrapper.get('button').trigger('click')
    await nextTick()

    await typeSearchQuery('hidden')

    // 关闭下拉：排队中的防抖定时器应被取消，也不应因 query 重置而尾随 emit。
    await wrapper.get('button').trigger('click')
    await nextTick()
    await vi.advanceTimersByTimeAsync(300)

    expect(wrapper.emitted('search')).toBeUndefined()
  })

  it('shows the loading text instead of empty text while loading with no options', async () => {
    const wrapper = mountRemoteSelect({ options: [], loading: true })
    await wrapper.get('button').trigger('click')
    await nextTick()

    const dropdown = await openDropdown()
    expect(dropdown.querySelector('.select-empty')?.textContent).toContain('common.loading')
  })

  it('keeps local filtering and emits nothing when remote is not set', async () => {
    vi.useFakeTimers()
    const wrapper = mount(Select, {
      props: {
        modelValue: null,
        searchable: true,
        options: [
          { value: 'alpha', label: 'Alpha account' },
          { value: 'beta', label: 'Beta account' },
        ],
      },
    })
    unmountWrapper = () => wrapper.unmount()
    await wrapper.get('button').trigger('click')
    await nextTick()

    await typeSearchQuery('alpha')
    await vi.advanceTimersByTimeAsync(300)

    expect(wrapper.emitted('search')).toBeUndefined()
    const dropdown = await openDropdown()
    const labels = [...dropdown.querySelectorAll('.select-option-label')].map((el) => el.textContent)
    expect(labels).toEqual(['Alpha account'])
  })
})


describe('Select accessibility', () => {
  it('names the search input after its field', async () => {
    const wrapper = mount(Select, { props: { modelValue: null, label: 'Severity', searchable: true, options: [] } })
    unmountWrapper = () => wrapper.unmount()
    await wrapper.get('button').trigger('click')
    expect(document.querySelector('[role="combobox"]')?.getAttribute('aria-label')).toBe('Severity: common.searchPlaceholder')
  })

  it('associates the trigger with a named listbox and consumes Escape', async () => {
    const wrapper = mount(Select, { attachTo: document.body, props: { modelValue: null, label: 'Account platform', options: [{ value: 'openai', label: 'OpenAI' }] } })
    const trigger = wrapper.get('button')
    expect(trigger.attributes('aria-label')).toBe('Account platform: common.selectOption')
    expect(trigger.attributes('aria-haspopup')).toBe('listbox')
    await trigger.trigger('click')
    await nextTick()
    const listbox = document.getElementById(trigger.attributes('aria-controls'))!
    expect(listbox.getAttribute('role')).toBe('listbox')
    expect(listbox.getAttribute('aria-label')).toBe('Account platform')
    const escape = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
    listbox.dispatchEvent(escape)
    await nextTick()
    expect(escape.defaultPrevented).toBe(true)
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(trigger.element)
    wrapper.unmount()
  })
})


describe('Select accessible field and value', () => {
  it('keeps keyboard selection valid after filtering and ignores IME confirmation', async () => {
    const wrapper = mount(Select, { props: {
      modelValue: 9, searchable: true,
      options: Array.from({ length: 10 }, (_, i) => ({ value: i, label: `model-${i}` }))
    } })
    unmountWrapper = () => wrapper.unmount()
    await wrapper.get('button').trigger('click')
    const input = document.querySelector<HTMLInputElement>('[role="combobox"]')!
    input.value = 'model-2'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()
    expect(document.getElementById(input.getAttribute('aria-activedescendant')!)?.textContent).toContain('model-2')
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, isComposing: true }))
    expect(wrapper.emitted('change')).toBeUndefined()
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    expect(wrapper.emitted('update:modelValue')).toEqual([[2]])
    expect(wrapper.emitted('search')).toBeUndefined()
  })

  it('resets replaced remote results without emitting another search', async () => {
    const wrapper = mount(Select, { props: {
      modelValue: 'b', remote: true,
      options: [{ value: 'a', label: 'Alpha' }, { value: 'b', label: 'Beta' }]
    } })
    unmountWrapper = () => wrapper.unmount()
    await wrapper.get('button').trigger('click')
    const input = document.querySelector<HTMLInputElement>('[role="combobox"]')!
    await wrapper.setProps({ options: [{ value: 'c', label: 'Charlie', disabled: true }, { value: 'd', label: 'Delta' }] })
    expect(document.getElementById(input.getAttribute('aria-activedescendant')!)?.textContent).toContain('Delta')
    await wrapper.setProps({ options: [] })
    expect(input.hasAttribute('aria-activedescendant')).toBe(false)
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    expect(wrapper.emitted('change')).toBeUndefined()
    expect(wrapper.emitted('search')).toBeUndefined()
  })

  it('announces an explicit field and its updated value', async () => {
    const wrapper = mount(Select, { props: { ariaLabel: 'Platform', modelValue: 'a', options: [{ value: 'a', label: 'Alpha' }, { value: 'b', label: 'Beta' }] } })
    unmountWrapper = () => wrapper.unmount()
    expect(wrapper.get('button').attributes('aria-label')).toBe('Platform: Alpha')
    await wrapper.setProps({ modelValue: 'b' })
    expect(wrapper.get('button').attributes('aria-label')).toBe('Platform: Beta')
  })
  it('combines an external field label with a referenced current value', async () => {
    document.body.innerHTML = '<span id="platform-label">Platform</span>'
    const wrapper = mount(Select, { attachTo: document.body, props: { clearable: true, ariaLabelledby: 'platform-label', modelValue: 'a', options: [{ value: 'a', label: 'Alpha' }] } })
    unmountWrapper = () => wrapper.unmount()
    const references = wrapper.get('button').attributes('aria-labelledby').split(' ')
    expect(references.map(id => document.getElementById(id)?.textContent?.trim())).toEqual(['Platform', 'Alpha'])
    const clearReferences = wrapper.get('.select-clear').attributes('aria-labelledby').split(' ')
    expect(clearReferences.map(id => document.getElementById(id)?.textContent?.trim())).toEqual(['common.clear', 'Platform'])
    await wrapper.get('button').trigger('click')
    expect(document.querySelector('[role="listbox"]')?.getAttribute('aria-labelledby')).toBe('platform-label')
  })
})


describe('Select keyboard actions and validation', () => {
  it('exposes a separate named native clear button and restores trigger focus', async () => {
    const wrapper = mount(Select, { attachTo: document.body, props: {
      modelValue: 'openai', label: 'Platform', clearable: true,
      options: [{ value: 'openai', label: 'OpenAI' }], error: true, ariaDescribedby: 'platform-error'
    } })
    unmountWrapper = () => wrapper.unmount()
    const trigger = wrapper.get<HTMLButtonElement>('.select-trigger')
    const clear = wrapper.get<HTMLButtonElement>('button.select-clear')
    expect(clear.attributes('aria-label')).toBe('common.clear: Platform')
    expect(clear.element.tabIndex).toBe(0)
    expect(trigger.find('button, [role="button"]').exists()).toBe(false)
    expect(trigger.attributes('aria-invalid')).toBe('true')
    expect(trigger.attributes('aria-describedby')).toBe('platform-error')
    clear.element.focus()
    await clear.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[null]])
    expect(document.activeElement).toBe(trigger.element)
    await wrapper.setProps({ disabled: true, error: false })
    expect(wrapper.find('.select-clear').exists()).toBe(false)
    expect(trigger.attributes('aria-invalid')).toBeUndefined()
  })

  it.each([false, true])('leaves native Tab navigation available (shift=%s)', async (shiftKey) => {
    const wrapper = mount(Select, { attachTo: document.body, props: {
      modelValue: null, label: 'Platform', options: [{ value: 'openai', label: 'OpenAI' }]
    } })
    unmountWrapper = () => wrapper.unmount()
    const trigger = wrapper.get('button')
    await trigger.trigger('keydown', { key: 'ArrowDown' })
    await nextTick()
    const listbox = document.querySelector<HTMLElement>('[role="listbox"]')!
    expect(document.activeElement).toBe(listbox)
    const tab = new KeyboardEvent('keydown', { key: 'Tab', shiftKey, bubbles: true, cancelable: true })
    listbox.dispatchEvent(tab)
    await nextTick()
    expect(tab.defaultPrevented).toBe(false)
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(trigger.element)
  })

  it('confirms a non-editable listbox with Space but preserves spaces in search', async () => {
    const wrapper = mount(Select, { props: {
      modelValue: null, options: [{ value: 'openai', label: 'OpenAI' }], searchable: false
    } })
    unmountWrapper = () => wrapper.unmount()
    await wrapper.get('button').trigger('click')
    document.querySelector('[role="listbox"]')!.dispatchEvent(new KeyboardEvent('keydown', { key: ' ', bubbles: true }))
    await nextTick()
    expect(wrapper.emitted('update:modelValue')).toEqual([['openai']])
    await wrapper.setProps({ searchable: true })
    await wrapper.get('button').trigger('click')
    const space = new KeyboardEvent('keydown', { key: ' ', bubbles: true, cancelable: true })
    document.querySelector('[role="combobox"]')!.dispatchEvent(space)
    expect(space.defaultPrevented).toBe(false)
    expect(wrapper.emitted('update:modelValue')).toHaveLength(1)
  })
})
