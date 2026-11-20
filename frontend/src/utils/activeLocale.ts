import { computed, getCurrentInstance, type ComputedRef } from 'vue'
import { useI18n } from 'vue-i18n'

export function useIsZhLocale(fallback: 'zh' | 'en' = 'zh'): ComputedRef<boolean> {
  let i18nLocale: { value?: string } | string | undefined
  try {
    const composer = useI18n()
    i18nLocale = composer?.locale
  } catch {
    i18nLocale = undefined
  }
  const gp = getCurrentInstance()?.appContext.config.globalProperties as
    | { $i18n?: { locale?: string | { value?: string } } }
    | undefined

  return computed(() => {
    const fromComposer = typeof i18nLocale === 'string' ? i18nLocale : i18nLocale?.value
    const fromGlobal =
      typeof gp?.$i18n?.locale === 'string' ? gp.$i18n.locale : gp?.$i18n?.locale?.value
    const fromDoc = typeof document !== 'undefined' ? document.documentElement?.lang : ''
    const raw = fromComposer || fromGlobal || fromDoc || fallback
    return String(raw).toLowerCase().startsWith('zh')
  })
}
