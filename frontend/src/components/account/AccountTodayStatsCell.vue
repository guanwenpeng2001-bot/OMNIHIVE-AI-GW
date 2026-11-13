<template>
  <div class="min-w-0 max-w-[260px]">
    <!-- Loading state -->
    <div v-if="props.loading && !props.stats" class="space-y-0.5">
      <div class="h-3 w-12 animate-pulse rounded bg-[var(--surface-2)] motion-reduce:animate-none"></div>
      <div class="h-3 w-16 animate-pulse rounded bg-[var(--surface-2)] motion-reduce:animate-none"></div>
      <div class="h-3 w-10 animate-pulse rounded bg-[var(--surface-2)] motion-reduce:animate-none"></div>
    </div>

    <!-- Error state -->
    <div v-else-if="props.error && !props.stats" class="max-w-[230px] truncate rounded border border-[color-mix(in_srgb,var(--bad)_30%,transparent)] bg-[color-mix(in_srgb,var(--bad)_10%,transparent)] px-1.5 py-0.5 text-[10px] text-[var(--bad-text)]" :title="props.error">
      {{ props.error }}
    </div>

    <!-- Stats data -->
    <div v-else-if="props.stats" class="inline-grid grid-cols-2 gap-x-3 gap-y-1 rounded-md border border-[var(--border)] bg-[color-mix(in_srgb,var(--surface-2)_70%,transparent)] px-2 py-1 font-mono text-[10px] tabular-nums">
      <!-- Requests -->
      <div class="flex items-center gap-1">
        <span class="text-[var(--muted-foreground)]"
          >{{ t('admin.accounts.stats.requests') }}:</span
        >
        <span class="font-semibold text-[var(--foreground)]">{{
          formatNumber(props.stats.requests)
        }}</span>
      </div>
      <!-- Tokens -->
      <div class="flex items-center gap-1">
        <span class="text-[var(--muted-foreground)]"
          >{{ t('admin.accounts.stats.tokens') }}:</span
        >
        <span class="font-semibold text-[var(--foreground)]">{{
          formatTokens(props.stats.tokens)
        }}</span>
      </div>
      <!-- Cost (Account) -->
      <div class="flex items-center gap-1">
        <span class="text-[var(--muted-foreground)]">{{ t('usage.accountBilled') }}:</span>
        <span class="font-semibold text-[var(--foreground)]">{{
          formatAccountStatsCurrency(props.stats.cost)
        }}</span>
      </div>
      <!-- Cost (User/API Key) -->
      <div v-if="props.stats.user_cost != null" class="flex items-center gap-1">
        <span class="text-[var(--muted-foreground)]">{{ t('usage.userBilled') }}:</span>
        <span class="font-semibold text-[var(--foreground)]">{{
          formatAccountStatsCurrency(props.stats.user_cost)
        }}</span>
      </div>
    </div>

    <!-- No data -->
    <div v-else class="text-[11px] text-[var(--muted-foreground)]">-</div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { WindowStats } from '@/types'
import { formatNumber, formatAccountStatsCurrency } from '@/utils/format'

const props = withDefaults(
  defineProps<{
    stats?: WindowStats | null
    loading?: boolean
    error?: string | null
  }>(),
  {
    stats: null,
    loading: false,
    error: null
  }
)

const { t } = useI18n()

// Format large token numbers (e.g., 1234567 -> 1.23M)
const formatTokens = (tokens: number): string => {
  if (tokens >= 1000000) {
    return `${(tokens / 1000000).toFixed(2)}M`
  } else if (tokens >= 1000) {
    return `${(tokens / 1000).toFixed(1)}K`
  }
  return tokens.toString()
}
</script>
