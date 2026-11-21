<template>
  <!-- Custom Home Content: Full Page Mode -->
  <div v-if="hasHomeContent" class="min-h-screen">
    <iframe
      v-if="isHomeContentUrl"
      :src="homeContent.trim()"
      class="h-screen w-full border-0"
      :sandbox="embeddedSandbox"
      :title="siteName"
      referrerpolicy="no-referrer"
      allowfullscreen
    ></iframe>
    <div v-else v-html="sanitizedHomeContent"></div>
  </div>

  <!-- Compact Home Page -->
  <div
    v-else-if="compactHomeEnabled"
    data-testid="compact-home"
    class="compact-portal flex min-h-screen flex-col bg-[var(--background)] text-[var(--foreground)]"
  >
    <header class="border-b border-[var(--border)] px-4 py-4 sm:px-6">
      <nav class="mx-auto flex max-w-5xl flex-wrap items-center justify-between gap-3 sm:gap-4">
        <div class="flex min-w-0 flex-1 items-center gap-3">
          <img
            v-if="siteLogo"
            :src="siteLogo"
            alt="Logo"
            class="h-9 w-9 shrink-0 rounded-lg object-contain"
          />
          <svg v-else class="h-9 w-9 shrink-0" viewBox="0 0 32 32" fill="none" role="img" aria-label="OmniHive">
            <path d="M16 2 28 9v14L16 30 4 23V9Z" fill="var(--primary)" />
            <path d="M16 6 24.5 11v10L16 26 7.5 21V11Z" stroke="var(--brand)" stroke-width="1.5" />
            <path d="m16 11 4.3 2.5v5L16 21l-4.3-2.5v-5Z" fill="var(--brand)" />
          </svg>
          <span class="min-w-0 truncate text-base font-semibold">{{ siteName }}</span>
        </div>
        <div class="flex max-w-full shrink-0 flex-wrap items-center justify-end gap-2">
          <LocaleSwitcher />
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-[var(--muted-foreground)] hover:bg-[var(--surface-2)]"
            :title="t('home.viewDocs')"
            :aria-label="t('home.viewDocs')"
          >
            <Icon name="book" size="md" />
          </a>
          <router-link
            v-if="showModelPlazaEntry"
            to="/model-plaza"
            class="flex h-10 shrink-0 items-center gap-1.5 rounded-lg px-2.5 text-sm font-medium text-[var(--muted-foreground)] hover:bg-[var(--surface-2)] hover:text-[var(--foreground)]"
            :title="t('nav.modelPlaza')"
            :aria-label="t('nav.modelPlaza')"
          >
            <Icon name="grid" size="md" />
            <span class="hidden sm:inline">{{ t('nav.modelPlaza') }}</span>
          </router-link>
          <button
            class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-[var(--muted-foreground)] hover:bg-[var(--surface-2)]"
            :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            :aria-label="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            @click="toggleTheme"
          >
            <Icon v-if="isDark" name="sun" size="md" />
            <Icon v-else name="moon" size="md" />
          </button>
          <router-link
            :to="isAuthenticated ? dashboardPath : '/login'"
            class="inline-flex min-h-10 shrink-0 items-center justify-center rounded-lg bg-[var(--primary)] px-4 py-2 text-sm font-medium text-white hover:bg-[var(--primary)]"
          >
            {{ isAuthenticated ? t('home.dashboard') : t('home.login') }}
          </router-link>
        </div>
      </nav>
    </header>

    <main class="flex min-w-0 flex-1 items-center justify-center px-4 py-16 sm:px-6">
      <div class="min-w-0 max-w-2xl text-center">
        <img
          v-if="siteLogo"
          :src="siteLogo"
          alt="Logo"
          class="mx-auto mb-6 h-20 w-20 rounded-2xl object-contain"
        />
        <div v-else class="compact-emblem mx-auto mb-6 flex h-24 w-24 items-center justify-center rounded-2xl border border-[var(--border)] bg-[var(--card)]">
          <svg class="h-16 w-16" viewBox="0 0 32 32" fill="none" role="img" aria-label="OmniHive">
            <path d="M16 2 28 9v14L16 30 4 23V9Z" fill="var(--primary)" />
            <path d="M16 6 24.5 11v10L16 26 7.5 21V11Z" stroke="var(--brand)" stroke-width="1.5" />
            <path d="m16 11 4.3 2.5v5L16 21l-4.3-2.5v-5Z" fill="var(--brand)" />
          </svg>
        </div>
        <h1 class="[overflow-wrap:anywhere] text-3xl font-bold md:text-4xl">{{ siteName }}</h1>
        <p class="mt-4 whitespace-pre-wrap [overflow-wrap:anywhere] text-base text-[var(--muted-foreground)]">{{ siteSubtitle }}</p>
        <router-link
          :to="isAuthenticated ? dashboardPath : '/login'"
          class="mt-8 inline-flex min-h-10 items-center justify-center rounded-lg bg-primary-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-primary-700"
        >
          {{ isAuthenticated ? t('home.goToDashboard') : t('home.login') }}
        </router-link>
      </div>
    </main>

    <footer class="min-w-0 border-t border-[var(--border)] px-4 py-5 text-center text-sm text-[var(--muted-foreground)] [overflow-wrap:anywhere] sm:px-6">
      &copy; {{ currentYear }} {{ siteName }}
    </footer>
  </div>

  <!-- Default Home Page: Zero-Fake-Data Gateway Portal -->
  <div
    v-else
    class="omnihive-portal flex min-h-screen flex-col bg-[var(--background)] text-[var(--foreground)]"
  >
    <header class="border-b border-[var(--border)] px-4 py-4 sm:px-6">
      <nav class="mx-auto flex max-w-6xl flex-wrap items-center justify-between gap-3 sm:gap-4">
        <div class="flex min-w-0 flex-1 items-center gap-3">
          <img
            v-if="siteLogo"
            :src="siteLogo"
            alt="Logo"
            class="h-9 w-9 shrink-0 rounded-lg object-contain"
          />
          <svg v-else class="h-9 w-9 shrink-0" viewBox="0 0 32 32" fill="none" role="img" aria-label="OmniHive">
            <path d="M16 2 28 9v14L16 30 4 23V9Z" fill="var(--primary)" />
            <path d="M16 6 24.5 11v10L16 26 7.5 21V11Z" stroke="var(--brand)" stroke-width="1.5" />
            <path d="m16 11 4.3 2.5v5L16 21l-4.3-2.5v-5Z" fill="var(--brand)" />
          </svg>
          <span class="min-w-0 truncate text-base font-semibold">{{ siteName }}</span>
        </div>
        <div class="flex max-w-full shrink-0 flex-wrap items-center justify-end gap-2">
          <LocaleSwitcher />
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-[var(--muted-foreground)] hover:bg-[var(--surface-2)]"
            :title="t('home.viewDocs')"
            :aria-label="t('home.viewDocs')"
          >
            <Icon name="book" size="md" />
          </a>
          <router-link
            v-if="showModelPlazaEntry"
            to="/model-plaza"
            class="flex h-10 shrink-0 items-center gap-1.5 rounded-lg px-2.5 text-sm font-medium text-[var(--muted-foreground)] hover:bg-[var(--surface-2)] hover:text-[var(--foreground)]"
            :title="t('nav.modelPlaza')"
            :aria-label="t('nav.modelPlaza')"
          >
            <Icon name="grid" size="md" />
            <span class="hidden sm:inline">{{ t('nav.modelPlaza') }}</span>
          </router-link>
          <button
            class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-[var(--muted-foreground)] hover:bg-[var(--surface-2)]"
            :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            :aria-label="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            @click="toggleTheme"
          >
            <Icon v-if="isDark" name="sun" size="md" />
            <Icon v-else name="moon" size="md" />
          </button>
          <router-link
            :to="isAuthenticated ? dashboardPath : '/login'"
            class="inline-flex min-h-10 shrink-0 items-center justify-center rounded-lg bg-[var(--primary)] px-4 py-2 text-sm font-medium text-white hover:bg-[var(--primary)]"
          >
            {{ isAuthenticated ? t('home.dashboard') : t('home.login') }}
          </router-link>
        </div>
      </nav>
    </header>

    <main class="mx-auto grid w-full max-w-6xl min-w-0 flex-1 items-center gap-10 px-4 py-14 sm:px-6 lg:grid-cols-12 lg:py-20">
      <div class="min-w-0 space-y-5 lg:col-span-6">
        <h1 class="[overflow-wrap:anywhere] text-3xl font-bold tracking-tight sm:text-4xl">{{ siteName }}</h1>
        <p class="whitespace-pre-wrap [overflow-wrap:anywhere] text-base leading-relaxed text-[var(--muted-foreground)]">
          {{ siteSubtitle }}
        </p>
        <div class="flex flex-wrap items-center gap-3 pt-2">
          <router-link
            :to="isAuthenticated ? dashboardPath : '/login'"
            class="inline-flex min-h-10 items-center justify-center rounded-lg bg-[var(--primary)] px-5 py-2.5 text-sm font-medium text-white"
          >
            {{ isAuthenticated ? t('home.goToDashboard') : t('home.login') }}
          </router-link>
          <router-link
            v-if="showModelPlazaEntry"
            to="/model-plaza"
            class="inline-flex min-h-10 items-center justify-center rounded-lg border border-[var(--border)] bg-[var(--card)] px-5 py-2.5 text-sm font-medium text-[var(--foreground)]"
          >
            {{ t('nav.modelPlaza') }}
          </router-link>
        </div>
      </div>

      <div class="terminal-container min-w-0 overflow-hidden rounded-xl border border-[var(--border)] bg-[var(--card)] lg:col-span-6">
        <div class="border-b border-[var(--border)] px-4 py-2.5 font-mono text-xs text-[var(--muted-foreground)]">
          {{ gatewayOrigin }}/v1/chat/completions
        </div>
        <pre class="overflow-x-auto p-4 font-mono text-xs leading-relaxed text-[var(--foreground)]"><code>{{ quickstartSnippet }}</code></pre>
      </div>
    </main>

    <footer class="min-w-0 border-t border-[var(--border)] px-4 py-5 text-center text-sm text-[var(--muted-foreground)] [overflow-wrap:anywhere] sm:px-6">
      &copy; {{ currentYear }} {{ siteName }}
    </footer>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import { sanitizeUrl } from '@/utils/url'
import { embeddedSandbox, sanitizeEmbeddedHtml } from '@/utils/embedded-url'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
import { useThemeDirection } from '@/composables/useThemeDirection'

const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()
const { isDark, initThemeDirection, setDarkMode, toggleDarkMode } = useThemeDirection()

const siteName = computed(
  () => appStore.cachedPublicSettings?.site_name?.trim() || appStore.siteName?.trim() || 'OmniHive'
)
const siteLogo = computed(() =>
  sanitizeUrl(appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '', {
    allowRelative: true,
    allowDataUrl: true
  })
)
const siteSubtitle = computed(() => {
  const configured = appStore.cachedPublicSettings?.site_subtitle?.trim()
  return !configured || /omniscription|sub2api/i.test(configured)
    ? '智能算力中转蜂巢 · 一个入口，连接模型与 Agent。'
    : configured
})
const docUrl = computed(() =>
  sanitizeUrl(appStore.cachedPublicSettings?.doc_url || appStore.docUrl || '')
)
const homeContent = computed(() => appStore.cachedPublicSettings?.home_content || '')
const sanitizedHomeContent = computed(() => sanitizeEmbeddedHtml(homeContent.value))
const hasHomeContent = computed(() => homeContent.value.trim().length > 0)
const compactHomeEnabled = computed(() => appStore.cachedPublicSettings?.compact_home_enabled === true)
const modelPlazaEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.modelPlaza))

const isHomeContentUrl = computed(() => {
  const content = homeContent.value.trim()
  return content.startsWith('http://') || content.startsWith('https://')
})

const isAuthenticated = computed(() => authStore.isAuthenticated)
const modelPlazaRequiresAuth = computed(
  () => appStore.cachedPublicSettings?.model_plaza_require_auth === true
)
const showModelPlazaEntry = computed(
  () => modelPlazaEnabled.value && (isAuthenticated.value || !modelPlazaRequiresAuth.value)
)
const isAdmin = computed(() => authStore.isAdmin)
const dashboardPath = computed(() => (isAdmin.value ? '/admin/dashboard' : '/dashboard'))
const currentYear = computed(() => new Date().getFullYear())

const gatewayOrigin = computed(() => {
  if (typeof window !== 'undefined' && window.location?.origin) {
    return window.location.origin
  }
  return 'https://api.omnihive.dev'
})

const quickstartSnippet = computed(
  () => `curl ${gatewayOrigin.value}/v1/chat/completions \\
  -H "Authorization: Bearer $OMNIHIVE_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"openai/gpt-5.6-sol","messages":[{"role":"user","content":"hello"}]}'`
)

function toggleTheme() {
  toggleDarkMode()
}

function initTheme() {
  initThemeDirection()
  const savedTheme = localStorage.getItem('theme')
  if (
    savedTheme === 'dark' ||
    (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)
  ) {
    setDarkMode(true)
  } else {
    setDarkMode(false)
  }
}

initTheme()

onMounted(() => {
  initTheme()
  authStore.checkAuth()
  if (!appStore.publicSettingsLoaded) {
    appStore.fetchPublicSettings()
  }
})
</script>

<style scoped>
.compact-emblem {
  box-shadow: 0 0 64px color-mix(in srgb, var(--brand) 12%, transparent);
}
.compact-portal :is(a, button):focus-visible,
.omnihive-portal :is(a, button):focus-visible {
  outline: 2px solid var(--brand);
  outline-offset: 3px;
}
.compact-portal header a:last-child,
.omnihive-portal header a:last-child {
  color: var(--primary-foreground);
}
</style>
