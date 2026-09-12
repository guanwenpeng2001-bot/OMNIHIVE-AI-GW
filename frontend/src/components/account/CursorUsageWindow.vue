<template>
  <div class="space-y-1" data-test="cursor-monthly">
    <div class="text-[10px] font-medium text-gray-500">{{ t('admin.accounts.cursor.monthlyUsage') }}</div>
    <div v-if="loading" class="h-3 w-24 animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
    <div v-else-if="error" role="status" class="text-xs text-red-500">{{ t('admin.accounts.cursor.usageFailed') }}</div>
    <template v-else-if="monthly && Object.keys(monthly.models).length">
      <div class="text-[9px] text-gray-500">
        <template v-if="monthly.start_of_month && monthly.resets_at">{{ formatDate(monthly.start_of_month) }} → {{ formatDate(monthly.resets_at) }}</template>
        <template v-else>{{ t('admin.accounts.cursor.periodUnknown') }}</template>
      </div>
      <div v-for="(usage, model) in monthly.models" :key="model" class="space-y-0.5 text-[9px] text-gray-500">
        <div class="font-medium">{{ model }}</div>
        <div>{{ t('admin.accounts.cursor.totalRequests') }}: {{ usage.numRequestsTotal }}</div>
        <div>{{ t('admin.accounts.cursor.requests') }}: {{ usage.numRequests }} / {{ usage.maxRequestUsage ?? t('admin.accounts.cursor.unlimited') }}</div>
        <UsageProgressBar v-if="usage.maxRequestUsage != null" :label="t('admin.accounts.cursor.requests')" :utilization="percent(usage.numRequests, usage.maxRequestUsage)" :resets-at="monthly.resets_at" color="indigo" />
        <div>{{ t('admin.accounts.cursor.tokens') }}: {{ usage.numTokens }} / {{ usage.maxTokenUsage ?? t('admin.accounts.cursor.unlimited') }}</div>
        <UsageProgressBar v-if="usage.maxTokenUsage != null" :label="t('admin.accounts.cursor.tokens')" :utilization="percent(usage.numTokens, usage.maxTokenUsage)" :resets-at="monthly.resets_at" color="emerald" />
      </div>
    </template>
    <div v-else class="text-xs text-gray-400">{{ t('admin.accounts.cursor.noUsage') }}</div>
    <button type="button" :disabled="loading" class="inline-flex items-center rounded px-1.5 py-0.5 text-[9px] font-medium text-blue-600 transition-colors hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-900/30" @click="$emit('refresh')">
      {{ t('admin.accounts.usageWindow.activeQuery') }}
    </button>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { CursorMonthlyUsage } from '@/types'
import UsageProgressBar from './UsageProgressBar.vue'

defineProps<{ monthly?: CursorMonthlyUsage; loading?: boolean; error?: string | null }>()
defineEmits<{ refresh: [] }>()
const { t, locale } = useI18n()
const formatDate = (value: string) => new Date(value).toLocaleDateString(locale.value, { timeZone: 'UTC' })
const percent = (used: number, limit: number) => limit > 0 ? used / limit * 100 : 100
</script>
