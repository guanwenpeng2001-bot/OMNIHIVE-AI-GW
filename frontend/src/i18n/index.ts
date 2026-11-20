import { resolveSiteBillingMode } from '@/utils/siteBillingMode'
import * as vueI18n from 'vue-i18n'

type LocaleCode = 'en' | 'zh'

type LocaleMessages = Record<string, any>

const LOCALE_KEY = 'sub2api_locale'
const DEFAULT_LOCALE: LocaleCode = 'en'

const localeLoaders: Record<LocaleCode, () => Promise<{ default: LocaleMessages }>> = {
  en: () => import('./locales/en'),
  zh: () => import('./locales/zh')
}

function isLocaleCode(value: string): value is LocaleCode {
  return value === 'en' || value === 'zh'
}

function getDefaultLocale(): LocaleCode {
  const saved = localStorage.getItem(LOCALE_KEY)
  if (saved && isLocaleCode(saved)) {
    return saved
  }

  const browserLang = navigator.language.toLowerCase()
  if (browserLang.startsWith('zh')) {
    return 'zh'
  }

  return DEFAULT_LOCALE
}

type I18nInstance = ReturnType<typeof vueI18n.createI18n>

export const i18n: I18nInstance =
  typeof vueI18n.createI18n === 'function'
    ? vueI18n.createI18n({
        legacy: false,
        locale: getDefaultLocale(),
        fallbackLocale: DEFAULT_LOCALE,
        messages: {},
        // 禁用 HTML 消息警告 - 引导步骤使用富文本内容（driver.js 支持 HTML）
        // 这些内容是内部定义的，不存在 XSS 风险
        warnHtmlMessage: false
      })
    : ({
        global: {
          locale: { value: getDefaultLocale() },
          t: (key: string) => key,
          te: () => false,
          mergeLocaleMessage: () => {},
          setLocaleMessage: () => {}
        },
        install: () => {}
      } as unknown as I18nInstance)

let localeRevision = 0
const pendingLocales = new Map<LocaleCode, Promise<void>>()
const loadedLocales = new Set<LocaleCode>()

export async function loadLocaleMessages(locale: LocaleCode): Promise<void> {
  if (loadedLocales.has(locale)) {
    return
  }

  let pending = pendingLocales.get(locale)
  if (!pending) {
    pending = localeLoaders[locale]().then(module => {
      i18n.global.setLocaleMessage(locale, module.default)
      loadedLocales.add(locale)
    }).finally(() => pendingLocales.delete(locale))
    pendingLocales.set(locale, pending)
  }
  await pending
}

export async function initI18n(): Promise<void> {
  const current = getLocale()
  document.documentElement.setAttribute('lang', current)
  await loadLocaleMessages(current)
  document.documentElement.setAttribute('lang', getLocale())
}

export async function setLocale(locale: string): Promise<void> {
  if (!isLocaleCode(locale)) {
    return
  }

  const revision = ++localeRevision
  await loadLocaleMessages(locale)
  if (revision !== localeRevision) return
  i18n.global.locale.value = locale
  localStorage.setItem(LOCALE_KEY, locale)
  document.documentElement.setAttribute('lang', locale)

  // 同步更新浏览器页签标题，使其跟随语言切换
  const { resolveRouteDocumentTitle } = await import('@/router/title')
  const { default: router } = await import('@/router')
  const { useAppStore } = await import('@/stores/app')
  const { useAuthStore } = await import('@/stores/auth')
  const { useAdminSettingsStore } = await import('@/stores/adminSettings')
  if (revision !== localeRevision) return
  const route = router.currentRoute.value
  const appStore = useAppStore()
  const authStore = useAuthStore()
  const adminSettingsStore = useAdminSettingsStore()
  const customMenuItems = [
    ...(appStore.cachedPublicSettings?.custom_menu_items ?? []),
    ...(authStore.isAdmin ? adminSettingsStore.customMenuItems : []),
  ]
  document.title = resolveRouteDocumentTitle(route, appStore.siteName, customMenuItems, {
    billingMode: resolveSiteBillingMode(appStore.cachedPublicSettings),
  })
}

export function getLocale(): LocaleCode {
  const current = i18n.global.locale.value
  return isLocaleCode(current) ? current : DEFAULT_LOCALE
}

export const availableLocales = [
  { code: 'en', name: 'English', flag: '🇺🇸' },
  { code: 'zh', name: '中文', flag: '🇨🇳' }
] as const

export default i18n
