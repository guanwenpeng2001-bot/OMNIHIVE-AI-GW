<template>
  <div
    class="hive-dots relative flex min-h-screen items-center justify-center overflow-hidden px-4 py-12"
    style="background: var(--background); color: var(--foreground)"
  >
    <div class="card relative z-10 w-full max-w-md p-8 text-center">
      <div class="mb-6 flex flex-col items-center">
        <span class="pill pill-ghost mb-4 font-mono text-[11px] tracking-widest uppercase">
          [404] · ROUTE NOT FOUND
        </span>
        <div
          class="flex h-16 w-16 items-center justify-center rounded-2xl border"
          style="border-color: var(--border); background: var(--surface-2)"
        >
          <svg width="34" height="34" viewBox="0 0 32 32" fill="none" aria-hidden="true">
            <path d="M16 2 28.1 9v14L16 30 3.9 23V9Z" fill="var(--primary, #16161a)" />
            <path d="M16 5.6 25 10.8v10.4L16 26.4 7 21.2V10.8Z" stroke="var(--brand, #f59e0b)" stroke-width="1.8" />
            <path d="M16 10.2 21 13.1v5.8L16 21.8l-5-2.9v-5.8Z" fill="var(--brand, #f59e0b)" />
            <circle cx="16" cy="16" r="2.1" fill="var(--primary, #16161a)" />
          </svg>
        </div>
      </div>

      <div class="mb-7">
        <h1 class="mb-2 text-2xl font-bold tracking-tight">
          {{ t('errors.pageNotFound') }}
        </h1>
        <p class="text-sm leading-relaxed" style="color: var(--muted-foreground)">
          {{ isZh ? '您访问的页面路径不存在或已迁移，请返回上一页或进入蜂巢控制台。' : "The page you are looking for doesn't exist or has been moved." }}
        </p>
      </div>

      <div class="flex flex-col justify-center gap-2.5 sm:flex-row">
        <button @click="goBack" class="btn btn-secondary">
          <Icon name="arrowLeft" size="sm" />
          <span>{{ isZh ? '返回上一页' : 'Go Back' }}</span>
        </button>
        <router-link to="/dashboard" class="btn btn-primary">
          <Icon name="home" size="sm" />
          <span>{{ isZh ? '进入控制台' : 'Go to Console' }}</span>
        </router-link>
      </div>

      <div class="mt-6 flex items-center justify-center gap-4 border-t pt-4 text-xs" style="border-color: var(--border); color: var(--muted-foreground)">
        <router-link
          to="/home"
          class="transition-colors hover:underline"
          style="color: var(--brand-text)"
        >
          {{ isZh ? '返回首页' : 'Home Portal' }}
        </router-link>
        <span aria-hidden="true">·</span>
        <router-link
          to="/model-plaza"
          class="transition-colors hover:underline"
          style="color: var(--brand-text)"
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
import Icon from '@/components/icons/Icon.vue'

const { t, locale } = useI18n()
const router = useRouter()
const isZh = computed(() => String(locale.value || '').startsWith('zh'))

function goBack(): void {
  router.back()
}
</script>
