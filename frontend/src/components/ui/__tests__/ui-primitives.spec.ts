import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import {
  Badge,
  Button,
  Checkbox,
  CommandPalette,
  CopyButton,
  EmptyState,
  ErrorState,
  IconButton,
  Input,
  Modal,
  SegmentedControl,
  Select,
  StatNumber,
  Stepper,
  Switch,
  Table,
  Tabs,
  Toast,
  clearAllUiToasts,
  pushUiToast
} from '../index'

describe('OmniHive UI Primitives', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    clearAllUiToasts()
  })

  afterEach(() => {
    clearAllUiToasts()
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('Button & IconButton prevent double-submit when loading or disabled', async () => {
    const onClick = vi.fn()
    const wrapper = mount(Button, {
      props: { variant: 'primary', loading: true, onClick },
      slots: { default: 'Publish Policy' }
    })

    const button = wrapper.get('button')
    expect(button.attributes('aria-busy')).toBe('true')
    expect(button.attributes('disabled')).toBeDefined()
    await button.trigger('click')
    expect(onClick).not.toHaveBeenCalled()

    await wrapper.setProps({ loading: false })
    await button.trigger('click')
    expect(onClick).toHaveBeenCalledTimes(1)

    const iconWrapper = mount(IconButton, {
      props: { ariaLabel: 'Inspect payload', active: true },
      slots: { default: '{}' }
    })
    expect(iconWrapper.get('button').attributes('aria-label')).toBe('Inspect payload')
    expect(iconWrapper.get('button').attributes('aria-pressed')).toBe('true')
  })

  it('Input wires aria-invalid, error alert, secret reveal, and clear action', async () => {
    const wrapper = mount(Input, {
      props: {
        modelValue: 'oh-sk-secret',
        type: 'password',
        label: 'API Secret',
        revealable: true,
        clearable: true,
        error: 'Invalid token format'
      }
    })

    const input = wrapper.get('input')
    expect(input.attributes('type')).toBe('password')
    expect(input.attributes('aria-invalid')).toBe('true')
    const alertEl = wrapper.get('[role="alert"]')
    expect(alertEl.text()).toBe('Invalid token format')
    expect(input.attributes('aria-describedby')).toBe(alertEl.attributes('id'))

    await wrapper.get('button[aria-label="Reveal secret"]').trigger('click')
    expect(wrapper.get('input').attributes('type')).toBe('text')

    await wrapper.get('button[aria-label="Clear input"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([''])
    expect(wrapper.emitted('clear')).toHaveLength(1)
  })

  it('Select, Checkbox (indeterminate), and Switch preserve typed values and ARIA state', async () => {
    const selectWrapper = mount(Select, {
      props: {
        modelValue: 10,
        label: 'Page Size',
        options: [
          { value: 10, label: '10 rows' },
          { value: 50, label: '50 rows' }
        ]
      }
    })
    await selectWrapper.get('select').setValue('50')
    expect(selectWrapper.emitted('update:modelValue')?.[0]).toEqual([50])

    const checkboxWrapper = mount(Checkbox, {
      props: { modelValue: false, indeterminate: true, label: 'Select pool' }
    })
    expect(checkboxWrapper.get('input').attributes('aria-checked')).toBe('mixed')

    const switchWrapper = mount(Switch, {
      props: { modelValue: false, label: 'Enable ZDR' }
    })
    const switchBtn = switchWrapper.get('[role="switch"]')
    expect(switchBtn.attributes('aria-checked')).toBe('false')
    await switchBtn.trigger('click')
    expect(switchWrapper.emitted('update:modelValue')?.[0]).toEqual([true])
  })

  it('CopyButton shows in-place Copied and Copy failed states without global toast spam', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.assign(navigator, { clipboard: { writeText } })

    const wrapper = mount(CopyButton, {
      props: { value: 'openai/gpt-6.2-sol', resetDelayMs: 1200 }
    })
    expect(wrapper.text()).toContain('Copy')

    await wrapper.get('button').trigger('click')
    await nextTick()
    expect(writeText).toHaveBeenCalledWith('openai/gpt-6.2-sol')
    expect(wrapper.text()).toContain('Copied')

    vi.advanceTimersByTime(1300)
    await nextTick()
    expect(wrapper.text()).toContain('Copy')

    writeText.mockRejectedValueOnce(new Error('Denied'))
    await wrapper.get('button').trigger('click')
    await nextTick()
    expect(wrapper.text()).toContain('Copy failed')
    expect(wrapper.emitted('error')).toHaveLength(1)
  })

  it('StatNumber distinguishes null/unknown (—), explicit zero (0), Free, compact, and settlement status', () => {
    const unknownStat = mount(StatNumber, {
      props: { value: null, unit: 'ms', unknownLabel: 'Insufficient sample' }
    })
    expect(unknownStat.text()).toBe('—')
    expect(unknownStat.text()).not.toContain('0')
    expect(unknownStat.find('[aria-label="Insufficient sample"]').exists()).toBe(true)

    const zeroStat = mount(StatNumber, {
      props: { value: 0, unit: 'errors' }
    })
    expect(zeroStat.text()).toContain('0')
    expect(zeroStat.text()).toContain('errors')

    const freeStat = mount(StatNumber, {
      props: { value: '0.00', freeWhenZero: true, unit: '/ 1M' }
    })
    expect(freeStat.text()).toBe('Free')

    const settledStat = mount(StatNumber, {
      props: {
        value: '24.5000',
        currency: '$',
        unit: 'USD',
        settlementState: 'settled'
      }
    })
    expect(settledStat.text()).toContain('$24.5000')
    expect(settledStat.text()).toContain('Settled')
  })

  it('Table supports ARIA sorting, row selection, skeleton, filtered empty, and error states', async () => {
    const columns = [
      { key: 'slug', label: 'Model', sortable: true },
      { key: 'price', label: 'Input $/1M', mono: true, sortable: true }
    ]
    const rows = [
      { id: 'm1', slug: 'openai/gpt-6.2-sol', price: 2 },
      { id: 'm2', slug: 'deepseek/deepseek-v4.1-flash', price: null }
    ]

    const wrapper = mount(Table, {
      props: {
        columns,
        rows,
        sortBy: 'price',
        sortOrder: 'asc',
        selectable: true,
        selectedKeys: []
      }
    })

    const headers = wrapper.findAll('th')
    expect(headers[1].attributes('aria-sort')).toBe('none')
    expect(headers[2].attributes('aria-sort')).toBe('ascending')
    expect(wrapper.text()).toContain('—')

    await headers[2].get('button').trigger('click')
    expect(wrapper.emitted('sort')?.[0]).toEqual([{ key: 'price', order: 'desc' }])

    await wrapper.setProps({ rows: [], filtered: true })
    expect(wrapper.findComponent(EmptyState).exists()).toBe(true)
    await wrapper.findComponent(EmptyState).get('button').trigger('click')
    expect(wrapper.emitted('clearFilters')).toHaveLength(1)

    await wrapper.setProps({
      errorTitle: 'Gateway timeout',
      errorRequestId: 'req_test_99'
    })
    expect(wrapper.findComponent(ErrorState).exists()).toBe(true)
    expect(wrapper.text()).toContain('req_test_99')
  })

  it('Tabs and SegmentedControl support keyboard arrow navigation skipping disabled items', async () => {
    const tabsWrapper = mount(Tabs, {
      props: {
        modelValue: 'all',
        items: [
          { value: 'all', label: 'All', count: 12 },
          { value: 'locked', label: 'Locked', disabled: true },
          { value: 'text', label: 'Text', count: 9 }
        ]
      }
    })

    const tabButtons = tabsWrapper.findAll('[role="tab"]')
    await tabButtons[0].trigger('keydown', { key: 'ArrowRight' })
    expect(tabsWrapper.emitted('update:modelValue')?.[0]).toEqual(['text'])

    const segWrapper = mount(SegmentedControl, {
      props: {
        modelValue: 'table',
        options: [
          { value: 'table', label: 'Table' },
          { value: 'list', label: 'List' }
        ]
      }
    })
    await segWrapper.findAll('[role="radio"]')[0].trigger('keydown', { key: 'ArrowRight' })
    expect(segWrapper.emitted('update:modelValue')?.[0]).toEqual(['list'])
  })

  it('Modal & CommandPalette handle Escape close, filtering, and keyboard selection', async () => {
    const modalWrapper = mount(Modal, {
      props: {
        open: true,
        title: 'Confirm Policy',
        teleportDisabled: true
      },
      slots: { default: '<button type="button">Inner Action</button>' },
      attachTo: document.body
    })

    expect(modalWrapper.get('[role="dialog"]').text()).toContain('Confirm Policy')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await nextTick()
    expect(modalWrapper.emitted('update:open')?.[0]).toEqual([false])
    modalWrapper.unmount()

    const paletteWrapper = mount(CommandPalette, {
      props: {
        open: true,
        teleportDisabled: true,
        groups: [
          {
            id: 'models',
            label: 'Models',
            items: [
              { id: '1', label: 'openai/gpt-6.2-sol', keywords: ['openai'] },
              { id: '2', label: 'anthropic/claude-opus-5-5', keywords: ['claude'] }
            ]
          }
        ]
      },
      attachTo: document.body
    })

    const searchInput = paletteWrapper.get('input[role="combobox"]')
    await searchInput.setValue('claude')
    await nextTick()
    await searchInput.trigger('keydown', { key: 'Enter' })
    expect(paletteWrapper.emitted('select')?.[0]?.[0]).toMatchObject({
      id: '2',
      label: 'anthropic/claude-opus-5-5'
    })
    paletteWrapper.unmount()
  })

  it('Stepper & Toast expose accessible step state and assertive error notifications', async () => {
    const stepperWrapper = mount(Stepper, {
      props: {
        currentStepId: 'verify',
        steps: [
          { id: 'provider', title: 'Provider', status: 'complete' },
          { id: 'verify', title: 'Verify', status: 'current' },
          { id: 'enable', title: 'Enable', status: 'upcoming' }
        ]
      }
    })

    const stepButtons = stepperWrapper.findAll('button')
    expect(stepButtons[1].attributes('aria-current')).toBe('step')
    await stepButtons[2].trigger('click')
    expect(stepperWrapper.emitted('update:currentStepId')).toBeUndefined()
    await stepButtons[0].trigger('click')
    expect(stepperWrapper.emitted('update:currentStepId')?.[0]).toEqual(['provider'])

    const badgeWrapper = mount(Badge, {
      props: { tone: 'unknown', mono: true },
      slots: { default: 'Unverified' }
    })
    expect(badgeWrapper.text()).toContain('Unverified')

    pushUiToast({
      title: 'Upstream 401',
      tone: 'danger',
      requestId: 'req_toast_01'
    })
    const toastWrapper = mount(Toast, { props: { teleportDisabled: true } })
    expect(toastWrapper.get('[role="alert"]').text()).toContain('Upstream 401')
    expect(toastWrapper.text()).toContain('req_toast_01')
  })
})
