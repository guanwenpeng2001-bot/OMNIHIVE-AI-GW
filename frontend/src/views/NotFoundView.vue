<template>
  <div class="flex min-h-screen flex-col items-center justify-center bg-oh-canvas px-4 py-12 text-oh-primary">
    <div class="w-full max-w-md rounded-oh-lg border border-oh-default bg-oh-surface p-8 text-center shadow-oh-xs">
      <div class="mb-6 flex flex-col items-center">
        <span
          class="mb-4 inline-flex items-center rounded-oh-pill border border-oh-default bg-oh-subtle px-2.5 py-0.5 font-mono text-[11px] font-semibold uppercase tracking-widest text-oh-muted"
        >
          404 · {{ isZh ? '路由不存在' : 'ROUTE NOT FOUND' }}
        </span>
        <div
          class="flex h-14 w-14 items-center justify-center rounded-oh-lg border border-oh-default bg-oh-subtle"
        >
          <BrandMark size="md" hide-wordmark />
        </div>
      </div>

      <div class="mb-7">
        <h1 class="mb-2 text-xl font-semibold tracking-tight text-oh-primary">
          {{ headingText }}
        </h1>
        <p class="text-sm leading-relaxed text-oh-secondary">
          {{
            isZh
              ? '您访问的页面路径不存在或已迁移，请返回上一页或前往控制台与模型广场。'
              : "The page you are looking for doesn't exist or has been moved."
          }}
        </p>
      </div>

      <div class="flex flex-col justify-center gap-2.5 sm:flex-row">
        <button
          type="button"
          class="oh-focus-ring inline-flex h-9 items-center justify-center gap-1.5 rounded-oh-md border border-oh-default bg-oh-surface px-3.5 text-[13px] font-medium text-oh-primary shadow-oh-xs transition-colors hover:border-oh-strong hover:bg-oh-hover"
          @click="goBack"
        >
          <Icon name="arrowLeft" size="sm" />
          <span>{{ isZh ? '返回上一页' : 'Go Back' }}</span>
        </button>
        <router-link
          to="/dashboard"
          class="oh-focus-ring inline-flex h-9 items-center justify-center gap-1.5 rounded-oh-md border border-transparent bg-oh-accent px-3.5 text-[13px] font-semibold text-oh-accent-ink shadow-oh-xs transition-colors hover:bg-oh-accent-hover active:bg-oh-accent-active"
        >
          <Icon name="home" size="sm" />
          <span>{{ isZh ? '进入控制台' : 'Go to Console' }}</span>
        </router-link>
      </div>

      <div class="mt-6 flex items-center justify-center gap-4 border-t border-oh-subtle pt-4 text-xs text-oh-muted">
        <router-link
          to="/home"
          class="oh-focus-ring rounded-oh-xs font-medium text-oh-accent-text transition-colors hover:underline"
        >
          {{ isZh ? '返回首页' : 'Home Portal' }}
        </router-link>
        <span aria-hidden="true">·</span>
        <router-link
          to="/model-plaza"
          class="oh-focus-ring rounded-oh-xs font-medium text-oh-accent-text transition-colors hover:underline"
        >
          {{ isZh ? '浏览模型广场' : 'Model Plaza' }}
        </router-link>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { i18n as globalI18n } from '@/i18n'
import Icon from '@/components/icons/Icon.vue'
import BrandMark from '@/components/layout/BrandMark.vue'

const router = useRouter()

let i18nTranslate: ((key: string) => string) | undefined
let i18nLocale: { value?: string } | string | undefined
try {
  const composer = useI18n()
  i18nTranslate = composer?.t
  i18nLocale = composer?.locale
} catch {
  i18nTranslate = undefined
  i18nLocale = undefined
}

const isZh = computed(() => {
  const raw =
    (typeof i18nLocale === 'string' ? i18nLocale : i18nLocale?.value) ||
    globalI18n?.global?.locale?.value ||
    'zh'
  return String(raw).toLowerCase().startsWith('zh')
})

const headingText = computed(() => {
  const translated = i18nTranslate?.('errors.pageNotFound')
  if (translated && translated !== 'errors.pageNotFound') return translated
  return isZh.value ? '页面未找到' : 'Page Not Found'
})

function goBack(): void {
  router.back()
}
</script>
