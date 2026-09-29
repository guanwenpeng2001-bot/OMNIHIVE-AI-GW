import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick, reactive } from 'vue'
import CustomPageView from '../CustomPageView.vue'

const { appStore } = vi.hoisted(() => ({
  appStore: {
    publicSettingsLoaded: true,
    cachedPublicSettings: { custom_menu_items: [{ id: 'docs', url: 'https://example.com/docs' }] },
  },
}))

vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))
const routeState = reactive({ params: { id: 'docs' } })
vi.mock('vue-router', () => ({ useRoute: () => routeState }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }) }))
vi.mock('@/stores', () => ({ useAppStore: () => appStore }))
const authState = reactive({ isAdmin: false, user: { id: 7, role: 'user' }, token: 'test-token' })
vi.mock('@/stores/auth', () => ({ useAuthStore: () => authState }))
vi.mock('@/stores/adminSettings', () => ({ useAdminSettingsStore: () => ({ customMenuItems: [] }) }))
vi.mock('@/api/client', () => ({ buildApiUrl: (path: string) => `/api/v1${path}` }))

let notifyResize: () => void
const wrappers: ReturnType<typeof mount>[] = []

function mountPage() {
  const wrapper = mount(CustomPageView, {
    global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } },
  })
  wrappers.push(wrapper)
  return wrapper
}

function mountEmbed() {
  const wrapper = mountPage()
  const shell = wrapper.get('.custom-embed-shell').element
  const button = wrapper.get<HTMLAnchorElement>('.custom-open-fab').element
  const size = { width: 800, height: 600 }
  let capturedPointer: number | null = null
  Object.defineProperties(shell, {
    clientWidth: { get: () => size.width }, clientHeight: { get: () => size.height },
  })
  Object.defineProperties(button, {
    offsetWidth: { value: 100 }, offsetHeight: { value: 32 },
    offsetLeft: { get: () => Number.parseFloat(button.style.left || '688') },
    offsetTop: { get: () => Number.parseFloat(button.style.top || '12') },
    setPointerCapture: { value: vi.fn((id: number) => { capturedPointer = id }) },
    hasPointerCapture: { value: (id: number) => capturedPointer === id },
    releasePointerCapture: { value: vi.fn(() => { capturedPointer = null }) },
  })
  return { wrapper, button, size }
}

async function pointer(button: HTMLElement, type: string, x: number, y: number, extra = {}) {
  const event = new MouseEvent(type, { clientX: x, clientY: y, bubbles: true, cancelable: true, ...extra })
  Object.defineProperties(event, {
    pointerId: { value: 1 }, isPrimary: { value: true },
  })
  button.dispatchEvent(event)
  await nextTick()
}

function click(button: HTMLElement, detail = 1) {
  const event = new MouseEvent('click', { bubbles: true, cancelable: true, detail })
  button.dispatchEvent(event)
  return event
}

describe('custom page open button', () => {
  beforeEach(() => {
    routeState.params.id = 'docs'
    appStore.cachedPublicSettings.custom_menu_items = [{ id: 'docs', url: 'https://example.com/docs' }]
    vi.stubGlobal('ResizeObserver', class {
      constructor(callback: () => void) { notifyResize = callback }
      observe() {}
      disconnect() {}
    })
  })

  afterEach(() => {
    wrappers.splice(0).forEach(wrapper => wrapper.unmount())
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it.each([undefined, false, true])('honors the per-menu hide button setting %s while keeping the iframe', (hidden) => {
    Object.assign(appStore.cachedPublicSettings.custom_menu_items[0], { hide_open_button: hidden })
    const wrapper = mountPage()
    expect(wrapper.find('.custom-open-fab').exists()).toBe(hidden !== true)
    expect(wrapper.get('iframe').attributes('src')).toContain('https://example.com/docs')
  })

  it('preserves the embedded URL, secure link attributes, and normal clicks with small pointer movements', async () => {
    const { wrapper, button } = mountEmbed()
    expect(button.href).toBe(wrapper.get('iframe').attributes('src'))
    expect(button.href).not.toContain('user_id=')
    expect(button.href).not.toContain('token=')
    expect(wrapper.get('iframe').attributes('sandbox')).toBe('allow-scripts')
    expect(wrapper.get('iframe').attributes('title')).toBeTruthy()
    expect(wrapper.get('iframe').attributes('referrerpolicy')).toBe('no-referrer')
    expect(button.target).toBe('_blank')
    expect(button.rel).toBe('noopener noreferrer')
    await pointer(button, 'pointerdown', 700, 24)
    await pointer(button, 'pointermove', 702, 25)
    await pointer(button, 'pointerup', 702, 25)
    expect(button.style.left).toBe('')
    expect(click(button).defaultPrevented).toBe(false)
    expect(click(button, 0).defaultPrevented).toBe(false)
  })

  it('captures the pointer across iframe content and suppresses only the click following a drag', async () => {
    const { button } = mountEmbed()
    await pointer(button, 'pointerdown', 700, 24)
    expect(button.setPointerCapture).toHaveBeenCalledWith(1)
    await pointer(button, 'pointermove', 200, 124)
    expect(button.style.left).toBe('188px')
    expect(button.style.top).toBe('112px')
    await pointer(button, 'pointerup', 200, 124)
    expect(button.releasePointerCapture).toHaveBeenCalledWith(1)
    expect(click(button).defaultPrevented).toBe(true)
    expect(click(button).defaultPrevented).toBe(false)
    await pointer(button, 'pointerdown', 200, 124)
    await pointer(button, 'pointermove', 220, 124)
    await pointer(button, 'pointerup', 220, 124)
    expect(click(button, 0).defaultPrevented).toBe(false)
  })

  it('keeps the button inside each boundary and reachable when the container shrinks', async () => {
    const { button, size } = mountEmbed()
    await pointer(button, 'pointerdown', 700, 24)
    await pointer(button, 'pointermove', -1000, -1000)
    expect([button.style.left, button.style.top]).toEqual(['0px', '0px'])
    await pointer(button, 'pointermove', 2000, 2000)
    expect([button.style.left, button.style.top]).toEqual(['700px', '568px'])
    await pointer(button, 'pointerup', 2000, 2000)
    size.width = 300
    size.height = 200
    notifyResize()
    await nextTick()
    expect([button.style.left, button.style.top]).toEqual(['200px', '168px'])
  })

  it('stops moving on cancellation or lost pointer capture and permits the next normal click', async () => {
    const { button } = mountEmbed()
    for (const endEvent of ['pointercancel', 'lostpointercapture']) {
      await pointer(button, 'pointerdown', 700, 24)
      await pointer(button, 'pointermove', 500, 124)
      await pointer(button, endEvent, 500, 124)
      const position = button.style.cssText
      await pointer(button, 'pointermove', 400, 224)
      expect(button.style.cssText).toBe(position)
      await pointer(button, 'pointerdown', 500, 124)
      await pointer(button, 'pointerup', 500, 124)
      expect(click(button).defaultPrevented).toBe(false)
    }
  })

  it('leaves secondary mouse button gestures alone', async () => {
    const { button } = mountEmbed()
    await pointer(button, 'pointerdown', 700, 24, { button: 2 })
    await pointer(button, 'pointermove', 500, 124)
    expect(button.setPointerCapture).not.toHaveBeenCalled()
    expect(button.style.left).toBe('')
  })

  it('keeps Markdown pages separate from the embedded-page controls', async () => {
    appStore.cachedPublicSettings.custom_menu_items = [{ id: 'docs', url: 'md:guide' }]
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, text: async () => '# Guide' }))
    const wrapper = mountPage()
    await flushPromises()
    expect(wrapper.find('.custom-open-fab').exists()).toBe(false)
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(wrapper.get('.markdown-page-content h1').text()).toBe('Guide')
  })

  it('uses the translated document load error', async () => {
    appStore.cachedPublicSettings.custom_menu_items = [{ id: 'docs', url: 'md:guide' }]
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline')))
    const wrapper = mountPage()
    await flushPromises()
    expect(wrapper.get('.markdown-page-content').text()).toBe('legal.loadFailed')
  })

  it('cleans copy handlers, timers and animation frames on navigation and unmount', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    const cancelFrame = vi.fn()
    vi.stubGlobal('requestAnimationFrame', vi.fn(() => 123))
    vi.stubGlobal('cancelAnimationFrame', cancelFrame)
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    appStore.cachedPublicSettings.custom_menu_items = [
      { id: 'docs', url: 'md:guide' }, { id: 'next', url: 'md:next' },
    ]
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, text: async () => '```js\nconst example = true\n```' }))
    const wrapper = mountPage()
    await flushPromises()
    const oldButton = wrapper.get<HTMLButtonElement>('.copy-btn').element
    oldButton.click()
    await flushPromises()
    expect(vi.getTimerCount()).toBe(1)
    await wrapper.get('.markdown-page-content').trigger('scroll')
    routeState.params.id = 'next'
    await flushPromises()
    expect(cancelFrame).toHaveBeenCalledWith(123)
    expect(vi.getTimerCount()).toBe(0)
    oldButton.click()
    expect(writeText).toHaveBeenCalledTimes(1)
    await wrapper.get('.copy-btn').trigger('click')
    await flushPromises()
    expect(vi.getTimerCount()).toBe(1)
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('does not schedule feedback when a pending clipboard operation settles after unmount', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    let finishCopy!: () => void
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true, value: { writeText: () => new Promise<void>(resolve => { finishCopy = resolve }) },
    })
    appStore.cachedPublicSettings.custom_menu_items = [{ id: 'docs', url: 'md:guide' }]
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, text: async () => '```\nexample\n```' }))
    const wrapper = mountPage()
    await flushPromises()
    await wrapper.get('.copy-btn').trigger('click')
    wrapper.unmount()
    finishCopy()
    await flushPromises()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('keeps the latest Markdown page when an older response arrives last', async () => {
    appStore.cachedPublicSettings.custom_menu_items = [
      { id: 'docs', url: 'md:first' }, { id: 'next', url: 'md:second' },
    ]
    let finishOld!: (response: unknown) => void
    vi.stubGlobal('fetch', vi.fn().mockReturnValueOnce(new Promise(resolve => { finishOld = resolve }))
      .mockResolvedValueOnce({ ok: true, text: async () => '# Second page' }))
    const wrapper = mountPage()
    routeState.params.id = 'next'
    await flushPromises()
    expect(wrapper.get('.markdown-page-content h1').text()).toBe('Second page')
    finishOld({ ok: true, text: async () => '# First page' })
    await flushPromises()
    expect(wrapper.get('.markdown-page-content h1').text()).toBe('Second page')
    expect(wrapper.get('.toc-nav').text()).toBe('Second page')
  })

  it('ignores Markdown from a previous identity on the same page', async () => {
    appStore.cachedPublicSettings.custom_menu_items = [{ id: 'docs', url: 'md:guide' }]
    let finishOld!: (response: unknown) => void
    vi.stubGlobal('fetch', vi.fn().mockReturnValueOnce(new Promise(resolve => { finishOld = resolve }))
      .mockResolvedValueOnce({ ok: true, text: async () => '# New identity' }))
    const wrapper = mountPage()
    authState.user = { id: 8, role: 'user' }
    await flushPromises()
    finishOld({ ok: true, text: async () => '# Private old identity' })
    await flushPromises()
    expect(wrapper.get('.markdown-page-content h1').text()).toBe('New identity')
    expect(wrapper.text()).not.toContain('Private old identity')
  })

  it('aborts Markdown loading when switching to an embedded page or unmounting', async () => {
    appStore.cachedPublicSettings.custom_menu_items = [
      { id: 'docs', url: 'md:first' }, { id: 'embed', url: 'https://example.com/embed' },
    ]
    const signals: AbortSignal[] = []
    vi.stubGlobal('fetch', vi.fn((_url: string, options: RequestInit) => {
      signals.push(options.signal as AbortSignal)
      return new Promise(() => {})
    }))
    const wrapper = mountPage()
    routeState.params.id = 'embed'
    await nextTick()
    expect(wrapper.find('iframe').exists()).toBe(true)
    expect(signals[0]?.aborted).toBe(true)
    routeState.params.id = 'docs'
    await nextTick()
    wrapper.unmount()
    expect(signals[1]?.aborted).toBe(true)
  })
})
