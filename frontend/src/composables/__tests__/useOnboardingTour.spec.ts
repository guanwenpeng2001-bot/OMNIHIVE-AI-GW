import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, nextTick, reactive } from 'vue'
import { mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import type { DriveStep } from 'driver.js'
import { useOnboardingTour } from '../useOnboardingTour'
import { useOnboardingStore } from '@/stores/onboarding'
import AppLayout from '@/components/layout/AppLayout.vue'

const fixtures = vi.hoisted(() => ({ steps: [] as DriveStep[] }))
const route = reactive({ fullPath: '/admin/dashboard' })
const app = reactive({ sidebarCollapsed: false })
const auth = reactive({ user: { id: 1, role: 'admin' }, isSimpleMode: false, sessionRevision: 'login-1' })
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/stores', () => ({ useAppStore: () => app }))
vi.mock('vue-router', () => ({ useRoute: () => route }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/components/Guide/steps', () => ({ getAdminSteps: () => fixtures.steps, getUserSteps: () => fixtures.steps }))
vi.mock('@/components/layout/AppSidebar.vue', () => ({ default: { template: '<aside />' } }))
vi.mock('@/components/layout/AppHeader.vue', () => ({ default: { template: '<header />' } }))
let wrapper: VueWrapper | undefined
let tour: ReturnType<typeof useOnboardingTour>
const store = () => useOnboardingStore()
const noOverlay = () => {
  expect(document.querySelector('.driver-overlay')).toBeNull()
  expect(document.querySelector('.driver-popover')).toBeNull()
  expect(document.body.classList.contains('driver-active')).toBe(false)
  expect(store().getDriverInstance()).toBeNull()
}
const host = (autoStart = false) => {
  wrapper = mount(defineComponent({ setup() { tour = useOnboardingTour({ autoStart }); return () => null } }), { attachTo: document.body })
}
const target = () => {
  const element = document.createElement('button')
  element.id = 'target'
  document.body.append(element)
  return element
}
const advance = async (ms = 200) => { await vi.advanceTimersByTimeAsync(ms); await nextTick() }
beforeEach(() => {
  vi.useFakeTimers()
  setActivePinia(createPinia())
  localStorage.clear()
  sessionStorage.clear()
  auth.user.id = 1
  auth.sessionRevision = 'login-1'
  fixtures.steps = [{ popover: { title: 'Welcome' } }, { element: '#target', popover: { title: 'Target', showButtons: ['next', 'previous'] } }]
  route.fullPath = '/admin/dashboard'
  app.sidebarCollapsed = false
  vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) {
    return { x: 0, y: 0, top: 0, left: 0, right: 100, bottom: 40, width: 100, height: this instanceof HTMLElement && this.hidden ? 0 : 40, toJSON: () => ({}) }
  })
  Element.prototype.scrollIntoView = vi.fn()
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  document.body.innerHTML = ''
  vi.clearAllTimers()
  vi.restoreAllMocks()
  vi.useRealTimers()
})
describe('onboarding cancellable lifecycle (real driver)', () => {
  it('remembers Esc cancellation across remount without recording completion', async () => {
    host(true)
    await advance(1100)
    expect(document.querySelector('.driver-overlay')).not.toBeNull()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    noOverlay()
    expect(tour.hasSeen()).toBe(false)
    wrapper!.unmount()
    host(true)
    await advance(1100)
    noOverlay()
    expect(tour.hasSeen()).toBe(false)
  })
  it.each(['Escape', 'Exit'])('keeps %s dismissal across consecutive AppLayout navigations and allows menu replay', async exit => {
    wrapper = mount(AppLayout, { attachTo: document.body })
    await advance(1100)
    expect(store().isDriverActive()).toBe(true)
    if (exit === 'Escape') document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    else document.querySelector<HTMLButtonElement>('.driver-popover-close-btn')!.click()
    for (const path of ['/admin/accounts', '/admin/groups', '/admin/dashboard']) {
      route.fullPath = path
      wrapper.unmount()
      wrapper = mount(AppLayout, { attachTo: document.body })
      await advance(1100)
      noOverlay()
      expect(localStorage.length).toBe(0)
    }
    store().replay()
    await nextTick()
    expect(store().isDriverActive()).toBe(true)
  })
  it.each(['user', 'login'])('does not suppress auto-start for a different %s', async scope => {
    host(true)
    await advance(1100)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    wrapper!.unmount()
    if (scope === 'user') auth.user.id = 2
    else auth.sessionRevision = 'login-2'
    host(true)
    await advance(1100)
    expect(store().isDriverActive()).toBe(true)
  })
  it('exposes Exit on a step hiding close and allows replay', async () => {
    target(); host()
    await tour.startTour(1)
    const close = document.querySelector<HTMLButtonElement>('.driver-popover-close-btn')!
    expect(close.textContent).toBe('onboarding.navigation.exit')
    expect(close.style.display).not.toBe('none')
    close.click()
    noOverlay()
    expect(tour.hasSeen()).toBe(false)
    tour.replayTour()
    await nextTick()
    expect(store().isDriverActive()).toBe(true)
  })
  it('marks completion only after last-step confirmation', async () => {
    target(); host()
    await tour.startTour(1)
    document.querySelector<HTMLButtonElement>('.driver-popover-next-btn')!.click()
    noOverlay()
    expect(tour.hasSeen()).toBe(true)
    wrapper!.unmount()
    host(true)
    await advance(1100)
    noOverlay()
    tour.replayTour()
    await nextTick()
    expect(store().isDriverActive()).toBe(true)
  })
  it('does not start with a missing initial target', async () => {
    host()
    const starting = tour.startTour(1)
    await advance(8200)
    await starting
    noOverlay()
    expect(tour.hasSeen()).toBe(false)
  })
  it('exits when an asynchronous next target never arrives', async () => {
    host()
    await tour.startTour()
    await tour.nextStep(0)
    await advance(8200)
    noOverlay()
    expect(tour.hasSeen()).toBe(false)
  })
  it('re-highlights an asynchronous target once it arrives', async () => {
    host()
    await tour.startTour()
    await tour.nextStep(0)
    const element = target()
    await advance()
    expect(store().getDriverInstance()?.getActiveElement()).toBe(element)
  })
  it('never records completion while the last target is missing', async () => {
    host()
    await tour.startTour()
    await tour.nextStep(0)
    document.querySelector<HTMLButtonElement>('.driver-popover-next-btn')!.click()
    noOverlay()
    expect(tour.hasSeen()).toBe(false)
  })
  it('cleans up a detached Element target without leaving a fallback overlay', async () => {
    fixtures.steps[1].element = document.createElement('button')
    host()
    await tour.startTour(1)
    await nextTick()
    await advance()
    noOverlay()
    expect(tour.hasSeen()).toBe(false)
  })
  it.each(['removed', 'hidden'])('exits when the target is %s', async mode => {
    const element = target(); host()
    await tour.startTour(1)
    if (mode === 'removed') element.remove()
    else element.hidden = true
    await advance()
    noOverlay()
    expect(tour.hasSeen()).toBe(false)
  })
  it('cancels initial polling on unmount without resurrecting the driver', async () => {
    host()
    const starting = tour.startTour(1)
    await nextTick()
    wrapper!.unmount()
    target()
    await starting
    await advance(8500)
    noOverlay()
  })
  it('cancels delayed advance across replay and restores interactive tabindex', async () => {
    fixtures.steps[0] = { element: '#target', popover: { title: 'Click', showButtons: ['close'] } }
    const element = document.createElement('div')
    element.id = 'target'
    document.body.append(element)
    host()
    await tour.startTour()
    expect(element.getAttribute('tabindex')).toBe('0')
    const advancing = tour.nextStep(500)
    element.click()
    tour.cancelTour()
    expect(element.hasAttribute('tabindex')).toBe(false)
    await tour.startTour()
    await advancing
    await advance(1100)
    expect(store().getDriverInstance()?.getActiveIndex()).toBe(0)
  })
  it('cancels pending target waits without affecting replay', async () => {
    host()
    await tour.startTour()
    await tour.nextStep(0)
    tour.cancelTour()
    await tour.startTour()
    target()
    await advance(8500)
    expect(store().getDriverInstance()?.getActiveIndex()).toBe(0)
  })
  it('cleans overlays and keyboard listeners on unmount', async () => {
    host()
    await tour.startTour()
    wrapper!.unmount()
    noOverlay()
    const event = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true })
    document.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(false)
    await advance(8500)
    noOverlay()
  })
  it.each(['route', 'sidebar'])('AppLayout cancels on %s changes and retains replay', async change => {
    wrapper = mount(AppLayout, { attachTo: document.body })
    await advance(1100)
    expect(store().isDriverActive()).toBe(true)
    if (change === 'route') route.fullPath = '/admin/groups'
    else app.sidebarCollapsed = true
    noOverlay()
    store().replay()
    await nextTick()
    expect(store().isDriverActive()).toBe(true)
    wrapper.unmount()
    noOverlay()
    store().replay()
    await nextTick()
    noOverlay()
  })
})
