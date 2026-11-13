<template>
  <div class="mb-4 space-y-2">
    <p class="text-xs leading-relaxed text-[var(--muted-foreground)]" data-testid="distribution-denominator">
      {{ description }} · {{ metric === 'actual_cost' ? `${t('dashboard.actual')} (USD)` : t('dashboard.tokens') }}:
      <strong class="font-mono font-medium tabular-nums text-[var(--foreground)]">{{ total }}</strong>
    </p>
    <div v-if="shares.length" class="flex h-1.5 overflow-hidden rounded-full bg-[var(--surface-2)]" aria-hidden="true">
      <span v-for="(share, index) in shares" :key="index" :style="{ width: `${share}%`, backgroundColor: palette[index % palette.length] }" :title="`${share.toFixed(1)}%`" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getLocale } from '@/i18n'
import { formatLedgerAmount, isKnownAmount } from '@/utils/money'
const props = defineProps<{ values: Array<number | null | undefined>; metric: 'tokens' | 'actual_cost'; colors?: string[] }>()
const { t } = useI18n()
const palette = computed(() => props.colors?.length ? props.colors : ['#d97706', '#059669', '#6366f1', '#fbbf24', '#34d399', '#a5b4fc', '#b45309', '#047857', '#4338ca', '#f59e0b', '#10b981', '#818cf8'])
const shares = computed(() => {
  if (!props.values.length || !props.values.every(value => isKnownAmount(value) && Number(value) >= 0)) return []
  const sum = props.values.reduce<number>((subtotal, value) => subtotal + Number(value), 0)
  return sum > 0 ? props.values.map(value => Number(value) / sum * 100) : []
})
const total = computed(() => {
  if (!props.values.every(isKnownAmount)) return '-'
  const sum = props.values.reduce<number>((subtotal, value) => subtotal + (value as number), 0)
  return props.metric === 'actual_cost' ? formatLedgerAmount(sum) : String(sum)
})
const description = computed(() => getLocale().startsWith('zh')
  ? '金额：所选范围内合计 USD（非单价）；占比的分母为当前图表全部项目合计'
  : 'Amounts: totals in USD for the selected range (not unit prices); percentage denominator is the sum of all chart entries')
</script>
