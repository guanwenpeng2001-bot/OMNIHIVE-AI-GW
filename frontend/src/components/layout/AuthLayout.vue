<template>
  <div class="auth-backdrop-shell hive-dots">
    <main class="auth-main">
      <section class="auth-local-card" :class="{ 'auth-local-wide': wide }">
        <div class="auth-brand-head">
          <svg width="26" height="26" viewBox="0 0 32 32" fill="none" aria-hidden="true">
            <path d="M16 2 28.1 9v14L16 30 3.9 23V9Z" fill="var(--primary)" />
            <path d="M16 5.6 25 10.8v10.4L16 26.4 7 21.2V10.8Z" stroke="var(--brand)" stroke-width="1.8" />
            <path d="M16 10.2 21 13.1v5.8L16 21.8l-5-2.9v-5.8Z" fill="var(--brand)" />
            <circle cx="16" cy="16" r="2.1" fill="var(--primary)" />
          </svg>
          <div>
            <strong>OmniHive</strong>
            <p>中立 AI 算力中转网关</p>
          </div>
          <router-link to="/home" class="auth-home-link">← 首页</router-link>
        </div>
        <slot />
        <div v-if="$slots.footer" class="auth-footer">
          <slot name="footer" />
        </div>
      </section>
    </main>
    <header class="auth-public-header" @keydown.esc="themeMenuOpen = false">
      <router-link to="/home" class="auth-logo" aria-label="OmniHive 首页">
        <svg width="26" height="26" viewBox="0 0 32 32" fill="none" aria-hidden="true">
          <path d="M16 2 28.1 9v14L16 30 3.9 23V9Z" fill="var(--primary)" />
          <path d="M16 5.6 25 10.8v10.4L16 26.4 7 21.2V10.8Z" stroke="var(--brand)" stroke-width="1.8" />
          <path d="M16 10.2 21 13.1v5.8L16 21.8l-5-2.9v-5.8Z" fill="var(--brand)" />
          <circle cx="16" cy="16" r="2.1" fill="var(--primary)" />
        </svg>
        <strong>OmniHive</strong>
      </router-link>
      <button type="button" class="auth-search" aria-label="搜索模型和页面" @click="openCommandPalette()">
        <span aria-hidden="true">⌕</span>
        <span class="auth-search-label">搜索模型 / 页面…</span>
        <kbd>⌘K</kbd>
      </button>
      <nav class="auth-public-links" aria-label="公开站导航">
        <router-link v-if="modelPlazaEnabled" to="/model-plaza">模型</router-link>
        <router-link to="/key-usage">查用量</router-link>
        <router-link to="/help">指南</router-link>
        <router-link to="/home">首页</router-link>
      </nav>
      <div class="auth-public-tools">
        <router-link to="/dashboard" class="auth-console">控制台 ›</router-link>
        <LocaleSwitcher v-if="locale" />
        <div ref="themePickerRef" class="auth-theme-picker">
          <button type="button" class="auth-theme-trigger" :aria-expanded="themeMenuOpen" aria-label="切换设计方向" @click="themeMenuOpen = !themeMenuOpen">
            <span class="auth-theme-dot" :style="{ background: currentThemeMeta.c2 }"></span>
            <span class="auth-theme-label">{{ currentThemeMeta.shortName }}</span>
            <span aria-hidden="true">▾</span>
          </button>
          <div v-if="themeMenuOpen" class="auth-theme-menu" role="group" aria-label="设计方向">
            <button v-for="item in themes" :key="item.key" type="button" :aria-pressed="currentTheme === item.key" @click="handleSelectTheme(item.key)">
              <span class="auth-theme-dot" :style="{ background: item.c2 }"></span>
              <span>{{ item.name }}</span>
              <span v-if="currentTheme === item.key" aria-hidden="true">✓</span>
            </button>
          </div>
        </div>
        <button type="button" class="auth-mode-toggle" :title="isDark ? '切换浅色模式' : '切换深色模式'" aria-label="切换深浅色" :aria-pressed="isDark" @click="toggleDarkMode">
          <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" aria-hidden="true">
            <circle cx="12" cy="12" r="8" />
            <path d="M12 4a8 8 0 0 1 0 16Z" fill="currentColor" />
          </svg>
        </button>
        <router-link to="/login" class="auth-nav-login">登录</router-link>
        <router-link v-if="registrationEnabled" to="/register" class="auth-nav-register">免费注册</router-link>
      </div>
    </header>
  </div>
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import { useThemeDirection, type ThemeDirectionKey } from '@/composables/useThemeDirection'

withDefaults(defineProps<{ wide?: boolean }>(), { wide: false })

const appStore = useAppStore()
const { locale } = useI18n()
const LocaleSwitcher = defineAsyncComponent(() => import('@/components/common/LocaleSwitcher.vue'))
const { themes, currentTheme, currentThemeMeta, isDark, setThemeDirection, toggleDarkMode, openCommandPalette } = useThemeDirection()
const themeMenuOpen = ref(false)
const themePickerRef = ref<HTMLElement | null>(null)
const modelPlazaEnabled = computed(() => appStore.cachedPublicSettings?.model_plaza_enabled !== false)
const registrationEnabled = computed(() => appStore.cachedPublicSettings?.registration_enabled === true && !appStore.cachedPublicSettings?.backend_mode_enabled)

function handleSelectTheme(key: ThemeDirectionKey) {
  setThemeDirection(key)
  themeMenuOpen.value = false
}

function handleOutsideClick(event: MouseEvent) {
  if (themePickerRef.value && !themePickerRef.value.contains(event.target as Node)) {
    themeMenuOpen.value = false
  }
}

onMounted(() => {
  appStore.fetchPublicSettings?.()
  document.addEventListener('click', handleOutsideClick)
})

onBeforeUnmount(() => document.removeEventListener('click', handleOutsideClick))
</script>

<style scoped>
.auth-backdrop-shell {
  display: flex;
  flex-direction: column;
  min-height: 100vh;
  color: var(--foreground);
  background-color: var(--background);
  background-image: radial-gradient(640px circle at 50% 18%, color-mix(in srgb, var(--brand) 9%, transparent), transparent 70%), radial-gradient(circle, color-mix(in srgb, var(--brand) 18%, transparent) 1px, transparent 1px);
  background-size: 100% 100%, 18px 18px;
}
.auth-public-header {
  order: -1;
  flex-shrink: 0;
  position: sticky;
  top: 0;
  z-index: 40;
  display: flex;
  align-items: center;
  gap: 18px;
  min-height: 56px;
  padding: 10px 24px;
  border-bottom: 1px solid var(--border);
  background: var(--glass);
  backdrop-filter: blur(16px);
  font-size: 12px;
}
.auth-logo, .auth-public-links, .auth-public-tools, .auth-theme-trigger, .auth-search { display: flex; align-items: center; gap: 10px; }
.auth-logo { flex-shrink: 0; font-size: 15px; letter-spacing: -.02em; }
.auth-public-links { justify-content: center; gap: 20px; flex: 1; white-space: nowrap; }
.auth-public-tools { margin-left: auto; gap: 12px; white-space: nowrap; }
.auth-search { padding: 6px 10px; border: 1px solid var(--border); border-radius: 8px; color: var(--muted-foreground); background: var(--surface-2); }
.auth-search kbd { font-size: 10px; }
.auth-theme-picker { position: relative; }
.auth-theme-trigger { padding: 5px 9px; border: 1px solid var(--border); border-radius: 999px; background: var(--card); }
.auth-theme-dot { display: inline-block; width: 10px; height: 10px; flex-shrink: 0; border-radius: 50%; }
.auth-theme-menu { position: absolute; right: 0; top: calc(100% + 8px); width: 224px; padding: 6px; background: var(--popover); border: 1px solid var(--border); border-radius: 12px; box-shadow: 0 12px 30px #0002; }
.auth-theme-menu button { display: flex; align-items: center; gap: 10px; width: 100%; padding: 9px; text-align: left; border-radius: 8px; }
.auth-theme-menu button:hover, .auth-theme-menu button[aria-pressed='true'] { background: var(--secondary); }
.auth-mode-toggle { padding: 6px; border-radius: 6px; }
.auth-nav-register { padding: 7px 12px; border-radius: 7px; background: var(--primary); color: var(--primary-foreground); font-weight: 600; }
.auth-main { display: flex; justify-content: center; align-items: center; min-height: calc(100vh - 56px); padding: 32px 16px; }
.auth-local-card { position: relative; width: min(100%, 29.5rem); min-width: 0; padding: 28px; border: 1px solid var(--border); border-radius: 16px; background: var(--card); color: var(--card-foreground); box-shadow: 0 20px 50px -12px #00000017; }
.auth-local-card::before { content: ''; position: absolute; top: 0; left: 0; right: 0; height: 3px; border-radius: 16px 16px 0 0; background: linear-gradient(90deg, var(--brand), #fb923c, var(--primary)); }
.auth-local-wide { width: min(100%, 52rem); }
.auth-brand-head { display: flex; align-items: center; gap: 10px; margin-bottom: 16px; padding-bottom: 14px; border-bottom: 1px solid var(--border); }
.auth-brand-head svg { flex-shrink: 0; }
.auth-brand-head strong { font-size: 14px; font-weight: 750; letter-spacing: -.02em; }
.auth-brand-head p { font-size: 11px; color: var(--muted-foreground); }
.auth-home-link { margin-left: auto; padding: 5px 8px; border-radius: 6px; font-size: 12px; }
.auth-home-link:hover { background: var(--secondary); }
.auth-footer { margin-top: 16px; font-size: 13px; }
.auth-local-card :deep(.auth-title) { font-size: 23px; line-height: 1.25; font-weight: 750; letter-spacing: -.02em; text-align: left; }
.auth-local-card :deep(.auth-intro) { margin: 6px 0 16px; color: var(--muted-foreground); font-size: 13.5px; line-height: 1.55; }
.auth-local-card :deep(.auth-summary) { padding: 12px; margin: 12px 0; background: var(--surface-2); border: 1px solid var(--border); border-radius: 10px; font-size: 13px; overflow-wrap: anywhere; }
.auth-local-card :deep(.auth-actions) { display: flex; flex-wrap: wrap; align-items: stretch; gap: 8px; margin-top: 16px; }
.auth-local-card :deep(.auth-actions > *) { flex: 1; min-width: 110px; }
.auth-local-card :deep(.auth-actions .btn) { width: 100%; }
.auth-local-card :deep(.auth-error) { padding: 10px 12px; border: 1px solid color-mix(in srgb, var(--bad) 35%, var(--border)); border-radius: 8px; background: color-mix(in srgb, var(--bad) 8%, var(--card)); color: var(--bad-text); font-size: 13px; }
.auth-local-card :deep(.input) { width: 100%; min-width: 0; min-height: 40px; border-radius: 8px; font-size: 14px; background: var(--card); color: var(--foreground); box-shadow: none; }
.auth-local-card :deep(.input-label) { margin-bottom: 4px; font-size: 13px; font-weight: 500; }
.auth-local-card :deep(.btn) { min-height: 38px; border-radius: 8px; font-size: 13px; box-shadow: none; background-image: none; transform: none; }
.auth-local-card :deep(.btn-primary) { background: var(--primary); color: var(--primary-foreground); }
.auth-local-card :deep(.btn-outline) { background: var(--card); border: 1px solid var(--border); color: var(--foreground); }
.auth-local-card :deep(.btn-outline:hover:not(:disabled)) { background: var(--surface-2); }
.auth-local-card :deep(input[type='checkbox']) { accent-color: var(--brand); }
.auth-local-card :deep(.auth-field-error) { margin-top: 4px; font-size: 12px; color: var(--bad-text); }
:is(button, a):focus-visible, .auth-local-card :deep(:is(button, a, input):focus-visible) { outline: 2px solid var(--ring); outline-offset: 3px; }
@media (max-width: 1120px) { .auth-search-label { display: none; } .auth-public-header { gap: 12px; padding-inline: 16px; } .auth-public-tools { gap: 8px; } }
@media (max-width: 800px) { .auth-public-header { flex-wrap: wrap; } .auth-public-links { order: 3; flex-basis: 100%; justify-content: flex-start; } .auth-console, .auth-nav-login { display: none; } }
@media (max-width: 520px) { .auth-public-header { gap: 8px; padding: 10px 12px; } .auth-logo strong, .auth-theme-label { display: none; } .auth-public-tools { gap: 5px; } .auth-main { align-items: flex-start; padding: 20px 12px; } .auth-local-card { padding: 20px; } }
</style>
