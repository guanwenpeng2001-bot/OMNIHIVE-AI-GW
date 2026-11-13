<template>
  <aside
    id="sidebar"
    class="sidebar top-14 h-[calc(100vh-3.5rem)]"
    :class="[
      sidebarCollapsed ? 'sidebar-compact' : 'sidebar-expanded',
      { '-translate-x-full lg:translate-x-0': !mobileOpen }
    ]"
    :inert="!mobileOpen && isMobile"
    aria-label="页面导航"
    @keydown.esc="closeMobile"
  >
    <nav ref="sidebarNavRef" class="sidebar-nav">
      <div id="navTree" class="space-y-4 text-sm">
        <div v-if="!sidebarCollapsed" class="nav-controls mb-2 border-b pb-2.5">
          <div class="relative">
            <Icon name="search" size="xs" class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 opacity-45" />
            <input
              id="navQuickSearch"
              v-model="sidebarSearchQuery"
              type="search"
              class="input !py-1.5 !pl-7 !pr-2 !text-xs"
              placeholder="快速筛选页面…"
              aria-label="筛选侧栏页面"
              @keydown.esc.stop="sidebarSearchQuery = ''"
            />
          </div>
        </div>
        <section
          v-for="group in visibleGroups"
          :key="group.key"
          class="sidebar-section nav-sec-block space-y-0.5"
          :aria-label="group.label"
        >
          <span v-if="isAdmin && group.key === 'user-overview'" class="sr-only">{{ t('nav.myAccount') }}</span>
          <button
            v-if="!sidebarCollapsed"
            type="button"
            class="nav-group-title flex w-full items-center justify-between gap-2 px-2.5 pb-1 pt-2 text-left"
            :aria-expanded="isGroupExpanded(group)"
            :aria-controls="`nav-${group.key}`"
            @click="toggleGroup(group)"
          >
            <span class="truncate">{{ group.label }}</span>
            <span class="flex shrink-0 items-center gap-1.5">
              <span class="font-mono text-[10px] opacity-45">{{ group.items.length }}</span>
              <Icon name="chevronDown" size="xs" class="opacity-45 transition-transform" :class="{ '-rotate-90': !isGroupExpanded(group) }" />
            </span>
          </button>
          <div v-else class="mx-2 my-2 border-t opacity-40" :title="group.label"></div>
          <div v-if="sidebarCollapsed || isGroupExpanded(group)" :id="`nav-${group.key}`" class="space-y-0.5">
            <router-link
              v-for="item in group.items"
              :id="tourIds[item.path]"
              :key="item.path"
              :to="item.path"
              class="sidebar-link tree-item"
              :class="{ 'sidebar-link-active active': isActive(item.path), 'sidebar-link-collapsed': sidebarCollapsed }"
              :aria-current="isActive(item.path) ? 'page' : undefined"
              :aria-label="item.label"
              :title="sidebarCollapsed ? item.label : undefined"
              :data-tour="item.path === '/keys' ? 'sidebar-my-keys' : undefined"
              @click="handleMenuItemClick(item.path)"
            >
              <span v-if="item.iconSvg" class="sidebar-svg-icon h-4 w-4 shrink-0" aria-hidden="true" v-html="sanitizeSvg(item.iconSvg)"></span>
              <Icon v-else :name="item.icon" size="sm" class="nav-ic shrink-0 opacity-65" />
              <span v-if="!sidebarCollapsed" class="min-w-0 truncate">{{ item.label }}</span>
            </router-link>
          </div>
        </section>
        <div v-if="visibleGroups.length === 0" class="px-2.5 py-6 text-center text-xs opacity-60" role="status">
          没有匹配的页面
          <button v-if="sidebarSearchQuery" type="button" class="mt-2 block w-full underline" @click="sidebarSearchQuery = ''">清除筛选</button>
        </div>
      </div>
    </nav>
    <div class="sidebar-footer shrink-0 border-t p-3">
      <div
        v-if="!sidebarCollapsed"
        class="sidebar-brand mb-2 flex items-center gap-2 px-2 text-[11px]"
        :data-site-logo="siteLogo ? 'custom' : 'default'"
      >
        <svg width="16" height="16" viewBox="0 0 32 32" fill="none" aria-hidden="true" class="shrink-0">
          <path d="M16 2 28.1 9v14L16 30 3.9 23V9Z" fill="var(--primary, #16161a)" />
          <path d="M16 5.6 25 10.8v10.4L16 26.4 7 21.2V10.8Z" stroke="var(--brand, #f59e0b)" stroke-width="1.8" />
          <path d="M16 10.2 21 13.1v5.8L16 21.8l-5-2.9v-5.8Z" fill="var(--brand, #f59e0b)" />
          <circle cx="16" cy="16" r="2.1" fill="var(--primary, #16161a)" />
        </svg>
        <span class="min-w-0 truncate font-semibold opacity-75">OmniHive</span>
        <span class="ml-auto inline-flex items-center gap-1 font-mono text-[10px] opacity-60">
          <span class="inline-block h-1.5 w-1.5 rounded-full" style="background: var(--ok)"></span>
          <span>GATEWAY</span>
        </span>
      </div>
      <div class="flex items-center justify-between gap-1" :class="{ 'flex-col': sidebarCollapsed }">
        <button type="button" class="sidebar-link tree-item min-w-0 flex-1" :aria-label="isDark ? t('nav.lightMode') : t('nav.darkMode')" @click="toggleDarkMode">
          <Icon :name="isDark ? 'sun' : 'moon'" size="sm" />
          <span v-if="!sidebarCollapsed" class="truncate text-xs">{{ isDark ? t('nav.lightMode') : t('nav.darkMode') }}</span>
        </button>
        <button type="button" class="btn-ghost btn-icon shrink-0 rounded-md" :aria-label="sidebarCollapsed ? t('nav.expand') : t('nav.collapse')" @click="appStore.toggleSidebar()">
          <Icon :name="sidebarCollapsed ? 'chevronRight' : 'chevronLeft'" size="sm" />
        </button>
      </div>
    </div>
  </aside>
  <transition name="fade">
    <button v-if="mobileOpen" type="button" class="fixed inset-x-0 bottom-0 top-14 z-20 bg-black/40 lg:hidden" aria-label="关闭导航" @click="closeMobile"></button>
  </transition>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAdminSettingsStore, useAppStore, useAuthStore, useOnboardingStore } from '@/stores'
import { uniqueNavigationItems } from '@/router/navigationItems'
import Icon from '@/components/icons/Icon.vue'
import { sanitizeSvg } from '@/utils/sanitize'
import { sanitizeUrl } from '@/utils/url'
import { FeatureFlags, makeSidebarFlag } from '@/utils/featureFlags'
import { resolveSiteBillingMode } from '@/utils/siteBillingMode'
import { useBatchImageAccess } from '@/composables/useBatchImageAccess'
import { useThemeDirection } from '@/composables/useThemeDirection'

type NavDomain = 'user' | 'admin' | 'public'
interface NavItem {
  path: string
  label: string
  icon: InstanceType<typeof Icon>['$props']['name']
  iconSvg?: string
  hideInSimpleMode?: boolean
  featureFlag?: () => boolean | undefined
  expandOnly?: boolean
  children?: NavItem[]
}
interface NavGroup {
  key: string
  domain: NavDomain
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
const mobileOpen = computed(() => appStore.mobileOpen)
const sidebarCollapsed = computed(() => appStore.sidebarCollapsed && !mobileOpen.value)
const isAdmin = computed(() => authStore.isAdmin)
const sidebarNavRef = ref<HTMLElement | null>(null)
const groupExpandOverrides = ref<Map<string, boolean>>(new Map())
const sidebarSearchQuery = ref('')
const tourRunning = computed(() => Boolean(onboardingStore.getDriverInstance?.()))
const mobileQuery = window.matchMedia('(max-width: 1023px)')
const isMobile = ref(mobileQuery.matches)
const siteLogo = computed(() => sanitizeUrl(appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const flagSubscription = makeSidebarFlag(FeatureFlags.subscription)
const flagPayment = makeSidebarFlag(FeatureFlags.payment)
const flagChannelMonitor = makeSidebarFlag(FeatureFlags.channelMonitor)
const flagAvailableChannels = makeSidebarFlag(FeatureFlags.availableChannels)
const flagAffiliate = makeSidebarFlag(FeatureFlags.affiliate)
const flagRiskControl = makeSidebarFlag(FeatureFlags.riskControl)
const flagPluginManagement = makeSidebarFlag(FeatureFlags.pluginManagement)
const flagModelPlaza = makeSidebarFlag(FeatureFlags.modelPlaza)
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

function label(key: string, chinese: string) {
  return locale.value.startsWith('zh') ? chinese : t(key)
}

const customMenuItemsForUser = computed(() => (appStore.cachedPublicSettings?.custom_menu_items ?? [])
  .filter(item => item.visibility === 'user').sort((first, second) => first.sort_order - second.sort_order))
const customMenuItemsForAdmin = computed(() => adminSettingsStore.customMenuItems
  .filter(item => item.visibility === 'admin').sort((first, second) => first.sort_order - second.sort_order))

const navigationGroups = computed((): NavGroup[] => [
  { key: 'user-overview', domain: 'user', label: isAdmin.value ? '个人控制台 · 概览' : '概览', items: [
    { path: '/dashboard', label: label('nav.dashboard', '控制台首页'), icon: 'grid' },
    { path: '/workspaces', label: label('workspaces.title', '工作空间'), icon: 'users' },
    { path: '/help', label: t('nav.helpGuide'), icon: 'book' }
  ] },
  { key: 'user-usage', domain: 'user', label: isAdmin.value ? '个人控制台 · 密钥与用量' : '密钥与用量', items: [
    { path: '/keys', label: t('nav.apiKeys'), icon: 'key' },
    { path: '/usage', label: label('nav.usage', '用量记录'), icon: 'chart', hideInSimpleMode: true }
  ] },
  { key: 'user-resources', domain: 'user', label: isAdmin.value ? '个人控制台 · 资源' : '资源', items: [
    { path: '/batch-image', label: label('nav.batchImage', '批量生图'), icon: 'sparkles', hideInSimpleMode: true },
    { path: '/available-channels', label: t('nav.availableChannels'), icon: 'server', hideInSimpleMode: true, featureFlag: flagAvailableChannels },
    { path: '/monitor', label: t('nav.channelStatus'), icon: 'chart', featureFlag: flagChannelMonitor },
    ...customMenuItemsForUser.value.map((item): NavItem => ({ path: `/custom/${item.id}`, label: item.label, icon: 'globe', iconSvg: item.icon_svg }))
  ] },
  { key: 'user-account', domain: 'user', label: isAdmin.value ? '个人控制台 · 账户' : '账户', items: [
    { path: '/subscriptions', label: t('nav.mySubscriptions'), icon: 'creditCard', hideInSimpleMode: true, featureFlag: flagSubscription },
    { path: '/purchase', label: purchaseNavLabel.value, icon: 'dollar', hideInSimpleMode: true, featureFlag: flagPayment },
    { path: '/orders', label: t('nav.myOrders'), icon: 'document', hideInSimpleMode: true, featureFlag: flagPayment },
    { path: '/redeem', label: t('nav.redeem'), icon: 'gift', hideInSimpleMode: true },
    { path: '/affiliate', label: t('nav.affiliate'), icon: 'users', hideInSimpleMode: true, featureFlag: flagAffiliate },
    { path: '/settings/mcp', label: 'MCP 授权', icon: 'link' },
    { path: '/profile', label: t('nav.profile'), icon: 'user' }
  ] },
  { key: 'admin-operations', domain: 'admin', label: '管理后台 · 运行', items: [
    { path: '/admin/dashboard', label: label('nav.dashboard', '总览仪表盘'), icon: 'grid' },
    { path: '/admin/ops', label: label('nav.ops', '运维实时监控'), icon: 'chart', featureFlag: flagOpsMonitoring },
    { path: '/admin/channels/monitor', label: t('nav.channelMonitor'), icon: 'chart', hideInSimpleMode: true, featureFlag: flagChannelMonitor },
    { path: '/admin/audit-logs', label: label('nav.auditLogs', '审计日志'), icon: 'shield', hideInSimpleMode: true },
    { path: '/admin/usage', label: label('nav.usage', '用量记录'), icon: 'chart' }
  ] },
  { key: 'admin-channels', domain: 'admin', label: '管理后台 · 渠道与账户', items: [
    { path: '/admin/channels/pricing', label: t('nav.channelPricing'), icon: 'dollar', hideInSimpleMode: true },
    { path: '/admin/accounts', label: label('nav.accounts', '上游账号'), icon: 'globe' },
    { path: '/admin/groups', label: t('nav.groups'), icon: 'grid' },
    { path: '/admin/proxies', label: label('nav.ipNodes', 'IP 代理'), icon: 'server' },
    { path: '/admin/proxy-groups', label: label('nav.ipGroups', 'IP 代理组'), icon: 'grid' },
    { path: '/admin/plugins', label: t('nav.plugins'), icon: 'cube', featureFlag: flagPluginManagement }
  ] },
  { key: 'admin-users', domain: 'admin', label: '管理后台 · 用户', items: [
    { path: '/admin/users', label: t('nav.users'), icon: 'users', hideInSimpleMode: true },
    { path: '/admin/announcements', label: t('nav.announcements'), icon: 'bell' },
    {
      path: '/admin/security-audit', label: t('nav.securityAudit'), icon: 'shield', expandOnly: true, featureFlag: flagRiskControl,
      children: [
        { path: '/admin/risk-control', label: label('nav.contentModeration', '风控中心'), icon: 'shield' },
        { path: '/admin/prompt-audit', label: t('nav.promptAudit'), icon: 'shield' }
      ]
    }
  ] },
  { key: 'admin-billing', domain: 'admin', label: '管理后台 · 结算', items: [
    { path: '/admin/orders', label: t('nav.orderManagement'), icon: 'document', hideInSimpleMode: true, featureFlag: flagAdminPayment },
    { path: '/admin/orders/dashboard', label: label('nav.paymentDashboard', '支付大盘'), icon: 'chart', hideInSimpleMode: true, featureFlag: flagAdminPayment },
    { path: '/admin/orders/plans', label: label('nav.paymentPlans', '订阅套餐'), icon: 'creditCard', hideInSimpleMode: true, featureFlag: flagAdminPayment },
    { path: '/admin/subscriptions', label: label('nav.subscriptions', '订阅分配'), icon: 'creditCard', hideInSimpleMode: true, featureFlag: flagSubscription },
    { path: '/admin/redeem', label: t('nav.redeemCodes'), icon: 'gift', hideInSimpleMode: true },
    { path: '/admin/promo-codes', label: t('nav.promoCodes'), icon: 'gift', hideInSimpleMode: true },
    { path: '/admin/affiliates/invites', label: label('nav.affiliateInviteRecords', '联盟邀请'), icon: 'users', hideInSimpleMode: true, featureFlag: flagAffiliate },
    { path: '/admin/affiliates/rebates', label: label('nav.affiliateRebateRecords', '联盟返利'), icon: 'dollar', hideInSimpleMode: true, featureFlag: flagAffiliate },
    { path: '/admin/affiliates/transfers', label: label('nav.affiliateTransferRecords', '提现审核'), icon: 'creditCard', hideInSimpleMode: true, featureFlag: flagAffiliate }
  ] },
  { key: 'admin-system', domain: 'admin', label: '管理后台 · 系统', items: [
    { path: '/admin/settings', label: t('nav.settings'), icon: 'cog' },
    { path: '/admin/backup', label: '备份与归档', icon: 'download' },
    ...customMenuItemsForAdmin.value.map((item): NavItem => ({ path: `/custom/${item.id}`, label: item.label, icon: 'globe', iconSvg: item.icon_svg }))
  ] },
  { key: 'public', domain: 'public', label: '公开层', items: [
    { path: '/home', label: '落地页 / 主页', icon: 'globe' },
    { path: '/login', label: '登录', icon: 'login' },
    { path: '/register', label: '注册', icon: 'userPlus' },
    { path: '/forgot-password', label: '忘记密码', icon: 'key' },
    { path: '/model-plaza', label: t('nav.modelPlaza'), icon: 'grid', featureFlag: flagModelPlaza },
    { path: '/key-usage', label: '公开用量查询', icon: 'chart' },
    { path: '/oauth/mcp/authorize', label: 'MCP 授权同意页', icon: 'shield' },
    { path: '/legal/terms', label: '法务文档', icon: 'book' },
    { path: '/setup', label: '安装向导', icon: 'cog' }
  ] }
])

function visibleItems(items: NavItem[]): NavItem[] {
  return items.flatMap(item => {
    if (item.featureFlag?.() === false || (authStore.isSimpleMode && item.hideInSimpleMode)) return []
    return item.children ? visibleItems(item.children) : [item]
  })
}

const availableGroups = computed(() => navigationGroups.value
  .filter(group => group.domain !== 'public')
  .filter(group => group.domain !== 'admin' || isAdmin.value)
  .filter(group => group.domain !== 'user' || isAdmin.value || !appStore.backendModeEnabled)
  .map(group => ({
    ...group,
    items: uniqueNavigationItems(visibleItems(group.items), router)
  }))
  .sort((first, second) => {
    const order = isAdmin.value
      ? ['admin-operations', 'admin-channels', 'admin-billing', 'admin-users', 'admin-system', 'user-overview', 'user-usage', 'user-resources', 'user-account']
      : ['user-overview', 'user-usage', 'user-resources', 'user-account']
    return order.indexOf(first.key) - order.indexOf(second.key)
  }))

const visibleGroups = computed(() => {
  const query = tourRunning.value ? '' : sidebarSearchQuery.value.trim().toLowerCase()
  return availableGroups.value
    .map(group => ({
      ...group,
      items: group.items.filter(item => !query || `${group.label} ${item.label} ${item.path}`.toLowerCase().includes(query))
    }))
    .filter(group => group.items.length > 0)
})
const activePath = computed(() => availableGroups.value.flatMap(group => group.items)
  .filter(item => route.path === item.path || route.path.startsWith(`${item.path}/`))
  .sort((first, second) => second.path.length - first.path.length)[0]?.path)

function isActive(path: string) {
  return activePath.value === path
}

function isGroupExpanded(group: NavGroup) {
  if (sidebarSearchQuery.value.trim() || tourRunning.value) return true
  return groupExpandOverrides.value.get(group.key) ?? true
}

function toggleGroup(group: NavGroup) {
  groupExpandOverrides.value.set(group.key, !isGroupExpanded(group))
}

function closeMobile() {
  appStore.setMobileOpen(false)
}

function handleMenuItemClick(itemPath: string) {
  if (mobileOpen.value) closeMobile()
  const selector = itemPath === '/keys' ? '[data-tour="sidebar-my-keys"]' : tourIds[itemPath] ? `#${tourIds[itemPath]}` : undefined
  if (selector && onboardingStore.isCurrentStep(selector)) onboardingStore.nextStep(500)
}

function updateMobile(event: MediaQueryListEvent) {
  isMobile.value = event.matches
  if (!event.matches && mobileOpen.value) closeMobile()
}

watch([isAdmin, () => authStore.user?.id, () => authStore.sessionRevision], ([admin]) => {
  if (admin) void adminSettingsStore.fetch()
}, { immediate: true })

watch(() => route.path, () => {
  if (mobileOpen.value) closeMobile()
})

onMounted(() => {
  void refreshBatchImageAccess()
  mobileQuery.addEventListener('change', updateMobile)
  void nextTick(() => {
    if (sidebarNavRef.value) sidebarNavRef.value.scrollTop = appStore.sidebarScrollTop
  })
})

onBeforeUnmount(() => {
  mobileQuery.removeEventListener('change', updateMobile)
  if (sidebarNavRef.value) appStore.sidebarScrollTop = sidebarNavRef.value.scrollTop
})
</script>

<style scoped>
.sidebar {
  top: 3.5rem;
  bottom: 0;
  z-index: 30;
  height: calc(100vh - 3.5rem);
  height: calc(100dvh - 3.5rem);
  background: var(--sidebar, #fcfcfd);
  border-color: var(--sidebar-border, #ececf1);
}

.sidebar-expanded {
  width: 15.5rem;
  max-width: calc(100vw - 2rem);
}

.sidebar-compact {
  width: 72px;
}

.sidebar-nav {
  min-height: 0;
  padding: 12px;
  overscroll-behavior: contain;
  scrollbar-width: thin;
}

.nav-controls,
.sidebar-footer {
  border-color: var(--sidebar-border, #ececf1);
}

#navScopeBar {
  background: var(--surface-2, #f4f4f5);
  border-color: var(--border, #e5e7eb);
}

.scope-button {
  color: var(--muted-foreground, #6f6f78);
  white-space: nowrap;
}

.scope-button.selected {
  background: var(--card, #fff);
  color: var(--foreground, #131316);
  box-shadow: 0 1px 2px rgb(0 0 0 / 8%);
}

.sidebar-section {
  margin-bottom: 0;
}

.nav-group-title {
  color: var(--muted-foreground, #6f6f78);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.025em;
}

.sidebar-link {
  gap: 8px;
  padding: 8px 10px;
  border-radius: 8px;
  font-size: 13px;
  font-weight: 400;
  color: var(--foreground, #131316);
}

.sidebar-link:hover {
  background: var(--secondary, #f4f4f5);
}

.sidebar-link-active {
  background: color-mix(in srgb, var(--brand, #f59e0b) 13%, var(--card, #fff));
  font-weight: 600;
}

.sidebar-link-collapsed {
  justify-content: center;
  padding: 10px;
}

.sidebar-brand {
  min-width: 0;
  white-space: nowrap;
}

.sidebar-svg-icon {
  color: currentColor;
  display: block;
}

.sidebar-svg-icon :deep(svg) {
  display: block;
  width: 1rem;
  height: 1rem;
}

.sidebar :is(button, a, input):focus-visible {
  outline: 2px solid var(--brand, #f59e0b);
  outline-offset: 2px;
}

@media (prefers-reduced-motion: reduce) {
  .sidebar,
  .sidebar * {
    transition: none;
  }
}
</style>
