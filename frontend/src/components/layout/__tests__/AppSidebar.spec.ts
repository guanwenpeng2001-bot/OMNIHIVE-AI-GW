import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'
import {
  ADMIN_MODELS_NAV_ITEM,
  CORE_NAVIGATION_ITEMS
} from '@/router/navigationRegistry'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../AppSidebar.vue')
const componentSource = readFileSync(componentPath, 'utf8')
const stylePath = resolve(dirname(fileURLToPath(import.meta.url)), '../../../style.css')
const styleSource = readFileSync(stylePath, 'utf8')

describe('AppSidebar custom SVG styles', () => {
  it('does not override uploaded SVG fill or stroke colors', () => {
    expect(componentSource).toContain('.sidebar-svg-icon {')
    expect(componentSource).toContain('color: currentColor;')
    expect(componentSource).toContain('display: block;')
    expect(componentSource).not.toContain('stroke: currentColor;')
    expect(componentSource).not.toContain('fill: none;')
  })
})

describe('AppSidebar scroll position persistence', () => {
  it('binds a template ref to the sidebar nav element', () => {
    expect(componentSource).toContain('ref="sidebarNavRef"')
    expect(componentSource).toContain('sidebar-nav')
  })

  it('declares sidebarNavRef in script setup', () => {
    expect(componentSource).toContain("const sidebarNavRef = ref<HTMLElement | null>(null)")
  })

  it('saves scroll position on beforeUnmount', () => {
    expect(componentSource).toContain('onBeforeUnmount')
    expect(componentSource).toContain('appStore.sidebarScrollTop')
    expect(componentSource).toContain('sidebarNavRef.value.scrollTop')
  })

  it('restores scroll position on mount', () => {
    expect(componentSource).toContain('onMounted')
    expect(componentSource).toContain('appStore.sidebarScrollTop')
    expect(componentSource).toContain('nextTick')
  })
})

describe('AppSidebar collapsible groups', () => {
  it('lets the user collapse a group even while a child route is active', () => {
    // The expand state must come from the user's override first, falling back
    // to the active-route heuristic only when the user has not clicked yet.
    expect(componentSource).toContain('const groupExpandOverrides = ref<Map<string, boolean>>(new Map())')
    expect(componentSource).not.toContain('expandedGroups.value.has(item.path) || isGroupActive(item)')
  })
})

describe('AppSidebar header styles', () => {
  it('does not clip the version badge dropdown', () => {
    const sidebarHeaderBlockMatch = styleSource.match(/\.sidebar-header\s*\{[\s\S]*?\n {2}\}/)
    const sidebarBrandBlockMatch = componentSource.match(/\.sidebar-brand\s*\{[\s\S]*?\n\}/)

    expect(sidebarHeaderBlockMatch).not.toBeNull()
    expect(sidebarBrandBlockMatch).not.toBeNull()
    expect(sidebarHeaderBlockMatch?.[0]).not.toContain('@apply overflow-hidden;')
    expect(sidebarBrandBlockMatch?.[0]).not.toContain('overflow: hidden;')
  })
})

describe('AppSidebar subscription feature flag', () => {
  it('gates the My Subscriptions entry behind the subscription public-settings flag', () => {
    expect(componentSource).toContain('const flagSubscription = makeSidebarFlag(FeatureFlags.subscription)')
    expect(componentSource).toMatch(/path: '\/subscriptions'[^\n]*featureFlag: flagSubscription/)
  })

  it('also hides the admin Subscription Management entry on recharge-only sites', () => {
    expect(componentSource).toMatch(/path: '\/admin\/subscriptions'[^\n]*featureFlag: flagSubscription/)
  })

  it('derives the purchase entry label from the site billing mode', () => {
    expect(componentSource).toContain("import { resolveSiteBillingMode } from '@/utils/siteBillingMode'")
    expect(componentSource).toMatch(/case 'recharge_only':\s*return t\('nav\.recharge'\)/)
    expect(componentSource).toMatch(/case 'subscription_only':\s*return t\('nav\.subscribe'\)/)
    expect(componentSource).toMatch(/path: '\/purchase'[^\n]*label: purchaseNavLabel\.value/)
  })
})

describe('AppSidebar product surface integration', () => {
  const activeRegistryPaths = new Set(
    CORE_NAVIGATION_ITEMS.filter((item) => !item.hiddenInSidebar).map((item) => item.path)
  )

  it.each([
    '/playground',
    '/requests',
    '/tasks',
    '/billing',
    '/admin/pools',
    '/admin/routing',
    '/admin/audit',
    '/admin/billing',
    '/admin/members',
    '/admin/plugins'
  ])('keeps %s reachable from the primary console navigation', (path) => {
    expect(componentSource).toContain('CORE_NAVIGATION_ITEMS')
    expect(activeRegistryPaths.has(path)).toBe(true)
  })

  it('drives sidebar sections from navigationRegistry with /keys, /byok, /workspaces under access and /admin/models at order 27', () => {
    expect(componentSource).toContain('USER_CONSOLE_SECTIONS')
    expect(componentSource).toContain('ADMIN_CONSOLE_SECTIONS')
    const overviewItems = CORE_NAVIGATION_ITEMS.filter(
      (item) => item.tier === 'user' && item.section === 'overview'
    )
    expect(overviewItems.map((i) => i.path)).toEqual(['/dashboard'])

    const accessItems = CORE_NAVIGATION_ITEMS.filter(
      (item) => item.tier === 'user' && item.section === 'access'
    )
    expect(accessItems.map((i) => i.path)).toContain('/keys')
    expect(accessItems.map((i) => i.path)).toContain('/byok')
    expect(accessItems.map((i) => i.path)).toContain('/workspaces')

    expect(ADMIN_MODELS_NAV_ITEM.path).toBe('/admin/models')
    expect(ADMIN_MODELS_NAV_ITEM.section).toBe('connections')
    expect(ADMIN_MODELS_NAV_ITEM.order).toBe(27)
    expect(ADMIN_MODELS_NAV_ITEM.fallbackLabelZh).toBe('模型目录')
  })

  it('persists section collapse state, scrolls active item into view, and renders bottom overflow fade', () => {
    expect(componentSource).toContain('SIDEBAR_SECTIONS_STORAGE_KEY')
    expect(componentSource).toContain("scrollIntoView({ block: 'nearest' })")
    expect(componentSource).toContain('data-testid="sidebar-scroll-fade"')
  })
})
