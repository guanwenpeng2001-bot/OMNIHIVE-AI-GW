import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { h, nextTick } from 'vue'
import BaseDialog from '../BaseDialog.vue'
import Select from '../Select.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

describe('BaseDialog', () => {
  afterEach(() => {
    document.body.innerHTML = ''
    document.body.classList.remove('modal-open')
  })

  it('resets body scroll position when reopened', async () => {
    const wrapper = mount(BaseDialog, {
      attachTo: document.body,
      props: { show: false, title: 'Details' },
      slots: { default: '<div style="height: 2000px">content</div>' },
      global: { stubs: { Icon: true } }
    })

    await wrapper.setProps({ show: true })
    await nextTick()
    const body = document.body.querySelector<HTMLElement>('.modal-body')
    expect(body).not.toBeNull()
    body!.scrollTop = 480

    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await nextTick()

    expect(document.body.querySelector<HTMLElement>('.modal-body')?.scrollTop).toBe(0)
    wrapper.unmount()
  })
  it('keeps background inert and closes only the top dialog, then restores focus', async () => {
    const trigger = document.createElement('button')
    document.body.append(trigger)
    trigger.focus()
    const parent = mount(BaseDialog, { attachTo: document.body, props: { show: true, title: 'Parent' }, slots: { default: '<button id="inner-trigger">Open child</button>' } })
    await nextTick()
    expect(trigger.inert).toBe(true)
    expect(document.activeElement?.textContent).toBe('Parent')
    const innerTrigger = document.querySelector<HTMLButtonElement>('#inner-trigger')!
    innerTrigger.focus()
    const child = mount(BaseDialog, { attachTo: document.body, props: { show: true, title: 'Child' } })
    await nextTick()
    const titles = Array.from(document.querySelectorAll('h3')).map(element => element.id)
    expect(new Set(titles).size).toBe(2)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    expect(child.emitted('close')).toHaveLength(1)
    expect(parent.emitted('close')).toBeUndefined()
    await child.setProps({ show: false })
    expect(document.activeElement).toBe(innerTrigger)
    expect(trigger.inert).toBe(true)
    child.unmount()
    await parent.setProps({ show: false })
    expect(document.activeElement).toBe(trigger)
    expect(trigger.inert).toBeFalsy()
    parent.unmount()
  })

  it('wraps Tab at the last action and Shift Tab from the title', async () => {
    const dialog = mount(BaseDialog, { attachTo: document.body, props: { show: true, title: 'Focus', showCloseButton: false }, slots: { default: '<button id="first-action">First</button><button id="last-action">Last</button>' } })
    await nextTick()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true, cancelable: true }))
    expect(document.activeElement?.id).toBe('last-action')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true }))
    expect(document.activeElement?.id).toBe('first-action')
    dialog.unmount()
  })

  it('closes a searchable Select before its dialog and keeps Tab inside the dialog', async () => {
    const dialog = mount(BaseDialog, {
      attachTo: document.body,
      props: { show: true, title: 'Settings', showCloseButton: false },
      slots: {
        default: () => h(Select, { modelValue: null, label: 'Severity', searchable: true, options: [{ value: 'critical', label: 'Critical' }] }),
        footer: '<button id="save-settings">Save</button>'
      }
    })
    await nextTick()
    const trigger = document.querySelector<HTMLButtonElement>('.select-trigger')!
    trigger.click()
    await nextTick()
    await nextTick()
    expect(document.activeElement?.getAttribute('role')).toBe('combobox')
    document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    await nextTick()
    expect(dialog.emitted('close')).toBeUndefined()
    expect(document.activeElement).toBe(trigger)

    trigger.click()
    await nextTick()
    await nextTick()
    document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true }))
    await nextTick()
    expect(document.activeElement).toBe(trigger)
    trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true, cancelable: true }))
    expect(document.activeElement?.id).toBe('save-settings')
    document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true }))
    expect(document.activeElement).toBe(trigger)
    trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    expect(dialog.emitted('close')).toHaveLength(1)
    dialog.unmount()
  })

  it('does not suspend the focus trap for a non-Select portal', async () => {
    const dialog = mount(BaseDialog, { attachTo: document.body, props: { show: true, title: 'Settings', showCloseButton: false }, slots: { default: '<button id="only-action">Save</button>' } })
    await nextTick()
    const portal = document.createElement('div')
    portal.dataset.dialogOwner = document.querySelector<HTMLElement>('[data-dialog-id]')!.dataset.dialogId
    document.body.append(portal)
    document.querySelector<HTMLButtonElement>('#only-action')!.focus()
    const tab = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true })
    document.activeElement!.dispatchEvent(tab)
    expect(tab.defaultPrevented).toBe(true)
    expect(document.activeElement?.id).toBe('only-action')
    dialog.unmount()
  })

  it('does not register an immediately unmounted dialog after its pending tick', async () => {
    const background = document.createElement('button')
    document.body.append(background)
    const dialog = mount(BaseDialog, { props: { show: true, title: 'Transient' } })
    dialog.unmount()
    await nextTick()
    expect(document.body.classList.contains('modal-open')).toBe(false)
    expect(background.inert).toBeFalsy()
  })

  it('keeps the upper dialog focused and scroll locked when a lower dialog unmounts', async () => {
    const parent = mount(BaseDialog, { props: { show: true, title: 'Parent' } })
    await nextTick()
    const child = mount(BaseDialog, { props: { show: true, title: 'Child' } })
    await nextTick()
    const focused = document.activeElement
    parent.unmount()
    expect(document.activeElement).toBe(focused)
    expect(document.body.classList.contains('modal-open')).toBe(true)
    child.unmount()
    expect(document.body.classList.contains('modal-open')).toBe(false)
  })

  it('preserves pre-existing inert state after the last dialog closes', async () => {
    const background = document.createElement('button')
    background.inert = true
    document.body.append(background)
    const dialog = mount(BaseDialog, { props: { show: true, title: 'Details' } })
    await nextTick()
    dialog.unmount()
    expect(background.inert).toBe(true)
  })

})
