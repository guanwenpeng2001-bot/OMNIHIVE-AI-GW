import { computed, ref } from 'vue'

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
    name: 'Hive Aura · 极简基准',
    shortName: 'Hive Aura',
    desc: '暖白极简 + 蜜金琥珀，OmniHive 默认',
    c1: '#fafafa',
    c2: '#f59e0b',
    c3: '#16161a',
    radius: '12px'
  },
  {
    key: 'control',
    name: 'Control Room · 工程风',
    shortName: 'Control Room',
    desc: '深色工程控制台，数据密度最高',
    c1: '#06080d',
    c2: '#22d3ee',
    c3: '#dbe4ee',
    radius: '10px'
  },
  {
    key: 'slate',
    name: 'Quiet Slate · 极简',
    shortName: 'Quiet Slate',
    desc: '冷静极简，向 Linear 靠拢',
    c1: '#fbfbfc',
    c2: '#4f46e5',
    c3: '#18181b',
    radius: '16px'
  },
  {
    key: 'indigo',
    name: 'Vertex Indigo · 品牌色',
    shortName: 'Vertex Indigo',
    desc: '品牌紫渐变，SaaS 产品感',
    c1: '#f6f7fb',
    c2: '#6d4aff',
    c3: '#15162b',
    radius: '14px'
  },
  {
    key: 'mono',
    name: 'Mono Terminal · 技术风',
    shortName: 'Mono Terminal',
    desc: '等宽字体 + 荧光绿，技术文档风',
    c1: '#0b0d0c',
    c2: '#62f0c4',
    c3: '#d6e4da',
    radius: '6px'
  }
]

const VALID_KEYS = new Set<ThemeDirectionKey>(['teal', 'control', 'slate', 'indigo', 'mono'])

const currentTheme = ref<ThemeDirectionKey>('teal')
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
  title: '详情',
  html: '',
  footerHtml: ''
})

let initialized = false

function safeGetStorage(key: string): string | null {
  try {
    return typeof localStorage !== 'undefined' ? localStorage.getItem(key) : null
  } catch {
    return null
  }
}

function safeSetStorage(key: string, value: string): void {
  try {
    if (typeof localStorage !== 'undefined') {
      localStorage.setItem(key, value)
    }
  } catch {
    // ignore storage failures
  }
}

function applyDomTheme(key: ThemeDirectionKey, animate = false): void {
  if (typeof document === 'undefined') return
  const root = document.documentElement
  if (key === 'teal') {
    root.removeAttribute('data-theme')
  } else {
    root.setAttribute('data-theme', key)
  }
  if (animate && document.body) {
    document.body.classList.add('theme-anim')
    window.setTimeout(() => {
      document.body?.classList.remove('theme-anim')
    }, 360)
  }
}

function applyDomDark(dark: boolean): void {
  if (typeof document === 'undefined') return
  document.documentElement.classList.toggle('dark', dark)
}

function applyDomDense(dense: boolean): void {
  if (typeof document === 'undefined' || !document.body) return
  document.body.classList.toggle('dense', dense)
}

function applyDomSpec(spec: boolean): void {
  if (typeof document === 'undefined' || !document.body) return
  document.body.classList.toggle('spec-mode', spec)
}

export function useThemeDirection() {
  function initThemeDirection(): void {
    if (typeof document === 'undefined') return
    const savedStyle = safeGetStorage('omnihive.style') as ThemeDirectionKey | null
    if (savedStyle && VALID_KEYS.has(savedStyle)) {
      currentTheme.value = savedStyle
    } else {
      currentTheme.value = 'teal'
    }
    applyDomTheme(currentTheme.value, false)

    const savedMode = safeGetStorage('omnihive.mode') ?? safeGetStorage('theme')
    if (savedMode === 'dark') {
      isDark.value = true
    } else if (savedMode === 'light') {
      isDark.value = false
    } else {
      isDark.value =
        document.documentElement.classList.contains('dark') ||
        (typeof window !== 'undefined' &&
          typeof window.matchMedia === 'function' &&
          window.matchMedia('(prefers-color-scheme: dark)').matches)
    }
    applyDomDark(isDark.value)

    const savedDense = safeGetStorage('omnihive.dense')
    isDense.value = savedDense === '1'
    applyDomDense(isDense.value)

    const savedSpec = safeGetStorage('omnihive.spec')
    isSpecMode.value = savedSpec === '1'
    applyDomSpec(isSpecMode.value)
    initialized = true
  }

  if (!initialized && typeof document !== 'undefined') {
    initThemeDirection()
  }

  function setThemeDirection(key: ThemeDirectionKey): void {
    if (!VALID_KEYS.has(key)) return
    currentTheme.value = key
    safeSetStorage('omnihive.style', key)
    applyDomTheme(key, true)
  }

  function setDarkMode(dark: boolean): void {
    isDark.value = dark
    const mode = dark ? 'dark' : 'light'
    safeSetStorage('omnihive.mode', mode)
    safeSetStorage('theme', mode)
    applyDomDark(dark)
  }

  function toggleDarkMode(): boolean {
    setDarkMode(!isDark.value)
    return isDark.value
  }

  function setDenseMode(dense: boolean): void {
    isDense.value = dense
    safeSetStorage('omnihive.dense', dense ? '1' : '0')
    applyDomDense(dense)
  }

  function toggleDenseMode(): boolean {
    setDenseMode(!isDense.value)
    return isDense.value
  }

  function setSpecMode(spec: boolean): void {
    isSpecMode.value = spec
    safeSetStorage('omnihive.spec', spec ? '1' : '0')
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
    themePickerOpen.value = true
    if (typeof window !== 'undefined') {
      window.dispatchEvent(new CustomEvent('omnihive:open-theme-picker'))
    }
  }

  function closeThemePickerModal(): void {
    themePickerOpen.value = false
  }

  const currentThemeMeta = computed(
    () => THEME_DIRECTIONS.find((t) => t.key === currentTheme.value) ?? THEME_DIRECTIONS[0]
  )

  return {
    themes: THEME_DIRECTIONS,
    currentTheme,
    currentThemeMeta,
    isDark,
    isDense,
    isSpecMode,
    cmdkOpen,
    themePickerOpen,
    drawerState,
    initThemeDirection,
    setThemeDirection,
    setDarkMode,
    toggleDarkMode,
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
