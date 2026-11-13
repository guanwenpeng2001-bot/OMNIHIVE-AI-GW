<script setup lang="ts">
import { computed, ref } from 'vue'
import { useMutationObserver } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import {
  Chart as ChartJS,
  CategoryScale,
  Filler,
  Legend,
  LineElement,
  LinearScale,
  PointElement,
  Title,
  Tooltip
} from 'chart.js'
import { Line } from 'vue-chartjs'
import type { OpsThroughputTrendPoint } from '@/api/admin/ops'
import type { ChartState } from '../types'
import { formatHistoryLabel, sumNumbers } from '../utils/opsFormatters'
import HelpTooltip from '@/components/common/HelpTooltip.vue'
import OpsTelemetryIdle from './OpsTelemetryIdle.vue'

ChartJS.register(Title, Tooltip, Legend, LineElement, LinearScale, PointElement, CategoryScale, Filler)

interface Props {
  points: OpsThroughputTrendPoint[]
  loading: boolean
  timeRange: string
  fullscreen?: boolean
}

const props = defineProps<Props>()
const { t } = useI18n()

const themeRevision = ref(0)
useMutationObserver(document.documentElement, () => { themeRevision.value += 1 }, {
  attributes: true,
  attributeFilter: ['class', 'data-theme']
})
const chartTheme = computed(() => {
  void themeRevision.value
  const style = getComputedStyle(document.documentElement)
  return {
    brand: style.getPropertyValue('--brand').trim() || '#f59e0b',
    accent: style.getPropertyValue('--accent').trim() || '#fdf4e3',
    grid: style.getPropertyValue('--border').trim() || '#e9e9ee',
    text: style.getPropertyValue('--muted-foreground').trim() || '#6f6f78',
    background: style.getPropertyValue('--card').trim() || '#ffffff',
    foreground: style.getPropertyValue('--foreground').trim() || '#131316'
  }
})
const colors = computed(() => ({
  brand: chartTheme.value.brand,
  brandAlpha: chartTheme.value.accent,
  grid: chartTheme.value.grid,
  text: chartTheme.value.text
}))

const totalRequests = computed(() => sumNumbers(props.points.map((p) => p.request_count)))

const chartData = computed(() => {
  if (!props.points.length || totalRequests.value <= 0) return null
  return {
    labels: props.points.map((p) => formatHistoryLabel(p.bucket_start, props.timeRange)),
    datasets: [
      {
        label: t('admin.ops.switchRate'),
        data: props.points.map((p) => {
          const requests = p.request_count ?? 0
          const switches = p.switch_count ?? 0
          if (requests <= 0) return 0
          return switches / requests
        }),
        borderColor: colors.value.brand,
        backgroundColor: colors.value.brandAlpha,
        fill: true,
        tension: 0.35,
        pointRadius: 0,
        pointHitRadius: 10
      }
    ]
  }
})

const state = computed<ChartState>(() => {
  if (chartData.value) return 'ready'
  if (props.loading) return 'loading'
  return 'empty'
})

const options = computed(() => {
  const c = colors.value
  return {
    responsive: true,
    maintainAspectRatio: false,
    interaction: { intersect: false, mode: 'index' as const },
    plugins: {
      legend: {
        position: 'top' as const,
        align: 'end' as const,
        labels: { color: c.text, usePointStyle: true, boxWidth: 6, font: { size: 10 } }
      },
      tooltip: {
        backgroundColor: chartTheme.value.background,
        titleColor: chartTheme.value.foreground,
        bodyColor: chartTheme.value.text,
        borderColor: c.grid,
        borderWidth: 1,
        padding: 10,
        displayColors: true,
        callbacks: {
          label: (context: any) => {
            const value = typeof context?.parsed?.y === 'number' ? context.parsed.y : 0
            return `${t('admin.ops.switchRate')}: ${value.toFixed(3)}`
          }
        }
      }
    },
    scales: {
      x: {
        type: 'category' as const,
        grid: { display: false },
        ticks: {
          color: c.text,
          font: { size: 10 },
          maxTicksLimit: 8,
          autoSkip: true,
          autoSkipPadding: 10
        }
      },
      y: {
        type: 'linear' as const,
        display: true,
        position: 'left' as const,
        grid: { color: c.grid, lineWidth: .5 },
        border: { dash: [3, 5] },
        ticks: {
          color: c.text,
          font: { size: 10 },
          callback: (value: any) => Number(value).toFixed(3)
        }
      }
    }
  }
})
</script>

<template>
  <div class="flex h-full min-w-0 flex-col rounded-xl border border-[var(--border)] bg-[var(--card)] p-4 shadow-none">
    <div class="mb-4 flex shrink-0 items-center justify-between">
      <h3 class="flex items-center gap-2 text-sm font-bold text-[var(--foreground)]">
        <svg class="h-4 w-4 text-[var(--brand-text)]" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 7h10M7 12h6m-6 5h3" />
        </svg>
        {{ t('admin.ops.switchRateTrend') }}
        <HelpTooltip v-if="!props.fullscreen" :content="t('admin.ops.tooltips.switchRateTrend')" />
      </h3>
    </div>

    <div class="min-h-0 flex-1">
      <Line v-if="state === 'ready' && chartData" :data="chartData" :options="options" />
      <div v-else class="flex h-full items-center justify-center">
        <div v-if="state === 'loading'" class="animate-pulse text-sm text-[var(--muted-foreground)]">{{ t('common.loading') }}</div>
        <OpsTelemetryIdle v-else :description="t('admin.ops.charts.emptyRequest')" />
      </div>
    </div>
  </div>
</template>
