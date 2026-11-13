<template>
  <slot v-if="state === 'known'" />
  <div v-else role="status" :aria-busy="state === 'loading'" class="space-y-3 rounded-lg border border-dashed border-[var(--border)] bg-[var(--card)] p-4 text-sm text-[var(--muted-foreground)]">
    <div v-if="state === 'loading'" class="space-y-3 motion-safe:animate-pulse" aria-hidden="true"><div v-for="row in 4" :key="row" class="h-1 rounded bg-[var(--border)]" :style="{ width: `${70 - row * 10}%` }" /></div>
    <span class="inline-flex items-center gap-2 rounded-full border border-[var(--border)] bg-[var(--surface-2)] px-2.5 py-1 text-xs">
      <span class="h-1.5 w-1.5 rounded-full" :class="state === 'error' ? 'bg-red-500' : 'bg-[var(--brand)]'" aria-hidden="true" />
      {{ state === 'unknown' ? '—' : t(state === 'loading' ? 'common.loading' : 'usage.failedToLoad') }}
    </span>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { HistoryGap } from '@/views/trendGaps'

defineProps<{
  state: 'loading' | 'known' | 'unknown' | 'error'
  historyGap?: HistoryGap | null
}>()

const { t } = useI18n()
</script>
