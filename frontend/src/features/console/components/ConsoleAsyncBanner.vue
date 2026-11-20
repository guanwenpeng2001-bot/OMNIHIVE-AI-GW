<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Badge from '@/components/ui/Badge.vue'
import Button from '@/components/ui/Button.vue'
import CopyButton from '@/components/ui/CopyButton.vue'
import type { ConsoleAsyncNotice } from '../asyncState'

const props = withDefaults(
  defineProps<{
    notice: ConsoleAsyncNotice
    retryLabel?: string
    dismissible?: boolean
  }>(),
  {
    retryLabel: undefined,
    dismissible: false
  }
)

const { locale } = useI18n()
const isZh = computed(() => String(locale?.value || '').startsWith('zh'))
const effectiveRetryLabel = computed(() => props.retryLabel || (isZh.value ? '重试' : 'Retry'))

const emit = defineEmits<{
  retry: []
  dismiss: []
}>()
</script>

<template>
  <div
    :data-testid="`console-state-${notice.kind}`"
    :role="notice.kind === 'error' || notice.kind === 'conflict' ? 'alert' : 'status'"
    :class="[
      'rounded-oh-lg border p-3.5 text-xs',
      notice.kind === 'error' && 'border-oh-status-danger-border bg-oh-status-danger-bg text-oh-primary',
      notice.kind === 'permission' && 'border-oh-default bg-oh-subtle text-oh-primary',
      notice.kind === 'stale' && 'border-oh-status-warn-border bg-oh-status-warn-bg text-oh-primary',
      notice.kind === 'conflict' && 'border-oh-status-warn-border bg-oh-status-warn-bg text-oh-primary',
      notice.kind === 'unknown' && 'border-dashed border-oh-strong bg-oh-subtle text-oh-primary',
      notice.kind === 'confirmed' && 'border-oh-status-ok-border bg-oh-status-ok-bg text-oh-primary',
      notice.kind === 'refreshing' && 'border-oh-subtle bg-oh-subtle text-oh-secondary'
    ]"
  >
    <div class="flex flex-col gap-2.5 sm:flex-row sm:items-start sm:justify-between">
      <div class="min-w-0 space-y-1">
        <div class="flex flex-wrap items-center gap-2">
          <Badge
            :tone="
              notice.kind === 'error'
                ? 'danger'
                : notice.kind === 'confirmed'
                  ? 'success'
                  : notice.kind === 'conflict' || notice.kind === 'stale'
                    ? 'warning'
                    : notice.kind === 'unknown'
                      ? 'unknown'
                      : 'neutral'
            "
            variant="subtle"
            mono
          >
            {{ String(notice.code || notice.kind).toUpperCase() }}
          </Badge>
          <p class="font-semibold text-oh-primary">{{ notice.title }}</p>
        </div>

        <p v-if="notice.message" class="text-oh-secondary leading-relaxed">
          {{ notice.message }}
        </p>

        <div
          v-if="notice.requiredGrant || notice.lastSyncedAt || notice.requestId"
          class="flex flex-wrap items-center gap-x-3 gap-y-1 pt-0.5 font-mono text-[11px] text-oh-muted"
        >
          <span v-if="notice.requiredGrant">
            {{ isZh ? '所需权限：' : 'Grant: ' }}<code class="text-oh-primary">{{ notice.requiredGrant }}</code>
          </span>
          <span v-if="notice.lastSyncedAt">
            {{ isZh ? '上次同步：' : 'Last synced: ' }}{{ notice.lastSyncedAt }}
          </span>
          <span v-if="notice.requestId" class="inline-flex items-center gap-1">
            <span>{{ isZh ? '请求 ID：' : 'req: ' }}{{ notice.requestId }}</span>
            <CopyButton :value="notice.requestId" size="xs" :aria-label="isZh ? '复制请求 ID' : 'Copy request ID'" />
          </span>
        </div>
      </div>

      <div v-if="notice.retryable || dismissible" class="flex shrink-0 items-center gap-2">
        <Button
          v-if="notice.retryable"
          variant="secondary"
          size="sm"
          data-testid="console-state-retry"
          @click="emit('retry')"
        >
          {{ effectiveRetryLabel }}
        </Button>
        <Button
          v-if="dismissible"
          variant="ghost"
          size="sm"
          @click="emit('dismiss')"
        >
          {{ isZh ? '关闭' : 'Dismiss' }}
        </Button>
      </div>
    </div>
  </div>
</template>
