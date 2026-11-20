<script setup lang="ts">
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import * as stores from '@/stores'
import { sanitizeUrl } from '@/utils/url'
import BrandMark from './BrandMark.vue'
import PublicHeader from './PublicHeader.vue'
import { activePublicShellCount } from './shellState'

const SiteICPFooter = defineAsyncComponent(() => import('./SiteICPFooter.vue'))

const SHELL_MESSAGES = {
  en: {
    brandSubtitle: 'Unified AI Inference Gateway',
    rightsReserved: 'All rights reserved.',
    footerNavAria: 'Footer navigation',
    models: 'Models',
    pricing: 'Pricing',
    status: 'Service Status',
    docsExternal: 'Docs ↗',
    docs: 'Docs',
    compatibility: 'Updates & Compatibility'
  },
  zh: {
    brandSubtitle: '统一 AI 推理网关',
    rightsReserved: '保留所有权利。',
    footerNavAria: '页脚快捷链接',
    models: '模型广场',
    pricing: '定价方案',
    status: '服务状态',
    docsExternal: '开放文档 ↗',
    docs: '接入文档',
    compatibility: '更新与兼容性'
  }
} as const

type ShellMessageKey = keyof typeof SHELL_MESSAGES.en

function toMessageFunctions<T extends Record<string, string>>(dict: T) {
  return Object.fromEntries(
    Object.entries(dict).map(([k, v]) => [k, () => v])
  ) as Record<keyof T, () => string>
}

withDefaults(
  defineProps<{
    prose?: boolean
    padded?: boolean
  }>(),
  {
    prose: false,
    padded: true
  }
)

const appStore = (() => {
  try {
    return typeof stores.useAppStore === 'function' ? stores.useAppStore() : null
  } catch {
    return null
  }
})()

const { t, locale } = useI18n({
  useScope: 'local',
  messages: {
    en: toMessageFunctions(SHELL_MESSAGES.en),
    zh: toMessageFunctions(SHELL_MESSAGES.zh),
    'zh-CN': toMessageFunctions(SHELL_MESSAGES.zh)
  }
})

function tr(key: ShellMessageKey): string {
  const translated = t(key)
  if (translated && translated !== key) return translated
  const active = String(locale?.value || 'en').toLowerCase()
  const dict = active.startsWith('zh') ? SHELL_MESSAGES.zh : SHELL_MESSAGES.en
  return dict[key]
}

const brand = computed(
  () =>
    appStore?.brandConfig ?? {
      siteName: appStore?.siteName || 'OmniHive',
      siteLogo: appStore?.siteLogo || '',
      subtitle: tr('brandSubtitle')
    }
)

const docUrl = computed(() =>
  sanitizeUrl(appStore?.cachedPublicSettings?.doc_url || appStore?.docUrl || '')
)

const currentYear = new Date().getFullYear()

onMounted(() => {
  activePublicShellCount.value += 1
})

onBeforeUnmount(() => {
  activePublicShellCount.value = Math.max(0, activePublicShellCount.value - 1)
})
</script>

<template>
  <div class="flex min-h-screen flex-col bg-oh-canvas text-oh-primary">
    <PublicHeader />

    <div v-if="$slots.subbar" class="border-b border-oh-default bg-oh-subtle">
      <div
        data-testid="public-shell-subbar"
        class="mx-auto w-full max-w-[1560px] px-4 py-2.5 sm:px-6 lg:px-8 2xl:px-10 3xl:px-12 min-[1560px]:border-x min-[1560px]:border-oh-default"
      >
        <slot name="subbar" />
      </div>
    </div>

    <main class="flex min-w-0 flex-1 flex-col">
      <div
        data-testid="public-shell-main"
        :class="[
          'mx-auto flex min-w-0 w-full max-w-[1560px] flex-1 flex-col min-[1560px]:border-x min-[1560px]:border-oh-default',
          padded ? 'px-4 py-8 sm:px-6 lg:px-8 lg:py-12 2xl:px-10 3xl:px-12' : ''
        ]"
      >
        <div
          data-testid="public-shell-content"
          :class="[prose ? 'mx-auto min-w-0 w-full max-w-[68ch]' : 'min-w-0 w-full flex-1']"
        >
          <slot />
        </div>
      </div>
    </main>

    <footer
      class="border-t border-oh-default bg-oh-subtle text-xs text-oh-muted"
    >
      <div
        data-testid="public-shell-footer"
        class="mx-auto w-full max-w-[1560px] px-4 py-6 sm:px-6 lg:px-8 2xl:px-10 3xl:px-12 min-[1560px]:border-x min-[1560px]:border-oh-default"
      >
        <slot name="footer">
          <div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
            <div class="flex flex-wrap items-center gap-3">
              <router-link
                to="/home"
                class="oh-focus-ring inline-flex items-center rounded-oh-sm"
              >
                <BrandMark
                  :site-name="brand.siteName"
                  :site-logo="brand.siteLogo"
                  size="sm"
                />
              </router-link>
              <span class="hidden text-oh-subtle sm:inline" aria-hidden="true">|</span>
              <span class="font-mono text-[11px] text-oh-muted">
                © {{ currentYear }} {{ brand.siteName }}.
                {{ tr('rightsReserved') }}
              </span>
              <SiteICPFooter inline />
            </div>

            <nav
              :aria-label="tr('footerNavAria')"
              class="flex flex-wrap items-center gap-x-5 gap-y-2 text-xs font-medium text-oh-secondary"
            >
              <router-link
                to="/model-plaza"
                class="oh-focus-ring rounded-oh-xs transition-colors hover:text-oh-primary"
              >
                {{ tr('models') }}
              </router-link>
              <router-link
                to="/pricing"
                class="oh-focus-ring rounded-oh-xs transition-colors hover:text-oh-primary"
              >
                {{ tr('pricing') }}
              </router-link>
              <router-link
                to="/status"
                class="oh-focus-ring rounded-oh-xs transition-colors hover:text-oh-primary"
              >
                {{ tr('status') }}
              </router-link>
              <a
                v-if="docUrl"
                :href="docUrl"
                target="_blank"
                rel="noopener noreferrer"
                class="oh-focus-ring rounded-oh-xs transition-colors hover:text-oh-primary"
              >
                {{ tr('docsExternal') }}
              </a>
              <router-link
                to="/docs"
                class="oh-focus-ring rounded-oh-xs transition-colors hover:text-oh-primary"
              >
                {{ tr('docs') }}
              </router-link>
              <router-link
                to="/compatibility"
                class="oh-focus-ring rounded-oh-xs transition-colors hover:text-oh-primary"
              >
                {{ tr('compatibility') }}
              </router-link>
            </nav>
          </div>
        </slot>
      </div>
    </footer>
  </div>
</template>
