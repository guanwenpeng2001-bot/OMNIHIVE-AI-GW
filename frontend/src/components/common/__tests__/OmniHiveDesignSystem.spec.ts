import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { useThemeDirection, THEME_DIRECTIONS } from '@/composables/useThemeDirection'
import CommandPalette from '../CommandPalette.vue'
import ThemeStyleDock from '../ThemeStyleDock.vue'

const pushSpy = vi.fn()

vi.mock('vue-router', () => ({
  useRouter: () => ({
    push: pushSpy
  })
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => ({
    isAdmin: true
  }),
  useAppStore: () => ({
    showInfo: vi.fn()
  })
}))

describe('OmniHive design system & command palette', () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.removeAttribute('data-theme')
    document.documentElement.classList.remove('dark')
    document.body.classList.remove('dense')
    const { setDarkMode, setDenseMode, setThemeDirection } = useThemeDirection()
    setDarkMode(false)
    setDenseMode(false)
    setThemeDirection('teal')
    pushSpy.mockReset()
  })

  it('switches between the 5 prototype theme directions and persists state', () => {
    const {
      currentTheme,
      isDark,
      isDense,
      setThemeDirection,
      toggleDarkMode,
      toggleDenseMode
    } = useThemeDirection()

    expect(THEME_DIRECTIONS).toHaveLength(5)
    setThemeDirection('control')
    expect(currentTheme.value).toBe('control')
    expect(document.documentElement.getAttribute('data-theme')).toBe('control')
    expect(localStorage.getItem('omnihive.style')).toBe('control')

    setThemeDirection('teal')
    expect(document.documentElement.getAttribute('data-theme')).toBeNull()

    toggleDarkMode()
    expect(isDark.value).toBe(true)
    expect(document.documentElement.classList.contains('dark')).toBe(true)

    toggleDenseMode()
    expect(isDense.value).toBe(true)
    expect(document.body.classList.contains('dense')).toBe(true)
  })

  it('opens CommandPalette on event and filters/executes entries', async () => {
    const wrapper = mount(CommandPalette, {
      global: {
        stubs: {
          Teleport: true
        }
      }
    })

    window.dispatchEvent(new CustomEvent('omnihive:open-cmdk'))
    await nextTick()

    const input = wrapper.get('input[aria-label="搜索命令或页面"]')
    await input.setValue('系统设置')
    await nextTick()

    const options = wrapper.findAll('[role="option"]')
    expect(options.length).toBeGreaterThan(0)
    await options[0].trigger('click')
    expect(pushSpy).toHaveBeenCalledWith('/admin/settings')
    wrapper.unmount()
  })

  it('renders ThemeStyleDock with all 5 theme dots and modal picker', async () => {
    const wrapper = mount(ThemeStyleDock, {
      global: {
        stubs: {
          Teleport: true
        }
      }
    })

    const dots = wrapper.findAll('.dock-dot')
    expect(dots).toHaveLength(5)
    await dots[2].trigger('click')
    expect(document.documentElement.getAttribute('data-theme')).toBe('slate')
    wrapper.unmount()
  })
})
