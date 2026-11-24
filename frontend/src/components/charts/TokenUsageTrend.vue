<template>
  <div class="card min-w-0 border border-[var(--border)] bg-[var(--card)] p-4 tabular-nums">
    <h3 class="mb-4 text-sm font-semibold text-[var(--foreground)]">
      {{ t('admin.dashboard.tokenUsageTrend') }}
      <span class="ml-2 font-normal text-[var(--muted-foreground)]">{{ t('common.displayTimeZone', { timezone: timeZone || getBrowserTimeZone() }) }}</span>
    </h3>
    <div v-if="loading" class="flex h-48 items-center justify-center">
      <LoadingSpinner />
    </div>
    <div v-else-if="trendData.length > 0 && chartData" class="h-48">
      <Line :data="chartData" :options="lineOptions" />
    </div>
    <div
      v-else
      class="trend-empty flex h-48 flex-col justify-center gap-4 text-sm text-[var(--muted-foreground)]"
      role="status"
    >
      <div class="space-y-4" aria-hidden="true"><div v-for="row in 4" :key="row" class="flex items-center gap-4 border-b border-dashed border-[var(--border)] pb-2"><span class="h-1 rounded bg-[var(--border)]" :style="{ width: `${60 - row * 8}%` }" /><span class="ml-auto h-3 w-10 rounded-full border border-[var(--border)]" /></div></div>
      <span class="self-start rounded-full border border-[var(--border)] px-2.5 py-1 text-xs">{{ t('admin.dashboard.noDataAvailable') }}</span>
    </div>
    <div v-if="!loading && chartData" class="mt-4 overflow-x-auto">
      <table class="w-max min-w-full whitespace-nowrap text-right text-xs" data-testid="trend-data-table">
        <caption class="mb-2 whitespace-normal text-left text-[var(--muted-foreground)]">{{ tableDescription }}</caption>
        <thead><tr>
          <th scope="col" class="p-2 text-left">{{ t('usage.time') }} ({{ timeZone || getBrowserTimeZone() }})</th>
          <th v-for="dataset in chartData.datasets" :key="dataset.label" scope="col" class="p-2">{{ formatDatasetHeader(dataset.label) }}</th>
          <th scope="col" class="p-2">{{ t('dashboard.actual') }} (USD)</th>
          <th scope="col" class="p-2">{{ t('dashboard.standard') }} (USD)</th>
        </tr></thead>
        <tbody><tr v-for="(point, index) in trendData" :key="`${point.date}-${index}`" class="border-t border-[var(--border)]">
          <th scope="row" class="p-2 text-left font-normal">{{ chartData.labels[index] }}</th>
          <td v-for="dataset in chartData.datasets" :key="dataset.label" class="p-2">{{ formatTableValue(dataset.data[index], dataset.yAxisID === 'yPercent') }}</td>
          <td class="p-2">{{ formatCost(point.actual_cost) }}</td>
          <td class="p-2">{{ formatCost(point.cost) }}</td>
        </tr></tbody>
      </table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { formatLedgerAmount as formatCost } from '@/utils/money'
import { getBrowserTimeZone } from '@/utils/format'
import { computed, onMounted, onBeforeUnmount, ref } from 'vue'
import { getLocale } from '@/i18n'
import { useI18n } from 'vue-i18n'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  Filler
} from 'chart.js'
import { Line } from 'vue-chartjs'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import type { TrendDataPoint } from '@/types'

ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  Filler
)

const i18n = useI18n()
const { t } = i18n
const isZh = computed(() => {
  const rawLocale = typeof i18n.locale === 'string' ? i18n.locale : i18n.locale?.value
  if (rawLocale) return String(rawLocale).toLowerCase().startsWith('zh')
  if (getLocale().startsWith('zh')) return true
  const probe = t('admin.dashboard.tokenUsageTrend')
  return Boolean(probe && probe !== 'admin.dashboard.tokenUsageTrend' && /[\u4e00-\u9fa5]/.test(probe))
})
const DATASET_HEADER_ZH: Record<string, string> = {
  Token: '总 Token',
  Tokens: '总 Token',
  Input: '输入',
  Output: '输出',
  'Cache Creation': '缓存写入',
  'Cache Read': '缓存读取',
  'Cache Hit Rate': '缓存命中率'
}
const formatDatasetHeader = (label: string): string => {
  if (!isZh.value) return label
  return DATASET_HEADER_ZH[label] ?? label
}
const tableDescription = computed(() => isZh.value
  ? '同源数据表：金额为每个时间段合计（USD）；缓存命中率 = 缓存读取 /（输入 + 缓存写入 + 缓存读取）；未知显示 -。'
  : 'Chart data: amounts are totals per time bucket (USD); cache hit rate = cache read / (input + cache creation + cache read); unknown is -.')
const formatTableValue = (value: number, percent: boolean): string => Number.isFinite(value)
  ? (percent ? `${value.toFixed(1)}%` : String(value)) : '-'

const props = defineProps<{
  trendData: TrendDataPoint[]
  loading?: boolean
  /** Query zone for calendar bucket labels, which must not be reparsed. */
  timeZone?: string
}>()

const themeColors = ref({ text: '#71717a', grid: '#e4e4e7', card: '#ffffff', foreground: '#18181b' })
let themeObserver: MutationObserver | undefined
onMounted(() => {
  const updateColors = () => {
    const styles = getComputedStyle(document.documentElement)
    themeColors.value = {
      text: styles.getPropertyValue('--muted-foreground').trim() || '#71717a',
      grid: styles.getPropertyValue('--border').trim() || '#e4e4e7',
      card: styles.getPropertyValue('--card').trim() || '#ffffff',
      foreground: styles.getPropertyValue('--foreground').trim() || '#18181b'
    }
  }
  updateColors()
  themeObserver = new MutationObserver(updateColors)
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class', 'style', 'data-theme', 'data-style'] })
})
onBeforeUnmount(() => themeObserver?.disconnect())
const chartColors = computed(() => ({
  text: themeColors.value.text,
  grid: themeColors.value.grid,
  input: '#6366f1',
  output: '#059669',
  cacheCreation: '#d97706',
  cacheRead: '#34d399',
  cacheHitRate: '#a5b4fc'
}))

const chartData = computed(() => {
  if (!props.trendData?.length) return null

  return {
    labels: props.trendData.map((d) => d.date),
    datasets: [
      {
        label: 'Input',
        data: props.trendData.map((d) => d.input_tokens),
        borderColor: chartColors.value.input,
        backgroundColor: `${chartColors.value.input}0f`,
        fill: true,
        tension: 0.3
      },
      {
        label: 'Output',
        data: props.trendData.map((d) => d.output_tokens),
        borderColor: chartColors.value.output,
        backgroundColor: `${chartColors.value.output}0f`,
        fill: true,
        tension: 0.3
      },
      {
        label: 'Cache Creation',
        data: props.trendData.map((d) => d.cache_creation_tokens),
        borderColor: chartColors.value.cacheCreation,
        backgroundColor: `${chartColors.value.cacheCreation}0f`,
        fill: true,
        tension: 0.3
      },
      {
        label: 'Cache Read',
        data: props.trendData.map((d) => d.cache_read_tokens),
        borderColor: chartColors.value.cacheRead,
        backgroundColor: `${chartColors.value.cacheRead}0f`,
        fill: true,
        tension: 0.3
      },
      {
        label: 'Cache Hit Rate',
        data: props.trendData.map((d) => {
          if (![d.input_tokens, d.cache_read_tokens, d.cache_creation_tokens].every(Number.isFinite)) return NaN
          const totalPromptTokens = d.input_tokens + d.cache_read_tokens + d.cache_creation_tokens
          return totalPromptTokens > 0 ? (d.cache_read_tokens / totalPromptTokens) * 100 : 0
        }),
        borderColor: chartColors.value.cacheHitRate,
        backgroundColor: `${chartColors.value.cacheHitRate}20`,
        borderDash: [5, 5],
        fill: false,
        tension: 0.3,
        yAxisID: 'yPercent'
      }
    ]
  }
})

const lineOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  animation: false as const,
  elements: { point: { radius: 0, hitRadius: 10, hoverRadius: 4 }, line: { borderWidth: 1.5 } },
  interaction: {
    intersect: false,
    mode: 'index' as const
  },
  plugins: {
    legend: {
      position: 'top' as const,
      labels: {
        color: chartColors.value.text,
        usePointStyle: true,
        pointStyle: 'circle',
        padding: 15,
        font: {
          size: 11
        }
      }
    },
    tooltip: {
      backgroundColor: themeColors.value.card,
      titleColor: themeColors.value.foreground,
      bodyColor: themeColors.value.foreground,
      footerColor: themeColors.value.text,
      borderColor: themeColors.value.grid,
      borderWidth: 1,
      padding: 12,
      callbacks: {
        label: (context: any) => {
          if (context.dataset.yAxisID === 'yPercent') {
            return `${context.dataset.label}: ${context.raw.toFixed(1)}%`
          }
          return `${context.dataset.label}: ${formatTokens(context.raw)}`
        },
        footer: (tooltipItems: any) => {
          const dataIndex = tooltipItems[0]?.dataIndex
          if (dataIndex !== undefined && props.trendData[dataIndex]) {
            const data = props.trendData[dataIndex]
            return `${t('dashboard.actual')}: USD ${formatCost(data.actual_cost)} | ${t('dashboard.standard')}: USD ${formatCost(data.cost)}`
          }
          return ''
        }
      }
    }
  },
  scales: {
    x: {
      border: { display: false, dash: [3, 4] },
      grid: {
        color: chartColors.value.grid
      },
      ticks: {
        color: chartColors.value.text,
        font: {
          size: 10
        }
      }
    },
    y: {
      beginAtZero: true,
      border: { display: false, dash: [3, 4] },
      grid: {
        color: chartColors.value.grid
      },
      ticks: {
        color: chartColors.value.text,
        font: {
          size: 10
        },
        callback: (value: string | number) => formatTokens(Number(value))
      }
    },
    yPercent: {
      border: { display: false },
      position: 'right' as const,
      min: 0,
      max: 100,
      grid: {
        drawOnChartArea: false
      },
      ticks: {
        color: chartColors.value.cacheHitRate,
        font: {
          size: 10
        },
        callback: (value: string | number) => `${value}%`
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

</script>
