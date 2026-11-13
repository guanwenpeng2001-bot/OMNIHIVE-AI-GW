<template>
  <div class="grid w-full grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
    <!-- Today Revenue -->
    <div class="card metric-card">
      <div class="metric-label">
        <p>{{ t('payment.admin.todayRevenue') }}</p>
        <Icon name="dollar" size="sm" aria-hidden="true" />
      </div>
      <p v-for="[currency, amount] in sortedAmounts(stats.today_amount)" :key="currency" class="metric-value font-mono text-2xl font-bold tabular-nums">
        {{ formatMoney(currency, amount) }}
      </p>
      <p class="metric-note"><span class="font-mono tabular-nums">{{ stats.today_count }}</span> {{ t('payment.admin.orders') }}</p>
    </div>

    <!-- Total Revenue -->
    <div class="card metric-card">
      <div class="metric-label">
        <p>{{ t('payment.admin.totalRevenue') }}</p>
        <Icon name="creditCard" size="sm" aria-hidden="true" />
      </div>
      <p v-for="[currency, amount] in sortedAmounts(stats.total_amount)" :key="currency" class="metric-value font-mono text-2xl font-bold tabular-nums">
        {{ formatMoney(currency, amount) }}
      </p>
      <p class="metric-note"><span class="font-mono tabular-nums">{{ stats.total_count }}</span> {{ t('payment.admin.orders') }}</p>
    </div>

    <!-- Today Orders -->
    <div class="card metric-card">
      <div class="metric-label">
        <p>{{ t('payment.admin.todayOrders') }}</p>
        <Icon name="chart" size="sm" aria-hidden="true" />
      </div>
      <p class="metric-value font-mono text-2xl font-bold tabular-nums">{{ stats.today_count }}</p>
      <p class="metric-note">{{ t('common.today') }} · {{ t('payment.admin.orders') }}</p>
    </div>

    <!-- Average Amount -->
    <div class="card metric-card">
      <div class="metric-label">
        <p>{{ t('payment.admin.avgAmount') }}</p>
        <Icon name="chart" size="sm" aria-hidden="true" />
      </div>
      <p v-for="[currency, amount] in sortedAmounts(stats.avg_amount)" :key="currency" class="metric-value font-mono text-2xl font-bold tabular-nums">
        {{ formatMoney(currency, amount) }}
      </p>
      <p class="metric-note">{{ t('payment.admin.avgAmount') }} / {{ t('payment.admin.orders') }}</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { CurrencyAmounts, DashboardStats } from '@/types/payment'

const { t } = useI18n()

defineProps<{
  stats: DashboardStats
}>()

function sortedAmounts(amounts: CurrencyAmounts): [string, number][] {
  return Object.entries(amounts).sort(([left], [right]) => left.localeCompare(right))
}

function formatMoney(currency: string, amount: number): string {
  return new Intl.NumberFormat(undefined, { style: 'currency', currency }).format(amount)
}
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
