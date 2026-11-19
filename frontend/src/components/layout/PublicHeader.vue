<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import * as stores from '@/stores'
import { useThemeDirection } from '@/composables/useThemeDirection'
import { sanitizeUrl } from '@/utils/url'
import BrandMark from './BrandMark.vue'

const LocaleSwitcher = defineAsyncComponent(
  () => import('@/components/common/LocaleSwitcher.vue')
)

const route = useRoute()
const { t, locale } = useI18n()
const appStore = stores.useAppStore()
const authStore = typeof stores.useAuthStore === 'function' ? stores.useAuthStore() : null
const { isDark, toggleDarkMode } = useThemeDirection()

const isZh = computed(() => String(locale?.value || 'zh').toLowerCase().startsWith('zh'))
const brand = computed(
  () =>
    appStore.brandConfig ?? {
      siteName: appStore.siteName || 'OmniHive',
      siteLogo: appStore.siteLogo || '',
      subtitle: isZh.value ? '统一 AI 推理网关' : 'Unified AI Inference Gateway'
    }
)
const docUrl = computed(() => sanitizeUrl(appStore.docUrl || ''))
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

function openSearch(): void {
  appStore.openCommandPalette?.()
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new CustomEvent('omnihive:open-cmdk'))
  }
}

onMounted(() => {
  appStore.fetchPublicSettings?.()
})
</script>

<template>
  <header
    data-testid="public-header"
    class="sticky top-0 z-40 h-14 w-full border-b border-oh-default bg-oh-canvas text-oh-primary"
  >
    <div class="mx-auto flex h-full max-w-[1200px] items-center justify-between gap-3 px-4 sm:px-6 xl:border-x xl:border-oh-default">
      <div class="flex min-w-0 items-center gap-5">
        <router-link
          to="/home"
          class="oh-focus-ring inline-flex shrink-0 items-center rounded-oh-sm py-1"
          :aria-label="`${brand.siteName} Home`"
        >
          <BrandMark
            :site-name="brand.siteName"
            :site-logo="brand.siteLogo"
            size="sm"
            tier-label="GATEWAY"
          />
        </router-link>

        <nav
          aria-label="Public navigation"
          class="hidden items-center gap-1 md:flex"
        >
          <router-link
            v-if="modelPlazaEnabled"
            to="/model-plaza"
            :class="[
              'oh-focus-ring whitespace-nowrap rounded-oh-sm px-2.5 py-1.5 font-mono text-[11px] font-semibold uppercase tracking-wider transition-colors',
              route.path.startsWith('/model-plaza')
                ? 'bg-oh-selected text-oh-accent-text'
                : 'text-oh-secondary hover:bg-oh-hover hover:text-oh-primary'
            ]"
          >
            {{ isZh ? 'MODELS · 模型' : 'MODELS' }}
          </router-link>

          <router-link
            to="/key-usage"
            class="hidden xl:inline-flex"
            :class="[
              'oh-focus-ring whitespace-nowrap rounded-oh-sm px-2.5 py-1.5 font-mono text-[11px] font-semibold uppercase tracking-wider transition-colors',
              route.path.startsWith('/key-usage')
                ? 'bg-oh-selected text-oh-accent-text'
                : 'text-oh-secondary hover:bg-oh-hover hover:text-oh-primary'
            ]"
          >
            {{ isZh ? 'QUOTA · 额度查询' : 'QUOTA' }}
          </router-link>

          <router-link
            to="/pricing"
            :class="[
              'oh-focus-ring whitespace-nowrap rounded-oh-sm px-2.5 py-1.5 font-mono text-[11px] font-semibold uppercase tracking-wider transition-colors',
              route.path.startsWith('/pricing')
                ? 'bg-oh-selected text-oh-accent-text'
                : 'text-oh-secondary hover:bg-oh-hover hover:text-oh-primary'
            ]"
          >
            {{ isZh ? 'PRICING · 价格' : 'PRICING' }}
          </router-link>

          <router-link
            to="/status"
            :class="[
              'oh-focus-ring whitespace-nowrap rounded-oh-sm px-2.5 py-1.5 font-mono text-[11px] font-semibold uppercase tracking-wider transition-colors',
              route.path.startsWith('/status')
                ? 'bg-oh-selected text-oh-accent-text'
                : 'text-oh-secondary hover:bg-oh-hover hover:text-oh-primary'
            ]"
          >
            {{ isZh ? 'STATUS · 状态' : 'STATUS' }}
          </router-link>

          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="oh-focus-ring rounded-oh-sm px-2.5 py-1.5 font-mono text-[11px] font-semibold uppercase tracking-wider text-oh-secondary transition-colors hover:bg-oh-hover hover:text-oh-primary"
          >
            {{ isZh ? 'DOCS · 文档 ↗' : 'DOCS ↗' }}
          </a>
          <router-link
            v-else
            to="/help"
            :class="[
              'oh-focus-ring whitespace-nowrap rounded-oh-sm px-2.5 py-1.5 font-mono text-[11px] font-semibold uppercase tracking-wider transition-colors',
              route.path.startsWith('/help') || route.path.startsWith('/docs')
                ? 'bg-oh-selected text-oh-accent-text'
                : 'text-oh-secondary hover:bg-oh-hover hover:text-oh-primary'
            ]"
          >
            {{ isZh ? 'DOCS · 文档' : 'DOCS' }}
          </router-link>
        </nav>
      </div>

      <div class="flex shrink-0 items-center gap-2">
        <button
          type="button"
          data-testid="public-cmdk-trigger"
          :aria-label="isZh ? '搜索模型与页面 (⌘K)' : 'Search models and routes (⌘K)'"
          class="oh-focus-ring inline-flex h-8 items-center gap-2 rounded-oh-md border border-oh-default bg-oh-surface px-2.5 text-xs text-oh-muted transition-colors hover:border-oh-strong hover:text-oh-primary"
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
          <span class="hidden sm:inline">
            {{ isZh ? '搜索模型或命令…' : 'Search…' }}
          </span>
          <kbd
            class="hidden rounded-oh-xs border border-oh-subtle bg-oh-subtle px-1 py-0.5 font-mono text-[10px] text-oh-muted sm:inline"
          >
            ⌘K
          </kbd>
        </button>

        <LocaleSwitcher v-if="locale" />

        <button
          type="button"
          data-testid="public-theme-toggle"
          :aria-label="isDark ? t('home.switchToLight') : t('home.switchToDark')"
          :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
          class="oh-focus-ring inline-flex h-8 w-8 items-center justify-center rounded-oh-md border border-oh-default bg-oh-surface text-oh-secondary transition-colors hover:border-oh-strong hover:text-oh-primary"
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
            class="oh-focus-ring inline-flex h-8 items-center rounded-oh-md border border-transparent bg-oh-accent px-3 font-mono text-[11px] font-semibold uppercase tracking-wider text-oh-accent-ink transition-colors hover:bg-oh-accent-hover"
          >
            {{ isZh ? 'CONSOLE · 控制台 →' : 'CONSOLE →' }}
          </router-link>
        </template>
        <template v-else>
          <router-link
            v-if="route.path !== '/login'"
            to="/login"
            class="oh-focus-ring inline-flex h-8 items-center rounded-oh-md border border-oh-default bg-oh-surface px-2.5 font-mono text-[11px] font-semibold uppercase tracking-wider text-oh-primary transition-colors hover:bg-oh-hover"
          >
            {{ isZh ? '登录' : 'SIGN IN' }}
          </router-link>
          <router-link
            v-if="registrationEnabled && route.path !== '/register'"
            to="/register"
            class="oh-focus-ring hidden h-8 items-center rounded-oh-md border border-transparent bg-oh-accent px-3 font-mono text-[11px] font-semibold uppercase tracking-wider text-oh-accent-ink transition-colors hover:bg-oh-accent-hover sm:inline-flex"
          >
            {{ isZh ? '注册' : 'GET STARTED' }}
          </router-link>
        </template>
      </div>
    </div>
  </header>
</template>
