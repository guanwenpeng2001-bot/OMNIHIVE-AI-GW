<template>
  <div class="space-y-6">
    <!-- Date Range Filter -->
    <div class="card oh-filter-card p-4">
      <div class="flex flex-wrap items-center gap-3 sm:gap-4">
        <div class="flex items-center gap-2">
          <span class="text-xs font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">{{ t('dashboard.timeRange') }}:</span>
          <DateRangePicker :start-date="startDate" :end-date="endDate" @update:startDate="$emit('update:startDate', $event)" @update:endDate="$emit('update:endDate', $event)" @change="$emit('dateRangeChange', $event)" />
        </div>
        <button @click="$emit('refresh')" :disabled="loading" class="btn btn-secondary btn-sm">
          <span>↻</span>
          <span>{{ t('common.refresh') }}</span>
        </button>
        <div class="ml-auto flex items-center gap-2">
          <span class="text-xs font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">{{ t('dashboard.granularity') }}:</span>
          <div class="w-28">
            <Select :model-value="granularity" :options="[{value:'day', label:t('dashboard.day')}, {value:'hour', label:t('dashboard.hour')}]" @update:model-value="$emit('update:granularity', $event)" @change="$emit('granularityChange')" />
          </div>
        </div>
      </div>
    </div>

    <!-- Charts Grid -->
    <div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
      <!-- Model Distribution Chart -->
      <ModelDistributionChart :model-stats="models" :loading="loading" :enable-breakdown="false" :show-account-cost="false" />

      <!-- Token Usage Trend Chart -->
      <TokenUsageTrend :time-zone="timeZone" :trend-data="trend" :loading="loading" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import Select from '@/components/common/Select.vue'
import ModelDistributionChart from '@/components/charts/ModelDistributionChart.vue'
import TokenUsageTrend from '@/components/charts/TokenUsageTrend.vue'
import type { TrendDataPoint, ModelStat } from '@/types'

defineProps<{ timeZone?: string, loading: boolean, startDate: string, endDate: string, granularity: string, trend: TrendDataPoint[], models: ModelStat[] }>()
defineEmits(['update:startDate', 'update:endDate', 'update:granularity', 'dateRangeChange', 'granularityChange', 'refresh'])
const { t } = useI18n()

</script>

<style scoped>
.oh-filter-card {
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--card);
}
</style>
