<template>
  <header
    class="omnihive-plaza-topbar sticky top-0 z-30 border-b"
  >
    <div class="mx-auto flex h-14 max-w-[1280px] items-center justify-between gap-3 px-4 sm:px-6 lg:px-8">
      <!-- 左:站点 logo + 名称 -->
      <div class="flex min-w-0 items-center gap-3">
        <template v-if="settings">
          <RouterLink
            to="/"
            class="group flex min-w-0 items-center gap-2.5 text-left"
            :title="siteName"
            :data-logo="Boolean(siteLogo)"
          >
            <span
              class="flex h-9 w-9 flex-shrink-0 items-center justify-center overflow-hidden rounded-xl border border-amber-500/25 bg-[var(--card)] shadow-sm transition-transform duration-200 group-hover:scale-105 "
            >
              <img v-if="siteLogo" :src="siteLogo" alt="Logo" class="h-full w-full object-contain" />
              <svg v-else width="22" height="22" viewBox="0 0 32 32" fill="none" aria-hidden="true">
                <path d="M16 2 28.1 9v14L16 30 3.9 23V9Z" fill="var(--primary, #16161a)" />
                <path d="M16 5.6 25 10.8v10.4L16 26.4 7 21.2V10.8Z" stroke="var(--brand, #f59e0b)" stroke-width="1.8" />
                <path d="M16 10.2 21 13.1v5.8L16 21.8l-5-2.9v-5.8Z" fill="var(--brand, #f59e0b)" />
                <circle cx="16" cy="16" r="2.1" fill="var(--primary, #16161a)" />
              </svg>
            </span>
            <div class="min-w-0 leading-tight">
              <div class="flex items-center gap-1.5">
                <span class="truncate text-sm font-bold tracking-tight text-[var(--foreground)] ">
                  {{ siteName }}
                </span>
                <span class="omnihive-plaza-badge hidden sm:inline-block">MODELS</span>
              </div>
              <div class="hidden truncate text-[10px] opacity-60 sm:block">模型广场与实时倍率目录</div>
            </div>
          </RouterLink>
        </template>
        <template v-else>
          <span class="h-9 w-9 flex-shrink-0 animate-pulse rounded-xl bg-[var(--surface-2)] " aria-hidden="true"></span>
          <span class="h-5 w-28 animate-pulse rounded bg-[var(--surface-2)] " aria-hidden="true"></span>
        </template>
        <button
          type="button"
          class="omnihive-cmdk-trigger hidden md:inline-flex"
          aria-label="搜索模型和页面 (⌘K)"
          @click="openCommandPalette"
        >
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <circle cx="11" cy="11" r="8" />
            <path d="m21 21-4.3-4.3" />
          </svg>
          <span class="text-xs">快速跳转 / 搜索…</span>
          <span class="omnihive-kbd">⌘K</span>
        </button>
      </div>

      <!-- 右:主题切换 + 明暗切换 + 登录 / 回到后台 -->
      <div class="flex items-center gap-2">
        <RouterLink to="/help" class="hidden text-xs text-[var(--muted-foreground)] hover:text-[var(--brand-text)] md:inline">接入指南</RouterLink>
        <RouterLink to="/key-usage" class="hidden text-xs text-[var(--muted-foreground)] hover:text-[var(--brand-text)] md:inline">查用量</RouterLink>
        <div class="relative hidden sm:block">
          <button
            type="button"
            class="omnihive-nav-pill inline-flex"
            :aria-expanded="showThemeMenu"
            aria-label="切换设计方向"
            @click="showThemeMenu = !showThemeMenu"
          >
            <span class="h-2.5 w-2.5 rounded-full" :style="{ background: currentThemeMeta.c2 }"></span>
            <span>{{ currentThemeMeta.shortName }}</span>
            <span class="text-[10px] opacity-60">▾</span>
          </button>
          <div
            v-if="showThemeMenu"
            class="omnihive-theme-menu right-0 mt-1.5 w-52 p-1"
            role="menu"
          >
            <button
              v-for="item in themes"
              :key="item.key"
              type="button"
              class="omnihive-theme-opt text-left text-xs"
              :class="{ 'is-active font-semibold': currentTheme === item.key }"
              @click="selectTheme(item.key)"
            >
              <span class="h-2.5 w-2.5 rounded-full shrink-0" :style="{ background: item.c2 }"></span>
              <span class="truncate">{{ item.name }}</span>
            </button>
          </div>
        </div>

        <button
          type="button"
          class="omnihive-icon-btn"
          :title="isDark ? '切换为浅色模式' : '切换为深色模式'"
          aria-label="切换深浅色"
          @click="toggleDarkMode()"
        >
          <span class="text-xs">🌓</span>
        </button>

        <RouterLink
          v-if="isAuthenticated"
          :to="backTarget"
          class="omnihive-cta-btn"
        >
          <span>{{ t('modelPlaza.nav.backToDashboard') }}</span>
          <span aria-hidden="true">→</span>
        </RouterLink>
        <RouterLink
          v-else
          :to="{ path: '/login', query: { redirect: '/model-plaza' } }"
          class="inline-flex flex-shrink-0 items-center justify-center rounded-xl bg-gradient-to-r from-primary-500 to-primary-600 px-4 py-2 text-sm font-semibold text-white shadow-md shadow-primary-500/25 transition-all duration-200 hover:from-primary-600 hover:to-primary-700 hover:shadow-lg hover:shadow-primary-500/30 active:scale-[0.98] dark:shadow-primary-500/20"
        >
          {{ t('modelPlaza.nav.login') }}
        </RouterLink>
      </div>
    </div>
  </header>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { sanitizeUrl } from '@/utils/url'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useThemeDirection, type ThemeDirectionKey } from '@/composables/useThemeDirection'

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const {
  themes,
  currentTheme,
  currentThemeMeta,
  isDark,
  setThemeDirection,
  toggleDarkMode,
  openCommandPalette,
} = useThemeDirection()
const showThemeMenu = ref(false)

const settings = computed(() => appStore.cachedPublicSettings)
const siteName = computed(() => settings.value?.site_name?.trim() || 'OmniHive')
const siteLogo = computed(() =>
  sanitizeUrl(settings.value?.site_logo || '', { allowRelative: true, allowDataUrl: true })
)
const isAuthenticated = computed(() => authStore.isAuthenticated)
const backTarget = computed(() => (authStore.isAdmin ? '/admin/dashboard' : '/dashboard'))

function selectTheme(key: ThemeDirectionKey) {
  setThemeDirection(key)
  showThemeMenu.value = false
}
</script>

<style scoped>
.omnihive-plaza-topbar {
  background: var(--glass, rgba(250, 250, 250, 0.84));
  border-color: var(--border, #e9e9ee);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
}

:global(.dark) .omnihive-plaza-topbar {
  background: var(--glass, rgba(19, 19, 22, 0.84));
  border-color: var(--border, #232329);
}

.omnihive-plaza-badge {
  border-radius: 9999px;
  padding: 1px 6px;
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: 10px;
  font-weight: 600;
  background: color-mix(in srgb, var(--brand, #f59e0b) 16%, transparent);
  color: var(--brand-text, #92610a);
}

:global(.dark) .omnihive-plaza-badge {
  color: var(--brand, #fbbf24);
}

.omnihive-cmdk-trigger {
  align-items: center;
  gap: 8px;
  border-radius: 8px;
  border: 1px solid var(--border, #e9e9ee);
  background: var(--surface-2, #f4f4f5);
  color: var(--muted-foreground, #6f6f78);
  padding: 5px 10px;
  transition: all 0.16s ease;
}

.omnihive-cmdk-trigger:hover {
  border-color: var(--brand, #f59e0b);
  color: var(--foreground, #131316);
}

.omnihive-kbd {
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: 10px;
  padding: 1px 5px;
  border-radius: 4px;
  border: 1px solid var(--border, #e9e9ee);
  background: var(--card, #ffffff);
}

.omnihive-nav-pill {
  align-items: center;
  gap: 6px;
  border-radius: 8px;
  border: 1px solid var(--border, #e9e9ee);
  background: var(--card, #ffffff);
  padding: 5px 10px;
  font-size: 12.5px;
  font-weight: 500;
  transition: all 0.16s ease;
}

.omnihive-nav-pill:hover {
  border-color: color-mix(in srgb, var(--brand, #f59e0b) 45%, var(--border, #e9e9ee));
  background: var(--secondary, #f4f4f5);
}

.omnihive-icon-btn {
  display: inline-flex;
  height: 32px;
  width: 32px;
  align-items: center;
  justify-content: center;
  border-radius: 8px;
  border: 1px solid var(--border, #e9e9ee);
  background: var(--card, #ffffff);
  transition: all 0.16s ease;
}

.omnihive-icon-btn:hover {
  border-color: color-mix(in srgb, var(--brand, #f59e0b) 45%, var(--border, #e9e9ee));
  background: var(--secondary, #f4f4f5);
}

.omnihive-cta-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  border-radius: 9px;
  background: var(--primary, #16161a);
  color: var(--primary-foreground, #ffffff);
  padding: 6px 13px;
  font-size: 12.5px;
  font-weight: 600;
  transition: transform 0.16s ease, filter 0.16s ease;
}

.omnihive-cta-btn:hover {
  filter: brightness(1.08);
  transform: translateY(-1px);
}

.omnihive-theme-menu {
  position: absolute;
  z-index: 50;
  border-radius: 12px;
  border: 1px solid var(--border, #e9e9ee);
  background: var(--popover, #ffffff);
  box-shadow: 0 14px 32px rgba(0, 0, 0, 0.14);
}

.omnihive-theme-opt {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 8px;
  border-radius: 7px;
  padding: 6px 9px;
  transition: background 0.15s ease;
}

.omnihive-theme-opt:hover,
.omnihive-theme-opt.is-active {
  background: var(--secondary, #f4f4f5);
}
</style>
