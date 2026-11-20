<template>
  <div class="card min-w-0 border border-[var(--border)] bg-[var(--card)] p-4 tabular-nums">
    <DistributionCaption v-if="activeView === 'spending_ranking' ? !rankingLoading && !rankingUnknown && !rankingError : !loading && !modelUnknown && !modelError" :values="activeView === 'spending_ranking' ? rankingDisplayItems.map(item => item.actual_cost) : displayModelStats.map(item => metric === 'actual_cost' ? item.actual_cost : item.total_tokens)" :metric="activeView === 'spending_ranking' ? 'actual_cost' : metric" :colors="activeView === 'spending_ranking' ? rankingChartData?.datasets[0]?.backgroundColor : undefined" />
    <div class="mb-4 flex items-center justify-between gap-3">
      <h3 class="text-sm font-semibold text-[var(--foreground)]">
        {{ !enableRankingView || activeView === 'model_distribution'
          ? t('admin.dashboard.modelDistribution')
          : t('admin.dashboard.spendingRankingTitle') }}
      </h3>
      <div class="flex flex-wrap items-center justify-end gap-2">
        <div
          v-if="showSourceToggle"
          class="inline-flex rounded-lg border border-[var(--border)] bg-[var(--surface-2)] p-0.5"
        >
          <button
            type="button"
            class="rounded-md px-2.5 py-1 text-xs font-medium transition-colors"
            :class="source === 'requested'
              ? 'bg-[var(--card)] text-[var(--foreground)] shadow-sm  '
              : 'text-[var(--muted-foreground)] hover:text-[var(--foreground)]  '"
            @click="emit('update:source', 'requested')"
          >
            {{ t('usage.requestedModel') }}
          </button>
          <button
            type="button"
            class="rounded-md px-2.5 py-1 text-xs font-medium transition-colors"
            :class="source === 'upstream'
              ? 'bg-[var(--card)] text-[var(--foreground)] shadow-sm  '
              : 'text-[var(--muted-foreground)] hover:text-[var(--foreground)]  '"
            @click="emit('update:source', 'upstream')"
          >
            {{ t('usage.upstreamModel') }}
          </button>
          <button
            type="button"
            class="rounded-md px-2.5 py-1 text-xs font-medium transition-colors"
            :class="source === 'mapping'
              ? 'bg-[var(--card)] text-[var(--foreground)] shadow-sm  '
              : 'text-[var(--muted-foreground)] hover:text-[var(--foreground)]  '"
            @click="emit('update:source', 'mapping')"
          >
            {{ t('usage.mapping') }}
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
        <div v-if="enableRankingView" class="inline-flex rounded-lg bg-[var(--surface-2)] p-1">
          <button
            type="button"
            class="rounded-md px-2.5 py-1 text-xs font-medium transition-colors"
            :class="
              activeView === 'model_distribution'
                ? 'bg-[var(--card)] text-[var(--foreground)] shadow-sm  '
                : 'text-[var(--muted-foreground)] hover:text-[var(--foreground)]  '
            "
            @click="activeView = 'model_distribution'"
          >
            {{ t('admin.dashboard.viewModelDistribution') }}
          </button>
          <button
            type="button"
            class="rounded-md px-2.5 py-1 text-xs font-medium transition-colors"
            :class="
              activeView === 'spending_ranking'
                ? 'bg-[var(--card)] text-[var(--foreground)] shadow-sm  '
                : 'text-[var(--muted-foreground)] hover:text-[var(--foreground)]  '
            "
            @click="activeView = 'spending_ranking'"
          >
            {{ t('admin.dashboard.viewSpendingRanking') }}
          </button>
        </div>
      </div>
    </div>

    <div v-if="activeView === 'model_distribution' && loading" class="flex h-48 items-center justify-center">
      <LoadingSpinner />
    </div>
    <div v-else-if="activeView === 'model_distribution' && (modelUnknown || modelError)" role="status" class="flex h-48 items-center justify-center text-sm text-[var(--muted-foreground)]">
      {{ modelUnknown ? '—' : t('admin.dashboard.failedToLoad') }}
    </div>
    <div
      v-else-if="activeView === 'model_distribution' && displayModelStats.length > 0 && chartData"
      class="flex flex-col items-center gap-4 sm:flex-row sm:gap-6"
    >
      <div class="h-48 w-48 shrink-0">
        <Doughnut :data="chartData" :options="doughnutOptions" />
      </div>
      <div class="w-full min-w-0 flex-1 overflow-x-auto">
        <table class="w-full text-xs">
          <thead>
            <tr class="text-[var(--muted-foreground)]">
              <th class="pb-2 text-left">{{ t('admin.dashboard.model') }}</th>
              <th class="pb-2 text-right">{{ t('admin.dashboard.requests') }}</th>
              <th class="pb-2 text-right">{{ tokensHeaderLabel }}</th>
              <th class="pb-2 text-right">{{ t('admin.dashboard.actual') }}</th>
              <th v-if="showAccountCost" class="pb-2 text-right">{{ t('admin.dashboard.accountCost') }}</th>
              <th class="pb-2 text-right">{{ t('admin.dashboard.standard') }}</th>
            </tr>
          </thead>
          <tbody>
            <template v-for="(model, index) in visibleModelStats" :key="model.model">
              <tr
                class="border-t border-[var(--border)] transition-colors"
                :class="enableBreakdown ? 'cursor-pointer hover:bg-[var(--surface-2)] ' : ''"
                @click="enableBreakdown && toggleBreakdown('model', model.model)"
              >
                <td
                  class="max-w-[100px] truncate py-1.5 font-medium"
                  :class="enableBreakdown ? 'text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300' : 'text-[var(--foreground)] '"
                  :title="model.model"
                >
                  <span class="inline-flex items-center gap-1">
                    <span class="h-1.5 w-1.5 shrink-0 rounded-full" :style="{ backgroundColor: chartColors[index % chartColors.length] }" aria-hidden="true" />
                    <svg v-if="enableBreakdown && expandedKey === `model-${model.model}`" class="h-3 w-3 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"/></svg>
                    <svg v-else-if="enableBreakdown" class="h-3 w-3 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7"/></svg>
                    {{ model.model }}
                  </span>
                </td>
                <td class="py-1.5 text-right text-[var(--muted-foreground)]">
                  {{ formatNumber(model.requests) }}
                </td>
                <td class="py-1.5 text-right text-[var(--muted-foreground)]">
                  {{ formatTokens(model.total_tokens) }}
                </td>
                <td class="py-1.5 text-right text-green-600 dark:text-green-400">
                  USD {{ formatCost(model.actual_cost) }}
                </td>
                <td v-if="showAccountCost" class="py-1.5 text-right text-orange-500 dark:text-orange-400">
                  USD {{ formatCost(model.account_cost) }}
                </td>
                <td class="py-1.5 text-right text-[var(--muted-foreground)]">
                  USD {{ formatCost(model.cost) }}
                </td>
              </tr>
              <tr v-if="expandedKey === `model-${model.model}`">
                <td :colspan="distributionColspan" class="p-0">
                  <UserBreakdownSubTable
                    :items="breakdownItems"
                    :loading="breakdownLoading"
                    :show-account-cost="showAccountCost"
                  />
                </td>
              </tr>
            </template>
          </tbody>
        </table>
        <div v-if="displayModelStats.length > 10" class="mt-2 flex justify-end">
          <button
            type="button"
            class="text-xs font-medium text-[var(--brand-text,#d97706)] hover:underline"
            @click="showAllModels = !showAllModels"
          >
            {{ showAllModels ? (isZh ? '收起' : 'Show less') : (isZh ? `展开更多 (${displayModelStats.length - 10})` : `Show more (${displayModelStats.length - 10})`) }}
          </button>
        </div>
      </div>
    </div>
    <div
      v-else-if="activeView === 'model_distribution'"
      class="flex h-48 flex-col justify-center gap-4 text-sm text-[var(--muted-foreground)]"
      role="status"
    >
      <div class="w-full space-y-4" aria-hidden="true"><div v-for="row in 4" :key="row" class="flex items-center gap-4 border-b border-dashed border-[var(--border)] pb-2"><span class="h-1 rounded bg-[var(--border)]" :style="{ width: `${64 - row * 9}%` }" /><span class="ml-auto h-3 w-10 rounded-full border border-[var(--border)]" /></div></div>
      <span class="self-start rounded-full border border-[var(--border)] px-2.5 py-1 text-xs">{{ t('admin.dashboard.noDataAvailable') }}</span>
    </div>

    <div v-else-if="rankingLoading" class="flex h-48 items-center justify-center">
      <LoadingSpinner />
    </div>
    <div v-else-if="rankingUnknown" role="status" class="flex h-48 items-center justify-center text-sm text-[var(--muted-foreground)]">—</div>
    <div
      v-else-if="rankingError"
      class="flex h-48 items-center justify-center text-sm text-[var(--muted-foreground)]"
    >
      {{ t('admin.dashboard.failedToLoad') }}
    </div>
    <div v-else-if="rankingDisplayItems.length > 0 && rankingChartData" class="flex flex-col items-center gap-4 sm:flex-row sm:gap-6">
      <div class="h-48 w-48 shrink-0">
        <Doughnut :data="rankingChartData" :options="rankingDoughnutOptions" />
      </div>
      <div class="w-full min-w-0 flex-1 overflow-x-auto">
        <table class="w-full text-xs">
          <thead>
            <tr class="text-[var(--muted-foreground)]">
              <th class="pb-2 text-left">{{ t('admin.dashboard.spendingRankingUser') }}</th>
              <th class="pb-2 text-right">{{ t('admin.dashboard.spendingRankingRequests') }}</th>
              <th class="pb-2 text-right">{{ spendingRankingTokensLabel }}</th>
              <th class="pb-2 text-right">{{ t('admin.dashboard.spendingRankingSpend') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="(item, index) in visibleRankingItems"
              :key="item.isOther ? 'others' : `${item.user_id}-${index}`"
              class="border-t border-[var(--border)] transition-colors"
              :class="item.isOther
                ? 'bg-[var(--surface-2)] '
                : 'cursor-pointer hover:bg-[var(--surface-2)] '"
              @click="item.isOther ? undefined : emit('ranking-click', item)"
            >
              <td class="py-1.5">
                <div class="flex min-w-0 items-center gap-2">
                  <span class="shrink-0 text-[11px] font-semibold text-[var(--muted-foreground)]">
                    {{ item.isOther ? 'Σ' : `#${index + 1}` }}
                  </span>
                  <span
                    class="block max-w-[140px] truncate font-medium text-[var(--foreground)]"
                    :title="getRankingRowLabel(item)"
                  >
                    {{ getRankingRowLabel(item) }}
                  </span>
                </div>
              </td>
              <td class="py-1.5 text-right text-[var(--muted-foreground)]">
                {{ formatNumber(item.requests) }}
              </td>
              <td class="py-1.5 text-right text-[var(--muted-foreground)]">
                {{ formatTokens(item.tokens) }}
              </td>
              <td class="py-1.5 text-right text-green-600 dark:text-green-400">
                USD {{ formatCost(item.actual_cost) }}
              </td>
            </tr>
          </tbody>
        </table>
        <div v-if="rankingDisplayItems.length > 10" class="mt-2 flex justify-end">
          <button
            type="button"
            class="text-xs font-medium text-[var(--brand-text,#d97706)] hover:underline"
            @click="showAllRanking = !showAllRanking"
          >
            {{ showAllRanking ? (isZh ? '收起' : 'Show less') : (isZh ? `展开更多 (${rankingDisplayItems.length - 10})` : `Show more (${rankingDisplayItems.length - 10})`) }}
          </button>
        </div>
      </div>
    </div>
    <div
      v-else
      class="flex h-48 flex-col justify-center gap-4 text-sm text-[var(--muted-foreground)]"
      role="status"
    >
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
import type { ModelStat, UserSpendingRankingItem, UserBreakdownItem } from '@/types'
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
const spendingRankingTokensLabel = computed(() => (isZh.value ? '总 Token' : t('admin.dashboard.spendingRankingTokens')))
const showAllModels = ref(false)
const showAllRanking = ref(false)

type DistributionMetric = 'tokens' | 'actual_cost'
type ModelSource = 'requested' | 'upstream' | 'mapping'
type RankingDisplayItem = UserSpendingRankingItem & { isOther?: boolean }
const props = withDefaults(defineProps<{
  modelStats: ModelStat[]
  modelUnknown?: boolean
  modelError?: boolean
  upstreamModelStats?: ModelStat[]
  mappingModelStats?: ModelStat[]
  source?: ModelSource
  enableRankingView?: boolean
  rankingItems?: UserSpendingRankingItem[]
  rankingTotalActualCost?: number
  rankingTotalRequests?: number
  rankingTotalTokens?: number
  loading?: boolean
  metric?: DistributionMetric
  showSourceToggle?: boolean
  showMetricToggle?: boolean
  enableBreakdown?: boolean
  showAccountCost?: boolean
  rankingLoading?: boolean
  rankingError?: boolean
  rankingUnknown?: boolean
  startDate?: string
  endDate?: string
  filters?: Record<string, any>
}>(), {
  upstreamModelStats: () => [],
  mappingModelStats: () => [],
  source: 'requested',
  enableRankingView: false,
  rankingItems: () => [],
  rankingTotalActualCost: 0,
  rankingTotalRequests: 0,
  rankingTotalTokens: 0,
  loading: false,
  metric: 'tokens',
  showSourceToggle: false,
  showMetricToggle: false,
  enableBreakdown: true,
  showAccountCost: true,
  rankingLoading: false,
  rankingError: false,
  rankingUnknown: false,
  modelUnknown: false,
  modelError: false
})

const expandedKey = ref<string | null>(null)
const breakdownItems = ref<UserBreakdownItem[]>([])
const breakdownLoading = ref(false)

const toggleBreakdown = async (type: string, id: string) => {
  const key = `${type}-${id}`
  if (expandedKey.value === key) {
    expandedKey.value = null
    return
  }
  expandedKey.value = key
  breakdownLoading.value = true
  breakdownItems.value = []
  try {
    const res = await getUserBreakdown({
      ...props.filters,
      start_date: props.startDate,
      end_date: props.endDate,
      model: id,
      model_source: props.source,
    })
    breakdownItems.value = res.users || []
  } catch {
    breakdownItems.value = []
  } finally {
    breakdownLoading.value = false
  }
}

const emit = defineEmits<{
  'update:metric': [value: DistributionMetric]
  'update:source': [value: ModelSource]
  'ranking-click': [item: UserSpendingRankingItem]
}>()

const enableRankingView = computed(() => props.enableRankingView)
const showAccountCost = computed(() => props.showAccountCost)
const distributionColspan = computed(() => showAccountCost.value ? 6 : 5)
const activeView = ref<'model_distribution' | 'spending_ranking'>('model_distribution')

const chartColors = ['#d97706', '#059669', '#6366f1', '#fbbf24', '#34d399', '#a5b4fc', '#b45309', '#047857', '#4338ca', '#f59e0b', '#10b981', '#818cf8']

const displayModelStats = computed(() => {
  const sourceStats = props.source === 'upstream'
    ? props.upstreamModelStats
    : props.source === 'mapping'
      ? props.mappingModelStats
      : props.modelStats
  if (!sourceStats?.length) return []

  const metricKey = props.metric === 'actual_cost' ? 'actual_cost' : 'total_tokens'
  return [...sourceStats].sort((a, b) => toFiniteNumber(b[metricKey]) - toFiniteNumber(a[metricKey]))
})
const visibleModelStats = computed(() =>
  showAllModels.value ? displayModelStats.value : displayModelStats.value.slice(0, 10)
)

const chartData = computed(() => {
  if (!displayModelStats.value.length) return null

  return {
    labels: displayModelStats.value.map((m) => m.model),
    datasets: [
      {
        data: displayModelStats.value.map((m) => (props.metric === 'actual_cost' ? m.actual_cost : m.total_tokens) ?? NaN),
        backgroundColor: displayModelStats.value.map((_, index) => chartColors[index % chartColors.length]),
        borderWidth: 0
      }
    ]
  }
})

const rankingChartData = computed(() => {
  if (!props.rankingItems?.length) return null

  const labels = props.rankingItems.map((item, index) => `#${index + 1} ${getRankingUserLabel(item)}`)
  const data = props.rankingItems.map((item) => toFiniteNumber(item.actual_cost))
  const backgroundColor = props.rankingItems.map((_, index) => chartColors[index % chartColors.length]!)
  backgroundColor[0] = '#3b82f6'

  if (otherRankingItem.value) {
    labels.push(t('admin.dashboard.spendingRankingOther'))
    data.push(otherRankingItem.value.actual_cost)
    backgroundColor.push('#94a3b8')
  }

  return {
    labels,
    datasets: [
      {
        data,
        backgroundColor,
        borderWidth: 0
      }
    ]
  }
})

const otherRankingItem = computed<RankingDisplayItem | null>(() => {
  if (!props.rankingItems?.length) return null

  const rankedActualCost = props.rankingItems.reduce((sum, item) => sum + toFiniteNumber(item.actual_cost), 0)
  const rankedRequests = props.rankingItems.reduce((sum, item) => sum + toFiniteNumber(item.requests), 0)
  const rankedTokens = props.rankingItems.reduce((sum, item) => sum + toFiniteNumber(item.tokens), 0)

  const otherActualCost = Math.max((props.rankingTotalActualCost || 0) - rankedActualCost, 0)
  const otherRequests = Math.max((props.rankingTotalRequests || 0) - rankedRequests, 0)
  const otherTokens = Math.max((props.rankingTotalTokens || 0) - rankedTokens, 0)

  if (otherActualCost <= 0.000001 && otherRequests <= 0 && otherTokens <= 0) return null

  return {
    user_id: 0,
    email: '',
    username: '',
    actual_cost: otherActualCost,
    requests: otherRequests,
    tokens: otherTokens,
    isOther: true
  }
})

const rankingDisplayItems = computed<RankingDisplayItem[]>(() => {
  if (!props.rankingItems?.length) return []
  return otherRankingItem.value
    ? [...props.rankingItems, otherRankingItem.value]
    : [...props.rankingItems]
})
const visibleRankingItems = computed<RankingDisplayItem[]>(() =>
  showAllRanking.value ? rankingDisplayItems.value : rankingDisplayItems.value.slice(0, 10)
)

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

const rankingDoughnutOptions = computed(() => ({
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
          return `${context.label}: USD ${formatCost(value)} (${percentage}%)`
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
  return toFiniteNumber(value).toLocaleString()
}

const getRankingUserLabel = (item: UserSpendingRankingItem): string => {
  if (item.username?.trim()) return item.username.trim()
  if (item.email?.trim()) return item.email.trim()
  return t('admin.redeem.userPrefix', { id: item.user_id })
}

const getRankingRowLabel = (item: RankingDisplayItem): string => {
  if (item.isOther) return t('admin.dashboard.spendingRankingOther')
  return getRankingUserLabel(item)
}

const toFiniteNumber = (value: unknown): number => {
  const numberValue = Number(value)
  return Number.isFinite(numberValue) ? numberValue : 0
}


</script>
