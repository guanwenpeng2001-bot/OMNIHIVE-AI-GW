import { computed, ref } from 'vue'
import {
  LegacyStorageKeys,
  themeModeStorage,
  uiDensityStorage
} from '@/api/tokenRefresh'

export type ThemeModePreference = 'light' | 'dark' | 'system'
export type DensityPreference = 'comfort' | 'compact'
export type ThemeDirectionKey = 'teal' | 'control' | 'slate' | 'indigo' | 'mono'

export interface ThemeDirectionMeta {
  key: ThemeDirectionKey
  name: string
  shortName: string
  desc: string
  c1: string
  c2: string
  c3: string
  radius: string
}

export const THEME_DIRECTIONS: ThemeDirectionMeta[] = [
  {
    key: 'teal',
    name: 'OmniHive · Charcoal & Amber',
    shortName: 'OmniHive',
    desc: 'Honeycomb · Charcoal · Amber architectural system',
    c1: '#f7f8fa',
    c2: '#f59e0b',
    c3: '#141519',
    radius: '8px'
  }
]

const currentTheme = ref<ThemeDirectionKey>('teal')
const themeMode = ref<ThemeModePreference>('system')
const densityMode = ref<DensityPreference>('comfort')
const isDark = ref<boolean>(false)
const isDense = ref<boolean>(false)
const isSpecMode = ref<boolean>(false)
const cmdkOpen = ref<boolean>(false)
const themePickerOpen = ref<boolean>(false)

export interface GlobalDrawerState {
  open: boolean
  title: string
  html: string
  footerHtml: string
}

const drawerState = ref<GlobalDrawerState>({
  open: false,
  title: '',
  html: '',
  footerHtml: ''
})

let initialized = false
let mediaQueryCleanup: (() => void) | null = null

export interface LegacyThemeMigrationResult {
  mode: 'light' | 'dark'
  density?: DensityPreference
}

export function mapLegacyThemeDirection(
  rawStyle: string | null | undefined
): LegacyThemeMigrationResult | null {
  const normalized = (rawStyle || '').trim().toLowerCase()
  switch (normalized) {
    case 'control':
      return { mode: 'dark', density: 'compact' }
    case 'mono':
      return { mode: 'dark' }
    case 'slate':
    case 'indigo':
    case 'teal':
      return { mode: 'light' }
    default:
      return null
  }
}

function readLegacyStyleFromDomOrStorage(): string | null {
  try {
    if (typeof localStorage !== 'undefined') {
      const stored = localStorage.getItem(LegacyStorageKeys.THEME_STYLE)
      if (stored && stored.trim()) return stored
    }
  } catch {
    // ignore storage failures
  }
  if (typeof document !== 'undefined') {
    const attr = document.documentElement.getAttribute('data-theme')
    if (attr && attr.trim()) return attr
  }
  return null
}

function safeRemoveLegacyStyle(): void {
  try {
    if (typeof localStorage !== 'undefined') {
      localStorage.removeItem(LegacyStorageKeys.THEME_STYLE)
      localStorage.removeItem(LegacyStorageKeys.UI_SPEC)
    }
  } catch {
    // ignore storage failures
  }
}

function prefersDarkColorScheme(): boolean {
  return (
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-color-scheme: dark)').matches
  )
}

export function isPublicDefaultLightPath(pathname: string): boolean {
  if (!pathname || pathname === '/' || pathname === '/home') return true
  return (
    pathname.startsWith('/models') ||
    pathname.startsWith('/model-plaza') ||
    pathname.startsWith('/pricing') ||
    pathname.startsWith('/status') ||
    pathname.startsWith('/docs') ||
    pathname.startsWith('/legal') ||
    pathname.startsWith('/key-usage') ||
    pathname.startsWith('/login') ||
    pathname.startsWith('/register') ||
    pathname.startsWith('/forgot-password') ||
    pathname.startsWith('/reset-password') ||
    pathname.startsWith('/email-verify') ||
    pathname.startsWith('/auth/') ||
    pathname.startsWith('/setup')
  )
}

function resolveEffectiveDark(mode: ThemeModePreference | null): boolean {
  if (mode === 'dark') return true
  if (mode === 'light') return false
  if (mode === 'system') return prefersDarkColorScheme()
  const pathname = typeof window !== 'undefined' ? window.location.pathname : '/'
  if (isPublicDefaultLightPath(pathname)) {
    return false
  }
  return true
}

function clearRetiredDomTheme(): void {
  if (typeof document === 'undefined') return
  document.documentElement.removeAttribute('data-theme')
}

function applyDomDark(dark: boolean): void {
  if (typeof document === 'undefined') return
  document.documentElement.classList.toggle('dark', dark)
  document.documentElement.style.colorScheme = dark ? 'dark' : 'light'
}

function applyDomDensity(density: DensityPreference): void {
  if (typeof document === 'undefined') return
  document.documentElement.setAttribute('data-density', density)
  if (document.body) {
    document.body.classList.toggle('dense', density === 'compact')
  }
}

function applyDomSpec(spec: boolean): void {
  if (typeof document === 'undefined' || !document.body) return
  document.body.classList.toggle('spec-mode', spec)
}

function ensureSystemThemeListener(): void {
  if (mediaQueryCleanup || typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
    return
  }
  const mql = window.matchMedia('(prefers-color-scheme: dark)')
  const onChange = () => {
    if (themeMode.value === 'system' && themeModeStorage.get() === 'system') {
      isDark.value = mql.matches
      applyDomDark(isDark.value)
    }
  }
  mql.addEventListener?.('change', onChange)
  mediaQueryCleanup = () => mql.removeEventListener?.('change', onChange)
}

export function resetThemeDirectionForTests(): void {
  initialized = false
  if (mediaQueryCleanup) {
    mediaQueryCleanup()
    mediaQueryCleanup = null
  }
}

export function useThemeDirection() {
  function initThemeDirection(): void {
    if (typeof document === 'undefined') return
    currentTheme.value = 'teal'
    const legacyStyle = readLegacyStyleFromDomOrStorage()
    const legacyMapped = mapLegacyThemeDirection(legacyStyle)
    clearRetiredDomTheme()
    safeRemoveLegacyStyle()

    let savedMode = themeModeStorage.get() as ThemeModePreference | null
    if (!savedMode && legacyMapped) {
      savedMode = legacyMapped.mode
      themeModeStorage.set(savedMode)
    }
    if (savedMode === 'dark' || savedMode === 'light' || savedMode === 'system') {
      themeMode.value = savedMode
      isDark.value = resolveEffectiveDark(savedMode)
    } else {
      themeMode.value = 'system'
      isDark.value = resolveEffectiveDark(null)
    }
    applyDomDark(isDark.value)
    ensureSystemThemeListener()

    let savedDensity = uiDensityStorage.get() as DensityPreference | null
    if (!savedDensity && legacyMapped?.density) {
      savedDensity = legacyMapped.density
      uiDensityStorage.set(savedDensity)
    }
    densityMode.value = savedDensity === 'compact' ? 'compact' : 'comfort'
    isDense.value = densityMode.value === 'compact'
    applyDomDensity(densityMode.value)

    isSpecMode.value = false
    applyDomSpec(isSpecMode.value)
    initialized = true
  }

  if (!initialized && typeof document !== 'undefined') {
    initThemeDirection()
  }

  function setThemeDirection(key: ThemeDirectionKey): void {
    currentTheme.value = 'teal'
    clearRetiredDomTheme()
    safeRemoveLegacyStyle()
    const mapped = mapLegacyThemeDirection(key)
    if (mapped) {
      setThemeMode(mapped.mode)
      if (mapped.density) {
        setDensityMode(mapped.density)
      }
    }
  }

  function setThemeMode(mode: ThemeModePreference): void {
    themeMode.value = mode
    themeModeStorage.set(mode)
    isDark.value = resolveEffectiveDark(mode)
    applyDomDark(isDark.value)
  }

  function setDarkMode(dark: boolean): void {
    setThemeMode(dark ? 'dark' : 'light')
  }

  function toggleDarkMode(): boolean {
    setDarkMode(!isDark.value)
    return isDark.value
  }

  function setDensityMode(density: DensityPreference): void {
    densityMode.value = density
    isDense.value = density === 'compact'
    uiDensityStorage.set(density)
    applyDomDensity(density)
  }

  function setDenseMode(dense: boolean): void {
    setDensityMode(dense ? 'compact' : 'comfort')
  }

  function toggleDenseMode(): boolean {
    setDenseMode(!isDense.value)
    return isDense.value
  }

  function setSpecMode(spec: boolean): void {
    isSpecMode.value = spec
    applyDomSpec(spec)
  }

  function toggleSpecMode(): boolean {
    setSpecMode(!isSpecMode.value)
    return isSpecMode.value
  }

  function openDrawer(title: string, html: string, footerHtml = ''): void {
    drawerState.value = {
      open: true,
      title,
      html,
      footerHtml
    }
  }

  function closeDrawer(): void {
    drawerState.value = {
      ...drawerState.value,
      open: false
    }
  }

  function openCommandPalette(): void {
    cmdkOpen.value = true
    if (typeof window !== 'undefined') {
      window.dispatchEvent(new CustomEvent('omnihive:open-cmdk'))
    }
  }

  function closeCommandPalette(): void {
    cmdkOpen.value = false
  }

  function openThemePickerModal(): void {
    themePickerOpen.value = false
  }

  function closeThemePickerModal(): void {
    themePickerOpen.value = false
  }

  const currentThemeMeta = computed(() => THEME_DIRECTIONS[0])

  return {
    themes: THEME_DIRECTIONS,
    currentTheme,
    currentThemeMeta,
    themeMode,
    densityMode,
    isDark,
    isDense,
    isSpecMode,
    cmdkOpen,
    themePickerOpen,
    drawerState,
    initThemeDirection,
    setThemeDirection,
    setThemeMode,
    setDarkMode,
    toggleDarkMode,
    setDensityMode,
    setDenseMode,
    toggleDenseMode,
    setSpecMode,
    toggleSpecMode,
    openDrawer,
    closeDrawer,
    openCommandPalette,
    closeCommandPalette,
    openThemePickerModal,
    closeThemePickerModal
  }
}
