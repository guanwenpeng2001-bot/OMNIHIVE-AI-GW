<script setup lang="ts">
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import * as stores from '@/stores'
import { useThemeDirection } from '@/composables/useThemeDirection'
import { sanitizeUrl } from '@/utils/url'
import BrandMark from './BrandMark.vue'

const LocaleSwitcher = defineAsyncComponent(
  () => import('@/components/common/LocaleSwitcher.vue')
)

interface PublicNavItem {
  key: 'models' | 'quota' | 'pricing' | 'status' | 'docs'
  label: string
  to?: string
  href?: string
  external?: boolean
  active: boolean
}

const MOBILE_NAV_ID = 'public-mobile-nav'
const XL_BREAKPOINT_PX = 1280

const route = useRoute()
const { t, locale } = useI18n()
const appStore = stores.useAppStore()
const authStore = typeof stores.useAuthStore === 'function' ? stores.useAuthStore() : null
const { isDark, toggleDarkMode } = useThemeDirection()

const mobileMenuOpen = ref(false)
const menuButtonRef = ref<HTMLButtonElement | null>(null)
const menuPanelRef = ref<HTMLElement | null>(null)

const isZh = computed(() => String(locale?.value || 'zh').toLowerCase().startsWith('zh'))
const brand = computed(
  () =>
    appStore.brandConfig ?? {
      siteName: appStore.siteName || 'OmniHive',
      siteLogo: appStore.siteLogo || '',
      subtitle: isZh.value ? '统一 AI 推理网关' : 'Unified AI Inference Gateway'
    }
)
const docUrl = computed(() =>
  sanitizeUrl(appStore.cachedPublicSettings?.doc_url || appStore.docUrl || '')
)
const modelPlazaEnabled = computed(
  () => appStore.cachedPublicSettings?.model_plaza_enabled !== false
)
const registrationEnabled = computed(
  () =>
    appStore.cachedPublicSettings?.registration_enabled !== false &&
    !appStore.cachedPublicSettings?.backend_mode_enabled
)
const isAuthenticated = computed(() => Boolean(authStore?.isAuthenticated))
const consolePath = computed(() => (authStore?.isAdmin ? '/admin/dashboard' : '/dashboard'))

const navItems = computed<PublicNavItem[]>(() => {
  const currentPath = route.path || ''
  const items: PublicNavItem[] = []

  if (modelPlazaEnabled.value) {
    items.push({
      key: 'models',
      to: '/model-plaza',
      label: isZh.value ? 'MODELS · 模型' : 'MODELS',
      active: currentPath.startsWith('/model-plaza')
    })
  }

  items.push(
    {
      key: 'quota',
      to: '/key-usage',
      label: isZh.value ? 'QUOTA · 额度查询' : 'QUOTA',
      active: currentPath.startsWith('/key-usage')
    },
    {
      key: 'pricing',
      to: '/pricing',
      label: isZh.value ? 'PRICING · 价格' : 'PRICING',
      active: currentPath.startsWith('/pricing')
    },
    {
      key: 'status',
      to: '/status',
      label: isZh.value ? 'STATUS · 状态' : 'STATUS',
      active: currentPath.startsWith('/status')
    }
  )

  if (docUrl.value) {
    items.push({
      key: 'docs',
      href: docUrl.value,
      external: true,
      label: isZh.value ? 'DOCS · 文档 ↗' : 'DOCS ↗',
      active: false
    })
  } else {
    items.push({
      key: 'docs',
      to: '/help',
      label: isZh.value ? 'DOCS · 文档' : 'DOCS',
      active: currentPath.startsWith('/help') || currentPath.startsWith('/docs')
    })
  }

  return items
})

const menuToggleLabel = computed(() => {
  if (mobileMenuOpen.value) {
    return isZh.value ? '关闭导航菜单' : 'Close navigation menu'
  }
  return isZh.value ? '打开导航菜单' : 'Open navigation menu'
})

function closeMobileMenu(restoreFocus = false): void {
  if (!mobileMenuOpen.value) return
  mobileMenuOpen.value = false
  if (restoreFocus) {
    menuButtonRef.value?.focus()
  }
}

function toggleMobileMenu(): void {
  mobileMenuOpen.value = !mobileMenuOpen.value
}

function handleDocumentClick(event: MouseEvent): void {
  if (!mobileMenuOpen.value) return
  const target = event.target as Node | null
  if (!target) {
    closeMobileMenu()
    return
  }
  if (menuPanelRef.value?.contains(target) || menuButtonRef.value?.contains(target)) {
    return
  }
  closeMobileMenu()
}

function handleDocumentKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape' && mobileMenuOpen.value) {
    event.preventDefault()
    closeMobileMenu(true)
  }
}

function handleViewportResize(): void {
  if (typeof window !== 'undefined' && window.innerWidth >= XL_BREAKPOINT_PX) {
    closeMobileMenu()
  }
}

function openSearch(): void {
  closeMobileMenu()
  appStore.openCommandPalette?.()
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new CustomEvent('omnihive:open-cmdk'))
  }
}

watch(
  () => [route.path, route.fullPath],
  () => {
    closeMobileMenu()
  }
)

onMounted(() => {
  appStore.fetchPublicSettings?.()
  if (typeof document !== 'undefined') {
    document.addEventListener('click', handleDocumentClick)
    document.addEventListener('keydown', handleDocumentKeydown)
  }
  if (typeof window !== 'undefined') {
    window.addEventListener('resize', handleViewportResize)
  }
})

onBeforeUnmount(() => {
  if (typeof document !== 'undefined') {
    document.removeEventListener('click', handleDocumentClick)
    document.removeEventListener('keydown', handleDocumentKeydown)
  }
  if (typeof window !== 'undefined') {
    window.removeEventListener('resize', handleViewportResize)
  }
})
</script>

<template>
  <header
    data-testid="public-header"
    class="sticky top-0 z-40 h-14 w-full border-b border-oh-default bg-oh-canvas text-oh-primary"
  >
    <div
      data-testid="public-header-container"
      class="mx-auto flex h-full w-full max-w-[1560px] items-center justify-between gap-2 px-3.5 sm:gap-3 sm:px-6 lg:px-8 2xl:px-10 3xl:px-12 min-[1560px]:border-x min-[1560px]:border-oh-default"
    >
      <div class="flex min-w-0 items-center gap-3 2xl:gap-5">
        <router-link
          to="/home"
          class="oh-focus-ring inline-flex min-w-0 max-w-[10rem] items-center rounded-oh-sm py-1 sm:max-w-[14rem] xl:max-w-[13rem] 2xl:max-w-[16rem]"
          :aria-label="`${brand.siteName} Home`"
        >
          <BrandMark
            :site-name="brand.siteName"
            :site-logo="brand.siteLogo"
            size="sm"
            tier-label="GATEWAY"
            tier-class="hidden sm:inline-block"
          />
        </router-link>

        <nav
          aria-label="Public navigation"
          data-testid="public-desktop-nav"
          class="hidden shrink-0 items-center gap-0.5 xl:flex"
        >
          <template v-for="item in navItems" :key="item.key">
            <a
              v-if="item.external && item.href"
              :href="item.href"
              target="_blank"
              rel="noopener noreferrer"
              class="oh-focus-ring inline-flex items-center whitespace-nowrap rounded-oh-sm px-2 py-1.5 font-mono text-[11px] font-semibold uppercase tracking-wider text-oh-secondary transition-colors hover:bg-oh-hover hover:text-oh-primary"
            >
              {{ item.label }}
            </a>
            <router-link
              v-else-if="item.to"
              :to="item.to"
              :aria-current="item.active ? 'page' : undefined"
              :class="[
                'oh-focus-ring inline-flex items-center whitespace-nowrap rounded-oh-sm px-2 py-1.5 font-mono text-[11px] font-semibold uppercase tracking-wider transition-colors',
                item.active
                  ? 'bg-oh-selected text-oh-accent-text'
                  : 'text-oh-secondary hover:bg-oh-hover hover:text-oh-primary'
              ]"
            >
              {{ item.label }}
            </router-link>
          </template>
        </nav>
      </div>

      <div class="flex shrink-0 items-center gap-1.5 sm:gap-2">
        <button
          type="button"
          data-testid="public-cmdk-trigger"
          :aria-label="isZh ? '搜索模型与页面 (⌘K)' : 'Search models and routes (⌘K)'"
          class="oh-focus-ring inline-flex h-8 w-8 shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-oh-md border border-oh-default bg-oh-surface px-0 text-xs text-oh-muted transition-colors hover:border-oh-strong hover:text-oh-primary sm:w-auto sm:px-2 md:px-2.5"
          @click="openSearch"
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
          <span class="hidden whitespace-nowrap md:inline xl:hidden">
            {{ isZh ? '搜索模型或命令…' : 'Search…' }}
          </span>
          <span class="hidden whitespace-nowrap xl:inline">
            {{ isZh ? '搜索' : 'Search' }}
          </span>
          <kbd
            class="hidden rounded-oh-xs border border-oh-subtle bg-oh-subtle px-1 py-0.5 font-mono text-[10px] text-oh-muted sm:inline"
          >
            ⌘K
          </kbd>
        </button>

        <div v-if="locale" class="hidden sm:block">
          <LocaleSwitcher />
        </div>

        <button
          type="button"
          data-testid="public-theme-toggle"
          :aria-label="isDark ? t('home.switchToLight') : t('home.switchToDark')"
          :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
          class="oh-focus-ring inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-oh-md border border-oh-default bg-oh-surface text-oh-secondary transition-colors hover:border-oh-strong hover:text-oh-primary"
          @click="toggleDarkMode()"
        >
          <svg
            v-if="isDark"
            viewBox="0 0 20 20"
            class="h-4 w-4"
            fill="none"
            stroke="currentColor"
            stroke-width="1.6"
            aria-hidden="true"
          >
            <circle cx="10" cy="10" r="3.5" />
            <path
              stroke-linecap="round"
              d="M10 2.25v1.5M10 16.25v1.5M4.52 4.52l1.06 1.06M14.42 14.42l1.06 1.06M2.25 10h1.5M16.25 10h1.5M4.52 15.48l1.06-1.06M14.42 5.58l1.06-1.06"
            />
          </svg>
          <svg
            v-else
            viewBox="0 0 20 20"
            class="h-4 w-4"
            fill="none"
            stroke="currentColor"
            stroke-width="1.6"
            aria-hidden="true"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              d="M15.5 11.8A6.25 6.25 0 0 1 8.2 4.5a6.25 6.25 0 1 0 7.3 7.3Z"
            />
          </svg>
        </button>

        <template v-if="isAuthenticated">
          <router-link
            :to="consolePath"
            class="oh-focus-ring inline-flex h-8 shrink-0 items-center whitespace-nowrap rounded-oh-md border border-transparent bg-oh-accent px-2.5 font-mono text-[11px] font-semibold uppercase tracking-wider text-oh-accent-ink transition-colors hover:bg-oh-accent-hover sm:px-3"
          >
            <span class="sm:hidden">{{ isZh ? '控制台 →' : 'CONSOLE →' }}</span>
            <span class="hidden sm:inline">{{ isZh ? 'CONSOLE · 控制台 →' : 'CONSOLE →' }}</span>
          </router-link>
        </template>
        <template v-else>
          <router-link
            v-if="route.path !== '/login'"
            to="/login"
            class="oh-focus-ring inline-flex h-8 shrink-0 items-center whitespace-nowrap rounded-oh-md border border-oh-default bg-oh-surface px-2.5 font-mono text-[11px] font-semibold uppercase tracking-wider text-oh-primary transition-colors hover:bg-oh-hover"
          >
            {{ isZh ? '登录' : 'SIGN IN' }}
          </router-link>
          <router-link
            v-if="registrationEnabled && route.path !== '/register'"
            to="/register"
            class="oh-focus-ring hidden h-8 shrink-0 items-center whitespace-nowrap rounded-oh-md border border-transparent bg-oh-accent px-3 font-mono text-[11px] font-semibold uppercase tracking-wider text-oh-accent-ink transition-colors hover:bg-oh-accent-hover sm:inline-flex"
          >
            {{ isZh ? '注册' : 'GET STARTED' }}
          </router-link>
        </template>

        <button
          ref="menuButtonRef"
          type="button"
          data-testid="public-nav-toggle"
          :aria-label="menuToggleLabel"
          :title="menuToggleLabel"
          :aria-expanded="mobileMenuOpen"
          :aria-controls="MOBILE_NAV_ID"
          :class="[
            'oh-focus-ring inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-oh-md border transition-colors xl:hidden',
            mobileMenuOpen
              ? 'border-oh-accent bg-oh-selected text-oh-accent-text'
              : 'border-oh-default bg-oh-surface text-oh-secondary hover:border-oh-strong hover:text-oh-primary'
          ]"
          @click.stop="toggleMobileMenu"
        >
          <svg
            v-if="mobileMenuOpen"
            viewBox="0 0 16 16"
            class="h-4 w-4"
            fill="none"
            stroke="currentColor"
            stroke-width="1.6"
            aria-hidden="true"
          >
            <path stroke-linecap="round" d="M4 4l8 8M12 4l-8 8" />
          </svg>
          <svg
            v-else
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
      </div>
    </div>

    <div
      v-if="mobileMenuOpen"
      :id="MOBILE_NAV_ID"
      ref="menuPanelRef"
      data-testid="public-mobile-nav"
      class="absolute inset-x-0 top-full z-40 border-b border-oh-default bg-oh-elevated shadow-oh-md xl:hidden"
      @click.stop
    >
      <div class="mx-auto w-full max-w-[1560px] px-3.5 py-3 sm:px-6 lg:px-8">
        <nav
          :aria-label="isZh ? '移动端公共导航' : 'Mobile public navigation'"
          data-testid="public-mobile-nav-list"
          class="grid gap-1 sm:grid-cols-2"
        >
          <template v-for="item in navItems" :key="item.key">
            <a
              v-if="item.external && item.href"
              :href="item.href"
              target="_blank"
              rel="noopener noreferrer"
              class="oh-focus-ring flex min-h-9 items-center justify-between whitespace-nowrap rounded-oh-md border border-transparent px-3 py-2 font-mono text-xs font-semibold uppercase tracking-wider text-oh-secondary transition-colors hover:border-oh-default hover:bg-oh-hover hover:text-oh-primary"
              @click="closeMobileMenu()"
            >
              <span>{{ item.label }}</span>
            </a>
            <router-link
              v-else-if="item.to"
              :to="item.to"
              :aria-current="item.active ? 'page' : undefined"
              :class="[
                'oh-focus-ring flex min-h-9 items-center justify-between whitespace-nowrap rounded-oh-md border px-3 py-2 font-mono text-xs font-semibold uppercase tracking-wider transition-colors',
                item.active
                  ? 'border-oh-accent-border bg-oh-selected text-oh-accent-text'
                  : 'border-transparent text-oh-secondary hover:border-oh-default hover:bg-oh-hover hover:text-oh-primary'
              ]"
              @click="closeMobileMenu()"
            >
              <span>{{ item.label }}</span>
            </router-link>
          </template>
        </nav>

        <div
          v-if="locale || (!isAuthenticated && registrationEnabled && route.path !== '/register')"
          data-testid="public-mobile-utilities"
          class="mt-2.5 flex flex-wrap items-center justify-between gap-2 border-t border-oh-subtle pt-2.5 sm:hidden"
        >
          <LocaleSwitcher v-if="locale" />
          <router-link
            v-if="!isAuthenticated && registrationEnabled && route.path !== '/register'"
            to="/register"
            class="oh-focus-ring ml-auto inline-flex h-8 items-center whitespace-nowrap rounded-oh-md border border-transparent bg-oh-accent px-3 font-mono text-[11px] font-semibold uppercase tracking-wider text-oh-accent-ink transition-colors hover:bg-oh-accent-hover"
            @click="closeMobileMenu()"
          >
            {{ isZh ? '注册' : 'GET STARTED' }}
          </router-link>
        </div>
      </div>
    </div>
  </header>
</template>
