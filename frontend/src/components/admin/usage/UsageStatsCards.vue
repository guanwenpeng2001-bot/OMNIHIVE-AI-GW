<template>
  <div v-if="loading || error || historyGap || !stats" data-testid="usage-stats" :aria-busy="loading" class="grid w-full grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
    <div v-for="label in ['usage.totalRequests', 'usage.totalTokens', 'usage.totalCost', 'usage.avgDuration']" :key="label" class="card metric-card">
      <div class="metric-label">
        <p>{{ t(label) }}</p>
        <span class="h-1.5 w-1.5 shrink-0 rounded-full" :class="error ? 'bg-[var(--bad)]' : 'bg-[var(--muted-foreground)]'" aria-hidden="true"></span>
      </div>
      <p class="metric-value font-mono text-2xl font-bold tabular-nums" role="status">{{ loading ? t('common.loading') : historyGap ? '—' : t('usage.failedToLoad') }}</p>
      <p class="metric-note">{{ t('usage.inSelectedRange') }}</p>
    </div>
  </div>
  <div v-else data-testid="usage-stats" :aria-busy="loading" class="grid w-full grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
    <div class="card metric-card">
      <div class="metric-label">
        <p>{{ t('usage.totalRequests') }}</p>
        <Icon name="document" size="sm" aria-hidden="true" />
      </div>
      <p class="metric-value font-mono text-2xl font-bold tabular-nums">{{ stats?.total_requests?.toLocaleString() || '0' }}</p>
      <p class="metric-note">{{ t('usage.inSelectedRange') }}</p>
    </div>
    <div class="card metric-card">
      <div class="metric-label">
        <p>{{ t('usage.totalTokens') }}</p>
        <Icon name="chart" size="sm" aria-hidden="true" />
      </div>
        <p class="metric-value font-mono text-2xl font-bold tabular-nums">{{ formatTokens(stats?.total_tokens || 0) }}</p>
        <p class="metric-note flex flex-wrap items-center gap-x-1 font-mono tabular-nums">
          <span>{{ t('usage.in') }}: {{ formatTokens(stats?.total_input_tokens || 0) }}</span>
          <span>/</span>
          <span>{{ t('usage.out') }}: {{ formatTokens(stats?.total_output_tokens || 0) }}</span>
          <span>/</span>
          <span class="group relative inline-flex cursor-help items-center gap-0.5" tabindex="0">
            <span>{{ cacheLabel() }}: {{ formatTokens(stats?.total_cache_tokens || 0) }}</span>
            <svg
              class="h-3.5 w-3.5 text-[var(--muted-foreground)]"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                stroke-width="2"
                d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
              />
            </svg>
            <span
              class="pointer-events-none absolute left-1/2 top-full z-30 mt-2 hidden w-56 -translate-x-1/2 rounded-lg border border-[var(--border)] bg-[var(--card)] p-3 text-left text-xs text-[var(--foreground)] shadow-lg group-hover:block group-focus:block"
            >
              <span class="mb-2 block font-medium text-[var(--foreground)]">
                {{ cacheDetailLabel() }}
              </span>
              <span class="flex items-center justify-between gap-3">
                <span>{{ t('usage.cacheCreationTokensLabel') }}</span>
                <span class="font-mono tabular-nums">
                  {{ formatTokens(stats?.total_cache_creation_tokens || 0) }}
                </span>
              </span>
              <span class="mt-1 flex items-center justify-between gap-3">
                <span>{{ t('usage.cacheReadTokensLabel') }}</span>
                <span class="font-mono tabular-nums">
                  {{ formatTokens(stats?.total_cache_read_tokens || 0) }}
                </span>
              </span>
            </span>
          </span>
        </p>
    </div>
    <div class="card metric-card">
      <div class="metric-label">
        <p>{{ t('usage.totalCost') }} · {{ t('usage.userBilled') }} · USD</p>
        <Icon name="dollar" size="sm" class="shrink-0" aria-hidden="true" />
      </div>
        <p class="metric-value text-xl">
          <span class="font-mono text-2xl font-bold tabular-nums">{{ formatLedgerAmount(stats?.total_actual_cost) }}</span>
        </p>
      <div class="metric-note">
        <p>{{ t('usage.inSelectedRange') }}</p>
        <p class="mt-1">
          <template v-if="showAccountCost && totalAccountCost != null">
            <span>{{ t('usage.accountCost') }} <span class="font-mono tabular-nums">USD {{ formatLedgerAmount(totalAccountCost) }}</span></span>
            <span> · </span>
          </template>
          <span>
            {{ t('usage.standardCost') }}
            <span class="font-mono tabular-nums" :class="{ 'line-through': strikeStandardCost }">USD {{ formatLedgerAmount(stats?.total_cost) }}</span>
          </span>
        </p>
      </div>
    </div>
    <div class="card metric-card">
      <div class="metric-label">
        <p>{{ t('usage.avgDuration') }}</p>
        <Icon name="clock" size="sm" aria-hidden="true" />
      </div>
      <p class="metric-value font-mono text-2xl font-bold tabular-nums">{{ formatDuration(stats?.average_duration_ms || 0) }}</p>
      <p class="metric-note">{{ t('usage.inSelectedRange') }}</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { formatLedgerAmount } from '@/utils/money'
import { useI18n } from 'vue-i18n'
import type { AdminUsageStatsResponse } from '@/api/admin/usage'
import type { UsageStatsResponse } from '@/types'
import Icon from '@/components/icons/Icon.vue'

const props = withDefaults(defineProps<{
  stats: (AdminUsageStatsResponse | UsageStatsResponse) | null
  loading?: boolean
  error?: boolean
  historyGap?: { reason: string } | null
  showAccountCost?: boolean
  strikeStandardCost?: boolean
}>(), {
  showAccountCost: true,
  strikeStandardCost: false,
})

const { t } = useI18n()

const totalAccountCost = computed(() => {
  const stats = props.stats as (AdminUsageStatsResponse & { total_account_cost?: number }) | null
  return stats?.total_account_cost ?? null
})
const showAccountCost = computed(() => props.showAccountCost)
const strikeStandardCost = computed(() => props.strikeStandardCost)

const formatDuration = (ms: number) =>
  ms < 1000 ? `${ms.toFixed(0)}ms` : `${(ms / 1000).toFixed(2)}s`

const formatTokens = (value: number) => {
  if (value >= 1e9) return (value / 1e9).toFixed(2) + 'B'
  if (value >= 1e6) return (value / 1e6).toFixed(2) + 'M'
  if (value >= 1e3) return (value / 1e3).toFixed(2) + 'K'
  return value.toLocaleString()
}

const cacheLabel = () => t('usage.cacheTotal')
const cacheDetailLabel = () => t('usage.cacheBreakdown')
</script>

<style scoped>
.metric-card {
  @apply flex min-w-0 flex-col gap-3 p-4;
  border-radius: var(--radius-sm);
  border-color: var(--border);
  background: var(--card);
  color: var(--foreground);
  box-shadow: none;
}
.metric-label {
  @apply flex items-center justify-between gap-2 text-xs font-medium;
  color: var(--muted-foreground);
}
.metric-value {
  overflow-wrap: anywhere;
}
.metric-note {
  @apply mt-auto text-xs;
  color: var(--muted-foreground);
}
</style>
