import { beforeEach, describe, expect, it } from 'vitest'
import { useThemeDirection, THEME_DIRECTIONS } from '@/composables/useThemeDirection'

describe('OmniHive design system', () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.removeAttribute('data-theme')
    document.documentElement.classList.remove('dark')
    document.body.classList.remove('dense')
    const { setDarkMode, setDenseMode, setThemeDirection } = useThemeDirection()
    setDarkMode(false)
    setDenseMode(false)
    setThemeDirection('teal')
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
})
