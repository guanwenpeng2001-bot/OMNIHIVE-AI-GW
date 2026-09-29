<template>
  <slot v-if="state === 'known'" />
  <div v-else role="status" class="text-sm text-gray-500 dark:text-gray-400">
    <span>{{ state === 'unknown' ? '—' : t(state === 'loading' ? 'common.loading' : 'usage.failedToLoad') }}</span>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { HistoryGap } from '@/views/trendGaps'

defineProps<{
  state: 'loading' | 'known' | 'unknown' | 'error'
  historyGap?: HistoryGap | null
}>()

const { t } = useI18n()
</script>
