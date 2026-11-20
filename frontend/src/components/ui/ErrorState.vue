<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { i18n as globalI18n } from '@/i18n'
import Button from './Button.vue'
import CopyButton from './CopyButton.vue'
import { cn } from './utils'

export interface ErrorStateProps {
  title: string
  message?: string
  variant?: 'error' | 'permission' | 'stale'
  code?: string | number
  requestId?: string
  requiredRole?: string
  lastSyncedAt?: string
  retryable?: boolean
  retryLabel?: string
  compact?: boolean
}

const props = withDefaults(defineProps<ErrorStateProps>(), {
  message: undefined,
  variant: 'error',
  code: undefined,
  requestId: undefined,
  requiredRole: undefined,
  lastSyncedAt: undefined,
  retryable: true,
  retryLabel: undefined,
  compact: false
})

const emit = defineEmits<{
  retry: []
}>()

let i18nLocale: { value?: string } | string | undefined
try {
  const composer = useI18n()
  i18nLocale = composer?.locale
} catch {
  i18nLocale = undefined
}

const isZh = computed(() => {
  const raw =
    (typeof i18nLocale === 'string' ? i18nLocale : i18nLocale?.value) ||
    globalI18n?.global?.locale?.value ||
    (typeof document !== 'undefined' ? document.documentElement?.lang : '') ||
    'en'
  return String(raw).toLowerCase().startsWith('zh')
})

const resolvedRetryLabel = computed(() => props.retryLabel ?? (isZh.value ? '重试' : 'Retry'))
</script>

<template>
  <div
    :role="variant === 'error' ? 'alert' : 'status'"
    :class="
      cn(
        'rounded-oh-lg border',
        compact ? 'p-3.5' : 'p-5',
        variant === 'error' && 'border-oh-status-danger-border bg-oh-status-danger-bg/60',
        variant === 'permission' && 'border-oh-default bg-oh-subtle',
        variant === 'stale' && 'border-oh-status-warn-border bg-oh-status-warn-bg/60'
      )
    "
  >
    <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div class="flex items-start gap-3 min-w-0">
        <span
          :class="
            cn(
              'mt-0.5 inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-oh-md border',
              variant === 'error' &&
                'border-oh-status-danger-border bg-oh-surface text-oh-status-danger-fg',
              variant === 'permission' &&
                'border-oh-default bg-oh-surface text-oh-secondary',
              variant === 'stale' &&
                'border-oh-status-warn-border bg-oh-surface text-oh-status-warn-fg'
            )
          "
          aria-hidden="true"
        >
          <svg
            v-if="variant === 'permission'"
            viewBox="0 0 18 18"
            class="h-4 w-4"
            fill="none"
            stroke="currentColor"
            stroke-width="1.6"
          >
            <rect x="4" y="8" width="10" height="7" rx="1.5" />
            <path stroke-linecap="round" d="M6.25 8V5.75a2.75 2.75 0 0 1 5.5 0V8" />
          </svg>
          <svg
            v-else
            viewBox="0 0 18 18"
            class="h-4 w-4"
            fill="none"
            stroke="currentColor"
            stroke-width="1.6"
          >
            <path stroke-linecap="round" stroke-linejoin="round" d="M9 6v3.75M9 12.5h.01M2.75 14.5h12.5L9 3.25 2.75 14.5z" />
          </svg>
        </span>

        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-2">
            <h4 class="text-sm font-semibold text-oh-primary">{{ title }}</h4>
            <span
              v-if="code !== undefined"
              class="rounded-oh-xs border border-oh-default bg-oh-surface px-1.5 py-0.5 font-mono text-[11px] text-oh-secondary"
            >
              {{ code }}
            </span>
          </div>

          <p v-if="message" class="mt-1 text-xs leading-5 text-oh-secondary">
            {{ message }}
          </p>

          <p v-if="variant === 'permission' && requiredRole" class="mt-1.5 text-xs text-oh-muted">
            {{ isZh ? '所需权限范围：' : 'Required access scope:' }}
            <span class="font-mono text-oh-primary">{{ requiredRole }}</span>
          </p>

          <p v-if="variant === 'stale' && lastSyncedAt" class="mt-1.5 font-mono text-xs text-oh-status-warn-fg">
            {{ isZh ? '最近验证同步：' : 'Last verified sync:' }} {{ lastSyncedAt }}
          </p>

          <div
            v-if="requestId"
            class="mt-2 inline-flex items-center gap-1.5 rounded-oh-sm border border-oh-default bg-oh-surface px-2 py-0.5"
          >
            <span class="font-mono text-[11px] text-oh-muted">request_id:</span>
            <code class="font-mono text-[11px] text-oh-primary">{{ requestId }}</code>
            <CopyButton
              :value="requestId"
              :aria-label="isZh ? '复制请求 ID' : 'Copy request ID'"
              variant="inline"
              size="xs"
              icon-only
            />
          </div>
        </div>
      </div>

      <div v-if="$slots.actions || (retryable && variant !== 'permission')" class="flex shrink-0 items-center gap-2">
        <slot name="actions">
          <Button variant="secondary" size="sm" @click="emit('retry')">
            {{ resolvedRetryLabel }}
          </Button>
        </slot>
      </div>
    </div>
  </div>
</template>
