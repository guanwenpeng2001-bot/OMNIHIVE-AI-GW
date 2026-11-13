<template>
  <div class="min-w-[155px] max-w-[210px] space-y-1 font-mono tabular-nums" data-test="cursor-monthly">
    <div v-if="loading" class="h-3 w-24 animate-pulse rounded bg-[var(--surface-2)] motion-reduce:animate-none" />
    <div v-else-if="error" role="status" class="rounded border border-[color-mix(in_srgb,var(--bad)_30%,transparent)] bg-[color-mix(in_srgb,var(--bad)_10%,transparent)] px-1.5 py-0.5 text-[10px] text-[var(--bad-text)]">{{ t('admin.accounts.cursor.usageFailed') }}</div>
    <template v-else-if="monthly && (monthly.plan_usage || monthly.resets_at || monthly.limit_status || Object.keys(monthly.models).length)">
      <div v-for="row in planRows" :key="row.label" class="space-y-0.5" :title="row.label">
        <div class="text-[10px] text-[var(--muted-foreground)]">{{ row.label }}</div>
        <UsageProgressBar v-if="row.percent != null" :label="t('admin.accounts.cursor.month')" :utilization="row.percent" :resets-at="monthly.resets_at" color="indigo" />
        <div v-else class="flex items-center gap-1 text-[10px] text-[var(--muted-foreground)]">
          <span class="w-[32px] shrink-0 rounded border border-[var(--border)] bg-[var(--surface-2)] px-1 py-0.5 text-center font-semibold text-[var(--foreground)]">{{ t('admin.accounts.cursor.month') }}</span>
          <span>{{ t('admin.accounts.cursor.quotaUnknown') }}</span>
        </div>
      </div>
      <div class="max-w-full truncate text-[10px] text-[var(--muted-foreground)]" :title="periodTitle">
        {{ resetLabel }}
      </div>
      <div v-if="monthly.limit_status?.isInSlowPool" class="inline-flex rounded border border-[color-mix(in_srgb,var(--warn)_30%,transparent)] bg-[color-mix(in_srgb,var(--warn)_10%,transparent)] px-1.5 py-0.5 text-[10px] text-[var(--warn-text)]" :title="monthly.limit_status.trayLabel">
        {{ t('admin.accounts.cursor.slowPool') }}
      </div>
      <div v-if="monthly.dashboard_partial" class="text-[10px] text-[var(--warn-text)]">{{ t('admin.accounts.cursor.dashboardPartial') }}</div>
      <details v-if="Object.keys(monthly.models).length" class="rounded-md border border-[var(--border)] bg-[color-mix(in_srgb,var(--surface-2)_70%,transparent)] text-[10px] text-[var(--muted-foreground)]">
        <summary class="cursor-pointer rounded-md px-2 py-1 transition-colors hover:text-[var(--brand-text)] focus-visible:outline focus-visible:outline-2 focus-visible:outline-[var(--brand)]">{{ t('admin.accounts.cursor.rawUsage') }}</summary>
        <div class="max-h-48 space-y-1 overflow-auto border-t border-[var(--border)] p-2">
          <div v-for="(usage, model) in monthly.models" :key="model" class="space-y-0.5">
            <div class="font-medium">{{ model }}</div>
            <div>{{ t('admin.accounts.cursor.totalRequests') }}: {{ usage.numRequestsTotal }}</div>
            <div>{{ t('admin.accounts.cursor.requests') }}: {{ usage.numRequests }} / {{ usage.maxRequestUsage ?? t('admin.accounts.cursor.quotaUnknown') }}</div>
            <UsageProgressBar v-if="usage.maxRequestUsage != null && usage.maxRequestUsage > 0" :label="t('admin.accounts.cursor.requests')" :utilization="usage.numRequests / usage.maxRequestUsage * 100" :resets-at="monthly.resets_at" color="indigo" />
            <div>{{ t('admin.accounts.cursor.tokens') }}: {{ usage.numTokens }} / {{ usage.maxTokenUsage ?? t('admin.accounts.cursor.quotaUnknown') }}</div>
            <UsageProgressBar v-if="usage.maxTokenUsage != null && usage.maxTokenUsage > 0" :label="t('admin.accounts.cursor.tokens')" :utilization="usage.numTokens / usage.maxTokenUsage * 100" :resets-at="monthly.resets_at" color="emerald" />
          </div>
        </div>
      </details>
    </template>
    <div v-else class="text-[10px] text-[var(--muted-foreground)]">{{ t('admin.accounts.cursor.noUsage') }}</div>
    <div class="flex flex-wrap items-center gap-1.5">
      <button type="button" :disabled="loading" class="inline-flex items-center gap-1 whitespace-nowrap rounded-md border border-[var(--border)] bg-[var(--card)] px-2 py-0.5 font-sans text-[10px] font-medium text-[var(--foreground)] shadow-sm transition-colors hover:border-[var(--brand)] hover:text-[var(--brand-text)] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--brand)] disabled:cursor-not-allowed disabled:opacity-50" @click="$emit('refresh')">
        {{ t('admin.accounts.usageWindow.activeQuery') }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useIntervalFn } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import type { CursorMonthlyUsage } from '@/types'
import UsageProgressBar from './UsageProgressBar.vue'

const props = defineProps<{ monthly?: CursorMonthlyUsage; loading?: boolean; error?: string | null }>()
defineEmits<{ refresh: [] }>()
const { t, locale } = useI18n()
const now = ref(Date.now())
useIntervalFn(() => { now.value = Date.now() }, 60_000)
const validPercent = (value?: number) => typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : null
const planRows = computed(() => {
  const plan = props.monthly?.plan_usage
  if (plan && (plan.autoPercentUsed != null || plan.apiPercentUsed != null)) {
    return [
      { label: t('admin.accounts.cursor.autoModels'), percent: validPercent(plan.autoPercentUsed) },
      { label: t('admin.accounts.cursor.apiModels'), percent: validPercent(plan.apiPercentUsed) }
    ]
  }
  return [{ label: t('admin.accounts.cursor.totalUsage'), percent: validPercent(plan?.totalPercentUsed) }]
})
const formatDate = (value: string) => new Date(value).toLocaleDateString(locale.value, { timeZone: 'UTC' })
const periodTitle = computed(() => props.monthly?.start_of_month && props.monthly.resets_at ? `${formatDate(props.monthly.start_of_month)} → ${formatDate(props.monthly.resets_at)}` : '')
const resetLabel = computed(() => {
  const reset = props.monthly?.resets_at
  if (!reset || !Number.isFinite(Date.parse(reset))) return t('admin.accounts.cursor.periodUnknown')
  if (Date.parse(reset) <= now.value) return t('usage.resetPending')
  return t('admin.accounts.cursor.resetDate', { date: formatDate(reset), days: Math.ceil((Date.parse(reset) - now.value) / 86_400_000) })
})
</script>
