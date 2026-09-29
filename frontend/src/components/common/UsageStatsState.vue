<template>
  <slot v-if="state === 'known'" />
  <div v-else role="status" class="text-sm text-gray-500 dark:text-gray-400">
    <template v-if="state === 'unknown' && historyGap">
      <p>{{ t('dashboard.historyUnavailable') }}</p>
      <p v-if="historyGap.rawRetainedFrom">
        {{ t('dashboard.rawRetainedFrom', { time: historyGap.rawRetainedFrom }) }}
      </p>
    </template>
    <span v-else>{{ t(state === 'loading' ? 'common.loading' : 'common.unknown') }}</span>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { HistoryGap } from '@/views/trendGaps'

defineProps<{
  state: 'loading' | 'known' | 'unknown'
  historyGap: HistoryGap | null
}>()

const { t } = useI18n()
</script>
