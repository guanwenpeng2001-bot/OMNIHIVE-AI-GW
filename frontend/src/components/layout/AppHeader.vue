<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore, useAuthStore, useOnboardingStore } from '@/stores'
import { useAdminSettingsStore } from '@/stores/adminSettings'
import {
  useThemeDirection,
  type DensityPreference,
  type ThemeModePreference
} from '@/composables/useThemeDirection'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import VersionBadge from '@/components/common/VersionBadge.vue'
import SubscriptionProgressMini from '@/components/common/SubscriptionProgressMini.vue'
import AnnouncementBell from '@/components/common/AnnouncementBell.vue'
import Icon from '@/components/icons/Icon.vue'
import BrandMark from './BrandMark.vue'
import ScopeSwitcher from './ScopeSwitcher.vue'
import { sanitizeUrl } from '@/utils/url'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
import { resolveRouteMetaKeys } from '@/router/title'
import { resolveSiteBillingMode } from '@/utils/siteBillingMode'

const router = useRouter()
const route = useRoute()
const { t, locale } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const adminSettingsStore = useAdminSettingsStore()
const onboardingStore = useOnboardingStore()
const {
  themeMode,
  densityMode,
  isDark,
  setThemeMode,
  setDensityMode,
  openCommandPalette
} = useThemeDirection()

const isZh = computed(() => String(locale?.value || 'zh').toLowerCase().startsWith('zh'))
const user = computed(() => authStore.user)
const isAdmin = computed(() => Boolean(authStore.isAdmin || user.value?.role === 'admin'))
const isAdminRoute = computed(() => Boolean(route.path?.startsWith('/admin')))

const dropdownOpen = ref(false)
const dropdownRef = ref<HTMLElement | null>(null)
const appearanceOpen = ref(false)
const appearanceRef = ref<HTMLElement | null>(null)

const contactInfo = computed(() => appStore.contactInfo || '')
const docUrl = computed(() => sanitizeUrl(appStore.docUrl))
const siteName = computed(() => appStore.brandConfig?.siteName || appStore.siteName || 'OmniHive')
const siteLogo = computed(() => appStore.brandConfig?.siteLogo || appStore.siteLogo || '')
const showModelPlazaEntry = computed(
  () => Boolean(user.value) && isFeatureFlagEnabled(FeatureFlags.modelPlaza)
)
const subscriptionFeatureEnabled = computed(() =>
  isFeatureFlagEnabled(FeatureFlags.subscription)
)

const avatarUrl = computed(() => user.value?.avatar_url?.trim() || '')
const rawBalance = computed(() => user.value?.balance)
const rawFrozenBalance = computed(() => user.value?.frozen_balance)
const hasNumericBalance = computed(
  () => typeof rawBalance.value === 'number' && Number.isFinite(rawBalance.value)
)
const frozenBalance = computed(() =>
  typeof rawFrozenBalance.value === 'number' && Number.isFinite(rawFrozenBalance.value)
    ? rawFrozenBalance.value
    : 0
)
const totalBalance = computed(() =>
  hasNumericBalance.value ? Number(rawBalance.value) + frozenBalance.value : null
)

function formatMoneyValue(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return '—'
  return `$${value.toFixed(2)}`
}

const showOnboardingButton = computed(
  () => !authStore.isSimpleMode && user.value?.role === 'admin'
)

const userInitials = computed(() => {
  if (!user.value) return ''
  if (user.value.username) {
    return user.value.username.substring(0, 2).toUpperCase()
  }
  if (user.value.email) {
    return user.value.email.split('@')[0].substring(0, 2).toUpperCase()
  }
  return ''
})

const displayName = computed(() => {
  if (!user.value) return ''
  return user.value.username || user.value.email?.split('@')[0] || ''
})

const routeMetaKeys = computed(() =>
  resolveRouteMetaKeys(route, {
    billingMode: resolveSiteBillingMode(appStore.cachedPublicSettings)
  })
)

const pageDomainLabel = computed(() => {
  const p = route.path || ''
  if (p.startsWith('/admin')) {
    if (
      p.startsWith('/admin/dashboard') ||
      p.startsWith('/admin/ops') ||
      p.startsWith('/admin/channels/monitor') ||
      p.startsWith('/admin/audit-logs')
    ) {
      return isZh.value ? 'ADMIN · 运行与观测' : 'ADMIN · OPS'
    }
    if (
      p.startsWith('/admin/accounts') ||
      p.startsWith('/admin/groups') ||
      p.startsWith('/admin/proxies') ||
      p.startsWith('/admin/proxy-groups') ||
      p.startsWith('/admin/channels')
    ) {
      return isZh.value ? 'ADMIN · 连接与路由' : 'ADMIN · CONNECTIONS'
    }
    if (
      p.startsWith('/admin/orders') ||
      p.startsWith('/admin/subscriptions') ||
      p.startsWith('/admin/redeem') ||
      p.startsWith('/admin/promo-codes') ||
      p.startsWith('/admin/affiliates') ||
      p.startsWith('/admin/usage')
    ) {
      return isZh.value ? 'ADMIN · 结算与账本' : 'ADMIN · SETTLEMENT'
    }
    return isZh.value ? 'ADMIN · 系统与治理' : 'ADMIN · GOVERNANCE'
  }
  return isZh.value ? 'CONSOLE · 开发者台' : 'CONSOLE'
})

const pageTitle = computed(() => {
  if (route.path?.startsWith('/custom/')) {
    const id = route.params?.id as string
    const allMenus = [
      ...(appStore.cachedPublicSettings?.custom_menu_items ?? []),
      ...(adminSettingsStore.customMenuItems ?? [])
    ]
    const item = allMenus.find((m) => m.id === id)
    if (item?.label) return item.label
  }
  const titleKey = routeMetaKeys.value.titleKey
  if (titleKey) {
    const translated = t(titleKey)
    if (translated && translated !== titleKey) return translated
  }
  return (route.meta?.title as string) || siteName.value
})

function closeMenus(): void {
  dropdownOpen.value = false
  appearanceOpen.value = false
}

function toggleMobileSidebar(): void {
  appStore.toggleMobileSidebar?.()
}

function handleOpenCommandPalette(): void {
  appStore.openCommandPalette?.()
  openCommandPalette()
}

function selectTheme(next: ThemeModePreference): void {
  setThemeMode(next)
}

function selectDensity(next: DensityPreference): void {
  setDensityMode(next)
}

async function handleReplayGuide(): Promise<void> {
  closeMenus()
  onboardingStore.replay?.()
}

async function handleLogout(): Promise<void> {
  closeMenus()
  try {
    await authStore.logout?.()
  } finally {
    await router.push('/login')
  }
}

function handleDocumentPointerDown(event: MouseEvent): void {
  const target = event.target as Node | null
  if (!target) return
  if (dropdownOpen.value && dropdownRef.value && !dropdownRef.value.contains(target)) {
    dropdownOpen.value = false
  }
  if (appearanceOpen.value && appearanceRef.value && !appearanceRef.value.contains(target)) {
    appearanceOpen.value = false
  }
}

onMounted(() => {
  document.addEventListener('mousedown', handleDocumentPointerDown)
})

onBeforeUnmount(() => {
  document.removeEventListener('mousedown', handleDocumentPointerDown)
})
</script>

<template>
  <header
    data-testid="console-header"
    class="sticky top-0 z-40 h-14 w-full border-b border-oh-default bg-oh-canvas text-oh-primary"
    @keydown.esc="closeMenus"
  >
    <div class="flex h-full items-center justify-between gap-2 px-2.5 sm:px-4 lg:px-5">
      <!-- Left Rail: Mobile Drawer + Collapse + Brand + ScopeSwitcher + Breadcrumb -->
      <div class="flex min-w-0 flex-1 items-center gap-2 sm:gap-2.5">
        <button
          type="button"
          data-testid="mobile-sidebar-toggle"
          :aria-label="t('common.toggleMenu')"
          :aria-expanded="Boolean(appStore.mobileOpen)"
          aria-controls="sidebar"
          class="oh-focus-ring inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-oh-md border border-oh-default bg-oh-surface text-oh-secondary hover:border-oh-strong hover:text-oh-primary lg:hidden"
          @click="toggleMobileSidebar"
        >
          <svg
            viewBox="0 0 16 16"
            class="h-4 w-4"
            fill="none"
            stroke="currentColor"
            stroke-width="1.6"
            aria-hidden="true"
          >
            <path stroke-linecap="round" d="M2.5 4.25h11M2.5 8h11M2.5 11.75h11" />
          </svg>
        </button>

        <button
          type="button"
          data-testid="desktop-sidebar-toggle"
          :aria-label="appStore.sidebarCollapsed ? t('nav.expand') : t('nav.collapse')"
          :aria-expanded="!appStore.sidebarCollapsed"
          aria-controls="sidebar"
          class="oh-focus-ring hidden h-8 w-8 shrink-0 items-center justify-center rounded-oh-md border border-oh-default bg-oh-surface text-oh-secondary hover:border-oh-strong hover:text-oh-primary lg:inline-flex"
          @click="appStore.toggleSidebar?.()"
        >
          <Icon :name="appStore.sidebarCollapsed ? 'chevronRight' : 'chevronLeft'" size="sm" />
        </button>

        <router-link
          to="/home"
          class="oh-focus-ring inline-flex shrink-0 items-center rounded-oh-sm py-1"
          :title="siteName"
        >
          <BrandMark
            :site-name="siteName"
            :site-logo="siteLogo"
            size="sm"
          />
        </router-link>

        <div v-if="appStore.siteVersion" class="hidden shrink-0 xl:block">
          <VersionBadge :version="appStore.siteVersion" />
        </div>

        <span class="hidden text-oh-muted sm:inline" aria-hidden="true">/</span>

        <ScopeSwitcher v-if="user" />

        <div class="hidden min-w-0 items-center gap-1.5 text-xs md:flex">
          <span class="text-oh-muted" aria-hidden="true">/</span>
          <span class="shrink-0 font-mono text-[10.5px] uppercase tracking-wider text-oh-muted">
            {{ pageDomainLabel }}
          </span>
          <span class="text-oh-muted" aria-hidden="true">·</span>
          <h1 class="truncate font-medium text-oh-primary">{{ pageTitle }}</h1>
        </div>
      </div>

      <!-- Right Controls: ⌘K + Tier Switch + Model Plaza + Docs + Telemetry + Appearance + User -->
      <div class="flex shrink-0 items-center gap-1.5 sm:gap-2">
        <button
          type="button"
          data-testid="console-cmdk-trigger"
          :aria-label="isZh ? '搜索模型和页面 (⌘K)' : 'Search models and routes (⌘K)'"
          class="oh-focus-ring inline-flex h-8 items-center gap-2 rounded-oh-md border border-oh-default bg-oh-surface px-2.5 text-xs text-oh-muted transition-colors hover:border-oh-strong hover:text-oh-primary"
          @click="handleOpenCommandPalette"
        >
          <svg
            viewBox="0 0 16 16"
            class="h-3.5 w-3.5 shrink-0"
            fill="none"
            stroke="currentColor"
            stroke-width="1.6"
            aria-hidden="true"
          >
            <circle cx="7" cy="7" r="4.5" />
            <path stroke-linecap="round" d="m10.5 10.5 3 3" />
          </svg>
          <span class="hidden lg:inline">
            {{ isZh ? '搜索模型 / 页面…' : 'Search…' }}
          </span>
          <kbd
            class="hidden rounded-oh-xs border border-oh-subtle bg-oh-subtle px-1 py-0.5 font-mono text-[10px] text-oh-muted sm:inline"
          >
            ⌘K
          </kbd>
        </button>

        <!-- Admin <-> Developer Console Tier Switcher -->
        <router-link
          v-if="isAdmin"
          data-testid="console-tier-switch"
          :to="isAdminRoute ? '/dashboard' : '/admin/dashboard'"
          :class="[
            'oh-focus-ring hidden h-8 items-center gap-1.5 rounded-oh-md border px-2.5 font-mono text-[11px] font-semibold uppercase tracking-wider transition-colors sm:inline-flex',
            isAdminRoute
              ? 'border-oh-accent bg-oh-selected text-oh-accent-text hover:border-oh-strong'
              : 'border-oh-default bg-oh-surface text-oh-secondary hover:border-oh-strong hover:text-oh-primary'
          ]"
        >
          <span>{{ isAdminRoute ? (isZh ? 'USER · 开发者台' : 'USER CONSOLE') : (isZh ? 'ADMIN · 管理台' : 'ADMIN') }}</span>
        </router-link>

        <!-- Model Plaza Entry (remains visible below small-screen breakpoint per contract) -->
        <router-link
          v-if="showModelPlazaEntry"
          to="/model-plaza"
          :aria-label="t('nav.modelPlaza')"
          :title="t('nav.modelPlaza')"
          class="oh-focus-ring flex h-8 items-center gap-1.5 rounded-oh-md border border-oh-default bg-oh-surface px-2 text-xs font-medium text-oh-secondary transition-colors hover:border-oh-strong hover:text-oh-primary sm:px-2.5"
        >
          <Icon name="grid" size="sm" />
          <span class="hidden xl:inline">{{ t('nav.modelPlaza') }}</span>
        </router-link>

        <a
          v-if="docUrl"
          :href="docUrl"
          target="_blank"
          rel="noopener noreferrer"
          :aria-label="t('nav.docs')"
          :title="t('nav.docs')"
          class="oh-focus-ring hidden h-8 items-center gap-1 rounded-oh-md border border-oh-default bg-oh-surface px-2.5 text-xs text-oh-secondary transition-colors hover:border-oh-strong hover:text-oh-primary md:inline-flex"
        >
          <Icon name="book" size="sm" />
          <span class="hidden xl:inline">{{ t('nav.docs') }}</span>
        </a>

        <SubscriptionProgressMini v-if="user && subscriptionFeatureEnabled && !authStore.isSimpleMode" />

        <router-link
          v-if="user && !authStore.isSimpleMode"
          to="/purchase"
          data-testid="header-balance-pill"
          class="oh-focus-ring hidden h-8 items-center gap-1.5 rounded-oh-md border border-oh-default bg-oh-surface px-2.5 font-mono text-xs tabular-nums text-oh-primary transition-colors hover:border-oh-strong md:inline-flex"
          :title="totalBalance !== null ? `Total ${formatMoneyValue(totalBalance)}` : undefined"
        >
          <span class="text-[10px] uppercase text-oh-muted">{{ isZh ? 'BAL' : 'BAL' }}</span>
          <span class="font-semibold">{{ formatMoneyValue(hasNumericBalance ? Number(rawBalance) : null) }}</span>
        </router-link>

        <AnnouncementBell v-if="user" />

        <LocaleSwitcher v-if="locale" />

        <!-- Appearance Popover (Light / Dark / System + Comfort / Compact) -->
        <div ref="appearanceRef" class="relative">
          <button
            type="button"
            data-testid="appearance-trigger"
            :aria-label="isDark ? t('nav.lightMode') : t('nav.darkMode')"
            :aria-expanded="appearanceOpen"
            class="oh-focus-ring inline-flex h-8 w-8 items-center justify-center rounded-oh-md border border-oh-default bg-oh-surface text-oh-secondary transition-colors hover:border-oh-strong hover:text-oh-primary"
            @click="appearanceOpen = !appearanceOpen; dropdownOpen = false"
          >
            <Icon :name="isDark ? 'sun' : 'moon'" size="sm" />
          </button>

          <div
            v-if="appearanceOpen"
            data-testid="appearance-popover"
            role="dialog"
            :aria-label="isZh ? '外观与密度偏好' : 'Appearance and density preferences'"
            class="absolute right-0 top-full z-50 mt-1.5 w-56 rounded-oh-lg border border-oh-default bg-oh-elevated p-3 text-xs text-oh-primary shadow-oh-md"
          >
            <div class="oh-eyebrow mb-1.5">{{ isZh ? 'THEME MODE · 主题' : 'THEME MODE' }}</div>
            <div class="grid grid-cols-3 gap-1 rounded-oh-md border border-oh-subtle bg-oh-subtle p-0.5">
              <button
                v-for="opt in (['light', 'dark', 'system'] as const)"
                :key="opt"
                type="button"
                :data-testid="`theme-mode-${opt}`"
                :aria-pressed="themeMode === opt"
                :class="[
                  'rounded-oh-sm px-2 py-1 font-mono text-[11px] font-medium uppercase transition-colors',
                  themeMode === opt
                    ? 'bg-oh-surface text-oh-primary shadow-oh-xs'
                    : 'text-oh-muted hover:text-oh-primary'
                ]"
                @click="selectTheme(opt)"
              >
                {{ opt === 'light' ? (isZh ? '浅色' : 'Light') : opt === 'dark' ? (isZh ? '深色' : 'Dark') : (isZh ? '系统' : 'Auto') }}
              </button>
            </div>

            <div class="oh-eyebrow mb-1.5 mt-3">{{ isZh ? 'DENSITY · 信息密度' : 'DENSITY' }}</div>
            <div class="grid grid-cols-2 gap-1 rounded-oh-md border border-oh-subtle bg-oh-subtle p-0.5">
              <button
                v-for="dens in (['comfort', 'compact'] as const)"
                :key="dens"
                type="button"
                :data-testid="`density-mode-${dens}`"
                :aria-pressed="densityMode === dens"
                :class="[
                  'rounded-oh-sm px-2 py-1 font-mono text-[11px] font-medium uppercase transition-colors',
                  densityMode === dens
                    ? 'bg-oh-surface text-oh-primary shadow-oh-xs'
                    : 'text-oh-muted hover:text-oh-primary'
                ]"
                @click="selectDensity(dens)"
              >
                {{ dens === 'comfort' ? (isZh ? '标准' : 'Comfort') : (isZh ? '紧凑' : 'Compact') }}
              </button>
            </div>
          </div>
        </div>

        <!-- User Menu -->
        <div v-if="user" ref="dropdownRef" class="relative">
          <button
            type="button"
            data-testid="user-menu-trigger"
            :aria-expanded="dropdownOpen"
            aria-haspopup="menu"
            class="oh-focus-ring inline-flex h-8 items-center gap-2 rounded-oh-md border border-oh-default bg-oh-surface px-2 text-xs font-medium text-oh-primary transition-colors hover:border-oh-strong"
            @click="dropdownOpen = !dropdownOpen; appearanceOpen = false"
          >
            <img
              v-if="avatarUrl"
              :src="avatarUrl"
              :alt="displayName"
              class="h-5 w-5 rounded-full object-cover"
            />
            <span
              v-else
              class="inline-flex h-5 w-5 items-center justify-center rounded-full bg-oh-accent font-mono text-[10px] font-semibold text-oh-accent-ink"
            >
              {{ userInitials || 'OH' }}
            </span>
            <span class="hidden max-w-[110px] truncate lg:inline">{{ displayName }}</span>
          </button>

          <div
            v-if="dropdownOpen"
            role="menu"
            class="absolute right-0 top-full z-50 mt-1.5 w-60 overflow-hidden rounded-oh-lg border border-oh-default bg-oh-elevated text-xs text-oh-primary shadow-oh-md"
          >
            <div class="border-b border-oh-subtle px-3 py-2.5">
              <p class="truncate font-semibold text-oh-primary">{{ displayName }}</p>
              <p class="truncate text-[11px] text-oh-muted">{{ user.email }}</p>
            </div>

            <div class="p-1">
              <router-link
                to="/profile"
                role="menuitem"
                class="flex items-center gap-2 rounded-oh-sm px-2.5 py-1.5 text-oh-secondary hover:bg-oh-hover hover:text-oh-primary"
                @click="closeMenus"
              >
                <Icon name="user" size="sm" />
                <span>{{ t('nav.profile') }}</span>
              </router-link>

              <router-link
                to="/keys"
                role="menuitem"
                class="flex items-center gap-2 rounded-oh-sm px-2.5 py-1.5 text-oh-secondary hover:bg-oh-hover hover:text-oh-primary"
                @click="closeMenus"
              >
                <Icon name="key" size="sm" />
                <span>{{ t('nav.apiKeys') }}</span>
              </router-link>

              <router-link
                v-if="isAdmin"
                :to="isAdminRoute ? '/dashboard' : '/admin/dashboard'"
                role="menuitem"
                class="flex items-center gap-2 rounded-oh-sm px-2.5 py-1.5 text-oh-secondary hover:bg-oh-hover hover:text-oh-primary"
                @click="closeMenus"
              >
                <Icon name="grid" size="sm" />
                <span>
                  {{ isAdminRoute ? (isZh ? '切换至开发者控制台' : 'Developer Console') : (isZh ? '切换至管理控制台' : 'Admin Console') }}
                </span>
              </router-link>

              <button
                v-if="showOnboardingButton"
                type="button"
                role="menuitem"
                class="flex w-full items-center gap-2 rounded-oh-sm px-2.5 py-1.5 text-left text-oh-secondary hover:bg-oh-hover hover:text-oh-primary"
                @click="handleReplayGuide"
              >
                <Icon name="sparkles" size="sm" />
                <span>{{ t('onboarding.restartTour') }}</span>
              </button>
            </div>

            <div
              v-if="contactInfo"
              class="border-t border-oh-subtle px-3 py-2 text-[11px] text-oh-muted"
            >
              <span class="block truncate">{{ contactInfo }}</span>
            </div>

            <div class="border-t border-oh-subtle p-1">
              <button
                type="button"
                role="menuitem"
                data-testid="logout-button"
                class="flex w-full items-center gap-2 rounded-oh-sm px-2.5 py-1.5 text-left text-[var(--oh-danger-fg)] hover:bg-[var(--oh-danger-bg)]"
                @click="handleLogout"
              >
                <Icon name="login" size="sm" />
                <span>{{ t('nav.logout') }}</span>
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </header>
</template>
