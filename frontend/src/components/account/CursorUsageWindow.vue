<template>
  <div class="min-w-[220px] space-y-1" data-test="cursor-monthly">
    <div v-if="loading" class="h-3 w-24 animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
    <div v-else-if="error" role="status" class="text-[10px] text-red-500">{{ t('admin.accounts.cursor.usageFailed') }}</div>
    <template v-else-if="monthly && (monthly.plan_usage || monthly.resets_at || monthly.limit_status || Object.keys(monthly.models).length)">
      <div v-for="row in planRows" :key="row.label" class="space-y-0.5" :title="row.label">
        <div class="text-[10px] text-gray-500 dark:text-gray-400">{{ row.label }}</div>
        <UsageProgressBar v-if="row.percent != null" :label="t('admin.accounts.cursor.month')" :utilization="row.percent" :resets-at="monthly.resets_at" color="indigo" />
        <div v-else class="flex items-center gap-1 text-[10px] text-gray-400">
          <span class="w-[32px] shrink-0 rounded bg-indigo-100 px-1 text-center font-medium text-indigo-700 dark:bg-indigo-900/40 dark:text-indigo-300">{{ t('admin.accounts.cursor.month') }}</span>
          <span>{{ t('admin.accounts.cursor.quotaUnknown') }}</span>
        </div>
      </div>
      <div class="text-[10px] text-gray-400" :title="periodTitle">
        {{ resetLabel }}
      </div>
      <div v-if="monthly.limit_status?.isInSlowPool" class="text-[10px] text-amber-600 dark:text-amber-400" :title="monthly.limit_status.trayLabel">
        {{ t('admin.accounts.cursor.slowPool') }}
      </div>
      <div v-if="monthly.dashboard_partial" class="text-[10px] text-amber-600">{{ t('admin.accounts.cursor.dashboardPartial') }}</div>
      <details v-if="Object.keys(monthly.models).length" class="text-[10px] text-gray-500 dark:text-gray-400">
        <summary class="cursor-pointer">{{ t('admin.accounts.cursor.rawUsage') }}</summary>
        <div class="space-y-1 pt-1">
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
    <div v-else class="text-xs text-gray-400">{{ t('admin.accounts.cursor.noUsage') }}</div>
    <div class="flex flex-wrap items-center gap-1.5">
      <button type="button" :disabled="loading" class="inline-flex items-center gap-0.5 whitespace-nowrap rounded px-1.5 py-0.5 text-[10px] font-medium leading-4 text-blue-600 transition-colors hover:bg-blue-50 disabled:cursor-not-allowed disabled:opacity-50 dark:text-blue-400 dark:hover:bg-blue-900/30" @click="$emit('refresh')">
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
