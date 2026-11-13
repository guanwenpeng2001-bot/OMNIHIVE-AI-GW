<script setup lang="ts">
import { computed, ref } from 'vue'
import { useMutationObserver } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import { Chart as ChartJS, BarElement, CategoryScale, Legend, LinearScale, Tooltip } from 'chart.js'
import { Bar } from 'vue-chartjs'
import type { OpsLatencyHistogramResponse } from '@/api/admin/ops'
import type { ChartState } from '../types'
import HelpTooltip from '@/components/common/HelpTooltip.vue'
import OpsTelemetryIdle from './OpsTelemetryIdle.vue'

ChartJS.register(BarElement, CategoryScale, LinearScale, Tooltip, Legend)

interface Props {
  latencyData: OpsLatencyHistogramResponse | null
  loading: boolean
}

const props = defineProps<Props>()
const { t } = useI18n()

const themeRevision = ref(0)
useMutationObserver(document.documentElement, () => { themeRevision.value += 1 }, {
  attributes: true,
  attributeFilter: ['class', 'data-theme']
})
const colors = computed(() => {
  void themeRevision.value
  const style = getComputedStyle(document.documentElement)
  return {
    blue: style.getPropertyValue('--brand').trim() || '#f59e0b',
    grid: style.getPropertyValue('--border').trim() || '#e9e9ee',
    text: style.getPropertyValue('--muted-foreground').trim() || '#6f6f78',
    background: style.getPropertyValue('--card').trim() || '#ffffff',
    foreground: style.getPropertyValue('--foreground').trim() || '#131316'
  }
})

const hasData = computed(() => (props.latencyData?.total_requests ?? 0) > 0)

const state = computed<ChartState>(() => {
  if (hasData.value) return 'ready'
  if (props.loading) return 'loading'
  return 'empty'
})

const chartData = computed(() => {
  if (!props.latencyData || !hasData.value) return null
  const c = colors.value
  return {
    labels: props.latencyData.buckets.map((b) => b.range),
    datasets: [
      {
        label: t('admin.ops.requests'),
        data: props.latencyData.buckets.map((b) => b.count),
        backgroundColor: c.blue,
        borderRadius: 4,
        barPercentage: 0.6
      }
    ]
  }
})

const options = computed(() => {
  const c = colors.value
  return {
    responsive: true,
    maintainAspectRatio: false,
    plugins: {
      legend: { display: false },
      tooltip: {
        backgroundColor: c.background,
        titleColor: c.foreground,
        bodyColor: c.text,
        borderColor: c.grid,
        borderWidth: 1
      }
    },
    scales: {
      x: {
        grid: { display: false },
        ticks: { color: c.text, font: { size: 10 } }
      },
      y: {
        beginAtZero: true,
        grid: { color: c.grid, lineWidth: .5 },
        border: { dash: [3, 5] },
        ticks: { color: c.text, font: { size: 10 } }
      }
    }
  }
})
</script>

<template>
  <div class="flex h-full min-w-0 flex-col rounded-xl border border-[var(--border)] bg-[var(--card)] p-4 shadow-none">
    <div class="mb-4 flex items-center justify-between">
      <h3 class="flex items-center gap-2 text-sm font-bold text-[var(--foreground)]">
        <svg class="h-4 w-4 text-[var(--brand-text)]" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            stroke-width="2"
            d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"
          />
        </svg>
        {{ t('admin.ops.latencyHistogram') }}
        <HelpTooltip :content="t('admin.ops.tooltips.latencyHistogram')" />
      </h3>
    </div>

    <div class="min-h-0 flex-1">
      <Bar v-if="state === 'ready' && chartData" :data="chartData" :options="options" />
      <div v-else class="flex h-full items-center justify-center">
        <div v-if="state === 'loading'" class="animate-pulse text-sm text-[var(--muted-foreground)]">{{ t('common.loading') }}</div>
        <OpsTelemetryIdle v-else mode="bars" :description="t('admin.ops.charts.emptyRequest')" />
      </div>
    </div>
  </div>
</template>
