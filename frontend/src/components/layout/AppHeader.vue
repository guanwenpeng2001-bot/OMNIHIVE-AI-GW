<template>
  <header
    class="glass-bar shell-header sticky top-0 z-40 h-14 w-full border-b backdrop-blur-md transition-colors"
    style="border-color: var(--sidebar-border, rgba(229, 231, 235, 0.7)); background: var(--glass, rgba(250, 250, 250, 0.82))"
    @keydown.esc="closeMenus"
  >
    <div class="flex h-14 items-center justify-between gap-2 px-2.5 sm:px-4 md:px-6">
      <!-- Left: Mobile Menu Toggle + Domain Breadcrumb + Page Title & Description -->
      <div class="flex min-w-0 flex-1 items-center gap-2 sm:gap-3">
        <button
          type="button"
          @click="toggleMobileSidebar"
          class="btn-ghost btn-icon rounded-md lg:hidden"
          :aria-label="t('common.toggleMenu')"
          :aria-expanded="appStore.mobileOpen"
          aria-controls="sidebar"
        >
          <span aria-hidden="true">☰</span>
        </button>

        <button
          type="button"
          class="btn-ghost btn-icon hidden shrink-0 rounded-md lg:inline-flex"
          :aria-label="appStore.sidebarCollapsed ? t('nav.expand') : t('nav.collapse')"
          :aria-expanded="!appStore.sidebarCollapsed"
          aria-controls="sidebar"
          @click="appStore.toggleSidebar()"
        >
          <Icon :name="appStore.sidebarCollapsed ? 'chevronRight' : 'chevronLeft'" size="sm" />
        </button>
        <router-link to="/home" class="shell-brand flex shrink-0 items-center gap-2 text-left" title="返回 OmniHive 首页">
          <svg width="32" height="32" viewBox="0 0 32 32" fill="none" aria-hidden="true" class="shrink-0">
            <path d="M16 2 28.1 9v14L16 30 3.9 23V9Z" fill="var(--primary)" />
            <path d="M16 5.6 25 10.8v10.4L16 26.4 7 21.2V10.8Z" stroke="var(--brand, #f59e0b)" stroke-width="1.6" />
            <path d="M16 10.2 21 13.1v5.8L16 21.8l-5-2.9v-5.8Z" fill="var(--brand, #f59e0b)" />
            <circle cx="16" cy="16" r="2.1" fill="var(--primary)" />
          </svg>
          <div class="shell-brand-copy whitespace-nowrap leading-tight">
            <div class="flex items-center gap-1.5 text-sm font-bold tracking-tight">
              OmniHive <span class="hive-badge rounded-full px-1.5 py-0.5 font-mono text-[10px]">HIVE</span>
            </div>
            <div class="text-[10px] opacity-60">智能算力中转蜂巢</div>
          </div>
        </router-link>
        <div class="shell-version hidden shrink-0 2xl:block">
          <VersionBadge :version="appStore.siteVersion" />
        </div>
        <div class="shell-breadcrumb hidden min-w-0 items-center gap-2 whitespace-nowrap text-xs sm:flex" :title="pageDescription">
          <span class="mr-1 opacity-30" aria-hidden="true">|</span>
          <span class="shrink-0 opacity-60">· {{ pageDomainLabel }} /</span>
          <h1 class="truncate font-medium">{{ pageTitle }}</h1>
        </div>

        <!-- OpenRouter-style Global Command Palette Trigger -->
        <button
          type="button"
          @click="handleOpenCommandPalette"
          class="shell-search hidden md:flex items-center gap-1.5 ml-1 rounded-lg border px-2 py-1.5 text-xs transition-all hover:border-[var(--brand)] cursor-pointer shrink-0 whitespace-nowrap"
          style="
            border-color: var(--border, #e5e7eb);
            background: var(--surface-2, #f4f4f5);
            color: var(--muted-foreground, #6f6f78);
          "
          aria-label="搜索模型和页面"
        >
          <svg
            width="13"
            height="13"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            aria-hidden="true"
          >
            <circle cx="11" cy="11" r="8" />
            <path d="m21 21-4.3-4.3" />
          </svg>
          <span class="shell-search-label text-left">搜索模型 / 页面…</span>
          <span class="kbd">⌘K</span>
        </button>
      </div>

      <!-- Right: Announcements + Docs + Model Plaza + Language + Subscriptions + Balance + Theme/Dense/Dark + User Dropdown -->
      <div class="flex shrink-0 items-center gap-1 sm:gap-2 whitespace-nowrap">
        <!-- Announcement Bell -->
        <div v-if="user" class="shrink-0">
          <AnnouncementBell />
        </div>

        <!-- Docs Link -->
        <a
          v-if="docUrl"
          :href="docUrl"
          target="_blank"
          rel="noopener noreferrer"
          class="hidden shrink-0 whitespace-nowrap items-center gap-1.5 rounded-lg px-2 py-1.5 text-xs font-medium transition-colors hover:bg-[var(--secondary)] sm:flex"
          style="color: var(--foreground)"
        >
          <Icon name="book" size="sm" />
          <span class="hidden lg:inline whitespace-nowrap">{{ t('nav.docs') }}</span>
        </a>

        <!-- Model Plaza Entry (icon only below sm) -->
        <router-link
          v-if="user && modelPlazaEnabled"
          :to="{ path: '/model-plaza', query: { embedded: '1' } }"
          :title="t('nav.modelPlaza')"
          :aria-label="t('nav.modelPlaza')"
          class="flex shrink-0 whitespace-nowrap items-center gap-1.5 rounded-lg px-2 py-1.5 text-xs font-medium transition-colors hover:bg-[var(--secondary)]"
          style="color: var(--foreground)"
        >
          <Icon name="grid" size="sm" />
          <span class="hidden lg:inline whitespace-nowrap">{{ t('nav.modelPlaza') }}</span>
        </router-link>

        <!-- Language Switcher -->
        <div class="shrink-0">
          <LocaleSwitcher />
        </div>

        <!-- Subscription Progress (for users with active subscriptions; not mounted at all when the feature is off) -->
        <div
          v-if="user && !isAdminRoute && subscriptionFeatureEnabled"
          class="shell-subscription hidden 2xl:block shrink-0 whitespace-nowrap"
        >
          <SubscriptionProgressMini />
        </div>

        <!-- Balance Display -->
        <div
          v-if="user"
          class="shell-balance group relative hidden shrink-0 whitespace-nowrap items-center gap-1.5 rounded-full border px-2.5 py-1 transition-colors sm:flex"
          style="
            border-color: color-mix(in srgb, var(--brand, #f59e0b) 35%, var(--border, #e5e7eb));
            background: color-mix(in srgb, var(--brand, #f59e0b) 10%, var(--card, #ffffff));
          "
        >
          <svg
            class="h-3.5 w-3.5 shrink-0"
            style="color: var(--brand-text, #92610a)"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            stroke-width="1.5"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              d="M2.25 18.75a60.07 60.07 0 0115.797 2.101c.727.198 1.453-.342 1.453-1.096V18.75M3.75 4.5v.75A.75.75 0 013 6h-.75m0 0v-.375c0-.621.504-1.125 1.125-1.125H20.25M2.25 6v9m18-10.5v.75c0 .414.336.75.75.75h.75m-1.5-1.5h.375c.621 0 1.125.504 1.125 1.125v9.75c0 .621-.504 1.125-1.125 1.125h-.375m1.5-1.5H21a.75.75 0 00-.75.75v.75m0 0H3.75m0 0h-.375a1.125 1.125 0 01-1.125-1.125V15m1.5 1.5v-.75A.75.75 0 003 15h-.75M15 10.5a3 3 0 11-6 0 3 3 0 016 0zm3 0h.008v.008H18V10.5zm-12 0h.008v.008H6V10.5z"
            />
          </svg>
          <span class="hidden text-[10px] opacity-65 2xl:inline">蜜金余额</span>
          <span
            class="font-mono text-xs font-semibold tabular-nums"
            style="color: var(--brand-text, #92610a)"
          >
            {{ formatHeaderMoney(availableBalance) }}
          </span>
          <span
            v-if="frozenBalance > 0"
            class="rounded-full bg-amber-100 px-1.5 py-0.5 text-[11px] font-medium text-amber-700 dark:bg-amber-900/40 dark:text-amber-200"
          >
            {{ balanceFrozenLabel }}
          </span>
          <div
            class="pointer-events-none absolute right-0 top-full z-50 mt-2 hidden w-56 rounded-xl border p-3 text-xs shadow-lg group-hover:block"
            style="border-color: var(--border); background: var(--popover); color: var(--popover-foreground)"
          >
            <div class="flex items-center justify-between">
              <span class="opacity-65">{{ balanceAvailableText }}</span>
              <span class="font-mono font-medium tabular-nums">{{ formatHeaderMoney(availableBalance) }}</span>
            </div>
            <div class="mt-2 flex items-center justify-between">
              <span class="opacity-65">{{ balanceFrozenText }}</span>
              <span class="font-mono font-medium tabular-nums" style="color: var(--warn-text)">{{ formatHeaderMoney(frozenBalance) }}</span>
            </div>
            <div class="mt-2 border-t pt-2" style="border-color: var(--border)">
              <div class="flex items-center justify-between">
                <span class="opacity-65">{{ balanceTotalText }}</span>
                <span class="font-mono font-semibold tabular-nums">{{ formatHeaderMoney(totalBalance) }}</span>
              </div>
            </div>
          </div>
        </div>

        <!-- Theme Direction Picker (5 Prototype Directions) -->
        <div class="relative hidden shrink-0 whitespace-nowrap sm:block" ref="themeMenuRef">
          <button
            type="button"
            @click="toggleThemeMenu"
            class="inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium transition-colors hover:border-[var(--brand)]"
            style="
              border-color: var(--border, #e5e7eb);
              background: var(--card, #ffffff);
              color: var(--foreground, #131316);
            "
            :aria-expanded="themeMenuOpen ? 'true' : 'false'"
            aria-label="切换设计方向"
          >
            <span
              class="h-2.5 w-2.5 rounded-full shrink-0"
              :style="{ background: currentThemeMeta.c2 }"
            ></span>
            <span class="max-w-[92px] truncate">{{ currentThemeMeta.shortName }}</span>
            <span class="text-[10px] opacity-60">▾</span>
          </button>

          <transition name="dropdown">
            <div
              v-if="themeMenuOpen"
              role="group"
              aria-label="设计方向"
              class="absolute right-0 z-50 mt-2 w-60 rounded-xl border p-1.5 shadow-lg"
              style="
                border-color: var(--border, #e5e7eb);
                background: var(--popover, #ffffff);
                color: var(--popover-foreground, #131316);
              "
            >
              <div class="px-2.5 py-1 text-[11px] font-semibold uppercase tracking-wider opacity-50">
                设计方向 · 5 套主题
              </div>
              <button
                v-for="item in themes"
                :key="item.key"
                type="button"
                @click="selectThemeDirection(item.key)"
                class="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-left text-xs transition-colors hover:bg-[var(--secondary)]"
                :style="
                  currentTheme === item.key
                    ? 'background: color-mix(in srgb, var(--brand, #f59e0b) 13%, transparent); font-weight: 600;'
                    : ''
                "
              >
                <span
                  class="h-3.5 w-3.5 rounded-full shrink-0 border"
                  :style="{ background: item.c2, borderColor: 'var(--border)' }"
                ></span>
                <div class="min-w-0 flex-1">
                  <div class="flex items-center justify-between gap-1">
                    <span class="truncate">{{ item.shortName }}</span>
                    <span
                      v-if="currentTheme === item.key"
                      class="text-[11px] font-mono"
                      style="color: var(--brand-text, #92610a)"
                    >✓</span>
                  </div>
                  <div class="truncate text-[11px] opacity-60">{{ item.desc }}</div>
                </div>
              </button>
            </div>
          </transition>
        </div>

        <!-- Density Toggle Button -->
        <button
          type="button"
          @click="toggleDenseMode"
          class="shell-density btn-ghost btn-icon hidden 2xl:inline-flex rounded-md text-xs"
          :style="isDense ? 'color: var(--brand-text, #f59e0b); background: color-mix(in srgb, var(--brand, #f59e0b) 12%, transparent);' : ''"
          :title="isDense ? '当前：紧凑高密度（点击切回舒适密度）' : '切换高密度工作台模式'"
          aria-label="切换信息密度"
        >
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" aria-hidden="true">
            <path d="M3 6h18M3 10h18M3 14h18M3 18h18" />
          </svg>
        </button>

        <!-- Dark / Light Mode Toggle Button -->
        <button
          type="button"
          @click="toggleDarkMode"
          class="btn-ghost btn-icon inline-flex shrink-0 rounded-md text-xs"
          :title="isDark ? '切换浅色模式' : '切换深色模式'"
          aria-label="切换深浅色"
        >
          <svg v-if="isDark" width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" aria-hidden="true">
            <circle cx="12" cy="12" r="4" />
            <path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41" />
          </svg>
          <svg v-else width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" aria-hidden="true">
            <path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z" />
          </svg>
        </button>

        <!-- User Dropdown -->
        <div v-if="user" class="relative shrink-0 whitespace-nowrap" ref="dropdownRef">
          <button
            @click="toggleDropdown"
            class="flex items-center gap-2 rounded-xl p-1.5 transition-colors hover:bg-[var(--secondary)]"
            :aria-label="t('common.userMenu')"
            :aria-expanded="dropdownOpen"
          >
            <div
              class="flex h-8 w-8 items-center justify-center overflow-hidden rounded-xl text-xs font-semibold text-white shadow-sm"
              style="background: linear-gradient(135deg, var(--brand, #f59e0b), #ea580c)"
            >
              <img
                v-if="avatarUrl"
                :src="avatarUrl"
                :alt="displayName"
                class="h-full w-full object-cover"
              >
              <span v-else>{{ userInitials }}</span>
            </div>
            <div class="shell-user-copy hidden max-w-[110px] truncate text-left 2xl:block">
              <div class="truncate text-xs font-medium" style="color: var(--foreground)">
                {{ displayName }}
              </div>
              <div class="max-w-[110px] truncate text-[11px] opacity-60">
                {{ user.email }}
              </div>
            </div>
            <Icon name="chevronDown" size="sm" class="hidden opacity-50 2xl:block" />
          </button>

          <!-- Dropdown Menu -->
          <transition name="dropdown">
            <div v-if="dropdownOpen" class="dropdown right-0 mt-2 w-56">
              <!-- User Info -->
              <div class="border-b px-4 py-3" style="border-color: var(--border)">
                <div class="text-sm font-medium" style="color: var(--foreground)">
                  {{ displayName }}
                </div>
                <div class="truncate text-xs opacity-65" :title="user.email">{{ user.email }}</div>
              </div>

              <!-- Balance (mobile only) -->
              <div class="border-b px-4 py-2 sm:hidden" style="border-color: var(--border)">
                <div class="text-xs opacity-65">
                  {{ t('common.balance') }}
                </div>
                <div class="font-mono text-sm font-semibold tabular-nums" style="color: var(--brand-text)">
                  {{ formatHeaderMoney(availableBalance) }}
                </div>
                <div v-if="frozenBalance > 0" class="mt-1 text-xs" style="color: var(--warn-text)">
                  {{ balanceFrozenText }} {{ formatHeaderMoney(frozenBalance) }}
                </div>
              </div>

              <div class="py-1">
                <router-link to="/profile" @click="closeDropdown" class="dropdown-item">
                  <Icon name="user" size="sm" />
                  {{ t('nav.profile') }}
                </router-link>

                <router-link to="/keys" @click="closeDropdown" class="dropdown-item">
                  <Icon name="key" size="sm" />
                  {{ t('nav.apiKeys') }}
                </router-link>

                <router-link to="/workspaces" @click="closeDropdown" class="dropdown-item">
                  <Icon name="grid" size="sm" />
                  {{ t('workspaces.title') === 'workspaces.title' ? '工作空间' : t('workspaces.title') }}
                </router-link>
              </div>

              <!-- Contact Support (only show if configured) -->
              <div
                v-if="contactInfo"
                class="border-t px-4 py-2.5"
                style="border-color: var(--border)"
              >
                <div class="flex items-center gap-2 text-xs opacity-70">
                  <svg
                    class="h-3.5 w-3.5 flex-shrink-0"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    stroke-width="1.5"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      d="M20.25 8.511c.884.284 1.5 1.128 1.5 2.097v4.286c0 1.136-.847 2.1-1.98 2.193-.34.027-.68.052-1.02.072v3.091l-3-3c-1.354 0-2.694-.055-4.02-.163a2.115 2.115 0 01-.825-.242m9.345-8.334a2.126 2.126 0 00-.476-.095 48.64 48.64 0 00-8.048 0c-1.131.094-1.976 1.057-1.976 2.192v4.286c0 .837.46 1.58 1.155 1.951m9.345-8.334V6.637c0-1.621-1.152-3.026-2.76-3.235A48.455 48.455 0 0011.25 3c-2.115 0-4.198.137-6.24.402-1.608.209-2.76 1.614-2.76 3.235v6.226c0 1.621 1.152 3.026 2.76 3.235.577.075 1.157.14 1.74.194V21l4.155-4.155"
                    />
                  </svg>
                  <span>{{ t('common.contactSupport') }}:</span>
                  <span class="font-medium" style="color: var(--foreground)">{{
                    contactInfo
                  }}</span>
                </div>
              </div>

              <div v-if="showOnboardingButton" class="border-t py-1" style="border-color: var(--border)">
                <button @click="handleReplayGuide" class="dropdown-item w-full">
                  <svg class="h-4 w-4" fill="currentColor" viewBox="0 0 24 24">
                    <path
                      d="M12 2a10 10 0 100 20 10 10 0 000-20zm0 14a1 1 0 110 2 1 1 0 010-2zm1.07-7.75c0-.6-.49-1.25-1.32-1.25-.7 0-1.22.4-1.43 1.02a1 1 0 11-1.9-.62A3.41 3.41 0 0111.8 5c2.02 0 3.25 1.4 3.25 2.9 0 2-1.83 2.55-2.43 3.12-.43.4-.47.75-.47 1.23a1 1 0 01-2 0c0-1 .16-1.82 1.1-2.7.69-.64 1.82-1.05 1.82-2.06z"
                    />
                  </svg>
                  {{ $t('onboarding.restartTour') }}
                </button>
              </div>

              <div class="border-t py-1" style="border-color: var(--border)">
                <button
                  @click="handleLogout"
                  class="dropdown-item w-full"
                  style="color: var(--bad-text)"
                >
                  <svg
                    class="h-4 w-4"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    stroke-width="1.5"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      d="M15.75 9V5.25A2.25 2.25 0 0013.5 3h-6a2.25 2.25 0 00-2.25 2.25v13.5A2.25 2.25 0 007.5 21h6a2.25 2.25 0 002.25-2.25V15M12 9l-3 3m0 0l3 3m-3-3h12.75"
                    />
                  </svg>
                  {{ t('nav.logout') }}
                </button>
              </div>
            </div>
          </transition>
        </div>
      </div>
    </div>
  </header>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore, useAuthStore, useOnboardingStore } from '@/stores'
import { useAdminSettingsStore } from '@/stores/adminSettings'
import { useThemeDirection, type ThemeDirectionKey } from '@/composables/useThemeDirection'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import VersionBadge from '@/components/common/VersionBadge.vue'
import SubscriptionProgressMini from '@/components/common/SubscriptionProgressMini.vue'
import AnnouncementBell from '@/components/common/AnnouncementBell.vue'
import Icon from '@/components/icons/Icon.vue'
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
  themes,
  currentTheme,
  currentThemeMeta,
  isDark,
  isDense,
  setThemeDirection,
  toggleDarkMode,
  toggleDenseMode,
  openCommandPalette
} = useThemeDirection()

const user = computed(() => authStore.user)
const isAdminRoute = computed(() => Boolean(route.path?.startsWith('/admin')))
const dropdownOpen = ref(false)
const dropdownRef = ref<HTMLElement | null>(null)
const themeMenuOpen = ref(false)
const themeMenuRef = ref<HTMLElement | null>(null)
const contactInfo = computed(() => appStore.contactInfo)
const docUrl = computed(() => sanitizeUrl(appStore.docUrl))
const modelPlazaEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.modelPlaza))
const avatarUrl = computed(() => user.value?.avatar_url?.trim() || '')
const availableBalance = computed(() => Number(user.value?.balance || 0))
const frozenBalance = computed(() => Number(user.value?.frozen_balance || 0))
const totalBalance = computed(() => availableBalance.value + frozenBalance.value)
const balanceAvailableText = computed(() => t('common.availableBalance') === 'common.availableBalance' ? '可用余额' : t('common.availableBalance'))
const balanceFrozenText = computed(() => t('common.frozenBalance') === 'common.frozenBalance' ? '冻结金额' : t('common.frozenBalance'))
const balanceTotalText = computed(() => t('common.totalBalance') === 'common.totalBalance' ? '总余额' : t('common.totalBalance'))
const balanceFrozenLabel = computed(() => `${balanceFrozenText.value} ${formatHeaderMoney(frozenBalance.value)}`)

// 只在标准模式的管理员下显示新手引导按钮
const showOnboardingButton = computed(() => {
  return !authStore.isSimpleMode && user.value?.role === 'admin'
})

const userInitials = computed(() => {
  if (!user.value) return ''
  // Prefer username, fallback to email
  if (user.value.username) {
    return user.value.username.substring(0, 2).toUpperCase()
  }
  if (user.value.email) {
    // Get the part before @ and take first 2 chars
    const localPart = user.value.email.split('@')[0]
    return localPart.substring(0, 2).toUpperCase()
  }
  return ''
})

const displayName = computed(() => {
  if (!user.value) return ''
  return user.value.username || user.value.email?.split('@')[0] || ''
})

// 订阅功能关闭时不挂载顶栏订阅徽章（组件 onMounted 会拉取订阅接口）。
const subscriptionFeatureEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.subscription))

// /purchase 的标题/描述随站点计费模式切换，与 document.title 共用同一解析。
const routeMetaKeys = computed(() => resolveRouteMetaKeys(route, {
  billingMode: resolveSiteBillingMode(appStore.cachedPublicSettings),
}))

const pageDomainLabel = computed(() => {
  const p = route.path || ''
  if (p.startsWith('/admin')) {
    if (
      p.startsWith('/admin/dashboard') ||
      p.startsWith('/admin/ops') ||
      p.startsWith('/admin/channels/monitor') ||
      p.startsWith('/admin/audit-logs') ||
      p.startsWith('/admin/usage')
    ) {
      return '运行'
    }
    if (
      p.startsWith('/admin/channels') ||
      p.startsWith('/admin/accounts') ||
      p.startsWith('/admin/groups') ||
      p.startsWith('/admin/proxies') ||
      p.startsWith('/admin/proxy-groups') ||
      p.startsWith('/admin/ip-management') ||
      p.startsWith('/admin/plugins')
    ) {
      return '渠道与账户'
    }
    if (
      p.startsWith('/admin/orders') ||
      p.startsWith('/admin/subscriptions') ||
      p.startsWith('/admin/redeem') ||
      p.startsWith('/admin/promo-codes') ||
      p.startsWith('/admin/affiliates')
    ) {
      return '结算'
    }
    if (
      p.startsWith('/admin/users') ||
      p.startsWith('/admin/announcements') ||
      p.startsWith('/admin/risk-control') ||
      p.startsWith('/admin/prompt-audit') ||
      p.startsWith('/admin/security-audit')
    ) {
      return '用户'
    }
    if (p.startsWith('/admin/settings') || p.startsWith('/admin/backup')) {
      return '系统'
    }
    return '运行'
  }
  if (p.startsWith('/dashboard') || p.startsWith('/workspaces') || p.startsWith('/help')) return '概览'
  if (p.startsWith('/keys') || p.startsWith('/usage')) return '密钥与用量'
  if (
    p.startsWith('/batch-image') ||
    p.startsWith('/available-channels') ||
    p.startsWith('/monitor') ||
    p.startsWith('/custom') ||
    p.startsWith('/model-plaza')
  ) {
    return '资源'
  }
  if (
    p.startsWith('/subscriptions') ||
    p.startsWith('/purchase') ||
    p.startsWith('/orders') ||
    p.startsWith('/redeem') ||
    p.startsWith('/affiliate') ||
    p.startsWith('/settings/mcp') ||
    p.startsWith('/profile') ||
    p.startsWith('/qr-pay') ||
    p.startsWith('/stripe-pay') ||
    p.startsWith('/airwallex-pay') ||
    p.startsWith('/result')
  ) {
    return '账户'
  }
  return '概览'
})

const pageTitle = computed(() => {
  if (locale?.value?.startsWith('zh')) {
    if (route.path === '/admin/dashboard') return '总览仪表盘'
    if (route.path === '/dashboard') return '控制台首页'
  }
  // For custom pages, use the menu item's label instead of generic "自定义页面"
  if (route.name === 'CustomPage') {
    const id = route.params.id as string
    const publicItems = appStore.cachedPublicSettings?.custom_menu_items ?? []
    const menuItem = publicItems.find((item) => item.id === id)
      ?? (authStore.isAdmin ? adminSettingsStore.customMenuItems.find((item) => item.id === id) : undefined)
    if (menuItem?.label) return menuItem.label
  }
  const titleKey = routeMetaKeys.value.titleKey
  if (titleKey) {
    return t(titleKey)
  }
  return (route.meta.title as string) || ''
})

const pageDescription = computed(() => {
  const descKey = routeMetaKeys.value.descriptionKey
  if (descKey) {
    return t(descKey)
  }
  return (route.meta.description as string) || ''
})

function handleOpenCommandPalette() {
  openCommandPalette()
}

function toggleThemeMenu() {
  themeMenuOpen.value = !themeMenuOpen.value
}

function selectThemeDirection(key: ThemeDirectionKey) {
  setThemeDirection(key)
  themeMenuOpen.value = false
}

function toggleMobileSidebar() {
  appStore.toggleMobileSidebar()
}

function toggleDropdown() {
  dropdownOpen.value = !dropdownOpen.value
}

function closeDropdown() {
  dropdownOpen.value = false
}

function closeMenus() {
  closeDropdown()
  themeMenuOpen.value = false
}

async function handleLogout() {
  closeDropdown()
  try {
    await authStore.logout()
  } catch (error) {
    // Ignore logout errors - still redirect to login
    console.error('Logout error:', error)
  }
  await router.push('/login')
}

function handleReplayGuide() {
  closeDropdown()
  onboardingStore.replay()
}

function formatHeaderMoney(value: number) {
  if (!Number.isFinite(value)) return '$0.00'
  return `$${value.toFixed(2)}`
}

function handleClickOutside(event: MouseEvent) {
  if (dropdownRef.value && !dropdownRef.value.contains(event.target as Node)) {
    closeDropdown()
  }
  if (themeMenuRef.value && !themeMenuRef.value.contains(event.target as Node)) {
    themeMenuOpen.value = false
  }
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', handleClickOutside)
})
</script>

<style scoped>
.hive-badge {
  background: color-mix(in srgb, var(--brand, #f59e0b) 16%, transparent);
  color: var(--brand-text, #92610a);
}

.shell-header :deep(button),
.shell-header :deep(a) {
  flex-shrink: 0;
  white-space: nowrap;
}

.shell-breadcrumb {
  flex: 0 1 auto;
  max-width: 320px;
}

@media (max-width: 1799px) {
  .shell-version,
  .shell-subscription,
  .shell-user-copy,
  .shell-density {
    display: none;
  }
}

@media (max-width: 1279px) {
  .shell-breadcrumb {
    display: none;
  }
}

@media (max-width: 1439px) {
  .shell-search-label {
    display: none;
  }
}

@media (max-width: 1023px) {
  .shell-brand-copy {
    display: none;
  }
}

@media (max-width: 379px) {
  .shell-header > div,
  .shell-header > div > div {
    gap: 0.125rem;
  }
}

.dropdown-enter-active,
.dropdown-leave-active {
  transition: all 0.2s ease;
}

.dropdown-enter-from,
.dropdown-leave-to {
  opacity: 0;
  transform: scale(0.95) translateY(-4px);
}
</style>
