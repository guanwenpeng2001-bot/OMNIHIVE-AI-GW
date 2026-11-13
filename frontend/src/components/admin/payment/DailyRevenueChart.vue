<template>
  <div class="card p-4">
    <h3 class="mb-4 border-b border-[var(--border)] pb-3 text-sm font-semibold text-[var(--foreground)]">
      {{ t('payment.admin.dailyRevenue') }}
    </h3>
    <div class="h-64">
      <div v-if="loading" class="flex h-full items-center justify-center">
        <LoadingSpinner size="md" />
      </div>
      <Line v-else-if="chartData" :data="chartData" :options="chartOptions" />
      <div
        v-else
        class="relative flex h-full items-center justify-center text-sm text-[var(--muted-foreground)]"
        role="status"
      >
        <svg class="absolute inset-0 h-full w-full" viewBox="0 0 400 200" preserveAspectRatio="none" aria-hidden="true">
          <path d="M0 30H400 M0 75H400 M0 120H400 M40 0V170 M120 0V170 M200 0V170 M280 0V170 M360 0V170" fill="none" stroke="var(--border)" stroke-width=".5" stroke-dasharray="3 5" />
          <path d="M0 170H400" fill="none" stroke="var(--brand)" stroke-opacity=".4" />
        </svg>
        <span class="relative rounded-full border border-[var(--border)] bg-[var(--card)] px-3 py-1.5 font-mono text-xs">{{ t('payment.admin.noData') }}</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useMutationObserver } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
  Legend,
  Filler
} from 'chart.js'
import { Line } from 'vue-chartjs'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import type { DailyPaymentStats } from '@/types/payment'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend, Filler)

const { t } = useI18n()

const props = defineProps<{
  data: DailyPaymentStats[]
  loading?: boolean
}>()

const themeRevision = ref(0)
useMutationObserver(document.documentElement, () => { themeRevision.value += 1 }, {
  attributes: true,
  attributeFilter: ['class', 'data-theme']
})
const chartTheme = computed(() => {
  void themeRevision.value
  const style = getComputedStyle(document.documentElement)
  const token = (name: string, fallback: string) => style.getPropertyValue(name).trim() || fallback
  return {
    series: [token('--brand', '#f59e0b'), token('--info', '#2563eb'), token('--ok', '#16a34a'), token('--foreground', '#131316')],
    grid: token('--border', '#e9e9ee'),
    text: token('--muted-foreground', '#6f6f78'),
    foreground: token('--foreground', '#131316'),
    background: token('--card', '#ffffff'),
    count: token('--muted-foreground', '#6f6f78')
  }
})

const chartData = computed(() => {
  if (!props.data || props.data.length === 0) return null
  const currencies = [...new Set(props.data.flatMap(day => Object.keys(day.amount)))].sort()
  return {
    labels: props.data.map(d => d.date),
    datasets: [
      ...currencies.map((currency, index) => {
        const borderColor = chartTheme.value.series[index % chartTheme.value.series.length]
        return {
          label: `${currency} ${t('payment.admin.revenue')}`,
          data: props.data.map(day => day.amount[currency] || 0),
          borderColor,
          backgroundColor: borderColor,
          fill: false,
          tension: 0.3,
          pointRadius: 0,
          pointHoverRadius: 5,
        }
      }),
      {
        label: t('payment.admin.orderCount'),
        data: props.data.map(d => d.count),
        borderColor: chartTheme.value.count,
        backgroundColor: chartTheme.value.count,
        borderDash: [4, 4],
        fill: false,
        tension: 0.3,
        pointRadius: 0,
        pointHoverRadius: 5,
        yAxisID: 'y1',
      }
    ]
  }
})

const chartOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: { mode: 'index' as const, intersect: false },
  scales: {
    x: {
      grid: { display: false },
      ticks: { color: chartTheme.value.text, maxTicksLimit: 7, font: { size: 10 } }
    },
    y: {
      type: 'linear' as const,
      display: true,
      position: 'left' as const,
      title: { display: true, text: t('payment.admin.revenue'), color: chartTheme.value.text },
      grid: { color: chartTheme.value.grid, lineWidth: .5 },
      ticks: { color: chartTheme.value.text },
      border: { dash: [3, 5] },
      beginAtZero: true,
    },
    y1: {
      type: 'linear' as const,
      display: true,
      position: 'right' as const,
      title: { display: true, text: t('payment.admin.orderCount'), color: chartTheme.value.text },
      ticks: { color: chartTheme.value.text, precision: 0 },
      beginAtZero: true,
      grid: { drawOnChartArea: false },
    }
  },
  plugins: {
    legend: { position: 'top' as const, align: 'end' as const, labels: { color: chartTheme.value.text, usePointStyle: true, boxWidth: 6, font: { size: 11 } } },
    tooltip: {
      backgroundColor: chartTheme.value.background,
      titleColor: chartTheme.value.foreground,
      bodyColor: chartTheme.value.text,
      borderColor: chartTheme.value.grid,
      borderWidth: 1
    }
  }
}))
</script>
