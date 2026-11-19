<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { Bar } from 'vue-chartjs'
import { BarElement, CategoryScale, Chart as ChartJS, LinearScale, Tooltip, type ChartOptions } from 'chart.js'
import { useI18n } from 'vue-i18n'
import { Button, SegmentedControl } from '@/components/ui'
import type { Schema } from '@/features/requests/api'
import { dateInZone } from '@/features/requests/filters'
import { displayNumber, moneyText, timestamp } from '@/features/requests/presentation'

ChartJS.register(BarElement, CategoryScale, LinearScale, Tooltip)
const props = defineProps<{ activity: Schema['CostActivity'] }>()
const emit = defineEmits<{ day: [date: string] }>()
const { t, locale } = useI18n()
const metric = ref('spend')
const day = ref('')
const days = computed(() => props.activity.items.filter(row => row.group !== null).sort((a, b) => a.group!.localeCompare(b.group!)))
const options = computed(() => [
  { value: 'spend', label: t('wp6.billing.spend') },
  { value: 'requests', label: t('wp6.requests.logicalRequests') },
])

const colors = ref({ bar: '#D97706', grid: '#DDE0E6', text: '#666C79', font: 'monospace' })
let observer: MutationObserver | undefined
function theme() {
  const css = getComputedStyle(document.documentElement)
  colors.value = {
    bar: css.getPropertyValue('--oh-chart-1').trim(),
    grid: css.getPropertyValue('--oh-border-subtle').trim(),
    text: css.getPropertyValue('--oh-fg-muted').trim(),
    font: css.getPropertyValue('--oh-font-mono').trim().split(',')[0],
  }
}
onMounted(() => {
  theme()
  observer = new MutationObserver(theme)
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class', 'data-theme'] })
})
onBeforeUnmount(() => observer?.disconnect())

function exact(row: Schema['CostGroup']): string {
  return metric.value === 'spend'
    ? moneyText(row.platform_charge?.currency === props.activity.currency ? row.platform_charge : null, locale.value)
    : displayNumber(row.logical_requests, locale.value)
}
function graphValue(row: Schema['CostGroup']): number | null {
  const value = metric.value === 'spend'
    ? row.platform_charge?.currency === props.activity.currency ? row.platform_charge.amount : null
    : row.logical_requests
  return value == null || !Number.isFinite(Number(value)) ? null : Number(value)
}
const data = computed(() => ({
  labels: days.value.map(row => row.group!.slice(5)),
  datasets: [{ data: days.value.map(graphValue), backgroundColor: colors.value.bar, borderRadius: 3, maxBarThickness: 42 }],
}))
const chartOptions = computed<ChartOptions<'bar'>>(() => ({
  responsive: true,
  maintainAspectRatio: false,
  animation: false,
  onClick: (_event, marks) => {
    const selected = marks[0] && days.value[marks[0].index]
    if (selected?.group) emit('day', selected.group)
  },
  scales: {
    x: { grid: { display: false }, ticks: { color: colors.value.text, maxTicksLimit: 9, font: { family: colors.value.font, size: 11 } } },
    y: { beginAtZero: true, grid: { color: colors.value.grid }, ticks: {
      maxTicksLimit: 5, color: colors.value.text, font: { family: colors.value.font, size: 11 },
      callback: raw => new Intl.NumberFormat(locale.value, { notation: 'compact', maximumFractionDigits: 2 }).format(Number(raw)),
    } },
  },
  plugins: {
    legend: { display: false },
    tooltip: { callbacks: { label: context => exact(days.value[context.dataIndex]) } },
  },
}))
function selectDay(event: Event) {
  day.value = (event.target as HTMLSelectElement).value
}
</script>

<template>
  <div class="space-y-3">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <p class="min-w-0 break-words font-mono text-xs tabular-nums text-oh-muted">
        {{ dateInZone(Date.parse(activity.from), activity.timezone) }} → {{ dateInZone(Date.parse(activity.to), activity.timezone) }} · {{ activity.timezone }}
        <br />{{ t('wp6.billing.samples', { count: displayNumber(activity.totals.observed_usage_requests, locale) }) }}
         · {{ t('wp6.billing.observedAt', { at: timestamp(activity.as_of, locale, activity.timezone) }) }}
      </p>
      <SegmentedControl v-model="metric" :options="options" :aria-label="t('wp6.billing.trend')" size="sm" />
    </div>
    <div v-if="days.some(row => graphValue(row) !== null)" class="relative h-48 min-w-0 md:h-52">
      <Bar :data="data" :options="chartOptions" :aria-label="metric === 'spend' ? `${t('wp6.billing.spend')} · ${activity.currency}` : t('wp6.requests.logicalRequests')" />
    </div>
    <p v-else class="rounded-oh-md border border-dashed border-oh-default bg-oh-subtle p-4 text-sm text-oh-muted">{{ t('wp6.billing.noTrend') }}</p>
    <div v-if="days.length" class="flex flex-wrap items-end justify-between gap-2 border-t border-oh-subtle pt-3">
      <label class="grid min-w-0 gap-1.5 text-xs font-medium text-oh-secondary">
        {{ t('wp6.billing.inspectDay') }}
        <select
          :value="day" class="oh-focus-ring min-h-8 max-w-full rounded-oh-sm border border-oh-default bg-oh-surface px-2 font-mono text-xs text-oh-primary"
          @change="selectDay"
        >
          <option value="">{{ t('wp6.billing.selectDay') }}</option>
          <option v-for="row in days" :key="row.group!" :value="row.group!">{{ row.group }} · {{ exact(row) }}</option>
        </select>
      </label>
      <Button size="sm" variant="secondary" :disabled="!day" @click="emit('day', day)">{{ t('wp6.billing.openDay') }}</Button>
    </div>
  </div>
</template>
