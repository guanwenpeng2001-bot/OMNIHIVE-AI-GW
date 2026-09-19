<template>
  <p class="mb-3 text-xs text-gray-600 dark:text-gray-400" data-testid="distribution-denominator">
    {{ description }} · {{ metric === 'actual_cost' ? `${t('dashboard.actual')} (USD)` : t('dashboard.tokens') }}: {{ total }}
  </p>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getLocale } from '@/i18n'
import { formatLedgerAmount, isKnownAmount } from '@/utils/money'
const props = defineProps<{ values: Array<number | null | undefined>; metric: 'tokens' | 'actual_cost' }>()
const { t } = useI18n()
const total = computed(() => {
  if (!props.values.every(isKnownAmount)) return '-'
  const sum = props.values.reduce<number>((subtotal, value) => subtotal + (value as number), 0)
  return props.metric === 'actual_cost' ? formatLedgerAmount(sum) : String(sum)
})
const description = computed(() => getLocale().startsWith('zh')
  ? '金额：所选范围内合计 USD（非单价）；占比的分母为当前图表全部项目合计'
  : 'Amounts: totals in USD for the selected range (not unit prices); percentage denominator is the sum of all chart entries')
</script>
