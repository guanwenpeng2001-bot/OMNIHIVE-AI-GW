import { i18n } from '@/i18n'
import type { RouteLocationNormalizedLoaded } from 'vue-router'
import type { CustomMenuItem } from '@/types'
import type { SiteBillingMode } from '@/utils/siteBillingMode'
import { resolveBrandSiteName } from './brandConfig'

interface DynamicRouteTitleEntry {
  routeName: string
  key: string
  title: string
}

let activeDynamicRouteTitle: DynamicRouteTitleEntry | null = null

function normalizeDynamicRouteKey(raw: unknown): string {
  const value = Array.isArray(raw) ? raw.join('/') : String(raw ?? '')
  try {
    return decodeURIComponent(value).trim()
  } catch {
    return value.trim()
  }
}

export function setDynamicRouteTitle(routeName: string, key: string, title: string | null): void {
  const normalizedKey = normalizeDynamicRouteKey(key)
  const normalizedTitle = title?.trim() || ''
  if (!normalizedTitle) {
    if (
      activeDynamicRouteTitle &&
      activeDynamicRouteTitle.routeName === routeName &&
      (!normalizedKey || activeDynamicRouteTitle.key === normalizedKey)
    ) {
      activeDynamicRouteTitle = null
    }
    return
  }
  activeDynamicRouteTitle = { routeName, key: normalizedKey, title: normalizedTitle }
}

function isCurrentLocaleZh(): boolean {
  const globalComposer = i18n?.global as { locale?: { value?: string } | string } | undefined
  const rawLocale = globalComposer?.locale
  const localeStr = typeof rawLocale === 'string' ? rawLocale : rawLocale?.value
  if (localeStr) {
    return String(localeStr).toLowerCase().startsWith('zh')
  }
  if (typeof document !== 'undefined' && document.documentElement?.lang) {
    return document.documentElement.lang.toLowerCase().startsWith('zh')
  }
  return false
}

/**
 * 统一生成页面标题，避免多处写入 document.title 产生覆盖冲突。
 * 优先使用 titleKey 通过 i18n 翻译，其次在中文模式下使用 routeTitleZh，最后回退到静态 routeTitle。
 */
export function resolveDocumentTitle(
  routeTitle: unknown,
  siteName?: string,
  titleKey?: string,
  routeTitleZh?: unknown
): string {
  const normalizedSiteName = resolveBrandSiteName(siteName)

  if (typeof titleKey === 'string' && titleKey.trim()) {
    const translated = i18n.global.t(titleKey)
    if (translated && translated !== titleKey) {
      return `${translated} - ${normalizedSiteName}`
    }
  }

  if (isCurrentLocaleZh() && typeof routeTitleZh === 'string' && routeTitleZh.trim()) {
    return `${routeTitleZh.trim()} - ${normalizedSiteName}`
  }

  if (typeof routeTitle === 'string' && routeTitle.trim()) {
    return `${routeTitle.trim()} - ${normalizedSiteName}`
  }

  return normalizedSiteName
}

export interface RouteTitleOptions {
  /**
   * 站点计费模式（见 utils/billingMode.ts）。/purchase 的标题与描述随之切换：
   * 仅充值 → 「充值」，仅订阅 → 「订阅」，缺省/两者都有 → 「充值/订阅」。
   */
  billingMode?: SiteBillingMode
}

export interface RouteMetaKeys {
  titleKey?: string
  descriptionKey?: string
}

/** /purchase 路由名，与 router/index.ts 中的声明保持一致。 */
export const PURCHASE_ROUTE_NAME = 'PurchaseSubscription'

/**
 * 解析路由的 i18n 标题/描述 key。绝大多数路由直接取 meta；
 * /purchase 随站点计费模式切换文案，AppHeader 与 document.title 共用这一处判断。
 */
export function resolveRouteMetaKeys(
  route: Pick<RouteLocationNormalizedLoaded, 'name' | 'meta'>,
  options: RouteTitleOptions = {},
): RouteMetaKeys {
  if (route.name === PURCHASE_ROUTE_NAME) {
    if (options.billingMode === 'recharge_only') {
      return { titleKey: 'nav.recharge', descriptionKey: 'purchase.rechargeDescription' }
    }
    if (options.billingMode === 'subscription_only') {
      return { titleKey: 'nav.subscribe', descriptionKey: 'purchase.subscriptionDescription' }
    }
  }
  return {
    titleKey: typeof route.meta.titleKey === 'string' ? route.meta.titleKey : undefined,
    descriptionKey: typeof route.meta.descriptionKey === 'string' ? route.meta.descriptionKey : undefined,
  }
}

export function resolveRouteDocumentTitle(
  route: Pick<RouteLocationNormalizedLoaded, 'name' | 'params' | 'meta'>,
  siteName: string | undefined,
  customMenuItems: CustomMenuItem[] = [],
  options: RouteTitleOptions = {},
): string {
  const id = typeof route.params.id === 'string' ? route.params.id : ''
  const menuItem = route.name === 'CustomPage' && id
    ? customMenuItems.find((item) => item.id === id)
    : undefined
  const dynamicTitle =
    activeDynamicRouteTitle &&
    String(route.name ?? '') === activeDynamicRouteTitle.routeName &&
    normalizeDynamicRouteKey(route.params.id) === activeDynamicRouteTitle.key
      ? activeDynamicRouteTitle.title
      : ''
  const menuTitle = menuItem?.label.trim()
  const overrideTitle = menuTitle || dynamicTitle
  const { titleKey } = resolveRouteMetaKeys(route, options)

  return resolveDocumentTitle(
    overrideTitle || route.meta.title,
    siteName,
    overrideTitle ? undefined : titleKey,
    overrideTitle ? undefined : route.meta.titleZh
  )
}
