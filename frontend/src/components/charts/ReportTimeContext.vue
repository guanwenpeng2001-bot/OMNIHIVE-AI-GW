<template>
  <p class="text-xs text-gray-600 dark:text-gray-400" data-testid="report-time-context">
    {{ t('common.displayTimeZone', { timezone: timeZone }) }} · {{ note }}
  </p>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getLocale } from '@/i18n'
defineProps<{ timeZone: string }>()
const { t } = useI18n()
const note = computed(() => getLocale().startsWith('zh')
  ? '日期按查询时区划分；图表与数据表使用同一份结果。概览“今日”沿接口默认日界，不能自定义。'
  : 'Dates use the query time zone; charts and tables share one result. Overview “today” uses the API default day boundary and cannot be customized.')
</script>
