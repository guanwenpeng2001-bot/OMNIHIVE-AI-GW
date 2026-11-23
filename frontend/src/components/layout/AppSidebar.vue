<template>
  <aside
    id="sidebar"
    data-testid="console-sidebar"
    :data-shell-tier="activeTier"
    :aria-label="activeTier === 'admin' ? 'Admin Console Navigation' : 'Developer Console Navigation'"
    class="fixed bottom-0 left-0 top-14 z-30 flex flex-col border-r border-oh-default bg-oh-subtle text-oh-primary transition-[width,transform] duration-150 motion-reduce:transition-none"
    :class="[
      sidebarCollapsed ? 'lg:w-[64px]' : 'lg:w-[240px]',
      mobileOpen ? 'w-[260px] translate-x-0 shadow-oh-xl' : 'w-[260px] -translate-x-full lg:translate-x-0'
    ]"
  >
    <!-- Sidebar Filter Input -->
    <div v-if="!sidebarCollapsed" class="border-b border-oh-subtle px-3 py-2.5">
      <div class="relative flex items-center">
        <Icon
          name="search"
          size="xs"
          class="pointer-events-none absolute left-2.5 text-oh-muted"
        />
        <input
          v-model="sidebarSearchQuery"
          type="search"
          data-testid="sidebar-filter-input"
          :placeholder="isZh ? '筛选导航菜单…' : 'Filter navigation…'"
          :aria-label="isZh ? '筛选导航菜单' : 'Filter navigation'"
          class="h-7 w-full rounded-oh-md border border-oh-default bg-oh-surface pl-8 pr-2 text-xs text-oh-primary placeholder:text-oh-muted focus:border-oh-strong focus:outline-none"
        />
      </div>
    </div>

    <!-- Scrollable Navigation Rail -->
    <div class="relative flex min-h-0 flex-1 flex-col overflow-hidden">
      <nav
        ref="sidebarNavRef"
        class="sidebar-nav flex-1 overflow-y-auto px-2 py-3"
        @scroll.passive="updateSidebarScrollShadow"
      >
        <div class="space-y-4">
          <section
            v-for="group in visibleGroups"
            :key="group.key"
            class="space-y-1"
          >
            <button
              v-if="!sidebarCollapsed"
              type="button"
              :aria-expanded="isSectionExpanded(group.key)"
              class="oh-focus-ring flex w-full items-center justify-between rounded-oh-sm px-2.5 py-0.5 text-left"
              @click="toggleSection(group.key)"
            >
              <span class="oh-eyebrow">{{ group.label }}</span>
              <Icon
                name="chevronDown"
                size="xs"
                class="text-oh-muted transition-transform"
                :class="{ '-rotate-90': !isSectionExpanded(group.key) }"
              />
            </button>
            <div v-else class="mx-2 my-1.5 border-t border-oh-subtle" :title="group.label" />

            <div
              v-if="sidebarCollapsed || isSectionExpanded(group.key)"
              :id="`nav-${group.key}`"
              class="space-y-0.5"
            >
              <template v-for="item in group.items" :key="item.path">
                <!-- Expandable Parent Item -->
                <div v-if="item.children && item.children.length > 0" class="space-y-0.5">
                  <button
                    type="button"
                    :title="sidebarCollapsed ? item.label : undefined"
                    :aria-expanded="isItemExpanded(item)"
                    :class="[
                      'oh-focus-ring relative flex w-full items-center gap-2.5 rounded-oh-md px-2.5 py-1.5 text-[13px] transition-colors',
                      sidebarCollapsed ? 'justify-center px-0' : '',
                      isGroupActive(item)
                        ? 'bg-oh-selected font-medium text-oh-primary'
                        : 'text-oh-secondary hover:bg-oh-hover hover:text-oh-primary'
                    ]"
                    @click="toggleItemChildren(item)"
                  >
                    <span
                      v-if="isGroupActive(item)"
                      class="absolute bottom-1.5 left-0 top-1.5 w-[2px] rounded-r bg-[var(--oh-accent)]"
                      aria-hidden="true"
                    />
                    <span
                      v-if="item.iconSvg"
                      class="sidebar-svg-icon h-4 w-4 shrink-0"
                      aria-hidden="true"
                      v-html="sanitizeSvg(item.iconSvg)"
                    />
                    <Icon v-else :name="item.icon" size="sm" class="shrink-0" />
                    <span v-if="!sidebarCollapsed" class="min-w-0 flex-1 truncate text-left">
                      {{ item.label }}
                    </span>
                    <Icon
                      v-if="!sidebarCollapsed"
                      name="chevronDown"
                      size="xs"
                      class="shrink-0 text-oh-muted transition-transform"
                      :class="{ '-rotate-90': !isItemExpanded(item) }"
                    />
                  </button>

                  <div
                    v-if="!sidebarCollapsed && isItemExpanded(item)"
                    class="ml-4 space-y-0.5 border-l border-oh-subtle pl-2"
                  >
                    <router-link
                      v-for="child in item.children"
                      :id="tourIds[child.path]"
                      :key="child.path"
                      :to="child.path"
                      :aria-current="isActive(child.path) ? 'page' : undefined"
                      :class="[
                        'oh-focus-ring relative flex items-center gap-2 rounded-oh-md px-2.5 py-1.5 text-[12.5px] transition-colors',
                        isActive(child.path)
                          ? 'bg-oh-selected font-medium text-oh-primary'
                          : 'text-oh-secondary hover:bg-oh-hover hover:text-oh-primary'
                      ]"
                      @click="handleMenuItemClick(child.path)"
                    >
                      <span class="min-w-0 truncate">{{ child.label }}</span>
                    </router-link>
                  </div>
                </div>

                <!-- Direct Route Item -->
                <router-link
                  v-else
                  :id="item.tourId || tourIds[item.path]"
                  :to="item.path"
                  :aria-current="isActive(item.path) ? 'page' : undefined"
                  :aria-label="item.label"
                  :title="sidebarCollapsed ? item.label : undefined"
                  :data-tour="item.tourId || (item.path === '/keys' ? 'sidebar-my-keys' : undefined)"
                  :class="[
                    'oh-focus-ring relative flex items-center gap-2.5 rounded-oh-md px-2.5 py-1.5 text-[13px] transition-colors',
                    sidebarCollapsed ? 'justify-center px-0' : '',
                    isActive(item.path)
                      ? 'bg-oh-selected font-medium text-oh-primary'
                      : 'text-oh-secondary hover:bg-oh-hover hover:text-oh-primary'
                  ]"
                  @click="handleMenuItemClick(item.path)"
                >
                  <span
                    v-if="isActive(item.path)"
                    class="absolute bottom-1.5 left-0 top-1.5 w-[2px] rounded-r bg-[var(--oh-accent)]"
                    aria-hidden="true"
                  />
                  <span
                    v-if="item.iconSvg"
                    class="sidebar-svg-icon h-4 w-4 shrink-0"
                    aria-hidden="true"
                    v-html="sanitizeSvg(item.iconSvg)"
                  />
                  <Icon v-else :name="item.icon" size="sm" class="shrink-0" />
                  <span v-if="!sidebarCollapsed" class="min-w-0 truncate">{{ item.label }}</span>
                </router-link>
              </template>
            </div>
          </section>

          <!-- Zero-Filter Empty State -->
          <div
            v-if="visibleGroups.length === 0"
            role="status"
            class="px-3 py-6 text-center text-xs text-oh-muted"
          >
            <p>{{ isZh ? '没有匹配的导航页面' : 'No matching navigation items' }}</p>
            <button
              v-if="sidebarSearchQuery"
              type="button"
              class="oh-focus-ring mt-2 rounded-oh-sm border border-oh-default bg-oh-surface px-2.5 py-1 font-mono text-[11px] text-oh-secondary hover:text-oh-primary"
              @click="sidebarSearchQuery = ''"
            >
              {{ isZh ? '清除筛选' : 'Clear filter' }}
            </button>
          </div>
        </div>
      </nav>

      <div
        v-if="hasBottomOverflow"
        data-testid="sidebar-scroll-fade"
        aria-hidden="true"
        class="pointer-events-none absolute inset-x-0 bottom-0 h-7 bg-gradient-to-t from-[var(--oh-bg-subtle)] to-transparent"
      />
    </div>

    <!-- Footer Rail: Tier Switcher + Brand Status + Collapse -->
    <div class="shrink-0 border-t border-oh-default p-2.5">
      <router-link
        v-if="isAdmin && !sidebarCollapsed"
        :to="activeTier === 'admin' ? '/dashboard' : '/admin/dashboard'"
        class="oh-focus-ring mb-2 flex items-center justify-between rounded-oh-md border border-oh-default bg-oh-surface px-2.5 py-1.5 font-mono text-[11px] font-medium text-oh-secondary transition-colors hover:border-oh-strong hover:text-oh-primary"
        @click="closeMobile"
      >
        <span>
          {{ activeTier === 'admin' ? (isZh ? '切换至开发者控制台' : 'Developer Console') : (isZh ? '切换至管理控制台' : 'Admin Console') }}
        </span>
        <span aria-hidden="true">→</span>
      </router-link>

      <div
        v-if="!sidebarCollapsed"
        class="sidebar-brand mb-1.5 flex items-center gap-2 px-2 text-[11px]"
        :data-site-logo="siteLogo ? 'custom' : 'default'"
      >
        <img
          v-if="siteLogo"
          :src="siteLogo"
          :alt="siteName"
          class="h-4 w-4 shrink-0 rounded-oh-xs object-contain"
        />
        <span class="min-w-0 truncate font-semibold text-oh-secondary">{{ siteName }}</span>
        <span class="ml-auto inline-flex items-center gap-1 font-mono text-[10px] text-oh-muted">
          <span class="inline-block h-1.5 w-1.5 rounded-full bg-[var(--oh-ok-fg)]" />
          <span>{{ activeTier === 'admin' ? 'ADMIN' : 'GATEWAY' }}</span>
        </span>
      </div>

      <div class="flex items-center justify-between gap-1" :class="{ 'flex-col': sidebarCollapsed }">
        <button
          type="button"
          :aria-label="isDark ? t('nav.lightMode') : t('nav.darkMode')"
          class="oh-focus-ring flex min-w-0 flex-1 items-center gap-2 rounded-oh-md px-2 py-1.5 text-xs text-oh-secondary transition-colors hover:bg-oh-hover hover:text-oh-primary"
          :class="{ 'justify-center px-0': sidebarCollapsed }"
          @click="toggleDarkMode"
        >
          <Icon :name="isDark ? 'sun' : 'moon'" size="sm" />
          <span v-if="!sidebarCollapsed" class="truncate">
            {{ isDark ? t('nav.lightMode') : t('nav.darkMode') }}
          </span>
        </button>
        <button
          type="button"
          :aria-label="sidebarCollapsed ? t('nav.expand') : t('nav.collapse')"
          class="oh-focus-ring hidden h-7 w-7 shrink-0 items-center justify-center rounded-oh-md text-oh-secondary transition-colors hover:bg-oh-hover hover:text-oh-primary lg:inline-flex"
          @click="appStore.toggleSidebar()"
        >
          <Icon :name="sidebarCollapsed ? 'chevronRight' : 'chevronLeft'" size="sm" />
        </button>
      </div>
    </div>
  </aside>

  <!-- Mobile Drawer Backdrop -->
  <button
    v-if="mobileOpen"
    type="button"
    data-testid="mobile-sidebar-backdrop"
    class="fixed inset-x-0 bottom-0 top-14 z-20 bg-black/50 lg:hidden"
    :aria-label="isZh ? '关闭导航菜单' : 'Close navigation drawer'"
    @click="closeMobile"
  />
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAdminSettingsStore, useAppStore, useAuthStore, useOnboardingStore } from '@/stores'
import { uniqueNavigationItems } from '@/router/navigationItems'
import {
  ADMIN_CONSOLE_SECTIONS,
  CORE_NAVIGATION_ITEMS,
  USER_CONSOLE_SECTIONS,
  getRegisteredNavigationItems,
  type NavSectionId,
  type ShellNavItemSpec
} from '@/router/navigationRegistry'
import type { RouteFeatureFlag } from '@/router/routePolicy'
import Icon from '@/components/icons/Icon.vue'
import { sanitizeSvg } from '@/utils/sanitize'
import { sanitizeUrl } from '@/utils/url'
import { FeatureFlags, makeSidebarFlag } from '@/utils/featureFlags'
import { resolveSiteBillingMode } from '@/utils/siteBillingMode'
import { useBatchImageAccess } from '@/composables/useBatchImageAccess'
import { useThemeDirection } from '@/composables/useThemeDirection'

type NavDomain = 'user' | 'admin'
type IconName = InstanceType<typeof Icon>['$props']['name']

interface NavItem {
  path: string
  label: string
  icon: IconName
  iconSvg?: string
  tourId?: string
  hideInSimpleMode?: boolean
  featureFlag?: () => boolean | undefined
  expandOnly?: boolean
  children?: NavItem[]
}

interface NavGroup {
  key: string
  domain: NavDomain
  section: NavSectionId
  label: string
  items: NavItem[]
}

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const appStore = useAppStore()
const authStore = useAuthStore()
const onboardingStore = useOnboardingStore()
const adminSettingsStore = useAdminSettingsStore()
const { refreshBatchImageAccess } = useBatchImageAccess()
const { isDark, toggleDarkMode } = useThemeDirection()

const isZh = computed(() => String(locale.value || 'zh').toLowerCase().startsWith('zh'))
const mobileOpen = computed(() => Boolean(appStore.mobileOpen))
const sidebarCollapsed = computed(() => Boolean(appStore.sidebarCollapsed) && !mobileOpen.value)
const isAdmin = computed(() => Boolean(authStore.isAdmin || authStore.user?.role === 'admin'))
const activeTier = computed<NavDomain>(() =>
  route.path?.startsWith('/admin') && isAdmin.value ? 'admin' : 'user'
)

const sidebarNavRef = ref<HTMLElement | null>(null)
const groupExpandOverrides = ref<Map<string, boolean>>(new Map())
const SIDEBAR_SECTIONS_STORAGE_KEY = 'omnihive.sidebar.sections.v1'

function readPersistedSectionOverrides(): Map<string, boolean> {
  try {
    if (typeof localStorage === 'undefined') return new Map()
    const raw = localStorage.getItem(SIDEBAR_SECTIONS_STORAGE_KEY)
    if (!raw) return new Map()
    const parsed = JSON.parse(raw) as Record<string, unknown>
    const entries: Array<[string, boolean]> = []
    for (const [k, v] of Object.entries(parsed || {})) {
      if (typeof v === 'boolean') entries.push([k, v])
    }
    return new Map(entries)
  } catch {
    return new Map()
  }
}

function persistSectionOverrides(map: Map<string, boolean>): void {
  try {
    if (typeof localStorage === 'undefined') return
    localStorage.setItem(SIDEBAR_SECTIONS_STORAGE_KEY, JSON.stringify(Object.fromEntries(map.entries())))
  } catch {
    // ignore storage errors
  }
}

const sectionExpandOverrides = ref<Map<string, boolean>>(readPersistedSectionOverrides())
const hasBottomOverflow = ref(false)
let navResizeObserver: ResizeObserver | null = null
const sidebarSearchQuery = ref('')
const tourRunning = computed(() => Boolean(onboardingStore.getDriverInstance?.()))

const mobileQuery =
  typeof window !== 'undefined' && typeof window.matchMedia === 'function'
    ? window.matchMedia('(max-width: 1023px)')
    : null

const siteName = computed(() => appStore.brandConfig?.siteName || appStore.siteName || 'OmniHive')
const siteLogo = computed(() =>
  sanitizeUrl(appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true })
)

const flagSubscription = makeSidebarFlag(FeatureFlags.subscription)
const flagPayment = makeSidebarFlag(FeatureFlags.payment)
const flagChannelMonitor = makeSidebarFlag(FeatureFlags.channelMonitor)
const flagAvailableChannels = makeSidebarFlag(FeatureFlags.availableChannels)
const flagAffiliate = makeSidebarFlag(FeatureFlags.affiliate)
const flagRiskControl = makeSidebarFlag(FeatureFlags.riskControl)
const flagOpsMonitoring = () => adminSettingsStore.opsMonitoringEnabled
const flagAdminPayment = () => adminSettingsStore.paymentEnabled

const purchaseNavLabel = computed(() => {
  switch (resolveSiteBillingMode(appStore.cachedPublicSettings)) {
    case 'recharge_only': return t('nav.recharge')
    case 'subscription_only': return t('nav.subscribe')
    default: return t('nav.buySubscription')
  }
})

const tourIds: Record<string, string> = {
  '/admin/accounts': 'sidebar-channel-manage',
  '/admin/groups': 'sidebar-group-manage',
  '/admin/redeem': 'sidebar-wallet'
}

function label(key: string, chinese: string): string {
  return isZh.value ? chinese : t(key)
}

const customMenuItemsForUser = computed(() =>
  (appStore.cachedPublicSettings?.custom_menu_items ?? [])
    .filter((item) => item.visibility === 'user')
    .sort((first, second) => first.sort_order - second.sort_order)
)

const customMenuItemsForAdmin = computed(() =>
  (adminSettingsStore.customMenuItems ?? [])
    .filter((item) => item.visibility === 'admin')
    .sort((first, second) => first.sort_order - second.sort_order)
)

const sidebarFeatureFlags: Partial<Record<RouteFeatureFlag, () => boolean | undefined>> = {
  subscription: flagSubscription,
  payment: flagPayment,
  channelMonitor: flagChannelMonitor,
  availableChannels: flagAvailableChannels,
  affiliate: flagAffiliate,
  riskControl: flagRiskControl,
  opsMonitoring: flagOpsMonitoring,
  adminPayment: flagAdminPayment
}

const registrySurfaceOverrides = computed<Record<string, Partial<NavItem>>>(() => ({
  '/purchase': { path: '/purchase', label: purchaseNavLabel.value, hideInSimpleMode: true, featureFlag: flagPayment },
  '/subscriptions': { path: '/subscriptions', label: t('nav.mySubscriptions'), hideInSimpleMode: true, featureFlag: flagSubscription },
  '/admin/subscriptions': { path: '/admin/subscriptions', label: label('nav.subscriptions', '用户订阅分配'), hideInSimpleMode: true, featureFlag: flagSubscription },
  '/admin/security-audit': {
    path: '/admin/security-audit',
    label: t('nav.securityAudit'),
    icon: 'shield',
    expandOnly: true,
    featureFlag: flagRiskControl,
    children: [
      { path: '/admin/risk-control', label: label('nav.contentModeration', '风控策略中心'), icon: 'shield' },
      { path: '/admin/prompt-audit', label: t('nav.promptAudit'), icon: 'shield' }
    ]
  }
}))

function resolveRegistrySpecLabel(spec: ShellNavItemSpec): string {
  if (spec.path === '/purchase') return purchaseNavLabel.value
  if (spec.labelKey) return t(spec.labelKey)
  return isZh.value ? spec.fallbackLabelZh : spec.fallbackLabelEn
}

function mapRegistrySpecToNavItem(spec: ShellNavItemSpec): NavItem {
  const override = registrySurfaceOverrides.value[spec.path]
  const children = spec.children?.filter((child) => !child.hiddenInSidebar).map(mapRegistrySpecToNavItem)
  return {
    path: spec.path,
    label: override?.label ?? resolveRegistrySpecLabel(spec),
    icon: (spec.icon as IconName) || 'grid',
    iconSvg: spec.iconSvg,
    tourId: spec.tourId,
    hideInSimpleMode: spec.hideInSimpleMode,
    featureFlag: override?.featureFlag ?? (spec.featureFlag ? sidebarFeatureFlags[spec.featureFlag] : undefined),
    expandOnly: spec.expandOnly,
    ...(children && children.length > 0 ? { children } : {})
  }
}

function registryItemsForSection(domain: NavDomain, section: NavSectionId): NavItem[] {
  const allSpecs: ShellNavItemSpec[] = [
    ...CORE_NAVIGATION_ITEMS,
    ...getRegisteredNavigationItems()
  ]
  const seenPaths = new Set<string>()
  const matching = allSpecs
    .filter((item) => item.tier === domain && item.section === section && !item.hiddenInSidebar)
    .sort((a, b) => (a.order ?? 100) - (b.order ?? 100))
    .filter((item) => {
      if (seenPaths.has(item.path)) return false
      seenPaths.add(item.path)
      return true
    })
    .map(mapRegistrySpecToNavItem)

  if (domain === 'user' && section === 'settings') {
    return [
      ...matching,
      ...customMenuItemsForUser.value.map((item): NavItem => ({
        path: `/custom/${item.id}`,
        label: item.label,
        icon: 'globe',
        iconSvg: item.icon_svg
      }))
    ]
  }

  if (domain === 'admin' && section === 'settings') {
    return [
      ...matching,
      ...customMenuItemsForAdmin.value.map((item): NavItem => ({
        path: `/custom/${item.id}`,
        label: item.label,
        icon: 'globe',
        iconSvg: item.icon_svg
      }))
    ]
  }

  return matching
}

const navigationGroups = computed((): NavGroup[] => [
  ...USER_CONSOLE_SECTIONS.map((sec): NavGroup => ({
    key: `user-${sec.id}`,
    domain: 'user',
    section: sec.id,
    label: isZh.value ? sec.eyebrowZh : sec.eyebrowEn,
    items: registryItemsForSection('user', sec.id)
  })),
  ...ADMIN_CONSOLE_SECTIONS.map((sec): NavGroup => ({
    key: `admin-${sec.id}`,
    domain: 'admin',
    section: sec.id,
    label: isZh.value ? sec.eyebrowZh : sec.eyebrowEn,
    items: registryItemsForSection('admin', sec.id)
  }))
])

function visibleItems(items: NavItem[]): NavItem[] {
  return items.flatMap((item) => {
    if (item.featureFlag?.() === false || (authStore.isSimpleMode && item.hideInSimpleMode)) {
      return []
    }
    if (item.children) {
      const children = visibleItems(item.children)
      return children.length > 0 ? [{ ...item, children }] : []
    }
    return [item]
  })
}

const availableGroups = computed(() =>
  navigationGroups.value
    .filter((group) => group.domain === activeTier.value)
    .filter((group) => group.domain !== 'user' || isAdmin.value || !appStore.backendModeEnabled)
    .map((group) => ({
      ...group,
      items: uniqueNavigationItems(visibleItems(group.items), router)
    }))
)

const visibleGroups = computed(() => {
  const query = tourRunning.value ? '' : sidebarSearchQuery.value.trim().toLowerCase()
  return availableGroups.value
    .map((group) => {
      if (!query) return group
      const items = group.items.flatMap((item) => {
        const haystack = `${group.label} ${item.label} ${item.path}`.toLowerCase()
        if (haystack.includes(query)) return [item]
        if (item.children) {
          const matchingChildren = item.children.filter((child) =>
            `${child.label} ${child.path}`.toLowerCase().includes(query)
          )
          if (matchingChildren.length > 0) {
            return [{ ...item, children: matchingChildren }]
          }
        }
        return []
      })
      return { ...group, items }
    })
    .filter((group) => group.items.length > 0)
})

const flatActiveCandidates = computed(() =>
  availableGroups.value.flatMap((group) =>
    group.items.flatMap((item) => (item.children ? [item, ...item.children] : [item]))
  )
)

const activePath = computed(
  () =>
    flatActiveCandidates.value
      .filter((item) => !item.expandOnly && (route.path === item.path || route.path.startsWith(`${item.path}/`)))
      .sort((first, second) => second.path.length - first.path.length)[0]?.path
)

function isActive(path: string): boolean {
  return activePath.value === path
}

function isGroupActive(item: NavItem): boolean {
  if (isActive(item.path)) return true
  return Boolean(item.children?.some((child) => isActive(child.path)))
}

function isItemExpanded(item: NavItem): boolean {
  if (sidebarSearchQuery.value.trim() || tourRunning.value) return true
  const override = groupExpandOverrides.value.get(item.path)
  if (override !== undefined) return override
  return isGroupActive(item)
}

function toggleItemChildren(item: NavItem): void {
  if (sidebarCollapsed.value) {
    const targetPath = item.children?.[0]?.path || item.path
    void router.push(targetPath)
    return
  }
  const next = new Map(groupExpandOverrides.value)
  next.set(item.path, !isItemExpanded(item))
  groupExpandOverrides.value = next
  void nextTick(updateSidebarScrollShadow)
}

function isSectionExpanded(key: string): boolean {
  if (sidebarSearchQuery.value.trim() || tourRunning.value) return true
  return sectionExpandOverrides.value.get(key) ?? true
}

function toggleSection(key: string): void {
  const next = new Map(sectionExpandOverrides.value)
  next.set(key, !isSectionExpanded(key))
  sectionExpandOverrides.value = next
  persistSectionOverrides(next)
  void nextTick(updateSidebarScrollShadow)
}

function ensureActiveSectionExpanded(): void {
  const currentActive = activePath.value
  if (!currentActive) return
  const owningGroup = availableGroups.value.find((group) =>
    group.items.some(
      (item) =>
        item.path === currentActive ||
        Boolean(item.children?.some((child) => child.path === currentActive))
    )
  )
  if (owningGroup && sectionExpandOverrides.value.get(owningGroup.key) === false) {
    const next = new Map(sectionExpandOverrides.value)
    next.set(owningGroup.key, true)
    sectionExpandOverrides.value = next
    persistSectionOverrides(next)
  }
}

function updateSidebarScrollShadow(): void {
  const nav = sidebarNavRef.value
  if (!nav) {
    hasBottomOverflow.value = false
    return
  }
  hasBottomOverflow.value = nav.scrollHeight - nav.scrollTop - nav.clientHeight > 6
}

function scrollActiveItemIntoView(): void {
  const nav = sidebarNavRef.value
  if (!nav) return
  const activeEl = nav.querySelector<HTMLElement>('[aria-current="page"]')
  if (activeEl && typeof activeEl.scrollIntoView === 'function') {
    activeEl.scrollIntoView({ block: 'nearest' })
  }
  updateSidebarScrollShadow()
}

function closeMobile(): void {
  appStore.setMobileOpen(false)
}

function handleMenuItemClick(itemPath: string): void {
  if (sidebarNavRef.value) {
    appStore.sidebarScrollTop = sidebarNavRef.value.scrollTop
  }
  if (mobileOpen.value) closeMobile()
  const selector =
    itemPath === '/keys'
      ? '[data-tour="sidebar-my-keys"]'
      : tourIds[itemPath]
        ? `#${tourIds[itemPath]}`
        : undefined
  if (selector && onboardingStore.isCurrentStep?.(selector)) {
    onboardingStore.nextStep?.(500)
  }
}

function updateMobile(event: MediaQueryListEvent): void {
  if (!event.matches && mobileOpen.value) closeMobile()
}

function handleKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape' && mobileOpen.value && !tourRunning.value) {
    closeMobile()
  }
}

watch(
  [isAdmin, () => authStore.user?.id, () => authStore.sessionRevision],
  ([admin]) => {
    if (admin) void adminSettingsStore.fetch()
  },
  { immediate: true }
)

watch(
  () => route.path,
  () => {
    if (mobileOpen.value) closeMobile()
    ensureActiveSectionExpanded()
    void nextTick(() => {
      scrollActiveItemIntoView()
    })
  }
)

watch(
  [sidebarCollapsed, () => visibleGroups.value.length],
  () => {
    void nextTick(updateSidebarScrollShadow)
  }
)

onMounted(() => {
  void refreshBatchImageAccess()
  mobileQuery?.addEventListener('change', updateMobile)
  window.addEventListener('keydown', handleKeydown)
  ensureActiveSectionExpanded()
  void nextTick(() => {
    if (sidebarNavRef.value) {
      sidebarNavRef.value.scrollTop = appStore.sidebarScrollTop
      if (typeof ResizeObserver !== 'undefined') {
        navResizeObserver = new ResizeObserver(() => updateSidebarScrollShadow())
        navResizeObserver.observe(sidebarNavRef.value)
      }
    }
    scrollActiveItemIntoView()
  })
})

onBeforeUnmount(() => {
  mobileQuery?.removeEventListener('change', updateMobile)
  window.removeEventListener('keydown', handleKeydown)
  navResizeObserver?.disconnect()
  navResizeObserver = null
  if (sidebarNavRef.value) appStore.sidebarScrollTop = sidebarNavRef.value.scrollTop
})
</script>

<style scoped>
.sidebar-svg-icon {
  color: currentColor;
  display: block;
}

.sidebar-svg-icon :deep(svg) {
  width: 100%;
  height: 100%;
  display: block;
}

.sidebar-brand {
  min-height: 24px;
}
</style>
