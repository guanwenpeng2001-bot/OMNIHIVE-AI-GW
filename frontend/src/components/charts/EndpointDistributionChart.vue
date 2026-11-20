<template>
  <div class="card min-w-0 border border-[var(--border)] bg-[var(--card)] p-4 tabular-nums">
    <DistributionCaption v-if="!loading" :values="displayEndpointStats.map(item => metric === 'actual_cost' ? item.actual_cost : item.total_tokens)" :metric="metric" />
    <div class="mb-4 flex items-center justify-between gap-3">
      <h3 class="text-sm font-semibold text-[var(--foreground)]">
        {{ title || t('usage.endpointDistribution') }}
      </h3>
      <div class="flex flex-wrap items-center justify-end gap-2">
        <div
          v-if="showSourceToggle"
          class="inline-flex rounded-lg border border-[var(--border)] bg-[var(--surface-2)] p-0.5"
        >
          <button
            type="button"
            class="rounded-md px-2.5 py-1 text-xs font-medium transition-colors"
            :class="source === 'inbound'
              ? 'bg-[var(--card)] text-[var(--foreground)] shadow-sm  '
              : 'text-[var(--muted-foreground)] hover:text-[var(--foreground)]  '"
            @click="emit('update:source', 'inbound')"
          >
            {{ t('usage.inbound') }}
          </button>
          <button
            type="button"
            class="rounded-md px-2.5 py-1 text-xs font-medium transition-colors"
            :class="source === 'upstream'
              ? 'bg-[var(--card)] text-[var(--foreground)] shadow-sm  '
              : 'text-[var(--muted-foreground)] hover:text-[var(--foreground)]  '"
            @click="emit('update:source', 'upstream')"
          >
            {{ t('usage.upstream') }}
          </button>
          <button
            type="button"
            class="rounded-md px-2.5 py-1 text-xs font-medium transition-colors"
            :class="source === 'path'
              ? 'bg-[var(--card)] text-[var(--foreground)] shadow-sm  '
              : 'text-[var(--muted-foreground)] hover:text-[var(--foreground)]  '"
            @click="emit('update:source', 'path')"
          >
            {{ t('usage.path') }}
          </button>
        </div>

        <div
          v-if="showMetricToggle"
          class="inline-flex rounded-lg border border-[var(--border)] bg-[var(--surface-2)] p-0.5"
        >
          <button
            type="button"
            class="rounded-md px-2.5 py-1 text-xs font-medium transition-colors"
            :class="metric === 'tokens'
              ? 'bg-[var(--card)] text-[var(--foreground)] shadow-sm  '
              : 'text-[var(--muted-foreground)] hover:text-[var(--foreground)]  '"
            @click="emit('update:metric', 'tokens')"
          >
            {{ t('admin.dashboard.metricTokens') }}
          </button>
          <button
            type="button"
            class="rounded-md px-2.5 py-1 text-xs font-medium transition-colors"
            :class="metric === 'actual_cost'
              ? 'bg-[var(--card)] text-[var(--foreground)] shadow-sm  '
              : 'text-[var(--muted-foreground)] hover:text-[var(--foreground)]  '"
            @click="emit('update:metric', 'actual_cost')"
          >
            {{ t('admin.dashboard.metricActualCost') }}
          </button>
        </div>
      </div>
    </div>
    <div v-if="loading" class="flex h-48 items-center justify-center">
      <LoadingSpinner />
    </div>
    <div v-else-if="displayEndpointStats.length > 0 && chartData" class="flex flex-col items-center gap-4 sm:flex-row sm:gap-6">
      <div class="h-48 w-48 shrink-0">
        <Doughnut :data="chartData" :options="doughnutOptions" />
      </div>
      <div class="w-full min-w-0 flex-1 overflow-x-auto">
        <table class="w-full text-xs">
          <thead>
            <tr class="text-[var(--muted-foreground)]">
              <th class="pb-2 text-left">{{ t('usage.endpoint') }}</th>
              <th class="pb-2 text-right">{{ t('admin.dashboard.requests') }}</th>
              <th class="pb-2 text-right">{{ tokensHeaderLabel }}</th>
              <th class="pb-2 text-right">{{ t('admin.dashboard.actual') }}</th>
              <th class="pb-2 text-right">{{ t('admin.dashboard.standard') }}</th>
            </tr>
          </thead>
          <tbody>
            <template v-for="(item, index) in visibleEndpointStats" :key="item.endpoint">
              <tr
                class="border-t border-[var(--border)] transition-colors"
                :class="enableBreakdown ? 'cursor-pointer hover:bg-[var(--surface-2)] ' : ''"
                @click="enableBreakdown && toggleBreakdown(item.endpoint)"
              >
                <td class="max-w-[180px] truncate py-1.5 font-medium" :class="enableBreakdown ? 'text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300' : 'text-[var(--foreground)] '" :title="item.endpoint">
                  <span class="inline-flex items-center gap-1">
                    <span class="h-1.5 w-1.5 shrink-0 rounded-full" :style="{ backgroundColor: chartColors[index % chartColors.length] }" aria-hidden="true" />
                    <svg v-if="enableBreakdown && expandedKey === item.endpoint" class="h-3 w-3 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"/></svg>
                    <svg v-else-if="enableBreakdown" class="h-3 w-3 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7"/></svg>
                    {{ item.endpoint }}
                  </span>
                </td>
                <td class="py-1.5 text-right text-[var(--muted-foreground)]">
                  {{ formatNumber(item.requests) }}
                </td>
                <td class="py-1.5 text-right text-[var(--muted-foreground)]">
                  {{ formatTokens(item.total_tokens) }}
                </td>
                <td class="py-1.5 text-right text-green-600 dark:text-green-400">
                  USD {{ formatCost(item.actual_cost) }}
                </td>
                <td class="py-1.5 text-right text-[var(--muted-foreground)]">
                  USD {{ formatCost(item.cost) }}
                </td>
              </tr>
              <tr v-if="expandedKey === item.endpoint">
                <td colspan="5" class="p-0">
                  <UserBreakdownSubTable
                    :items="breakdownItems"
                    :loading="breakdownLoading"
                  />
                </td>
              </tr>
            </template>
          </tbody>
        </table>
        <div v-if="displayEndpointStats.length > 10" class="mt-2 flex justify-end">
          <button
            type="button"
            class="text-xs font-medium text-[var(--brand-text,#d97706)] hover:underline"
            @click="showAllEndpoints = !showAllEndpoints"
          >
            {{ showAllEndpoints ? (isZh ? '收起' : 'Show less') : (isZh ? `展开更多 (${displayEndpointStats.length - 10})` : `Show more (${displayEndpointStats.length - 10})`) }}
          </button>
        </div>
      </div>
    </div>
    <div v-else class="flex h-48 flex-col justify-center gap-4 text-sm text-[var(--muted-foreground)]" role="status">
      <div class="w-full space-y-4" aria-hidden="true"><div v-for="row in 4" :key="row" class="flex items-center gap-4 border-b border-dashed border-[var(--border)] pb-2"><span class="h-1 rounded bg-[var(--border)]" :style="{ width: `${64 - row * 9}%` }" /><span class="ml-auto h-3 w-10 rounded-full border border-[var(--border)]" /></div></div>
      <span class="self-start rounded-full border border-[var(--border)] px-2.5 py-1 text-xs">{{ t('admin.dashboard.noDataAvailable') }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import DistributionCaption from './DistributionCaption.vue'
import { formatLedgerAmount as formatCost } from '@/utils/money'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Chart as ChartJS, ArcElement, Tooltip, Legend } from 'chart.js'
import { Doughnut } from 'vue-chartjs'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import UserBreakdownSubTable from './UserBreakdownSubTable.vue'
import type { EndpointStat, UserBreakdownItem } from '@/types'
import { getUserBreakdown } from '@/api/admin/dashboard'

ChartJS.register(ArcElement, Tooltip, Legend)

const i18n = useI18n()
const { t } = i18n
const isZh = computed(() => {
  const rawLocale = typeof i18n.locale === 'string' ? i18n.locale : i18n.locale?.value
  if (rawLocale) return String(rawLocale).toLowerCase().startsWith('zh')
  const probe = t('admin.dashboard.requests')
  return Boolean(probe && probe !== 'admin.dashboard.requests' && /[\u4e00-\u9fa5]/.test(probe))
})
const tokensHeaderLabel = computed(() => (isZh.value ? '总 Token' : t('admin.dashboard.tokens')))
const showAllEndpoints = ref(false)

type DistributionMetric = 'tokens' | 'actual_cost'
type EndpointSource = 'inbound' | 'upstream' | 'path'

const props = withDefaults(
  defineProps<{
    endpointStats: EndpointStat[]
    upstreamEndpointStats?: EndpointStat[]
    endpointPathStats?: EndpointStat[]
    loading?: boolean
    title?: string
    metric?: DistributionMetric
    source?: EndpointSource
    showMetricToggle?: boolean
    showSourceToggle?: boolean
    enableBreakdown?: boolean
    startDate?: string
    endDate?: string
    filters?: Record<string, any>
  }>(),
  {
    upstreamEndpointStats: () => [],
    endpointPathStats: () => [],
    loading: false,
    title: '',
    metric: 'tokens',
    source: 'inbound',
    showMetricToggle: false,
    showSourceToggle: false,
    enableBreakdown: true
  }
)

const emit = defineEmits<{
  'update:metric': [value: DistributionMetric]
  'update:source': [value: EndpointSource]
}>()

const expandedKey = ref<string | null>(null)
const breakdownItems = ref<UserBreakdownItem[]>([])
const breakdownLoading = ref(false)

const toggleBreakdown = async (endpoint: string) => {
  if (expandedKey.value === endpoint) {
    expandedKey.value = null
    return
  }
  expandedKey.value = endpoint
  breakdownLoading.value = true
  breakdownItems.value = []
  try {
    const res = await getUserBreakdown({
      ...props.filters,
      start_date: props.startDate,
      end_date: props.endDate,
      endpoint,
      endpoint_type: props.source,
    })
    breakdownItems.value = res.users || []
  } catch {
    breakdownItems.value = []
  } finally {
    breakdownLoading.value = false
  }
}

const chartColors = ['#d97706', '#059669', '#6366f1', '#fbbf24', '#34d399', '#a5b4fc', '#b45309', '#047857', '#4338ca', '#f59e0b', '#10b981', '#818cf8']

const displayEndpointStats = computed(() => {
  const sourceStats = props.source === 'upstream'
    ? props.upstreamEndpointStats
    : props.source === 'path'
      ? props.endpointPathStats
      : props.endpointStats
  if (!sourceStats?.length) return []

  const metricKey = props.metric === 'actual_cost' ? 'actual_cost' : 'total_tokens'
  return [...sourceStats].sort((a, b) => b[metricKey] - a[metricKey])
})
const visibleEndpointStats = computed(() =>
  showAllEndpoints.value ? displayEndpointStats.value : displayEndpointStats.value.slice(0, 10)
)

const chartData = computed(() => {
  if (!displayEndpointStats.value?.length) return null

  return {
    labels: displayEndpointStats.value.map((item) => item.endpoint),
    datasets: [
      {
        data: displayEndpointStats.value.map((item) =>
          props.metric === 'actual_cost' ? item.actual_cost : item.total_tokens
        ),
        backgroundColor: displayEndpointStats.value.map((_, index) => chartColors[index % chartColors.length]),
        borderWidth: 0
      }
    ]
  }
})

const doughnutOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  cutout: '72%',
  animation: false as const,
  plugins: {
    legend: {
      display: false
    },
    tooltip: {
      callbacks: {
        label: (context: any) => {
          const value = context.raw as number
          const total = context.dataset.data.reduce((a: number, b: number) => a + b, 0)
          const percentage = total > 0 ? ((value / total) * 100).toFixed(1) : '0.0'
          const formattedValue = props.metric === 'actual_cost'
            ? `USD ${formatCost(value)}`
            : formatTokens(value)
          return `${context.label}: ${formattedValue} (${percentage}%)`
        }
      }
    }
  }
}))

const formatTokens = (value: number): string => {
  if (value >= 1_000_000_000) {
    return `${(value / 1_000_000_000).toFixed(2)}B`
  } else if (value >= 1_000_000) {
    return `${(value / 1_000_000).toFixed(2)}M`
  } else if (value >= 1_000) {
    return `${(value / 1_000).toFixed(2)}K`
  }
  return value.toLocaleString()
}

const formatNumber = (value: number): string => {
  return value.toLocaleString()
}


</script>
