import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { useThemeDirection, THEME_DIRECTIONS } from '@/composables/useThemeDirection'
import CommandPalette from '../CommandPalette.vue'

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

  it('keeps the single OmniHive direction and persists mode state', () => {
    const {
      currentTheme,
      isDark,
      isDense,
      setThemeDirection,
      toggleDarkMode,
      toggleDenseMode
    } = useThemeDirection()

    expect(THEME_DIRECTIONS).toHaveLength(1)
    expect(THEME_DIRECTIONS[0].key).toBe('teal')
    setThemeDirection('teal')
    expect(currentTheme.value).toBe('teal')
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
})
